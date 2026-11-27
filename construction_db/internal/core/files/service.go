package files

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
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

func IsLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

// mayUpload: supervisors capture certificates and medicals on site (D6);
// appointments and documents are the leads' (D20, D21).
func mayUpload(role, purpose string) bool {
	return IsLead(role) || purpose == PurposeCompetency || purpose == PurposeMedical
}

// errIDTaken: the insert hit the primary key, from a concurrent identical
// declare or another organisation's file; looking again tells them apart.
var errIDTaken = errors.New("file id taken")

// Declare records a file under an id the device chose and returns an upload
// URL while it is pending. An identical repeat answers 200 with a fresh URL.
func (s *Service) Declare(ctx context.Context, orgID, userID, role, fileID string, req DeclareRequest) (*File, bool, error) {
	id, err := uuid.Parse(fileID)
	if err != nil {
		return nil, false, ErrBadFileID
	}
	sum, err := checkDeclare(&req)
	if err != nil {
		return nil, false, err
	}
	if !mayUpload(role, req.Purpose) {
		return nil, false, ErrCantUpload
	}
	var (
		f       *File
		created bool
	)
	for attempt := 1; ; attempt++ {
		f, created, err = s.declareOnce(ctx, orgID, userID, role, id.String(), sum, req)
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
		return nil, false, fmt.Errorf("declare file: %w", err)
	}
	if f.Status == StatusPending {
		p, err := s.store.PresignPut(ctx, f.objectKey, sum, f.SizeBytes, f.ContentType, uploadTTL)
		if err != nil {
			return nil, false, fmt.Errorf("declare file: %w", err)
		}
		f.Upload = &evidence.Upload{Method: p.Method, URL: p.URL, Headers: p.Headers, ExpiresAt: p.ExpiresAt.UTC()}
	}
	return f, created, nil
}

func checkDeclare(req *DeclareRequest) ([]byte, error) {
	sum, err := hex.DecodeString(req.ContentSHA256)
	switch {
	case !slices.Contains(Purposes, req.Purpose):
		return nil, ErrBadPurpose
	case SitePurpose(req.Purpose) != (req.SiteID != nil):
		return nil, ErrSiteRequired
	case err != nil || len(sum) != 32:
		return nil, ErrBadHash
	case req.SizeBytes < 1 || req.SizeBytes > MaxSizeBytes:
		return nil, ErrBadSize
	case !slices.Contains(ContentTypes, req.ContentType):
		return nil, ErrBadType
	case !textOK(req.FileName, 200):
		return nil, ErrBadFileName
	case req.DeviceID != nil && (len(*req.DeviceID) == 0 || len(*req.DeviceID) > 200 || !utf8.ValidString(*req.DeviceID)):
		return nil, ErrBadDeviceID
	}
	req.ContentSHA256 = strings.ToLower(req.ContentSHA256)
	return sum, nil
}

func (s *Service) declareOnce(ctx context.Context, orgID, userID, role, id string, sum []byte, req DeclareRequest) (f *File, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var siteID *string
		if req.SiteID != nil {
			site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, *req.SiteID)
			if err != nil {
				return err
			}
			siteID = &site.ID
		}
		existing, err := GetFile(ctx, tx, orgID, id)
		switch {
		case err == nil && existing.Status == StatusRejected:
			return ErrRejected
		case err == nil && !sameDeclaration(existing, userID, siteID, req):
			return ErrIDReused
		case err == nil:
			f = existing
			return nil
		case !isNotFound(err):
			return err
		}
		err = insertFile(ctx, tx, newFile{
			ID: id, OrgID: orgID, SiteID: siteID, UploadedBy: userID, Purpose: req.Purpose, Sum: sum, Size: req.SizeBytes,
			ContentType: req.ContentType, FileName: req.FileName, DeviceID: req.DeviceID,
			ObjectKey: fmt.Sprintf("orgs/%s/files/%s", orgID, id),
		})
		if isUniqueViolation(err, "files_pkey") {
			return errIDTaken
		}
		if err != nil {
			return err
		}
		created = true
		f, err = GetFile(ctx, tx, orgID, id)
		return err
	})
	return f, created, err
}

func sameDeclaration(f *File, by string, siteID *string, req DeclareRequest) bool {
	return f.UploadedBy == by && f.Purpose == req.Purpose && equalPtr(f.SiteID, siteID) && f.ContentSHA256 == req.ContentSHA256 &&
		f.SizeBytes == req.SizeBytes && f.ContentType == req.ContentType && equalPtr(f.FileName, req.FileName) &&
		equalPtr(f.DeviceID, req.DeviceID)
}

// Get returns a file's record (never its bytes) to its uploader or a lead.
func (s *Service) Get(ctx context.Context, orgID, userID, role, fileID string) (*File, error) {
	var f *File
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var err error
		f, err = GetFile(ctx, tx, orgID, fileID)
		if isNotFound(err) || (err == nil && f.UploadedBy != userID && !IsLead(role)) {
			return ErrFileNotFound
		}
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("get file: %w", err)
	}
	return f, nil
}

// Attachable is the check a record runs in its own transaction before it
// attaches a file: the file exists, has the record's purpose (and site), and
// is verified. The record's foreign key enforces the same.
func Attachable(ctx context.Context, tx DBTX, orgID, fileID, purpose string, siteID *string) error {
	if _, err := uuid.Parse(fileID); err != nil {
		return ErrFileNotFound
	}
	f, err := GetFile(ctx, tx, orgID, fileID)
	switch {
	case isNotFound(err):
		return ErrFileNotFound
	case err != nil:
		return err
	case f.Purpose != purpose || (siteID != nil && !equalPtr(f.SiteID, siteID)):
		return ErrWrongPurpose
	case f.Status == StatusRejected:
		return ErrRejected
	case f.Status != StatusVerified:
		return ErrNotVerified
	}
	return nil
}

// IsSentinel reports whether err is one of this package's API errors, for
// record packages that pass them through.
func IsSentinel(err error) bool { return isSentinel(err) }

func isSentinel(err error) bool {
	for _, s := range Sentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	var rej *Rejected
	return errors.As(err, &rej)
}

// Sentinels are the errors the handlers send; TestAPIDoc checks each is quoted.
var Sentinels = []error{
	ErrBatchSize,
	sites.ErrSiteNotFound,
	ErrFileNotFound, ErrBadFileID, ErrBadPurpose, ErrSiteRequired, ErrBadHash, ErrBadSize, ErrBadType, ErrBadFileName,
	ErrBadDeviceID, ErrCantUpload, ErrIDReused, ErrRejected, ErrNotUploaded, ErrNotVerified, ErrWrongPurpose, ErrCantViewHealth,
}

func textOK(s *string, max int) bool {
	if s == nil {
		return true
	}
	n := utf8.RuneCountInString(*s)
	return utf8.ValidString(*s) && n >= 1 && n <= max && !strings.ContainsRune(*s, 0) && strings.TrimSpace(*s) != ""
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
