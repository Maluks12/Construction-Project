package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"construction_db/internal/core/evidence"
	"construction_db/internal/platform/storage"
	"construction_db/internal/platform/storagetest"
)

func testImage(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func declareFor(content []byte, contentType string) map[string]any {
	sum := sha256.Sum256(content)
	return map[string]any{
		"content_sha256":     hex.EncodeToString(sum[:]),
		"size_bytes":         len(content),
		"content_type":       contentType,
		"client_captured_at": "2026-11-06T08:15:30.123456Z",
		"lat":                -26.204103,
		"lng":                28.047305,
		"device_id":          "pixel-7a",
	}
}

// putUpload sends content to a declare's upload URL, as a client would.
func putUpload(t *testing.T, up *evidence.Upload, content []byte) {
	t.Helper()
	req, err := http.NewRequest(up.Method, up.URL, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range up.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload: %d", resp.StatusCode)
	}
}

// declareAndUpload declares content on site and uploads it; returns the id.
func declareAndUpload(t *testing.T, router http.Handler, token, site string, content []byte, contentType string) string {
	t.Helper()
	id := uuid.NewString()
	rec := do(t, router, http.MethodPut, evidencePath(site, id), token, declareFor(content, contentType))
	wantStatus(t, rec, http.StatusCreated)
	putUpload(t, decodeEvidence(t, rec).Upload, content)
	return id
}

func completePath(site, id string) string { return evidencePath(site, id) + "/complete" }

// wantChainValid recomputes the entry's hashes from the response's own fields.
func wantChainValid(t *testing.T, ev evidence.Evidence, wantSeq int64, wantPrev string) {
	t.Helper()
	c := ev.Chain
	if c == nil {
		t.Fatalf("chain is null on %s evidence", ev.Status)
	}
	if c.Seq != wantSeq || c.PrevChainHash != wantPrev {
		t.Errorf("seq %d prev %s, want seq %d prev %s", c.Seq, c.PrevChainHash, wantSeq, wantPrev)
	}
	meta, err := evidence.MetadataHash(evidence.Metadata{
		EvidenceID: ev.ID, ClientCapturedAt: ev.ClientCapturedAt, Lat: ev.Lat, Lng: ev.Lng,
		UploadedBy: ev.UploadedBy, DeviceID: ev.DeviceID,
	})
	if err != nil || hex.EncodeToString(meta[:]) != c.MetadataHash {
		t.Errorf("metadata_hash %s, recomputed %x (err %v)", c.MetadataHash, meta, err)
	}
	content, _ := hex.DecodeString(ev.ContentSHA256)
	prev, _ := hex.DecodeString(c.PrevChainHash)
	link, err := evidence.ChainHash(evidence.Link{
		ContentSHA256: content, ServerReceivedAt: c.ServerReceivedAt, SiteID: ev.SiteID,
		PrevChainHash: prev, MetadataHash: meta[:],
	})
	if err != nil || hex.EncodeToString(link[:]) != c.ChainHash {
		t.Errorf("chain_hash %s, recomputed %x (err %v)", c.ChainHash, link, err)
	}
	if c.ServerReceivedAt.Before(ev.CreatedAt) {
		t.Errorf("server_received_at %s is before created_at %s", c.ServerReceivedAt, ev.CreatedAt)
	}
}

var genesis = strings.Repeat("0", 64)

func TestEvidenceComplete(t *testing.T) {
	f := newEvidenceFixture(t, "complete")
	jpg := testImage(t, "jpeg", 100, 80)

	first := declareAndUpload(t, f.router, f.owner, f.siteA, jpg, "image/jpeg")
	rec := do(t, f.router, http.MethodPost, completePath(f.siteA, first), f.owner, nil)
	wantStatus(t, rec, http.StatusCreated)
	ev1 := decodeEvidence(t, rec)
	if ev1.Status != evidence.StatusVerified {
		t.Fatalf("status %s, want verified", ev1.Status)
	}
	wantChainValid(t, ev1, 1, genesis)

	t.Run("get shows the chain", func(t *testing.T) {
		got := decodeEvidence(t, do(t, f.router, http.MethodGet, evidencePath(f.siteA, first), f.owner, nil))
		if got.Chain == nil || *got.Chain != *ev1.Chain {
			t.Errorf("get chain %+v, want %+v", got.Chain, ev1.Chain)
		}
	})

	t.Run("repeat complete is idempotent", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPost, completePath(f.siteA, first), f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeEvidence(t, rec); *got.Chain != *ev1.Chain {
			t.Errorf("repeat chain %+v, want %+v", got.Chain, ev1.Chain)
		}
	})

	t.Run("next evidence links to the previous", func(t *testing.T) {
		png2048 := testImage(t, "png", 2048, 1)
		id := declareAndUpload(t, f.router, f.owner, f.siteA, png2048, "image/png")
		rec := do(t, f.router, http.MethodPost, completePath(f.siteA, id), f.owner, nil)
		wantStatus(t, rec, http.StatusCreated)
		wantChainValid(t, decodeEvidence(t, rec), 2, ev1.Chain.ChainHash)
	})

	t.Run("another site has its own chain", func(t *testing.T) {
		id := declareAndUpload(t, f.router, f.owner, f.siteB, jpg, "image/jpeg")
		rec := do(t, f.router, http.MethodPost, completePath(f.siteB, id), f.owner, nil)
		wantStatus(t, rec, http.StatusCreated)
		wantChainValid(t, decodeEvidence(t, rec), 1, genesis)
	})

	t.Run("concurrent completes seal it once", func(t *testing.T) {
		id := declareAndUpload(t, f.router, f.owner, f.siteA, testImage(t, "jpeg", 10, 10), "image/jpeg")
		const n = 5
		codes := make([]int, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i] = do(t, f.router, http.MethodPost, completePath(f.siteA, id), f.owner, nil).Code
			}()
		}
		wg.Wait()
		counts := map[int]int{}
		for _, c := range codes {
			counts[c]++
		}
		if counts[http.StatusCreated] != 1 || counts[http.StatusOK] != n-1 {
			t.Errorf("status counts %v, want one 201 and %d 200s", counts, n-1)
		}
		var entries int
		if err := testAdmin.QueryRow(context.Background(), `SELECT count(*) FROM evidence_chain WHERE evidence_id = $1`, id).Scan(&entries); err != nil || entries != 1 {
			t.Errorf("chain entries for it: %d (err %v), want 1", entries, err)
		}
	})

	t.Run("not uploaded yet", func(t *testing.T) {
		id := uuid.NewString()
		rec := do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, declareFor(jpg, "image/jpeg"))
		wantStatus(t, rec, http.StatusCreated)
		up := decodeEvidence(t, rec).Upload

		rec = do(t, f.router, http.MethodPost, completePath(f.siteA, id), f.owner, nil)
		wantStatus(t, rec, http.StatusConflict)
		if rec.Body.String() != `{"error":"file not uploaded yet"}` {
			t.Errorf("body %s", rec.Body.String())
		}
		putUpload(t, up, jpg)
		wantStatus(t, do(t, f.router, http.MethodPost, completePath(f.siteA, id), f.owner, nil), http.StatusCreated)
	})
}

func TestEvidenceCompleteRejects(t *testing.T) {
	f := newEvidenceFixture(t, "reject")
	jpg := testImage(t, "jpeg", 100, 80)

	// writeDirect stores bytes at the evidence's key without the presigned
	// URL, as someone with bucket access might.
	writeDirect := func(t *testing.T, id string, content []byte) {
		t.Helper()
		key := "orgs/" + f.ownerMe.OrgID + "/sites/" + f.siteA + "/evidence/" + id
		_, err := testS3.Admin.PutObject(context.Background(), &s3.PutObjectInput{
			Bucket: aws.String(storagetest.Bucket), Key: aws.String(key), Body: bytes.NewReader(content),
		})
		if err != nil {
			t.Fatalf("direct put: %v", err)
		}
	}
	declareOnly := func(t *testing.T, content []byte, contentType string) string {
		t.Helper()
		id := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, declareFor(content, contentType)), http.StatusCreated)
		return id
	}

	tests := []struct {
		name   string
		setup  func(t *testing.T) string
		reason string
		msg    string
	}{
		{"tampered bytes", func(t *testing.T) string {
			id := declareOnly(t, jpg, "image/jpeg")
			tampered := append([]byte{}, jpg...)
			tampered[len(tampered)/2] ^= 0xFF // same length, so only the hash can catch it
			writeDirect(t, id, tampered)
			return id
		}, evidence.ReasonHashMismatch, "uploaded file does not match content_sha256"},
		{"other length", func(t *testing.T) string {
			id := declareOnly(t, jpg, "image/jpeg")
			writeDirect(t, id, append(append([]byte{}, jpg...), 0))
			return id
		}, evidence.ReasonSizeMismatch, "uploaded file is not size_bytes long"},
		{"not an image", func(t *testing.T) string {
			return declareAndUpload(t, f.router, f.owner, f.siteA, []byte("photo bytes"), "image/jpeg")
		}, evidence.ReasonNotAnImage, "uploaded file is not a JPEG or PNG"},
		{"png declared as jpeg", func(t *testing.T) string {
			return declareAndUpload(t, f.router, f.owner, f.siteA, testImage(t, "png", 10, 10), "image/jpeg")
		}, evidence.ReasonWrongType, "uploaded file is not the declared content_type"},
		{"2049px wide", func(t *testing.T) string {
			return declareAndUpload(t, f.router, f.owner, f.siteA, testImage(t, "jpeg", 2049, 1), "image/jpeg")
		}, evidence.ReasonTooLargeDimensions, "image is larger than 2048px on its long edge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.setup(t)
			rec := do(t, f.router, http.MethodPost, completePath(f.siteA, id), f.owner, nil)
			wantStatus(t, rec, http.StatusBadRequest)
			if want := `{"error":"` + tt.msg + `; declare a new evidence id","rejected_reason":"` + tt.reason + `"}`; rec.Body.String() != want {
				t.Errorf("body %s, want %s", rec.Body.String(), want)
			}

			got := decodeEvidence(t, do(t, f.router, http.MethodGet, evidencePath(f.siteA, id), f.owner, nil))
			if got.Status != evidence.StatusRejected || got.RejectedReason == nil || *got.RejectedReason != tt.reason || got.Chain != nil {
				t.Errorf("after rejection: %+v, want rejected (%s) with no chain", got, tt.reason)
			}
			wantStatus(t, do(t, f.router, http.MethodPost, completePath(f.siteA, id), f.owner, nil), http.StatusConflict)
		})
	}
}

func TestEvidenceCompleteAccess(t *testing.T) {
	f := newEvidenceFixture(t, "complete-access")
	jpg := testImage(t, "jpeg", 10, 10)
	bySupervisor := declareAndUpload(t, f.router, f.supervisor, f.siteA, jpg, "image/jpeg")

	for _, tt := range []struct {
		name, token string
		want        int
	}{
		{"bookkeeper", f.bookkeeper, http.StatusForbidden},
		{"other org", f.other, http.StatusNotFound},
		{"owner, not the uploader", f.owner, http.StatusNotFound},
		{"the uploader", f.supervisor, http.StatusCreated},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wantStatus(t, do(t, f.router, http.MethodPost, completePath(f.siteA, bySupervisor), tt.token, nil), tt.want)
		})
	}
}

// overwritingStore lets a new version of the object land right after Stat,
// as a write between Stat and ReadVersion would.
type overwritingStore struct {
	storage.Store
	overwrite func(key string)
}

func (s overwritingStore) Stat(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	info, err := s.Store.Stat(ctx, key)
	if err == nil {
		s.overwrite(key)
	}
	return info, err
}

// Complete reads exactly the version Stat saw, so a version written after
// that can't swap what is checked and sealed.
func TestEvidenceCompleteReadsTheStatVersion(t *testing.T) {
	ctx := context.Background()
	store := overwritingStore{Store: testStore, overwrite: func(key string) {
		_, err := testS3.Admin.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(storagetest.Bucket), Key: aws.String(key), Body: strings.NewReader("a newer version"),
		})
		if err != nil {
			t.Errorf("write newer version: %v", err)
		}
	}}
	router, err := NewRouter(ctx, testPool, testSecret, testRegistry, testEdition, store, testHTTP)
	if err != nil {
		t.Fatal(err)
	}
	token := signup(t, router, "Version Co", "owner@version-"+uuid.NewString()[:8]+".test")
	site := listSites(t, router, token)[0].ID

	id := declareAndUpload(t, router, token, site, testImage(t, "jpeg", 10, 10), "image/jpeg")
	rec := do(t, router, http.MethodPost, completePath(site, id), token, nil)
	wantStatus(t, rec, http.StatusCreated)
	if ev := decodeEvidence(t, rec); ev.Status != evidence.StatusVerified {
		t.Errorf("status %s, want the original version verified", ev.Status)
	}
}

// A bucket that doesn't lock what it stores is the server's fault, not the
// client's: 500, and the evidence stays pending.
func TestEvidenceCompleteNeedsObjectLock(t *testing.T) {
	ctx := context.Background()
	bucket := "no-retention-" + uuid.NewString()[:8]
	if err := storagetest.CreateBucket(ctx, testS3.Admin, bucket, true); err != nil {
		t.Fatal(err)
	}
	cfg := testS3.Config
	cfg.Bucket = bucket
	store, err := storage.NewS3(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(ctx, testPool, testSecret, testRegistry, testEdition, store, testHTTP)
	if err != nil {
		t.Fatal(err)
	}
	token := signup(t, router, "No Lock Co", "owner@nolock-"+uuid.NewString()[:8]+".test")
	site := listSites(t, router, token)[0].ID

	id := declareAndUpload(t, router, token, site, testImage(t, "jpeg", 10, 10), "image/jpeg")
	rec := do(t, router, http.MethodPost, completePath(site, id), token, nil)
	wantStatus(t, rec, http.StatusInternalServerError)
	if got := decodeEvidence(t, do(t, router, http.MethodGet, evidencePath(site, id), token, nil)); got.Status != evidence.StatusPending {
		t.Errorf("status %s, want still pending", got.Status)
	}
}
