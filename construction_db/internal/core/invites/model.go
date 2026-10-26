package invites

import (
	"errors"
	"time"
)

var (
	ErrBlankName         = errors.New("name is required")
	ErrContactRequired   = errors.New("email or mobile is required")
	ErrAlreadyMember     = errors.New("this person is already a member")
	ErrCannotInviteOwner = errors.New("only an owner can invite an owner")
	ErrInviteNotFound    = errors.New("invite not found")
	ErrContactConflict   = errors.New("that email and mobile belong to two different accounts")
)

type InviteRequest struct {
	Email    *string `json:"email" binding:"omitempty,email,max=254"`
	Mobile   *string `json:"mobile" binding:"omitempty,e164"`
	FullName string  `json:"full_name" binding:"required,max=200"`
	Role     string  `json:"role" binding:"required,oneof=owner she_officer supervisor claims_handler contracts_manager bookkeeper"`
}

type InviteResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PendingInvite struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	FullName  string    `json:"full_name"`
	Email     *string   `json:"email"`
	Mobile    *string   `json:"mobile"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
