package clock

import (
	"testing"
	"time"
)

func TestClock(t *testing.T) {
	var real Clock
	if d := time.Since(real.Now()); d < 0 || d > time.Second {
		t.Errorf("nil clock is %s from the real time", d)
	}
	if Offset(0) != nil {
		t.Error("Offset(0) should be the real clock (nil)")
	}
	back := Offset(-10 * time.Hour)
	if d := time.Since(back.Now()); d < 10*time.Hour || d > 10*time.Hour+time.Second {
		t.Errorf("Offset(-10h) is %s behind", d)
	}
}
