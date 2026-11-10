package evidence

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
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

// View returns a short-lived URL for the photo itself. This is the only place
// that hands out evidence bytes (internal/archtest enforces it).
//
// The view_bytes row commits before the URL is signed, so no URL exists
// without a log row. If signing then failed, the log would show a view that
// never happened: over-logging, the safe direction.
func (s *Service) View(ctx context.Context, orgID, userID, role, siteID, evidenceID string, client Client) (*ViewURL, error) {
	var ev *Evidence
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		ev, err = GetSiteEvidence(ctx, tx, orgID, site.ID, evidenceID)
		if isNotFound(err) {
			return ErrEvidenceNotFound
		}
		if err != nil {
			return err
		}
		// Pending bytes aren't checked yet and rejected ones failed the checks.
		if ev.Status != StatusVerified || ev.ObjectVersionID == nil {
			return ErrNotVerified
		}
		return InsertAccess(ctx, tx, accessEntry{
			OrgID: orgID, SiteID: site.ID, UserID: userID, Action: ActionViewBytes,
			EvidenceID: &ev.ID, ObjectVersionID: ev.ObjectVersionID, Client: client,
		})
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("view evidence: %w", err)
	}

	// The sealed version, served as the type that version was verified to be.
	p, err := s.store.PresignGet(ctx, ev.ObjectKey, *ev.ObjectVersionID, ev.ContentType, viewTTL)
	if err != nil {
		return nil, fmt.Errorf("view evidence: %w", err)
	}
	return &ViewURL{Method: p.Method, URL: p.URL, ExpiresAt: p.ExpiresAt.UTC()}, nil
}

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
