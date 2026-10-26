package invites

import (
	"construction_db/internal/core/auth"
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
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("invite: %w", err)
	}
	defer tx.Rollback(ctx)

	userID, err := findOrCreateUser(ctx, s.pool, req.Email, req.Mobile, name)

	if err != nil {
		return nil, fmt.Errorf("invite: %w", err)
	}

	upserted, err := UpsertInvitedMember(ctx, s.pool, orgID, userID, req.Role)
	if err != nil {
		return nil, fmt.Errorf("invite: %w", err)
	}

	if !upserted {
		return nil, ErrAlreadyMember
	}

	if err := RevokePendingInvites(ctx, s.pool, orgID, userID); err != nil {
		return nil, fmt.Errorf("invite: %w", err)
	}

	plain, hash, err := auth.NewRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("invite: %w", err)
	}

	expiresAt := time.Now().Add(inviteTTL)

	inviteID, err := CreateInvitation(ctx, s.pool, orgID, userID, hash, inviterID, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("invite: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("create invite: %w", err)
	}

	return &InviteResponse{
		ID:        inviteID,
		UserID:    userID,
		Token:     plain,
		ExpiresAt: expiresAt,
	}, nil

}

func (s *Service) ListPending(ctx context.Context, orgID string) ([]PendingInvite, error) {
	invites, err := ListPendingInvites(ctx, s.pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("list pending invites: %w", err)
	}
	return invites, nil
}

func (s *Service) Revoke(ctx context.Context, orgID, inviteID string) error {
	revoked, err := RevokeInvite(ctx, s.pool, orgID, inviteID)
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
