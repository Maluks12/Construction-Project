package devicesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

// maxXid bounds a full pass: every row, whatever the transaction (D2).
const maxXid = "9223372036854775807"

// RefPart is one part of a site's reference bundle, rendered by a GET.
type RefPart struct {
	Name    string
	Pattern string
	Path    func(siteID, regimeCode string) string
}

type Service struct {
	pool  *pgxpool.Pool
	sites *sites.Service
	reg   *regimes.Registry

	kinds []Kind
	refs  []RefPart
	push  []string // "METHOD pattern"

	// Router serves every sub-request; set once the router is built.
	Router http.Handler
	Clock  clock.Clock
	// A pull page stops after the item that uses up either (D4).
	PullBudget time.Duration
	PullBytes  int
	// A push starts no record with less than this left of the deadline (D6).
	PushReserve time.Duration

	refCache sync.Map // regime code -> Reference without its site
}

func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service, reg *regimes.Registry) *Service {
	return &Service{
		pool: pool, sites: sitesSvc, reg: reg,
		kinds: CoreKinds(), refs: CoreReferenceParts(), push: CorePushRoutes(),
		PullBudget: 5 * time.Second, PullBytes: 1 << 20, PushReserve: 2 * time.Second,
	}
}

// Sources are what an edition adds: its kinds, reference parts and push
// routes ("METHOD pattern").
type Sources struct {
	Kinds     []Kind
	Reference []RefPart
	Push      []string
}

func (s *Service) Add(src Sources) {
	s.kinds = append(s.kinds, src.Kinds...)
	s.refs = append(s.refs, src.Reference...)
	s.push = append(s.push, src.Push...)
}

func (s *Service) Kinds() []Kind        { return slices.Clone(s.kinds) }
func (s *Service) PushRoutes() []string { return slices.Clone(s.push) }

// Caller is who is syncing, and the request whose headers sub-requests carry.
type Caller struct {
	OrgID, UserID, Role string
	Request             *http.Request
}

func (c Caller) lead() bool { return c.Role == auth.RoleOwner || c.Role == auth.RoleSHEOfficer }

// UpstreamError is a sub-request's own failure (a 503, say), passed on as is.
type UpstreamError struct {
	Status     int
	Body       json.RawMessage
	RetryAfter *int
}

func (e *UpstreamError) Error() string { return fmt.Sprintf("sub-request answered %d", e.Status) }

func (s *Service) kindsFor(scope Scope, c Caller) []Kind {
	var out []Kind
	for _, k := range s.kinds {
		if k.Scope == scope && (!k.Leads || c.lead()) {
			out = append(out, k)
		}
	}
	return out
}

type candidate struct {
	kind int
	root string
}

// pull is one page for scope (a site id, or "org").
func (s *Service) pull(ctx context.Context, c Caller, scope Scope, scopeID, raw string, limit int, start time.Time) (*Pull, error) {
	scopeKey := "org"
	if scope == ScopeSite {
		scopeKey = scopeID
	}
	var cur *cursor
	if raw != "" {
		cc, err := decodeCursor(raw)
		if err != nil || cc.Scope != scopeKey {
			return nil, ErrBadCursor
		}
		cur = &cc
	}
	kinds := s.kindsFor(scope, c)

	var pass cursor
	var reset bool
	var cands []candidate
	err := database.WithTenantSnapshot(ctx, s.pool, database.Tenant{OrgID: c.OrgID, UserID: c.UserID}, func(tx pgx.Tx) error {
		if scope == ScopeSite {
			if _, err := s.sites.GetInTx(ctx, tx, c.OrgID, c.UserID, c.Role, scopeID); err != nil {
				return err
			}
		}
		// The transaction's own snapshot (REPEATABLE READ), the one every
		// query below reads: below its xmin every transaction has finished.
		var epoch, xmin, xmax string
		if err := tx.QueryRow(ctx, `SELECT sync_epoch(), pg_snapshot_xmin(s)::text, pg_snapshot_xmax(s)::text
			FROM pg_current_snapshot() s`).Scan(&epoch, &xmin, &xmax); err != nil {
			return fmt.Errorf("sync window: %w", err)
		}
		full := cursor{V: cursorVersion, Epoch: epoch, Scope: scopeKey, Org: c.OrgID, User: c.UserID, Role: c.Role, Lo: "0", Hi: xmin}
		switch {
		case cur == nil:
			pass = full
		case cur.Epoch != epoch || cur.Org != c.OrgID || cur.User != c.UserID || cur.Role != c.Role ||
			xidLess(xmax, cur.Lo) || (cur.paging() && xidLess(xmax, cur.Hi)):
			// Another server epoch, organisation, person or role, or a cursor from
			// the future (a restore): start again (D2, D5).
			reset, pass = true, full
		case cur.paging():
			pass = *cur
		default:
			pass = full
			pass.Lo, pass.Bounded = cur.Lo, true
		}

		first, after := 0, ""
		if pass.Kind != "" {
			first = slices.IndexFunc(kinds, func(k Kind) bool { return k.Name == pass.Kind })
			after = pass.After
			if first < 0 {
				reset, pass, first, after = true, full, 0, ""
			}
		}
		hi := pass.Hi
		if !pass.Bounded {
			hi = maxXid
		}
		for i := first; i < len(kinds) && len(cands) < limit; i++ {
			a := ""
			if i == first {
				a = after
			}
			rows, err := tx.Query(ctx, kinds[i].query(), scopeID, pass.Lo, hi, c.UserID, a, limit-len(cands))
			if err != nil {
				return fmt.Errorf("sync %s: %w", kinds[i].Name, err)
			}
			roots, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return fmt.Errorf("sync %s: %w", kinds[i].Name, err)
			}
			for _, r := range roots {
				cands = append(cands, candidate{i, r})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	items := []Item{}
	size, done := 0, 0
render:
	for _, cd := range cands {
		if done > 0 && (time.Since(start) >= s.PullBudget || size >= s.PullBytes) {
			break
		}
		k := kinds[cd.kind]
		r := serve(ctx, s.Router, c.Request, http.MethodGet, k.Path(scopeID, cd.root), k.Pattern, nil)
		switch r.status {
		case http.StatusOK:
			items = append(items, Item{Kind: k.Name, ID: cd.root, Data: r.body})
			size += len(r.body)
		case http.StatusNotFound, http.StatusForbidden:
			// Gone, or not this role's to read: nothing to send.
		default:
			if done == 0 {
				return nil, &UpstreamError{Status: r.status, Body: r.body, RetryAfter: r.retryAfter}
			}
			break render
		}
		done++
	}
	next := cursor{V: cursorVersion, Epoch: pass.Epoch, Scope: scopeKey, Org: c.OrgID, User: c.UserID, Role: c.Role, Lo: pass.Hi}
	hasMore := done < len(cands) || len(cands) == limit
	if hasMore {
		last := cands[done-1]
		next = pass
		next.Kind, next.After = kinds[last.kind].Name, last.root
	}
	return &Pull{ServerTime: s.Clock.Now().UTC(), Reset: reset, HasMore: hasMore, Cursor: next.encode(), Items: items}, nil
}

// PullSite is one page of a site's changes.
func (s *Service) PullSite(ctx context.Context, c Caller, siteID, raw string, limit int) (*Pull, error) {
	return s.pull(ctx, c, ScopeSite, siteID, raw, limit, time.Now())
}

// PullOrg is one page of the organisation's changes, with the caller and
// every site they can see now.
func (s *Service) PullOrg(ctx context.Context, c Caller, raw string, limit int) (*OrgPull, error) {
	// The sites and me come first and count in the page's time budget; the
	// items get what is left (at least one).
	start := time.Now()
	out := &OrgPull{Sites: []SiteEntry{}}
	me := serve(ctx, s.Router, c.Request, http.MethodGet, "/v1/me", "/v1/me", nil)
	if me.status != http.StatusOK {
		return nil, &UpstreamError{Status: me.status, Body: me.body, RetryAfter: me.retryAfter}
	}
	out.Me = me.body

	visible, err := s.sites.Visible(ctx, c.OrgID, c.UserID, c.Role)
	if err != nil {
		return nil, err
	}
	for _, v := range visible {
		r := serve(ctx, s.Router, c.Request, http.MethodGet, "/v1/sites/"+v.ID, "/v1/sites/:id", nil)
		if r.status == http.StatusNotFound {
			continue
		}
		if r.status != http.StatusOK {
			return nil, &UpstreamError{Status: r.status, Body: r.body, RetryAfter: r.retryAfter}
		}
		ref, err := s.reference(ctx, c, v.ID, v.RegimeCode)
		if err != nil {
			return nil, err
		}
		out.Sites = append(out.Sites, SiteEntry{Site: r.body, RegimeCode: v.RegimeCode, ReferenceSHA256: ref.SHA256})
	}
	p, err := s.pull(ctx, c, ScopeOrg, c.OrgID, raw, limit, start)
	if err != nil {
		return nil, err
	}
	out.Pull = *p
	return out, nil
}

// Reference is a site's regime bundle; the same for every site of a regime.
func (s *Service) Reference(ctx context.Context, c Caller, siteID string) (*Reference, error) {
	ds, err := s.sites.Regime(ctx, c.OrgID, c.UserID, c.Role, siteID)
	if err != nil {
		return nil, err
	}
	ref, err := s.reference(ctx, c, siteID, ds.Regime.Code)
	if err != nil {
		return nil, err
	}
	out := *ref
	out.SiteID = siteID
	return &out, nil
}

// reference renders the bundle once per regime: its parts are the regime's
// data, the same for every site and every role that may read them, and
// embedded in the binary, so they can't change while it runs.
func (s *Service) reference(ctx context.Context, c Caller, siteID, code string) (*Reference, error) {
	if r, ok := s.refCache.Load(code); ok {
		return r.(*Reference), nil
	}
	ds, ok := s.reg.Get(code)
	if !ok {
		return nil, fmt.Errorf("sync reference: no data set for %q", code)
	}
	parts := map[string]json.RawMessage{}
	for _, p := range s.refs {
		r := serve(ctx, s.Router, c.Request, http.MethodGet, p.Path(siteID, code), p.Pattern, nil)
		if r.status != http.StatusOK {
			return nil, &UpstreamError{Status: r.status, Body: r.body, RetryAfter: r.retryAfter}
		}
		parts[p.Name] = r.body
	}
	for name, v := range map[string]any{"incident_types": ds.Taxonomy, "vocabulary": ds.Vocabulary} {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("sync reference: %w", err)
		}
		parts[name] = b
	}
	all, err := json.Marshal(parts) // map keys sorted: a stable hash
	if err != nil {
		return nil, fmt.Errorf("sync reference: %w", err)
	}
	sum := sha256.Sum256(all)
	ref := &Reference{RegimeCode: code, SHA256: hex.EncodeToString(sum[:]), Parts: parts}
	actual, _ := s.refCache.LoadOrStore(code, ref)
	return actual.(*Reference), nil
}

// Push runs each record through its own endpoint, in order (D6).
func (s *Service) Push(ctx context.Context, c Caller, req PushRequest) (*PushResponse, error) {
	if n := len(req.Records); n < 1 || n > MaxPushRecords {
		return nil, &BatchError{Err: ErrBatchSize, Index: -1}
	}
	patterns := make([]string, len(req.Records))
	for i, r := range req.Records {
		if r.Ref != nil && len(*r.Ref) > 200 {
			return nil, &BatchError{Err: ErrBadRef, Index: i}
		}
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			return nil, &BatchError{Err: ErrBadMethod, Index: i}
		}
		p := s.allowed(r.Method, r.Path)
		if p == "" {
			return nil, &BatchError{Err: ErrRouteNotAllowed, Index: i}
		}
		patterns[i] = p
	}

	out := &PushResponse{Results: []PushResult{}}
	for i, r := range req.Records {
		if ctx.Err() != nil {
			break
		}
		if d, ok := ctx.Deadline(); ok && i > 0 && time.Until(d) < s.PushReserve {
			break
		}
		var body []byte
		if len(r.Body) > 0 && string(r.Body) != "null" {
			body = r.Body
		}
		res := serve(ctx, s.Router, c.Request, r.Method, r.Path, patterns[i], body)
		out.Results = append(out.Results, PushResult{Ref: r.Ref, Status: res.status, Body: res.body, RetryAfter: res.retryAfter})
		out.Processed = i + 1
		// Signed out, or the phone hung up: nothing after this can succeed.
		if res.status == http.StatusUnauthorized || res.status == 499 {
			break
		}
	}
	out.ServerTime = s.Clock.Now().UTC()
	return out, nil
}

// allowed is the push route pattern path matches, or "".
func (s *Service) allowed(method, path string) string {
	if !cleanPath(path) {
		return ""
	}
	for _, r := range s.push {
		m, p, _ := strings.Cut(r, " ")
		if m == method && matches(p, path) {
			return p
		}
	}
	return ""
}

// BatchError refuses the whole batch before anything runs.
type BatchError struct {
	Err   error
	Index int
}

func (e *BatchError) Error() string { return e.Err.Error() }
