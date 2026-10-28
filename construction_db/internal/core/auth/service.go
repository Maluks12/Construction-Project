package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/platform/database"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

type Service struct {
	pool           *pgxpool.Pool
	secret         string
	defaultEdition string
	defaultRegime  string
}

// defaultEdition and defaultRegime are used for the organisation and first
// site created at signup.
func NewService(pool *pgxpool.Pool, secret, defaultEdition, defaultRegime string) *Service {
	return &Service{pool: pool, secret: secret, defaultEdition: defaultEdition, defaultRegime: defaultRegime}
}

// IssueTokens must run in a transaction whose tenant is {orgID, userID}.
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

	var tokens *TokenPair
	err := database.WithTenant(ctx, s.pool, database.Tenant{}, func(tx pgx.Tx) error {
		var user *User
		var err error
		if req.Email != "" {
			user, err = FindUserByEmail(ctx, tx, req.Email)
		} else {
			user, err = FindUserByMobile(ctx, tx, req.Mobile)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidCredentials
		}
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}

		if user.PasswordHash == nil || !CheckPassword(*user.PasswordHash, req.Password) {
			return ErrInvalidCredentials
		}
		if user.ArchivedAt != nil {
			return ErrInvalidCredentials
		}

		// The user's own memberships across orgs, before an org is chosen.
		if err := database.SetTenant(ctx, tx, database.Tenant{UserID: user.ID}); err != nil {
			return fmt.Errorf("login: %w", err)
		}
		memberships, err := ListMemberships(ctx, tx, user.ID)
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		orgID, err := pickOrg(memberships, req.OrgID)
		if err != nil {
			return err
		}

		if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: orgID, UserID: user.ID}); err != nil {
			return fmt.Errorf("login: %w", err)
		}
		tokens, err = s.IssueTokens(ctx, tx, user.ID, orgID, req.DeviceName, userAgent)
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tokens, nil
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

	var tokens *TokenPair
	err = database.WithTenant(ctx, s.pool, database.Tenant{}, func(tx pgx.Tx) error {
		// The org's id is picked first so the organisation row is inserted under its own tenant.
		orgID, err := NewID(ctx, tx)
		if err != nil {
			return fmt.Errorf("signup: %w", err)
		}
		if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: orgID}); err != nil {
			return fmt.Errorf("signup: %w", err)
		}
		if err := CreateOrganisation(ctx, tx, orgID, req.CompanyName, s.defaultEdition); err != nil {
			return fmt.Errorf("signup: %w", err)
		}

		userID, err := CreateUser(ctx, tx, req.FullName, req.Email, hash)
		if isEmailTaken(err) {
			return ErrEmailTaken
		}
		if err != nil {
			return fmt.Errorf("signup: %w", err)
		}

		if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: orgID, UserID: userID}); err != nil {
			return fmt.Errorf("signup: %w", err)
		}
		if err := AddMember(ctx, tx, orgID, userID, RoleOwner); err != nil {
			return fmt.Errorf("signup: %w", err)
		}
		if _, err := CreateSite(ctx, tx, orgID, s.defaultRegime, req.SiteName); err != nil {
			return fmt.Errorf("signup: %w", err)
		}

		tokens, err = s.IssueTokens(ctx, tx, userID, orgID, req.DeviceName, userAgent)
		if err != nil {
			return fmt.Errorf("signup: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
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
	hash := HashRefreshToken(refreshToken)

	orgID, userID, err := RefreshTokenTenant(ctx, s.pool, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidRefreshToken
	}
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}

	var tokens *TokenPair
	// A reused token revokes the session. That revoke must commit, so fn
	// returns nil and the error is reported after the transaction.
	reused := false
	err = database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, func(tx pgx.Tx) error {
		rt, err := FindRefreshToken(ctx, tx, hash)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidRefreshToken
		}
		if err != nil {
			return fmt.Errorf("refresh: %w", err)
		}

		if time.Now().After(rt.ExpiresAt) {
			return ErrInvalidRefreshToken
		}

		session, err := GetSession(ctx, tx, rt.SessionID)
		if err != nil {
			return fmt.Errorf("refresh: %w", err)
		}
		if !session.Active(time.Now()) {
			return ErrSessionRevoked
		}

		marked := false
		if rt.UsedAt == nil {
			if marked, err = MarkRefreshTokenUsed(ctx, tx, rt.ID); err != nil {
				return fmt.Errorf("refresh: %w", err)
			}
		}
		if !marked {
			if _, err := RevokeSession(ctx, tx, session.UserID, session.ID); err != nil {
				return fmt.Errorf("refresh: revoke after reuse: %w", err)
			}
			reused = true
			return nil
		}

		plain, newHash, err := NewRefreshToken()
		if err != nil {
			return fmt.Errorf("refresh: %w", err)
		}
		if err := InsertRefreshToken(ctx, tx, session.ID, newHash, session.ExpiresAt); err != nil {
			return fmt.Errorf("refresh: %w", err)
		}

		accessToken, err := NewAccessToken(session.UserID, session.OrgID, session.ID, s.secret, accessTokenTTL)
		if err != nil {
			return fmt.Errorf("refresh: %w", err)
		}

		tokens = &TokenPair{
			AccessToken:  accessToken,
			RefreshToken: plain,
			ExpiresIn:    int(accessTokenTTL.Seconds()),
			TokenType:    "Bearer",
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if reused {
		return nil, ErrSessionRevoked
	}
	return tokens, nil
}

// Sessions are visible to their own user in every org, so {UserID} is enough here.
func (s *Service) Logout(ctx context.Context, userID, sessionID string) error {
	err := database.WithTenant(ctx, s.pool, database.Tenant{UserID: userID}, func(tx pgx.Tx) error {
		_, err := RevokeSession(ctx, tx, userID, sessionID)
		return err
	})
	if err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

func (s *Service) ListSessions(ctx context.Context, userID, currentSessionID string) ([]SessionResponse, error) {
	var sessions []Session
	err := database.WithTenant(ctx, s.pool, database.Tenant{UserID: userID}, func(tx pgx.Tx) error {
		var err error
		sessions, err = ListSessions(ctx, tx, userID)
		return err
	})
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
	var revoked bool
	err := database.WithTenant(ctx, s.pool, database.Tenant{UserID: userID}, func(tx pgx.Tx) error {
		var err error
		revoked, err = RevokeSession(ctx, tx, userID, sessionID)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("revoke session: %w", err)
	}

	return revoked, nil
}
