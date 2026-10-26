package invites

import (
	"context"
	"errors"
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

func FindUserIDByEmail(ctx context.Context, db DBTX, email string) (id string, err error) {
	query := `
		SELECT id FROM users
		WHERE lower(email) = lower($1)
	`

	err = db.QueryRow(ctx, query, email).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("find user by email: %w", err)
	}

	return id, nil
}

func FindUserIDByMobile(ctx context.Context, db DBTX, mobile string) (id string, err error) {
	query := `
		SELECT id FROM users
		WHERE mobile = $1
	`
	err = db.QueryRow(ctx, query, mobile).Scan(&id)

	if err != nil {
		return "", fmt.Errorf("find user id by mobile: %w", err)
	}

	return id, nil
}

func CreateInvitedUser(ctx context.Context, db DBTX, email, mobile *string, fullName string) (userID string, err error) {
	query := `
		INSERT INTO users (email, mobile, full_name)
		VALUES ($1, $2, $3)
		RETURNING id
	`
	err = db.QueryRow(ctx, query, email, mobile, fullName).Scan(&userID)

	if err != nil {
		return "", fmt.Errorf("create invited user: %w", err)
	}

	return userID, nil
}

func UpsertInvitedMember(ctx context.Context, db DBTX, orgID, userID, role string) (bool, error) {
	query := `
		INSERT INTO org_members (org_id, user_id, role, status)
		VALUES ($1, $2, $3, 'invited')
		ON CONFLICT (org_id, user_id) DO UPDATE
			SET role = EXCLUDED.role, status = 'invited', updated_at = now()
			WHERE org_members.status IN ('invited', 'removed')
		RETURNING user_id
	`

	var returnedID string
	err := db.QueryRow(ctx, query, orgID, userID, role).Scan(&returnedID)

	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("upsert invited member: %w", err)
	}

	return true, nil
}

func RevokePendingInvites(ctx context.Context, db DBTX, orgID, userID string) error {
	query := `
		UPDATE invitations
		SET revoked_at = now()
		WHERE org_id = $1 AND user_id = $2 AND accepted_at IS NULL AND revoked_at IS NULL
	`

	_, err := db.Exec(ctx, query, orgID, userID)

	if err != nil {
		return fmt.Errorf("revoke pending invites: %w", err)
	}

	return nil
}

func CreateInvitation(ctx context.Context, db DBTX, orgID, userID, tokenHash, invitedBy string, expiresAt time.Time) (id string, err error) {
	query := `
		INSERT INTO invitations (org_id, user_id, token_hash, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`

	err = db.QueryRow(ctx, query, orgID, userID, tokenHash, invitedBy, expiresAt).Scan(&id)

	if err != nil {
		return "", fmt.Errorf("create invitation: %w", err)
	}

	return id, nil
}

func ListPendingInvites(ctx context.Context, db DBTX, orgID string) ([]PendingInvite, error) {
	query := `
		SELECT i.id, i.user_id, u.full_name, u.email, u.mobile, om.role, i.created_at, i.expires_at
		FROM invitations i
		JOIN users u ON u.id = i.user_id
		JOIN org_members om ON om.org_id = i.org_id AND om.user_id = i.user_id
		WHERE i.org_id = $1
		AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > now()
		ORDER BY i.created_at DESC
	`
	rows, err := db.Query(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("list pending invites: %w", err)
	}
	defer rows.Close()

	invites := []PendingInvite{}

	for rows.Next() {
		var i PendingInvite
		if err := rows.Scan(&i.ID, &i.UserID, &i.FullName, &i.Email, &i.Mobile, &i.Role, &i.CreatedAt, &i.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan pending invites: %w", err)
		}
		invites = append(invites, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending invites: %w", err)
	}

	return invites, nil
}

func RevokeInvite(ctx context.Context, db DBTX, orgID, inviteID string) (bool, error) {
	query := `
		UPDATE invitations SET revoked_at = now()
		WHERE org_id = $1 AND id = $2 AND accepted_at IS NULL AND revoked_at IS NULL
	`
	tag, err := db.Exec(ctx, query, orgID, inviteID)

	if err != nil {
		return false, fmt.Errorf("revoke invite: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}
