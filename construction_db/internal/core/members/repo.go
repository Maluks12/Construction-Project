package members

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const memberSelect = `
	SELECT m.user_id, u.full_name, u.email, u.mobile, m.role, m.status,
	       coalesce((SELECT array_agg(s.site_id::text ORDER BY s.site_id) FROM site_members s
	                 WHERE s.org_id = m.org_id AND s.user_id = m.user_id), '{}'),
	       m.created_at, m.updated_at
	FROM org_members m JOIN users u ON u.id = m.user_id`

func scanMember(row pgx.Row) (*Member, error) {
	var m Member
	if err := row.Scan(&m.UserID, &m.FullName, &m.Email, &m.Mobile, &m.Role, &m.Status, &m.SiteIDs, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func ListMembers(ctx context.Context, db DBTX, orgID string, statuses []string) ([]Member, error) {
	rows, err := db.Query(ctx, memberSelect+`
		WHERE m.org_id = $1 AND m.status = ANY ($2)
		ORDER BY lower(u.full_name), m.user_id`, orgID, statuses)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, fmt.Errorf("list members: %w", err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return out, nil
}

func GetMember(ctx context.Context, db DBTX, orgID, userID string) (*Member, error) {
	m, err := scanMember(db.QueryRow(ctx, memberSelect+` WHERE m.org_id = $1 AND m.user_id = $2`, orgID, userID))
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	return m, nil
}

// OpenActions are the actions userID owns, open or awaiting verification.
func OpenActions(ctx context.Context, db DBTX, orgID, userID string, now time.Time) ([]OpenAction, error) {
	rows, err := db.Query(ctx, `
		SELECT id, site_id, title, status, due_on::text, (status = 'open' AND $3 >= due_at), action_site_member(org_id, site_id, owner_id)
		FROM corrective_actions
		WHERE org_id = $1 AND owner_id = $2 AND status IN ('open', 'awaiting_verification')
		ORDER BY due_on, id`, orgID, userID, now)
	if err != nil {
		return nil, fmt.Errorf("open actions: %w", err)
	}
	defer rows.Close()
	out := []OpenAction{}
	for rows.Next() {
		var a OpenAction
		if err := rows.Scan(&a.ID, &a.SiteID, &a.Title, &a.Status, &a.DueOn, &a.Overdue, &a.OwnerEligible); err != nil {
			return nil, fmt.Errorf("open actions: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("open actions: %w", err)
	}
	return out, nil
}

// UpdateMember writes the new role and status; the 000054 triggers enforce
// the rules and do the rest (sites, sessions, emails, history).
func UpdateMember(ctx context.Context, db DBTX, orgID, userID, role, status string) error {
	tag, err := db.Exec(ctx, `
		UPDATE org_members SET role = $3, status = $4, updated_at = now()
		WHERE org_id = $1 AND user_id = $2`, orgID, userID, role, status)
	if err != nil {
		return fmt.Errorf("update member: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("update member: %w", pgx.ErrNoRows)
	}
	return nil
}

func ListHistory(ctx context.Context, db DBTX, orgID, userID string) ([]HistoryEntry, error) {
	rows, err := db.Query(ctx, `
		SELECT h.role, h.status, h.full_name, h.prev_role, h.prev_status, h.reason,
		       h.site_ids::text[], h.prev_site_ids::text[], h.changed_at, h.changed_by, actor.full_name
		FROM member_history h LEFT JOIN users actor ON actor.id = h.changed_by
		WHERE h.org_id = $1 AND h.user_id = $2
		ORDER BY h.changed_at, h.id`, orgID, userID)
	if err != nil {
		return nil, fmt.Errorf("member history: %w", err)
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var e HistoryEntry
		if err := rows.Scan(&e.Role, &e.Status, &e.FullName, &e.PrevRole, &e.PrevStatus, &e.Reason,
			&e.SiteIDs, &e.PrevSiteIDs, &e.ChangedAt, &e.ChangedBy, &e.ChangedByName); err != nil {
			return nil, fmt.Errorf("member history: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("member history: %w", err)
	}
	return out, nil
}

func ListSiteHistory(ctx context.Context, db DBTX, orgID, userID string) ([]SiteHistoryEntry, error) {
	rows, err := db.Query(ctx, `
		SELECT site_id, change, changed_at, changed_by FROM site_member_history
		WHERE org_id = $1 AND user_id = $2
		ORDER BY changed_at, id`, orgID, userID)
	if err != nil {
		return nil, fmt.Errorf("site member history: %w", err)
	}
	defer rows.Close()
	out := []SiteHistoryEntry{}
	for rows.Next() {
		var e SiteHistoryEntry
		if err := rows.Scan(&e.SiteID, &e.Change, &e.ChangedAt, &e.ChangedBy); err != nil {
			return nil, fmt.Errorf("site member history: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("site member history: %w", err)
	}
	return out, nil
}
