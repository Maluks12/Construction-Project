package sites

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func ListSites(ctx context.Context, db DBTX, orgID string) ([]Site, error) {
	query := `
		SELECT id, org_id, name, address, status, created_at
		FROM sites
		WHERE org_id = $1 AND archived_at IS NULL
		ORDER BY created_at
	`

	rows, err := db.Query(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	defer rows.Close()

	sites := []Site{}
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.OrgID, &s.Name, &s.Address, &s.Status, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan sites: %w", err)
		}
		sites = append(sites, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	return sites, nil
}

func GetSite(ctx context.Context, db DBTX, orgID, siteID string) (*Site, error) {
	query := `
		SELECT id, org_id, name, address, status, created_at
		FROM sites
		WHERE org_id = $1 AND id = $2
	`
	var s Site

	err := db.QueryRow(ctx, query, orgID, siteID).Scan(
		&s.ID,
		&s.OrgID,
		&s.Name,
		&s.Address,
		&s.Status,
		&s.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("get site: %w", err)
	}
	return &s, nil
}
