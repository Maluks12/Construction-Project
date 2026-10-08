package auth

import (
	"context"
	"fmt"
	//"os/user"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func CreateOrganisation(ctx context.Context, db DBTX, name string) (orgID string, err error) {
	query := `
		INSERT INTO organisations (trading_name, primary_edition, enabled_editions)
		VALUES($1, $2, $3)
		RETURNING id
	`
	err = db.QueryRow(ctx,query, name, EditionConstruction, []string{EditionConstruction}).Scan(&orgID)

	if err != nil {
		return "", fmt.Errorf("create organisation: %w", err)
	}
	return orgID, nil
}

func CreateUSer(ctx context.Context, db DBTX, fullName, email, passwordHash string) (userID string, err error) {

	query := `
		INSERT INTO users (email, full_name, password_hash)
		VALUES($1, $2, $3)
		RETURNING id
	`
	err = db.QueryRow(ctx, query, email, fullName, passwordHash).Scan(
		&userID,
	)

	if err != nil{
		return "", fmt.Errorf("create user: %w", err)
	}

	return userID, nil
}

func AddMember(ctx context.Context, db DBTX, orgID, userID, role string) error {
	query := `
		INSERT INTO org_members (org_id, user_id, role)
		VALUES ($1, $2, $3)
	`
	_, err := db.Exec(ctx, query,orgID, userID, role )

	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	return nil
}

func CreateSite(ctx context.Context, db DBTX, orgID, regimeCode, name string) (siteID string, err error) {
	query := `
		INSERT INTO sites (org_id, regime_id, name)
		SELECT $1, id, $3
		FROM regulatory_regimes
		WHERE code = $2
		RETURNING id
	`
	err = db.QueryRow(ctx, query, orgID, regimeCode, name).Scan(&siteID)

	if err != nil {
		return "", fmt.Errorf("create site: %w", err)
	}
	return siteID, nil
}

func FindUserByEmail(ctx context.Context, db DBTX, email string) (*User, error) {
	query := `
		SELECT email, mobile, full_name, password_hash, archived_at
		FROM users
		WHERE lower(email) = lower($1)
	`
	var u User
	err := db.QueryRow(ctx, query, email).Scan(
		&u.ID,
		&u.Email,
		&u.Mobile,
		&u.FullName,
		&u.PasswordHash,
		&u.ArchivedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("find user by email: %w", err)
	}
	return &u, nil
}

func ListMemberships(ctx context.Context, db DBTX, userID string) ([]Membership, error) {
	query := `
		SELECT org_id, user_id, role, status
		FROM org_members
		WHERE user_id = $1 AND status = 'active'
		ORDER BY created_at
	`
	rows, err := db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	defer rows.Close()

	var memberships []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.OrgID, &m.UserID, &m.Role, &m.Status); err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		memberships = append(memberships, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	return memberships, nil
}

func CreateSession(ctx context.Context, db DBTX, userID, orgID, deviceName, userAgent string, expiresAt time.Time) (sessionID string, err error) {
	query := `
		INSERT INTO sessions (user_id, org_id, device_name, user_agent, expires_at)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5)
		RETURNING id
	`

	err = db.QueryRow(ctx, query, userID, orgID, deviceName, userAgent, expiresAt).Scan(&sessionID)

	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	return sessionID, nil
}

func InsertRefreshToken(ctx context.Context, db DBTX, sessionID, hash string, expiresAt time.Time) error {
	query := `
		INSERT INTO refresh_tokens (session_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`
	_, err := db.Exec(ctx, query, sessionID, hash, expiresAt)

	if err != nil {
		return fmt.Errorf("insert refreshtoken: %w", err)
	}
	return nil
}

func FindRefreshToken(ctx context.Context, db DBTX, hash string) (*RefreshToken, error) {
	query := `
		SELECT id, session_id, token_hash, expires_at, used_at
		FROM refresh_tokens
		WHERE token_hash = $1
	`
	var r RefreshToken
	err := db.QueryRow(ctx, query, hash).Scan(
		&r.ID,
		&r.SessionID,
		&r.TokenHash,
		&r.ExpiresAt,
		&r.UsedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("find refresh token")
	}

	return &r, nil
}

func MarkRefreshTokenUsed(ctx context.Context, db DBTX, id string) (bool, error) {
	query := `
		UPDATE refresh_tokens
		SET used_at = now()
		WHERE id = $1 AND used_at IS NULL
	`

	tag, err := db.Exec(ctx, query, id)
	if err != nil {
		return false, fmt.Errorf("mark refresh token used: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func GetSession(ctx context.Context, db DBTX, sessionID string) (*Session, error) {
	query := `
		SELECT id, user_id, org_id, device_name, user_agent, created_at, last_used_at, expires_at, revoked_at
		FROM sessions
		WHERE id = $1
	`
	var s Session
	err := db.QueryRow(ctx, query, sessionID).Scan(
		&s.ID,
		&s.UserID,
		&s.OrgID,
		&s.DeviceName,
		&s.UserAgent,
		&s.CreatedAt,
		&s.LastUsedAt,
		&s.ExpiresAt,
		&s.RevokedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	return &s, nil
}

func ListSessions(ctx context.Context, db DBTX, userID string) ([]Session, error) {
	query := `
		SELECT id, user_id, org_id, device_name, user_agent, created_at, last_used_at, expires_at, revoked_at
		FROM sessions
		WHERE user_id = $1 
		AND revoked_at IS NULL
		AND expires_at > now()
		ORDER BY last_used_at DESC
	`
	rows, err := db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.OrgID, &s.DeviceName, &s.UserAgent, &s.CreatedAt, &s.LastUsedAt, &s.ExpiresAt, &s.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan sessions: %w", err)
		}
		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	return sessions, nil
}

func RevokeSession(ctx context.Context, db DBTX, userID, sessionID string) (bool, error) {
	query := `
		UPDATE sessions
		SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
	`  
	tag, err := db.Exec(ctx, query, userID, sessionID)
	if err != nil {
		return false, fmt.Errorf("revoke session: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
