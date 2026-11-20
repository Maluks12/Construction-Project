package exports

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"slices"
	"strings"
	"time"

	"construction_db/internal/core/evidence"
)

// Manifest is manifest.json, canonical so the same data gives the same bytes.
type Manifest struct {
	Format       int             `json:"format"`
	Export       ManifestExport  `json:"export"`
	Notes        []string        `json:"notes"`
	Organisation json.RawMessage `json:"organisation"`
	Site         json.RawMessage `json:"site"`
	Members      json.RawMessage `json:"members"`
	Sections     []Section       `json:"sections"`
	Chain        ManifestChain   `json:"chain"`
	Files        []ManifestFile  `json:"files"`
}

type ManifestExport struct {
	ID                string    `json:"id"`
	SiteID            string    `json:"site_id"`
	From              string    `json:"from"`
	To                string    `json:"to"`
	TimeZone          string    `json:"time_zone"`
	AsOf              time.Time `json:"as_of"`
	RequestedBy       string    `json:"requested_by"`
	RequesterRole     string    `json:"requester_role"`
	IncludesHealth    bool      `json:"includes_health"`
	PackFormat        int       `json:"pack_format"`
	RegimeCode        string    `json:"regime_code"`
	RegimeVersion     string    `json:"regime_version"`
	RegimeDataSHA256  string    `json:"regime_data_sha256"`
	ClientRequestedAt time.Time `json:"client_requested_at"`
}

// ManifestChain lets anyone recompute the chain (CHAIN.md) and records its head outside the database.
type ManifestChain struct {
	Verification evidence.ChainVerification `json:"verification"`
	Entries      []evidence.ChainRecord     `json:"entries"`
}

type ManifestFile struct {
	Path        string `json:"path"`
	Kind        string `json:"kind"` // evidence or document
	ID          string `json:"id"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentType string `json:"content_type"`
}

type packFile struct {
	ManifestFile
	key, version string
}

// Opener reads exactly one stored version of an object.
type Opener func(ctx context.Context, key, versionID string) (io.ReadCloser, error)

var extensions = map[string]string{"image/jpeg": "jpg", "image/png": "png", "application/pdf": "pdf"}

func filePath(kind, id, contentType string) string {
	ext, ok := extensions[contentType]
	if !ok {
		ext = "bin"
	}
	dir := map[string]string{"evidence": "files/evidence/", "document": "files/documents/"}[kind]
	return dir + id + "." + ext
}

// writePack stores entries uncompressed (deflate output may change between Go
// versions), in name order, each dated as_of.
func writePack(ctx context.Context, w io.Writer, m *Manifest, files []packFile, open Opener) error {
	zw := zip.NewWriter(w)
	modified := m.Export.AsOf.UTC().Truncate(time.Second)
	entry := func(name string) (io.Writer, error) {
		return zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: modified})
	}
	put := func(name string, body []byte) error {
		fw, err := entry(name)
		if err != nil {
			return err
		}
		_, err = fw.Write(body)
		return err
	}

	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("write pack: manifest: %w", err)
	}
	index, err := renderIndex(m)
	if err != nil {
		return fmt.Errorf("write pack: index: %w", err)
	}
	// "CHAIN.md" < "README.txt" < "files/" < "index.html" < "manifest.json".
	if err := put("CHAIN.md", []byte(evidence.ChainSpec())); err != nil {
		return fmt.Errorf("write pack: %w", err)
	}
	if err := put("README.txt", []byte(readme(m))); err != nil {
		return fmt.Errorf("write pack: %w", err)
	}
	slices.SortFunc(files, func(a, b packFile) int { return strings.Compare(a.Path, b.Path) })
	for _, f := range files {
		if err := copyFile(ctx, entry, f, open); err != nil {
			return fmt.Errorf("write pack: %w", err)
		}
	}
	if err := put("index.html", index); err != nil {
		return fmt.Errorf("write pack: %w", err)
	}
	if err := put("manifest.json", append(manifest, '\n')); err != nil {
		return fmt.Errorf("write pack: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("write pack: %w", err)
	}
	return nil
}

var ErrSealMismatch = errors.New("stored bytes don't match their seal")

// SealError names the stored object whose bytes no longer match its seal.
type SealError struct {
	Kind, ID, Key, Version string
}

func (e *SealError) Error() string {
	return fmt.Sprintf("%s %s (object %s, version %s): %v", e.Kind, e.ID, e.Key, e.Version, ErrSealMismatch)
}

func (e *SealError) Unwrap() error { return ErrSealMismatch }

func copyFile(ctx context.Context, entry func(string) (io.Writer, error), f packFile, open Opener) error {
	rc, err := open(ctx, f.key, f.version)
	if err != nil {
		return fmt.Errorf("%s %s: %w", f.Kind, f.ID, err)
	}
	defer rc.Close()
	fw, err := entry(f.Path)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(fw, h), rc)
	if err != nil {
		return fmt.Errorf("%s %s: %w", f.Kind, f.ID, err)
	}
	if n != f.SizeBytes || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return &SealError{Kind: f.Kind, ID: f.ID, Key: f.key, Version: f.version}
	}
	return nil
}

func readme(m *Manifest) string {
	var b strings.Builder
	e := m.Export
	fmt.Fprintf(&b, "SiteProof audit pack\r\n====================\r\n\r\n")
	fmt.Fprintf(&b, "Export %s, site %s, %s to %s (local dates, %s).\r\n", e.ID, e.SiteID, e.From, e.To, e.TimeZone)
	fmt.Fprintf(&b, "As of %s UTC: records the server had received by then. Nothing received later is in this pack.\r\n\r\n",
		e.AsOf.UTC().Format("2006-01-02 15:04:05.000000"))
	b.WriteString("What is in it\r\n-------------\r\n")
	b.WriteString("CHAIN.md       how the evidence chain is computed, to the byte, with test vectors.\r\n")
	b.WriteString("index.html     the records, readable in a browser, in the order an inspector asks for them.\r\n")
	b.WriteString("manifest.json  the same records as data, the evidence chain and every file's SHA-256.\r\n")
	b.WriteString("files/         the original photos and documents, exactly as they were sealed.\r\n\r\n")
	for _, n := range m.Notes {
		b.WriteString("- ")
		b.WriteString(n)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\nHow to check it\r\n---------------\r\n")
	b.WriteString("1. The SHA-256 of this ZIP file is recorded with the export in SiteProof. Compute it\r\n")
	b.WriteString("   (sha256sum, certutil -hashfile <file> SHA256) and compare: a match means not one byte changed.\r\n")
	b.WriteString("2. Each file in files/ has its SHA-256 in manifest.json (\"files\"). It was checked against the\r\n")
	b.WriteString("   hash sealed when the server received it, as it was written into this pack.\r\n")
	b.WriteString("3. manifest.json \"chain\" lists every entry of the site's evidence chain with every input of its\r\n")
	b.WriteString("   hash. The formulas, byte encoding and test vectors are in CHAIN.md in this pack:\r\n")
	b.WriteString("   chain_hash = SHA-256(content_sha256 || server_received_at || site_id || prev_chain_hash || metadata_hash).\r\n")
	b.WriteString("   The chain's head hash and length on this date are a record outside SiteProof's database: a later\r\n")
	b.WriteString("   chain that no longer contains this head, or is shorter, has lost entries.\r\n\r\n")
	b.WriteString("The chain makes changes detectable; it does not make them impossible. This pack is built to the\r\n")
	b.WriteString("reliability factors in section 15 of the Electronic Communications and Transactions Act 25 of 2002.\r\n")
	return b.String()
}

// renderIndex: self-contained, every value escaped, maps by key.
func renderIndex(m *Manifest) ([]byte, error) {
	var b bytes.Buffer
	esc := html.EscapeString
	e := m.Export
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><title>Audit pack ")
	b.WriteString(esc(e.ID))
	b.WriteString("</title>\n<style>body{font:14px/1.45 system-ui,sans-serif;margin:24px;max-width:1100px;color:#111}" +
		"h1{font-size:22px}h2{font-size:18px;border-bottom:1px solid #999;margin-top:32px}" +
		"dl{margin:4px 0 4px 12px}dt{font-weight:600;float:left;clear:left;width:200px}dd{margin-left:210px}" +
		".rec{border:1px solid #ccc;border-radius:4px;padding:8px;margin:8px 0;break-inside:avoid}" +
		"code{font-size:12px;word-break:break-all}</style></head><body>\n")
	fmt.Fprintf(&b, "<h1>Audit pack</h1>\n<p>Site %s, %s to %s (local dates, %s). As of %s UTC.</p>\n",
		esc(siteName(m)), esc(e.From), esc(e.To), esc(e.TimeZone), esc(e.AsOf.UTC().Format("2006-01-02 15:04:05")))
	b.WriteString("<ul>\n")
	for _, n := range m.Notes {
		b.WriteString("<li>" + esc(n) + "</li>\n")
	}
	b.WriteString("</ul>\n")

	write := func(title string, raw json.RawMessage) error {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		b.WriteString("<h2>" + esc(title) + "</h2>\n")
		if list, ok := v.([]any); ok {
			if len(list) == 0 {
				b.WriteString("<p>None.</p>\n")
			}
			for _, item := range list {
				b.WriteString("<div class=\"rec\">")
				renderValue(&b, item)
				b.WriteString("</div>\n")
			}
			return nil
		}
		renderValue(&b, v)
		return nil
	}
	exportJSON, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	for _, s := range []struct {
		title string
		raw   json.RawMessage
	}{{"The export", exportJSON}, {"Organisation", m.Organisation}, {"Site and regime", m.Site}, {"People (names and roles at the cut-off)", m.Members}} {
		if err := write(s.title, s.raw); err != nil {
			return nil, err
		}
	}
	for _, s := range m.Sections {
		if s.Key == "received_photos" {
			continue
		}
		if err := write(s.Title, s.Records); err != nil {
			return nil, err
		}
	}
	chain, err := json.Marshal(m.Chain.Verification)
	if err != nil {
		return nil, err
	}
	if err := write("Evidence chain verification", chain); err != nil {
		return nil, err
	}
	entries, err := json.Marshal(m.Chain.Entries)
	if err != nil {
		return nil, err
	}
	if err := write("Evidence chain entries", entries); err != nil {
		return nil, err
	}
	b.WriteString("<h2>Files</h2>\n<ul>\n")
	for _, f := range m.Files {
		fmt.Fprintf(&b, "<li><a href=\"%s\">%s</a> <code>%s</code></li>\n", esc(f.Path), esc(f.Path), esc(f.SHA256))
	}
	b.WriteString("</ul>\n</body></html>\n")
	return b.Bytes(), nil
}

func siteName(m *Manifest) string {
	var s struct{ Name string }
	_ = json.Unmarshal(m.Site, &s)
	return s.Name
}

func renderValue(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		b.WriteString("<dl>")
		for _, k := range keys {
			b.WriteString("<dt>" + html.EscapeString(k) + "</dt><dd>")
			renderValue(b, x[k])
			b.WriteString("</dd>")
		}
		b.WriteString("</dl>")
	case []any:
		if len(x) == 0 {
			b.WriteString("none")
			return
		}
		b.WriteString("<ol>")
		for _, item := range x {
			b.WriteString("<li>")
			renderValue(b, item)
			b.WriteString("</li>")
		}
		b.WriteString("</ol>")
	case nil:
		b.WriteString("—")
	case string:
		b.WriteString(html.EscapeString(x))
	default:
		raw, _ := json.Marshal(x)
		b.WriteString(html.EscapeString(string(raw)))
	}
}
