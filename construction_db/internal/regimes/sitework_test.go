package regimes

import (
	"slices"
	"testing"
)

func TestSiteWorkRulesFor(t *testing.T) {
	reg, err := LoadFS(validFS())
	if err != nil {
		t.Fatal(err)
	}
	ds, _ := reg.Get("test_regime")
	tests := map[string]struct {
		facts []string
		want  []string
	}{
		"nothing":                   {nil, []string{}},
		"one-fact group":            {[]string{"dig"}, []string{"test_notify"}},
		"half an all-of group":      {[]string{"long", "dig"}, []string{"test_notify"}},
		"whole all-of group":        {[]string{"long", "big"}, []string{"test_permit"}},
		"permit excludes notice":    {[]string{"long", "big", "dig"}, []string{"test_permit"}},
		"second group":              {[]string{"grade", "dig"}, []string{"test_permit"}},
		"house excludes the permit": {[]string{"house", "grade"}, []string{"test_notify"}},
		"unknown fact ignored":      {[]string{"shed"}, []string{}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ds.SiteWork.RulesFor(tc.facts); !slices.Equal(got, tc.want) {
				t.Errorf("RulesFor(%v) = %v, want %v", tc.facts, got, tc.want)
			}
		})
	}
}

// The readings in SOURCES (REG4 D2, D6), on the shipped data.
func TestShippedSiteWorkReadings(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	ds, _ := reg.Get("za_construction_2014")
	const (
		reg3 = "cr2014_reg3_permit_application"
		reg4 = "cr2014_reg4_notification"
	)
	tests := map[string]struct {
		facts []string
		want  []string
	}{
		"excavation":                       {[]string{"excavation"}, []string{reg4}},
		"work at height":                   {[]string{"work_at_height_fall_risk"}, []string{reg4}},
		"demolition":                       {[]string{"demolition"}, []string{reg4}},
		"explosives":                       {[]string{"explosives"}, []string{reg4}},
		"reg 4(2) dwelling":                {[]string{"single_storey_dwelling_for_resident_client"}, []string{reg4}},
		"no trigger":                       {[]string{"exceeds_365_days"}, []string{}},
		"GN 850 both limbs of (1)":         {[]string{"exceeds_365_days", "exceeds_3600_person_days"}, []string{reg3}},
		"GN 850 grade":                     {[]string{"cidb_grade_7_8_or_9"}, []string{reg3}},
		"reg 4(1): permit work, no notice": {[]string{"cidb_grade_7_8_or_9", "excavation"}, []string{reg3}},
		// GN 850's "and": 365 days alone is below the threshold, so reg 4 applies.
		"below GN 850, so reg 4":        {[]string{"exceeds_365_days", "excavation"}, []string{reg4}},
		"3600 person-days alone, reg 4": {[]string{"exceeds_3600_person_days", "demolition"}, []string{reg4}},
		"reg 2(2): dwelling, no permit": {[]string{"single_storey_dwelling_for_resident_client", "cidb_grade_7_8_or_9"}, []string{reg4}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ds.SiteWork.RulesFor(tc.facts); !slices.Equal(got, tc.want) {
				t.Errorf("RulesFor(%v) = %v, want %v", tc.facts, got, tc.want)
			}
		})
	}

	for _, d := range ds.Deadlines {
		if d.AppliesWhen != AppliesSiteWork {
			continue
		}
		if d.Unit != UnitClearDays || d.Direction != "before" || d.Caveat == "" {
			t.Errorf("%s: unit %q direction %q caveat %q; want clear days before, with the reading's caveat", d.Key, d.Unit, d.Direction, d.Caveat)
		}
	}
}
