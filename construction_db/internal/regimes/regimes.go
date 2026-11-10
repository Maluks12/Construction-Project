// Package regimes loads the regulatory data sets (checklists, deadline rules,
// incident taxonomy, vocabulary) that the core reads instead of branching on
// edition. Each data set is a directory of JSON files named after its
// regulatory_regimes.code. See Rev 4, section 1.4.
//
// Rule: a shipped key never changes meaning. Fix wording in place, but a
// change in meaning gets a new key. testdata/shipped.json lists the keys that
// have shipped, and a test fails if any of them disappears.
//
// Checklist templates also lock their content: any change to what an
// inspector reads (title, prompts, items) needs a higher version, because an
// inspection records (key, version) and the version must identify the
// wording. shipped.json holds each template's version and content hash, and
// a test fails until both are updated.
package regimes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
)

const (
	fileRegime     = "regime.json"
	fileChecklists = "checklists.json"
	fileDeadlines  = "deadlines.json"
	fileTaxonomy   = "taxonomies.json"
	fileVocabulary = "vocabulary.json"
)

// Regime mirrors a regulatory_regimes row. Version is the version of the law
// (e.g. "2014"), not of this data set; CheckDatabase compares it.
type Regime struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Edition       string `json:"edition"`
	Jurisdiction  string `json:"jurisdiction"`
	Version       string `json:"version"`
	EffectiveFrom string `json:"effective_from"` // YYYY-MM-DD
}

// ChecklistTemplate has its own Version so a completed checklist can record
// exactly which template it was filled in against (Rev 4, section 2.4).
type ChecklistTemplate struct {
	Key       string          `json:"key"`
	Version   int             `json:"version"`
	Title     string          `json:"title"`
	Cadence   string          `json:"cadence"`
	SourceRef string          `json:"source_ref"`
	Items     []ChecklistItem `json:"items"`
}

type ChecklistItem struct {
	Key          string `json:"key"`
	Prompt       string `json:"prompt"`
	ResponseType string `json:"response_type"`
	SourceRef    string `json:"source_ref"`
}

// DeadlineRule is "Period Units Direction Basis", e.g. 7 calendar_days after
// incident_occurred_at, or 7 calendar_days before work_starts_at.
//
// Unit "immediately" (period 0) is for "forthwith" duties. It is not a
// countdown, so the engine shows it as something to do now rather than a due
// date (Rev 4, section 4).
//
// Kind says what the clock is for: "report" (something sent to an authority
// after an event), "investigation" (an internal duty) or "notification"
// (something due before work starts).
//
// Holidays are not here: the calendar is a database table (Rev 4, section 2.3).
type DeadlineRule struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	Period    int    `json:"period"`
	Unit      string `json:"unit"`
	Direction string `json:"direction"`
	Basis     string `json:"basis"`
	SourceRef string `json:"source_ref"`
}

// IncidentType is one entry in the incident taxonomy. DeadlineRules are the
// keys of every DeadlineRule an incident of this type starts. A death starts
// three: written notice within 7 days, a telephone report forthwith (GAR 2003
// reg 8(1)) and an investigation within 7 days (reg 9(2)). A non-reportable
// type can still start clocks: medical treatment beyond first aid must be
// investigated even though it is not reported.
//
// Reportable is true exactly when at least one of the rules is a "report";
// validate enforces it in both directions.
type IncidentType struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	Reportable    bool     `json:"reportable"`
	DeadlineRules []string `json:"deadline_rules"`
}

type Term struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type DataSet struct {
	Regime     Regime              `json:"regime"`
	Checklists []ChecklistTemplate `json:"checklists"`
	Deadlines  []DeadlineRule      `json:"deadlines"`
	Taxonomy   []IncidentType      `json:"taxonomy"`
	Vocabulary []Term              `json:"vocabulary"`
}

// Registry holds every loaded data set by regime code. It is built once at
// startup and only read after that; callers must not modify what Get returns.
type Registry struct {
	byCode map[string]*DataSet
}

func (r *Registry) Get(code string) (*DataSet, bool) {
	ds, ok := r.byCode[code]
	return ds, ok
}

// CheckDefault fails unless regime is loaded and belongs to edition.
func (r *Registry) CheckDefault(regime, edition string) error {
	ds, ok := r.byCode[regime]
	if !ok {
		return fmt.Errorf("default regime %q has no data set", regime)
	}
	if ds.Regime.Edition != edition {
		return fmt.Errorf("default regime %q is edition %q, not default edition %q", regime, ds.Regime.Edition, edition)
	}
	return nil
}

// Codes returns the regime codes in sorted order.
func (r *Registry) Codes() []string {
	return slices.Sorted(maps.Keys(r.byCode))
}

// LoadFS loads every data set in fsys: one directory per regime, each with all
// five files. It reports every problem it finds, not just the first, so one
// failed startup (or test run) shows everything that needs fixing.
func LoadFS(fsys fs.FS) (*Registry, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("load regimes: %w", err)
	}

	reg := &Registry{byCode: map[string]*DataSet{}}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			errs = append(errs, fmt.Errorf("%s: unexpected file; each regime is a directory", e.Name()))
			continue
		}
		ds, err := loadDataSet(fsys, e.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		reg.byCode[ds.Regime.Code] = ds
	}
	if len(entries) == 0 {
		errs = append(errs, errors.New("no regime data sets found"))
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("load regimes: %w", err)
	}
	return reg, nil
}

func loadDataSet(fsys fs.FS, dir string) (*DataSet, error) {
	var ds DataSet
	files := []struct {
		name string
		dst  any
	}{
		{fileRegime, &ds.Regime},
		{fileChecklists, &ds.Checklists},
		{fileDeadlines, &ds.Deadlines},
		{fileTaxonomy, &ds.Taxonomy},
		{fileVocabulary, &ds.Vocabulary},
	}

	var errs []error
	for _, f := range files {
		if err := decodeFile(fsys, path.Join(dir, f.name), f.dst); err != nil {
			errs = append(errs, err)
		}
	}
	// Validating half-decoded data would only add noise, so stop here.
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	// A file containing null decodes to a nil slice; keep lists as [] like
	// everywhere else.
	if ds.Checklists == nil {
		ds.Checklists = []ChecklistTemplate{}
	}
	if ds.Deadlines == nil {
		ds.Deadlines = []DeadlineRule{}
	}
	if ds.Taxonomy == nil {
		ds.Taxonomy = []IncidentType{}
	}
	for i := range ds.Taxonomy {
		if ds.Taxonomy[i].DeadlineRules == nil {
			ds.Taxonomy[i].DeadlineRules = []string{}
		}
	}
	if ds.Vocabulary == nil {
		ds.Vocabulary = []Term{}
	}

	if ds.Regime.Code != dir {
		errs = append(errs, fmt.Errorf("%s/%s: code %q does not match directory name", dir, fileRegime, ds.Regime.Code))
	}
	if err := ds.validate(dir); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &ds, nil
}

// decodeFile reads one JSON value strictly.
//
// DisallowUnknownFields turns a misspelt field into an error. Without it,
// "reportabel": true would be dropped silently and the incident type would
// load as not reportable.
//
// The second Decode must hit EOF because Decode stops after the first JSON
// value. Without that check, a stray "]" or a second array pasted at the end
// of the file would load without complaint.
func decodeFile(fsys fs.FS, name string, dst any) error {
	f, err := fsys.Open(name)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: unexpected data after the JSON value", name)
	}
	return nil
}
