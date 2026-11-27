package workers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrBadImportID       = errors.New("import id must be a UUID")
	ErrBadDryRun         = errors.New("dry_run must be true or false")
	ErrImportIDReused    = errors.New("import id already used with a different file")
	ErrImportHasProblems = errors.New("the file has problems; nothing was imported")
	ErrRegisterChanged   = errors.New("the register changed while importing; send the file again")
)

// ImportRow is one worker of the file, as read: its row number, and the
// worker it became (null in a dry run or when the file has problems).
type ImportRow struct {
	Row         int     `json:"row"`
	WorkerID    *string `json:"worker_id"`
	Name        string  `json:"name"`
	Firm        *string `json:"firm"`
	ClockNumber *string `json:"clock_number"`
	Trade       *string `json:"trade"`
}

// ImportResult answers an import, a dry run and a repeat alike. Error is
// set only on the 400 for a file with problems.
type ImportResult struct {
	ImportID  string       `json:"import_id"`
	DryRun    bool         `json:"dry_run"`
	Encoding  string       `json:"encoding"`
	Separator string       `json:"separator"`
	Columns   []string     `json:"columns"`
	RowCount  int          `json:"row_count"`
	Rows      []ImportRow  `json:"rows"`
	Problems  []RowProblem `json:"problems"`
	Error     string       `json:"error,omitempty"`
}

// Encodings are ImportResult.Encoding's values.
var Encodings = []string{EncodingUTF8, EncodingWindows1252}

// Import reads a CSV of workers and adds them all, or none (ONBOARD D9,
// D10). importID is the dashboard's: a repeat with the same file is the
// first result (created false); with another file, ErrImportIDReused. A dry
// run checks everything and writes nothing. A file with problems returns
// the result and ErrImportHasProblems.
func (s *Service) Import(ctx context.Context, orgID, userID, importID string, body []byte, dryRun bool) (*ImportResult, bool, error) {
	id, err := uuid.Parse(importID)
	if err != nil {
		return nil, false, ErrBadImportID
	}
	p, err := ParseCSV(body)
	if err != nil {
		return nil, false, err
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	res := &ImportResult{ImportID: id.String(), DryRun: dryRun, Encoding: p.Encoding, Separator: p.Separator,
		Columns:  slices.DeleteFunc(slices.Clone(p.Columns), func(c string) bool { return c == "" }),
		RowCount: len(p.Rows), Rows: make([]ImportRow, len(p.Rows)), Problems: []RowProblem{}}
	for i, w := range p.Rows {
		res.Rows[i] = ImportRow{Row: p.Numbers[i], Name: w.Name, Firm: w.Firm, ClockNumber: w.ClockNumber, Trade: w.Trade}
	}

	created := false
	err = once(func() error {
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			if !dryRun {
				prev, err := getImport(ctx, tx, orgID, id.String())
				switch {
				case err == nil && prev.hash == hash:
					res.setWorkers(prev.workerIDs)
					return nil
				case err == nil:
					return ErrImportIDReused
				case !isNotFound(err):
					return err
				}
			}
			problems, err := CheckRows(ctx, tx, orgID, p.Rows, p.Numbers)
			if err != nil {
				return err
			}
			res.Problems = append(slices.Clone(p.Problems), problems...)
			slices.SortStableFunc(res.Problems, func(a, b RowProblem) int { return a.Row - b.Row })
			if len(res.Problems) > 0 {
				if dryRun {
					return nil
				}
				res.Error = ErrImportHasProblems.Error()
				return ErrImportHasProblems
			}
			if dryRun {
				return nil
			}
			ids := make([]string, len(p.Rows))
			for i := range ids {
				ids[i] = uuid.NewString()
			}
			// The import first: a second request with this id waits on its
			// key, then finds it (errRetry reads it as a repeat).
			if err := insertImport(ctx, tx, orgID, id.String(), userID, hash, p.Encoding, ids); err != nil {
				if isUniqueViolation(err, "worker_imports_pkey") {
					return errRetry
				}
				return err
			}
			if err := insertWorkers(ctx, tx, orgID, userID, ids, p.Rows); err != nil {
				if isUniqueViolation(err, "workers_identity") {
					// Someone added one of these people since CheckRows read
					// the register; nothing is written.
					return ErrRegisterChanged
				}
				return err
			}
			res.setWorkers(ids)
			created = true
			return nil
		})
	})
	if errors.Is(err, ErrImportHasProblems) {
		return res, false, err
	}
	if err != nil {
		return nil, false, wrapImport(err)
	}
	return res, created, nil
}

func (r *ImportResult) setWorkers(ids []string) {
	for i := range r.Rows {
		if i < len(ids) {
			id := ids[i]
			r.Rows[i].WorkerID = &id
		}
	}
}

func wrapImport(err error) error {
	for _, e := range []error{ErrImportIDReused, ErrRegisterChanged} {
		if errors.Is(err, e) {
			return e
		}
	}
	return fmt.Errorf("import workers: %w", err)
}

type importRecord struct {
	hash      string
	workerIDs []string
}

func getImport(ctx context.Context, db DBTX, orgID, id string) (*importRecord, error) {
	var r importRecord
	err := db.QueryRow(ctx, `SELECT file_sha256, worker_ids::text[] FROM worker_imports WHERE org_id = $1 AND id = $2`,
		orgID, id).Scan(&r.hash, &r.workerIDs)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func insertImport(ctx context.Context, db DBTX, orgID, id, by, hash, encoding string, workerIDs []string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO worker_imports (id, org_id, imported_by, file_sha256, encoding, row_count, worker_ids)
		VALUES ($1, $2, $3, $4, $5, $6, $7::uuid[])`, id, orgID, by, hash, encoding, len(workerIDs), workerIDs)
	return err
}

// insertWorkers adds every row in one statement: thousands of single
// inserts could outlast the request's deadline.
func insertWorkers(ctx context.Context, db DBTX, orgID, by string, ids []string, rows []WorkerRequest) error {
	names := make([]string, len(rows))
	firms, clocks, trades := make([]*string, len(rows)), make([]*string, len(rows)), make([]*string, len(rows))
	for i, r := range rows {
		names[i], firms[i], clocks[i], trades[i] = r.Name, r.Firm, r.ClockNumber, r.Trade
	}
	_, err := db.Exec(ctx, `
		INSERT INTO workers (id, org_id, name, firm, clock_number, trade, active, created_by)
		SELECT u.id, $1, u.name, u.firm, u.clock_number, u.trade, true, $2
		FROM unnest($3::uuid[], $4::text[], $5::text[], $6::text[], $7::text[]) WITH ORDINALITY
		     AS u (id, name, firm, clock_number, trade, n)
		ORDER BY u.n`, orgID, by, ids, names, firms, clocks, trades)
	return err
}

// ImportFileProblem is the 400 for a problem with the whole file: the
// column or the row where it was found, when there is one.
type ImportFileProblem struct {
	Error  string  `json:"error"`
	Column *string `json:"column"`
	Row    *int    `json:"row"`
}

// ImportErrors are every error text the import sends (workers.md quotes each).
var ImportErrors = []error{ErrBadImportID, ErrBadDryRun, ErrImportIDReused, ErrImportHasProblems, ErrRegisterChanged,
	ErrNotCSV, ErrEmptyFile, ErrNotText, ErrNoNameColumn, ErrUnknownColumn, ErrRepeatedColumn, ErrBadCSV,
	ErrNoWorkers, ErrTooManyRows}
