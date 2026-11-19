package mail

import "testing"

// The worker records an email 'sent' only when its mailer delivers (M11 review fix 1).
func TestDelivers(t *testing.T) {
	if (Log{}).Delivers() {
		t.Error("Log.Delivers() = true; a logged email would be recorded sent")
	}
	s, err := NewSMTP("smtp://mailpit:1025?from=reminders@siteproof.test")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Delivers() {
		t.Error("SMTP.Delivers() = false; sent emails would be recorded logged")
	}
}
