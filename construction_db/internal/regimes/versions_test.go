package regimes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// An inspection records (template key, version). If wording could change
// without a version bump, the version would stop identifying the prompts the
// inspector saw. So each template's content is locked in
// testdata/shipped.json under its version.
type lockedVersion struct {
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
}

// templateContent is exactly what the lock covers: what an inspector reads.
// It's a separate struct so adding a field to ChecklistTemplate doesn't
// change every hash; a new field that inspectors see belongs here too, in the
// same change that bumps the versions it affects.
type templateContent struct {
	Key       string        `json:"key"`
	Title     string        `json:"title"`
	Cadence   string        `json:"cadence"`
	SourceRef string        `json:"source_ref"`
	Items     []itemContent `json:"items"`
}

type itemContent struct {
	Key          string `json:"key"`
	Prompt       string `json:"prompt"`
	ResponseType string `json:"response_type"`
	SourceRef    string `json:"source_ref"`
}

func contentHash(t ChecklistTemplate) string {
	c := templateContent{Key: t.Key, Title: t.Title, Cadence: t.Cadence, SourceRef: t.SourceRef, Items: []itemContent{}}
	for _, it := range t.Items {
		c.Items = append(c.Items, itemContent{Key: it.Key, Prompt: it.Prompt, ResponseType: it.ResponseType, SourceRef: it.SourceRef})
	}
	b, err := json.Marshal(c)
	if err != nil {
		panic(err) // plain strings and slices always marshal
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// versionProblems returns one message per template, in every loaded regime,
// whose version and content don't match the lock exactly. Each says what to do.
func versionProblems(reg *Registry, lock map[string]shippedKeys) []string {
	var out []string
	for _, code := range reg.Codes() {
		ds, _ := reg.Get(code)
		for _, t := range ds.Checklists {
			hash := contentHash(t)
			want := fmt.Sprintf(`{"version": %d, "sha256": "%s"}`, t.Version, hash)
			l, ok := lock[code].ChecklistVersions[t.Key]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("%s checklist %q is not in checklist_versions; add %q: %s when it ships", code, t.Key, t.Key, want))
			case t.Version < l.Version:
				out = append(out, fmt.Sprintf("%s checklist %q is version %d but %d has shipped; versions only go up", code, t.Key, t.Version, l.Version))
			case t.Version == l.Version && hash != l.SHA256:
				out = append(out, fmt.Sprintf("%s checklist %q changed without a version bump: bump its version past %d, then update the lock", code, t.Key, l.Version))
			case t.Version != l.Version || hash != l.SHA256:
				out = append(out, fmt.Sprintf("%s checklist %q is now version %d; update the lock to %s", code, t.Key, t.Version, want))
			}
		}
	}
	slices.Sort(out)
	return out
}

func TestChecklistVersionsLocked(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var lock map[string]shippedKeys
	if err := decodeFile(os.DirFS("testdata"), "shipped.json", &lock); err != nil {
		t.Fatalf("read lock: %v", err)
	}
	for _, p := range versionProblems(reg, lock) {
		t.Error(p)
	}
}

func TestVersionProblems(t *testing.T) {
	reg, err := LoadFS(validFS())
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	ds, _ := reg.Get("test_regime")
	current := ds.Checklists[0]
	hash := contentHash(current)

	// Edit a copy of the template, as a change to the data file would.
	edited := func(change func(*ChecklistTemplate)) *Registry {
		c := current
		c.Items = slices.Clone(current.Items)
		change(&c)
		copyDS := *ds
		copyDS.Checklists = []ChecklistTemplate{c}
		return &Registry{byCode: map[string]*DataSet{"test_regime": &copyDS}}
	}
	locked := func(version int, sha string) map[string]shippedKeys {
		return map[string]shippedKeys{"test_regime": {ChecklistVersions: map[string]lockedVersion{"scaffold_daily": {version, sha}}}}
	}

	tests := map[string]struct {
		reg  *Registry
		lock map[string]shippedKeys
		want string // a substring of the single problem; "" means none
	}{
		"locked and unchanged":          {reg, locked(1, hash), ""},
		"not in the lock":               {reg, map[string]shippedKeys{}, `"scaffold_daily" is not in checklist_versions; add "scaffold_daily": {"version": 1, "sha256": "` + hash + `"}`},
		"prompt reworded, same version": {edited(func(c *ChecklistTemplate) { c.Items[0].Prompt = "Base plates firm?" }), locked(1, hash), "changed without a version bump: bump its version past 1"},
		"title changed, same version":   {edited(func(c *ChecklistTemplate) { c.Title = "Scaffold check" }), locked(1, hash), "changed without a version bump"},
		"item added, same version": {edited(func(c *ChecklistTemplate) {
			c.Items = append(c.Items, ChecklistItem{Key: "ties", Prompt: "Ties fixed?", ResponseType: "pass_fail_na"})
		}), locked(1, hash), "changed without a version bump"},
		"bumped, lock not updated": {edited(func(c *ChecklistTemplate) { c.Version = 2; c.Items[0].Prompt = "Base plates firm?" }), locked(1, hash), `is now version 2; update the lock to {"version": 2, "sha256": "`},
		"version went down":        {edited(func(c *ChecklistTemplate) { c.Version = 1 }), locked(2, hash), "is version 1 but 2 has shipped; versions only go up"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := versionProblems(tc.reg, tc.lock)
			switch {
			case tc.want == "" && len(got) != 0:
				t.Errorf("problems %q, want none", got)
			case tc.want != "" && (len(got) != 1 || !strings.Contains(got[0], tc.want)):
				t.Errorf("problems %q, want exactly one containing %q", got, tc.want)
			}
		})
	}

	// The hash covers what inspectors read, and only that.
	if contentHash(current) != hash {
		t.Error("contentHash is not deterministic")
	}
	bumpedOnly := current
	bumpedOnly.Version = 9
	if contentHash(bumpedOnly) != hash {
		t.Error("the version changed the content hash; it should cover content only")
	}
}
