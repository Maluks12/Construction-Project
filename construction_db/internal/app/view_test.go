package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/evidence"
)

func viewPath(site, id string) string { return evidencePath(site, id) + "/view" }

// verifiedJPEG declares, uploads and completes a JPEG on site; returns its id and bytes.
func verifiedJPEG(t *testing.T, f evidenceFixture, token, site string) (string, []byte) {
	t.Helper()
	content := testImage(t, "jpeg", 40, 30)
	id := declareAndUpload(t, f.router, token, site, content, "image/jpeg")
	wantStatus(t, do(t, f.router, http.MethodPost, completePath(site, id), token, nil), http.StatusCreated)
	return id, content
}

type bytesRow struct{ UserID, ObjectVersionID string }

// viewRows are the view_bytes rows for one evidence id, oldest first.
func viewRows(t *testing.T, evidenceID string) []bytesRow {
	t.Helper()
	rows, err := testAdmin.Query(context.Background(), `
		SELECT user_id::text, object_version_id FROM evidence_access_log
		WHERE evidence_id = $1 AND action = 'view_bytes' ORDER BY accessed_at, id`, evidenceID)
	if err != nil {
		t.Fatalf("read access log: %v", err)
	}
	defer rows.Close()
	var out []bytesRow
	for rows.Next() {
		var r bytesRow
		if err := rows.Scan(&r.UserID, &r.ObjectVersionID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read access log: %v", err)
	}
	return out
}

func TestEvidenceView(t *testing.T) {
	f := newEvidenceFixture(t, "view")
	id, content := verifiedJPEG(t, f, f.owner, f.siteA)
	var sealedVersion string
	if err := testAdmin.QueryRow(context.Background(), `SELECT object_version_id FROM evidence WHERE id = $1`, id).Scan(&sealedVersion); err != nil {
		t.Fatalf("read version: %v", err)
	}

	t.Run("the URL serves the sealed bytes as the verified type", func(t *testing.T) {
		before := time.Now()
		rec := do(t, f.router, http.MethodPost, viewPath(f.siteA, id), f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		var v evidence.ViewURL
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatalf("decode: %v (body: %s)", err, rec.Body.String())
		}
		if v.Method != http.MethodGet || v.ExpiresAt.Before(before.Add(time.Minute)) || v.ExpiresAt.After(time.Now().Add(time.Minute)) {
			t.Errorf("view = %+v, want GET expiring about 60s from now", v)
		}

		resp, err := http.Get(v.URL)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		got, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || !bytes.Equal(got, content) {
			t.Fatalf("fetch: status %d, %d bytes, err %v; want the %d uploaded bytes", resp.StatusCode, len(got), err, len(content))
		}
		if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
			t.Errorf("Content-Type %q, want image/jpeg", ct)
		}
	})

	pending := declareAndUpload(t, f.router, f.owner, f.siteA, testImage(t, "jpeg", 10, 10), "image/jpeg")
	rejected := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, rejected), f.owner, declareBody("rejected")), http.StatusCreated)
	execAsTenant(t, f.ownerMe.OrgID, 1, `UPDATE evidence SET status = 'rejected', rejected_reason = 'test' WHERE id = $1`, rejected)

	tests := []struct {
		name, token, path string
		want              int
	}{
		{"supervisor on own site", f.supervisor, viewPath(f.siteA, id), http.StatusOK},
		{"pending", f.owner, viewPath(f.siteA, pending), http.StatusConflict},
		{"rejected", f.owner, viewPath(f.siteA, rejected), http.StatusConflict},
		{"bookkeeper", f.bookkeeper, viewPath(f.siteA, id), http.StatusForbidden},
		{"supervisor elsewhere", f.supervisor, viewPath(f.siteB, id), http.StatusNotFound},
		{"the wrong site", f.owner, viewPath(f.siteB, id), http.StatusNotFound},
		{"other org", f.other, viewPath(f.siteA, id), http.StatusNotFound},
		{"unknown id", f.owner, viewPath(f.siteA, uuid.NewString()), http.StatusNotFound},
		{"malformed id", f.owner, viewPath(f.siteA, "not-a-uuid"), http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, f.router, http.MethodPost, tt.path, tt.token, nil)
			wantStatus(t, rec, tt.want)
			if tt.want != http.StatusOK && strings.Contains(rec.Body.String(), `"url"`) {
				t.Errorf("refusal carries a URL: %s", rec.Body.String())
			}
		})
	}
	if body := do(t, f.router, http.MethodPost, viewPath(f.siteA, pending), f.owner, nil).Body.String(); body != `{"error":"evidence not verified"}` {
		t.Errorf("pending body %s", body)
	}

	supervisorID := me(t, f.router, f.supervisor).UserID
	got := viewRows(t, id)
	want := []bytesRow{{f.ownerMe.UserID, sealedVersion}, {supervisorID, sealedVersion}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("view_bytes rows %+v, want one per successful view, each naming the sealed version: %+v", got, want)
	}
	for _, other := range []string{pending, rejected} {
		if rows := viewRows(t, other); len(rows) != 0 {
			t.Errorf("refused view of %s logged %d rows", other, len(rows))
		}
	}
}

// No log, no data: if the access-log row can't be written, the read fails
// and returns nothing, not even a URL.
//
// Revoking a grant is safe only because nothing else uses this database
// meanwhile: each test package starts its own Postgres (dbtest.Start), and no
// test in this package calls t.Parallel. Adding t.Parallel here or to any
// test in this package would break that. t.Cleanup runs even after t.Fatal
// or a panic, and checks the grant is really back.
func TestAccessLogFailureFailsTheRead(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "nolog")
	id, _ := verifiedJPEG(t, f, f.owner, f.siteA)

	if _, err := testAdmin.Exec(ctx, `REVOKE INSERT ON evidence_access_log FROM siteproof_app`); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testAdmin.Exec(ctx, `GRANT INSERT ON evidence_access_log TO siteproof_app`); err != nil {
			t.Errorf("restore grant: %v", err)
		}
		var ok bool
		if err := testAdmin.QueryRow(ctx, `SELECT has_table_privilege('siteproof_app', 'evidence_access_log', 'INSERT')`).Scan(&ok); err != nil || !ok {
			t.Errorf("siteproof_app INSERT on evidence_access_log after cleanup: %v, err %v; later tests will fail", ok, err)
		}
	})

	for name, req := range map[string]struct{ method, path string }{
		"get":  {http.MethodGet, evidencePath(f.siteA, id)},
		"list": {http.MethodGet, "/v1/sites/" + f.siteA + "/evidence"},
		"view": {http.MethodPost, viewPath(f.siteA, id)},
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, f.router, req.method, req.path, f.owner, nil)
			if rec.Code != http.StatusInternalServerError || rec.Body.String() != `{"error":"internal error"}` {
				t.Errorf("got %d %s, want 500 internal error and nothing else", rec.Code, rec.Body.String())
			}
		})
	}
}
