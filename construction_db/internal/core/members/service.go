package members

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
)

type Service struct {
	pool  *pgxpool.Pool
	Clock clock.Clock
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// defaultStatuses is the list's default: everyone but the removed.
var defaultStatuses = []string{StatusInvited, StatusActive, StatusSuspended}

func (s *Service) List(ctx context.Context, orgID, status string) ([]Member, error) {
	statuses := defaultStatuses
	if status != "" {
		if !slices.Contains(Statuses, status) {
			return nil, ErrBadStatusFilter
		}
		statuses = []string{status}
	}
	var out []Member
	err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		var err error
		out, err = ListMembers(ctx, tx, orgID, statuses)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, orgID, userID string) (*MemberDetail, error) {
	var d *MemberDetail
	err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		var err error
		d, err = s.detail(ctx, tx, orgID, userID)
		return err
	})
	if err != nil {
		return nil, wrap("get member", err)
	}
	return d, nil
}

func (s *Service) detail(ctx context.Context, tx pgx.Tx, orgID, userID string) (*MemberDetail, error) {
	m, err := GetMember(ctx, tx, orgID, userID)
	if isNotFound(err) {
		return nil, ErrMemberNotFound
	}
	if err != nil {
		return nil, err
	}
	actions, err := OpenActions(ctx, tx, orgID, userID, s.Clock.Now())
	if err != nil {
		return nil, err
	}
	return &MemberDetail{Member: *m, OpenWork: OpenWork{Actions: actions, SiteIDs: m.SiteIDs}}, nil
}

// transitionOK mirrors 000054's statuses for a PATCH (D2): active and
// suspended swap; active, suspended and invited can be removed.
func transitionOK(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case StatusActive:
		return to == StatusSuspended || to == StatusRemoved
	case StatusSuspended:
		return to == StatusActive || to == StatusRemoved
	case StatusInvited:
		return to == StatusRemoved
	}
	return false
}

// Update changes userID's role, status or both, as actorID (D1-D8), in one
// transaction that takes the organisation's lock first (D3): the order every
// writer of org_members follows.
func (s *Service) Update(ctx context.Context, orgID, actorID, actorRole, userID string, req UpdateRequest) (*Change, error) {
	reason := strings.TrimSpace(req.Reason)
	switch {
	case req.Role == nil && req.Status == nil:
		return nil, ErrNothingToChange
	case utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 500:
		return nil, ErrBadReason
	case req.Role != nil && !slices.Contains(auth.Roles, *req.Role):
		return nil, ErrBadRole
	case req.Status != nil && !slices.Contains(PatchStatuses, *req.Status):
		return nil, ErrBadStatus
	case userID == actorID:
		return nil, ErrSelf
	}
	var siteIDs []string
	if req.SiteIDs != nil {
		siteIDs = []string{}
		for _, id := range *req.SiteIDs {
			if _, err := uuid.Parse(id); err != nil {
				return nil, ErrBadSiteIDs
			}
			siteIDs = append(siteIDs, id)
		}
	}

	var out *Change
	err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: actorID}, func(tx pgx.Tx) error {
		if err := database.LockOrganisation(ctx, tx, orgID); err != nil {
			return err
		}
		m, err := GetMember(ctx, tx, orgID, userID)
		if isNotFound(err) {
			return ErrMemberNotFound
		}
		if err != nil {
			return err
		}
		role, status := m.Role, m.Status
		if req.Role != nil {
			role = *req.Role
		}
		if req.Status != nil {
			status = *req.Status
		}
		if (m.Status == StatusInvited || m.Status == StatusRemoved) && role != m.Role {
			return ErrInviteRole
		}
		if !transitionOK(m.Status, status) {
			return ErrTransition
		}
		if !auth.MemberChangeAllowed(actorRole, m.Role, role) {
			return ErrOwnerOnly
		}
		into := role == auth.RoleSupervisor && m.Role != auth.RoleSupervisor && status != StatusRemoved
		switch {
		case into && siteIDs == nil:
			return ErrSiteIDsRequired
		case !into && siteIDs != nil:
			return ErrSiteIDsUnexpected
		}

		// Nothing changes: no UPDATE, so no updated_at, sync stamp or history.
		if role != m.Role || status != m.Status {
			if err := database.SetMemberChange(ctx, tx, reason, siteIDs); err != nil {
				return err
			}
			if err := UpdateMember(ctx, tx, orgID, userID, role, status); err != nil {
				return fromDatabase(err)
			}
		}
		d, err := s.detail(ctx, tx, orgID, userID)
		if err != nil {
			return err
		}
		cleared := []string{}
		for _, id := range m.SiteIDs {
			if !slices.Contains(d.SiteIDs, id) {
				cleared = append(cleared, id)
			}
		}
		out = &Change{MemberDetail: *d, ClearedSiteIDs: cleared}
		return nil
	})
	if err != nil {
		return nil, wrap("update member", err)
	}
	return out, nil
}

func (s *Service) History(ctx context.Context, orgID, userID string) (*History, error) {
	var h History
	err := database.WithTenantSnapshot(ctx, s.pool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		if _, err := GetMember(ctx, tx, orgID, userID); isNotFound(err) {
			return ErrMemberNotFound
		} else if err != nil {
			return err
		}
		var err error
		if h.History, err = ListHistory(ctx, tx, orgID, userID); err != nil {
			return err
		}
		h.SiteHistory, err = ListSiteHistory(ctx, tx, orgID, userID)
		return err
	})
	if err != nil {
		return nil, wrap("member history", err)
	}
	return &h, nil
}

// fromDatabase maps 000054's refusals, by constraint name, to the API's
// errors. The service checks the same rules first, so these are what only
// the database can see: mainly a concurrent change (in two owners demoting
// each other, the second is ErrLastOwner here).
func fromDatabase(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.ConstraintName {
	case "org_members_last_owner":
		return ErrLastOwner
	case "org_members_site_ids":
		if strings.Contains(pgErr.Message, "required") {
			return ErrSiteIDsRequired
		}
		return ErrBadSiteIDs
	case "org_members_self":
		return ErrSelf
	case "org_members_actor":
		return ErrOwnerOnly
	case "org_members_reason":
		return ErrBadReason
	case "org_members_transition":
		return ErrTransition
	}
	return err
}

func isNotFound(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

// wrap leaves the sentinels bare for the handler and wraps anything else.
func wrap(prefix string, err error) error {
	for _, e := range Sentinels {
		if errors.Is(err, e) {
			return e
		}
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
