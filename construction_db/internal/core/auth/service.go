package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

type Service struct {
	pool   *pgxpool.Pool
	secret string
}

func NewService(pool *pgxpool.Pool, secret string) *Service {
	return &Service{pool: pool, secret: secret}
}

func (s *Service) IssueTokens(ctx context.Context, db DBTX, userID, orgID, deviceName, userAgent string) (*TokenPair, error) {
	expiresAt := time.Now().Add(refreshTokenTTL)

	sessionID, err := CreateSession(ctx, db, userID, orgID, deviceName, userAgent, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("issue tokens: %w", err)
	}

	plain, hash, err := NewRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("issue tokens: %w", err)
	}

	if err := InsertRefreshToken(ctx, db, sessionID, hash, expiresAt); err != nil {
		return nil, fmt.Errorf("issue tokens: %w", err)
	}

	accessToken, err := NewAccessToken(userID, orgID, sessionID, s.secret, accessTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("issue tokens: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: plain,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

func (s *Service) Login(ctx context.Context, req LoginRequest, userAgent string) (*TokenPair, error) {
	if (req.Email == "") == (req.Mobile == "") {
		return nil, ErrLoginIDRequired
	}

	var user *User
	var err error

	if req.Email != "" {
		user, err = FindUserByEmail(ctx, s.pool, req.Email)
	} else {
		user, err = FindUserByMobile(ctx, s.pool, req.Mobile)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}

	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	if user.PasswordHash == nil || !CheckPassword(*user.PasswordHash, req.Password) {
		return nil, ErrInvalidCredentials
	}

	if user.ArchivedAt != nil {
		return nil, ErrInvalidCredentials
	}

	memberships, err := ListMemberships(ctx, s.pool, user.ID)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	orgID, err := pickOrg(memberships, req.OrgID)
	if err != nil {
		return nil, err
	}

	return s.IssueTokens(ctx, s.pool, user.ID, orgID, req.DeviceName, userAgent)
}

func pickOrg(memberships []Membership, requested string) (string, error) {
	if len(memberships) == 0 {
		return "", ErrInvalidCredentials
	}
	if requested != "" {
		for _, m := range memberships {
			if m.OrgID == requested {
				return m.OrgID, nil
			}
		}
		return "", ErrNotMember
	}

	if len(memberships) == 1 {
		return memberships[0].OrgID, nil
	}

	return "", ErrOrgRequired
}

func (s *Service) Signup(ctx context.Context, req SignupRequest, userAgent string) (*TokenPair, error) {
	hash, err := HashPassword(req.Password)
	if err != nil {
		return nil, fmt.Errorf("signup: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("signup: %w", err)
	}
	defer tx.Rollback(ctx)

	orgID, err := CreateOrganisation(ctx, tx, req.CompanyName)
	if err != nil {
		return nil, fmt.Errorf("signup: %w", err)
	}

	userID, err := CreateUser(ctx, tx, req.FullName, req.Email, hash)
	if isEmailTaken(err) {
		return nil, ErrEmailTaken
	}

	if err != nil {
		return nil, fmt.Errorf("signup: %w", err)
	}

	if err := AddMember(ctx, tx, orgID, userID, RoleOwner); err != nil {
		return nil, fmt.Errorf("signup: %w", err)
	}

	if _, err := CreateSite(ctx, tx, orgID, RegimeConstruction, req.SiteName); err != nil {
		return nil, fmt.Errorf("signup: %w", err)
	}

	tokens, err := s.IssueTokens(ctx, tx, userID, orgID, req.DeviceName, userAgent)
	if err != nil {
		return nil, fmt.Errorf("signup: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("signup: %w", err)
	}
	return tokens, nil
}

func isEmailTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "users_email_unique"
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	rt, err := FindRefreshToken(ctx, s.pool, HashRefreshToken(refreshToken))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("refresh: %w", err)
	}

	if time.Now().After(rt.ExpiresAt) {
		return nil, ErrInvalidRefreshToken
	}

	session, err := GetSession(ctx, s.pool, rt.SessionID)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	if !session.Active(time.Now()) {
		return nil, ErrSessionRevoked
	}

	if rt.UsedAt != nil {
		return nil, s.revokeForReuse(ctx, session)
	}

	marked, err := MarkRefreshTokenUsed(ctx, s.pool, rt.ID)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	if !marked {
		return nil, s.revokeForReuse(ctx, session)
	}

	plain, hash, err := NewRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}

	if err := InsertRefreshToken(ctx, s.pool, session.ID, hash, session.ExpiresAt); err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}

	accessToken, err := NewAccessToken(session.UserID, session.OrgID, session.ID, s.secret, accessTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: plain,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

func (s *Service) revokeForReuse(ctx context.Context, session *Session) error {
	if _, err := RevokeSession(ctx, s.pool, session.UserID, session.ID); err != nil {
		return fmt.Errorf("refresh: revoke after reuse: %w", err)
	}
	return ErrSessionRevoked
}

func (s *Service) Logout(ctx context.Context, userID, sessionID string) error {
	if _, err := RevokeSession(ctx, s.pool, userID, sessionID); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

func (s *Service) ListSessions(ctx context.Context, userID, currentSessionID string) ([]SessionResponse, error) {
	sessions, err := ListSessions(ctx, s.pool, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	out := make([]SessionResponse, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, SessionResponse{
			ID:         sess.ID,
			DeviceName: sess.DeviceName,
			UserAgent:  sess.UserAgent,
			CreatedAt:  sess.CreatedAt,
			LastUsedAt: sess.LastUsedAt,
			Current:    sess.ID == currentSessionID,
		})
	}
	return out, nil
}

func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) (bool, error) {
	revoked, err := RevokeSession(ctx, s.pool, userID, sessionID)
	if err != nil {
		return false, fmt.Errorf("revoke session: %w", err)
	}

	return revoked, nil
}
