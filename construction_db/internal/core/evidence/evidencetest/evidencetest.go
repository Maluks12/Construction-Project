// Package evidencetest reads the evidence chain specification, CHAIN.md, so
// tests anywhere check the code against the document itself.
package evidencetest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jackc/pgx/v5"
)

// CHAINPath is internal/core/evidence/CHAIN.md, wherever the tests run from.
func CHAINPath() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("locate evidencetest.go")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "CHAIN.md"), nil
}

// Block returns the fenced block that follows <!-- marker --> in CHAIN.md.
func Block(marker string) (string, error) {
	path, err := CHAINPath()
	if err != nil {
		return "", err
	}
	doc, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read CHAIN.md: %w", err)
	}
	_, rest, ok := strings.Cut(string(doc), "<!-- "+marker+" -->")
	if !ok {
		return "", fmt.Errorf("CHAIN.md has no %q marker", marker)
	}
	_, rest, ok = strings.Cut(rest, "```")
	if !ok {
		return "", fmt.Errorf("CHAIN.md: no fenced block after %q", marker)
	}
	_, rest, _ = strings.Cut(rest, "\n") // skip the language tag
	body, _, ok := strings.Cut(rest, "```")
	if !ok {
		return "", fmt.Errorf("CHAIN.md: unterminated block after %q", marker)
	}
	return strings.ReplaceAll(body, "\r\n", "\n"), nil
}

// Check is one row of CHAIN.md's verification query.
type Check struct {
	Seq                                int64
	EvidenceID                         string
	SeqOK, PrevOK, MetadataOK, ChainOK bool
}

func (c Check) OK() bool { return c.SeqOK && c.PrevOK && c.MetadataOK && c.ChainOK }

type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Recompute runs CHAIN.md's verification query, exactly as published, for one site.
func Recompute(ctx context.Context, db Querier, siteID string) ([]Check, error) {
	query, err := Block("recompute-sql")
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, query, siteID)
	if err != nil {
		return nil, fmt.Errorf("recompute query: %w", err)
	}
	checks, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Check, error) {
		var c Check
		err := r.Scan(&c.Seq, &c.EvidenceID, &c.SeqOK, &c.PrevOK, &c.MetadataOK, &c.ChainOK)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("recompute query: %w", err)
	}
	return checks, nil
}
