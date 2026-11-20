package storage

import (
	"errors"
	"testing"
)

// The part size grows so any object up to S3's 5 TiB fits in 10,000 parts.
func TestPartSizeFor(t *testing.T) {
	for _, c := range []struct {
		size, want int64
	}{
		{1, PartSize},
		{80 << 30, 9 << 20}, // 80 GiB in 8 MiB parts is 10,240: too many
		{78 << 30, PartSize},
		{100 << 30, 11 << 20},
		{MaxObjectSize, 525 << 20},
	} {
		got, err := PartSizeFor(c.size)
		if err != nil || got != c.want {
			t.Errorf("%d bytes: part %d, err %v; want %d", c.size, got, err, c.want)
		}
		if got*MaxParts < c.size {
			t.Errorf("%d bytes don't fit in %d parts of %d", c.size, MaxParts, got)
		}
	}
	if _, err := PartSizeFor(MaxObjectSize + 1); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over 5 TiB: %v", err)
	}
}
