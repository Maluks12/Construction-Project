package regimes

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Keys are stored in other tables (incidents.taxonomy_code, deadlines.rule_code,
// ...), so they are restricted to a form that is safe in URLs, logs and SQL.
var keyPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

var jurisdictionPattern = regexp.MustCompile(`^[A-Z]{2}$`)

// The same values as the CHECK on regulatory_regimes.edition.
var editions = set("construction", "mining")

// Rev 4, section 2.4. Cadences is exported because the API sends it
// (docs/api/inspections.md checks its union against it).
var (
	Cadences      = []string{"per_shift", "daily", "weekly", "per_activity", "ad_hoc"}
	cadences      = set(Cadences...)
	responseTypes = set("pass_fail_na", "scale", "numeric", "count", "text")
)

var (
	units      = set("calendar_days", "working_days", "hours", unitImmediately)
	directions = set("after", "before")
	kinds      = set(kindReport, "investigation", "notification")

	// Each basis is an event the deadline engine (M8) must supply to start the
	// clock. A misspelt basis would never be supplied, so the clock would
	// silently never start; a fixed list catches that at load time. Adding a
	// basis here means the engine must learn to supply it too.
	bases = set("incident_occurred_at", "person_died_at", "employer_notified_at", "work_starts_at")
)

const (
	unitImmediately = "immediately"
	kindReport      = "report"
)

func set(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}

// validate checks one decoded data set. dir prefixes every message so a
// problem can be found without guessing which regime it came from.
func (ds *DataSet) validate(dir string) error {
	var errs []error
	add := func(file, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s/%s: %s", dir, file, fmt.Sprintf(format, args...)))
	}
	need := func(file, where, field, value string) {
		if value == "" {
			add(file, "%s: %s is required", where, field)
		}
	}
	oneOf := func(file, where, field, value string, allowed map[string]bool) {
		if !allowed[value] {
			add(file, "%s: %s %q is not one of %s", where, field, value,
				strings.Join(slices.Sorted(maps.Keys(allowed)), ", "))
		}
	}
	key := func(file, where, k string, seen map[string]bool) {
		switch {
		case k == "":
			add(file, "%s: key is required", where)
		case !keyPattern.MatchString(k):
			add(file, "%s: key %q must be lower snake case", where, k)
		case seen[k]:
			add(file, "%s: duplicate key %q", where, k)
		}
		seen[k] = true
	}

	r := ds.Regime
	key(fileRegime, "regime", r.Code, map[string]bool{})
	need(fileRegime, "regime", "name", r.Name)
	oneOf(fileRegime, "regime", "edition", r.Edition, editions)
	if !jurisdictionPattern.MatchString(r.Jurisdiction) {
		add(fileRegime, "regime: jurisdiction %q must be two capital letters", r.Jurisdiction)
	}
	need(fileRegime, "regime", "version", r.Version)
	if _, err := time.Parse(time.DateOnly, r.EffectiveFrom); err != nil {
		add(fileRegime, "regime: effective_from %q must be YYYY-MM-DD", r.EffectiveFrom)
	}

	seenChecklists := map[string]bool{}
	for i, c := range ds.Checklists {
		where := fmt.Sprintf("checklists[%d]", i)
		key(fileChecklists, where, c.Key, seenChecklists)
		if c.Version < 1 {
			add(fileChecklists, "%s: version must be 1 or more", where)
		}
		need(fileChecklists, where, "title", c.Title)
		oneOf(fileChecklists, where, "cadence", c.Cadence, cadences)
		need(fileChecklists, where, "source_ref", c.SourceRef)
		if len(c.Items) == 0 {
			add(fileChecklists, "%s: needs at least one item", where)
		}

		// Item keys only need to be unique inside their own checklist.
		seenItems := map[string]bool{}
		for j, it := range c.Items {
			itemWhere := fmt.Sprintf("%s.items[%d]", where, j)
			key(fileChecklists, itemWhere, it.Key, seenItems)
			need(fileChecklists, itemWhere, "prompt", it.Prompt)
			oneOf(fileChecklists, itemWhere, "response_type", it.ResponseType, responseTypes)
		}
	}

	seenDeadlines := map[string]bool{}
	kindOf := map[string]string{}
	for i, d := range ds.Deadlines {
		where := fmt.Sprintf("deadlines[%d]", i)
		key(fileDeadlines, where, d.Key, seenDeadlines)
		kindOf[d.Key] = d.Kind
		need(fileDeadlines, where, "label", d.Label)
		oneOf(fileDeadlines, where, "kind", d.Kind, kinds)
		switch {
		case d.Unit == unitImmediately && d.Period != 0:
			add(fileDeadlines, "%s: period must be 0 when unit is %q", where, unitImmediately)
		case d.Unit != unitImmediately && d.Period < 1:
			add(fileDeadlines, "%s: period must be 1 or more", where)
		}
		oneOf(fileDeadlines, where, "unit", d.Unit, units)
		oneOf(fileDeadlines, where, "direction", d.Direction, directions)
		if d.Unit == unitImmediately && d.Direction != "after" {
			add(fileDeadlines, "%s: direction must be \"after\" when unit is %q", where, unitImmediately)
		}
		oneOf(fileDeadlines, where, "basis", d.Basis, bases)
		// Rev 4, section 2.3: every deadline shows the rule and its source.
		need(fileDeadlines, where, "source_ref", d.SourceRef)
	}

	seenTaxonomy := map[string]bool{}
	for i, t := range ds.Taxonomy {
		where := fmt.Sprintf("taxonomy[%d]", i)
		key(fileTaxonomy, where, t.Key, seenTaxonomy)
		need(fileTaxonomy, where, "label", t.Label)
		hasReport := false
		seenRules := map[string]bool{}
		for _, r := range t.DeadlineRules {
			kind, ok := kindOf[r]
			switch {
			case seenRules[r]:
				add(fileTaxonomy, "%s: deadline_rules lists %q twice", where, r)
			case !ok:
				add(fileTaxonomy, "%s: deadline_rules entry %q is not a deadline rule in %s", where, r, fileDeadlines)
			case kind == kindReport:
				hasReport = true
			}
			seenRules[r] = true
		}
		// Both directions, so "reportable" can never disagree with the clocks
		// the type actually starts.
		switch {
		case t.Reportable && !hasReport:
			add(fileTaxonomy, "%s: reportable types need at least one deadline rule of kind %q", where, kindReport)
		case !t.Reportable && hasReport:
			add(fileTaxonomy, "%s: deadline_rules includes a %q rule but reportable is false", where, kindReport)
		}
	}

	seenTerms := map[string]bool{}
	for i, v := range ds.Vocabulary {
		where := fmt.Sprintf("vocabulary[%d]", i)
		key(fileVocabulary, where, v.Key, seenTerms)
		need(fileVocabulary, where, "label", v.Label)
	}

	return errors.Join(errs...)
}
