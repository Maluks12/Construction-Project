package evidence

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/platform/dbtest"
)

var admin *pgxpool.Pool // superuser: fixtures and the auditor's query

func TestMain(m *testing.M) {
	app, adm, cleanup, err := dbtest.Start(context.Background())
	if err != nil {
		log.Printf("start test database: %v", err)
		os.Exit(1)
	}
	app.Close()
	admin = adm

	code := m.Run()
	cleanup()
	os.Exit(code)
}

type vector struct {
	Seq              int64     `json:"seq"`
	SiteID           string    `json:"site_id"`
	EvidenceID       string    `json:"evidence_id"`
	ClientCapturedAt time.Time `json:"client_captured_at"`
	Lat              *float64  `json:"lat"`
	Lng              *float64  `json:"lng"`
	UploadedBy       string    `json:"uploaded_by"`
	DeviceID         *string   `json:"device_id"`
	MetadataPreimage string    `json:"metadata_preimage"`
	MetadataHash     string    `json:"metadata_hash"`
	ContentSHA256    string    `json:"content_sha256"`
	ServerReceivedAt time.Time `json:"server_received_at"`
	PrevChainHash    string    `json:"prev_chain_hash"`
	ChainPreimage    string    `json:"chain_preimage"`
	ChainHash        string    `json:"chain_hash"`
}

func (v vector) metadata() Metadata {
	return Metadata{
		EvidenceID:       v.EvidenceID,
		ClientCapturedAt: v.ClientCapturedAt,
		Lat:              v.Lat,
		Lng:              v.Lng,
		UploadedBy:       v.UploadedBy,
		DeviceID:         v.DeviceID,
	}
}

func (v vector) link(t *testing.T) Link {
	return Link{
		ContentSHA256:    unhex(t, v.ContentSHA256),
		ServerReceivedAt: v.ServerReceivedAt,
		SiteID:           v.SiteID,
		PrevChainHash:    unhex(t, v.PrevChainHash),
		MetadataHash:     unhex(t, v.MetadataHash),
	}
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return b
}

// block returns the fenced block that follows marker in CHAIN.md, so the
// document is the fixture and can't drift from the code.
func block(t *testing.T, marker string) string {
	t.Helper()
	doc, err := os.ReadFile("CHAIN.md")
	if err != nil {
		t.Fatalf("read CHAIN.md: %v", err)
	}
	_, rest, ok := strings.Cut(string(doc), "<!-- "+marker+" -->")
	if !ok {
		t.Fatalf("CHAIN.md has no %q marker", marker)
	}
	_, rest, ok = strings.Cut(rest, "```")
	if !ok {
		t.Fatalf("CHAIN.md: no fenced block after %q", marker)
	}
	_, rest, _ = strings.Cut(rest, "\n") // skip the language tag
	body, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatalf("CHAIN.md: unterminated block after %q", marker)
	}
	return strings.ReplaceAll(body, "\r\n", "\n")
}

func vectors(t *testing.T) []vector {
	t.Helper()
	var vs []vector
	if err := json.Unmarshal([]byte(block(t, "vectors")), &vs); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if len(vs) != 3 {
		t.Fatalf("got %d vectors, want 3", len(vs))
	}
	return vs
}

func TestVectors(t *testing.T) {
	prev := GenesisHash[:]
	for _, v := range vectors(t) {
		pre, err := encodeMetadata(v.metadata())
		if err != nil {
			t.Fatalf("seq %d: encode metadata: %v", v.Seq, err)
		}
		if got := hex.EncodeToString(pre); got != v.MetadataPreimage {
			t.Errorf("seq %d metadata preimage:\n got  %s\n want %s", v.Seq, got, v.MetadataPreimage)
		}
		mh, err := MetadataHash(v.metadata())
		if err != nil || hex.EncodeToString(mh[:]) != v.MetadataHash {
			t.Errorf("seq %d metadata hash = %x, err %v; want %s", v.Seq, mh, err, v.MetadataHash)
		}

		if !bytes.Equal(unhex(t, v.PrevChainHash), prev) {
			t.Errorf("seq %d prev_chain_hash = %s, want the previous chain hash %x", v.Seq, v.PrevChainHash, prev)
		}
		pre, err = encodeLink(v.link(t))
		if err != nil {
			t.Fatalf("seq %d: encode link: %v", v.Seq, err)
		}
		if got := hex.EncodeToString(pre); got != v.ChainPreimage {
			t.Errorf("seq %d chain preimage:\n got  %s\n want %s", v.Seq, got, v.ChainPreimage)
		}
		ch, err := ChainHash(v.link(t))
		if err != nil || hex.EncodeToString(ch[:]) != v.ChainHash {
			t.Errorf("seq %d chain hash = %x, err %v; want %s", v.Seq, ch, err, v.ChainHash)
		}
		prev = unhex(t, v.ChainHash)
	}
}

// No GPS (seq 2) and a fix at exactly (0, 0) (seq 3) differ only in the flag
// byte of the 17-byte location field.
func TestLocationFlag(t *testing.T) {
	vs := vectors(t)
	const start, end = 1 + 16 + 8, 1 + 16 + 8 + 17
	none, err := encodeMetadata(vs[1].metadata())
	if err != nil {
		t.Fatal(err)
	}
	zero, err := encodeMetadata(vs[2].metadata())
	if err != nil {
		t.Fatal(err)
	}
	a, b := none[start:end], zero[start:end]
	if a[0] != 0 || b[0] != 1 || !bytes.Equal(a[1:], b[1:]) {
		t.Errorf("location fields: no GPS %x, at (0, 0) %x; want them to differ only in the flag", a, b)
	}
}

func TestEveryFieldIsSealed(t *testing.T) {
	v := vectors(t)[0]
	f := func(x float64) *float64 { return &x }
	s := func(x string) *string { return &x }

	metaChanges := map[string]func(*Metadata){
		"evidence_id":           func(m *Metadata) { m.EvidenceID = "11111111-1111-4111-8111-111111111119" },
		"captured_at +1µs":      func(m *Metadata) { m.ClientCapturedAt = m.ClientCapturedAt.Add(time.Microsecond) },
		"lat only":              func(m *Metadata) { m.Lat = f(math.Nextafter(*m.Lat, 0)) },
		"lng only":              func(m *Metadata) { m.Lng = f(math.Nextafter(*m.Lng, 0)) },
		"no location":           func(m *Metadata) { m.Lat, m.Lng = nil, nil },
		"uploaded_by":           func(m *Metadata) { m.UploadedBy = "22222222-2222-4222-8222-222222222229" },
		"device_id":             func(m *Metadata) { m.DeviceID = s("pixel-7a-Ü002") },
		"no device_id":          func(m *Metadata) { m.DeviceID = nil },
		"lat and lng swapped":   func(m *Metadata) { m.Lat, m.Lng = m.Lng, m.Lat },
		"lat sign (-0 vs 0)":    func(m *Metadata) { m.Lat, m.Lng = f(math.Copysign(0, -1)), f(0) },
		"lng sign (0 vs -0)":    func(m *Metadata) { m.Lat, m.Lng = f(0), f(math.Copysign(0, -1)) },
		"device_id one byte":    func(m *Metadata) { m.DeviceID = s("x") },
		"device_id another one": func(m *Metadata) { m.DeviceID = s("y") },
	}
	seen := map[[32]byte]string{}
	base, err := MetadataHash(v.metadata())
	if err != nil {
		t.Fatal(err)
	}
	seen[base] = "base"
	for name, change := range metaChanges {
		m := v.metadata()
		change(&m)
		h, err := MetadataHash(m)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if other, dup := seen[h]; dup {
			t.Errorf("metadata %q hashes the same as %q", name, other)
		}
		seen[h] = name
	}

	linkChanges := map[string]func(*Link){
		"content_sha256":          func(l *Link) { l.ContentSHA256 = bytes.Repeat([]byte{1}, 32) },
		"server_received_at +1µs": func(l *Link) { l.ServerReceivedAt = l.ServerReceivedAt.Add(time.Microsecond) },
		"site_id":                 func(l *Link) { l.SiteID = "33333333-3333-4333-8333-333333333339" },
		"prev_chain_hash":         func(l *Link) { l.PrevChainHash = bytes.Repeat([]byte{1}, 32) },
		"metadata_hash":           func(l *Link) { l.MetadataHash = bytes.Repeat([]byte{1}, 32) },
	}
	baseLink, err := ChainHash(v.link(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range linkChanges {
		l := v.link(t)
		change(&l)
		h, err := ChainHash(l)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if h == baseLink {
			t.Errorf("changing %s doesn't change the chain hash", name)
		}
	}
}

func TestRejects(t *testing.T) {
	v := vectors(t)[0]
	f := func(x float64) *float64 { return &x }
	s := func(x string) *string { return &x }

	metaCases := map[string]func(*Metadata){
		"malformed evidence_id": func(m *Metadata) { m.EvidenceID = "not-a-uuid" },
		"malformed uploaded_by": func(m *Metadata) { m.UploadedBy = "" },
		"lat without lng":       func(m *Metadata) { m.Lng = nil },
		"lng without lat":       func(m *Metadata) { m.Lat = nil },
		"NaN":                   func(m *Metadata) { m.Lat = f(math.NaN()) },
		"+Inf":                  func(m *Metadata) { m.Lng = f(math.Inf(1)) },
		"empty device_id":       func(m *Metadata) { m.DeviceID = s("") },
		"device_id too long":    func(m *Metadata) { m.DeviceID = s(strings.Repeat("a", 201)) },
		"device_id not UTF-8":   func(m *Metadata) { m.DeviceID = s("\xff") },
		"nanosecond time":       func(m *Metadata) { m.ClientCapturedAt = m.ClientCapturedAt.Add(time.Nanosecond) },
	}
	for name, change := range metaCases {
		m := v.metadata()
		change(&m)
		if _, err := MetadataHash(m); err == nil {
			t.Errorf("metadata with %s: want an error", name)
		}
	}
	m := v.metadata()
	m.DeviceID = s(strings.Repeat("a", 200))
	if _, err := MetadataHash(m); err != nil {
		t.Errorf("200-byte device_id: %v, want it accepted", err)
	}

	linkCases := map[string]func(*Link){
		"short content_sha256": func(l *Link) { l.ContentSHA256 = l.ContentSHA256[:31] },
		"nil prev_chain_hash":  func(l *Link) { l.PrevChainHash = nil },
		"long metadata_hash":   func(l *Link) { l.MetadataHash = append(l.MetadataHash, 0) },
		"malformed site_id":    func(l *Link) { l.SiteID = "site" },
		"nanosecond time":      func(l *Link) { l.ServerReceivedAt = l.ServerReceivedAt.Add(time.Nanosecond) },
	}
	for name, change := range linkCases {
		l := v.link(t)
		change(&l)
		if _, err := ChainHash(l); err == nil {
			t.Errorf("link with %s: want an error", name)
		}
	}
}

type sqlCheck struct {
	seq                                int64
	evidenceID                         string
	seqOK, prevOK, metadataOK, chainOK bool
}

func recompute(t *testing.T, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, siteID string) []sqlCheck {
	t.Helper()
	rows, err := db.Query(context.Background(), block(t, "recompute-sql"), siteID)
	if err != nil {
		t.Fatalf("recompute query: %v", err)
	}
	got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (sqlCheck, error) {
		var c sqlCheck
		err := r.Scan(&c.seq, &c.evidenceID, &c.seqOK, &c.prevOK, &c.metadataOK, &c.chainOK)
		return c, err
	})
	if err != nil {
		t.Fatalf("recompute query: %v", err)
	}
	return got
}

// The SQL in CHAIN.md must accept the vectors as stored by the real schema,
// and must notice a sealed field being edited or an entry being deleted.
func TestRecomputeSQL(t *testing.T) {
	ctx := context.Background()
	vs := vectors(t)
	const org = "44444444-4444-4444-8444-444444444444"
	site, user := vs[0].SiteID, vs[0].UploadedBy

	fixtures := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'Vectors', 'construction', ARRAY['construction'])`, []any{org}},
		{`INSERT INTO users (id, email, full_name) VALUES ($1, 'vectors@chain.test', 'Vectors')`, []any{user}},
		{`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, []any{org, user}},
		{`INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Vectors' FROM regulatory_regimes LIMIT 1`, []any{site, org}},
	}
	for _, v := range vs {
		fixtures = append(fixtures,
			struct {
				sql  string
				args []any
			}{`INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes, content_type, client_captured_at, lat, lng, device_id, object_key, object_version_id, retain_until, status)
			   VALUES ($1, $2, $3, $4, $5, 1, 'image/jpeg', $6, $7, $8, $9, $10, 'v1', now() + interval '1 day', 'verified')`,
				[]any{v.EvidenceID, org, site, user, unhex(t, v.ContentSHA256), v.ClientCapturedAt, v.Lat, v.Lng, v.DeviceID, "vectors/" + v.EvidenceID}},
			struct {
				sql  string
				args []any
			}{`INSERT INTO evidence_chain (evidence_id, org_id, site_id, seq, content_sha256, server_received_at, prev_chain_hash, metadata_hash, chain_hash)
			   VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				[]any{v.EvidenceID, org, site, v.Seq, unhex(t, v.ContentSHA256), v.ServerReceivedAt, unhex(t, v.PrevChainHash), unhex(t, v.MetadataHash), unhex(t, v.ChainHash)}},
		)
	}
	for _, f := range fixtures {
		if _, err := admin.Exec(ctx, f.sql, f.args...); err != nil {
			t.Fatalf("fixture %q: %v", f.sql, err)
		}
	}

	t.Run("intact chain", func(t *testing.T) {
		got := recompute(t, admin, site)
		if len(got) != 3 {
			t.Fatalf("got %d rows, want 3", len(got))
		}
		for _, c := range got {
			if !c.seqOK || !c.prevOK || !c.metadataOK || !c.chainOK {
				t.Errorf("seq %d: %+v, want every check true", c.seq, c)
			}
		}
	})

	// Rolled back. Runs as a superuser with the freeze trigger switched off, to
	// show the chain still catches what the database's own guards would stop.
	tamper := func(t *testing.T, stmt string) []sqlCheck {
		t.Helper()
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		if _, err := tx.Exec(ctx, `ALTER TABLE evidence DISABLE TRIGGER evidence_freeze`); err != nil {
			t.Fatalf("disable trigger: %v", err)
		}
		if _, err := tx.Exec(ctx, stmt, vs[1].EvidenceID); err != nil {
			t.Fatalf("tamper %q: %v", stmt, err)
		}
		return recompute(t, tx, site)
	}

	t.Run("edited capture time", func(t *testing.T) {
		got := tamper(t, `UPDATE evidence SET client_captured_at = client_captured_at + interval '1 microsecond' WHERE id = $1`)
		for _, c := range got {
			if want := c.seq != 2; c.metadataOK != want || !c.seqOK || !c.prevOK || !c.chainOK {
				t.Errorf("seq %d: %+v, want only seq 2's metadata_ok false", c.seq, c)
			}
		}
	})

	// The known limit (CHAIN.md): cutting entries off the end leaves a chain
	// that still verifies. Only an external record of the head catches it.
	t.Run("truncated chain still verifies", func(t *testing.T) {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `DELETE FROM evidence_chain WHERE site_id = $1 AND seq = 3`, site); err != nil {
			t.Fatal(err)
		}
		got := recompute(t, tx, site)
		if len(got) != 2 {
			t.Fatalf("got %d rows, want 2", len(got))
		}
		for _, c := range got {
			if !c.seqOK || !c.prevOK || !c.metadataOK || !c.chainOK {
				t.Errorf("seq %d: %+v; truncation was expected to go unnoticed here", c.seq, c)
			}
		}
	})

	t.Run("deleted entry", func(t *testing.T) {
		got := tamper(t, `DELETE FROM evidence_chain WHERE evidence_id = $1`)
		if len(got) != 2 || got[1].seq != 3 || got[1].seqOK || got[1].prevOK {
			t.Errorf("after deleting seq 2: %+v, want seq 3 to fail seq_ok and prev_ok", got)
		}
	})
}
