package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/editions/construction/diary"
	"construction_db/internal/regimes"
)

func diaryPath(site, date string) string { return "/v1/construction/sites/" + site + "/diary/" + date }
func revisionPath(site, date, id string) string {
	return diaryPath(site, date) + "/revisions/" + id
}
func addendumPath(site, date, id string) string {
	return diaryPath(site, date) + "/addenda/" + id
}

// localAt is hour:min local time, daysAgo days back. Yesterday's diary is
// always inside the 48-hour grace; one 5 days back never is.
func localAt(daysAgo, hour, min int) time.Time {
	y, m, d := time.Now().In(joburg).AddDate(0, 0, -daysAgo).Date()
	return time.Date(y, m, d, hour, min, 0, 0, joburg)
}

func daysAgo(n int) string { return localDay(time.Now(), -n) }

// revisionBody is the minimum: weather, headcount, work done.
func revisionBody(at time.Time, fields map[string]any) map[string]any {
	body := map[string]any{
		"weather":         map[string]any{"conditions": []string{"fine"}},
		"headcount":       12,
		"work_done":       "Slab poured on level 2",
		"client_saved_at": rfc(at),
	}
	for k, v := range fields {
		body = with(body, k, v)
	}
	return body
}

func decodeDiary(t *testing.T, rec *httptest.ResponseRecorder) diary.Diary {
	t.Helper()
	var d diary.Diary
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode diary: %v (body: %s)", err, rec.Body.String())
	}
	return d
}

// fullSections has a row in every optional section, on the day back days ago.
func fullSections(back int) map[string]any {
	at := func(h int) string { return rfc(localAt(back, h, 0)) }
	return map[string]any{
		"weather_times": map[string]any{
			"rain_started_at": at(10), "rain_stopped_at": at(12), "work_stopped_at": at(10), "work_restarted_at": at(13),
			"rain_gauge_mm": 18.5, "hours_lost": 3, "activities_affected": "Bricklaying on level 2",
			"critical_path": true, "delayed_effect": true,
		},
		"labour": []map[string]any{
			{"id": uuid.NewString(), "firm": "Our team", "own_team": true, "headcount": 8},
			{"id": uuid.NewString(), "firm": "ABC Scaffolding", "own_team": false, "trade": "Scaffolders", "headcount": 4},
		},
		"plant": []map[string]any{
			{"id": uuid.NewString(), "description": "TLB", "ref": "HIRE-17", "state": "working", "hours_working": 6, "hours_standing": 2},
			{"id": uuid.NewString(), "description": "Tower crane", "state": "broken_down"},
		},
		"instructions": []map[string]any{
			{"id": uuid.NewString(), "from_name": "J. Naidoo", "from_role": "Principal agent", "received_at": at(9), "form": "verbal",
				"description": "Move the stair core opening 300 mm east"},
		},
		"delays": []map[string]any{
			{"id": uuid.NewString(), "description": "Waiting for revised drawings", "started_at": at(9), "ended_at": at(11), "hours_lost": 2, "cause": "Late information"},
		},
		"visitors": []map[string]any{
			{"id": uuid.NewString(), "name": "Inspector M. Dube", "organisation": "Department of Employment and Labour", "purpose": "Inspection",
				"arrived_at": at(8), "left_at": at(9)},
		},
		"deliveries": []map[string]any{
			{"id": uuid.NewString(), "description": "Rebar Y16", "supplier": "Steelco", "quantity": "4 t", "status": "received", "received_at": at(7)},
			{"id": uuid.NewString(), "description": "Ready-mix 30 MPa", "status": "expected_not_received"},
		},
	}
}

func TestDiaryMinimumAndFull(t *testing.T) {
	f := newEvidenceFixture(t, "diary")
	date := daysAgo(1)
	supervisor := me(t, f.router, f.supervisor).UserID

	first := uuid.NewString()
	body := revisionBody(localAt(1, 16, 0), nil)
	rec := do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, first), f.supervisor, body)
	wantStatus(t, rec, http.StatusCreated)
	d := decodeDiary(t, rec)
	if d.DiaryDate != date || d.Current.ID != first || d.Current.Headcount != 12 || d.Current.SavedBy != supervisor ||
		d.Late || d.RevisionCount != 1 || len(d.ConflictRevisionIDs) != 0 || !d.Current.ReceivedAfterClose || d.Current.SavedAfterClose ||
		!d.Closed || len(d.Addenda) != 0 || d.CreatedBy != supervisor {
		t.Errorf("minimal diary %+v", d)
	}
	wantStatus(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, first), f.supervisor, body), http.StatusOK)
	wantError(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, first), f.supervisor, with(body, "headcount", 13)),
		http.StatusConflict, diary.ErrRevisionIDReused.Error())

	t.Run("every optional section", func(t *testing.T) {
		photo := photoTakenAt(t, f, f.supervisor, f.siteA, time.Now())
		full := revisionBody(localAt(1, 17, 0), map[string]any{
			"base_revision_id": first, "sections": fullSections(1), "evidence_ids": []string{photo},
			"weather": map[string]any{"conditions": []string{"rain", "wind"}, "note": "Heavy showers mid-morning"},
		})
		id := uuid.NewString()
		rec := do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, id), f.supervisor, full)
		wantStatus(t, rec, http.StatusCreated)
		got := decodeDiary(t, rec)
		s := got.Current.Sections
		if got.Current.ID != id || got.RevisionCount != 2 || len(got.ConflictRevisionIDs) != 0 || s.WeatherTimes == nil ||
			!*s.WeatherTimes.CriticalPath || len(s.Labour) != 2 || len(s.Plant) != 2 || len(s.Instructions) != 1 || len(s.Delays) != 1 ||
			len(s.Visitors) != 1 || len(s.Deliveries) != 2 || s.Deliveries[1].Status != "expected_not_received" ||
			!slices.Equal(got.Current.EvidenceIDs, []string{photo}) {
			t.Errorf("full diary %+v", got)
		}
		// A repeat with every section is still a repeat.
		wantStatus(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, id), f.supervisor, full), http.StatusOK)
	})

	t.Run("headcount: suggested from the day's talks, beside the entered one", func(t *testing.T) {
		talk := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, talk), f.supervisor,
			talkBody(map[string]any{"started_at": rfc(localAt(1, 7, 0))})), http.StatusCreated)
		for i, name := range []string{"Sipho N", "Lerato K", "Johan V"} {
			wantStatus(t, do(t, f.router, http.MethodPut, signPath(f.siteA, talk, uuid.NewString()), f.supervisor,
				signBody(name, localAt(1, 7, 10+i), nil)), http.StatusCreated)
		}
		d := decodeDiary(t, do(t, f.router, http.MethodGet, diaryPath(f.siteA, date), f.owner, nil))
		if d.HeadcountSuggestedNow != 3 || d.Current.HeadcountSuggested != 0 || !slices.Equal(d.ToolboxTalkIDs, []string{talk}) {
			t.Errorf("before a new revision: suggested now %d, stored %d, talks %v; want 3, 0, [%s]",
				d.HeadcountSuggestedNow, d.Current.HeadcountSuggested, d.ToolboxTalkIDs, talk)
		}
		rec := do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, uuid.NewString()), f.supervisor,
			revisionBody(localAt(1, 18, 0), map[string]any{"base_revision_id": d.Current.ID}))
		wantStatus(t, rec, http.StatusCreated)
		if got := decodeDiary(t, rec).Current; got.Headcount != 12 || got.HeadcountSuggested != 3 {
			t.Errorf("headcount %d, suggested %d; want 12 and 3 (the gap stays visible)", got.Headcount, got.HeadcountSuggested)
		}
	})

	t.Run("firms used on the site", func(t *testing.T) {
		rec := do(t, f.router, http.MethodGet, "/v1/construction/sites/"+f.siteA+"/diary-firms", f.supervisor, nil)
		wantStatus(t, rec, http.StatusOK)
		var body struct {
			Firms []string `json:"firms"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !slices.Equal(body.Firms, []string{"ABC Scaffolding", "Our team"}) {
			t.Errorf("firms %v, err %v", body.Firms, err)
		}
	})
}

func TestDiaryConflicts(t *testing.T) {
	f := newEvidenceFixture(t, "conflict")
	date := daysAgo(1)
	put := func(id string, at time.Time, base any) diary.Diary {
		t.Helper()
		rec := do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, id), f.supervisor, revisionBody(at, map[string]any{"base_revision_id": base}))
		wantStatus(t, rec, http.StatusCreated)
		return decodeDiary(t, rec)
	}
	r1, r2, r3, r4 := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	put(r1, localAt(1, 8, 0), nil)
	put(r2, localAt(1, 9, 0), r1)
	// A second phone saw only r1.
	d := put(r3, localAt(1, 10, 0), r1)
	if d.Current.ID != r3 || !slices.Equal(slices.Sorted(slices.Values(d.ConflictRevisionIDs)), slices.Sorted(slices.Values([]string{r2, r3}))) {
		t.Errorf("after r3: current %s, conflict %v; want r3 and [r2 r3]", d.Current.ID, d.ConflictRevisionIDs)
	}
	rec := do(t, f.router, http.MethodGet, diaryPath(f.siteA, date)+"/revisions", f.owner, nil)
	wantStatus(t, rec, http.StatusOK)
	var body struct {
		Revisions []diary.Revision `json:"revisions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Revisions) != 3 || body.Revisions[2].ID != r3 {
		t.Errorf("revisions %+v, err %v", body.Revisions, err)
	}
	// Someone who saw the newest merges them.
	if d := put(r4, localAt(1, 11, 0), r3); d.Current.ID != r4 || len(d.ConflictRevisionIDs) != 0 {
		t.Errorf("after the merge: current %s, conflict %v; want r4 and none", d.Current.ID, d.ConflictRevisionIDs)
	}
	wantError(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, uuid.NewString()), f.supervisor,
		revisionBody(localAt(1, 12, 0), map[string]any{"base_revision_id": uuid.NewString()})), http.StatusBadRequest, diary.ErrUnknownBase.Error())
}

func TestDiaryClosing(t *testing.T) {
	f := newEvidenceFixture(t, "closing")
	put := func(date, id string, body map[string]any) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, id), f.supervisor, body)
	}
	yesterday, old := daysAgo(1), daysAgo(5)
	first := uuid.NewString()

	// Saved before midnight, received after it: the original.
	wantStatus(t, put(yesterday, first, revisionBody(localAt(1, 23, 50), nil)), http.StatusCreated)
	// Saved after midnight: refused, however it's sent.
	wantError(t, put(yesterday, uuid.NewString(), revisionBody(time.Now(), map[string]any{"base_revision_id": first})),
		http.StatusBadRequest, diary.ErrLateReason.Error())
	wantError(t, put(yesterday, uuid.NewString(), revisionBody(time.Now(), map[string]any{"base_revision_id": first, "late_reason": "Forgot the plant"})),
		http.StatusConflict, diary.ErrDiaryClosed.Error())
	wantError(t, put(yesterday, uuid.NewString(), revisionBody(localAt(1, 23, 55), map[string]any{"late_reason": "Not late"})),
		http.StatusBadRequest, diary.ErrNotLate.Error())

	// Saved on the day but arriving after the grace.
	wantError(t, put(old, uuid.NewString(), revisionBody(localAt(5, 17, 0), nil)), http.StatusConflict, diary.ErrPastGrace.Error())
	// A missed day written late: once.
	rec := put(old, uuid.NewString(), revisionBody(time.Now(), map[string]any{"late_reason": "Phone was stolen"}))
	wantStatus(t, rec, http.StatusCreated)
	if d := decodeDiary(t, rec); !d.Late || !d.Current.SavedAfterClose {
		t.Errorf("late diary %+v", d)
	}
	wantError(t, put(old, uuid.NewString(), revisionBody(time.Now(), map[string]any{"late_reason": "Again"})), http.StatusConflict, diary.ErrDiaryClosed.Error())

	// Two days on: "tomorrow" is within the 5-minute clock leeway just before midnight.
	later := localDay(time.Now(), 2)
	wantError(t, put(later, uuid.NewString(), revisionBody(time.Now(), nil)), http.StatusBadRequest, diary.ErrDayNotBegun.Error())
	wantError(t, put(daysAgo(3), uuid.NewString(), revisionBody(localAt(4, 23, 0), nil)), http.StatusBadRequest, diary.ErrSavedBeforeDay.Error())
}

func TestDiaryAddenda(t *testing.T) {
	f := newEvidenceFixture(t, "addenda")
	today, yesterday := daysAgo(0), daysAgo(1)
	wantStatus(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, today, uuid.NewString()), f.supervisor, revisionBody(time.Now(), nil)), http.StatusCreated)
	original := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, yesterday, original), f.supervisor, revisionBody(localAt(1, 17, 0), nil)), http.StatusCreated)

	photo := photoTakenAt(t, f, f.supervisor, f.siteA, time.Now())
	body := map[string]any{
		"reason": "Forgot the crane", "note": "Tower crane stood 3 hours waiting for the drawings",
		"sections":        map[string]any{"plant": []map[string]any{{"id": uuid.NewString(), "description": "Tower crane", "state": "idle", "hours_standing": 3}}},
		"evidence_ids":    []string{photo},
		"client_added_at": rfc(time.Now()),
	}
	add := func(date, id string, body map[string]any) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, addendumPath(f.siteA, date, id), f.supervisor, body)
	}
	wantError(t, add(today, uuid.NewString(), body), http.StatusConflict, diary.ErrDiaryOpen.Error())
	wantError(t, add(daysAgo(2), uuid.NewString(), body), http.StatusNotFound, diary.ErrDiaryNotFound.Error())
	wantError(t, add(yesterday, uuid.NewString(), with(body, "reason", "")), http.StatusBadRequest, diary.ErrBadReason.Error())
	wantError(t, add(yesterday, uuid.NewString(), map[string]any{"reason": "Nothing", "client_added_at": rfc(time.Now())}),
		http.StatusBadRequest, diary.ErrAddsNothing.Error())
	wantError(t, add(yesterday, uuid.NewString(), with(body, "client_added_at", ahead(time.Hour))), http.StatusBadRequest, diary.ErrAddedInFuture.Error())

	id := uuid.NewString()
	rec := add(yesterday, id, body)
	wantStatus(t, rec, http.StatusCreated)
	var a diary.Addendum
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil || a.Reason != "Forgot the crane" || len(a.Sections.Plant) != 1 || !slices.Equal(a.EvidenceIDs, []string{photo}) {
		t.Errorf("addendum %+v, err %v", a, err)
	}
	wantStatus(t, add(yesterday, id, body), http.StatusOK)
	wantError(t, add(yesterday, id, with(body, "note", "Other")), http.StatusConflict, diary.ErrAddendumIDReused.Error())

	d := decodeDiary(t, do(t, f.router, http.MethodGet, diaryPath(f.siteA, yesterday), f.owner, nil))
	if d.Current.ID != original || d.RevisionCount != 1 || len(d.Addenda) != 1 || d.AddendumCount != 1 || len(d.Current.Sections.Plant) != 0 {
		t.Errorf("after the addendum: %+v; want the original untouched and one addendum", d)
	}
}

func TestDiaryValidation(t *testing.T) {
	f := newEvidenceFixture(t, "diarycheck")
	date := daysAgo(1)
	put := func(body map[string]any) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, uuid.NewString()), f.supervisor, body)
	}
	at := localAt(1, 12, 0)
	for _, c := range []struct {
		body map[string]any
		want error
	}{
		{with(revisionBody(at, nil), "weather", nil), diary.ErrNoWeather},
		{revisionBody(at, map[string]any{"weather": map[string]any{"conditions": []string{}}}), diary.ErrNoWeather},
		{revisionBody(at, map[string]any{"weather": map[string]any{"conditions": []string{"sunny"}}}), diary.ErrBadCondition},
		{revisionBody(at, map[string]any{"weather": map[string]any{"conditions": []string{"rain", "rain"}}}), diary.ErrBadCondition},
		{with(revisionBody(at, nil), "headcount", nil), diary.ErrNoHeadcount},
		{revisionBody(at, map[string]any{"headcount": -1}), diary.ErrNoHeadcount},
		{revisionBody(at, map[string]any{"work_done": " "}), diary.ErrBadWorkDone},
		{with(revisionBody(at, nil), "client_saved_at", nil), diary.ErrNoSavedAt},
		{revisionBody(time.Now().Add(time.Hour), nil), diary.ErrSavedInFuture},
		{revisionBody(at, map[string]any{"evidence_ids": []string{"x"}}), diary.ErrBadEvidenceIDs},
		{revisionBody(at, map[string]any{"base_revision_id": "x"}), diary.ErrBadBase},
	} {
		wantError(t, put(c.body), http.StatusBadRequest, c.want.Error())
	}
	wantError(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, "16-11-2026", uuid.NewString()), f.supervisor, revisionBody(at, nil)),
		http.StatusBadRequest, diary.ErrBadDate.Error())
	wantError(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, "x"), f.supervisor, revisionBody(at, nil)),
		http.StatusBadRequest, diary.ErrBadRevisionID.Error())

	// Section problems name the section, row and field.
	row := func(section string, rows ...map[string]any) map[string]any {
		return revisionBody(at, map[string]any{"sections": map[string]any{section: rows}})
	}
	id := uuid.NewString
	for _, c := range []struct {
		body    map[string]any
		want    error
		section string
		rowN    int
		field   string
	}{
		{row("labour", map[string]any{"id": id(), "firm": "A", "own_team": true, "headcount": 1}, map[string]any{"id": id(), "firm": "B", "own_team": true, "headcount": 1}),
			diary.ErrTwoOwnTeams, "labour", 1, "own_team"},
		{row("labour", map[string]any{"id": "nope", "firm": "A", "headcount": 1}), diary.ErrBadRowID, "labour", 0, "id"},
		{row("plant", map[string]any{"id": id(), "description": "TLB", "state": "resting"}), diary.ErrBadChoice, "plant", 0, "state"},
		{row("plant", map[string]any{"id": id(), "description": "TLB", "state": "idle", "hours_standing": 25}), diary.ErrBadNumber, "plant", 0, "hours_standing"},
		{row("instructions", map[string]any{"id": id(), "from_name": "PA", "form": "verbal", "description": "Do it"}), diary.ErrMissingTime, "instructions", 0, "received_at"},
		{row("delays", map[string]any{"id": id(), "description": "Rain", "started_at": rfc(localAt(0, 9, 0))}), diary.ErrBadTime, "delays", 0, "started_at"},
		{row("deliveries", map[string]any{"id": id(), "description": "Bricks", "status": "lost"}), diary.ErrBadChoice, "deliveries", 0, "status"},
		{row("visitors", map[string]any{"id": id(), "name": " "}), diary.ErrBadText, "visitors", 0, "name"},
	} {
		rec := put(c.body)
		wantStatus(t, rec, http.StatusBadRequest)
		var got struct {
			Error, Section, Field string
			Row                   int
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Error != c.want.Error() || got.Section != c.section || got.Row != c.rowN || got.Field != c.field {
			t.Errorf("%s: body %s; want %q at %s[%d].%s", c.section, rec.Body.String(), c.want, c.section, c.rowN, c.field)
		}
	}
}

func TestDiaryPhotos(t *testing.T) {
	f := newEvidenceFixture(t, "diaryphoto")
	date := daysAgo(1)
	pending := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, pending), f.supervisor, declareBody("not uploaded")), http.StatusCreated)
	onB := photoTakenAt(t, f, f.owner, f.siteB, time.Now())
	put := func(ids ...string) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, uuid.NewString()), f.supervisor,
			revisionBody(localAt(1, 12, 0), map[string]any{"evidence_ids": ids}))
	}
	wantProblem(t, put(pending), http.StatusConflict, diary.ErrEvidenceNotVerified.Error(), pending)
	wantProblem(t, put(onB), http.StatusBadRequest, diary.ErrEvidenceNotFound.Error(), onB)
}

func TestDiaryScoping(t *testing.T) {
	f := newEvidenceFixture(t, "diaryscope")
	date := daysAgo(1)
	wantStatus(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, uuid.NewString()), f.owner, revisionBody(localAt(1, 9, 0), nil)), http.StatusCreated)

	wantError(t, do(t, f.router, http.MethodPut, revisionPath(f.siteB, date, uuid.NewString()), f.supervisor, revisionBody(localAt(1, 9, 0), nil)),
		http.StatusNotFound, "site not found")
	wantError(t, do(t, f.router, http.MethodGet, diaryPath(f.siteA, date), f.other, nil), http.StatusNotFound, "site not found")
	wantStatus(t, do(t, f.router, http.MethodGet, diaryPath(f.siteA, date), f.bookkeeper, nil), http.StatusForbidden)
	wantError(t, do(t, f.router, http.MethodGet, diaryPath(f.siteA, daysAgo(2)), f.owner, nil), http.StatusNotFound, diary.ErrDiaryNotFound.Error())
	wantStatus(t, do(t, f.router, http.MethodGet, diaryPath(f.siteA, date)+"/revisions", f.supervisor, nil), http.StatusOK)

	t.Run("the register", func(t *testing.T) {
		list := func(query string) []diary.DiarySummary {
			rec := do(t, f.router, http.MethodGet, "/v1/construction/sites/"+f.siteA+"/diary"+query, f.supervisor, nil)
			wantStatus(t, rec, http.StatusOK)
			var body struct {
				Diaries []diary.DiarySummary `json:"diaries"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			return body.Diaries
		}
		if got := list(""); len(got) != 1 || got[0].DiaryDate != date || got[0].Headcount != 12 {
			t.Errorf("register %+v", got)
		}
		if got := list("?from=" + daysAgo(0)); len(got) != 0 {
			t.Errorf("from today: %+v", got)
		}
		wantError(t, do(t, f.router, http.MethodGet, "/v1/construction/sites/"+f.siteA+"/diary?from=2026-11-10&to=2026-11-01", f.owner, nil),
			http.StatusBadRequest, diary.ErrBadRange.Error())
		wantError(t, do(t, f.router, http.MethodGet, "/v1/construction/sites/"+f.siteA+"/diary?limit=0", f.owner, nil),
			http.StatusBadRequest, diary.ErrBadLimit.Error())
	})

	t.Run("an archived site keeps its diaries", func(t *testing.T) {
		wantStatus(t, do(t, f.router, http.MethodDelete, "/v1/sites/"+f.siteA, f.owner, nil), http.StatusNoContent)
		wantStatus(t, do(t, f.router, http.MethodGet, diaryPath(f.siteA, date), f.owner, nil), http.StatusOK)
		// Yesterday began before the archiving; tomorrow wouldn't.
		wantStatus(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, daysAgo(2), uuid.NewString()), f.owner,
			revisionBody(localAt(2, 9, 0), nil)), http.StatusCreated)
	})
}

// A site under another edition's regime has no diary: 404, as every edition
// route does (construction_db/CLAUDE.md).
func TestDiaryOnlyOnConstructionSites(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{}
	src := os.DirFS("../regimes/data")
	if err := fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(src, path)
		fsys[path] = &fstest.MapFile{Data: data}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	mining := regimeFS("test_regime_mining")
	for name, file := range mining {
		fsys[name] = file
	}
	fsys["test_regime_mining/regime.json"] = &fstest.MapFile{Data: []byte(`{"code": "test_regime_mining", "name": "Test mining",
		"edition": "mining", "jurisdiction": "ZA", "version": "1", "effective_from": "2020-01-01", "time_zone": "Africa/Johannesburg"}`)}
	reg, err := regimes.LoadFS(fsys)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(ctx, testPool, testSecret, reg, testEdition, testStore, testHTTP)
	if err != nil {
		t.Fatal(err)
	}

	name := uuid.NewString()[:8]
	owner := signup(t, router, "Mine Co "+name, "owner@"+name+".mining.test")
	orgID := me(t, router, owner).OrgID
	if _, err := testAdmin.Exec(ctx, `INSERT INTO regulatory_regimes (code, name, edition, version, effective_from)
		VALUES ('test_regime_mining', 'Test mining', 'mining', '1', '2020-01-01') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var mine string
	if err := testAdmin.QueryRow(ctx, `INSERT INTO sites (org_id, regime_id, name) SELECT $1, id, 'Shaft 1' FROM regulatory_regimes
		WHERE code = 'test_regime_mining' RETURNING id`, orgID).Scan(&mine); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		execAsTenant(t, orgID, 1, `DELETE FROM sites WHERE id = $1`, mine)
		_, _ = testAdmin.Exec(ctx, `DELETE FROM regulatory_regimes WHERE code = 'test_regime_mining'`)
	})

	wantError(t, do(t, router, http.MethodPut, revisionPath(mine, daysAgo(1), uuid.NewString()), owner, revisionBody(localAt(1, 9, 0), nil)),
		http.StatusNotFound, "site not found")
	wantError(t, do(t, router, http.MethodGet, "/v1/construction/sites/"+mine+"/diary", owner, nil), http.StatusNotFound, "site not found")
	construction := listSites(t, router, owner)[0].ID
	wantStatus(t, do(t, router, http.MethodGet, "/v1/construction/sites/"+construction+"/diary", owner, nil), http.StatusOK)
}

// A revision whose insert loses the race to an identical one answers as a
// repeat (200). Forced: the winner holds its diary and revision open.
func TestDiaryRevisionLosesTheRace(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "diarylost")
	date, id := daysAgo(1), uuid.NewString()
	supervisor := me(t, f.router, f.supervisor).UserID
	at := localAt(1, 15, 0)

	winner := beginTenant(t, f.ownerMe.OrgID)
	defer winner.Rollback(ctx)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO site_diaries (org_id, site_id, diary_date, time_zone, created_by) VALUES ($1, $2, $3, 'Africa/Johannesburg', $4)`,
			[]any{f.ownerMe.OrgID, f.siteA, date, supervisor}},
		// What revisionBody stores.
		{`INSERT INTO site_diary_revisions (id, org_id, site_id, diary_date, weather_conditions, headcount, work_done, saved_by, client_saved_at)
		  VALUES ($1, $2, $3, $4, ARRAY['fine'], 12, 'Slab poured on level 2', $5, $6)`, []any{id, f.ownerMe.OrgID, f.siteA, date, supervisor, at}},
	} {
		if _, err := winner.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("winner: %v", err)
		}
	}
	loser := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		loser <- do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, id), f.supervisor, revisionBody(at, nil))
	}()
	waitForLock(t, "INSERT INTO site_diaries")
	if err := winner.Commit(ctx); err != nil {
		t.Fatalf("commit winner: %v", err)
	}
	wantStatus(t, <-loser, http.StatusOK)
	if n := countRows(t, `SELECT count(*) FROM site_diary_revisions WHERE site_id = $1 AND diary_date = $2`, f.siteA, date); n != 1 {
		t.Errorf("%d revisions, want 1", n)
	}
}

// GraceAfterClose mirrors site_diary_grace() (000021).
func TestDiaryGraceMatchesDatabase(t *testing.T) {
	var secs float64
	if err := testAdmin.QueryRow(context.Background(), `SELECT extract(epoch FROM site_diary_grace())`).Scan(&secs); err != nil {
		t.Fatal(err)
	}
	if time.Duration(secs)*time.Second != diary.GraceAfterClose {
		t.Errorf("site_diary_grace() is %vs, diary.GraceAfterClose %v", secs, diary.GraceAfterClose)
	}
}
