package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/toolbox"
)

func talkPath(site, id string) string { return "/v1/sites/" + site + "/toolbox-talks/" + id }
func signPath(site, talk, id string) string {
	return talkPath(site, talk) + "/attendance/" + id
}
func talkPhotoPath(site, talk, evidence string) string {
	return talkPath(site, talk) + "/photos/" + evidence
}

// A talk on Sat 14 Nov at 08:30 SAST; people sign from 09:00. Fixed times,
// so a run near midnight can't put a signature on another local day.
var (
	talkAt = time.Date(2026, 11, 14, 6, 30, 0, 0, time.UTC)
	signAt = time.Date(2026, 11, 14, 7, 0, 0, 0, time.UTC)
)

func talkBody(fields map[string]any) map[string]any {
	body := map[string]any{"topic_key": "scaffolding", "presenter_name": "Thabo M", "started_at": rfc(talkAt)}
	for k, v := range fields {
		body = with(body, k, v)
	}
	return body
}

func signBody(name string, at time.Time, fields map[string]any) map[string]any {
	body := map[string]any{"name": name, "method": "tap", "client_signed_at": rfc(at)}
	for k, v := range fields {
		body = with(body, k, v)
	}
	return body
}

func decodeTalk(t *testing.T, rec *httptest.ResponseRecorder) toolbox.Talk {
	t.Helper()
	var v toolbox.Talk
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode talk: %v (body: %s)", err, rec.Body.String())
	}
	return v
}

func decodeAttendance(t *testing.T, rec *httptest.ResponseRecorder) toolbox.Attendance {
	t.Helper()
	var v toolbox.Attendance
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode attendance: %v (body: %s)", err, rec.Body.String())
	}
	return v
}

func listTalks(t *testing.T, router http.Handler, token, site, query string) []toolbox.TalkSummary {
	t.Helper()
	rec := do(t, router, http.MethodGet, "/v1/sites/"+site+"/toolbox-talks"+query, token, nil)
	wantStatus(t, rec, http.StatusOK)
	var body struct {
		Talks []toolbox.TalkSummary `json:"talks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode talks: %v (body: %s)", err, rec.Body.String())
	}
	return body.Talks
}

func TestToolboxTopics(t *testing.T) {
	f := newEvidenceFixture(t, "topics")
	path := "/v1/sites/" + f.siteA + "/toolbox-topics"
	rec := do(t, f.router, http.MethodGet, path, f.supervisor, nil)
	wantStatus(t, rec, http.StatusOK)
	var body struct {
		Topics []struct{ Key, Title, SourceRef string } `json:"topics"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Topics) != 9 || body.Topics[2].Key != "scaffolding" {
		t.Errorf("topics %+v; want the regime's 9, scaffolding third", body.Topics)
	}
	wantStatus(t, do(t, f.router, http.MethodGet, path, f.bookkeeper, nil), http.StatusForbidden)
	wantStatus(t, do(t, f.router, http.MethodGet, path, f.other, nil), http.StatusNotFound)
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteB+"/toolbox-topics", f.supervisor, nil), http.StatusNotFound)
}

func TestToolboxTalk(t *testing.T) {
	f := newEvidenceFixture(t, "talk")
	supervisor := me(t, f.router, f.supervisor).UserID
	id := uuid.NewString()

	// Created with the topic's title from the regime.
	rec := do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(nil))
	wantStatus(t, rec, http.StatusCreated)
	talk := decodeTalk(t, rec)
	if talk.Topic != "Scaffolding: competent erection and inspection" || talk.TalkDate != "2026-11-14" ||
		talk.RecordedBy != supervisor || talk.TimeZone != "Africa/Johannesburg" || talk.UpdatedAt != nil ||
		len(talk.Attendance) != 0 || len(talk.Photos) != 0 {
		t.Errorf("created talk %+v", talk)
	}
	// An identical repeat changes nothing.
	rec = do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(nil))
	wantStatus(t, rec, http.StatusOK)
	if decodeTalk(t, rec).UpdatedAt != nil {
		t.Error("an identical repeat set updated_at")
	}
	// Until someone signs, anything can change, by anyone who can write talks.
	rec = do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.owner, talkBody(map[string]any{"topic": "Scaffold tags", "notes": "Check the tags"}))
	wantStatus(t, rec, http.StatusOK)
	if got := decodeTalk(t, rec); got.Topic != "Scaffold tags" || got.Notes == nil || got.UpdatedAt == nil || got.RecordedBy != supervisor {
		t.Errorf("edited talk %+v", got)
	}

	t.Run("photos", func(t *testing.T) {
		photo := photoTakenAt(t, f, f.supervisor, f.siteA, time.Now())
		wantStatus(t, do(t, f.router, http.MethodPut, talkPhotoPath(f.siteA, id, photo), f.supervisor, map[string]any{"kind": "photo"}), http.StatusCreated)
		rec := do(t, f.router, http.MethodPut, talkPhotoPath(f.siteA, id, photo), f.supervisor, map[string]any{"kind": "photo"})
		wantStatus(t, rec, http.StatusOK)
		if got := decodeTalk(t, rec).Photos; len(got) != 1 || got[0].EvidenceID != photo || got[0].Kind != "photo" {
			t.Errorf("photos %+v", got)
		}
		wantError(t, do(t, f.router, http.MethodPut, talkPhotoPath(f.siteA, id, photo), f.supervisor, map[string]any{"kind": "paper_register"}),
			http.StatusConflict, toolbox.ErrPhotoKindDiffers.Error())
	})

	t.Run("three people sign", func(t *testing.T) {
		for i, name := range []string{"Sipho N", "Lerato K", "Johan V"} {
			rec := do(t, f.router, http.MethodPut, signPath(f.siteA, id, uuid.NewString()), f.supervisor,
				signBody(name, signAt.Add(time.Duration(i)*time.Minute), map[string]any{"firm": "Own team"}))
			wantStatus(t, rec, http.StatusCreated)
			if a := decodeAttendance(t, rec); a.Name != name || a.RecordedBy != supervisor || a.Method != "tap" || a.Retraction != nil {
				t.Errorf("signature %+v", a)
			}
		}
		att := uuid.NewString()
		body := signBody("Thandi Z", signAt, map[string]any{"clock_number": "C-17"})
		wantStatus(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, att), f.supervisor, body), http.StatusCreated)
		wantStatus(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, att), f.supervisor, body), http.StatusOK)
		// The same body from another phone isn't a repeat: the recorder differs.
		wantError(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, att), f.owner, body), http.StatusConflict, toolbox.ErrAttendanceIDReused.Error())
		wantError(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, att), f.supervisor, signBody("Someone else", signAt, nil)),
			http.StatusConflict, toolbox.ErrAttendanceIDReused.Error())
		// The same person under a new id: case and spacing don't count.
		wantAlreadySigned(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, uuid.NewString()), f.supervisor,
			signBody("  sipho   n", signAt, map[string]any{"firm": "own  TEAM"})))
	})

	t.Run("after a signature only the end time can be set, once", func(t *testing.T) {
		wantError(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(map[string]any{"topic": "Other"})),
			http.StatusConflict, toolbox.ErrTalkSigned.Error())
		ended := talkBody(map[string]any{"topic": "Scaffold tags", "notes": "Check the tags", "ended_at": rfc(signAt.Add(20 * time.Minute))})
		rec := do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, ended)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeTalk(t, rec); got.EndedAt == nil || got.SignedCount != 4 {
			t.Errorf("talk %+v; want ended_at set, 4 signed", got)
		}
		wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, ended), http.StatusOK)
		wantError(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor,
			with(ended, "ended_at", rfc(signAt.Add(30*time.Minute)))), http.StatusConflict, toolbox.ErrTalkSigned.Error())
	})

	t.Run("retraction", func(t *testing.T) {
		got := decodeTalk(t, do(t, f.router, http.MethodGet, talkPath(f.siteA, id), f.owner, nil))
		first := got.Attendance[0].ID
		path := signPath(f.siteA, id, first) + "/retract"
		body := map[string]any{"reason": "Tapped the wrong name", "client_retracted_at": rfc(time.Now())}
		rec := do(t, f.router, http.MethodPost, path, f.supervisor, body)
		wantStatus(t, rec, http.StatusCreated)
		if a := decodeAttendance(t, rec); a.Retraction == nil || a.Retraction.RetractedBy != supervisor {
			t.Errorf("retracted %+v", a)
		}
		wantStatus(t, do(t, f.router, http.MethodPost, path, f.supervisor, body), http.StatusOK)
		wantError(t, do(t, f.router, http.MethodPost, path, f.supervisor, with(body, "reason", "Other")),
			http.StatusConflict, toolbox.ErrAlreadyRetracted.Error())
		got = decodeTalk(t, do(t, f.router, http.MethodGet, talkPath(f.siteA, id), f.owner, nil))
		if got.SignedCount != 3 || len(got.Attendance) != 4 {
			t.Errorf("signed_count %d, attendance %d; want 3 and 4 (the retracted one stays)", got.SignedCount, len(got.Attendance))
		}
	})

	t.Run("the register", func(t *testing.T) {
		talks := listTalks(t, f.router, f.owner, f.siteA, "?date=2026-11-14")
		if len(talks) != 1 || talks[0].ID != id || talks[0].SignedCount != 3 || talks[0].PhotoCount != 1 {
			t.Errorf("talks on the 14th: %+v", talks)
		}
		if talks := listTalks(t, f.router, f.owner, f.siteA, "?date=2026-11-15"); len(talks) != 0 {
			t.Errorf("talks on the 15th: %+v", talks)
		}
		wantError(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/toolbox-talks?date=14/11", f.owner, nil), http.StatusBadRequest, toolbox.ErrBadDate.Error())
		wantError(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/toolbox-talks?limit=0", f.owner, nil), http.StatusBadRequest, toolbox.ErrBadLimit.Error())
	})
}

func TestToolboxValidation(t *testing.T) {
	f := newEvidenceFixture(t, "talkcheck")
	put := func(id string, body map[string]any) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, body)
	}
	id := uuid.NewString()
	for _, c := range []struct {
		body map[string]any
		want error
	}{
		{talkBody(map[string]any{"topic_key": "ladders"}), toolbox.ErrUnknownTopic},
		{with(talkBody(nil), "topic_key", nil), toolbox.ErrTopicRequired},
		{talkBody(map[string]any{"topic": "   "}), toolbox.ErrBadTopic},
		{talkBody(map[string]any{"presenter_name": ""}), toolbox.ErrBadPresenter},
		{talkBody(map[string]any{"notes": " "}), toolbox.ErrBadNotes},
		{with(talkBody(nil), "started_at", nil), toolbox.ErrNoStartedAt},
		{talkBody(map[string]any{"started_at": ahead(time.Hour)}), toolbox.ErrStartedInFuture},
		{talkBody(map[string]any{"ended_at": rfc(talkAt.Add(-time.Microsecond))}), toolbox.ErrEndsBeforeStart},
		{talkBody(map[string]any{"ended_at": rfc(talkAt.Add(toolbox.SigningWindow + time.Microsecond))}), toolbox.ErrEndsTooLate},
		{talkBody(map[string]any{"ended_at": ahead(time.Hour)}), toolbox.ErrEndedInFuture},
	} {
		wantError(t, put(id, c.body), http.StatusBadRequest, c.want.Error())
	}
	wantError(t, put("not-a-uuid", talkBody(nil)), http.StatusBadRequest, toolbox.ErrBadTalkID.Error())
	// A free-text topic.
	wantStatus(t, put(id, talkBody(map[string]any{"topic_key": nil, "topic": "Snake bites"})), http.StatusCreated)

	sign := func(att string, body map[string]any) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, signPath(f.siteA, id, att), f.supervisor, body)
	}
	att := uuid.NewString()
	for _, c := range []struct {
		body   map[string]any
		status int
		want   error
	}{
		{signBody("", signAt, nil), http.StatusBadRequest, toolbox.ErrBadName},
		{signBody("A", signAt, map[string]any{"firm": ""}), http.StatusBadRequest, toolbox.ErrBadFirm},
		{signBody("A", signAt, map[string]any{"clock_number": ""}), http.StatusBadRequest, toolbox.ErrBadClockNumber},
		{signBody("A", signAt, map[string]any{"method": "fingerprint"}), http.StatusBadRequest, toolbox.ErrBadMethod},
		{with(signBody("A", signAt, nil), "client_signed_at", nil), http.StatusBadRequest, toolbox.ErrNoSignedAt},
		{signBody("A", time.Now().Add(time.Hour), nil), http.StatusBadRequest, toolbox.ErrSignedInFuture},
		{signBody("A", talkAt.Add(-time.Minute), nil), http.StatusBadRequest, toolbox.ErrSignedBeforeStart},
		// 00:30 SAST on the 15th: 16 hours after an 08:30 start with no end.
		{signBody("A", time.Date(2026, 11, 14, 22, 30, 0, 0, time.UTC), nil), http.StatusBadRequest, toolbox.ErrSignedTooLate},
	} {
		wantError(t, sign(att, c.body), c.status, c.want.Error())
	}
	wantError(t, sign("nope", signBody("A", signAt, nil)), http.StatusBadRequest, toolbox.ErrBadAttendanceID.Error())
	wantError(t, do(t, f.router, http.MethodPut, signPath(f.siteA, uuid.NewString(), att), f.supervisor, signBody("A", signAt, nil)),
		http.StatusNotFound, toolbox.ErrTalkNotFound.Error())
	wantError(t, do(t, f.router, http.MethodGet, talkPath(f.siteA, "nope"), f.supervisor, nil), http.StatusNotFound, toolbox.ErrTalkNotFound.Error())
}

func TestToolboxScoping(t *testing.T) {
	f := newEvidenceFixture(t, "talkscope")
	id := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.owner, talkBody(nil)), http.StatusCreated)

	// The supervisor isn't on site B; other organisations see nothing.
	wantError(t, do(t, f.router, http.MethodPut, talkPath(f.siteB, uuid.NewString()), f.supervisor, talkBody(nil)), http.StatusNotFound, "site not found")
	wantError(t, do(t, f.router, http.MethodGet, talkPath(f.siteA, id), f.other, nil), http.StatusNotFound, "site not found")
	wantError(t, do(t, f.router, http.MethodGet, talkPath(f.otherSite, id), f.other, nil), http.StatusNotFound, toolbox.ErrTalkNotFound.Error())
	wantStatus(t, do(t, f.router, http.MethodGet, talkPath(f.siteA, id), f.bookkeeper, nil), http.StatusForbidden)
	// The id belongs to site A.
	wantError(t, do(t, f.router, http.MethodPut, talkPath(f.siteB, id), f.owner, talkBody(nil)), http.StatusConflict, toolbox.ErrTalkIDReused.Error())
	wantError(t, do(t, f.router, http.MethodGet, talkPath(f.siteB, id), f.owner, nil), http.StatusNotFound, toolbox.ErrTalkNotFound.Error())

	t.Run("an archived site keeps its talks and takes signatures", func(t *testing.T) {
		wantStatus(t, do(t, f.router, http.MethodDelete, "/v1/sites/"+f.siteA, f.owner, nil), http.StatusNoContent)
		wantStatus(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, uuid.NewString()), f.owner, signBody("Late signer", signAt, nil)), http.StatusCreated)
		wantStatus(t, do(t, f.router, http.MethodGet, talkPath(f.siteA, id), f.owner, nil), http.StatusOK)
		// A talk from before the archiving is accepted; one started after isn't.
		wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, uuid.NewString()), f.owner, talkBody(nil)), http.StatusCreated)
		wantError(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, uuid.NewString()), f.owner,
			talkBody(map[string]any{"started_at": ahead(time.Minute)})), http.StatusConflict, toolbox.ErrStartedAfterClose.Error())
	})
}

func TestToolboxPaperAttendance(t *testing.T) {
	f := newEvidenceFixture(t, "paper")
	id := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(nil)), http.StatusCreated)
	paper := signBody("Paper Worker", signAt, map[string]any{"method": "paper"})

	wantError(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, uuid.NewString()), f.supervisor, paper),
		http.StatusConflict, toolbox.ErrNeedsRegisterPhoto.Error())
	// An ordinary photo isn't the register.
	group := photoTakenAt(t, f, f.supervisor, f.siteA, time.Now())
	wantStatus(t, do(t, f.router, http.MethodPut, talkPhotoPath(f.siteA, id, group), f.supervisor, map[string]any{"kind": "photo"}), http.StatusCreated)
	wantError(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, uuid.NewString()), f.supervisor, paper),
		http.StatusConflict, toolbox.ErrNeedsRegisterPhoto.Error())
	register := photoTakenAt(t, f, f.supervisor, f.siteA, time.Now())
	wantStatus(t, do(t, f.router, http.MethodPut, talkPhotoPath(f.siteA, id, register), f.supervisor, map[string]any{"kind": "paper_register"}), http.StatusCreated)
	rec := do(t, f.router, http.MethodPut, signPath(f.siteA, id, uuid.NewString()), f.supervisor, paper)
	wantStatus(t, rec, http.StatusCreated)
	if a := decodeAttendance(t, rec); a.Method != "paper" {
		t.Errorf("method %q, want paper", a.Method)
	}
}

func TestToolboxPhotoRules(t *testing.T) {
	f := newEvidenceFixture(t, "talkphoto")
	id := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(nil)), http.StatusCreated)
	add := func(ev string, body map[string]any) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, talkPhotoPath(f.siteA, id, ev), f.supervisor, body)
	}
	kind := map[string]any{"kind": "photo"}

	pending := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, pending), f.supervisor, declareBody("not uploaded")), http.StatusCreated)
	wantError(t, add(pending, kind), http.StatusConflict, toolbox.ErrEvidenceNotVerified.Error())
	wantError(t, add(uuid.NewString(), kind), http.StatusBadRequest, toolbox.ErrEvidenceNotFound.Error())
	onB := photoTakenAt(t, f, f.owner, f.siteB, time.Now())
	wantError(t, add(onB, kind), http.StatusBadRequest, toolbox.ErrEvidenceNotFound.Error())
	wantError(t, add("nope", kind), http.StatusBadRequest, toolbox.ErrBadEvidenceID.Error())
	wantError(t, add(pending, map[string]any{"kind": "selfie"}), http.StatusBadRequest, toolbox.ErrBadKind.Error())
	wantError(t, do(t, f.router, http.MethodPut, talkPhotoPath(f.siteA, uuid.NewString(), pending), f.supervisor, kind),
		http.StatusNotFound, toolbox.ErrTalkNotFound.Error())
}

func TestToolboxRetractRules(t *testing.T) {
	f := newEvidenceFixture(t, "unsign")
	name := uuid.NewString()[:8]
	other := inviteMember(t, f.router, f.owner, "second@"+name+".toolbox.test", auth.RoleSupervisor)
	wantStatus(t, do(t, f.router, http.MethodPut, "/v1/sites/"+f.siteA+"/members/"+other, f.owner, nil), http.StatusNoContent)
	second := login(t, f.router, "second@"+name+".toolbox.test")

	id := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(nil)), http.StatusCreated)
	att := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, att), f.supervisor, signBody("Sipho N", signAt, nil)), http.StatusCreated)
	path := signPath(f.siteA, id, att) + "/retract"
	body := map[string]any{"reason": "Left before the talk", "client_retracted_at": rfc(time.Now())}

	wantError(t, do(t, f.router, http.MethodPost, path, second, body), http.StatusForbidden, toolbox.ErrCantRetract.Error())
	wantError(t, do(t, f.router, http.MethodPost, path, f.owner, with(body, "reason", "")), http.StatusBadRequest, toolbox.ErrBadReason.Error())
	wantError(t, do(t, f.router, http.MethodPost, path, f.owner, with(body, "client_retracted_at", nil)), http.StatusBadRequest, toolbox.ErrNoRetractedAt.Error())
	wantError(t, do(t, f.router, http.MethodPost, path, f.owner, with(body, "client_retracted_at", ahead(time.Hour))),
		http.StatusBadRequest, toolbox.ErrRetractedInFuture.Error())
	wantError(t, do(t, f.router, http.MethodPost, signPath(f.siteA, uuid.NewString(), att)+"/retract", f.owner, body),
		http.StatusNotFound, toolbox.ErrAttendanceNotFound.Error())
	// A lead may retract anyone's.
	wantStatus(t, do(t, f.router, http.MethodPost, path, f.owner, body), http.StatusCreated)
}

// A signature whose insert loses the race to an identical one answers as a
// repeat (200), not 409 or 500. Forced: the winner holds its insert open.
func TestToolboxSignLosesTheRace(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "signlost")
	id, att := uuid.NewString(), uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(nil)), http.StatusCreated)
	supervisor := me(t, f.router, f.supervisor).UserID

	winner := beginTenant(t, f.ownerMe.OrgID)
	defer winner.Rollback(ctx)
	if _, err := winner.Exec(ctx, `
		INSERT INTO toolbox_attendance (id, org_id, site_id, talk_id, name, method, recorded_by, client_signed_at)
		VALUES ($1, $2, $3, $4, 'Sipho N', 'tap', $5, $6)`, att, f.ownerMe.OrgID, f.siteA, id, supervisor, signAt); err != nil {
		t.Fatalf("winner's insert: %v", err)
	}
	loser := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		loser <- do(t, f.router, http.MethodPut, signPath(f.siteA, id, att), f.supervisor, signBody("Sipho N", signAt, nil))
	}()
	waitForLock(t, "INSERT INTO toolbox_attendance")
	if err := winner.Commit(ctx); err != nil {
		t.Fatalf("commit winner: %v", err)
	}
	wantStatus(t, <-loser, http.StatusOK)
	if n := countRows(t, `SELECT count(*) FROM toolbox_attendance WHERE talk_id = $1`, id); n != 1 {
		t.Errorf("%d signatures, want 1", n)
	}
}
