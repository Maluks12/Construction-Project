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

// GetSiteRegime returns the id and code of the site's regime
// (sites.regime_id, never the organisation's).
func GetSiteRegime(ctx context.Context, db DBTX, orgID, siteID string) (id, code string, err error) {
	query := `
		SELECT r.id, r.code
		FROM sites s
		JOIN regulatory_regimes r ON r.id = s.regime_id
		WHERE s.org_id = $1 AND s.id = $2
	`
	if err := db.QueryRow(ctx, query, orgID, siteID).Scan(&id, &code); err != nil {
		return "", "", fmt.Errorf("get site regime: %w", err)
	}
	return id, code, nil
}

// ListVisibleSites is every site the caller can open, archived ones too,
// with its regime code. all is false for a supervisor: only their sites.
func ListVisibleSites(ctx context.Context, db DBTX, orgID, userID string, all bool) ([]VisibleSite, error) {
	rows, err := db.Query(ctx, `
		SELECT s.id::text, r.code
		FROM sites s
		JOIN regulatory_regimes r ON r.id = s.regime_id
		WHERE s.org_id = $1 AND ($3 OR EXISTS (SELECT 1 FROM site_members m WHERE m.site_id = s.id AND m.user_id = $2))
		ORDER BY s.created_at, s.id`, orgID, userID, all)
	if err != nil {
		return nil, fmt.Errorf("list visible sites: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[VisibleSite])
	if err != nil {
		return nil, fmt.Errorf("list visible sites: %w", err)
	}
	return out, nil
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

// locationColumns is every location's SELECT list, in Scan order.
const locationColumns = `id, site_id, parent_id, kind, name, position, created_at`

func scanLocation(row pgx.Row) (*Location, error) {
	var l Location
	if err := row.Scan(&l.ID, &l.SiteID, &l.ParentID, &l.Kind, &l.Name, &l.Position, &l.CreatedAt); err != nil {
		return nil, err
	}
	return &l, nil
}

// ListLocations orders siblings by position (numbered first), then name; the
// list is flat, so that order holds within each parent.
func ListLocations(ctx context.Context, db DBTX, orgID, siteID string) ([]Location, error) {
	query := `
		SELECT ` + locationColumns + `
		FROM site_locations
		WHERE org_id = $1 AND site_id = $2 AND archived_at IS NULL
		ORDER BY position NULLS LAST, lower(name), created_at, id
	`
	rows, err := db.Query(ctx, query, orgID, siteID)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	defer rows.Close()

	locations := []Location{}
	for rows.Next() {
		l, err := scanLocation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan locations: %w", err)
		}
		locations = append(locations, *l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}

	return locations, nil
}

func CreateLocation(ctx context.Context, db DBTX, orgID, siteID string, parentID *string, kind, name string, position *int) (*Location, error) {
	query := `
		INSERT INTO site_locations (org_id, site_id, parent_id, kind, name, position)
		SELECT $1, $2, $3, $4, $5, $6
		WHERE $3::uuid IS NULL OR EXISTS (
			SELECT 1 FROM site_locations p
			WHERE p.org_id = $1 AND p.site_id = $2 AND p.id = $3 AND p.archived_at IS NULL
		)
		RETURNING ` + locationColumns
	l, err := scanLocation(db.QueryRow(ctx, query, orgID, siteID, parentID, kind, name, position))
	if err != nil {
		return nil, fmt.Errorf("create location: %w", err)
	}
	return l, nil
}

// UpdateLocation sets name unless it is nil, and position when setPosition
// (nil clears it).
func UpdateLocation(ctx context.Context, db DBTX, orgID, siteID, locationID string, name *string, setPosition bool, position *int) (*Location, error) {
	query := `
		UPDATE site_locations
		SET name = coalesce($4, name),
		    position = CASE WHEN $5 THEN $6 ELSE position END,
		    updated_at = now()
		WHERE org_id = $1 AND site_id = $2 AND id = $3 AND archived_at IS NULL
		RETURNING ` + locationColumns
	l, err := scanLocation(db.QueryRow(ctx, query, orgID, siteID, locationID, name, setPosition, position))
	if err != nil {
		return nil, fmt.Errorf("update location: %w", err)
	}
	return l, nil
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
