package evidence

import (
	"net/netip"
	"strings"
	"unicode/utf8"
)

// What an evidence_access_log row records (Rev 4 §2.1: every view, export and share).
const (
	ActionViewBytes  = "view_bytes"
	ActionViewRecord = "view_record"
	ActionList       = "list"
	ActionExport     = "export"
	ActionShare      = "share"

	maxUserAgentBytes = 512 // the column's CHECK
)

// Client is where a request came from, as the handler saw it. IP must come
// from gin's ClientIP, which only believes X-Forwarded-For from trusted proxies.
type Client struct {
	IP        string
	UserAgent string
}

// logIP is the IP for the inet column, or nil if it isn't one. Postgres inet
// rejects an IPv6 zone (fe80::1%eth0), and an IPv4-mapped address
// (::ffff:203.0.113.9) would be stored as IPv6 and missed by a search for
// the IPv4 address, so both are normalised.
func (c Client) logIP() *string {
	addr, err := netip.ParseAddr(c.IP)
	if err != nil {
		return nil
	}
	s := addr.Unmap().WithZone("").String()
	return &s
}

// logUserAgent is the user agent as Postgres will accept it: valid UTF-8, no
// NUL, at most 512 bytes and cut on a character boundary; nil if empty.
// Postgres refuses invalid UTF-8 and NUL in text, which would turn a view
// into a 500.
func (c Client) logUserAgent() *string {
	ua := strings.ReplaceAll(strings.ToValidUTF8(c.UserAgent, "�"), "\x00", "")
	if len(ua) > maxUserAgentBytes {
		ua = ua[:maxUserAgentBytes]
		for !utf8.ValidString(ua) {
			ua = ua[:len(ua)-1]
		}
	}
	if ua == "" {
		return nil
	}
	return &ua
}
