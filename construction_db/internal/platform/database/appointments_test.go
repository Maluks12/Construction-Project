package database_test

import (
	"fmt"
	"testing"
)

func apptID(n int) string { return fmt.Sprintf("48000000-0000-0000-0000-%012d", n) }

// $1 id, $2 org, $3 site, $4 worker, $5 type, $6 file, $7 supersedes, $8 recorded_by.
const appointmentSQL = `
	INSERT INTO site_appointments (id, org_id, site_id, worker_id, regime_code, type_key, appointed_on, file_id, supersedes_id,
	                               recorded_by, client_recorded_at)
	VALUES ($1, $2, $3, $4, 'za_construction_2014', $5, '2026-11-01', $6, $7, $8, now())`

func TestAppointmentRules(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	w1, w2 := workerID(401), workerID(402)
	must(t, tx, 1, workerSQL, w1, rlsOrgA, "Construction Manager", nil, nil, rlsUserA)
	must(t, tx, 1, workerSQL, w2, rlsOrgA, "Next Manager", nil, nil, rlsUserA)
	letter := func(n int) string {
		t.Helper()
		id := fileID(n)
		must(t, tx, 1, fileSQL, id, rlsOrgA, rlsSiteA, "appointment", rlsUserA, hash32(byte(n)), "verified")
		return id
	}

	refusedBatch(t, tx, "23514", "only an owner or SHE officer records appointments",
		stmt{appointmentSQL, []any{apptID(1), rlsOrgA, rlsSiteA, w1, "construction_manager", letter(401), nil, supOnSite}})
	okBatch(t, tx, stmt{appointmentSQL, []any{apptID(1), rlsOrgA, rlsSiteA, w1, "construction_manager", letter(402), nil, sheOfficer}})
	// A letter for another purpose doesn't do.
	refusedBatch(t, tx, "23503", "", stmt{appointmentSQL, []any{apptID(2), rlsOrgA, rlsSiteA, w2, "construction_manager", verifiedFile(t, tx, 403, "competency"), nil, rlsUserA}})
	// The successor is the same type on the same site; the appointee may change.
	refusedBatch(t, tx, "23503", "", stmt{appointmentSQL, []any{apptID(2), rlsOrgA, rlsSiteA, w2, "risk_assessor", letter(404), apptID(1), rlsUserA}})
	okBatch(t, tx, stmt{appointmentSQL, []any{apptID(2), rlsOrgA, rlsSiteA, w2, "construction_manager", letter(405), apptID(1), rlsUserA}})
	refusedBatch(t, tx, "23505", "site_appointments_one_successor",
		stmt{appointmentSQL, []any{apptID(3), rlsOrgA, rlsSiteA, w2, "construction_manager", letter(406), apptID(1), rlsUserA}})
	refused(t, tx, "23514", "only an owner or SHE officer can retract it",
		`UPDATE site_appointments SET retracted_by = $2, retract_reason = 'x' WHERE id = $1`, apptID(2), supOnSite)
	must(t, tx, 1, `UPDATE site_appointments SET retracted_by = $2, retract_reason = 'Wrong letter' WHERE id = $1`, apptID(2), sheOfficer)
	refused(t, tx, "42501", "", `DELETE FROM site_appointments WHERE id = $1`, apptID(1))
}
