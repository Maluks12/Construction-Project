package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/evidence"
	"construction_db/internal/core/exports"
	"construction_db/internal/core/files"
	"construction_db/internal/core/notifications"
	"construction_db/internal/editions/construction"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/storage"
)

func exportPath(site, id string) string { return "/v1/sites/" + site + "/exports/" + id }

// newBuilder is cmd/worker's builder with a millisecond settle window.
func newBuilder(lease time.Duration) *exports.Builder {
	return &exports.Builder{Pool: testPool, Evidence: testStore, Exports: testExports,
		Contributor: construction.New().ExportsContributor(), Instance: "test-" + uuid.NewString()[:8],
		Settle: time.Millisecond, Lease: lease, Backoff: time.Millisecond}
}

func exportBody(from, to string) map[string]any {
	return map[string]any{"from": from, "to": to, "client_requested_at": rfc(testClock.Now())}
}

func decodeExport(t *testing.T, rec interface{ Bytes() []byte }) exports.Export {
	t.Helper()
	var e exports.Export
	if err := json.Unmarshal(rec.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func queueExport(t *testing.T, f evidenceFixture, site string, body map[string]any) exports.Export {
	t.Helper()
	rec := do(t, f.router, http.MethodPut, exportPath(site, uuid.NewString()), f.owner, body)
	wantStatus(t, rec, http.StatusCreated)
	return decodeExport(t, rec.Body)
}

func getExportStatus(t *testing.T, router http.Handler, token, site, id string) exports.Export {
	t.Helper()
	rec := do(t, router, http.MethodGet, exportPath(site, id), token, nil)
	wantStatus(t, rec, http.StatusOK)
	return decodeExport(t, rec.Body)
}

// claim claims the next export of org, which must be want.
func claim(t *testing.T, b *exports.Builder, org, want string) *exports.Attempt {
	t.Helper()
	time.Sleep(5 * time.Millisecond) // past the settle window
	a, err := b.Claim(context.Background(), org)
	if err != nil || a == nil || a.ExportID != want {
		t.Fatalf("claim: got %+v, err %v; want export %s", a, err, want)
	}
	return a
}

func build(t *testing.T, b *exports.Builder, org, id string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := b.BuildTo(context.Background(), org, id, &buf); err != nil {
		t.Fatalf("build: %v", err)
	}
	return buf.Bytes()
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func readManifest(t *testing.T, pack []byte) (exports.Manifest, map[string][]byte, []string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(pack), int64(len(pack)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	var names []string
	for _, f := range zr.File {
		if f.Method != zip.Store {
			t.Errorf("%s is compressed", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name] = b
		names = append(names, f.Name)
	}
	var m exports.Manifest
	if err := json.Unmarshal(entries["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	return m, entries, names
}

// exportFixture: a site with a record in every section of the pack.
type exportFixture struct {
	actionFixture
	worker, talk, incident, medical string
	action, closeout, lgs, open     string // open: an inspection still in progress
	siteWork, annexure2             string // REG4: work starting today, its notice met with the Annexure 2
	talkStart                       time.Time
}

func newExportFixture(t *testing.T, name string) exportFixture {
	t.Helper()
	f := exportFixture{actionFixture: newActionFixture(t, name)}
	r, clk := f.router, newSASTClock(t)

	// A corrective action, closed out with a photo.
	f.action = f.raise(t, f.owner, f.raiseBody(f.ownerID, testClock.Now().Add(-time.Minute))).ID
	f.closeout = uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, closeoutPath(f.siteA, f.action, f.closeout), f.owner,
		closeoutBody(testClock.Now(), f.fixPhoto)), http.StatusCreated)
	// An inspection still in progress.
	f.open = startInspection(t, r, f.supervisor, f.siteA, f.tmpl)
	// Someone signs in, with a medical and a licence.
	f.worker = uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor,
		signInBody(f.worker, clk.Earlier(20*time.Minute), map[string]any{"new_worker": map[string]any{"name": "Sipho Dlamini"}})), http.StatusCreated)
	medFile := uploadFile(t, r, f.supervisor, files.PurposeMedical, nil)
	f.medical = uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, workerPath(f.worker)+"/medicals/"+f.medical, f.supervisor,
		medicalBody(clk, medFile, "fit", clk.Today(), clk.Day(300))), http.StatusCreated)
	licence := uploadFile(t, r, f.supervisor, files.PurposeCompetency, nil)
	wantStatus(t, do(t, r, http.MethodPut, workerPath(f.worker)+"/competencies/"+uuid.NewString(), f.supervisor,
		competencyBody(clk, licence, clk.Today(), clk.Day(200))), http.StatusCreated)
	// A toolbox talk with a group photo and a signature.
	f.talk, f.talkStart = uuid.NewString(), clk.Earlier(30*time.Minute)
	wantStatus(t, do(t, r, http.MethodPut, talkPath(f.siteA, f.talk), f.supervisor,
		talkBody(map[string]any{"started_at": rfc(f.talkStart)})), http.StatusCreated)
	wantStatus(t, do(t, r, http.MethodPut, talkPhotoPath(f.siteA, f.talk, f.photo2), f.supervisor, map[string]any{"kind": "photo"}), http.StatusCreated)
	wantStatus(t, do(t, r, http.MethodPut, signPath(f.siteA, f.talk, uuid.NewString()), f.supervisor,
		signBody("Sipho Dlamini", clk.Earlier(10*time.Minute), nil)), http.StatusCreated)
	// Today's diary.
	wantStatus(t, do(t, r, http.MethodPut, revisionPath(f.siteA, clk.Today(), uuid.NewString()), f.supervisor,
		revisionBody(clk.Earlier(5*time.Minute), nil)), http.StatusCreated)
	// An incident: someone knocked unconscious.
	f.incident = uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, versionPath(f.siteA, f.incident, 1), f.supervisor,
		with(with(versionBody(clk.Earlier(15*time.Minute), "unconsciousness"), "employer_aware_at", rfc(clk.Earlier(15*time.Minute))), "people", []map[string]any{{
			"name": "Thabo Injured", "employee_of_org": true, "injured": true, "injury_description": "Knocked out by a falling plank",
			"body_part": "Head"}})), http.StatusCreated)
	// The vault, the H&S file and an appointment.
	lgs := uploadFile(t, r, f.owner, files.PurposeOrgDocument, nil)
	f.lgs = uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, vaultPath(f.lgs), f.owner, map[string]any{
		"regime_code": "za_construction_2014", "type_key": "coida_letter_of_good_standing", "title": "LGS 2026",
		"version": versionReq(lgs, map[string]any{"issuer": "FEM", "expires_on": clk.Day(90)})}), http.StatusCreated)
	plan := uploadFile(t, r, f.owner, files.PurposeSiteDocument, &f.siteA)
	wantStatus(t, do(t, r, http.MethodPut, safetyFilePath(f.siteA, uuid.NewString()), f.owner, map[string]any{
		"type_key": "health_and_safety_plan", "title": "H&S plan rev A", "version": versionReq(plan, nil)}), http.StatusCreated)
	letter := uploadFile(t, r, f.owner, files.PurposeAppointment, &f.siteA)
	wantStatus(t, do(t, r, http.MethodPut, appointmentPath(f.siteA, uuid.NewString()), f.owner, map[string]any{
		"worker_id": f.worker, "type_key": "construction_supervisor", "appointed_on": clk.Today(),
		"file_id": letter, "client_recorded_at": rfc(clk.Now())}), http.StatusCreated)
	// Site work starting today (REG4), its notice recorded with the Annexure 2.
	f.siteWork = uuid.NewString()
	rec := do(t, r, http.MethodPut, workVersionPath(f.siteA, f.siteWork, 1), f.owner, workBody(clk, clk.Today(), "excavation"))
	wantStatus(t, rec, http.StatusCreated)
	notice := liveWork(t, decodeWork(t, rec), reg4Key)
	annex := uploadFile(t, r, f.owner, files.PurposeSiteDocument, &f.siteA)
	f.annexure2 = uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, safetyFilePath(f.siteA, f.annexure2), f.owner, map[string]any{
		"type_key": "notification_of_construction_work", "title": "Annexure 2", "version": versionReq(annex, nil)}), http.StatusCreated)
	wantStatus(t, do(t, r, http.MethodPut, satisfactionPath(f.siteA, notice.ID, uuid.NewString()), f.owner, map[string]any{
		"satisfied_at": rfc(clk.Earlier(time.Minute)), "method": "email", "document_id": f.annexure2, "document_version": 1}), http.StatusCreated)
	return f
}

// The audit pack end to end (M12 D5-D10): queued as of now, built by the
// worker's builder, downloaded with every photo and file logged, the same
// bytes on every rebuild, whatever was edited or received since.
func TestExportPack(t *testing.T) {
	f := newExportFixture(t, "export")
	r, clk, ctx := f.router, newSASTClock(t), context.Background()
	org := f.ownerMe.OrgID
	body := exportBody(clk.Day(-1), clk.Today())

	// Queue: owners and SHE officers only; a repeat is 200, other details 409.
	wantStatus(t, do(t, r, http.MethodPut, exportPath(f.siteA, uuid.NewString()), f.supervisor, body), http.StatusForbidden)
	wantStatus(t, do(t, r, http.MethodPut, exportPath(f.siteA, uuid.NewString()), f.bookkeeper, body), http.StatusForbidden)
	wantError(t, do(t, r, http.MethodPut, exportPath(f.siteA, uuid.NewString()), f.owner, exportBody(clk.Today(), clk.Day(-1))),
		http.StatusBadRequest, exports.ErrBadRange.Error())
	wantError(t, do(t, r, http.MethodPut, exportPath(f.otherSite, uuid.NewString()), f.owner, body), http.StatusNotFound, "site not found")
	id := uuid.NewString()
	rec := do(t, r, http.MethodPut, exportPath(f.siteA, id), f.owner, body)
	wantStatus(t, rec, http.StatusCreated)
	e := decodeExport(t, rec.Body)
	if e.Status != exports.StatusQueued || !e.IncludesHealth || e.RequesterRole != "owner" || e.PackFormat != exports.PackFormat || e.AsOf.IsZero() {
		t.Errorf("queued %+v", e)
	}
	wantStatus(t, do(t, r, http.MethodPut, exportPath(f.siteA, id), f.owner, body), http.StatusOK)
	wantError(t, do(t, r, http.MethodPut, exportPath(f.siteA, id), f.owner, exportBody(clk.Day(-2), clk.Today())),
		http.StatusConflict, exports.ErrIDReused.Error())
	wantError(t, do(t, r, http.MethodPost, exportPath(f.siteA, id)+"/download", f.owner, nil), http.StatusConflict, exports.ErrNotReady.Error())

	// Not before the settle window (2 minutes by default).
	if a, err := (&exports.Builder{Pool: testPool, Instance: "x"}).Claim(ctx, org); err != nil || a != nil {
		t.Fatalf("claimed inside the settle window: %+v, %v", a, err)
	}
	b := newBuilder(10 * time.Second)
	a := claim(t, b, org, id)
	if got := getExportStatus(t, r, f.owner, f.siteA, id); got.Status != exports.StatusBuilding || got.Attempts != 1 {
		t.Errorf("while building %+v", got)
	}
	if err := b.Run(ctx, a); err != nil {
		t.Fatal(err)
	}
	e = getExportStatus(t, r, f.owner, f.siteA, id)
	if e.Status != exports.StatusReady || e.SHA256 == nil || *e.EvidenceCount < 3 || *e.FileCount < 5 || *e.ChainLength < 3 || !*e.ChainIntact {
		t.Fatalf("built %+v", e)
	}

	// Download: logged first, then a link to exactly the recorded bytes.
	rec = do(t, r, http.MethodPost, exportPath(f.siteA, id)+"/download", f.owner, nil)
	wantStatus(t, rec, http.StatusOK)
	var d exports.Download
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	resp, err := http.Get(d.URL)
	if err != nil {
		t.Fatal(err)
	}
	pack, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if sha(pack) != *e.SHA256 || d.SHA256 != *e.SHA256 || int64(len(pack)) != *e.SizeBytes {
		t.Fatalf("downloaded %d bytes, sha %s; recorded %s", len(pack), sha(pack), *e.SHA256)
	}
	if n := countRows(t, `SELECT count(*) FROM evidence_access_log WHERE export_id = $1 AND action = 'export'`, id); n != *e.EvidenceCount {
		t.Errorf("%d evidence export rows, want %d", n, *e.EvidenceCount)
	}
	if n := countRows(t, `SELECT count(*) FROM file_access_log WHERE export_id = $1 AND action = 'export'`, id); n != *e.FileCount {
		t.Errorf("%d file export rows, want %d", n, *e.FileCount)
	}
	if n := countRows(t, `SELECT count(*) FROM audit_export_downloads WHERE export_id = $1`, id); n != 1 {
		t.Errorf("%d download rows", n)
	}
	// The log's IP is the peer's: a forwarded header from an untrusted peer is ignored.
	req := httptest.NewRequest(http.MethodPost, exportPath(f.siteA, id)+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+f.owner)
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	spoofed := httptest.NewRecorder()
	r.ServeHTTP(spoofed, req)
	wantStatus(t, spoofed, http.StatusOK)
	if n := countRows(t, `SELECT count(*) FROM evidence_access_log WHERE export_id = $1 AND ip IS DISTINCT FROM '192.0.2.1'`, id); n != 0 {
		t.Errorf("%d evidence export rows not logged with the peer's IP", n)
	}
	if n := countRows(t, `SELECT count(*) FROM file_access_log WHERE export_id = $1 AND ip IS DISTINCT FROM '192.0.2.1'`, id); n != 0 {
		t.Errorf("%d file export rows not logged with the peer's IP", n)
	}

	// Inside: the entries in order, every file's sealed hash, every section.
	m, entries, names := readManifest(t, pack)
	if names[0] != "CHAIN.md" || names[1] != "README.txt" || names[len(names)-2] != "index.html" || names[len(names)-1] != "manifest.json" || !slices.IsSorted(names) {
		t.Errorf("entries %v", names)
	}
	for _, mf := range m.Files {
		if sha(entries[mf.Path]) != mf.SHA256 {
			t.Errorf("%s: sha %s, manifest %s", mf.Path, sha(entries[mf.Path]), mf.SHA256)
		}
	}
	var keys []string
	for _, s := range m.Sections {
		keys = append(keys, s.Key)
	}
	if want := []string{"vault", "appointments", "hs_file", "site_work", "workers", "toolbox_talks", "inspections", "actions",
		"incidents", "diary", "sign_ins", "received_photos"}; !slices.Equal(keys, want) {
		t.Errorf("sections %v, want %v", keys, want)
	}
	if m.Format != 2 || !bytes.Contains(entries["manifest.json"], []byte(f.siteWork)) || !bytes.Contains(entries["manifest.json"], []byte(f.annexure2)) {
		t.Errorf("pack_format %d, or the site work or its Annexure 2 is missing", m.Format)
	}
	if !bytes.Contains(entries["manifest.json"], []byte(f.medical)) || !strings.Contains(string(entries["index.html"]), "Sipho Dlamini") {
		t.Error("the medical or the worker's name is missing")
	}
	// The chain's specification travels with the pack, from the same file
	// the tests check; the README points at it and names the role in words.
	if string(entries["CHAIN.md"]) != evidence.ChainSpec() || !strings.Contains(string(entries["README.txt"]), "CHAIN.md in this pack") ||
		!strings.Contains(string(entries["README.txt"]), "requested by an owner, a role allowed") {
		t.Error("CHAIN.md or the README's notes are wrong")
	}
	// An owner's export holds injury details (D8), and the in-progress
	// inspection as its header only.
	if !bytes.Contains(entries["manifest.json"], []byte("Knocked out by a falling plank")) ||
		!bytes.Contains(entries["manifest.json"], []byte("in_progress_at_cut_off")) {
		t.Error("the injury details or the in-progress inspection are missing")
	}
	// Anyone can recompute the chain from the manifest alone (CHAIN.md).
	prev := evidence.GenesisHash[:]
	for _, c := range m.Chain.Entries {
		meta, err := evidence.MetadataHash(evidence.Metadata{EvidenceID: c.EvidenceID, ClientCapturedAt: c.ClientCapturedAt,
			Lat: c.Lat, Lng: c.Lng, UploadedBy: c.UploadedBy, DeviceID: c.DeviceID})
		content, _ := hex.DecodeString(c.ContentSHA256)
		link, err2 := evidence.ChainHash(evidence.Link{ContentSHA256: content, ServerReceivedAt: c.ServerReceivedAt,
			SiteID: f.siteA, PrevChainHash: prev, MetadataHash: meta[:]})
		if err != nil || err2 != nil || hex.EncodeToString(meta[:]) != c.MetadataHash || hex.EncodeToString(link[:]) != c.ChainHash {
			t.Fatalf("entry %d doesn't recompute", c.Seq)
		}
		prev = link[:]
	}
	if hex.EncodeToString(prev) != *e.ChainHead || int64(len(m.Chain.Entries)) != *e.ChainLength {
		t.Errorf("recomputed head %x of %d entries; recorded %s of %d", prev, len(m.Chain.Entries), *e.ChainHead, *e.ChainLength)
	}

	// The same data gives the same bytes; so does everything edited or
	// received after as_of (D10): names, the site, a member's role, new records.
	if got := sha(build(t, b, org, id)); got != *e.SHA256 {
		t.Fatalf("rebuilt %s, recorded %s", got, *e.SHA256)
	}
	wantStatus(t, do(t, r, http.MethodPut, workerPath(f.worker), f.owner, map[string]any{"name": "Sipho Renamed"}), http.StatusOK)
	wantStatus(t, do(t, r, http.MethodPatch, "/v1/sites/"+f.siteA, f.owner, map[string]any{"name": "Renamed site"}), http.StatusOK)
	execAsTenant(t, org, 1, `UPDATE users SET full_name = 'Renamed Owner' WHERE id = $1`, f.ownerID)
	execAsTenant(t, org, 1, `UPDATE org_members SET role = 'she_officer' WHERE user_id = $1`, f.supervisorID)
	wantStatus(t, do(t, r, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.owner,
		signInBody(uuid.NewString(), clk.Earlier(time.Minute), map[string]any{"new_worker": map[string]any{"name": "Late Arrival"}})), http.StatusCreated)
	photoTakenAt(t, f.evidenceFixture, f.owner, f.siteA, testClock.Now())
	// Records that changed state after as_of: the inspection completed, the
	// action verified, the talk ended, a second vault version, a new diary
	// revision. The pack keeps them as they stood.
	answerAllBySQL(t, org, f.open, len(f.tmpl.Items))
	wantStatus(t, do(t, r, http.MethodPost, completePathFor(f.siteA, f.open), f.supervisor, completeBody()), http.StatusCreated)
	wantStatus(t, do(t, r, http.MethodPut, eventPath(f.siteA, f.action, uuid.NewString()), f.owner,
		eventBody("verify", testClock.Now(), map[string]any{"closeout_id": f.closeout})), http.StatusCreated)
	wantStatus(t, do(t, r, http.MethodPut, talkPath(f.siteA, f.talk), f.supervisor,
		talkBody(map[string]any{"started_at": rfc(f.talkStart), "ended_at": rfc(f.talkStart.Add(20 * time.Minute))})), http.StatusOK)
	lgs2 := uploadFile(t, r, f.owner, files.PurposeOrgDocument, nil)
	wantStatus(t, do(t, r, http.MethodPut, vaultPath(f.lgs)+"/versions/2", f.owner,
		versionReq(lgs2, map[string]any{"issuer": "FEM", "expires_on": clk.Day(400)})), http.StatusCreated)
	wantStatus(t, do(t, r, http.MethodPut, revisionPath(f.siteA, clk.Today(), uuid.NewString()), f.supervisor,
		revisionBody(clk.Earlier(time.Second), map[string]any{"work_done": "Formwork stripped"})), http.StatusCreated)
	wantStatus(t, do(t, r, http.MethodPut, workVersionPath(f.siteA, f.siteWork, 2), f.owner,
		amendWork(workBody(clk, clk.Today(), "excavation"), "Description corrected", "description", "Basement excavation")), http.StatusCreated)
	if got := sha(build(t, b, org, id)); got != *e.SHA256 {
		t.Errorf("after edits and new records: rebuilt %s, recorded %s", got, *e.SHA256)
	}
	if fresh := queueExport(t, f.evidenceFixture, f.siteA, body); fresh.ID != "" {
		got := build(t, b, org, fresh.ID)
		if !bytes.Contains(got, []byte("Renamed site")) || !bytes.Contains(got, []byte("Late Arrival")) {
			t.Error("a new export doesn't show today's names and records")
		}
	}

	// A sealed field changed behind the database's back: another pack, and
	// its chain shows the break.
	tamper(t, `UPDATE evidence SET lat = 1, lng = 1 WHERE id = '`+f.failPhoto+`'`)
	tampered := build(t, b, org, id)
	if sha(tampered) == *e.SHA256 {
		t.Fatal("a tampered photo gave the same pack")
	}
	if tm, _, _ := readManifest(t, tampered); tm.Chain.Verification.Intact || tm.Chain.Verification.FirstBreak == nil ||
		tm.Chain.Verification.FirstBreak.Reason != evidence.BreakMetadataChanged {
		t.Errorf("tampered chain %+v", tm.Chain.Verification)
	}
}

// corruptStore serves the stored bytes with one bit flipped: the same
// size, so only the hash shows it.
type corruptStore struct{ storage.Store }

func (s corruptStore) ReadVersion(ctx context.Context, key, version string) (io.ReadCloser, error) {
	rc, err := s.Store.ReadVersion(ctx, key, version)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	b[len(b)/2] ^= 1
	return io.NopCloser(bytes.NewReader(b)), nil
}

// failingUploads stops partway through every pack, as a crash would.
type failingUploads struct{ storage.Store }

func (failingUploads) PutStream(_ context.Context, _ string, r io.Reader, _ string, _ int64) (string, error) {
	_, _ = io.CopyN(io.Discard, r, 1024)
	return "", errors.New("connection reset by peer")
}

// A failure leaves no result and nothing recorded as in the pack, and the
// export is queued again; after three it is failed for good.
func TestExportFailures(t *testing.T) {
	f := newExportFixture(t, "exportfail")
	clk, ctx, org := newSASTClock(t), context.Background(), f.ownerMe.OrgID
	e := queueExport(t, f.evidenceFixture, f.siteA, exportBody(clk.Today(), clk.Today()))

	// A stored photo that no longer matches its seal (same size, one bit
	// off) fails its own export for good: an integrity alarm, never retried.
	bad := newBuilder(10 * time.Second)
	bad.Evidence = corruptStore{testStore}
	if _, err := bad.BuildTo(ctx, org, e.ID, io.Discard); !errors.Is(err, exports.ErrSealMismatch) {
		t.Errorf("corrupt bytes: %v", err)
	}
	sealed := queueExport(t, f.evidenceFixture, f.siteA, exportBody(clk.Today(), clk.Today()))
	if err := bad.Run(ctx, claim(t, bad, org, e.ID)); !errors.Is(err, exports.ErrSealMismatch) {
		t.Errorf("run with corrupt bytes: %v", err)
	}
	if got := getExportStatus(t, f.router, f.owner, f.siteA, e.ID); got.Status != exports.StatusFailed || !got.IntegrityAlarm {
		t.Errorf("after a seal mismatch %+v", got)
	}
	e = sealed
	// An upload that dies halfway.
	crash := newBuilder(10 * time.Second)
	crash.Exports = failingUploads{testExports}
	if err := crash.Run(ctx, claim(t, crash, org, e.ID)); err == nil {
		t.Error("a failed upload succeeded")
	}
	got := getExportStatus(t, f.router, f.owner, f.siteA, e.ID)
	if got.Status != exports.StatusQueued || got.Attempts != 1 || got.FailedAttempts != 1 || got.LastError == nil ||
		!strings.Contains(*got.LastError, "connection reset") || got.SHA256 != nil {
		t.Errorf("after a failed upload %+v", got)
	}
	for _, table := range []string{"audit_export_results", "audit_export_evidence", "audit_export_files"} {
		if n := countRows(t, `SELECT count(*) FROM `+table+` WHERE export_id = $1`, e.ID); n != 0 {
			t.Errorf("%s has %d rows after failed builds", table, n)
		}
	}
	// The second attempt succeeds, with the bytes any rebuild gives.
	good := newBuilder(10 * time.Second)
	if err := good.Run(ctx, claim(t, good, org, e.ID)); err != nil {
		t.Fatal(err)
	}
	got = getExportStatus(t, f.router, f.owner, f.siteA, e.ID)
	if got.Status != exports.StatusReady || got.Attempts != 2 || *got.SHA256 != sha(build(t, good, org, e.ID)) {
		t.Errorf("second attempt %+v", got)
	}

	// Three failures: failed for good, never claimed again.
	doomed := queueExport(t, f.evidenceFixture, f.siteA, exportBody(clk.Day(-1), clk.Today()))
	for range exports.MaxAttempts {
		_ = crash.Run(ctx, claim(t, crash, org, doomed.ID))
	}
	if a, err := good.Claim(ctx, org); err != nil || a != nil {
		t.Errorf("claimed a failed export: %+v %v", a, err)
	}
	if got := getExportStatus(t, f.router, f.owner, f.siteA, doomed.ID); got.Status != exports.StatusFailed {
		t.Errorf("after %d failures: %s", exports.MaxAttempts, got.Status)
	}
	wantError(t, do(t, f.router, http.MethodPost, exportPath(f.siteA, doomed.ID)+"/download", f.owner, nil),
		http.StatusConflict, exports.ErrFailed.Error())

	// Seven days on, the built pack is gone; its record stays.
	later := routerAt(t, func() time.Time { return testClock.Now().Add(exports.Keep + time.Hour) })
	if got := getExportStatus(t, later, f.owner, f.siteA, e.ID); got.Status != exports.StatusExpired || got.SHA256 == nil {
		t.Errorf("a week on %+v", got)
	}
	wantError(t, do(t, later, http.MethodPost, exportPath(f.siteA, e.ID)+"/download", f.owner, nil),
		http.StatusConflict, exports.ErrExpired.Error())
}

// Two builders never build one export into two results. A claim held open
// makes a second builder skip it; two attempts inserted at once take turns
// and the second is refused; a builder whose lease lapsed and was
// re-claimed can't record its pack (fencing).
func TestExportRaces(t *testing.T) {
	f := newEvidenceFixture(t, "exportrace")
	clk, ctx, org := newSASTClock(t), context.Background(), f.ownerMe.OrgID
	e := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	time.Sleep(5 * time.Millisecond)

	insertAttempt := `
		INSERT INTO audit_export_attempts (id, org_id, export_id, attempt, instance, claimed_at, lease_until)
		VALUES ($1, $2, $3, 1, 'race', now(), clock_timestamp() + interval '1 minute') /* attempt race */`
	held, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	if err := database.SetTenant(ctx, held, database.Tenant{OrgID: org}); err != nil {
		t.Fatal(err)
	}
	if _, err := held.Exec(ctx, `SELECT set_config('siteproof.export_settle', '0 seconds', true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := held.Exec(ctx, insertAttempt, uuid.NewString(), org, e.ID); err != nil {
		t.Fatal(err)
	}
	// A builder claiming now skips the locked export instead of waiting.
	if a, err := newBuilder(time.Minute).Claim(ctx, org); err != nil || a != nil {
		t.Fatalf("claimed a held export: %+v, %v", a, err)
	}
	// A second attempt inserted directly waits for the first, then is refused.
	second := make(chan error, 1)
	go func() {
		second <- database.WithTenant(ctx, testPool, database.Tenant{OrgID: org}, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT set_config('siteproof.export_settle', '0 seconds', true)`); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, insertAttempt, uuid.NewString(), org, e.ID)
			return err
		})
	}()
	waitForLock(t, "attempt race")
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-second; err == nil || !strings.Contains(err.Error(), "another attempt holds the lease") {
		t.Errorf("a second attempt while the first held the lease: %v", err)
	}
	if n := countRows(t, `SELECT count(*) FROM audit_export_attempts WHERE export_id = $1`, e.ID); n != 1 {
		t.Errorf("%d attempts", n)
	}

	// Fencing: a lease that lapses, a re-claim, and the first builder late.
	e2 := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	slow := newBuilder(300 * time.Millisecond)
	first := claim(t, slow, org, e2.ID)
	time.Sleep(400 * time.Millisecond)
	if err := slow.Extend(ctx, first); err == nil {
		t.Error("a lapsed lease was extended")
	}
	fast := newBuilder(10 * time.Second)
	again := claim(t, fast, org, e2.ID)
	if again.Number != 2 {
		t.Errorf("re-claim is attempt %d", again.Number)
	}
	if err := slow.Run(ctx, first); err == nil {
		t.Error("the lapsed attempt recorded a result")
	}
	if err := fast.Run(ctx, again); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, `SELECT count(*) FROM audit_export_results WHERE export_id = $1 AND attempt_id = $2`, e2.ID, again.ID); n != 1 {
		t.Error("the result isn't the re-claim's")
	}
	if n := countRows(t, `SELECT count(*) FROM audit_export_attempts WHERE id = $1 AND outcome = 'lapsed'`, first.ID); n != 1 {
		t.Error("the first attempt isn't marked lapsed")
	}
}

// The database's own rules, whatever the code does: no attempt before the
// settle window; a result only from a live lease; a result's contents
// exactly its counts at commit; and no edit of a finished record.
func TestExportDatabaseRules(t *testing.T) {
	f := newEvidenceFixture(t, "exportdb")
	clk, ctx, org := newSASTClock(t), context.Background(), f.ownerMe.OrgID
	e := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	attempt := uuid.NewString()
	run := func(settle string, stmts ...string) error {
		return database.WithTenant(ctx, testPool, database.Tenant{OrgID: org}, func(tx pgx.Tx) error {
			if settle != "" {
				if _, err := tx.Exec(ctx, `SELECT set_config('siteproof.export_settle', $1, true)`, settle); err != nil {
					return err
				}
			}
			for _, s := range stmts {
				if _, err := tx.Exec(ctx, s); err != nil {
					return err
				}
			}
			return nil
		})
	}
	insert := `INSERT INTO audit_export_attempts (id, org_id, export_id, attempt, instance, claimed_at, lease_until)
		VALUES ('` + attempt + `', '` + org + `', '` + e.ID + `', 1, 'x', now(), clock_timestamp() + interval '1 minute')`
	if err := run("", insert); err == nil || !strings.Contains(err.Error(), "not settled yet") {
		t.Errorf("an attempt inside the default settle window: %v", err)
	}
	if err := run("0 seconds", insert); err != nil {
		t.Fatal(err)
	}
	result := `INSERT INTO audit_export_results (export_id, org_id, attempt_id, sha256, size_bytes, object_key, object_version_id,
		evidence_count, file_count, chain_length, chain_intact)
		VALUES ('` + e.ID + `', '` + org + `', '` + attempt + `', decode(repeat('0a', 32), 'hex'), 10, 'k', 'v', 1, 0, 0, true)`
	if err := run("", result); err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Errorf("a result without its photo row: %v", err)
	}
	if err := run("", `UPDATE audit_export_attempts SET lease_until = lease_until - interval '1 second' WHERE id = '`+attempt+`'`); err == nil {
		t.Error("a lease moved back")
	}
	if err := run("", `UPDATE audit_export_attempts SET outcome = 'ready', finished_at = now() WHERE id = '`+attempt+`'`); err == nil {
		t.Error("an attempt marked ready without a result")
	}
	if err := run("", `UPDATE audit_export_attempts SET outcome = 'lapsed', finished_at = now() WHERE id = '`+attempt+`'`); err == nil {
		t.Error("a live lease marked lapsed")
	}
	if err := run("", `UPDATE audit_export_attempts SET outcome = 'failed', error = 'x', finished_at = now() WHERE id = '`+attempt+`'`); err != nil {
		t.Fatal(err)
	}
	if err := run("", strings.Replace(result, "1, 0, 0, true", "0, 0, 0, true", 1)); err == nil {
		t.Error("a result from a finished attempt")
	}
	if err := run("", `UPDATE audit_export_attempts SET error = 'y' WHERE id = '`+attempt+`'`); err == nil {
		t.Error("a finished attempt edited")
	}
	if err := run("", `UPDATE audit_exports SET id = id WHERE id = '`+e.ID+`'`); err == nil {
		t.Error("an export edited")
	}
	// Fencing: a result from an attempt whose lease has lapsed is refused,
	// even before anyone marks it.
	e2 := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	lapsing := uuid.NewString()
	if err := run("0 seconds", `INSERT INTO audit_export_attempts (id, org_id, export_id, attempt, instance, claimed_at, lease_until)
		VALUES ('`+lapsing+`', '`+org+`', '`+e2.ID+`', 1, 'x', now(), clock_timestamp() + interval '50 milliseconds')`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	late := strings.NewReplacer(e.ID, e2.ID, attempt, lapsing, "1, 0, 0, true", "0, 0, 0, true").Replace(result)
	if err := run("", late); err == nil || !strings.Contains(err.Error(), "no longer holds the lease") {
		t.Errorf("a result from a lapsed lease: %v", err)
	}
	// The requester's role is checked against the membership.
	if err := run("", `INSERT INTO audit_exports (id, org_id, site_id, requested_by, requester_role, from_date, to_date, time_zone, as_of,
		pack_format, regime_code, regime_version, regime_data_sha256, includes_health, client_requested_at)
		VALUES (gen_random_uuid(), '`+org+`', '`+f.siteA+`', '`+f.ownerMe.UserID+`', 'she_officer', current_date, current_date,
		'Africa/Johannesburg', now(), 1, 'za_construction_2014', '2014', decode(repeat('0b', 32), 'hex'), true, now())`); err == nil ||
		!strings.Contains(err.Error(), "requester_role") {
		t.Errorf("an export claiming another role: %v", err)
	}
}

// Retries back off (M12 review): a second attempt 1 minute after the
// first ended, a third 5 minutes after the second. A failing build gets one
// attempt per pass, not three in a row.
func TestExportBackoff(t *testing.T) {
	f := newEvidenceFixture(t, "exportbackoff")
	clk, ctx, org := newSASTClock(t), context.Background(), f.ownerMe.OrgID
	e := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	time.Sleep(5 * time.Millisecond)

	crash := &exports.Builder{Pool: testPool, Evidence: testStore, Exports: failingUploads{testExports},
		Instance: "backoff", Settle: time.Millisecond, Lease: 10 * time.Second, OnlyOrgs: []string{org}}
	_, err := crash.RunOnce(ctx)
	if err == nil || !strings.Contains(err.Error(), "connection reset") || strings.Contains(err.Error(), "next attempt may start") {
		t.Errorf("one pass: %v", err)
	}
	if n := countRows(t, `SELECT count(*) FROM audit_export_attempts WHERE export_id = $1`, e.ID); n != 1 {
		t.Fatalf("a failing build got %d attempts in one pass, want 1", n)
	}
	if a, err := crash.Claim(ctx, org); err != nil || a != nil {
		t.Errorf("claimed within a minute of the failure: %+v, %v", a, err)
	}
	// The database refuses it too, whatever the claim query says.
	early := database.WithTenant(ctx, testPool, database.Tenant{OrgID: org}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('siteproof.export_settle', '0 seconds', true)`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO audit_export_attempts (id, org_id, export_id, attempt, instance, claimed_at, lease_until)
			VALUES (gen_random_uuid(), $1, $2, 2, 'x', now(), clock_timestamp() + interval '1 minute')`, org, e.ID)
		return err
	})
	if early == nil || !strings.Contains(early.Error(), "the next attempt may start") {
		t.Errorf("an early second attempt: %v", early)
	}

	// A worker that stopped: its lease lapses, then the backoff runs from there.
	e2 := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	time.Sleep(5 * time.Millisecond)
	stopped := &exports.Builder{Pool: testPool, Instance: "stopped", Settle: time.Millisecond, Lease: 200 * time.Millisecond}
	if a, err := stopped.Claim(ctx, org); err != nil || a == nil || a.ExportID != e2.ID {
		t.Fatalf("claim: %+v, %v", a, err)
	}
	time.Sleep(300 * time.Millisecond)
	if a, err := stopped.Claim(ctx, org); err != nil || a != nil {
		t.Errorf("re-claimed right after the lease lapsed: %+v, %v", a, err)
	}

	// The backoff runs from when the lease ran out, not from the claim.
	e4 := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	time.Sleep(5 * time.Millisecond)
	timed := &exports.Builder{Pool: testPool, Instance: "timed", Settle: time.Millisecond, Lease: 300 * time.Millisecond,
		Backoff: 200 * time.Millisecond}
	claimE4 := func() *exports.Attempt {
		t.Helper()
		for {
			a, err := timed.Claim(ctx, org)
			if err != nil {
				t.Fatal(err)
			}
			if a == nil || a.ExportID == e4.ID {
				return a
			}
			_ = timed.Fail(ctx, a, errors.New("not this test's export"))
		}
	}
	if a := claimE4(); a == nil {
		t.Fatal("no claim of e4")
	}
	time.Sleep(350 * time.Millisecond) // lapsed at 300 ms; may restart at 500
	if a := claimE4(); a != nil {
		t.Errorf("re-claimed 50 ms after the lease lapsed, inside the 200 ms backoff")
	}
	time.Sleep(250 * time.Millisecond)
	if a := claimE4(); a == nil {
		t.Error("no re-claim after the backoff")
	}

	// With a 100 ms base: the second attempt after 100 ms, the third after 500.
	e3 := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	quick := &exports.Builder{Pool: testPool, Evidence: testStore, Exports: failingUploads{testExports},
		Instance: "quick", Settle: time.Millisecond, Lease: 10 * time.Second, Backoff: 100 * time.Millisecond}
	runNext := func(want bool) {
		t.Helper()
		var a *exports.Attempt
		for a == nil || a.ExportID != e3.ID {
			if a, err = quick.Claim(ctx, org); err != nil {
				t.Fatal(err)
			}
			if a == nil {
				break
			}
			if a.ExportID != e3.ID {
				_ = quick.Fail(ctx, a, errors.New("not this test's export"))
			}
		}
		if (a != nil) != want {
			t.Fatalf("claim of e3: got %+v, want a claim: %v", a, want)
		}
		if a != nil {
			_ = quick.Run(ctx, a)
		}
	}
	time.Sleep(5 * time.Millisecond)
	runNext(true)
	time.Sleep(150 * time.Millisecond)
	runNext(true)
	time.Sleep(150 * time.Millisecond)
	runNext(false) // the third waits 500 ms
	time.Sleep(450 * time.Millisecond)
	runNext(true)
}

// A stored original that no longer matches its seal is an integrity alarm
// (M12 review): the export fails at once and for good, it is logged at
// error with the object, export and site, and owners and SHE officers are
// told. Tampered in MinIO: a new version of the photo's object with one
// bit flipped, which the sealed record is pointed at.
func TestExportIntegrityAlarm(t *testing.T) {
	f := newEvidenceFixture(t, "exportalarm")
	clk, ctx, org := newSASTClock(t), context.Background(), f.ownerMe.OrgID
	photo := photoTakenAt(t, f, f.owner, f.siteA, testClock.Now())
	var key, version string
	if err := testAdmin.QueryRow(ctx, `SELECT object_key, object_version_id FROM evidence WHERE id = $1`, photo).Scan(&key, &version); err != nil {
		t.Fatal(err)
	}
	obj, err := testS3.Admin.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(testS3.Config.Bucket), Key: aws.String(key), VersionId: aws.String(version)})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(obj.Body)
	obj.Body.Close()
	body[len(body)/2] ^= 1
	put, err := testS3.Admin.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(testS3.Config.Bucket), Key: aws.String(key),
		Body: bytes.NewReader(body), ChecksumAlgorithm: s3types.ChecksumAlgorithmSha256})
	if err != nil {
		t.Fatal(err)
	}
	tamper(t, `UPDATE evidence SET object_version_id = '`+*put.VersionId+`' WHERE id = '`+photo+`'`)

	e := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	b := newBuilder(10 * time.Second)
	err = b.Run(ctx, claim(t, b, org, e.ID))
	slog.SetDefault(prev)
	if !errors.Is(err, exports.ErrSealMismatch) {
		t.Fatalf("run: %v", err)
	}
	got := getExportStatus(t, f.router, f.owner, f.siteA, e.ID)
	if got.Status != exports.StatusFailed || !got.IntegrityAlarm || got.Attempts != 1 || got.LastError == nil ||
		!strings.Contains(*got.LastError, photo) || !strings.Contains(*got.LastError, key) {
		t.Errorf("after the alarm %+v", got)
	}
	for _, want := range []string{"level=ERROR", "no longer matches its seal", "export=" + e.ID, "site=" + f.siteA, "object=" + key} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("the log lacks %q:\n%s", want, logs.String())
		}
	}
	// Never again: not by the claim, not by the database.
	time.Sleep(10 * time.Millisecond)
	if a, err := b.Claim(ctx, org); err != nil || a != nil {
		t.Errorf("claimed after an integrity alarm: %+v, %v", a, err)
	}
	again := database.WithTenant(ctx, testPool, database.Tenant{OrgID: org}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('siteproof.export_settle', '0 seconds', true), set_config('siteproof.export_backoff', '0 seconds', true)`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO audit_export_attempts (id, org_id, export_id, attempt, instance, claimed_at, lease_until)
			VALUES (gen_random_uuid(), $1, $2, 2, 'x', now(), clock_timestamp() + interval '1 minute')`, org, e.ID)
		return err
	})
	if again == nil || !strings.Contains(again.Error(), "stopped for good") {
		t.Errorf("a second attempt after the alarm: %v", again)
	}
	// The owner is told, in-app and by email; the supervisor isn't.
	alarms := 0
	for _, n := range inbox(t, f.router, f.owner) {
		if n.Kind == "integrity_alarm" && n.SubjectType == "export" && n.SubjectID == e.ID && n.SubjectState == "current" {
			alarms++
		}
	}
	if alarms != 1 {
		t.Errorf("owner's inbox has %d integrity alarms, want 1", alarms)
	}
	for _, n := range inbox(t, f.router, f.supervisor) {
		if n.Kind == "integrity_alarm" {
			t.Error("a supervisor was alerted")
		}
	}
	if n := countRows(t, `SELECT count(*) FROM notification_emails m JOIN notifications n ON n.id = m.notification_id
		WHERE n.kind = 'integrity_alarm' AND n.subject_id = $1`, e.ID); n != 1 {
		t.Errorf("%d alarm emails, want 1 (the owner)", n)
	}
}

// A pack too large for one object is refused for good, with a clear
// reason, and never retried (M12 review). The limit is lowered for the test.
func TestExportTooLarge(t *testing.T) {
	f := newEvidenceFixture(t, "exporttoolarge")
	clk, ctx, org := newSASTClock(t), context.Background(), f.ownerMe.OrgID
	e := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	prev := exports.MaxPackSize
	exports.MaxPackSize = 1000
	defer func() { exports.MaxPackSize = prev }()

	b := newBuilder(10 * time.Second)
	if err := b.Run(ctx, claim(t, b, org, e.ID)); !errors.Is(err, exports.ErrPackTooLarge) {
		t.Fatalf("run: %v", err)
	}
	got := getExportStatus(t, f.router, f.owner, f.siteA, e.ID)
	if got.Status != exports.StatusFailed || got.Attempts != 1 || got.LastError == nil || !strings.Contains(*got.LastError, "export a shorter period") {
		t.Errorf("refused %+v", got)
	}
	time.Sleep(10 * time.Millisecond)
	if a, err := b.Claim(ctx, org); err != nil || a != nil {
		t.Errorf("claimed a refused export: %+v, %v", a, err)
	}
	if n := countRows(t, `SELECT count(*) FROM audit_export_results WHERE export_id = $1`, e.ID); n != 0 {
		t.Error("a refused export has a result")
	}
}

// slowUploads holds every pack upload for a while, as a big pack would.
type slowUploads struct {
	storage.Store
	wait time.Duration
}

func (s slowUploads) PutStream(ctx context.Context, key string, r io.Reader, contentType string, size int64) (string, error) {
	select {
	case <-time.After(s.wait):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return s.Store.PutStream(ctx, key, r, contentType, size)
}

// A long healthy build heartbeats into job_runs while it runs, so
// /healthz/jobs doesn't call exports stale (M12 review).
func TestExportHeartbeat(t *testing.T) {
	f := newEvidenceFixture(t, "exportheartbeat")
	clk, ctx, org := newSASTClock(t), context.Background(), f.ownerMe.OrgID
	e := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))

	// Long enough that a busy CI machine renews the lease in time (it lapsed
	// at 300 ms under load); about 12 renewals in a 4 s build.
	b := newBuilder(time.Second)
	b.Exports, b.OnlyOrgs = slowUploads{Store: testExports, wait: 4 * time.Second}, []string{org}
	b.Heartbeat = func(ctx context.Context) {
		_ = notifications.RecordRun(ctx, testPool, notifications.JobExports, b.Instance, func(context.Context) (int, error) { return 0, nil })
	}
	time.Sleep(10 * time.Millisecond)
	built := 0
	err := notifications.RecordRun(ctx, testPool, notifications.JobExports, b.Instance+"-build", func(ctx context.Context) (int, error) {
		n, err := b.RunOne(ctx)
		built = n
		return n, err
	})
	if err != nil || built != 1 {
		t.Fatalf("run one: %d, %v", built, err)
	}
	if got := getExportStatus(t, f.router, f.owner, f.siteA, e.ID); got.Status != exports.StatusReady {
		t.Fatalf("export %s", got.Status)
	}
	// Heartbeats finished while the build was still running.
	n := countRows(t, `SELECT count(*) FROM job_runs h, job_runs r
		WHERE h.instance = $1 AND r.instance = $1 || '-build' AND h.job = 'exports' AND h.ok AND h.finished_at < r.finished_at`, b.Instance)
	if n < 3 {
		t.Errorf("%d heartbeats during a 4 s build with a 1 s lease, want at least 3", n)
	}
	if n, err := b.RunOne(ctx); err != nil || n != 0 {
		t.Errorf("nothing left to build: %d, %v", n, err)
	}
}

// A build that loses its lease stops and says why, not just "context
// canceled". Forced: the attempt row is held, so a renewal waits past the
// lease, and the guard then refuses to extend a lapsed lease.
func TestExportLostLease(t *testing.T) {
	f := newEvidenceFixture(t, "exportlostlease")
	clk, ctx, org := newSASTClock(t), context.Background(), f.ownerMe.OrgID
	e := queueExport(t, f, f.siteA, exportBody(clk.Today(), clk.Today()))
	b := newBuilder(time.Second)
	b.Exports, b.OnlyOrgs = slowUploads{Store: testExports, wait: time.Minute}, []string{org}
	time.Sleep(10 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := b.RunOne(ctx)
		done <- err
	}()

	var attempt string
	for deadline := time.Now().Add(10 * time.Second); attempt == ""; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no attempt claimed")
		}
		_ = testAdmin.QueryRow(ctx, `SELECT id FROM audit_export_attempts WHERE export_id = $1`, e.ID).Scan(&attempt)
	}
	hold := holdOpen(t, f)
	if _, err := hold.Exec(ctx, `SELECT 1 FROM audit_export_attempts WHERE id = $1 FOR UPDATE`, attempt); err != nil {
		t.Fatal(err)
	}
	waitForLock(t, "UPDATE audit_export_attempts")
	time.Sleep(1200 * time.Millisecond) // the lease lapses while the renewal waits
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "lost the lease") || !strings.Contains(err.Error(), "only while it is live") {
			t.Errorf("run: %v; want it to name the lost lease", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the build didn't stop after losing its lease")
	}
}
