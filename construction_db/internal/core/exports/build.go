package exports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/storage"
)

// Builder runs in cmd/worker. Postgres decides who builds what (000039): a
// result is accepted only from the newest attempt while its lease is live.
type Builder struct {
	Pool        *pgxpool.Pool
	Evidence    storage.Store // the evidence bucket: photos and document files are read from it
	Exports     storage.Store // the exports bucket: packs are written to it
	Contributor Contributor   // the edition's sections; nil for none
	Instance    string        // who is building, for the attempt row
	// Settle, Lease and Backoff are the defaults when zero; tests shorten them.
	Settle, Lease, Backoff time.Duration
	// OnlyOrgs limits RunOnce to these organisations (tests).
	OnlyOrgs []string
	// Heartbeat, when set, is called each time a build extends its lease:
	// the worker records it, so a long healthy build isn't reported stale.
	Heartbeat func(context.Context)
}

func (b *Builder) settle() time.Duration { return cmpOr(b.Settle, Settle) }
func (b *Builder) lease() time.Duration  { return cmpOr(b.Lease, Lease) }

func cmpOr(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func interval(d time.Duration) string { return fmt.Sprintf("%d milliseconds", d.Milliseconds()) }

// Attempt is a claimed build.
type Attempt struct {
	ID, OrgID, ExportID string
	Number              int
}

// Result is a built pack.
type Result struct {
	SHA256      []byte
	SizeBytes   int64
	EvidenceIDs []string
	FileIDs     []string
	Chain       evidence.ChainVerification
}

// RunOnce builds every claimable export, one at a time.
func (b *Builder) RunOnce(ctx context.Context) (int, error) {
	built := 0
	var errs []error
	for {
		n, err := b.RunOne(ctx)
		if err != nil {
			errs = append(errs, err)
		}
		if n == 0 {
			return built, errors.Join(errs...)
		}
		built += n
	}
}

// RunOne claims the first claimable export of any organisation and builds
// it: 1 when it claimed one (built or not), 0 when there was none. The
// worker records each call as its own 'exports' run.
func (b *Builder) RunOne(ctx context.Context) (int, error) {
	orgs := b.OnlyOrgs
	if len(orgs) == 0 {
		err := database.WithTenant(ctx, b.Pool, database.Tenant{}, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT id::text FROM worker_org_ids() id`)
			if err != nil {
				return err
			}
			orgs, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		})
		if err != nil {
			return 0, fmt.Errorf("exports: list orgs: %w", err)
		}
	}
	var errs []error
	for _, org := range orgs {
		a, err := b.Claim(ctx, org)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if a == nil {
			continue
		}
		if err := b.Run(ctx, a); err != nil {
			slog.Error("audit pack build failed", "export", a.ExportID, "attempt", a.Number, "err", err)
			return 1, err
		}
		return 1, nil
	}
	return 0, errors.Join(errs...)
}

// Claim returns nil when nothing is claimable. SKIP LOCKED: a second builder
// moves on instead of waiting.
func (b *Builder) Claim(ctx context.Context, orgID string) (*Attempt, error) {
	var a *Attempt
	err := database.WithTenant(ctx, b.Pool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('siteproof.export_settle', $1, true)`, interval(b.settle())); err != nil {
			return err
		}
		if b.Backoff > 0 {
			if _, err := tx.Exec(ctx, `SELECT set_config('siteproof.export_backoff', $1, true)`, interval(b.Backoff)); err != nil {
				return err
			}
		}
		var exportID string
		err := tx.QueryRow(ctx, `
			SELECT e.id::text FROM audit_exports e
			WHERE NOT EXISTS (SELECT 1 FROM audit_export_results r WHERE r.export_id = e.id)
			  AND e.as_of <= clock_timestamp() - audit_export_settle()
			  AND audit_export_retry_at(e.id) <= clock_timestamp()
			  AND NOT EXISTS (SELECT 1 FROM audit_export_attempts x WHERE x.export_id = e.id AND x.outcome IN ('integrity', 'refused'))
			  AND NOT EXISTS (SELECT 1 FROM audit_export_attempts x
			                  WHERE x.export_id = e.id AND x.finished_at IS NULL AND x.lease_until > clock_timestamp())
			  AND (SELECT count(*) FROM audit_export_attempts x WHERE x.export_id = e.id
			         AND (x.outcome IN ('failed', 'lapsed') OR (x.finished_at IS NULL AND x.lease_until <= clock_timestamp()))) < $1
			ORDER BY e.as_of, e.id
			LIMIT 1
			FOR UPDATE OF e SKIP LOCKED`, MaxAttempts).Scan(&exportID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE audit_export_attempts SET outcome = 'lapsed', finished_at = clock_timestamp(),
			       error = 'the lease lapsed: the builder stopped or lost the database'
			WHERE export_id = $1 AND finished_at IS NULL AND lease_until <= clock_timestamp()`, exportID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM audit_export_attempts WHERE export_id = $1`, exportID).Scan(&n); err != nil {
			return err
		}
		id := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_export_attempts (id, org_id, export_id, attempt, instance, claimed_at, lease_until)
			VALUES ($1, $2, $3, $4, $5, clock_timestamp(), clock_timestamp() + $6::interval)`,
			id, orgID, exportID, n+1, b.Instance, interval(b.lease())); err != nil {
			return err
		}
		a = &Attempt{ID: id, OrgID: orgID, ExportID: exportID, Number: n + 1}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("exports: claim: %w", err)
	}
	return a, nil
}

// Extend fails once the lease has lapsed: the build must then stop.
func (b *Builder) Extend(ctx context.Context, a *Attempt) error {
	return database.WithTenant(ctx, b.Pool, database.Tenant{OrgID: a.OrgID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE audit_export_attempts SET lease_until = clock_timestamp() + $2::interval
			WHERE id = $1 AND finished_at IS NULL`, a.ID, interval(b.lease()))
		if err == nil && tag.RowsAffected() != 1 {
			err = errors.New("attempt already finished")
		}
		return err
	})
}

// Fail leaves a lapsed or finished attempt as it is.
func (b *Builder) Fail(ctx context.Context, a *Attempt, cause error) error {
	msg := cause.Error()
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	return database.WithTenant(ctx, b.Pool, database.Tenant{OrgID: a.OrgID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE audit_export_attempts SET outcome = 'failed', error = $2, finished_at = clock_timestamp()
			WHERE id = $1 AND finished_at IS NULL AND lease_until > clock_timestamp()`, a.ID, msg)
		return err
	})
}

// Run records a result only after the pack is fully stored.
func (b *Builder) Run(ctx context.Context, a *Attempt) error {
	bctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go b.keepLease(bctx, cancel, a)

	p, err := b.prepare(bctx, a.OrgID, a.ExportID)
	if err == nil && p.size > MaxPackSize {
		err = fmt.Errorf("the pack would be about %d bytes, more than one object can hold (%d): %w; export a shorter period",
			p.size, MaxPackSize, ErrPackTooLarge)
	}
	if err == nil {
		err = b.upload(bctx, a, p)
	}
	if err != nil {
		// A lost lease cancels the build: name it, not just "context canceled".
		if cause := context.Cause(bctx); cause != nil && !errors.Is(err, cause) {
			err = fmt.Errorf("%w (%w)", err, cause)
		}
		var seal *SealError
		switch {
		case errors.As(err, &seal):
			if aerr := b.alarm(context.WithoutCancel(ctx), a, seal); aerr != nil {
				slog.Error("audit pack: could not record the integrity alarm", "export", a.ExportID, "err", aerr)
			}
		case errors.Is(err, ErrPackTooLarge):
			if rerr := b.finish(context.WithoutCancel(ctx), a, "refused", err); rerr != nil {
				slog.Warn("audit pack: could not record the refusal", "export", a.ExportID, "err", rerr)
			}
		default:
			if ferr := b.Fail(context.WithoutCancel(ctx), a, err); ferr != nil {
				slog.Warn("audit pack: could not record the failure", "export", a.ExportID, "err", ferr)
			}
		}
		return fmt.Errorf("exports: build %s: %w", a.ExportID, err)
	}
	return nil
}

// upload streams the pack to the exports bucket, parts sized by its estimate,
// then records the result.
func (b *Builder) upload(ctx context.Context, a *Attempt, p *prepared) error {
	key := fmt.Sprintf("exports/%s/%s/%d.zip", a.OrgID, a.ExportID, a.Number)
	pr, pw := io.Pipe()
	type built struct {
		res *Result
		err error
	}
	done := make(chan built, 1)
	go func() {
		res, err := b.write(ctx, p, pw)
		pw.CloseWithError(err)
		done <- built{res, err}
	}()
	version, upErr := b.Exports.PutStream(ctx, key, pr, ContentType, p.size)
	pr.CloseWithError(upErr) // unblocks the writer if the upload stopped first
	out := <-done
	if err := errors.Join(out.err, upErr); err != nil {
		return err
	}
	if err := context.Cause(ctx); err != nil {
		return err // the lease was lost while uploading
	}
	return b.record(context.WithoutCancel(ctx), a, out.res, key, version)
}

// alarm: a stored original no longer matches its seal (M12 review). The
// export stops for good and its owners and SHE officers are told, in-app
// and by email.
func (b *Builder) alarm(ctx context.Context, a *Attempt, seal *SealError) error {
	var siteID, siteName string
	err := database.WithTenant(ctx, b.Pool, database.Tenant{OrgID: a.OrgID}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT e.site_id::text, s.name FROM audit_exports e JOIN sites s ON s.id = e.site_id WHERE e.id = $1`,
			a.ExportID).Scan(&siteID, &siteName); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE audit_export_attempts SET outcome = 'integrity', error = $2, finished_at = clock_timestamp()
			WHERE id = $1 AND finished_at IS NULL`, a.ID, truncate(seal.Error(), 2000)); err != nil {
			return err
		}
		title := truncate("Integrity alarm: a photo or file no longer matches its seal ("+siteName+")", 300)
		_, err := tx.Exec(ctx, `
			WITH added AS (
			    INSERT INTO notifications (org_id, user_id, kind, subject_type, subject_id, threshold, site_id, title)
			    SELECT m.org_id, m.user_id, 'integrity_alarm', 'export', $1, 'seal_mismatch', $2, $3
			    FROM org_members m
			    WHERE m.org_id = app_org_id() AND m.status = 'active' AND m.role IN ('owner', 'she_officer')
			    ON CONFLICT ON CONSTRAINT notifications_once DO NOTHING
			    RETURNING id, org_id, user_id
			)
			INSERT INTO notification_emails (notification_id, org_id, to_address)
			SELECT a.id, a.org_id, u.email FROM added a JOIN users u ON u.id = a.user_id WHERE u.email IS NOT NULL`,
			a.ExportID, siteID, title)
		return err
	})
	slog.Error("audit pack: a stored original no longer matches its seal", "export", a.ExportID, "site", siteID,
		"object", seal.Key, "version", seal.Version, "kind", seal.Kind, "id", seal.ID)
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (b *Builder) keepLease(ctx context.Context, cancel context.CancelCauseFunc, a *Attempt) {
	t := time.NewTicker(b.lease() / 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := b.Extend(ctx, a); err != nil {
				cancel(fmt.Errorf("lost the lease: %w", err))
				return
			}
			if b.Heartbeat != nil {
				b.Heartbeat(ctx)
			}
		}
	}
}

// record: the result and its contents in one transaction, checked at commit.
func (b *Builder) record(ctx context.Context, a *Attempt, res *Result, key, version string) error {
	head, err := hex.DecodeString(res.Chain.HeadHash)
	if err != nil {
		return err
	}
	if res.Chain.Length == 0 {
		head = nil
	}
	return database.WithTenant(ctx, b.Pool, database.Tenant{OrgID: a.OrgID}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_export_results (export_id, org_id, attempt_id, sha256, size_bytes, object_key, object_version_id,
			                                  evidence_count, file_count, chain_length, chain_head, chain_intact)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			a.ExportID, a.OrgID, a.ID, res.SHA256, res.SizeBytes, key, version, len(res.EvidenceIDs), len(res.FileIDs),
			res.Chain.Length, head, res.Chain.Intact); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_export_evidence (org_id, export_id, site_id, evidence_id)
			SELECT $1, $2, e.site_id, e.id FROM evidence e WHERE e.id = ANY($3::uuid[])`,
			a.OrgID, a.ExportID, res.EvidenceIDs); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO audit_export_files (org_id, export_id, file_id) SELECT $1, $2, unnest($3::uuid[])`,
			a.OrgID, a.ExportID, res.FileIDs)
		return err
	})
}

// MaxPackSize is the largest pack built: one S3 object. A var, so a test
// can lower it.
var MaxPackSize int64 = storage.MaxObjectSize

// ErrPackTooLarge: refused for good (outcome 'refused'), never retried.
var ErrPackTooLarge = errors.New("audit pack too large")

type prepared struct {
	m     *Manifest
	files []packFile
	docs  *packDocs
	size  int64 // an upper bound
}

func (b *Builder) prepare(ctx context.Context, orgID, exportID string) (*prepared, error) {
	m, files, err := b.assemble(ctx, orgID, exportID)
	if err != nil {
		return nil, err
	}
	docs, err := renderDocs(m)
	if err != nil {
		return nil, err
	}
	return &prepared{m: m, files: files, docs: docs, size: estimateSize(docs, files)}, nil
}

func (b *Builder) write(ctx context.Context, p *prepared, w io.Writer) (*Result, error) {
	h := sha256.New()
	cw := &countingWriter{}
	if err := writePack(ctx, io.MultiWriter(w, h, cw), p.docs, p.files, b.Evidence.ReadVersion); err != nil {
		return nil, err
	}
	res := &Result{SHA256: h.Sum(nil), SizeBytes: cw.n, Chain: p.m.Chain.Verification, EvidenceIDs: []string{}, FileIDs: []string{}}
	for _, f := range p.m.Files {
		if f.Kind == "evidence" {
			res.EvidenceIDs = append(res.EvidenceIDs, f.ID)
		} else {
			res.FileIDs = append(res.FileIDs, f.ID)
		}
	}
	return res, nil
}

// BuildTo depends on nothing but the data as of the export's as_of (D10).
func (b *Builder) BuildTo(ctx context.Context, orgID, exportID string, w io.Writer) (*Result, error) {
	p, err := b.prepare(ctx, orgID, exportID)
	if err != nil {
		return nil, err
	}
	return b.write(ctx, p, w)
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// ErrPackFormat: this build can't give the bytes the export was queued for (D10).
var ErrPackFormat = errors.New("this version doesn't build the export's pack format")

// assemble: one snapshot, the session in UTC so times print the same way.
func (b *Builder) assemble(ctx context.Context, orgID, exportID string) (*Manifest, []packFile, error) {
	m := &Manifest{Format: PackFormat}
	var files []packFile
	err := database.WithTenantSnapshot(ctx, b.Pool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL TimeZone = 'UTC'`); err != nil {
			return err
		}
		e := &m.Export
		if err := tx.QueryRow(ctx, `
			SELECT id::text, site_id::text, from_date::text, to_date::text, time_zone, as_of, requested_by::text, requester_role,
			       includes_health, pack_format, regime_code, regime_version, encode(regime_data_sha256, 'hex'), client_requested_at
			FROM audit_exports WHERE id = $1`, exportID).Scan(&e.ID, &e.SiteID, &e.From, &e.To, &e.TimeZone, &e.AsOf, &e.RequestedBy,
			&e.RequesterRole, &e.IncludesHealth, &e.PackFormat, &e.RegimeCode, &e.RegimeVersion, &e.RegimeDataSHA256,
			&e.ClientRequestedAt); err != nil {
			return fmt.Errorf("read export: %w", err)
		}
		if e.PackFormat != PackFormat {
			return fmt.Errorf("%w (%d)", ErrPackFormat, e.PackFormat)
		}
		e.AsOf, e.ClientRequestedAt = e.AsOf.UTC(), e.ClientRequestedAt.UTC()
		m.Notes = notes(e)

		for _, x := range []struct {
			dst *json.RawMessage
			sql string
			arg string
		}{{&m.Organisation, organisationSQL, orgID}, {&m.Site, siteSQL, e.SiteID}, {&m.Members, membersSQL, orgID}} {
			var raw []byte
			if err := tx.QueryRow(ctx, x.sql, x.arg, e.AsOf).Scan(&raw); err != nil {
				return fmt.Errorf("read header: %w", err)
			}
			*x.dst = raw
		}

		q := Query{OrgID: orgID, SiteID: e.SiteID, AsOf: e.AsOf, From: e.From, To: e.To, TimeZone: e.TimeZone, IncludesHealth: e.IncludesHealth}
		sections, err := coreSections(ctx, tx, q)
		if err != nil {
			return err
		}
		if b.Contributor != nil {
			more, err := b.Contributor.PackSections(ctx, tx, q)
			if err != nil {
				return err
			}
			sections = append(sections, more...)
		}
		m.Sections = orderSections(sections)

		m.Chain.Entries = []evidence.ChainRecord{}
		v, err := evidence.VerifyChainAsOf(ctx, tx, orgID, e.SiteID, e.AsOf, func(r evidence.ChainRecord) error {
			m.Chain.Entries = append(m.Chain.Entries, r)
			return nil
		})
		if err != nil {
			return err
		}
		m.Chain.Verification = *v

		files, err = packFiles(ctx, tx, q, sections)
		if err != nil {
			return err
		}
		m.Files = make([]ManifestFile, 0, len(files))
		for _, f := range files {
			m.Files = append(m.Files, f.ManifestFile)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("assemble pack: %w", err)
	}
	return m, files, nil
}

// Section order: an inspector's (Rev 3 §2.3.10), then the rest.
var sectionOrder = []string{"vault", "appointments", "hs_file", "workers", "toolbox_talks", "inspections", "actions",
	"incidents", "diary", "sign_ins", "received_photos"}

func orderSections(in []Section) []Section {
	out := make([]Section, 0, len(in))
	for _, k := range sectionOrder {
		for _, s := range in {
			if s.Key == k {
				out = append(out, s)
			}
		}
	}
	for _, s := range in {
		known := false
		for _, k := range sectionOrder {
			known = known || s.Key == k
		}
		if !known {
			out = append(out, s)
		}
	}
	return out
}

// packFiles: only photos sealed into the chain by as_of.
func packFiles(ctx context.Context, tx pgx.Tx, q Query, sections []Section) ([]packFile, error) {
	var evidenceIDs, fileIDs []string
	for _, s := range sections {
		evidenceIDs = append(evidenceIDs, s.EvidenceIDs...)
		fileIDs = append(fileIDs, s.FileIDs...)
	}
	out := []packFile{}
	rows, err := tx.Query(ctx, `
		SELECT e.id::text, e.object_key, e.object_version_id, encode(e.content_sha256, 'hex'), e.size_bytes, e.content_type
		FROM evidence e JOIN evidence_chain c ON c.evidence_id = e.id
		WHERE e.org_id = $1 AND e.site_id = $2 AND e.id = ANY($3::uuid[]) AND c.server_received_at <= $4
		ORDER BY e.id`, q.OrgID, q.SiteID, evidenceIDs, q.AsOf)
	if err != nil {
		return nil, fmt.Errorf("pack photos: %w", err)
	}
	ev, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (packFile, error) {
		f := packFile{ManifestFile: ManifestFile{Kind: "evidence"}}
		err := r.Scan(&f.ID, &f.key, &f.version, &f.SHA256, &f.SizeBytes, &f.ContentType)
		f.Path = filePath(f.Kind, f.ID, f.ContentType)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("pack photos: %w", err)
	}
	out = append(out, ev...)
	rows, err = tx.Query(ctx, `
		SELECT f.id::text, f.object_key, f.object_version_id, encode(f.content_sha256, 'hex'), f.size_bytes, f.content_type
		FROM files f WHERE f.org_id = $1 AND f.id = ANY($2::uuid[]) AND f.status = 'verified'
		ORDER BY f.id`, q.OrgID, fileIDs)
	if err != nil {
		return nil, fmt.Errorf("pack files: %w", err)
	}
	docs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (packFile, error) {
		f := packFile{ManifestFile: ManifestFile{Kind: "document"}}
		err := r.Scan(&f.ID, &f.key, &f.version, &f.SHA256, &f.SizeBytes, &f.ContentType)
		f.Path = filePath(f.Kind, f.ID, f.ContentType)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("pack files: %w", err)
	}
	out = append(out, docs...)
	return out, nil
}

func notes(e *ManifestExport) []string {
	n := []string{
		"Records as the server had them at the cut-off (as_of, UTC). Anything received later, even if it happened in the period, is not in this pack.",
		"Dates are local dates in " + e.TimeZone + ". The period is " + e.From + " to " + e.To + ", both included.",
		"Names, roles and site details are as they were at the cut-off, not as they are now.",
		"Inspections still in progress at the cut-off show their header only: their answers could still change.",
		"Photos: those received in the period, and those attached to a record in the pack. Each was re-hashed against its seal as it was written into the pack.",
		"The evidence chain lists every entry received by the cut-off with every input of its hash; its head hash and length are this site's record outside SiteProof's database.",
	}
	if e.IncludesHealth {
		n = append(n, "Medical certificates of fitness and injury and treatment details are included: the export was requested by "+
			roleInWords(e.RequesterRole)+", a role allowed to see them (POPIA s26 health information; SiteProof M12 D8).")
	} else {
		n = append(n, "Medical certificates of fitness and injury and treatment details are left out: the requester's role may not see them (POPIA s26).")
	}
	return n
}

func roleInWords(role string) string {
	switch role {
	case auth.RoleOwner:
		return "an owner"
	case auth.RoleSHEOfficer:
		return "a SHE officer"
	}
	return "a " + strings.ReplaceAll(role, "_", " ")
}

// finish records a terminal outcome ('refused'), whatever the lease.
func (b *Builder) finish(ctx context.Context, a *Attempt, outcome string, cause error) error {
	return database.WithTenant(ctx, b.Pool, database.Tenant{OrgID: a.OrgID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE audit_export_attempts SET outcome = $2, error = $3, finished_at = clock_timestamp()
			WHERE id = $1 AND finished_at IS NULL`, a.ID, outcome, truncate(cause.Error(), 2000))
		return err
	})
}
