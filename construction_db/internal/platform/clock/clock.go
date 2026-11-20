// Package clock is the time the services read "now" from, so the API and the
// worker can be run as if at another hour (tests near SAST midnight).
package clock

import "time"

// Clock gives the current time. The zero value (nil) is the real time.
type Clock func() time.Time

func (c Clock) Now() time.Time {
	if c == nil {
		return time.Now()
	}
	return c()
}

// Offset is the real time moved by d.
func Offset(d time.Duration) Clock {
	if d == 0 {
		return nil
	}
	return func() time.Time { return time.Now().Add(d) }
}
