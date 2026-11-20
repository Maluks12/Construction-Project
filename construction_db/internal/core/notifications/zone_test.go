package notifications

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"construction_db/internal/regimes"
)

// twoZones: "aa_first" sorts first and is UTC-11; "zz_site" is SAST.
func twoZones(t *testing.T) *regimes.Registry {
	t.Helper()
	fsys := fstest.MapFS{}
	for code, zone := range map[string]string{"aa_first": "Pacific/Pago_Pago", "zz_site": "Africa/Johannesburg"} {
		fsys[code+"/regime.json"] = &fstest.MapFile{Data: []byte(`{"code": "` + code + `", "name": "Test",
			"edition": "construction", "jurisdiction": "ZA", "version": "1", "effective_from": "2020-01-01",
			"time_zone": "` + zone + `"}`)}
		for _, f := range []string{"checklists.json", "deadlines.json", "taxonomies.json", "vocabulary.json", "toolbox_topics.json", "documents.json"} {
			fsys[code+"/"+f] = &fstest.MapFile{Data: []byte("[]")}
		}
		fsys[code+"/workforce.json"] = &fstest.MapFile{Data: []byte(`{"competency_types": [], "appointment_types": [],
			"medical": {"default_validity_months": 0, "source_ref": "t"}}`)}
		fsys[code+"/reminders.json"] = &fstest.MapFile{Data: []byte(`{"worker_expiry_days": [0], "company_document_days": [0],
			"deadline_days_before": [0], "action_escalation_days": [0, 1, 3], "verification_stalled_days": 2, "roster_lapse_days": 14, "source_ref": "t"}`)}
	}
	reg, err := regimes.LoadFS(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// An email's dates are told in its own regime's zone, not the first loaded
// regime's (the bug before 000036).
func TestEmailZoneIsTheSubjectsRegime(t *testing.T) {
	j := &Jobs{Registry: twoZones(t)}
	site := "zz_site"
	if got := j.zone(&site).String(); got != "Africa/Johannesburg" {
		t.Errorf("zone for zz_site = %s", got)
	}
	unknown := "not_loaded"
	if got := j.zone(&unknown).String(); got != "Pacific/Pago_Pago" {
		t.Errorf("fallback zone = %s, want the first loaded regime's", got)
	}

	// 08:00 SAST on 20 Nov is still 19 Nov in Pago Pago: told in SAST, a
	// licence expiring 30 Nov is 10 days away; told in the other zone, 11.
	now := time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)
	due := "2026-11-30"
	_, body := Render("Thabo: Rope access", "competency_expiry", "before_14d", &due, nil, now, j.zone(&site))
	if !strings.HasPrefix(body, "Thabo: Rope access expires on 30 Nov 2026, in 10 days.") {
		t.Errorf("body %q", body)
	}
}

// Days are counted by date: across a daylight-saving change the span is 47
// hours, still two days.
func TestCalendarDaysAcrossDST(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skip("no tzdata for Europe/London")
	}
	now := time.Date(2026, 3, 28, 9, 0, 0, 0, london) // clocks go forward on Sun 29 Mar
	due := "2026-03-30"
	_, body := Render("Licence", "competency_expiry", "before_7d", &due, nil, now, london)
	if !strings.HasPrefix(body, "Licence expires on 30 Mar 2026, in 2 days.") {
		t.Errorf("body %q", body)
	}
}
