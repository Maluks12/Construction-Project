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

func GetSiteRegimeCode(ctx context.Context, db DBTX, orgID, siteID string) (string, error) {
	query := `
		SELECT r.code
		FROM sites s
		JOIN regulatory_regimes r ON r.id = s.regime_id
		WHERE s.org_id = $1 AND s.id = $2
	`
	var code string
	if err := db.QueryRow(ctx, query, orgID, siteID).Scan(&code); err != nil {
		return "", fmt.Errorf("get site regime code: %w", err)
	}
	return code, nil
}

func CreateSite(ctx context.Context, db DBTX, orgID, regimeCode, name string, address *string) (*Site, error) {
	query := `
		INSERT INTO sites (org_id, regime_id, name, address)
		SELECT $1, id, $3, $4 FROM regulatory_regimes WHERE code = $2
		RETURNING id, org_id, name, address, status, created_at
	`
	var s Site
	err := db.QueryRow(ctx, query, orgID, regimeCode, name, address).Scan(
		&s.ID,
		&s.OrgID,
		&s.Name,
		&s.Address,
		&s.Status,
		&s.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("create site: %w", err)
	}
	return &s, nil
}

func UpdateSite(ctx context.Context, db DBTX, orgID, siteID string, name, address *string) (*Site, error) {
	query := `
		UPDATE sites
		SET name = COALESCE($3, name),
			address = COALESCE($4, address),
			updated_at = now()
		WHERE org_id = $1 AND id = $2 AND archived_at IS NULL
		RETURNING id, org_id, name, address, status, created_at
	`

	var s Site
	err := db.QueryRow(ctx, query, orgID, siteID, name, address).Scan(
		&s.ID,
		&s.OrgID,
		&s.Name,
		&s.Address,
		&s.Status,
		&s.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("update site: %w", err)
	}

	return &s, nil
}

func ArchiveSite(ctx context.Context, db DBTX, orgID, siteID string) (bool, error) {
	query := `
		UPDATE sites
		SET status = 'archived', archived_at = now(), updated_at = now()
		WHERE org_id = $1 AND id = $2 AND archived_at IS NULL
	`
	tag, err := db.Exec(ctx, query, orgID, siteID)

	if err != nil {
		return false, fmt.Errorf("archive site: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func ListAssignedSites(ctx context.Context, db DBTX, orgID, userID string) ([]Site, error) {
	query := `
		SELECT s.id, s.org_id, s.name, s.address, s.status, s.created_at
		FROM sites s
		JOIN site_members m ON m.site_id = s.id
		WHERE s.org_id = $1 AND m.user_id = $2 AND s.archived_at IS NULL
		ORDER BY s.created_at
	`
	rows, err := db.Query(ctx, query, orgID, userID)
	if err != nil {
		return nil, fmt.Errorf("list assigned sites: %w", err)
	}
	defer rows.Close()

	sites := []Site{}
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.OrgID, &s.Name, &s.Address, &s.Status, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan assigned sites: %w", err)
		}
		sites = append(sites, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list assigned sites: %w", err)
	}
	return sites, nil
}

func GetAssignedSite(ctx context.Context, db DBTX, orgID, userID, siteID string) (*Site, error) {
	query := `
		SELECT s.id, s.org_id, s.name, s.address, s.status, s.created_at
		FROM sites s
		JOIN site_members m ON m.site_id = s.id
		WHERE s.org_id = $1 AND m.user_id = $2 AND s.id = $3
	`
	var s Site

	err := db.QueryRow(ctx, query, orgID, userID, siteID).Scan(
		&s.ID,
		&s.OrgID,
		&s.Name,
		&s.Address,
		&s.Status,
		&s.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get assigned site: %w", err)
	}

	return &s, nil
}

func ListSiteMembers(ctx context.Context, db DBTX, orgID, siteID string) ([]SiteMember, error) {
	query := `
		SELECT u.id, u.full_name, u.email, u.mobile, om.role
		FROM site_members sm
		JOIN users u ON u.id = sm.user_id
		JOIN org_members om ON om.org_id = sm.org_id AND om.user_id = sm.user_id
		WHERE sm.org_id = $1 AND sm.site_id = $2
		ORDER BY u.full_name
	`

	rows, err := db.Query(ctx, query, orgID, siteID)
	if err != nil {
		return nil, fmt.Errorf("list site members: %w", err)
	}
	defer rows.Close()

	members := []SiteMember{}

	for rows.Next() {
		var m SiteMember
		if err := rows.Scan(&m.UserID, &m.FullName, &m.Email, &m.Mobile, &m.Role); err != nil {
			return nil, fmt.Errorf("scan site members: %w", err)
		}
		members = append(members, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list site members: %w", err)
	}
	return members, nil
}

func AddSiteMember(ctx context.Context, db DBTX, orgID, siteID, userID string) error {
	query := `
		INSERT INTO site_members (org_id, site_id, user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (site_id, user_id) DO NOTHING
	`

	_, err := db.Exec(ctx, query, orgID, siteID, userID)

	if err != nil {
		return fmt.Errorf("add site member: %w", err)
	}

	return nil
}

func RemoveSiteMember(ctx context.Context, db DBTX, orgID, siteID, userID string) (bool, error) {
	query := `
		DELETE FROM site_members
		WHERE org_id = $1 AND site_id = $2 AND user_id = $3
	`
	tag, err := db.Exec(ctx, query, orgID, siteID, userID)

	if err != nil {
		return false, fmt.Errorf("remove site member: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func ListLocations(ctx context.Context, db DBTX, orgID, siteID string) ([]Location, error) {
	query := `
		SELECT id, site_id, parent_id, kind, name, created_at
		FROM site_locations
		WHERE org_id = $1 AND site_id = $2 AND archived_at IS NULL
		ORDER BY created_at
	`
	rows, err := db.Query(ctx, query, orgID, siteID)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	defer rows.Close()

	locations := []Location{}
	for rows.Next() {
		var l Location
		if err := rows.Scan(&l.ID, &l.SiteID, &l.ParentID, &l.Kind, &l.Name, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan locations: %w", err)
		}
		locations = append(locations, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}

	return locations, nil
}

func CreateLocation(ctx context.Context, db DBTX, orgID, siteID string, parentID *string, kind, name string) (*Location, error) {
	query := `
		INSERT INTO site_locations (org_id, site_id, parent_id, kind, name)
		SELECT $1, $2, $3, $4, $5
		WHERE $3::uuid IS NULL OR EXISTS (
			SELECT 1 FROM site_locations p
			WHERE p.org_id = $1 AND p.site_id = $2 AND p.id = $3 AND p.archived_at IS NULL
		)
		RETURNING id, site_id, parent_id, kind, name, created_at
	`
	var l Location
	err := db.QueryRow(ctx, query, orgID, siteID, parentID, kind, name).Scan(
		&l.ID,
		&l.SiteID,
		&l.ParentID,
		&l.Kind,
		&l.Name,
		&l.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("create location: %w", err)
	}

	return &l, nil
}

func RenameLocation(ctx context.Context, db DBTX, orgID, siteID, locationID, name string) (*Location, error) {
	query := `
		UPDATE site_locations
		SET name = $4, updated_at = now()
		WHERE org_id = $1 AND site_id = $2 AND id = $3 AND archived_at IS NULL
		RETURNING id, site_id, parent_id, kind, name, created_at
	`
	var l Location
	err := db.QueryRow(ctx, query, orgID, siteID, locationID, name).Scan(
		&l.ID,
		&l.SiteID,
		&l.ParentID,
		&l.Kind,
		&l.Name,
		&l.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("rename location: %w", err)
	}

	return &l, nil
}

func HasActiveChildren(ctx context.Context, db DBTX, orgID, siteID, locationID string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM site_locations
			WHERE org_id =$1 AND site_id = $2 AND parent_id = $3 AND archived_at IS NULL
		)
	`
	var exists bool
	if err := db.QueryRow(ctx, query, orgID, siteID, locationID).Scan(&exists); err != nil {
		return false, fmt.Errorf("has active children: %w", err)
	}

	return exists, nil
}

func ArchiveLocation(ctx context.Context, db DBTX, orgID, siteID, locationID string) (bool, error) {
	query := `
		UPDATE site_locations
		SET archived_at = now(), updated_at = now()
		WHERE org_id = $1 AND site_id = $2 AND id = $3 AND archived_at IS NULL
	`

	tag, err := db.Exec(ctx, query, orgID, siteID, locationID)

	if err != nil {
		return false, fmt.Errorf("archive location: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}
