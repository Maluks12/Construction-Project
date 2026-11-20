package toolbox

// notSignedSQL: workers on the talk's site who didn't sign it. A sign-in
// counts when it is not retracted, its presence hadn't ended when the talk
// began, and either it was standing at the talk's start (whatever its date:
// night shifts sign in before midnight, decided Fri 20 Nov) or it was made
// that day before the talk ended.
const notSignedSQL = `
	SELECT DISTINCT ON (lower(w.name), s.worker_id) s.worker_id::text, w.name, w.firm, w.clock_number, s.id::text
	FROM toolbox_talks t
	JOIN site_sign_ins s ON s.site_id = t.site_id AND s.retracted_at IS NULL
	                    AND site_sign_in_ends_at(s) > t.started_at
	                    AND (s.client_signed_in_at <= t.started_at
	                         OR (s.sign_in_date = t.talk_date AND s.client_signed_in_at <= coalesce(t.ended_at, now())))
	JOIN workers w ON w.id = s.worker_id
	WHERE t.org_id = $1 AND t.id = $2
	  AND NOT EXISTS (
	      SELECT 1 FROM toolbox_attendance a
	      WHERE a.talk_id = t.id
	        AND NOT EXISTS (SELECT 1 FROM toolbox_attendance_retractions r WHERE r.attendance_id = a.id)
	        AND toolbox_attendance_person(a.id) = 'w:' || s.worker_id)
	ORDER BY lower(w.name), s.worker_id, s.client_signed_in_at`
