package reg4

import (
	"errors"
	"testing"
	"time"
)

// Below the HTTP guard (httpapi.RejectNUL), the fields still refuse what
// Postgres text can't hold.
func TestNormaliseRefusesNUL(t *testing.T) {
	now := time.Now()
	str := func(s string) *string { return &s }
	base := func() VersionRequest {
		return VersionRequest{Description: "Dig", StartsOn: "2026-12-01", ClientSubmittedAt: now, AmendReason: str("Moved")}
	}
	for _, tc := range []struct {
		name string
		edit func(*VersionRequest)
		want error
	}{
		{"description", func(r *VersionRequest) { r.Description = "D\x00ig" }, ErrBadDescription},
		{"amend_reason", func(r *VersionRequest) { r.AmendReason = str("Mo\x00ved") }, ErrReasonRequired},
		{"amend_reason not UTF-8", func(r *VersionRequest) { r.AmendReason = str("Mo\xffved") }, ErrReasonRequired},
		{"device_id", func(r *VersionRequest) { r.DeviceID = str("phone\x00") }, ErrBadDeviceID},
		{"device_id not UTF-8", func(r *VersionRequest) { r.DeviceID = str("phone\xfe") }, ErrBadDeviceID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			tc.edit(&r)
			if err := normalise(&r, 2, now); !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
	ok := base()
	if err := normalise(&ok, 2, now); err != nil {
		t.Errorf("clean request: %v", err)
	}
}
