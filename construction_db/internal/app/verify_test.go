package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
)

func chainPath(site string) string { return "/v1/sites/" + site + "/chain" }

// buildChain gives site a real chain of n entries: declare, upload, complete.
func buildChain(t *testing.T, router http.Handler, token, site string, n int) []evidence.Evidence {
	t.Helper()
	var out []evidence.Evidence
	for i := range n {
		id := declareAndUpload(t, router, token, site, testImage(t, "jpeg", 20+i, 20), "image/jpeg")
		rec := do(t, router, http.MethodPost, completePath(site, id), token, nil)
		wantStatus(t, rec, http.StatusCreated)
		out = append(out, decodeEvidence(t, rec))
	}
	return out
}

func verifyChain(t *testing.T, router http.Handler, token, site string) evidence.ChainVerification {
	t.Helper()
	rec := do(t, router, http.MethodGet, chainPath(site), token, nil)
	wantStatus(t, rec, http.StatusOK)
	var v evidence.ChainVerification
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode verification: %v", err)
	}
	return v
}

// tamper runs statements as the database superuser in one committed
// transaction, with the evidence freeze trigger switched off.
func tamper(t *testing.T, stmts ...string) {
	t.Helper()
	ctx := context.Background()
	tx, err := testAdmin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	all := append([]string{`ALTER TABLE evidence DISABLE TRIGGER evidence_freeze`}, stmts...)
	all = append(all, `ALTER TABLE evidence ENABLE TRIGGER evidence_freeze`)
	for _, s := range all {
		tag, err := tx.Exec(ctx, s)
		if err != nil {
			t.Fatalf("tamper %q: %v", s, err)
		}
		if strings.HasPrefix(s, "UPDATE") || strings.HasPrefix(s, "DELETE") {
			if tag.RowsAffected() != 1 {
				t.Fatalf("tamper %q changed %d rows, want 1", s, tag.RowsAffected())
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyChain(t *testing.T) {
	f := newEvidenceFixture(t, "verify")
	inviteMember(t, f.router, f.owner, "she@"+uuid.NewString()[:8]+".verify.test", auth.RoleSHEOfficer)

	t.Run("an empty site", func(t *testing.T) {
		v := verifyChain(t, f.router, f.owner, createSite(t, f.router, f.owner, "Empty"))
		if !v.Intact || v.Length != 0 || v.HeadSeq != 0 || v.HeadHash != genesis || v.FirstBreak != nil {
			t.Errorf("got %+v, want intact, length 0, genesis head", v)
		}
	})

	t.Run("an untouched chain", func(t *testing.T) {
		site := createSite(t, f.router, f.owner, "Untouched")
		built := buildChain(t, f.router, f.owner, site, 3)
		v := verifyChain(t, f.router, f.owner, site)
		if !v.Intact || v.Length != 3 || v.HeadSeq != 3 || v.HeadHash != built[2].Chain.ChainHash || v.FirstBreak != nil {
			t.Errorf("got %+v, want intact with seq 3's hash as head", v)
		}
		if v.SiteID != site || v.CheckedAt.IsZero() {
			t.Errorf("site %s checked at %s", v.SiteID, v.CheckedAt)
		}
		if raw := do(t, f.router, http.MethodGet, chainPath(site), f.owner, nil).Body.String(); !strings.Contains(raw, `"first_break":null`) {
			t.Errorf("body %s, want first_break null", raw)
		}
	})

	cases := []struct {
		name   string
		tamper func(site string, built []evidence.Evidence) []string
		reason string // "" means intact
		atSeq  int64  // 0 means null
		length int64
	}{
		{"a capture time moved 1µs", func(site string, b []evidence.Evidence) []string {
			return []string{`UPDATE evidence SET client_captured_at = client_captured_at + interval '1 microsecond' WHERE id = '` + b[1].ID + `'`}
		}, evidence.BreakMetadataChanged, 2, 3},
		{"a server time moved 1µs", func(site string, b []evidence.Evidence) []string {
			return []string{`UPDATE evidence_chain SET server_received_at = server_received_at + interval '1 microsecond' WHERE evidence_id = '` + b[1].ID + `'`}
		}, evidence.BreakChainHashMismatch, 2, 3},
		{"a chained evidence row set back to pending", func(site string, b []evidence.Evidence) []string {
			return []string{`UPDATE evidence SET status = 'pending' WHERE id = '` + b[1].ID + `'`}
		}, evidence.BreakEntryWithoutVerifiedEvidence, 2, 3},
		{"an entry deleted from the middle", func(site string, b []evidence.Evidence) []string {
			return []string{`DELETE FROM evidence_chain WHERE evidence_id = '` + b[1].ID + `'`}
		}, evidence.BreakSeqGap, 3, 2},
		{"the last entry deleted, head left alone", func(site string, b []evidence.Evidence) []string {
			return []string{`DELETE FROM evidence_chain WHERE evidence_id = '` + b[2].ID + `'`}
		}, evidence.BreakHeadMismatch, 3, 2},
		{"the head moved past the last entry", func(site string, b []evidence.Evidence) []string {
			return []string{`UPDATE site_chain_heads SET seq = seq + 1 WHERE site_id = '` + site + `'`}
		}, evidence.BreakHeadMismatch, 4, 3},
		{"the last entry deleted and the head rewound", func(site string, b []evidence.Evidence) []string {
			return []string{
				`DELETE FROM evidence_chain WHERE evidence_id = '` + b[2].ID + `'`,
				`UPDATE site_chain_heads SET seq = 2, head_hash = decode('` + b[1].Chain.ChainHash + `', 'hex') WHERE site_id = '` + site + `'`,
			}
		}, evidence.BreakVerifiedWithoutEntry, 0, 2},
		// The documented limit: remove the evidence row as well and nothing in
		// the database shows it. Only an external record of the head does.
		{"truncated completely: entry, evidence row and head", func(site string, b []evidence.Evidence) []string {
			return []string{
				`DELETE FROM evidence_chain WHERE evidence_id = '` + b[2].ID + `'`,
				`DELETE FROM evidence WHERE id = '` + b[2].ID + `'`,
				`UPDATE site_chain_heads SET seq = 2, head_hash = decode('` + b[1].Chain.ChainHash + `', 'hex') WHERE site_id = '` + site + `'`,
			}
		}, "", 0, 2},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			site := createSite(t, f.router, f.owner, tt.name)
			built := buildChain(t, f.router, f.owner, site, 3)
			tamper(t, tt.tamper(site, built)...)

			v := verifyChain(t, f.router, f.owner, site)
			if v.Length != tt.length {
				t.Errorf("length %d, want %d", v.Length, tt.length)
			}
			if tt.reason == "" {
				if !v.Intact || v.FirstBreak != nil {
					t.Errorf("got %+v, want intact (the known truncation limit)", v)
				}
				return
			}
			b := v.FirstBreak
			if v.Intact || b == nil {
				t.Fatalf("got intact, want %s", tt.reason)
			}
			gotSeq := int64(0)
			if b.Seq != nil {
				gotSeq = *b.Seq
			}
			if b.Reason != tt.reason || gotSeq != tt.atSeq {
				t.Errorf("break %s at seq %d, want %s at seq %d", b.Reason, gotSeq, tt.reason, tt.atSeq)
			}
		})
	}

	t.Run("verified evidence never chained", func(t *testing.T) {
		site := createSite(t, f.router, f.owner, "Orphan")
		buildChain(t, f.router, f.owner, site, 2)
		orphan := declareAndUpload(t, f.router, f.owner, site, testImage(t, "jpeg", 30, 30), "image/jpeg")
		execAsTenant(t, f.ownerMe.OrgID, 1, `UPDATE evidence SET status = 'verified', object_version_id = 'v1' WHERE id = $1`, orphan)

		v := verifyChain(t, f.router, f.owner, site)
		if b := v.FirstBreak; b == nil || b.Reason != evidence.BreakVerifiedWithoutEntry || b.Seq != nil || b.EvidenceID != orphan {
			t.Errorf("got %+v, want verified_without_entry for %s with seq null", b, orphan)
		}
	})
}

func TestVerifyChainAccess(t *testing.T) {
	f := newEvidenceFixture(t, "verify-access")
	sheEmail := "she@" + uuid.NewString()[:8] + ".verify.test"
	inviteMember(t, f.router, f.owner, sheEmail, auth.RoleSHEOfficer)
	she := login(t, f.router, sheEmail)

	for _, tt := range []struct {
		name, token string
		want        int
	}{
		{"owner", f.owner, http.StatusOK},
		{"SHE officer", she, http.StatusOK},
		{"supervisor", f.supervisor, http.StatusForbidden},
		{"bookkeeper", f.bookkeeper, http.StatusForbidden},
		{"other org", f.other, http.StatusNotFound},
		{"no token", "", http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wantStatus(t, do(t, f.router, http.MethodGet, chainPath(f.siteA), tt.token, nil), tt.want)
		})
	}
	wantStatus(t, do(t, f.router, http.MethodGet, chainPath("not-a-uuid"), f.owner, nil), http.StatusNotFound)
}

// Verifying while appends commit must never report a false break: the walk
// reads one snapshot. (TestWithTenantSnapshot is the deterministic proof.)
func TestVerifyDuringAppends(t *testing.T) {
	f := newEvidenceFixture(t, "verify-busy")
	const n = 10
	var contents [][]byte
	for i := range n {
		contents = append(contents, testImage(t, "jpeg", 40+i, 10))
	}

	ids := make([]string, n)
	errs := make([]error, n)
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	completes := make([]int, n)
	verifies := make([]evidence.ChainVerification, n)
	verifyCodes := make([]int, n)
	for i := range n {
		ready.Add(1)
		done.Add(2)
		go func() {
			defer done.Done()
			ids[i], errs[i] = prepareUpload(f.router, f.owner, f.siteA, contents[i])
			ready.Done()
			<-start
			if errs[i] != nil {
				return
			}
			rec, err := request(f.router, http.MethodPost, completePath(f.siteA, ids[i]), f.owner, nil)
			if err == nil {
				completes[i] = rec.Code
			}
		}()
		go func() {
			defer done.Done()
			<-start
			rec, err := request(f.router, http.MethodGet, chainPath(f.siteA), f.owner, nil)
			if err != nil {
				return
			}
			verifyCodes[i] = rec.Code
			_ = json.Unmarshal(rec.Body.Bytes(), &verifies[i])
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()

	for i := range n {
		if errs[i] != nil || completes[i] != http.StatusCreated {
			t.Errorf("complete %d: %d, err %v; want 201", i, completes[i], errs[i])
		}
		if verifyCodes[i] != http.StatusOK || !verifies[i].Intact {
			t.Errorf("verify %d during appends: %d %+v; want 200 intact", i, verifyCodes[i], verifies[i])
		}
	}
	if v := verifyChain(t, f.router, f.owner, f.siteA); !v.Intact || v.Length != n {
		t.Errorf("after the appends: %+v, want intact with %d entries", v, n)
	}
}
