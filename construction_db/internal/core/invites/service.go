package invites

import (
	"construction_db/internal/core/auth"
	"construction_db/internal/platform/database"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const inviteTTL = 7 * 24 * time.Hour

type Service struct {
	pool *pgxpool.Pool
	auth *auth.Service
}

func NewService(pool *pgxpool.Pool, authSvc *auth.Service) *Service {
	return &Service{pool: pool, auth: authSvc}
}

// clientErrors are sent to the client as err.Error(), so they must not be wrapped.
var clientErrors = []error{
	ErrInvalidInvite, ErrWrongPassword, ErrBlankName, ErrContactRequired,
	ErrAlreadyMember, ErrCannotInviteOwner, ErrInviteNotFound, ErrContactConflict,
}

func wrap(prefix string, err error) error {
	for _, ce := range clientErrors {
		if errors.Is(err, ce) {
			return err
		}
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func (s *Service) Invite(ctx context.Context, orgID, inviterID, inviterRole string, req InviteRequest) (*InviteResponse, error) {
	name := strings.TrimSpace(req.FullName)
	if name == "" {
		return nil, ErrBlankName
	}

	if req.Email == nil && req.Mobile == nil {
		return nil, ErrContactRequired
	}

	if req.Role == auth.RoleOwner && inviterRole != auth.RoleOwner {
		return nil, ErrCannotInviteOwner
	}

	var resp *InviteResponse
	err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: inviterID}, func(tx pgx.Tx) error {
		userID, err := findOrCreateUser(ctx, tx, req.Email, req.Mobile, name)
		if err != nil {
			return err
		}

		upserted, err := UpsertInvitedMember(ctx, tx, orgID, userID, req.Role)
		if err != nil {
			return err
		}
		if !upserted {
			return ErrAlreadyMember
		}

		if err := RevokePendingInvites(ctx, tx, orgID, userID); err != nil {
			return err
		}

		plain, hash, err := auth.NewRefreshToken()
		if err != nil {
			return err
		}
		expiresAt := time.Now().Add(inviteTTL)

		inviteID, err := CreateInvitation(ctx, tx, orgID, userID, hash, inviterID, expiresAt)
		if err != nil {
			return err
		}

		resp = &InviteResponse{
			ID:        inviteID,
			UserID:    userID,
			Token:     plain,
			ExpiresAt: expiresAt,
		}
		return nil
	})
	if err != nil {
		return nil, wrap("invite", err)
	}
	return resp, nil
}

func (s *Service) ListPending(ctx context.Context, orgID string) ([]PendingInvite, error) {
	var invites []PendingInvite
	err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		var err error
		invites, err = ListPendingInvites(ctx, tx, orgID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list pending invites: %w", err)
	}
	return invites, nil
}

func (s *Service) Revoke(ctx context.Context, orgID, inviteID string) error {
	var revoked bool
	err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		var err error
		revoked, err = RevokeInvite(ctx, tx, orgID, inviteID)
		return err
	})
	if isNotFound(err) {
		return ErrInviteNotFound
	}
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	if !revoked {
		return ErrInviteNotFound
	}
	return nil
}

func isNotFound(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

func findOrCreateUser(ctx context.Context, db DBTX, email, mobile *string, fullName string) (string, error) {
	var byEmail, byMobile string

	if email != nil {
		id, err := FindUserIDByEmail(ctx, db, *email)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		byEmail = id
	}

	if mobile != nil {
		id, err := FindUserIDByMobile(ctx, db, *mobile)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		byMobile = id
	}

	switch {
	case byEmail != "" && byMobile != "" && byEmail != byMobile:
		return "", ErrContactConflict
	case byEmail != "":
		return byEmail, nil
	case byMobile != "":
		return byMobile, nil
	}

	userID, err := CreateInvitedUser(ctx, db, email, mobile, fullName)
	if isUniqueViolation(err, "users_email_unique") || isUniqueViolation(err, "users_mobile_unique") {
		return "", ErrContactConflict
	}
	if err != nil {
		return "", err
	}
	return userID, nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == constraint
}

func (s *Service) Preview(ctx context.Context, token string) (*InvitePreview, error) {
	hash := auth.HashRefreshToken(token)
	orgID, err := InvitationOrg(ctx, s.pool, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidInvite
	}
	if err != nil {
		return nil, fmt.Errorf("preview invite: %w", err)
	}

	var p *InvitePreview
	err = database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		var err error
		p, err = GetInvitePreview(ctx, tx, hash)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidInvite
	}
	if err != nil {
		return nil, fmt.Errorf("preview invite: %w", err)
	}
	return p, nil
}

func (s *Service) Accept(ctx context.Context, req AcceptRequest, userAgent string) (*auth.TokenPair, error) {
	hash := auth.HashRefreshToken(req.Token)
	orgID, err := InvitationOrg(ctx, s.pool, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidInvite
	}
	if err != nil {
		return nil, fmt.Errorf("accept invite: %w", err)
	}

	var tokens *auth.TokenPair
	err = database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		a, err := FindInviteForAccept(ctx, tx, hash)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidInvite
		}
		if err != nil {
			return err
		}

		if a.AcceptedAt != nil || a.RevokedAt != nil || a.ArchivedAt != nil || time.Now().After(a.ExpiresAt) {
			return ErrInvalidInvite
		}

		if a.PasswordHash == nil {
			hash, err := auth.HashPassword(req.Password)
			if err != nil {
				return err
			}
			ok, err := SetPassword(ctx, tx, a.UserID, hash)
			if err != nil {
				return err
			}
			if !ok {
				return ErrInvalidInvite
			}
		} else if !auth.CheckPassword(*a.PasswordHash, req.Password) {
			return ErrWrongPassword
		}

		ok, err := MarkInviteAccepted(ctx, tx, a.ID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidInvite
		}

		ok, err = ActivateMember(ctx, tx, a.OrgID, a.UserID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidInvite
		}

		if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: a.OrgID, UserID: a.UserID}); err != nil {
			return err
		}
		tokens, err = s.auth.IssueTokens(ctx, tx, a.UserID, a.OrgID, req.DeviceName, userAgent)
		return err
	})
	if err != nil {
		return nil, wrap("accept invite", err)
	}
	return tokens, nil
}
