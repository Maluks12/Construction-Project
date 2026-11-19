package notifications

import (
	"strings"
	"testing"
	"time"
)

// Emails say who and what in plain words (M11 review).
func TestRender(t *testing.T) {
	loc, _ := time.LoadLocation("Africa/Johannesburg")
	now := time.Date(2026, 12, 5, 9, 0, 0, 0, loc)
	on := func(s string) *string { return &s }
	at := time.Date(2026, 12, 5, 14, 30, 0, 0, loc)
	tests := []struct {
		name, title, kind, threshold string
		dueOn                        *string
		dueAt                        *time.Time
		want                         string
	}{
		{"expiry in 7 days", "Sipho Dlamini: Rope access operator licence", "competency_expiry", "before_7d", on("2026-12-12"), nil,
			"Sipho Dlamini: Rope access operator licence expires on 12 Dec 2026, in 7 days."},
		{"expiry in 1 day", "Sipho Dlamini: medical certificate of fitness", "medical_expiry", "before_7d", on("2026-12-06"), nil,
			"Sipho Dlamini: medical certificate of fitness expires on 6 Dec 2026, in 1 day."},
		{"expiry today", "LGS 2026", "document_expiry", "v2_on_day", on("2026-12-05"), nil, "LGS 2026 expires today, 5 Dec 2026."},
		{"expired", "Thabo: Scaffold erector (Rosebank)", "appointment_expiry", "on_day", on("2026-12-02"), nil,
			"Thabo: Scaffold erector (Rosebank) expired on 2 Dec 2026, 3 days ago."},
		{"deadline in 2 days", "Written notice (Rosebank)", "incident_deadline", "before_2d", on("2026-12-07"), nil,
			"Written notice (Rosebank) is due on 7 Dec 2026, in 2 days."},
		{"deadline overdue", "Written notice (Rosebank)", "incident_deadline", "overdue", on("2026-12-03"), nil,
			"Written notice (Rosebank) is overdue by 2 days (it was due on 3 Dec 2026)."},
		{"forthwith", "Report the death (Rosebank)", "incident_deadline", "forthwith", nil, &at,
			"Report the death (Rosebank) must be done forthwith: now, not later."},
		{"hour-based", "Phone the inspector (Rosebank)", "incident_deadline", "open", nil, &at,
			"Phone the inspector (Rosebank) is due at 14:30 on 5 Dec 2026."},
		{"action overdue", "Fix the guardrail (Rosebank)", "action_overdue", "escalation_2", on("2026-12-03"), nil,
			"Fix the guardrail (Rosebank) is overdue by 2 days (it was due on 3 Dec 2026)."},
		{"stalled", "Verify the close-out: Fix the guardrail (Rosebank)", "verification_stalled", "stalled", nil, nil,
			"Verify the close-out: Fix the guardrail (Rosebank): a close-out has been waiting for verification for 2 days or more."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			subject, body := Render(tc.title, tc.kind, tc.threshold, tc.dueOn, tc.dueAt, now, loc)
			if subject != "SiteProof: "+tc.title {
				t.Errorf("subject %q", subject)
			}
			if first := strings.SplitN(body, "\n", 2)[0]; first != tc.want {
				t.Errorf("body %q\n want %q", first, tc.want)
			}
			for _, code := range []string{tc.kind, tc.threshold, "before_", "escalation_", "_expiry", "fit_with", "unfit"} {
				// Codes are snake_case; "overdue" or "forthwith" are plain words.
				if strings.Contains(code, "_") && strings.Contains(body, code) {
					t.Errorf("body %q contains the code %q", body, code)
				}
			}
		})
	}
}
