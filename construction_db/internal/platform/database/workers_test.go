package database_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

func workerID(n int) string { return fmt.Sprintf("41000000-0000-0000-0000-%012d", n) }
func compID(n int) string   { return fmt.Sprintf("42000000-0000-0000-0000-%012d", n) }
func medID(n int) string    { return fmt.Sprintf("43000000-0000-0000-0000-%012d", n) }

const (
	// $1 id, $2 org, $3 name, $4 firm, $5 clock_number, $6 created_by.
	workerSQL = `INSERT INTO workers (id, org_id, name, firm, clock_number, created_by) VALUES ($1, $2, $3, $4, $5, $6)`
	// $1 id, $2 org, $3 worker, $4 type, $5 file, $6 supersedes, $7 recorded_by.
	compSQL = `
		INSERT INTO worker_competencies (id, org_id, worker_id, regime_code, type_key, issued_on, expires_on, file_id, supersedes_id,
		                                 recorded_by, client_recorded_at)
		VALUES ($1, $2, $3, 'za_construction_2014', $4, '2026-01-01', '2027-01-01', $5, $6, $7, now())`
	// $1 id, $2 org, $3 worker, $4 outcome, $5 file, $6 supersedes, $7 recorded_by.
	medSQL = `
		INSERT INTO worker_medicals (id, org_id, worker_id, regime_code, outcome, examined_on, expires_on, file_id, supersedes_id,
		                             recorded_by, client_recorded_at)
		VALUES ($1, $2, $3, 'za_construction_2014', $4, '2026-01-01', '2027-01-01', $5, $6, $7, now())`
	retractCompSQL = `UPDATE worker_competencies SET retracted_by = $2, retract_reason = 'Wrong certificate' WHERE id = $1`
)

// verifiedFile inserts a verified file of purpose in org A and returns its id.
func verifiedFile(t *testing.T, tx pgx.Tx, n int, purpose string) string {
	t.Helper()
	id := fileID(n)
	must(t, tx, 1, fileSQL, id, rlsOrgA, nil, purpose, rlsUserA, hash32(byte(n)), "verified")
	return id
}

func TestWorkerRules(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)

	t.Run("one worker per identity key, however it is spelt", func(t *testing.T) {
		must(t, tx, 1, workerSQL, workerID(1), rlsOrgA, "Thabo Mokoena", "ABC Builders", "C-17", rlsUserA)
		refused(t, tx, "23505", "workers_identity", workerSQL, workerID(2), rlsOrgA, "  thabo\tMOKOENA ", "abc  builders", "c-17", rlsUserA)
		// A different firm or clock number is a different person.
		must(t, tx, 1, workerSQL, workerID(3), rlsOrgA, "Thabo Mokoena", "ABC Builders", nil, rlsUserA)
		must(t, tx, 1, workerSQL, workerID(4), rlsOrgA, "Thabo Mokoena", nil, nil, rlsUserA)
		var key string
		if err := tx.QueryRow(context.Background(), `SELECT identity_key FROM workers WHERE id = $1`, workerID(1)).Scan(&key); err != nil || key != "thabo mokoena|abc builders|c-17" {
			t.Errorf("identity_key %q, err %v", key, err)
		}
	})
	t.Run("a worker is edited, never deleted", func(t *testing.T) {
		must(t, tx, 1, `UPDATE workers SET trade = 'Scaffolder', active = false, updated_by = $2 WHERE id = $1`, workerID(4), rlsUserA)
		refused(t, tx, "23514", "only the name, firm, clock number, trade and active can change",
			`UPDATE workers SET created_by = $2 WHERE id = $1`, workerID(4), supOnSite)
		refused(t, tx, "42501", "", `DELETE FROM workers WHERE id = $1`, workerID(4))
	})

	f1 := verifiedFile(t, tx, 101, "competency")
	t.Run("a competency needs a verified certificate file of its own purpose", func(t *testing.T) {
		must(t, tx, 1, fileSQL, fileID(102), rlsOrgA, nil, "competency", rlsUserA, hash32(102), "pending")
		refused(t, tx, "23503", "", compSQL, compID(9), rlsOrgA, workerID(1), "scaffold_erector", fileID(102), nil, rlsUserA)
		med := verifiedFile(t, tx, 103, "medical")
		refused(t, tx, "23503", "", compSQL, compID(9), rlsOrgA, workerID(1), "scaffold_erector", med, nil, rlsUserA)
		must(t, tx, 1, compSQL, compID(1), rlsOrgA, workerID(1), "scaffold_erector", f1, nil, rlsUserA)
		// A file belongs to one record.
		refused(t, tx, "23505", "", compSQL, compID(9), rlsOrgA, workerID(3), "scaffold_erector", f1, nil, rlsUserA)
	})

	t.Run("a renewal is of the same worker and type, once", func(t *testing.T) {
		f2, f3, f4 := verifiedFile(t, tx, 104, "competency"), verifiedFile(t, tx, 105, "competency"), verifiedFile(t, tx, 106, "competency")
		refused(t, tx, "23503", "", compSQL, compID(2), rlsOrgA, workerID(3), "scaffold_erector", f2, compID(1), rlsUserA)
		refused(t, tx, "23503", "", compSQL, compID(2), rlsOrgA, workerID(1), "scaffold_inspector", f2, compID(1), rlsUserA)
		must(t, tx, 1, compSQL, compID(2), rlsOrgA, workerID(1), "scaffold_erector", f2, compID(1), rlsUserA)
		refused(t, tx, "23505", "worker_competencies_one_successor", compSQL, compID(3), rlsOrgA, workerID(1), "scaffold_erector", f3, compID(1), rlsUserA)
		// Retracting the renewal frees its place.
		must(t, tx, 1, retractCompSQL, compID(2), sheOfficer)
		must(t, tx, 1, compSQL, compID(3), rlsOrgA, workerID(1), "scaffold_erector", f3, compID(1), rlsUserA)
		// A retracted record can't be renewed.
		refused(t, tx, "23514", "a retracted record can't be renewed", compSQL, compID(4), rlsOrgA, workerID(1), "scaffold_erector", f4, compID(2), rlsUserA)
	})

	t.Run("a record changes only by a lead's retraction, once", func(t *testing.T) {
		refused(t, tx, "23514", "only an owner or SHE officer can retract it", retractCompSQL, compID(1), supOnSite)
		refused(t, tx, "23514", "only an owner or SHE officer can retract it", retractCompSQL, compID(1), suspendedSO)
		refused(t, tx, "42501", "", `UPDATE worker_competencies SET expires_on = '2030-01-01' WHERE id = $1`, compID(1))
		refused(t, tx, "23514", "already retracted", retractCompSQL, compID(2), rlsUserA)
		refused(t, tx, "23514", "the only change is a retraction", `UPDATE worker_competencies SET retract_reason = 'x' WHERE id = $1`, compID(1))
		refused(t, tx, "23514", "never at insert", `
			INSERT INTO worker_competencies (id, org_id, worker_id, regime_code, type_key, issued_on, file_id, recorded_by, client_recorded_at,
			                                 retracted_at, retracted_by, retract_reason)
			VALUES ($1, $2, $3, 'r', 'k', '2026-01-01', $4, $5, now(), now(), $5, 'x')`, compID(8), rlsOrgA, workerID(3), verifiedFile(t, tx, 107, "competency"), rlsUserA)
		refused(t, tx, "42501", "", `DELETE FROM worker_competencies WHERE id = $1`, compID(1))
		refused(t, tx, "23514", "client_recorded_at is in the future", `
			INSERT INTO worker_competencies (id, org_id, worker_id, regime_code, type_key, issued_on, file_id, recorded_by, client_recorded_at)
			VALUES ($1, $2, $3, 'r', 'k', '2026-01-01', $4, $5, now() + interval '6 minutes')`, compID(8), rlsOrgA, workerID(3), verifiedFile(t, tx, 108, "competency"), rlsUserA)
	})

	t.Run("a medical is an outcome, dates, a practice number and a certificate", func(t *testing.T) {
		m := verifiedFile(t, tx, 110, "medical")
		refused(t, tx, "23514", "worker_medicals_outcome_check", medSQL, medID(1), rlsOrgA, workerID(1), "fit_for_heights", m, nil, rlsUserA)
		must(t, tx, 1, medSQL, medID(1), rlsOrgA, workerID(1), "fit_with_restrictions", m, nil, rlsUserA)
		refused(t, tx, "23514", "worker_medicals_practice_number_check", `
			INSERT INTO worker_medicals (id, org_id, worker_id, regime_code, outcome, examined_on, expires_on, practice_number, file_id,
			                             recorded_by, client_recorded_at)
			VALUES ($1, $2, $3, 'r', 'fit', '2026-01-01', '2027-01-01', 'epilepsy; no work at height', $4, $5, now())`,
			medID(3), rlsOrgA, workerID(3), verifiedFile(t, tx, 112, "medical"), rlsUserA)
		must(t, tx, 1, `
			INSERT INTO worker_medicals (id, org_id, worker_id, regime_code, outcome, examined_on, expires_on, practice_number, file_id,
			                             recorded_by, client_recorded_at)
			VALUES ($1, $2, $3, 'r', 'fit', '2026-01-01', '2027-01-01', 'MP 0123456', $4, $5, now())`,
			medID(4), rlsOrgA, workerID(3), verifiedFile(t, tx, 113, "medical"), rlsUserA)
		refused(t, tx, "23503", "", medSQL, medID(2), rlsOrgA, workerID(1), "fit", verifiedFile(t, tx, 111, "competency"), nil, rlsUserA)
		// The table has no column a diagnosis could go in.
		rows, err := tx.Query(context.Background(), `
			SELECT column_name FROM information_schema.columns WHERE table_name = 'worker_medicals' AND data_type = 'text' ORDER BY column_name`)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := pgx.CollectRows(rows, pgx.RowTo[string])
		want := []string{"device_id", "file_purpose", "file_status", "outcome", "practice_number", "regime_code", "retract_reason"}
		if err != nil || !slices.Equal(cols, want) {
			t.Errorf("worker_medicals text columns %v, err %v; want %v (no free text beyond the practice number)", cols, err, want)
		}
	})

}

// A test of its own: TRUNCATE waits for every open transaction on the table.
func TestWorkerTablesRefuseTruncate(t *testing.T) {
	seedRLS(t)
	refuseTruncates(t, beginAs(t, admin, ""), "toolbox_attendance_workers", "worker_medicals", "worker_competencies", "workers")
}

func TestSignatureLinks(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	okBatch(t, tx, talkA(talkID(500), talkStart, rlsUserA),
		signA(attID(501), talkID(500), "Sipho Dlamini", nil, "tap", rlsUserA, signTime),
		signA(attID(502), talkID(500), "Sipho Dlamini", "C-9", "tap", rlsUserA, signTime))
	const link = `INSERT INTO toolbox_attendance_workers (org_id, site_id, attendance_id, worker_id, method, linked_by) VALUES ($1, $2, $3, $4, $5, $6)`
	const unlink = `UPDATE toolbox_attendance_workers SET unlinked_by = $2, unlink_reason = 'Different person' WHERE attendance_id = $1 AND unlinked_at IS NULL`
	person := func(att string) string {
		t.Helper()
		var p string
		if err := tx.QueryRow(ctx, `SELECT toolbox_attendance_person($1)`, att).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("without a worker, a signature is its identity key", func(t *testing.T) {
		if p := person(attID(501)); p != "k:sipho dlamini||" {
			t.Errorf("person %q", p)
		}
	})
	must(t, tx, 1, workerSQL, workerID(50), rlsOrgA, "SIPHO  dlamini", nil, nil, rlsUserA)
	must(t, tx, 1, workerSQL, workerID(51), rlsOrgA, "Sipho Dlamini", nil, "C-10", rlsUserA)
	t.Run("a register worker with the same key is that person, linked or not", func(t *testing.T) {
		if p := person(attID(501)); p != "w:"+workerID(50) {
			t.Errorf("person %q", p)
		}
		// C-9 isn't C-10: no partial matches.
		if p := person(attID(502)); p != "k:sipho dlamini||c-9" {
			t.Errorf("person %q", p)
		}
	})
	t.Run("an automatic link needs the same key; a manual one a lead", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "an automatic link needs the same identity key",
			stmt{link, []any{rlsOrgA, rlsSiteA, attID(502), workerID(51), "auto", supOnSite}}, stmt{`SET CONSTRAINTS ALL IMMEDIATE`, nil})
		refusedBatch(t, tx, "23514", "only an owner or SHE officer can link a signature by hand",
			stmt{link, []any{rlsOrgA, rlsSiteA, attID(502), workerID(51), "manual", supOnSite}}, stmt{`SET CONSTRAINTS ALL IMMEDIATE`, nil})
		okBatch(t, tx, stmt{link, []any{rlsOrgA, rlsSiteA, attID(502), workerID(51), "manual", sheOfficer}}, stmt{`SET CONSTRAINTS ALL IMMEDIATE`, nil})
		if p := person(attID(502)); p != "w:"+workerID(51) {
			t.Errorf("person %q", p)
		}
		refused(t, tx, "23505", "toolbox_attendance_workers_one_live", link, rlsOrgA, rlsSiteA, attID(502), workerID(50), "manual", sheOfficer)
	})
	t.Run("unlinking a different person keeps them apart", func(t *testing.T) {
		okBatch(t, tx, stmt{link, []any{rlsOrgA, rlsSiteA, attID(501), workerID(50), "auto", supOnSite}}, stmt{`SET CONSTRAINTS ALL IMMEDIATE`, nil})
		refused(t, tx, "23514", "only an owner or SHE officer can unlink it", unlink, attID(501), supOnSite)
		must(t, tx, 1, unlink, attID(501), sheOfficer)
		if p := person(attID(501)); p != "k:sipho dlamini||" {
			t.Errorf("after unlinking, person %q; want the key, not the worker it was unlinked from", p)
		}
		refused(t, tx, "23514", "already unlinked", `UPDATE toolbox_attendance_workers SET unlink_reason = 'x' WHERE attendance_id = $1`, attID(501))
		refused(t, tx, "42501", "", `DELETE FROM toolbox_attendance_workers WHERE attendance_id = $1`, attID(501))
	})
}
