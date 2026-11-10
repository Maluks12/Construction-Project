package evidence

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/sites"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/storage"
)

type Service struct {
	pool  *pgxpool.Pool
	sites *sites.Service
	store storage.Store
}

func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service, store storage.Store) *Service {
	return &Service{pool: pool, sites: sitesSvc, store: store}
}

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

// Declare records evidence the client is about to upload and returns where to
// PUT the bytes. created is false for an idempotent repeat.
func (s *Service) Declare(ctx context.Context, orgID, userID, role, siteID, evidenceID string, req DeclareRequest) (ev *Evidence, created bool, err error) {
	id, err := uuid.Parse(evidenceID)
	if err != nil {
		return nil, false, ErrBadEvidenceID
	}
	sum, err := hex.DecodeString(req.ContentSHA256)
	if err != nil || len(sum) != 32 {
		return nil, false, ErrBadHash
	}
	if (req.Lat == nil) != (req.Lng == nil) {
		return nil, false, ErrLocationPair
	}
	// Bytes, not characters: the column and the chain encoding both count bytes.
	if d := req.DeviceID; d != nil && (len(*d) == 0 || len(*d) > 200 || !utf8.ValidString(*d)) {
		return nil, false, ErrBadDeviceID
	}
	// Postgres keeps microseconds; truncating here makes a retry compare equal.
	req.ClientCapturedAt = req.ClientCapturedAt.Truncate(time.Microsecond)

	// The insert can lose a race to an identical declare (a client retrying
	// before its first request finished). The second attempt sees the winner's
	// committed row and answers as an idempotent repeat.
	for attempt := 1; ; attempt++ {
		ev, created, err = s.declareOnce(ctx, orgID, userID, role, siteID, id.String(), sum, req)
		if !errors.Is(err, errIDTaken) || attempt == 2 {
			break
		}
	}
	if errors.Is(err, errIDTaken) {
		err = ErrIDReused
	}
	if isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("declare evidence: %w", err)
	}

	if ev.Status == StatusPending {
		p, err := s.store.PresignPut(ctx, ev.ObjectKey, sum, ev.SizeBytes, uploadTTL)
		if err != nil {
			return nil, false, fmt.Errorf("declare evidence: %w", err)
		}
		headers := map[string]string{"Content-Type": ev.ContentType}
		maps.Copy(headers, p.Headers)
		ev.Upload = &Upload{Method: p.Method, URL: p.URL, Headers: headers, ExpiresAt: p.ExpiresAt.UTC()}
	}
	return ev, created, nil
}

// errIDTaken: the insert hit the primary key. Either a concurrent declare of
// the same id committed first, or the id belongs to another organisation and
// RLS hides it. Declare tells the two apart by looking again.
var errIDTaken = errors.New("evidence id taken")

func (s *Service) declareOnce(ctx context.Context, orgID, userID, role, siteID, id string, sum []byte, req DeclareRequest) (ev *Evidence, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}

		existing, err := GetEvidence(ctx, tx, orgID, id)
		switch {
		case err == nil && existing.Status == StatusRejected:
			return ErrRejected
		case err == nil && !sameDeclaration(existing, site.ID, userID, req):
			return ErrIDReused
		case err == nil:
			ev = existing
			return nil
		case !isNotFound(err):
			return err
		}

		err = InsertEvidence(ctx, tx, newEvidence{
			ID:               id,
			OrgID:            orgID,
			SiteID:           site.ID,
			UploadedBy:       userID,
			ContentSHA256:    sum,
			SizeBytes:        req.SizeBytes,
			ContentType:      req.ContentType,
			ClientCapturedAt: req.ClientCapturedAt,
			Lat:              req.Lat,
			Lng:              req.Lng,
			DeviceID:         req.DeviceID,
			ObjectKey:        fmt.Sprintf("orgs/%s/sites/%s/evidence/%s", orgID, site.ID, id),
		})
		if isUniqueViolation(err, "evidence_pkey") {
			return errIDTaken
		}
		if err != nil {
			return err
		}
		created = true
		ev, err = GetEvidence(ctx, tx, orgID, id)
		return err
	})
	return ev, created, err
}

// sameDeclaration: a retry must match everything that will be sealed.
func sameDeclaration(e *Evidence, siteID, userID string, req DeclareRequest) bool {
	return e.SiteID == siteID &&
		e.UploadedBy == userID &&
		e.ContentSHA256 == hexLower(req.ContentSHA256) &&
		e.SizeBytes == req.SizeBytes &&
		e.ContentType == req.ContentType &&
		e.ClientCapturedAt.Equal(req.ClientCapturedAt) &&
		equalPtr(e.Lat, req.Lat) &&
		equalPtr(e.Lng, req.Lng) &&
		equalPtr(e.DeviceID, req.DeviceID)
}

// Get logs the read in the same transaction: if the log row can't be
// written, the caller gets an error, not the evidence.
func (s *Service) Get(ctx context.Context, orgID, userID, role, siteID, evidenceID string, client Client) (*Evidence, error) {
	var ev *Evidence
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		ev, err = GetSiteEvidence(ctx, tx, orgID, site.ID, evidenceID)
		if isNotFound(err) {
			return ErrEvidenceNotFound
		}
		if err != nil {
			return err
		}
		return InsertAccess(ctx, tx, accessEntry{
			OrgID: orgID, SiteID: site.ID, UserID: userID, Action: ActionViewRecord,
			EvidenceID: &ev.ID, Client: client,
		})
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("get evidence: %w", err)
	}
	return ev, nil
}

// List logs one row for the whole request, with its filters and how many
// items it returned, in the same transaction as the read.
func (s *Service) List(ctx context.Context, orgID, userID, role, siteID, status string, limit int, client Client) ([]Evidence, error) {
	if status != "" && status != StatusPending && status != StatusVerified && status != StatusRejected {
		return nil, ErrBadStatus
	}
	if limit < 1 || limit > 200 {
		return nil, ErrBadLimit
	}

	var items []Evidence
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		items, err = ListEvidence(ctx, tx, orgID, site.ID, status, limit)
		if err != nil {
			return err
		}
		var filterStatus *string
		if status != "" {
			filterStatus = &status
		}
		count := len(items)
		return InsertAccess(ctx, tx, accessEntry{
			OrgID: orgID, SiteID: site.ID, UserID: userID, Action: ActionList,
			FilterStatus: filterStatus, FilterLimit: &limit, ResultCount: &count, Client: client,
		})
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("list evidence: %w", err)
	}
	return items, nil
}

// isSentinel reports errors the handler maps to a status, which must reach it unwrapped.
func isSentinel(err error) bool {
	for _, s := range []error{sites.ErrSiteNotFound, ErrEvidenceNotFound, ErrIDReused, ErrRejected} {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

func isNotFound(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == constraint
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func hexLower(s string) string {
	b, _ := hex.DecodeString(s)
	return hex.EncodeToString(b)
}
