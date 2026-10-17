package sites

import (
	"errors"
	"time"
)

var ErrSiteNotFound = errors.New("site not found")
var ErrUserNotFound = errors.New("user not found")

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
