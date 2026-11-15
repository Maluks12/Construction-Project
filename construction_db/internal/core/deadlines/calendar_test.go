package deadlines_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"construction_db/internal/regimes"
)

const sast = "Africa/Johannesburg"

type due struct {
	at, on                 string // at: RFC 3339 in UTC; on: YYYY-MM-DD or ""
	excluded               []string
	legalLatestOn, legalAt string
}

func callDue(ctx context.Context, basis time.Time, period int, unit, direction, tz string) (due, error) {
	var (
		at                   time.Time
		on, legalOn, legalAt *time.Time
		excluded             []time.Time
	)
	err := pool.QueryRow(ctx, `
		SELECT due_at, due_on, excluded_days, legal_latest_on, legal_latest_at
		FROM deadline_due($1, $2, $3, $4, $5, 'ZA')`, basis, period, unit, direction, tz).
		Scan(&at, &on, &excluded, &legalOn, &legalAt)
	if err != nil {
		return due{}, err
	}
	day := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Format(time.DateOnly)
	}
	got := due{at: at.UTC().Format(time.RFC3339), on: day(on), legalLatestOn: day(legalOn), excluded: []string{}}
	if legalAt != nil {
		got.legalAt = legalAt.UTC().Format(time.RFC3339)
	}
	for _, d := range excluded {
		got.excluded = append(got.excluded, d.Format(time.DateOnly))
	}
	return got, nil
}

// at parses a local SAST wall-clock time.
func at(t *testing.T, s string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(sast)
	if err != nil {
		t.Fatal(err)
	}
	v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Worked examples, each checked by hand against a calendar. SAST is UTC+2, so
// the end of a local day is 22:00 UTC that day.
func TestDeadlineDue(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name            string
		basis           string // SAST
		period          int
		unit, direction string
		want            due
	}{
		{
			// The incident's own date is excluded: day 1 is Tue 17, day 7 Mon 23.
			name: "7 calendar days", basis: "2026-11-16 14:00", period: 7, unit: "calendar_days", direction: "after",
			want: due{at: "2026-11-23T22:00:00Z", on: "2026-11-23"},
		},
		{
			// 01:30 SAST on the 16th is still the 15th in UTC; the local date counts.
			name: "local date, not UTC", basis: "2026-11-16 01:30", period: 7, unit: "calendar_days", direction: "after",
			want: due{at: "2026-11-23T22:00:00Z", on: "2026-11-23"},
		},
		{
			// Due Fri 25 Dec (Christmas). IA s4: 25 and 26 Dec are holidays,
			// 27 Dec a Sunday, so the legal latest is Mon 28 Dec. due is not moved.
			name: "Christmas: legal latest after the holidays", basis: "2026-12-18 10:00", period: 7, unit: "calendar_days", direction: "after",
			want: due{at: "2026-12-25T22:00:00Z", on: "2026-12-25", legalLatestOn: "2026-12-28", legalAt: "2026-12-28T22:00:00Z"},
		},
		{
			// s4 names Sundays and public holidays only.
			name: "Saturday is not extended", basis: "2026-11-14 09:00", period: 7, unit: "calendar_days", direction: "after",
			want: due{at: "2026-11-21T22:00:00Z", on: "2026-11-21"},
		},
		{
			name: "Sunday is extended to Monday", basis: "2026-11-15 09:00", period: 7, unit: "calendar_days", direction: "after",
			want: due{at: "2026-11-22T22:00:00Z", on: "2026-11-22", legalLatestOn: "2026-11-23", legalAt: "2026-11-23T22:00:00Z"},
		},
		{
			// Wed 4 Nov 2026 was proclaimed (PN 346 of 2026).
			name: "proclaimed holiday", basis: "2026-10-28 09:00", period: 7, unit: "calendar_days", direction: "after",
			want: due{at: "2026-11-04T22:00:00Z", on: "2026-11-04", legalLatestOn: "2026-11-05", legalAt: "2026-11-05T22:00:00Z"},
		},
		{
			// Sun 9 Aug 2026 is Women's Day, so Mon 10 Aug is a holiday too (PHA s2(1)).
			name: "Sunday holiday and its Monday", basis: "2026-08-02 09:00", period: 7, unit: "calendar_days", direction: "after",
			want: due{at: "2026-08-09T22:00:00Z", on: "2026-08-09", legalLatestOn: "2026-08-11", legalAt: "2026-08-11T22:00:00Z"},
		},
		{
			// Holidays inside the window don't count for calendar days.
			name: "across the new year", basis: "2026-12-28 09:00", period: 7, unit: "calendar_days", direction: "after",
			want: due{at: "2027-01-04T22:00:00Z", on: "2027-01-04"},
		},
		{
			// Easter 2027: Good Friday 26 Mar, Family Day 29 Mar. Thu 25 is day 1,
			// then Tue 30, Wed 31, Thu 1 and Fri 2 Apr.
			name: "working days over Easter", basis: "2027-03-24 09:00", period: 5, unit: "working_days", direction: "after",
			want: due{at: "2027-04-02T22:00:00Z", on: "2027-04-02", excluded: []string{"2027-03-26", "2027-03-27", "2027-03-28", "2027-03-29"}},
		},
		{
			// Sun 21 Mar 2027 is Human Rights Day; Mon 22 Mar is its substitute.
			name: "working days over a Sunday substitute", basis: "2027-03-19 09:00", period: 2, unit: "working_days", direction: "after",
			want: due{at: "2027-03-24T22:00:00Z", on: "2027-03-24", excluded: []string{"2027-03-20", "2027-03-21", "2027-03-22"}},
		},
		{
			// Back from Wed 31 Mar: Tue 30 (1), skip 29 to 26, Thu 25 (2), Wed 24 (3).
			name: "working days before", basis: "2027-03-31 09:00", period: 3, unit: "working_days", direction: "before",
			want: due{at: "2027-03-24T22:00:00Z", on: "2027-03-24", excluded: []string{"2027-03-26", "2027-03-27", "2027-03-28", "2027-03-29"}},
		},
		{
			// cr2014_reg4_notification's parameters. No legal latest for "before".
			name: "7 calendar days before", basis: "2026-11-30 07:00", period: 7, unit: "calendar_days", direction: "before",
			want: due{at: "2026-11-23T22:00:00Z", on: "2026-11-23"},
		},
		{
			// cr2014_reg3_permit_application's parameters.
			name: "30 calendar days before", basis: "2027-01-15 07:00", period: 30, unit: "calendar_days", direction: "before",
			want: due{at: "2026-12-16T22:00:00Z", on: "2026-12-16"},
		},
		{
			name: "hours are a moment", basis: "2026-11-16 14:00", period: 48, unit: "hours", direction: "after",
			want: due{at: "2026-11-18T12:00:00Z"},
		},
		{
			name: "immediately is the basis itself", basis: "2026-11-16 14:05", period: 0, unit: "immediately", direction: "after",
			want: due{at: "2026-11-16T12:05:00Z"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := callDue(ctx, at(t, tc.basis), tc.period, tc.unit, tc.direction, sast)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want.excluded == nil {
				tc.want.excluded = []string{}
			}
			if got.at != tc.want.at || got.on != tc.want.on || !slices.Equal(got.excluded, tc.want.excluded) ||
				got.legalLatestOn != tc.want.legalLatestOn || got.legalAt != tc.want.legalAt {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func TestDeadlineDueRefuses(t *testing.T) {
	ctx := context.Background()
	basis := at(t, "2026-11-16 14:00")
	tests := []struct {
		name            string
		basis           time.Time
		period          int
		unit, direction string
		tz              string
		code, msg       string
	}{
		{"unknown time zone", basis, 7, "calendar_days", "after", "Africa/Joburg", "22023", ""},
		// Never "no holiday" by default: a day outside the calendar fails.
		{"due day after the calendar", at(t, "2030-12-28 09:00"), 7, "calendar_days", "after", sast, "22000", "ZA ends on 2030-12-31; 2031-01-04 is after it"},
		{"working day before the calendar", at(t, "2023-12-29 09:00"), 3, "working_days", "after", sast, "22000", "ZA starts on 2024-01-01; 2023-12-30 is before it"},
		{"due day before the calendar", at(t, "2023-12-20 09:00"), 7, "calendar_days", "after", sast, "22000", "ZA starts on 2024-01-01; 2023-12-27 is before it"},
		{"unknown unit", basis, 7, "fortnights", "after", sast, "23514", "unknown unit fortnights"},
		{"unknown direction", basis, 7, "calendar_days", "during", sast, "23514", "unknown direction during"},
		{"immediately with a period", basis, 1, "immediately", "after", sast, "23514", "period 0"},
		{"immediately before", basis, 0, "immediately", "before", sast, "23514", "period 0"},
		{"zero days", basis, 0, "calendar_days", "after", sast, "23514", "period must be 1 or more"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callDue(ctx, tc.basis, tc.period, tc.unit, tc.direction, tc.tz)
			if pgCode(err) != tc.code || !strings.Contains(fmt.Sprint(err), tc.msg) {
				t.Errorf("got %v; want SQLSTATE %s %q", err, tc.code, tc.msg)
			}
		})
	}
	t.Run("null argument", func(t *testing.T) {
		err := pool.QueryRow(ctx, `SELECT due_at FROM deadline_due(NULL, 7, 'calendar_days', 'after', $1, 'ZA')`, sast).Scan(new(time.Time))
		if pgCode(err) != "22004" {
			t.Errorf("got %v; want SQLSTATE 22004", err)
		}
	})
}

// Schedule 1 of the Public Holidays Act, and the s2A proclamations quoted in
// SOURCES_za_construction_2014.md. A new proclamation goes in a migration and
// here.
var (
	scheduleFixed = []struct {
		month time.Month
		day   int
		name  string
	}{
		{time.January, 1, "New Year's Day"}, {time.March, 21, "Human Rights Day"}, {time.April, 27, "Freedom Day"},
		{time.May, 1, "Workers' Day"}, {time.June, 16, "Youth Day"}, {time.August, 9, "National Women's Day"},
		{time.September, 24, "Heritage Day"}, {time.December, 16, "Day of Reconciliation"},
		{time.December, 25, "Christmas Day"}, {time.December, 26, "Day of Goodwill"},
	}
	proclaimed = map[string]string{
		"2024-05-29": "Proclamation Notice 157 of 2024, GG 50165",
		"2026-11-04": "Proclamation Notice 346 of 2026, GG 55352",
	}
)

// easterSunday is the Gregorian computus (Meeus/Jones/Butcher).
func easterSunday(y int) time.Time {
	a, b, c := y%19, y/100, y%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(y, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func TestEasterSunday(t *testing.T) {
	// Published Easter Sundays, so a computus bug can't hide behind the
	// calendar test, which uses it.
	known := map[int]string{2024: "2024-03-31", 2025: "2025-04-20", 2026: "2026-04-05", 2027: "2027-03-28",
		2028: "2028-04-16", 2029: "2029-04-01", 2030: "2030-04-21"}
	for y, want := range known {
		if got := easterSunday(y).Format(time.DateOnly); got != want {
			t.Errorf("Easter %d = %s, want %s", y, got, want)
		}
	}
}

// expectedHolidays builds the calendar the Act gives for one year: Schedule 1,
// plus the Monday after any of them that falls on a Sunday (PHA s2(1)),
// unless that Monday is already a holiday.
func expectedHolidays(y int) map[string]string {
	out := map[string]string{}
	for _, f := range scheduleFixed {
		out[time.Date(y, f.month, f.day, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)] = "statutory"
	}
	e := easterSunday(y)
	out[e.AddDate(0, 0, -2).Format(time.DateOnly)] = "statutory" // Good Friday
	out[e.AddDate(0, 0, 1).Format(time.DateOnly)] = "statutory"  // Family Day
	for day := range maps.Clone(out) {
		d, _ := time.Parse(time.DateOnly, day)
		if d.Weekday() == time.Sunday {
			if mon := d.AddDate(0, 0, 1).Format(time.DateOnly); out[mon] == "" {
				out[mon] = "sunday_substitute"
			}
		}
	}
	return out
}

func TestHolidayCalendarFollowsTheAct(t *testing.T) {
	ctx := context.Background()
	var first, last time.Time
	if err := pool.QueryRow(ctx, `SELECT first_date, last_date FROM holiday_calendar_coverage WHERE jurisdiction = 'ZA'`).Scan(&first, &last); err != nil {
		t.Fatal(err)
	}
	// Whole years, so every year below is complete.
	if first.YearDay() != 1 || last.Month() != time.December || last.Day() != 31 {
		t.Fatalf("coverage %s to %s should be whole years", first.Format(time.DateOnly), last.Format(time.DateOnly))
	}

	want := map[string]string{}
	for y := first.Year(); y <= last.Year(); y++ {
		maps.Copy(want, expectedHolidays(y))
	}
	for day := range proclaimed {
		want[day] = "proclaimed"
	}

	rows, err := pool.Query(ctx, `SELECT holiday_date, kind, source_ref FROM public_holidays WHERE jurisdiction = 'ZA'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var (
			d         time.Time
			kind, ref string
		)
		if err := rows.Scan(&d, &kind, &ref); err != nil {
			t.Fatal(err)
		}
		day := d.Format(time.DateOnly)
		got[day] = kind
		if kind == "proclaimed" && !strings.Contains(ref, proclaimed[day]) {
			t.Errorf("%s: source_ref %q should cite %q", day, ref, proclaimed[day])
		}
		if d.Before(first) || d.After(last) {
			t.Errorf("%s is outside the coverage %s to %s", day, first.Format(time.DateOnly), last.Format(time.DateOnly))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	for _, day := range slices.Sorted(maps.Keys(want)) {
		if got[day] != want[day] {
			t.Errorf("%s: table has %q, the Act gives %q", day, got[day], want[day])
		}
	}
	for _, day := range slices.Sorted(maps.Keys(got)) {
		if want[day] == "" {
			t.Errorf("%s: table has %q, but the Act and the listed proclamations give no holiday", day, got[day])
		}
	}
}

// Fails two years before any regime's calendar runs out, so it is extended
// (a migration adding the next years) long before a deadline needs a day
// past the end.
func TestHolidayCalendarCoverageAhead(t *testing.T) {
	reg, err := regimes.Load()
	if err != nil {
		t.Fatal(err)
	}
	limit := time.Now().AddDate(2, 0, 0)
	for _, code := range reg.Codes() {
		ds, _ := reg.Get(code)
		var last time.Time
		err := pool.QueryRow(context.Background(), `SELECT last_date FROM holiday_calendar_coverage WHERE jurisdiction = $1`,
			ds.Regime.Jurisdiction).Scan(&last)
		if err != nil {
			t.Errorf("%s: no holiday calendar for jurisdiction %s: %v", code, ds.Regime.Jurisdiction, err)
			continue
		}
		if last.Before(limit) {
			t.Errorf("%s: the %s holiday calendar ends %s, less than two years from now; add the next years in a migration",
				code, ds.Regime.Jurisdiction, last.Format(time.DateOnly))
		}
	}
}

// Only migrations write the calendar.
func TestHolidayCalendarReadOnlyToApp(t *testing.T) {
	ctx := context.Background()
	for _, sql := range []string{
		`INSERT INTO public_holidays (jurisdiction, holiday_date, name, kind, source_ref) VALUES ('ZA', '2026-11-17', 'X', 'proclaimed', 'X')`,
		`UPDATE public_holidays SET name = 'X' WHERE holiday_date = '2026-12-25'`,
		`DELETE FROM public_holidays WHERE holiday_date = '2026-12-25'`,
		`UPDATE holiday_calendar_coverage SET last_date = '2040-12-31'`,
		`DELETE FROM holiday_calendar_coverage`,
	} {
		if _, err := pool.Exec(ctx, sql); pgCode(err) != "42501" {
			t.Errorf("%s: got %v, want SQLSTATE 42501", sql, err)
		}
	}
	// A substitute must be a Monday (checked for everyone, the superuser too).
	_, err := admin.Exec(ctx, `INSERT INTO public_holidays (jurisdiction, holiday_date, name, kind, source_ref)
		VALUES ('ZA', '2026-11-17', 'X', 'sunday_substitute', 'X')`)
	if pgCode(err) != "23514" {
		t.Errorf("a Tuesday substitute: got %v, want SQLSTATE 23514", err)
	}
}
