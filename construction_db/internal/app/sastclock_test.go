package app

import (
	"fmt"
	"os"
	"testing"
	"time"

	"construction_db/internal/platform/clock"
)

// joburgLoc is the zone every test site's regime uses.
var joburgLoc = func() *time.Location {
	loc, err := time.LoadLocation("Africa/Johannesburg")
	if err != nil {
		panic(err)
	}
	return loc
}()

// testClock is the time the API and the worker run at in these tests: the
// real time, or with SITEPROOF_TEST_SAST=HH:MM the most recent HH:MM in
// Johannesburg (the database's clock is moved to match, dbtest.StartAt).
var testClock clock.Clock

// sastOffset reads SITEPROOF_TEST_SAST: how far back the most recent HH:MM
// SAST is, in whole seconds (libfaketime takes seconds). Zero when unset.
func sastOffset(now time.Time) (time.Duration, error) {
	v := os.Getenv("SITEPROOF_TEST_SAST")
	if v == "" {
		return 0, nil
	}
	hm, err := time.Parse("15:04", v)
	if err != nil {
		return 0, fmt.Errorf("SITEPROOF_TEST_SAST=%q: want HH:MM", v)
	}
	local := now.In(joburgLoc)
	at := time.Date(local.Year(), local.Month(), local.Day(), hm.Hour(), hm.Minute(), 0, 0, joburgLoc)
	if at.After(local) {
		at = at.AddDate(0, 0, -1)
	}
	d := at.Sub(now).Truncate(time.Second)
	if d == 0 {
		d = -time.Second // zero means "no offset"
	}
	return d, nil
}

// sastClock is a test's view of the time: one instant, on one
// Africa/Johannesburg day, so a test never straddles SAST midnight however
// close to it it runs. Take every instant from it; don't add to them.
type sastClock struct{ now time.Time }

func newSASTClock(t *testing.T) sastClock {
	t.Helper()
	return sastClock{now: testClock.Now().Truncate(time.Microsecond)}
}

func (c sastClock) Now() time.Time { return c.now }

// Frozen is a clock.Clock stopped at Now: for the worker's jobs, so an email
// says "in 10 days" from the same instant the test counted from.
func (c sastClock) Frozen() clock.Clock { return func() time.Time { return c.now } }

// Long is a date as emails write it: 2 Jan 2006.
func (c sastClock) Long(days int) string {
	return c.now.In(joburgLoc).AddDate(0, 0, days).Format("2 Jan 2006")
}

// Today is the SAST date, YYYY-MM-DD.
func (c sastClock) Today() string { return c.Day(0) }

// Day is today plus days, SAST.
func (c sastClock) Day(days int) string {
	return c.now.In(joburgLoc).AddDate(0, 0, days).Format(time.DateOnly)
}

// earlierSpan is the most a test steps back with Earlier.
const earlierSpan = time.Hour

// Earlier is d before now, never before 00:00 SAST today. Within the first
// hour of the day the hour is squeezed into what has passed since midnight,
// keeping the order and the steps distinct: at 00:01 a 10-minute step is
// 10 seconds.
func (c sastClock) Earlier(d time.Duration) time.Time {
	if d < 0 || d > earlierSpan {
		panic(fmt.Sprintf("Earlier(%s): want 0 to %s", d, earlierSpan))
	}
	local := c.now.In(joburgLoc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, joburgLoc)
	if since := c.now.Sub(midnight); since < earlierSpan {
		d = time.Duration(float64(d) * float64(since) / float64(earlierSpan))
	}
	return c.now.Add(-d).Truncate(time.Microsecond)
}

// At is a SAST wall-clock time on today plus day (day <= -1: always past).
func (c sastClock) At(day int, hhmm string) time.Time {
	if day > -1 {
		panic("At: day must be in the past")
	}
	hm, err := time.Parse("15:04", hhmm)
	if err != nil {
		panic(err)
	}
	d := c.now.In(joburgLoc).AddDate(0, 0, day)
	return time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, joburgLoc)
}
