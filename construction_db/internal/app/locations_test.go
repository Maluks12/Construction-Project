package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
)

func TestLocations(t *testing.T) {
	router := NewRouter(testPool, testSecret)

	ownerToken := signup(t, router, "Loc Co", "owner@locations.test")
	otherToken := signup(t, router, "Loc Other Co", "owner@locations-other.test")

	site := listSites(t, router, ownerToken)[0].ID
	site2 := createSite(t, router, ownerToken, "Second Site")

	supID := inviteMember(t, router, ownerToken, "foreman@locations.test", auth.RoleSupervisor)
	supToken := login(t, router, "foreman@locations.test")
	if rec := do(t, router, http.MethodPut, "/v1/sites/"+site+"/members/"+supID, ownerToken, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("assign supervisor: got %d (body: %s)", rec.Code, rec.Body.String())
	}

	locPath := "/v1/sites/" + site + "/locations"

	blockA := createLocation(t, router, ownerToken, site, map[string]any{"kind": "zone", "name": "Block A"})
	level1 := createLocation(t, router, ownerToken, site, map[string]any{"kind": "floor", "name": "Level 1", "parent_id": blockA.ID})
	level2 := createLocation(t, router, ownerToken, site, map[string]any{"kind": "floor", "name": "Level 2", "parent_id": blockA.ID})

	if level1.ParentID == nil || *level1.ParentID != blockA.ID {
		t.Fatalf("level 1: want parent %s, got %v", blockA.ID, level1.ParentID)
	}

	tests := []struct {
		name   string
		token  string
		method string
		path   string
		body   any
		want   int
	}{
		// Unique index, case-insensitive. Also catches the "unique check never runs" bug.
		{"duplicate sibling name is 409", ownerToken, http.MethodPost, locPath, map[string]any{"kind": "zone", "name": "block a"}, http.StatusConflict},
		// Trimmed in the service, not a 500 from the CHECK constraint.
		{"blank name is 400", ownerToken, http.MethodPost, locPath, map[string]any{"kind": "zone", "name": "   "}, http.StatusBadRequest},
		{"unknown kind is 400", ownerToken, http.MethodPost, locPath, map[string]any{"kind": "shaft", "name": "Shaft 1"}, http.StatusBadRequest},
		{"malformed parent id is 400", ownerToken, http.MethodPost, locPath, map[string]any{"kind": "floor", "name": "Level 9", "parent_id": "not-a-uuid"}, http.StatusBadRequest},
		// INSERT ... WHERE EXISTS. Also catches the "parent → site not found" bug.
		{"parent on another site is 400", ownerToken, http.MethodPost, "/v1/sites/" + site2 + "/locations", map[string]any{"kind": "floor", "name": "Level 9", "parent_id": blockA.ID}, http.StatusBadRequest},
		{"other org cannot list", otherToken, http.MethodGet, locPath, nil, http.StatusNotFound},
		{"other org cannot create", otherToken, http.MethodPost, locPath, map[string]any{"kind": "zone", "name": "Intruder"}, http.StatusNotFound},
		// Catches the c.Param("userId") bug in ListLocations.
		{"supervisor lists assigned site", supToken, http.MethodGet, locPath, nil, http.StatusOK},
		{"supervisor cannot list unassigned site", supToken, http.MethodGet, "/v1/sites/" + site2 + "/locations", nil, http.StatusNotFound},
		{"supervisor cannot create", supToken, http.MethodPost, locPath, map[string]any{"kind": "zone", "name": "Block S"}, http.StatusForbidden},
		// Catches the unique-check ordering bug in RenameLocation.
		{"rename to sibling's name is 409", ownerToken, http.MethodPatch, locPath + "/" + level2.ID, map[string]any{"name": "LEVEL 1"}, http.StatusConflict},
		{"rename unknown location is 404", ownerToken, http.MethodPatch, locPath + "/00000000-0000-0000-0000-000000000000", map[string]any{"name": "X"}, http.StatusNotFound},
		// Catches the discarded HasActiveChildren result.
		{"archive with children is 409", ownerToken, http.MethodDelete, locPath + "/" + blockA.ID, nil, http.StatusConflict},
		{"archive malformed id is 404", ownerToken, http.MethodDelete, locPath + "/not-a-uuid", nil, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, router, tt.method, tt.path, tt.token, tt.body)
			if rec.Code != tt.want {
				t.Errorf("%s %s: got %d, want %d (body: %s)", tt.method, tt.path, rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	// Catches the req.Name vs name bug in RenameLocation (and the missing bind).
	t.Run("rename trims the name", func(t *testing.T) {
		rec := do(t, router, http.MethodPatch, locPath+"/"+level1.ID, ownerToken, map[string]any{"name": "  Level 1A  "})
		if rec.Code != http.StatusOK {
			t.Fatalf("rename: got %d (body: %s)", rec.Code, rec.Body.String())
		}
		var loc sites.Location
		if err := json.Unmarshal(rec.Body.Bytes(), &loc); err != nil {
			t.Fatalf("decode location: %v", err)
		}
		if loc.Name != "Level 1A" {
			t.Fatalf("want name %q, got %q", "Level 1A", loc.Name)
		}
	})

	t.Run("archive children, then the parent", func(t *testing.T) {
		for _, id := range []string{level1.ID, level2.ID, blockA.ID} {
			if rec := do(t, router, http.MethodDelete, locPath+"/"+id, ownerToken, nil); rec.Code != http.StatusNoContent {
				t.Fatalf("archive %s: got %d (body: %s)", id, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("cannot create under an archived parent", func(t *testing.T) {
		rec := do(t, router, http.MethodPost, locPath, ownerToken, map[string]any{"kind": "floor", "name": "Level 3", "parent_id": blockA.ID})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400 (body: %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("name is reusable after archive, and list shows only active", func(t *testing.T) {
		fresh := createLocation(t, router, ownerToken, site, map[string]any{"kind": "zone", "name": "Block A"})

		rec := do(t, router, http.MethodGet, locPath, ownerToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list: got %d (body: %s)", rec.Code, rec.Body.String())
		}
		var body struct {
			Locations []sites.Location `json:"locations"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode locations: %v", err)
		}
		if len(body.Locations) != 1 || body.Locations[0].ID != fresh.ID {
			t.Fatalf("want only the new Block A (%s), got %+v", fresh.ID, body.Locations)
		}
	})
}

func createLocation(t *testing.T, router http.Handler, token, siteID string, body map[string]any) sites.Location {
	t.Helper()

	rec := do(t, router, http.MethodPost, "/v1/sites/"+siteID+"/locations", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create location %v: got %d (body: %s)", body["name"], rec.Code, rec.Body.String())
	}
	var loc sites.Location
	if err := json.Unmarshal(rec.Body.Bytes(), &loc); err != nil {
		t.Fatalf("decode location: %v", err)
	}
	return loc
}
