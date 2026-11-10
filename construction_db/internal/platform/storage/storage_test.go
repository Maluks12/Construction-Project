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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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
	return presignSized(t, key, body, int64(len(body)))
}

func presignSized(t *testing.T, key string, body []byte, size int64) *storage.PresignedPut {
	t.Helper()
	sum := sha256.Sum256(body)
	p, err := env.Store.PresignPut(context.Background(), key, sum[:], size, "image/jpeg", 5*time.Minute)
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

	if p.Headers["X-Amz-Checksum-Sha256"] == "" || p.Headers["If-None-Match"] != "*" || p.Headers["Content-Type"] != "image/jpeg" {
		t.Fatalf("signed headers %v lack the checksum, If-None-Match or Content-Type", p.Headers)
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
	head, err := env.Admin.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(env.Config.Bucket), Key: aws.String("put/ok")})
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if ct := aws.ToString(head.ContentType); ct != "image/jpeg" {
		t.Errorf("stored Content-Type %q, want the signed image/jpeg", ct)
	}

	for _, version := range []string{info.VersionID, ""} {
		r, err := env.Store.ReadVersion(ctx, "put/ok", version)
		if err != nil {
			t.Fatalf("read version %q: %v", version, err)
		}
		got, err := io.ReadAll(r)
		r.Close()
		if err != nil || !bytes.Equal(got, body) {
			t.Errorf("read version %q got %q, err %v; want %q", version, got, err, body)
		}
	}
}

func TestPresignedPutRejectsOtherBytes(t *testing.T) {
	p := presign(t, "put/mismatch", []byte("declared"))

	// Same length, so only the checksum can refuse it (400 BadDigest).
	if resp := upload(t, p, []byte("DECLARED"), ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("put with other bytes: status %d, want 400", resp.StatusCode)
	}
	if _, err := env.Store.Stat(context.Background(), "put/mismatch"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("stat after rejected put: %v, want ErrNotFound", err)
	}
}

// The checksum alone would accept any file that matches its own hash; the
// signed size stops a huge one being parked under Object Lock.
func TestPresignedPutEnforcesSize(t *testing.T) {
	body := []byte("twenty bytes of data")
	cases := map[string]int64{
		"more bytes than signed":  int64(len(body)) - 5,
		"fewer bytes than signed": int64(len(body)) + 5,
	}
	for name, signed := range cases {
		t.Run(name, func(t *testing.T) {
			key := "put/size-" + strings.ReplaceAll(name, " ", "-")
			p := presignSized(t, key, body, signed)
			if _, listed := p.Headers["Content-Length"]; listed {
				t.Errorf("Content-Length is listed in the headers %v; clients set it from the body", p.Headers)
			}
			if resp := upload(t, p, body, ""); resp.StatusCode < 400 || resp.StatusCode > 499 {
				t.Errorf("put of %d bytes to a URL signed for %d: status %d, want 4xx", len(body), signed, resp.StatusCode)
			}
			if _, err := env.Store.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("stat after refused put: %v, want ErrNotFound", err)
			}
		})
	}
}

func TestPresignedPutNeedsSignedHeaders(t *testing.T) {
	body := []byte("photo bytes")
	for _, h := range []string{"X-Amz-Checksum-Sha256", "If-None-Match", "Content-Type"} {
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

// The declared type is signed: an upload as anything else is refused, so a
// stored object can't carry a type the server never saw.
func TestPresignedPutRefusesOtherContentType(t *testing.T) {
	body := []byte("photo bytes")
	p := presign(t, "put/other-type", body)
	other := *p
	other.Headers = map[string]string{}
	for k, v := range p.Headers {
		other.Headers[k] = v
	}
	other.Headers["Content-Type"] = "text/html"

	// AWS answers 403 SignatureDoesNotMatch; either way nothing may be stored.
	if resp := upload(t, &other, body, ""); resp.StatusCode < 400 || resp.StatusCode > 499 {
		t.Errorf("put as text/html to a URL signed for image/jpeg: status %d, want 4xx", resp.StatusCode)
	}
	if _, err := env.Store.Stat(context.Background(), "put/other-type"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("stat after refused put: %v, want ErrNotFound", err)
	}

	sum := sha256.Sum256(body)
	if _, err := env.Store.PresignPut(context.Background(), "put/no-type", sum[:], int64(len(body)), "", time.Minute); !errors.Is(err, storage.ErrNoContentType) {
		t.Errorf("presign without a content type: %v, want ErrNoContentType", err)
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
func TestReadVersionOmitsEmptyVersion(t *testing.T) {
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
		if _, err := s.ReadVersion(context.Background(), "k", version); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("read version %q: %v, want ErrNotFound", version, err)
		}
		if got := (<-queries).Has("versionId"); got != want {
			t.Errorf("read version %q: versionId in query = %v, want %v", version, got, want)
		}
	}
}

// A view URL serves exactly the version it names, as the type the server
// verified, whatever Content-Type the uploader stored.
func TestPresignedGet(t *testing.T) {
	ctx := context.Background()
	const key = "get/versioned"
	put := func(body, contentType string) string {
		t.Helper()
		out, err := env.Admin.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(env.Config.Bucket), Key: aws.String(key),
			Body: strings.NewReader(body), ContentType: aws.String(contentType),
		})
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		return aws.ToString(out.VersionId)
	}
	// The first version is stored as HTML, as an uploader could arrange
	// (the presigned PUT doesn't sign Content-Type).
	first := put("first version", "text/html")
	put("second version", "image/png")

	before := time.Now()
	p, err := env.Store.PresignGet(ctx, key, first, "image/jpeg", time.Minute)
	if err != nil {
		t.Fatalf("presign get: %v", err)
	}
	if p.Method != http.MethodGet {
		t.Errorf("method %s, want GET", p.Method)
	}
	if p.ExpiresAt.Before(before.Add(time.Minute)) || p.ExpiresAt.After(time.Now().Add(time.Minute)) {
		t.Errorf("expires at %s, want about a minute from now", p.ExpiresAt)
	}
	u, err := url.Parse(p.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if got := u.Query().Get("X-Amz-Expires"); got != "60" {
		t.Errorf("X-Amz-Expires = %q, want 60", got)
	}

	resp, err := http.Get(p.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status %d, err %v", resp.StatusCode, err)
	}
	if string(body) != "first version" {
		t.Errorf("body %q, want the named version, not the latest", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type %q, want the forced image/jpeg, not the stored text/html", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "inline" {
		t.Errorf("Content-Disposition %q, want inline", cd)
	}

	if _, err := env.Store.PresignGet(ctx, key, "", "image/jpeg", time.Minute); !errors.Is(err, storage.ErrNoVersionID) {
		t.Errorf("no version: %v, want ErrNoVersionID", err)
	}
	if _, err := env.Store.PresignGet(ctx, key, first, "", time.Minute); !errors.Is(err, storage.ErrNoContentType) {
		t.Errorf("no content type: %v, want ErrNoContentType", err)
	}
}

func TestMissingObject(t *testing.T) {
	ctx := context.Background()
	if _, err := env.Store.Stat(ctx, "missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("stat: %v, want ErrNotFound", err)
	}
	if _, err := env.Store.ReadVersion(ctx, "missing", "00000000-0000-0000-0000-000000000000"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("read version: %v, want ErrNotFound", err)
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
