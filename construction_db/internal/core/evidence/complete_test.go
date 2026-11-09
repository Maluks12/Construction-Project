package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math/rand"
	"testing"
	"testing/iotest"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/platform/storage"
)

func encodeImage(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	// Noise, so the file is far larger than any read buffer.
	rand.New(rand.NewSource(1)).Read(img.Pix)
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withAppSegments inserts n APP2 segments of size bytes each after the JPEG's
// start marker, the way EXIF or ICC metadata pushes the frame header back.
func withAppSegments(jpg []byte, n, size int) []byte {
	out := append([]byte{}, jpg[:2]...)
	for range n {
		seg := []byte{0xFF, 0xE2, byte((size + 2) >> 8), byte(size + 2)}
		out = append(append(out, seg...), make([]byte, size)...)
	}
	return append(out, jpg[2:]...)
}

// flipByte returns a copy with one byte changed: same length, other bytes.
func flipByte(b []byte) []byte {
	out := append([]byte{}, b...)
	out[len(out)/2] ^= 0xFF
	return out
}

func declared(b []byte, contentType string) *Evidence {
	sum := sha256.Sum256(b)
	return &Evidence{SizeBytes: int64(len(b)), ContentSHA256: hex.EncodeToString(sum[:]), ContentType: contentType}
}

func TestInspect(t *testing.T) {
	big := encodeImage(t, "jpeg", 600, 400)
	// Several times bufio's 4 KiB buffer, so the drain after decoding matters.
	if len(big) <= 64<<10 {
		t.Fatalf("fixture is %d bytes, want more than 64 KiB", len(big))
	}
	small := encodeImage(t, "png", 2048, 1)
	tooWide := encodeImage(t, "jpeg", 2049, 1)
	tooTall := encodeImage(t, "jpeg", 1, 2049)

	tests := []struct {
		name   string
		stored []byte
		ev     *Evidence
		want   string // "" passes
	}{
		{"jpeg much larger than the read buffer", big, declared(big, "image/jpeg"), ""},
		{"png at exactly 2048px", small, declared(small, "image/png"), ""},
		{"one byte longer than declared", append(append([]byte{}, big...), 0), declared(big, "image/jpeg"), ReasonSizeMismatch},
		{"same length, other bytes", flipByte(big), declared(big, "image/jpeg"), ReasonHashMismatch},
		{"not an image", []byte("photo bytes"), declared([]byte("photo bytes"), "image/jpeg"), ReasonNotAnImage},
		{"png declared as jpeg", small, declared(small, "image/jpeg"), ReasonWrongType},
		{"2049px wide", tooWide, declared(tooWide, "image/jpeg"), ReasonTooLargeDimensions},
		{"2049px tall", tooTall, declared(tooTall, "image/jpeg"), ReasonTooLargeDimensions},
		{"over 10 MiB", make([]byte, MaxSizeBytes+1), &Evidence{SizeBytes: MaxSizeBytes, ContentType: "image/jpeg"}, ReasonSizeMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rej, err := inspect(bytes.NewReader(tt.stored), tt.ev)
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if rej != nil {
				got = rej.Reason
			}
			if got != tt.want {
				t.Errorf("rejection %q, want %q", got, tt.want)
			}
		})
	}
}

// EXIF and ICC blocks (up to 64 KiB each) come before a JPEG's frame header.
// Reading the header from the stream itself means no amount of metadata can
// push it out of reach.
func TestInspectHeaderLimit(t *testing.T) {
	jpg := withAppSegments(encodeImage(t, "jpeg", 100, 80), 5, 60<<10)
	if len(jpg) < 300<<10 {
		t.Fatalf("fixture has %d bytes, want 300 KiB of metadata", len(jpg))
	}
	rej, err := inspect(bytes.NewReader(jpg), declared(jpg, "image/jpeg"))
	if err != nil || rej != nil {
		t.Errorf("a JPEG with 300 KiB of APP segments before the image header: rejection %+v, err %v; want accepted", rej, err)
	}
}

// A read that fails partway is a storage problem: an error, so the evidence
// stays pending, never a rejection that would burn its id.
func TestInspectReadErrorIsNotARejection(t *testing.T) {
	jpg := encodeImage(t, "jpeg", 600, 400)
	reset := errors.New("connection reset")
	for name, r := range map[string]io.Reader{
		// Seen again by the drain after decoding.
		"fails from halfway on": io.MultiReader(bytes.NewReader(jpg[:len(jpg)/2]), iotest.ErrReader(reset)),
		// Fails once inside the header, where the decoder swallows it, then
		// ends cleanly: only the remembered error tells this from a short file.
		"fails once in the header": io.MultiReader(bytes.NewReader(jpg[:100]), &failOnce{err: reset}),
	} {
		t.Run(name, func(t *testing.T) {
			rej, err := inspect(r, declared(jpg, "image/jpeg"))
			if !errors.Is(err, reset) || rej != nil {
				t.Errorf("rejection %+v, err %v; want the read error and no rejection", rej, err)
			}
		})
	}
}

// failOnce returns err on its first read and io.EOF after that.
type failOnce struct {
	err  error
	done bool
}

func (f *failOnce) Read([]byte) (int, error) {
	if f.done {
		return 0, io.EOF
	}
	f.done = true
	return 0, f.err
}

// Two completes that both pass the S3 checks race into appendToChain. The
// loser must find the evidence sealed under the lock and return that entry,
// not append a second one. Calling appendToChain twice reaches that branch
// every time; racing HTTP requests only reach it when they happen to overlap.
func TestAppendToChainTwice(t *testing.T) {
	ctx := context.Background()
	// Fresh ids each run, so -count=N works against the shared database.
	org, user, site, id := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, f := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'Twice', 'construction', ARRAY['construction'])`, []any{org}},
		{`INSERT INTO users (id, email, full_name) VALUES ($1, $2, 'Twice')`, []any{user, "twice-" + user + "@chain.test"}},
		{`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, []any{org, user}},
		{`INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Twice' FROM regulatory_regimes LIMIT 1`, []any{site, org}},
		{`INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes, content_type, client_captured_at, object_key)
		  VALUES ($1, $2, $3, $4, $5, 1, 'image/jpeg', '2026-11-06T08:00:00Z', $6)`, []any{id, org, site, user, bytes.Repeat([]byte{7}, 32), "twice/" + id}},
	} {
		if _, err := admin.Exec(ctx, f.sql, f.args...); err != nil {
			t.Fatalf("fixture %q: %v", f.sql, err)
		}
	}

	s := &Service{pool: appPool}
	retain := time.Now().Add(24 * time.Hour)
	info := &storage.ObjectInfo{VersionID: "v1", LockMode: storage.LockGovernance, RetainUntil: &retain}

	first, created, err := s.appendToChain(ctx, org, user, id, info)
	if err != nil || !created || first.Chain == nil || first.Chain.Seq != 1 {
		t.Fatalf("first append: %+v, created %v, err %v; want seq 1", first, created, err)
	}
	second, created, err := s.appendToChain(ctx, org, user, id, info)
	if err != nil {
		t.Fatalf("second append: %v; want the first entry back", err)
	}
	if created || second.Chain == nil || *second.Chain != *first.Chain {
		t.Errorf("second append: chain %+v, created %v; want the first entry, not created", second.Chain, created)
	}

	var entries, headSeq int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM evidence_chain WHERE site_id = $1), (SELECT seq FROM site_chain_heads WHERE site_id = $1)`, site).Scan(&entries, &headSeq); err != nil {
		t.Fatal(err)
	}
	if entries != 1 || headSeq != 1 {
		t.Errorf("site has %d entries and head seq %d, want 1 and 1", entries, headSeq)
	}
}
