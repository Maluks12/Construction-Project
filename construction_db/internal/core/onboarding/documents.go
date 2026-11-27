package onboarding

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/documents"
	"construction_db/internal/core/files"
	"construction_db/internal/core/workers"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/httpapi"
	"construction_db/internal/regimes"
)

type Service struct {
	pool      *pgxpool.Pool
	workers   *workers.Service
	documents *documents.Service
	reg       *regimes.Registry
	contrib   Contributor
	Clock     clock.Clock
}

func NewService(pool *pgxpool.Pool, w *workers.Service, d *documents.Service, reg *regimes.Registry, contrib Contributor) *Service {
	return &Service{pool: pool, workers: w, documents: d, reg: reg, contrib: contrib}
}

// errRollback ends a transaction that must write nothing (a dry run, or a
// batch with problems) after its result is built.
var errRollback = errors.New("rollback")

// errRace: an item lost a race for its id to a request that committed
// meanwhile; the whole batch runs again, and that item is then a repeat.
var errRace = errors.New("race")

// SaveDocuments creates every item, or none (D11). Each item goes through
// its own endpoint's in-transaction path, so it is checked exactly as it
// would be alone, and repeating the batch is safe. Each runs in a savepoint,
// so one refused item doesn't stop the others being checked: the answer
// lists every problem. Files must be verified first.
func (s *Service) SaveDocuments(ctx context.Context, orgID, userID, role string, req DocumentsRequest, dryRun bool) (*DocumentsResult, error) {
	if n := len(req.Items); n < 1 || n > MaxDocuments {
		return nil, ErrBatchSize
	}
	var res *DocumentsResult
	run := func() error {
		res = &DocumentsResult{DryRun: dryRun, Items: []ItemResult{}, Problems: []ItemProblem{}}
		err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, func(tx pgx.Tx) error {
			seen := map[string]int{}
			for i, it := range req.Items {
				if _, ok := seen[it.ID]; ok && it.ID != "" {
					res.Problems = append(res.Problems, ItemProblem{Index: i, Error: ErrRepeatedID.Error()})
					continue
				}
				seen[it.ID] = i
				created, err := s.saveItem(ctx, tx, orgID, userID, role, it)
				switch {
				case err == nil:
					res.Items = append(res.Items, ItemResult{Index: i, Kind: it.Kind, ID: it.ID, Created: created})
					if created {
						res.Created++
					} else {
						res.Repeated++
					}
				case workers.IsRetry(err) || documents.IsRetry(err):
					return errRace
				case clientError(err):
					res.Problems = append(res.Problems, problemOf(i, err))
				default:
					return fmt.Errorf("item %d: %w", i, err)
				}
			}
			if len(res.Problems) > 0 || dryRun {
				return errRollback
			}
			return nil
		})
		if errors.Is(err, errRollback) {
			return nil
		}
		return err
	}
	err := run()
	if errors.Is(err, errRace) {
		err = run()
	}
	if errors.Is(err, errRace) {
		// Lost twice: busy, 503 with Retry-After; the batch is safe to resend.
		return nil, fmt.Errorf("save documents: %w", httpapi.ErrBusy)
	}
	if err != nil {
		return nil, fmt.Errorf("save documents: %w", err)
	}
	if len(res.Problems) > 0 {
		res.Items = []ItemResult{}
		res.Created, res.Repeated = 0, 0
		if dryRun {
			return res, nil
		}
		for _, p := range res.Problems {
			if p.Error == files.ErrNotVerified.Error() {
				res.Error = ErrFilesNotReady.Error()
				return res, ErrFilesNotReady
			}
		}
		res.Error = ErrHasProblems.Error()
		return res, ErrHasProblems
	}
	return res, nil
}

// saveItem runs one item in a savepoint of tx: refused, it is rolled back
// alone and the batch goes on.
func (s *Service) saveItem(ctx context.Context, tx pgx.Tx, orgID, userID, role string, it DocumentItem) (bool, error) {
	if err := checkShape(it); err != nil {
		return false, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return false, err
	}
	var created bool
	switch it.Kind {
	case KindCompetency:
		_, created, err = s.workers.PutCompetencyTx(ctx, sp, orgID, userID, *it.WorkerID, it.ID, *it.Competency)
	case KindMedical:
		_, created, err = s.workers.PutMedicalTx(ctx, sp, orgID, userID, *it.WorkerID, it.ID, *it.Medical)
	case KindVaultDocument:
		_, created, err = s.documents.CreateTx(ctx, sp, orgID, userID, role, documents.Where{Scope: documents.ScopeOrg}, it.ID, *it.Document)
	}
	if err != nil {
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			return false, rbErr
		}
		return false, err
	}
	return created, sp.Commit(ctx)
}

func checkShape(it DocumentItem) error {
	switch it.Kind {
	case KindCompetency:
		if it.Competency == nil || it.WorkerID == nil || it.Medical != nil || it.Document != nil {
			return ErrPayload
		}
	case KindMedical:
		if it.Medical == nil || it.WorkerID == nil || it.Competency != nil || it.Document != nil {
			return ErrPayload
		}
	case KindVaultDocument:
		if it.Document == nil || it.WorkerID != nil || it.Competency != nil || it.Medical != nil {
			return ErrPayload
		}
	default:
		return ErrBadKind
	}
	return nil
}

func clientError(err error) bool {
	return errors.Is(err, ErrPayload) || errors.Is(err, ErrBadKind) ||
		workers.IsClientError(err) || documents.IsClientError(err)
}

func problemOf(i int, err error) ItemProblem {
	var p *workers.Problem
	if errors.As(err, &p) {
		f := p.Field
		return ItemProblem{Index: i, Field: &f, Error: p.FieldProblem.Error}
	}
	return ItemProblem{Index: i, Error: err.Error()}
}
