package app

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"testing/fstest"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/checklists"
	"construction_db/internal/regimes"
)

func catalogPath(site string) string { return "/v1/sites/" + site + "/checklists" }

func decodeCatalog(t *testing.T, router http.Handler, site, token string) checklists.ChecklistCatalog {
	t.Helper()
	rec := do(t, router, http.MethodGet, catalogPath(site), token, nil)
	wantStatus(t, rec, http.StatusOK)
	var c checklists.ChecklistCatalog
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatalf("decode catalogue: %v (body: %s)", err, rec.Body.String())
	}
	return c
}

func catalogKeys(c checklists.ChecklistCatalog) []string {
	keys := []string{}
	for _, tmpl := range c.Checklists {
		keys = append(keys, tmpl.Key)
	}
	return keys
}

func TestChecklistCatalog(t *testing.T) {
	f := newEvidenceFixture(t, "catalog")
	inviteMember(t, f.router, f.owner, "claims@"+f.ownerMe.OrgID+".catalog.test", auth.RoleClaimsHandler)
	claims := login(t, f.router, "claims@"+f.ownerMe.OrgID+".catalog.test")
	inviteMember(t, f.router, f.owner, "she@"+f.ownerMe.OrgID+".catalog.test", auth.RoleSHEOfficer)
	she := login(t, f.router, "she@"+f.ownerMe.OrgID+".catalog.test")

	t.Run("the site regime's templates and the severity scale", func(t *testing.T) {
		c := decodeCatalog(t, f.router, f.siteA, f.owner)
		ds, _ := testRegistry.Get(testEdition.DefaultRegime())
		var want []string
		for _, tmpl := range ds.Checklists {
			want = append(want, tmpl.Key)
		}
		if c.Regime.Code != ds.Regime.Code || c.Regime.Version != ds.Regime.Version {
			t.Errorf("regime %+v, want %s version %s", c.Regime, ds.Regime.Code, ds.Regime.Version)
		}
		if got := catalogKeys(c); !slices.Equal(got, want) || len(got) != 5 {
			t.Errorf("checklists %v, want the regime's 5 in data order %v", got, want)
		}
		if first := c.Checklists[0]; len(first.Items) == 0 || first.Version < 1 || first.Items[0].Prompt == "" {
			t.Errorf("first template %+v, want its version and items with prompts", first)
		}
		if !slices.Equal(c.Severities, checklists.Severities) {
			t.Errorf("severities %+v, want checklists.Severities", c.Severities)
		}
	})

	tests := []struct {
		name, token, site string
		want              int
	}{
		{"owner on another site of the org", f.owner, f.siteB, http.StatusOK},
		{"SHE officer on any site", she, f.siteB, http.StatusOK},
		{"supervisor on own site", f.supervisor, f.siteA, http.StatusOK},
		{"supervisor elsewhere", f.supervisor, f.siteB, http.StatusNotFound},
		{"bookkeeper", f.bookkeeper, f.siteA, http.StatusForbidden},
		{"claims handler", claims, f.siteA, http.StatusForbidden},
		{"other org", f.other, f.siteA, http.StatusNotFound},
		{"malformed site id", f.owner, "not-a-uuid", http.StatusNotFound},
		{"no token", "", f.siteA, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantStatus(t, do(t, f.router, http.MethodGet, catalogPath(tt.site), tt.token, nil), tt.want)
		})
	}
}

// Templates come from each site's own regime, not the organisation's or a
// default: two sites of one org on different regimes get different lists.
func TestChecklistCatalogFollowsTheSiteRegime(t *testing.T) {
	router := newRouter(t)
	token := signup(t, router, "Two Regimes Co", "owner@two-regimes-catalog.test")
	owner := me(t, router, token)
	siteA, siteB := twoRegimeSites(t, owner.OrgID)

	fsys := regimeFS("test_regime_a", "test_regime_b")
	withChecklist := func(code, key string) {
		fsys[code+"/checklists.json"] = &fstest.MapFile{Data: []byte(`[{"key": "` + key + `", "version": 1, "title": "T",
			"cadence": "daily", "source_ref": "test", "items": [{"key": "item", "prompt": "OK?",
			"response_type": "pass_fail_na", "source_ref": "test"}]}]`)}
	}
	withChecklist("test_regime_a", "only_in_a")
	withChecklist("test_regime_b", "only_in_b")
	reg, err := regimes.LoadFS(fsys)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	twoRegimes, err := NewRouter(context.Background(), testPool, testSecret, reg,
		fakeEdition{name: "construction", regime: "test_regime_a"}, testStore, testHTTP)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	for site, want := range map[string]string{siteA: "only_in_a", siteB: "only_in_b"} {
		if got := catalogKeys(decodeCatalog(t, twoRegimes, site, token)); !slices.Equal(got, []string{want}) {
			t.Errorf("site %s: checklists %v, want [%s]", site, got, want)
		}
	}
}
