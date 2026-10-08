package auth

import (
	"errors"
	"time"
)

const (
	RoleOwner = "owner" // org_members.role

	EditionConstruction = "construction"         // organisations.primary_edition
	RegimeConstruction  = "za_construction_2014" // regulatory_regimes.code
)

var (
	// Wrong email OR wrong password. Deliberately the same error for both.
	ErrInvalidCredentials = errors.New("invalid email or password")

	// Signup with an email that already exists (any letter case).
	ErrEmailTaken = errors.New("email already registered")

	// The user belongs to more than one organisation and didn't say which.
	ErrOrgRequired = errors.New("org_id is required for this account")

	// The user asked for an organisation they are not a member of.
	ErrNotMember = errors.New("not a member of this organisation")

	// Refresh token not found or expired.
	ErrInvalidRefreshToken = errors.New("invalid refresh token")

	// The session was revoked, expired, or a used refresh token came back.
	ErrSessionRevoked = errors.New("session revoked")
)

type SignupRequest struct {
	CompanyName string `json:"company_name" binding:"required,max=200"`
	FullName    string `json:"full_name" binding:"required,max=200"`
	Email       string `json:"email" binding:"required,email,max=254"`
	Password   string `json:"password" binding:"required,min=10,max=72"`
	SiteName   string `json:"site_name" binding:"required,max=200"`
	DeviceName string `json:"device_name" binding:"omitempty,max=100"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	OrgID      string `json:"org_id" binding:"omitempty,uuid"`
	DeviceName string `json:"device_name" binding:"omitempty,max=100"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // access token lifetime in seconds
	TokenType    string `json:"token_type"` // always "Bearer"
}

type MeResponse struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	SessionID string `json:"session_id"`
}

type SessionResponse struct {
	ID         string    `json:"id"`
	DeviceName *string   `json:"device_name"`
	UserAgent  *string   `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

type User struct {
	ID           string
	Email        *string
	Mobile       *string
	FullName     string
	PasswordHash *string // nil for invited users who haven't set a password
	ArchivedAt   *time.Time
}

type Membership struct {
	OrgID  string
	UserID string
	Role   string
	Status string
}

type Session struct {
	ID         string
	UserID     string
	OrgID      string
	DeviceName *string
	UserAgent  *string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
}

func (s Session) Active(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

type RefreshToken struct {
	ID        string
	SessionID string
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
}