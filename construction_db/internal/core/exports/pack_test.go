package exports

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"
	"time"

	"construction_db/internal/core/evidence"
)

// pattern yields n bytes of a fixed sequence, fast, without holding them.
type pattern struct{ n, off int64 }

var patternBlock = func() []byte {
	b := make([]byte, 251*4096)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}()

func (p *pattern) Read(b []byte) (int, error) {
	if p.off >= p.n {
		return 0, io.EOF
	}
	k := 0
	for k < len(b) && p.off < p.n {
		start := p.off % int64(len(patternBlock))
		c := copy(b[k:], patternBlock[start:min(int64(len(patternBlock)), start+p.n-p.off)])
		k += c
		p.off += int64(c)
	}
	return k, nil
}

// tail keeps the last bytes written.
type tail struct{ b []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 512 {
		t.b = t.b[len(t.b)-512:]
	}
	return len(p), nil
}

func testPack(t *testing.T, sizes ...int64) (*packDocs, []packFile, Opener) {
	t.Helper()
	m := &Manifest{Format: PackFormat, Export: ManifestExport{ID: "x", AsOf: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)},
		Notes: []string{"a note"}, Organisation: json.RawMessage(`{}`), Site: json.RawMessage(`{"name":"Big site"}`),
		Members: json.RawMessage(`[]`), Sections: []Section{}, Chain: ManifestChain{Entries: []evidence.ChainRecord{}}}
	var files []packFile
	byKey := map[string]int64{}
	for i, n := range sizes {
		h := sha256.New()
		if _, err := io.Copy(h, &pattern{n: n}); err != nil {
			t.Fatal(err)
		}
		f := packFile{ManifestFile: ManifestFile{Path: "files/documents/" + string(rune('a'+i)) + ".pdf", Kind: "document",
			ID: string(rune('a' + i)), SHA256: hex.EncodeToString(h.Sum(nil)), SizeBytes: n, ContentType: "application/pdf"},
			key: string(rune('a' + i)), version: "v1"}
		files = append(files, f)
		m.Files = append(m.Files, f.ManifestFile)
		byKey[f.key] = n
	}
	d, err := renderDocs(m)
	if err != nil {
		t.Fatal(err)
	}
	open := func(_ context.Context, key, _ string) (io.ReadCloser, error) {
		return io.NopCloser(&pattern{n: byKey[key]}), nil
	}
	return d, files, open
}

func buildPack(t *testing.T, d *packDocs, files []packFile, open Opener) (string, int64, []byte) {
	t.Helper()
	h, cw, end := sha256.New(), &countingWriter{}, &tail{}
	if err := writePack(context.Background(), io.MultiWriter(h, cw, end), d, files, open); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil)), cw.n, end.b
}

// A pack over 4 GiB (ZIP64) is the same bytes on every build, and its
// estimate is an upper bound (M12 review).
func TestPackOver4GiBIsDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 4 GiB twice")
	}
	d, files, open := testPack(t, 4<<30+64<<20, 1000)
	sum1, n1, end := buildPack(t, d, files, open)
	sum2, n2, _ := buildPack(t, d, files, open)
	if sum1 != sum2 || n1 != n2 {
		t.Fatalf("two builds: %s (%d bytes) and %s (%d bytes)", sum1, n1, sum2, n2)
	}
	if n1 <= 4<<30 || !bytes.Contains(end, []byte{0x50, 0x4b, 0x06, 0x06}) {
		t.Errorf("%d bytes, ZIP64 end record present: %v", n1, bytes.Contains(end, []byte{0x50, 0x4b, 0x06, 0x06}))
	}
	if est := estimateSize(d, files); est < n1 {
		t.Errorf("estimate %d is below the real %d", est, n1)
	}
}

// The estimate bounds small packs too, where headers weigh most.
func TestEstimateBoundsSmallPacks(t *testing.T) {
	for _, sizes := range [][]int64{{}, {1}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {100 << 10}} {
		d, files, open := testPack(t, sizes...)
		_, n, _ := buildPack(t, d, files, open)
		if est := estimateSize(d, files); est < n {
			t.Errorf("%v: estimate %d is below the real %d", sizes, est, n)
		}
	}
}
