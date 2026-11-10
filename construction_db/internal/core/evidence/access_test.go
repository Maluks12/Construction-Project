package evidence

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClientLogIP(t *testing.T) {
	tests := map[string]*string{
		"192.0.2.1":   ptr("192.0.2.1"),
		"2001:db8::1": ptr("2001:db8::1"),
		"":            nil,
		"not an ip":   nil,
		// inet rejects a zone; it would turn the view into a 500.
		"fe80::1%eth0": ptr("fe80::1"),
		// Stored as IPv4, so a search for the IPv4 address finds it.
		"::ffff:203.0.113.9": ptr("203.0.113.9"),
	}
	for in, want := range tests {
		got := Client{IP: in}.logIP()
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("logIP(%q) = %v, want %v", in, orNil(got), orNil(want))
		}
	}
}

func TestClientLogUserAgent(t *testing.T) {
	// "é" is two bytes, so 511 ASCII bytes + "é" crosses the 512-byte limit
	// in the middle of a character.
	acrossLimit := strings.Repeat("a", 511) + "é"

	tests := []struct {
		name, in string
		want     *string
	}{
		{"ordinary", "SiteProof/1.0 (Android 14)", ptr("SiteProof/1.0 (Android 14)")},
		{"empty", "", nil},
		{"only NUL", "\x00", nil},
		{"NUL removed", "a\x00b", ptr("ab")},
		{"invalid UTF-8 replaced", "a\xffb", ptr("a�b")},
		{"exactly 512 bytes kept", strings.Repeat("a", 512), ptr(strings.Repeat("a", 512))},
		{"long cut to 512", strings.Repeat("a", 600), ptr(strings.Repeat("a", 512))},
		{"cut never splits a character", acrossLimit, ptr(strings.Repeat("a", 511))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Client{UserAgent: tt.in}.logUserAgent()
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Fatalf("got %q, want %q", orNil(got), orNil(tt.want))
			}
			if got != nil && (len(*got) > maxUserAgentBytes || !utf8.ValidString(*got) || strings.Contains(*got, "\x00")) {
				t.Errorf("%q is not storable", *got)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func orNil(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
