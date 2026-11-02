package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"construction_db/internal/platform/storage"
	"construction_db/internal/platform/storagetest"
)

var env *storagetest.Env

func TestMain(m *testing.M) {
	e, cleanup, err := storagetest.Start(context.Background())
	if err != nil {
		log.Printf("start test storage: %v", err)
		os.Exit(1)
	}
	env = e

	code := m.Run()
	cleanup()
	os.Exit(code)
}

func presign(t *testing.T, key string, body []byte) *storage.PresignedPut {
	t.Helper()
	sum := sha256.Sum256(body)
	p, err := env.Store.PresignPut(context.Background(), key, sum[:], 5*time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	return p
}

// upload sends body the way a client would, with the signed headers minus skip.
func upload(t *testing.T, p *storage.PresignedPut, body []byte, skip string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(p.Method, p.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range p.Headers {
		if k != skip {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	return resp
}

func TestPresignedPut(t *testing.T) {
	ctx := context.Background()
	body := []byte("photo bytes")
	p := presign(t, "put/ok", body)

	if p.Headers["X-Amz-Checksum-Sha256"] == "" || p.Headers["If-None-Match"] != "*" {
		t.Fatalf("signed headers %v lack the checksum or If-None-Match", p.Headers)
	}
	u, err := url.Parse(p.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	for k := range u.Query() {
		if strings.EqualFold(k, "X-Amz-Checksum-Sha256") {
			t.Errorf("checksum was hoisted into the query, where enforcement is undocumented: %s", p.URL)
		}
	}

	if resp := upload(t, p, body, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("put: status %d, want 200", resp.StatusCode)
	}

	info, err := env.Store.Stat(ctx, "put/ok")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size != int64(len(body)) || info.VersionID == "" {
		t.Errorf("stat = %+v, want size %d and a version id", info, len(body))
	}
	if info.LockMode != storage.LockGovernance || info.RetainUntil == nil || !info.RetainUntil.After(time.Now()) {
		t.Errorf("stat = %+v, want the bucket's default GOVERNANCE retention", info)
	}

	for _, version := range []string{info.VersionID, ""} {
		r, err := env.Store.Open(ctx, "put/ok", version)
		if err != nil {
			t.Fatalf("open version %q: %v", version, err)
		}
		got, err := io.ReadAll(r)
		r.Close()
		if err != nil || !bytes.Equal(got, body) {
			t.Errorf("open version %q read %q, err %v; want %q", version, got, err, body)
		}
	}
}

func TestPresignedPutRejectsOtherBytes(t *testing.T) {
	p := presign(t, "put/mismatch", []byte("declared"))

	if resp := upload(t, p, []byte("something else"), ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("put with other bytes: status %d, want 400", resp.StatusCode)
	}
	if _, err := env.Store.Stat(context.Background(), "put/mismatch"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("stat after rejected put: %v, want ErrNotFound", err)
	}
}

func TestPresignedPutNeedsSignedHeaders(t *testing.T) {
	body := []byte("photo bytes")
	for _, h := range []string{"X-Amz-Checksum-Sha256", "If-None-Match"} {
		t.Run("without "+h, func(t *testing.T) {
			key := "put/unsigned-" + h
			p := presign(t, key, body)
			// AWS answers 403, MinIO 400; either way nothing may be stored.
			if resp := upload(t, p, body, h); resp.StatusCode < 400 || resp.StatusCode > 499 {
				t.Errorf("put without %s: status %d, want 4xx", h, resp.StatusCode)
			}
			if _, err := env.Store.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("stat after refused put: %v, want ErrNotFound", err)
			}
		})
	}
}

func TestPresignedPutOnlyOnce(t *testing.T) {
	body := []byte("photo bytes")
	p := presign(t, "put/once", body)

	if resp := upload(t, p, body, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("first put: status %d, want 200", resp.StatusCode)
	}
	if resp := upload(t, p, body, ""); resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("second put: status %d, want 412", resp.StatusCode)
	}
}

// MinIO accepts an empty versionId but AWS rejects it, so this checks the
// request itself rather than relying on MinIO.
func TestOpenOmitsEmptyVersion(t *testing.T) {
	queries := make(chan url.Values, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query()
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
	}))
	defer srv.Close()

	cfg := env.Config
	cfg.Endpoint = srv.URL
	s, err := storage.NewS3(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	for version, want := range map[string]bool{"": false, "v1": true} {
		if _, err := s.Open(context.Background(), "k", version); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("open version %q: %v, want ErrNotFound", version, err)
		}
		if got := (<-queries).Has("versionId"); got != want {
			t.Errorf("open version %q: versionId in query = %v, want %v", version, got, want)
		}
	}
}

func TestMissingObject(t *testing.T) {
	ctx := context.Background()
	if _, err := env.Store.Stat(ctx, "missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("stat: %v, want ErrNotFound", err)
	}
	if _, err := env.Store.Open(ctx, "missing", "00000000-0000-0000-0000-000000000000"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("open: %v, want ErrNotFound", err)
	}
}

func TestCheckObjectLock(t *testing.T) {
	ctx := context.Background()

	if err := env.Store.CheckObjectLock(ctx, storage.LockGovernance); err != nil {
		t.Errorf("locked bucket, matching mode: %v", err)
	}
	if err := env.Store.CheckObjectLock(ctx, storage.LockCompliance); err == nil {
		t.Error("locked bucket, wrong mode: want an error")
	}

	storeFor := func(t *testing.T, bucket string, lock bool) *storage.S3 {
		t.Helper()
		if err := storagetest.CreateBucket(ctx, env.Admin, bucket, lock); err != nil {
			t.Fatal(err)
		}
		cfg := env.Config
		cfg.Bucket = bucket
		s, err := storage.NewS3(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	t.Run("bucket without object lock", func(t *testing.T) {
		if err := storeFor(t, "no-lock", false).CheckObjectLock(ctx, storage.LockGovernance); err == nil {
			t.Error("want an error")
		}
	})
	t.Run("locked bucket without default retention", func(t *testing.T) {
		if err := storeFor(t, "no-retention", true).CheckObjectLock(ctx, storage.LockGovernance); err == nil {
			t.Error("want an error")
		}
	})
	t.Run("compliance bucket", func(t *testing.T) {
		s := storeFor(t, "compliance", true)
		if err := storagetest.SetDefaultRetention(ctx, env.Admin, "compliance", types.ObjectLockRetentionModeCompliance); err != nil {
			t.Fatal(err)
		}
		if err := s.CheckObjectLock(ctx, storage.LockCompliance); err != nil {
			t.Errorf("want no error, got %v", err)
		}
	})
}
