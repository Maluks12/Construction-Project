package app

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/documents"
	"construction_db/internal/core/files"
)

func vaultPath(id string) string { return "/v1/vault/documents/" + id }
func safetyFilePath(site, id string) string {
	return "/v1/construction/sites/" + site + "/safety-file/" + id
}

func versionReq(file string, fields map[string]any) map[string]any {
	body := map[string]any{"file_id": file, "client_uploaded_at": rfc(time.Now())}
	for k, v := range fields {
		body = with(body, k, v)
	}
	return body
}

func decodeDocument(t *testing.T, body []byte) documents.Document {
	t.Helper()
	var d documents.Document
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatalf("decode document: %v (%s)", err, body)
	}
	return d
}

func decodeRegister(t *testing.T, router http.Handler, token, path string) documents.Register {
	t.Helper()
	rec := do(t, router, http.MethodGet, path, token, nil)
	wantStatus(t, rec, http.StatusOK)
	var r documents.Register
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func readinessOf(r documents.Register, key string) documents.Readiness {
	for _, x := range r.Readiness {
		if x.TypeKey == key {
			return x
		}
	}
	return documents.Readiness{}
}

func TestVault(t *testing.T) {
	f := newEvidenceFixture(t, "vault")
	expires := time.Now().AddDate(0, 0, 20).Format(time.DateOnly)
	later := time.Now().AddDate(1, 0, 0).Format(time.DateOnly)
	lgs := func(file, issuer, exp string) map[string]any {
		return map[string]any{"regime_code": "za_construction_2014", "type_key": "coida_letter_of_good_standing", "title": "LGS 2026",
			"version": versionReq(file, map[string]any{"issuer": issuer, "expires_on": exp, "reference": "CF 990001234"})}
	}

	if r := decodeRegister(t, f.router, f.owner, "/v1/vault/documents"); readinessOf(r, "coida_letter_of_good_standing").Status != documents.StatusMissing {
		t.Errorf("empty vault readiness %+v", r.Readiness)
	}
	f1 := uploadFile(t, f.router, f.owner, files.PurposeOrgDocument, nil)
	id := uuid.NewString()
	wantError(t, do(t, f.router, http.MethodPut, vaultPath(id), f.owner, lgs(f1, "Someone", expires)), http.StatusBadRequest,
		"issuer must be 1-200 characters, and one of the type's issuers when it lists them")
	wantError(t, do(t, f.router, http.MethodPut, vaultPath(id), f.owner, lgs(f1, "FEM", "")), http.StatusBadRequest,
		"issued_on and expires_on must be YYYY-MM-DD, expiry not before issue")
	first := lgs(f1, "FEM", expires)
	rec := do(t, f.router, http.MethodPut, vaultPath(id), f.owner, first)
	wantStatus(t, rec, http.StatusCreated)
	if d := decodeDocument(t, rec.Body.Bytes()); d.LatestVersion != 1 || d.Current.State != documents.StateExpiring || len(d.Versions) != 1 {
		t.Errorf("created %+v", d)
	}
	wantStatus(t, do(t, f.router, http.MethodPut, vaultPath(id), f.owner, first), http.StatusOK)

	// Version 2: the renewal. Version 1 stays, superseded.
	f2 := uploadFile(t, f.router, f.owner, files.PurposeOrgDocument, nil)
	v2 := versionReq(f2, map[string]any{"issuer": "FEM", "expires_on": later})
	wantError(t, do(t, f.router, http.MethodPut, vaultPath(id)+"/versions/3", f.owner, v2), http.StatusConflict,
		"version is not the next version of this document")
	rec = do(t, f.router, http.MethodPut, vaultPath(id)+"/versions/2", f.owner, v2)
	wantStatus(t, rec, http.StatusCreated)
	d := decodeDocument(t, rec.Body.Bytes())
	if d.LatestVersion != 2 || d.Current.State != documents.StateValid || d.Versions[0].State != documents.StateSuperseded {
		t.Errorf("renewed %+v", d)
	}
	wantStatus(t, do(t, f.router, http.MethodPut, vaultPath(id)+"/versions/2", f.owner, v2), http.StatusOK)
	f3 := uploadFile(t, f.router, f.owner, files.PurposeOrgDocument, nil)
	wantError(t, do(t, f.router, http.MethodPut, vaultPath(id)+"/versions/2", f.owner, versionReq(f3, map[string]any{"issuer": "FEM", "expires_on": later})),
		http.StatusConflict, "that version already exists with different details")

	if r := decodeRegister(t, f.router, f.owner, "/v1/vault/documents"); readinessOf(r, "coida_letter_of_good_standing").Status != documents.StatusPresent {
		t.Errorf("readiness %+v", r.Readiness)
	}
	// Both versions open, logged.
	wantStatus(t, do(t, f.router, http.MethodPost, vaultPath(id)+"/versions/1/view", f.owner, nil), http.StatusOK)
	if n := countRows(t, `SELECT count(*) FROM file_access_log WHERE file_id = $1`, f1); n != 1 {
		t.Errorf("view of version 1 logged %d rows", n)
	}

	// The vault is the leads' and the organisation's.
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/vault/documents", f.supervisor, nil), http.StatusForbidden)
	wantError(t, do(t, f.router, http.MethodGet, vaultPath(id), f.other, nil), http.StatusNotFound, "document not found")
	if r := decodeRegister(t, f.router, f.other, "/v1/vault/documents"); len(r.Documents) != 0 {
		t.Errorf("another org sees %d documents", len(r.Documents))
	}
	// A site document's file can't go in the vault.
	siteFile := uploadFile(t, f.router, f.owner, files.PurposeSiteDocument, &f.siteA)
	wantError(t, do(t, f.router, http.MethodPut, vaultPath(uuid.NewString()), f.owner, lgs(siteFile, "FEM", expires)),
		http.StatusBadRequest, "file was uploaded for another purpose")
	wantError(t, do(t, f.router, http.MethodPut, vaultPath(uuid.NewString()), f.owner,
		map[string]any{"regime_code": "za_construction_2014", "type_key": "health_and_safety_plan", "title": "x", "version": versionReq(f3, nil)}),
		http.StatusBadRequest, "type_key is not a document type of this regime and scope")
}

func TestSafetyFile(t *testing.T) {
	f := newEvidenceFixture(t, "safetyfile")
	register := "/v1/construction/sites/" + f.siteA + "/safety-file"
	r := decodeRegister(t, f.router, f.supervisor, register)
	if p := readinessOf(r, "health_and_safety_plan"); p.Status != documents.StatusMissing || !p.Required {
		t.Errorf("plan readiness %+v", p)
	}
	file := uploadFile(t, f.router, f.owner, files.PurposeSiteDocument, &f.siteA)
	id := uuid.NewString()
	body := map[string]any{"type_key": "health_and_safety_plan", "title": "H&S plan rev A", "version": versionReq(file, nil)}
	wantError(t, do(t, f.router, http.MethodPut, safetyFilePath(f.siteA, id), f.supervisor, body), http.StatusForbidden,
		"only an owner or SHE officer can add documents")
	wantError(t, do(t, f.router, http.MethodPut, safetyFilePath(f.siteA, id), f.owner, with(body, "regime_code", "za_construction_2014")),
		http.StatusBadRequest, "a safety file document takes its site's regime; leave regime_code out")
	wantStatus(t, do(t, f.router, http.MethodPut, safetyFilePath(f.siteA, id), f.owner, body), http.StatusCreated)
	// A file of another site doesn't go in this site's file.
	other := uploadFile(t, f.router, f.owner, files.PurposeSiteDocument, &f.siteB)
	wantError(t, do(t, f.router, http.MethodPut, safetyFilePath(f.siteA, uuid.NewString()), f.owner, with(body, "version", versionReq(other, nil))),
		http.StatusBadRequest, "file was uploaded for another purpose")

	// Supervisors read their own site's file, and open documents (logged).
	r = decodeRegister(t, f.router, f.supervisor, register)
	if p := readinessOf(r, "health_and_safety_plan"); p.Status != documents.StatusPresent || len(r.Documents) != 1 {
		t.Errorf("after the plan: %+v", r)
	}
	wantStatus(t, do(t, f.router, http.MethodPost, safetyFilePath(f.siteA, id)+"/versions/1/view", f.supervisor, nil), http.StatusOK)
	wantError(t, do(t, f.router, http.MethodGet, "/v1/construction/sites/"+f.siteB+"/safety-file", f.supervisor, nil), http.StatusNotFound, "site not found")
	wantError(t, do(t, f.router, http.MethodGet, safetyFilePath(f.siteB, id), f.owner, nil), http.StatusNotFound, "document not found")
	wantStatus(t, do(t, f.router, http.MethodGet, register, f.bookkeeper, nil), http.StatusForbidden)
}
