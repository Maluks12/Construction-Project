package sites

import (
	"errors"
	"time"
)

var ErrSiteNotFound = errors.New("site not found")

type Site struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Address   *string   `json:"address"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
