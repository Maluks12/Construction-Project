// Package members is team management (ONBOARD): the organisation's members,
// changes of role and status, and their history. The rules are the
// database's (000054); the service checks the same ones first so the API
// answers with a clear 4xx, and maps the database's refusals when only it
// can see them (two owners demoting each other).
package members

import (
	"errors"
	"time"
)

const (
	StatusInvited   = "invited"
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusRemoved   = "removed"
)

// Statuses are org_members.status's values; PatchStatuses what a PATCH sets.
var (
	Statuses      = []string{StatusInvited, StatusActive, StatusSuspended, StatusRemoved}
	PatchStatuses = []string{StatusActive, StatusSuspended, StatusRemoved}
)

var (
	ErrMemberNotFound    = errors.New("member not found")
	ErrNothingToChange   = errors.New("send role, status or both")
	ErrBadReason         = errors.New("reason must be 1-500 characters")
	ErrBadRole           = errors.New("role must be owner, she_officer, supervisor, claims_handler, contracts_manager or bookkeeper")
	ErrBadStatus         = errors.New("status must be active, suspended or removed")
	ErrBadStatusFilter   = errors.New("status must be invited, active, suspended or removed")
	ErrSelf              = errors.New("you can't change your own membership; ask another owner")
	ErrOwnerOnly         = errors.New("only an owner can change an owner or grant the owner role")
	ErrTransition        = errors.New("that status change isn't allowed: active and suspended swap; active, suspended and invited members can be removed; a removed member comes back only by a new invite")
	ErrInviteRole        = errors.New("an invited or removed member's role changes by inviting them again")
	ErrSiteIDsRequired   = errors.New("site_ids is required when the role becomes supervisor")
	ErrSiteIDsUnexpected = errors.New("site_ids goes only with a change into supervisor")
	ErrBadSiteIDs        = errors.New("site_ids must name the organisation's sites that aren't archived")
	ErrLastOwner         = errors.New("an organisation must keep at least one active owner")
)

// Sentinels are every error text the members endpoints send besides the
// guards' (docs/api/members.md quotes each).
var Sentinels = []error{ErrMemberNotFound, ErrNothingToChange, ErrBadReason, ErrBadRole, ErrBadStatus,
	ErrBadStatusFilter, ErrSelf, ErrOwnerOnly, ErrTransition, ErrInviteRole, ErrSiteIDsRequired,
	ErrSiteIDsUnexpected, ErrBadSiteIDs, ErrLastOwner}

type Member struct {
	UserID    string    `json:"user_id"`
	FullName  string    `json:"full_name"`
	Email     *string   `json:"email"`
	Mobile    *string   `json:"mobile"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	SiteIDs   []string  `json:"site_ids"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OpenAction is a corrective action the member owns that isn't finished.
type OpenAction struct {
	ID      string `json:"id"`
	SiteID  string `json:"site_id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	DueOn   string `json:"due_on"`
	Overdue bool   `json:"overdue"`
	// Whether they can still act on it (the action's owner_eligible).
	OwnerEligible bool `json:"owner_eligible"`
}

// OpenWork is what may need a new owner when someone leaves (D5). Nothing
// is reassigned automatically.
type OpenWork struct {
	Actions []OpenAction `json:"actions"`
	SiteIDs []string     `json:"site_ids"`
}

type MemberDetail struct {
	Member
	OpenWork OpenWork `json:"open_work"`
}

// UpdateRequest changes a member's role, status or both, with a reason.
// SiteIDs (only with a change into supervisor, and then required) are the
// sites they'll see; [] is none.
type UpdateRequest struct {
	Role    *string   `json:"role"`
	Status  *string   `json:"status"`
	Reason  string    `json:"reason"`
	SiteIDs *[]string `json:"site_ids"`
}

// Change is a PATCH's answer: the member as they are now, their open work,
// and the sites the change took away (a supervisor no longer, or removed).
type Change struct {
	MemberDetail
	ClearedSiteIDs []string `json:"cleared_site_ids"`
}

// HistoryEntry is one member_history row: the membership as it became, from
// what, by whom and why. Rows before ONBOARD, and invitations and their
// acceptance, have no reason; a name change has no previous role or status.
type HistoryEntry struct {
	Role          string    `json:"role"`
	Status        string    `json:"status"`
	FullName      string    `json:"full_name"`
	PrevRole      *string   `json:"prev_role"`
	PrevStatus    *string   `json:"prev_status"`
	Reason        *string   `json:"reason"`
	SiteIDs       *[]string `json:"site_ids"`
	PrevSiteIDs   *[]string `json:"prev_site_ids"`
	ChangedAt     time.Time `json:"changed_at"`
	ChangedBy     *string   `json:"changed_by"`
	ChangedByName *string   `json:"changed_by_name"`
}

// SiteHistoryEntry is one site_member_history row: added to or removed from
// a site, by any path (D7b).
type SiteHistoryEntry struct {
	SiteID    string    `json:"site_id"`
	Change    string    `json:"change"`
	ChangedAt time.Time `json:"changed_at"`
	ChangedBy *string   `json:"changed_by"`
}

type History struct {
	History     []HistoryEntry     `json:"history"`
	SiteHistory []SiteHistoryEntry `json:"site_history"`
}

// SiteChanges are site_member_history.change's values.
var SiteChanges = []string{"added", "removed"}
