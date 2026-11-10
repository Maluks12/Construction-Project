package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

func TestSiteRegime(t *testing.T) {
	ctx := context.Background()
	router := newRouter(t)
	svc := sites.NewService(testPool, testRegistry, testEdition.DefaultRegime())

	ownerToken := signup(t, router, "Regime Co", "owner@regime.test")
	otherToken := signup(t, router, "Other Regime Co", "owner@other-regime.test")
	owner := me(t, router, ownerToken)

	signupSite := listSites(t, router, ownerToken)[0].ID
	createdSite := createSite(t, router, ownerToken, "Second Site")
	otherSite := listSites(t, router, otherToken)[0].ID

	supervisorID := inviteMember(t, router, ownerToken, "foreman@regime.test", auth.RoleSupervisor)
	if rec := do(t, router, http.MethodPut, "/v1/sites/"+signupSite+"/members/"+supervisorID, ownerToken, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("assign supervisor: got %d (body: %s)", rec.Code, rec.Body.String())
	}

	t.Run("new sites get the default regime", func(t *testing.T) {
		for _, siteID := range []string{signupSite, createdSite} {
			ds, err := svc.Regime(ctx, owner.OrgID, owner.UserID, auth.RoleOwner, siteID)
			if err != nil {
				t.Fatalf("Regime(%s): %v", siteID, err)
			}
			if ds.Regime.Code != testEdition.DefaultRegime() {
				t.Errorf("site %s: regime %q, want %q", siteID, ds.Regime.Code, testEdition.DefaultRegime())
			}
		}
	})

	t.Run("in the caller's transaction, with the regime's row id", func(t *testing.T) {
		var wantID string
		if err := testAdmin.QueryRow(ctx, `SELECT id::text FROM regulatory_regimes WHERE code = $1`, testEdition.DefaultRegime()).Scan(&wantID); err != nil {
			t.Fatalf("read regime id: %v", err)
		}
		err := database.WithTenant(ctx, testPool, database.Tenant{OrgID: owner.OrgID, UserID: owner.UserID}, func(tx pgx.Tx) error {
			sr, err := svc.RegimeInTx(ctx, tx, owner.OrgID, owner.UserID, auth.RoleOwner, createdSite)
			if err != nil {
				return err
			}
			if sr.Site.ID != createdSite || sr.RegimeID != wantID || sr.Data.Regime.Code != testEdition.DefaultRegime() {
				t.Errorf("RegimeInTx = site %s, regime id %s, code %s; want %s, %s, %s",
					sr.Site.ID, sr.RegimeID, sr.Data.Regime.Code, createdSite, wantID, testEdition.DefaultRegime())
			}
			_, err = svc.RegimeInTx(ctx, tx, owner.OrgID, supervisorID, auth.RoleSupervisor, createdSite)
			if !errors.Is(err, sites.ErrSiteNotFound) {
				t.Errorf("unassigned supervisor: got %v, want ErrSiteNotFound", err)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("RegimeInTx: %v", err)
		}
	})

	t.Run("assigned supervisor", func(t *testing.T) {
		if _, err := svc.Regime(ctx, owner.OrgID, supervisorID, auth.RoleSupervisor, signupSite); err != nil {
			t.Fatalf("Regime: %v", err)
		}
	})

	notFound := map[string]struct{ userID, role, siteID string }{
		"unassigned supervisor": {supervisorID, auth.RoleSupervisor, createdSite},
		"other org's site":      {owner.UserID, auth.RoleOwner, otherSite},
		"malformed id":          {owner.UserID, auth.RoleOwner, "not-a-uuid"},
	}
	for name, tc := range notFound {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Regime(ctx, owner.OrgID, tc.userID, tc.role, tc.siteID)
			if !errors.Is(err, sites.ErrSiteNotFound) {
				t.Fatalf("got %v, want ErrSiteNotFound", err)
			}
		})
	}

	t.Run("per site, not per org", func(t *testing.T) {
		siteA, siteB := twoRegimeSites(t, owner.OrgID)

		reg, err := regimes.LoadFS(regimeFS("test_regime_a", "test_regime_b"))
		if err != nil {
			t.Fatalf("LoadFS: %v", err)
		}
		twoSvc := sites.NewService(testPool, reg, "test_regime_a")

		for siteID, want := range map[string]string{siteA: "test_regime_a", siteB: "test_regime_b"} {
			ds, err := twoSvc.Regime(ctx, owner.OrgID, owner.UserID, auth.RoleOwner, siteID)
			if err != nil {
				t.Fatalf("Regime(%s): %v", siteID, err)
			}
			if ds.Regime.Code != want {
				t.Errorf("site %s: regime %q, want %q", siteID, ds.Regime.Code, want)
			}
		}

		// The signup site's regime isn't in this registry: an internal error, not a 404.
		_, err = twoSvc.Regime(ctx, owner.OrgID, owner.UserID, auth.RoleOwner, signupSite)
		if err == nil || errors.Is(err, sites.ErrSiteNotFound) {
			t.Fatalf("got %v, want an internal error", err)
		}
	})
}

func TestNewRouterRejectsDefaultWithoutRow(t *testing.T) {
	reg, err := regimes.LoadFS(regimeFS("test_regime_unseeded"))
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	ed := fakeEdition{name: "construction", regime: "test_regime_unseeded"}

	_, err = NewRouter(context.Background(), testPool, testSecret, reg, ed, testStore, testHTTP)
	want := `default regime "test_regime_unseeded" has no regulatory_regimes row`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error %v does not mention %q", err, want)
	}
}

// twoRegimeSites adds two regimes and one site under each, in the same org.
func twoRegimeSites(t *testing.T, orgID string) (siteA, siteB string) {
	t.Helper()
	ctx := context.Background()

	// Regime rows need the admin pool; the app role can only read them.
	tag, err := testAdmin.Exec(ctx, `
		INSERT INTO regulatory_regimes (code, name, edition, version, effective_from)
		VALUES ('test_regime_a', 'Test A', 'construction', '1', '2020-01-01'),
		       ('test_regime_b', 'Test B', 'construction', '1', '2020-01-01')
	`)
	if err != nil || tag.RowsAffected() != 2 {
		t.Fatalf("insert regimes: %v (%d rows)", err, tag.RowsAffected())
	}
	t.Cleanup(func() {
		execAsTenant(t, orgID, 2, `DELETE FROM sites WHERE org_id = $1 AND regime_id IN (SELECT id FROM regulatory_regimes WHERE code LIKE 'test_regime_%')`, orgID)
		if tag, err := testAdmin.Exec(ctx, `DELETE FROM regulatory_regimes WHERE code LIKE 'test_regime_%'`); err != nil || tag.RowsAffected() != 2 {
			t.Errorf("delete regimes: %v (%d rows)", err, tag.RowsAffected())
		}
	})

	insert := `INSERT INTO sites (org_id, regime_id, name) SELECT $1, id, $2 FROM regulatory_regimes WHERE code = $3 RETURNING id`
	err = database.WithTenant(ctx, testPool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, insert, orgID, "Site A", "test_regime_a").Scan(&siteA); err != nil {
			return err
		}
		return tx.QueryRow(ctx, insert, orgID, "Site B", "test_regime_b").Scan(&siteB)
	})
	if err != nil {
		t.Fatalf("insert sites: %v", err)
	}
	return siteA, siteB
}

func regimeFS(codes ...string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for _, code := range codes {
		fsys[code+"/regime.json"] = &fstest.MapFile{Data: []byte(`{"code": "` + code + `", "name": "Test",
			"edition": "construction", "jurisdiction": "ZA", "version": "1", "effective_from": "2020-01-01"}`)}
		for _, f := range []string{"checklists.json", "deadlines.json", "taxonomies.json", "vocabulary.json"} {
			fsys[code+"/"+f] = &fstest.MapFile{Data: []byte("[]")}
		}
	}
	return fsys
}
