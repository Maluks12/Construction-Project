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

	// What starts each kind of clock fixes its basis, so a rule can't be
	// attached one way and timed from an event that trigger never supplies.
	appliesWhenBasis = map[string]string{
		AppliesIncidentType:          "incident_occurred_at",
		AppliesEmployeeInjured:       "employer_notified_at",
		AppliesPersonDiedAfterNotice: "person_died_at",
		AppliesSiteWork:              "work_starts_at",
	}
)

// The values of DeadlineRule.AppliesWhen.
const (
	AppliesIncidentType          = "incident_type"
	AppliesEmployeeInjured       = "employee_injured"
	AppliesPersonDiedAfterNotice = "person_died_after_notice"
	AppliesSiteWork              = "site_work"
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
	// LoadLocation accepts "" and "Local", which would mean the server's zone.
	if _, err := time.LoadLocation(r.TimeZone); err != nil || r.TimeZone == "" || r.TimeZone == "Local" {
		add(fileRegime, "regime: time_zone %q must be an IANA time zone such as Africa/Johannesburg", r.TimeZone)
	}

	// A key appears once per version: versions are append-only, so every
	// shipped one stays. A key's versions sit together, oldest first.
	seenVersions := map[string]bool{}
	lastVersion := map[string]int{}
	prevKey := ""
	for i, c := range ds.Checklists {
		where := fmt.Sprintf("checklists[%d]", i)
		key(fileChecklists, where, c.Key, map[string]bool{})
		if c.Version < 1 {
			add(fileChecklists, "%s: version must be 1 or more", where)
		}
		id := fmt.Sprintf("%s@%d", c.Key, c.Version)
		last, seenKey := lastVersion[c.Key]
		switch {
		case seenVersions[id]:
			add(fileChecklists, "%s: duplicate version %d of %q", where, c.Version, c.Key)
		case seenKey && c.Key != prevKey:
			add(fileChecklists, "%s: versions of %q must be listed together", where, c.Key)
		case seenKey && c.Version < last:
			add(fileChecklists, "%s: versions of %q must be listed oldest first (%d after %d)", where, c.Key, c.Version, last)
		}
		seenVersions[id] = true
		lastVersion[c.Key] = max(last, c.Version)
		prevKey = c.Key
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
	appliesOf := map[string]string{}
	for _, d := range ds.Deadlines {
		appliesOf[d.Key] = d.AppliesWhen
	}
	for i, d := range ds.Deadlines {
		where := fmt.Sprintf("deadlines[%d]", i)
		key(fileDeadlines, where, d.Key, seenDeadlines)
		kindOf[d.Key] = d.Kind
		if basis, ok := appliesWhenBasis[d.AppliesWhen]; !ok {
			add(fileDeadlines, "%s: applies_when %q is not one of %s", where, d.AppliesWhen,
				strings.Join(slices.Sorted(maps.Keys(appliesWhenBasis)), ", "))
		} else if d.Basis != basis {
			add(fileDeadlines, "%s: basis must be %q when applies_when is %q", where, basis, d.AppliesWhen)
		}
		// A death after notice needs to know which notices count, and those
		// must be incident reports the engine can see satisfied.
		if (d.AppliesWhen == AppliesPersonDiedAfterNotice) != (len(d.AfterSatisfied) > 0) {
			add(fileDeadlines, "%s: after_satisfied is required exactly when applies_when is %q", where, AppliesPersonDiedAfterNotice)
		}
		for _, r := range d.AfterSatisfied {
			if appliesOf[r] != AppliesIncidentType || !slices.ContainsFunc(ds.Deadlines, func(o DeadlineRule) bool {
				return o.Key == r && o.Kind == kindReport
			}) {
				add(fileDeadlines, "%s: after_satisfied entry %q must be a %q rule with applies_when %q", where, r, kindReport, AppliesIncidentType)
			}
		}
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
			case appliesOf[r] != AppliesIncidentType:
				add(fileTaxonomy, "%s: deadline_rules entry %q has applies_when %q; an incident type may only list %q rules",
					where, r, appliesOf[r], AppliesIncidentType)
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

	seenTopics := map[string]bool{}
	for i, tp := range ds.ToolboxTopics {
		where := fmt.Sprintf("toolbox_topics[%d]", i)
		key(fileToolboxTopics, where, tp.Key, seenTopics)
		need(fileToolboxTopics, where, "title", tp.Title)
		need(fileToolboxTopics, where, "source_ref", tp.SourceRef)
	}

	expiries := set(ExpiryRequired, ExpiryOptional, ExpiryNone)
	seenComp := map[string]bool{}
	for i, c := range ds.Workforce.CompetencyTypes {
		where := fmt.Sprintf("competency_types[%d]", i)
		key(fileWorkforce, where, c.Key, seenComp)
		need(fileWorkforce, where, "label", c.Label)
		oneOf(fileWorkforce, where, "kind", c.Kind, competencyKinds)
		oneOf(fileWorkforce, where, "expiry", c.Expiry, expiries)
		need(fileWorkforce, where, "source_ref", c.SourceRef)
	}
	seenAppt := map[string]bool{}
	for i, a := range ds.Workforce.AppointmentTypes {
		where := fmt.Sprintf("appointment_types[%d]", i)
		key(fileWorkforce, where, a.Key, seenAppt)
		need(fileWorkforce, where, "label", a.Label)
		oneOf(fileWorkforce, where, "expiry", a.Expiry, expiries)
		need(fileWorkforce, where, "source_ref", a.SourceRef)
		if a.Required != (a.RequiredNote != "") {
			add(fileWorkforce, "%s: required_note is needed exactly when required is true", where)
		}
	}
	if m := ds.Workforce.Medical.DefaultValidityMonths; m < 0 || m > 60 {
		add(fileWorkforce, "medical: default_validity_months must be 0-60")
	}
	need(fileWorkforce, "medical", "source_ref", ds.Workforce.Medical.SourceRef)

	seenDocs := map[string]bool{}
	for i, d := range ds.DocumentTypes {
		where := fmt.Sprintf("document_types[%d]", i)
		key(fileDocuments, where, d.Key, seenDocs)
		need(fileDocuments, where, "label", d.Label)
		oneOf(fileDocuments, where, "scope", d.Scope, set(ScopeOrg, ScopeSite))
		oneOf(fileDocuments, where, "expiry", d.Expiry, expiries)
		need(fileDocuments, where, "source_ref", d.SourceRef)
		for j, is := range d.Issuers {
			if is == "" || slices.Index(d.Issuers, is) != j {
				add(fileDocuments, "%s: issuers must be non-empty and distinct", where)
			}
		}
	}

	rem := ds.Reminders
	days := func(field string, v []int) {
		if len(v) == 0 {
			add(fileReminders, "%s is required", field)
		}
		for i, d := range v {
			if d < 0 || d > 365 || (i > 0 && d >= v[i-1]) {
				add(fileReminders, "%s must be 0-365 days, strictly descending", field)
				return
			}
		}
	}
	days("worker_expiry_days", rem.WorkerExpiryDays)
	days("company_document_days", rem.CompanyDocumentDays)
	days("deadline_days_before", rem.DeadlineDaysBefore)
	if e := rem.ActionEscalationDays; len(e) != 3 || e[0] < 0 || e[1] <= e[0] || e[2] <= e[1] || e[2] > 365 {
		add(fileReminders, "action_escalation_days must be three ascending days (owner and raiser, SHE officers, owners)")
	}
	if rem.VerificationStalledDays < 1 || rem.VerificationStalledDays > 60 {
		add(fileReminders, "verification_stalled_days must be 1-60")
	}
	if rem.RosterLapseDays < 1 || rem.RosterLapseDays > 365 {
		add(fileReminders, "roster_lapse_days must be 1-365")
	}
	need(fileReminders, "reminders", "source_ref", rem.SourceRef)

	return errors.Join(errs...)
}

var competencyKinds = set("certificate", "licence", "training", "registration")
