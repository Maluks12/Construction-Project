package deadlines

import (
	"context"
	"errors"
	"testing"

	"construction_db/internal/core/auth"
)

// The retract reason refuses NUL and bad UTF-8 before any database work
// (the HTTP guard stops NUL first; this holds below it).
func TestRetractReasonText(t *testing.T) {
	s := &Service{}
	const id = "5e000000-0000-0000-0000-000000000001"
	for _, reason := range []string{"", "a\x00b", "a\xffb"} {
		if _, _, err := s.Retract(context.Background(), "org", "user", auth.RoleOwner, "site", id, id, RetractRequest{Reason: reason}); !errors.Is(err, ErrBadReason) {
			t.Errorf("reason %q: got %v, want ErrBadReason", reason, err)
		}
	}
}
