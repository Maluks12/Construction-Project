package sites

import (
	"errors"
	"time"
)

var (
	ErrBlankName           = errors.New("name is required")
	ErrSiteNotFound        = errors.New("site not found")
	ErrUserNotFound        = errors.New("user not found")
	ErrLocationNotFound    = errors.New("location not found")
	ErrParentNotFound      = errors.New("parent location not found")
	ErrLocationNameTaken   = errors.New("a location with that name already exists here")
	ErrLocationHasChildren = errors.New("location has active child locations")
)

// StatusArchived is Site.Status after DELETE /v1/sites/:id. Reads of an
// archived site still work; features decide what may still be written.
const StatusArchived = "archived"

type SiteMember struct {
	UserID   string  `json:"user_id"`
	FullName string  `json:"full_name"`
	Email    *string `json:"email"`
	Mobile   *string `json:"mobile"`
	Role     string  `json:"role"`
}

type Site struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Address   *string   `json:"address"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateSiteRequest struct {
	Name    string  `json:"name" binding:"required,max=200"`
	Address *string `json:"address" binding:"omitempty,max=500"`
}

type UpdateSiteRequest struct {
	Name    *string `json:"name" binding:"omitempty,min=1,max=200"`
	Address *string `json:"address" binding:"omitempty,max=500"`
}

type Location struct {
	ID        string    `json:"id"`
	SiteID    string    `json:"site_id"`
	ParentID  *string   `json:"parent_id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateLocationRequest struct {
	Kind     string  `json:"kind" binding:"required,oneof=zone floor"`
	Name     string  `json:"name" binding:"required,max=100"`
	ParentID *string `json:"parent_id" binding:"omitempty,uuid"`
}

type UpdateLocationRequest struct {
	Name string `json:"name" binding:"required,max=100"`
}
