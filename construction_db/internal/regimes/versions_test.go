package regimes

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Template versions are append-only. An inspection records (key, version,
// content hash), so a shipped version must never change or disappear, and a
// wording fix is published as a new version. testdata/shipped.json holds
// every shipped version's ContentHash.

// versionProblems returns one message per broken rule, in every loaded regime
// and every locked one. Each says what to do.
func versionProblems(reg *Registry, lock map[string]shippedKeys) []string {
	var out []string
	codes := slices.Sorted(maps.Keys(lock))
	for _, c := range reg.Codes() {
		if !slices.Contains(codes, c) {
			codes = append(codes, c)
		}
	}

	for _, code := range codes {
		data := map[string]map[int]string{} // key → version → hash
		if ds, ok := reg.Get(code); ok {
			for _, t := range ds.Checklists {
				if data[t.Key] == nil {
					data[t.Key] = map[int]string{}
				}
				data[t.Key][t.Version] = t.ContentHash()
			}
		}
		locked := map[string]map[int]string{}
		for key, versions := range lock[code].ChecklistVersions {
			locked[key] = map[int]string{}
			for v, hash := range versions {
				n, err := strconv.Atoi(v)
				if err != nil {
					out = append(out, fmt.Sprintf("%s checklist %q: lock version %q is not a number", code, key, v))
					continue
				}
				locked[key][n] = hash
			}
		}

		// Every locked version is still there, unchanged.
		for key, versions := range locked {
			for v, hash := range versions {
				got, ok := data[key][v]
				switch {
				case !ok:
					out = append(out, fmt.Sprintf("%s checklist %q version %d has shipped and was removed; put it back (versions are append-only)", code, key, v))
				case got != hash:
					out = append(out, fmt.Sprintf("%s checklist %q version %d has shipped and was changed; put it back and publish the change as version %d", code, key, v, slices.Max(slices.Collect(maps.Keys(versions)))+1))
				}
			}
		}

		// Every version in the data is locked, and new ones come after the shipped ones.
		for key, versions := range data {
			shipped := locked[key]
			for v, hash := range versions {
				if _, ok := shipped[v]; ok {
					continue
				}
				if len(shipped) > 0 {
					if top := slices.Max(slices.Collect(maps.Keys(shipped))); v < top {
						out = append(out, fmt.Sprintf("%s checklist %q version %d is below shipped version %d; versions only go up, use %d", code, key, v, top, top+1))
						continue
					}
				}
				out = append(out, fmt.Sprintf("%s checklist %q version %d is not in checklist_versions; add \"%d\": %q under %q when it ships", code, key, v, v, hash, key))
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
	load := func(t *testing.T, e ...string) *Registry {
		t.Helper()
		reg, err := LoadFS(with(validFS(), "test_regime/checklists.json", entries(e...)))
		if err != nil {
			t.Fatalf("LoadFS: %v", err)
		}
		return reg
	}
	hashOf := func(key string, version int, prompt string) string {
		reg := load(t, entry(key, version, prompt))
		ds, _ := reg.Get("test_regime")
		return ds.Checklists[0].ContentHash()
	}
	v1 := hashOf("scaffold_daily", 1, "Base plates sound?")
	v2 := hashOf("scaffold_daily", 2, "Base plates firm and level?")
	lock := func(versions map[string]string) map[string]shippedKeys {
		return map[string]shippedKeys{"test_regime": {ChecklistVersions: map[string]map[string]string{"scaffold_daily": versions}}}
	}

	tests := map[string]struct {
		reg  *Registry
		lock map[string]shippedKeys
		want string // a substring of the single problem; "" means none
	}{
		"v1 locked and unchanged": {
			load(t, entry("scaffold_daily", 1, "Base plates sound?")), lock(map[string]string{"1": v1}), ""},
		"v2 added and locked": {
			load(t, entry("scaffold_daily", 1, "Base plates sound?"), entry("scaffold_daily", 2, "Base plates firm and level?")),
			lock(map[string]string{"1": v1, "2": v2}), ""},
		"v1 edited in place": {
			load(t, entry("scaffold_daily", 1, "Base plates firm and level?")), lock(map[string]string{"1": v1}),
			`"scaffold_daily" version 1 has shipped and was changed; put it back and publish the change as version 2`},
		"v1 removed when v2 shipped": {
			load(t, entry("scaffold_daily", 2, "Base plates firm and level?")), lock(map[string]string{"1": v1, "2": v2}),
			`"scaffold_daily" version 1 has shipped and was removed; put it back`},
		"v2 added, not locked": {
			load(t, entry("scaffold_daily", 1, "Base plates sound?"), entry("scaffold_daily", 2, "Base plates firm and level?")),
			lock(map[string]string{"1": v1}),
			`"scaffold_daily" version 2 is not in checklist_versions; add "2": "` + v2 + `" under "scaffold_daily"`},
		"new key, not locked": {
			load(t, entry("scaffold_daily", 1, "Base plates sound?")), map[string]shippedKeys{},
			`"scaffold_daily" version 1 is not in checklist_versions; add "1": "` + v1 + `"`},
		"new version below a shipped one": {
			load(t, entry("scaffold_daily", 1, "Base plates sound?"), entry("scaffold_daily", 3, "Base plates firm and level?")),
			lock(map[string]string{"3": hashOf("scaffold_daily", 3, "Base plates firm and level?")}),
			`"scaffold_daily" version 1 is below shipped version 3; versions only go up, use 4`},
		"locked regime gone": {
			load(t, entry("scaffold_daily", 1, "Base plates sound?")),
			map[string]shippedKeys{"test_regime": lock(map[string]string{"1": v1})["test_regime"],
				"gone_regime": {ChecklistVersions: map[string]map[string]string{"old": {"1": v1}}}},
			`gone_regime checklist "old" version 1 has shipped and was removed`},
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
}
