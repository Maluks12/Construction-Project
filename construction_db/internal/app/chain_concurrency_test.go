package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/evidence"
	"construction_db/internal/core/evidence/evidencetest"
)

// request is do without a *testing.T, for use in goroutines.
func request(router http.Handler, method, path, token string, body any) (*httptest.ResponseRecorder, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec, nil
}

// prepareUpload declares content and uploads it, without t.Fatal so it can
// run in a goroutine.
func prepareUpload(router http.Handler, token, site string, content []byte) (string, error) {
	id := uuid.NewString()
	rec, err := request(router, http.MethodPut, evidencePath(site, id), token, declareFor(content, "image/jpeg"))
	if err != nil {
		return "", err
	}
	if rec.Code != http.StatusCreated {
		return "", fmt.Errorf("declare: %d %s", rec.Code, rec.Body.String())
	}
	var ev evidence.Evidence
	if err := json.Unmarshal(rec.Body.Bytes(), &ev); err != nil {
		return "", err
	}
	req, err := http.NewRequest(ev.Upload.Method, ev.Upload.URL, bytes.NewReader(content))
	if err != nil {
		return "", err
	}
	for k, v := range ev.Upload.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload: %d", resp.StatusCode)
	}
	return id, nil
}

// Many uploads to one site at once must end as one unbroken chain: the head
// row lock serialises the appends.
func TestParallelUploadsGiveOneChain(t *testing.T) {
	f := newEvidenceFixture(t, "parallel")
	perSite := map[string]int{f.siteA: 20, f.siteB: 5}

	type job struct {
		site    string
		content []byte
		code    int
		err     error
	}
	var jobs []*job
	for site, n := range perSite {
		for range n {
			// A distinct file each, made here: t.Fatal can't run in a goroutine.
			jobs = append(jobs, &job{site: site, content: testImage(t, "jpeg", 10+len(jobs), 10)})
		}
	}

	// Every goroutine declares and uploads, then waits here, so all the
	// completes fire together and contend for each site's lock.
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	for _, j := range jobs {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			id, err := prepareUpload(f.router, f.owner, j.site, j.content)
			ready.Done()
			<-start
			if err != nil {
				j.err = err
				return
			}
			rec, err := request(f.router, http.MethodPost, completePath(j.site, id), f.owner, nil)
			if err != nil {
				j.err = err
				return
			}
			j.code = rec.Code
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()

	for _, j := range jobs {
		if j.err != nil || j.code != http.StatusCreated {
			t.Errorf("site %s: complete got %d, err %v; want 201", j.site, j.code, j.err)
		}
	}
	for site, n := range perSite {
		wantUnbrokenChain(t, site, n)
	}
}

// wantUnbrokenChain checks a site's chain as stored, in Go and with the SQL
// published in CHAIN.md.
func wantUnbrokenChain(t *testing.T, site string, n int) {
	t.Helper()
	ctx := context.Background()
	rows, err := testAdmin.Query(ctx, `
		SELECT c.seq, c.content_sha256, c.server_received_at, c.prev_chain_hash, c.metadata_hash, c.chain_hash,
		       e.id, e.client_captured_at, e.lat, e.lng, e.uploaded_by, e.device_id
		FROM evidence_chain c JOIN evidence e ON e.id = c.evidence_id
		WHERE c.site_id = $1 ORDER BY c.seq`, site)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	prev := evidence.GenesisHash[:]
	var lastReceived time.Time
	var seen int64
	for rows.Next() {
		var (
			seq                         int64
			content, prevHash, meta, ch []byte
			received                    time.Time
			m                           evidence.Metadata
		)
		if err := rows.Scan(&seq, &content, &received, &prevHash, &meta, &ch,
			&m.EvidenceID, &m.ClientCapturedAt, &m.Lat, &m.Lng, &m.UploadedBy, &m.DeviceID); err != nil {
			t.Fatal(err)
		}
		seen++
		if seq != seen {
			t.Errorf("site %s: entry %d has seq %d", site, seen, seq)
		}
		if !bytes.Equal(prevHash, prev) {
			t.Errorf("site %s seq %d: prev %x, want the previous chain hash %x", site, seq, prevHash, prev)
		}
		if received.Before(lastReceived) {
			t.Errorf("site %s seq %d: server_received_at %s is before seq %d's %s", site, seq, received, seq-1, lastReceived)
		}
		wantMeta, err := evidence.MetadataHash(m)
		if err != nil || !bytes.Equal(meta, wantMeta[:]) {
			t.Errorf("site %s seq %d: metadata_hash %x, recomputed %x (err %v)", site, seq, meta, wantMeta, err)
		}
		wantLink, err := evidence.ChainHash(evidence.Link{
			ContentSHA256: content, ServerReceivedAt: received, SiteID: site, PrevChainHash: prevHash, MetadataHash: meta,
		})
		if err != nil || !bytes.Equal(ch, wantLink[:]) {
			t.Errorf("site %s seq %d: chain_hash %x, recomputed %x (err %v)", site, seq, ch, wantLink, err)
		}
		prev, lastReceived = ch, received
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != int64(n) {
		t.Errorf("site %s: %d entries, want %d", site, seen, n)
	}

	var headSeq int64
	var headHash []byte
	if err := testAdmin.QueryRow(ctx, `SELECT seq, head_hash FROM site_chain_heads WHERE site_id = $1`, site).Scan(&headSeq, &headHash); err != nil {
		t.Fatal(err)
	}
	if headSeq != seen || !bytes.Equal(headHash, prev) {
		t.Errorf("site %s: head (%d, %s), want the last entry (%d, %s)", site, headSeq, hex.EncodeToString(headHash), seen, hex.EncodeToString(prev))
	}

	checks, err := evidencetest.Recompute(ctx, testAdmin, site)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != n {
		t.Errorf("site %s: CHAIN.md's query returned %d rows, want %d", site, len(checks), n)
	}
	for _, c := range checks {
		if !c.OK() {
			t.Errorf("site %s seq %d: CHAIN.md's query says %+v", site, c.Seq, c)
		}
	}
}
