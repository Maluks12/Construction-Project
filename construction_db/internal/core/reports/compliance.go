package reports

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/deadlines"
	"construction_db/internal/core/documents"
	"construction_db/internal/core/sites"
	"construction_db/internal/core/workers"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

// PendingAfter: a declared upload still pending this long is listed (M12).
const PendingAfter = 24 * time.Hour

type Service struct {
	pool          *pgxpool.Pool
	sites         *sites.Service
	docs          *documents.Service
	reg           *regimes.Registry
	contrib       Contributor // nil: the edition adds nothing
	defaultRegime string
	Clock         clock.Clock // nil is the real time
}

// defaultRegime reads the vault of an organisation with no active site.
func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service, docs *documents.Service, reg *regimes.Registry, contrib Contributor,
	defaultRegime string) *Service {
	return &Service{pool: pool, sites: sitesSvc, docs: docs, reg: reg, contrib: contrib, defaultRegime: defaultRegime}
}

func isLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

func colour(c string) *string { return &c }
func str(s string) *string    { return &s }

// Site is one site's summary now, "today" in the site regime's zone (D2).
func (s *Service) Site(ctx context.Context, orgID, userID, role, siteID string) (*SiteSummary, error) {
	return s.site(ctx, orgID, userID, role, siteID, map[string]Category{})
}

// site takes the organisation's vault from vaults when it has the site's
// regime, and adds it when not: the rollup reads it once, not per site.
func (s *Service) site(ctx context.Context, orgID, userID, role, siteID string, vaults map[string]Category) (*SiteSummary, error) {
	now := s.Clock.Now().UTC().Truncate(time.Microsecond)
	var (
		sc   SiteContext
		cats []Category
	)
	err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(sr.Data.Regime.TimeZone)
		if err != nil {
			return err
		}
		sc = SiteContext{OrgID: orgID, UserID: userID, Role: role, Site: sr.Site, Regime: sr.Data, Now: now,
			Today: now.In(loc).Format(time.DateOnly)}
		cats, err = s.coreCategories(ctx, tx, sc)
		return err
	})
	if errors.Is(err, sites.ErrSiteNotFound) {
		return nil, ErrSiteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("site compliance: %w", err)
	}

	vault, ok := vaults[sc.Regime.Regime.Code]
	if !ok {
		if vault, err = s.vault(ctx, sc, sc.Regime.Regime.Code); err != nil {
			return nil, fmt.Errorf("site compliance: %w", err)
		}
		vaults[sc.Regime.Regime.Code] = vault
	}
	vault.Items = slices.Clone(vault.Items)
	cats = append(cats, vault)
	if s.contrib != nil {
		more, err := s.contrib.ComplianceCategories(ctx, sc)
		if err != nil {
			return nil, fmt.Errorf("site compliance: %w", err)
		}
		cats = append(cats, more...)
	}
	return summarise(sc, cats), nil
}

// summarise colours each counted category and gives the site its worst (D1).
func summarise(sc SiteContext, cats []Category) *SiteSummary {
	slices.SortStableFunc(cats, func(a, b Category) int {
		return cmp.Compare(slices.Index(CategoryKeys, a.Key), slices.Index(CategoryKeys, b.Key))
	})
	out := &SiteSummary{SiteID: sc.Site.ID, SiteName: sc.Site.Name, SiteStatus: sc.Site.Status, RegimeCode: sc.Regime.Regime.Code,
		TimeZone: sc.Regime.Regime.TimeZone, Date: sc.Today, ComputedAt: sc.Now, Status: Green, Categories: cats}
	for i := range cats {
		c := &cats[i]
		for j := range c.Items {
			if c.Items[j].Warnings == nil {
				c.Items[j].Warnings = []string{}
			}
		}
		c.Count = len(c.Items)
		if !c.Counted {
			c.Status = nil
			continue
		}
		st := Green
		for _, it := range c.Items {
			switch {
			case it.Colour == nil:
			case *it.Colour == Red:
				st = Red
				out.RedItems++
			case *it.Colour == Amber:
				if st == Green {
					st = Amber
				}
				out.AmberItems++
			}
		}
		c.Status = &st
		out.Status = worse(out.Status, st)
	}
	return out
}

func rank(status string) int { return map[string]int{Red: 0, Amber: 1, Green: 2}[status] }

func worse(a, b string) string {
	if rank(b) < rank(a) {
		return b
	}
	return a
}

func (s *Service) coreCategories(ctx context.Context, tx pgx.Tx, sc SiteContext) ([]Category, error) {
	steps := []func(context.Context, pgx.Tx, SiteContext) (Category, error){
		s.findings, s.actions, s.verifications, s.signIns, s.incidentDeadlines, s.workerRecords, s.absent, s.pending,
	}
	out := make([]Category, 0, len(steps))
	for _, step := range steps {
		c, err := step(ctx, tx, sc)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// findings: unactioned (finding_coverage, 000019), red if high or critical.
func (s *Service) findings(ctx context.Context, tx pgx.Tx, sc SiteContext) (Category, error) {
	c := Category{Key: CatFindings, Counted: true, Items: []Item{}}
	rows, err := tx.Query(ctx, `
		SELECT f.inspection_id::text, f.item_key, f.severity, r.prompt, f.raised_at
		FROM inspection_findings f
		JOIN inspection_responses r ON r.inspection_id = f.inspection_id AND r.item_key = f.item_key
		CROSS JOIN LATERAL finding_coverage(f.inspection_id, f.item_key) cov
		WHERE f.org_id = $1 AND f.site_id = $2 AND cov.coverage = 'unactioned'
		ORDER BY f.raised_at, f.inspection_id, f.item_key`, sc.OrgID, sc.Site.ID)
	if err != nil {
		return c, fmt.Errorf("findings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var insp, key, sev, prompt string
		var raised time.Time
		if err := rows.Scan(&insp, &key, &sev, &prompt, &raised); err != nil {
			return c, fmt.Errorf("findings: %w", err)
		}
		col := Amber
		if sev == "high" || sev == "critical" {
			col = Red
		}
		c.Items = append(c.Items, Item{SubjectType: SubjectFinding, SubjectID: str(insp), State: ItemUnactioned, Colour: colour(col),
			Label: prompt, InspectionID: str(insp), ItemKey: str(key), Severity: str(sev), Since: &raised})
	}
	return c, rows.Err()
}

// actions: open and past due, red, with the reminders' escalation step.
func (s *Service) actions(ctx context.Context, tx pgx.Tx, sc SiteContext) (Category, error) {
	c := Category{Key: CatActions, Counted: true, Items: []Item{}}
	rows, err := tx.Query(ctx, `
		SELECT a.id::text, a.title, a.severity, a.due_on::text, action_escalation_step($3, a.due_on, a.time_zone, $4)
		FROM corrective_actions a
		WHERE a.org_id = $1 AND a.site_id = $2 AND a.status = 'open' AND $3::timestamptz >= a.due_at
		ORDER BY a.due_at, a.id`, sc.OrgID, sc.Site.ID, sc.Now, sc.Regime.Reminders.ActionEscalationDays)
	if err != nil {
		return c, fmt.Errorf("overdue actions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, title, sev, due string
		var step int
		if err := rows.Scan(&id, &title, &sev, &due, &step); err != nil {
			return c, fmt.Errorf("overdue actions: %w", err)
		}
		c.Items = append(c.Items, Item{SubjectType: SubjectAction, SubjectID: str(id), State: ItemOverdue, Colour: colour(Red),
			Label: title, ActionID: str(id), Severity: str(sev), DueOn: str(due), EscalationStep: &step})
	}
	return c, rows.Err()
}

// verifications: close-outs awaiting a lead past verification_stalled_days (amber).
func (s *Service) verifications(ctx context.Context, tx pgx.Tx, sc SiteContext) (Category, error) {
	c := Category{Key: CatVerifications, Counted: true, Items: []Item{}}
	rows, err := tx.Query(ctx, `
		SELECT a.id::text, a.pending_closeout_id::text, a.title, a.awaiting_since
		FROM corrective_actions a
		WHERE a.org_id = $1 AND a.site_id = $2 AND a.status = 'awaiting_verification'
		  AND a.awaiting_since <= $3::timestamptz - make_interval(days => $4::int)
		ORDER BY a.awaiting_since, a.id`, sc.OrgID, sc.Site.ID, sc.Now, sc.Regime.Reminders.VerificationStalledDays)
	if err != nil {
		return c, fmt.Errorf("stalled verifications: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, closeout, title string
		var since time.Time
		if err := rows.Scan(&id, &closeout, &title, &since); err != nil {
			return c, fmt.Errorf("stalled verifications: %w", err)
		}
		c.Items = append(c.Items, Item{SubjectType: SubjectCloseout, SubjectID: str(closeout), State: ItemStalled, Colour: colour(Amber),
			Label: title, ActionID: str(id), Since: &since})
	}
	return c, rows.Err()
}

// signIns: today's standing sign-ins whose worker has a warning now (red).
func (s *Service) signIns(ctx context.Context, tx pgx.Tx, sc SiteContext) (Category, error) {
	c := Category{Key: CatSignIns, Counted: true, Items: []Item{}}
	type signIn struct{ id, worker, name string }
	rows, err := tx.Query(ctx, `
		SELECT id::text, worker_id::text, name FROM site_sign_ins
		WHERE org_id = $1 AND site_id = $2 AND sign_in_date = $3::date AND retracted_at IS NULL
		ORDER BY client_signed_in_at, id`, sc.OrgID, sc.Site.ID, sc.Today)
	if err != nil {
		return c, fmt.Errorf("sign-ins: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (signIn, error) {
		var x signIn
		return x, r.Scan(&x.id, &x.worker, &x.name)
	})
	if err != nil {
		return c, fmt.Errorf("sign-ins: %w", err)
	}
	for _, si := range list {
		warnings, err := workers.WarningsFor(ctx, tx, s.reg, sc.OrgID, si.worker, sc.Now)
		if err != nil {
			return c, fmt.Errorf("sign-in warnings: %w", err)
		}
		if len(warnings) == 0 {
			continue
		}
		c.Items = append(c.Items, Item{SubjectType: SubjectSignIn, SubjectID: str(si.id), State: ItemWarnings, Colour: colour(Red),
			Label: si.name, WorkerID: str(si.worker), Warnings: warnings})
	}
	return c, nil
}

// incidentDeadlines: red when overdue or forthwith, amber when due soon.
func (s *Service) incidentDeadlines(ctx context.Context, tx pgx.Tx, sc SiteContext) (Category, error) {
	c := Category{Key: CatIncidentDeadlines, Counted: true, Items: []Item{}}
	live, err := deadlines.ListLiveForSite(ctx, tx, sc.OrgID, sc.Site.ID, sc.Now)
	if err != nil {
		return c, fmt.Errorf("incident deadlines: %w", err)
	}
	soon := 0
	if d := sc.Regime.Reminders.DeadlineDaysBefore; len(d) > 0 {
		soon = d[0]
	}
	for _, d := range live {
		var state, col string
		switch {
		case d.State == deadlines.StateOverdue:
			state, col = ItemOverdue, Red
		case d.State == deadlines.StateDueNow:
			state, col = ItemDueNow, Red
		case d.State == deadlines.StateOpen && dueSoon(d, sc, soon):
			state, col = ItemDueSoon, Amber
		default:
			continue
		}
		dueAt := d.DueAt
		c.Items = append(c.Items, Item{SubjectType: SubjectDeadline, SubjectID: str(d.ID), State: state, Colour: colour(col),
			Label: d.Label, IncidentID: d.IncidentID, DueOn: d.DueOn, Since: &dueAt})
	}
	return c, nil
}

// dueSoon compares local dates for day-based rules, instants for hour-based ones.
func dueSoon(d deadlines.Deadline, sc SiteContext, soon int) bool {
	if d.DueOn != nil {
		due, err1 := time.Parse(time.DateOnly, *d.DueOn)
		today, err2 := time.Parse(time.DateOnly, sc.Today)
		return err1 == nil && err2 == nil && !due.After(today.AddDate(0, 0, soon))
	}
	return !d.DueAt.After(sc.Now.Add(time.Duration(soon) * 24 * time.Hour))
}

// workerRecords: today's roster (site_roster_on, 000042), expiring records (amber); expired ones count via sign-in warnings.
func (s *Service) workerRecords(ctx context.Context, tx pgx.Tx, sc SiteContext) (Category, error) {
	c := Category{Key: CatWorkerRecords, Counted: true, Items: []Item{}}
	rows, err := tx.Query(ctx, `
		WITH roster AS (SELECT DISTINCT worker_id FROM site_roster_on($2, $3::date, $4, $5))
		SELECT 'competency', r.id::text, r.worker_id::text, w.name, r.regime_code, r.type_key, r.expires_on::text
		FROM worker_competencies r JOIN workers w ON w.id = r.worker_id
		WHERE r.org_id = $1 AND r.worker_id IN (SELECT worker_id FROM roster) AND r.retracted_at IS NULL AND r.expires_on IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM worker_competencies x WHERE x.supersedes_id = r.id AND x.retracted_at IS NULL)
		UNION ALL
		SELECT 'medical', r.id::text, r.worker_id::text, w.name, r.regime_code, NULL, r.expires_on::text
		FROM worker_medicals r JOIN workers w ON w.id = r.worker_id
		WHERE r.org_id = $1 AND r.worker_id IN (SELECT worker_id FROM roster) AND r.retracted_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM worker_medicals x WHERE x.supersedes_id = r.id AND x.retracted_at IS NULL)
		ORDER BY 7, 4, 2`, sc.OrgID, sc.Site.ID, sc.Today, sc.Regime.Regime.TimeZone, sc.Regime.Reminders.RosterLapseDays)
	if err != nil {
		return c, fmt.Errorf("worker records: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id, worker, name, regime string
		var typeKey, expires *string
		if err := rows.Scan(&kind, &id, &worker, &name, &regime, &typeKey, &expires); err != nil {
			return c, fmt.Errorf("worker records: %w", err)
		}
		if workers.RecordState(s.reg, &workers.RecordHeader{RegimeCode: regime}, expires, sc.Now) != workers.StateExpiring {
			continue
		}
		label, subject := name+": medical certificate of fitness", SubjectMedical
		if kind == "competency" {
			subject = SubjectCompetency
			label = name + ": " + s.competencyLabel(regime, *typeKey)
		}
		c.Items = append(c.Items, Item{SubjectType: subject, SubjectID: str(id), State: ItemExpiring, Colour: colour(Amber),
			Label: label, WorkerID: str(worker), TypeKey: typeKey, DueOn: expires})
	}
	return c, rows.Err()
}

func (s *Service) competencyLabel(regime, key string) string {
	if ds, ok := s.reg.Get(regime); ok {
		if t, ok := ds.CompetencyType(key); ok {
			return t.Label
		}
	}
	return key
}

// absent is shown, not counted.
func (s *Service) absent(ctx context.Context, tx pgx.Tx, sc SiteContext) (Category, error) {
	c := Category{Key: CatAbsentWorkers, Items: []Item{}}
	rows, err := tx.Query(ctx, `
		SELECT roster_id::text, worker_id::text, name FROM site_absent($1, $2::date, $3, $4) ORDER BY name, worker_id`,
		sc.Site.ID, sc.Today, sc.Regime.Regime.TimeZone, sc.Regime.Reminders.RosterLapseDays)
	if err != nil {
		return c, fmt.Errorf("absent workers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, worker, name string
		if err := rows.Scan(&id, &worker, &name); err != nil {
			return c, fmt.Errorf("absent workers: %w", err)
		}
		c.Items = append(c.Items, Item{SubjectType: SubjectRosterEntry, SubjectID: str(id), State: ItemAbsent, Label: name, WorkerID: str(worker)})
	}
	return c, rows.Err()
}

func (s *Service) pending(ctx context.Context, tx pgx.Tx, sc SiteContext) (Category, error) {
	return pendingUploads(ctx, tx, sc.OrgID, &sc.Site.ID, sc.Now)
}

// pendingUploads with site nil lists the organisation's files with no site.
func pendingUploads(ctx context.Context, tx pgx.Tx, orgID string, site *string, now time.Time) (Category, error) {
	c := Category{Key: CatPendingUploads, Items: []Item{}}
	rows, err := tx.Query(ctx, `
		SELECT 'evidence', id::text, 'Photo (' || content_type || ')', created_at FROM evidence
		WHERE org_id = $1 AND $2::uuid IS NOT NULL AND site_id = $2 AND status = 'pending' AND created_at < $3
		UNION ALL
		SELECT 'file', id::text, coalesce(file_name, purpose), created_at FROM files
		WHERE org_id = $1 AND site_id IS NOT DISTINCT FROM $2::uuid AND status = 'pending' AND created_at < $3
		ORDER BY 4, 2`, orgID, site, now.Add(-PendingAfter))
	if err != nil {
		return c, fmt.Errorf("pending uploads: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id, label string
		var since time.Time
		if err := rows.Scan(&kind, &id, &label, &since); err != nil {
			return c, fmt.Errorf("pending uploads: %w", err)
		}
		subject := SubjectEvidence
		if kind == "file" {
			subject = SubjectFile
		}
		c.Items = append(c.Items, Item{SubjectType: subject, SubjectID: str(id), State: ItemPending, Label: label, Since: &since})
	}
	return c, rows.Err()
}

// vault: missing vault types aren't counted (decided Thu 19 Nov).
func (s *Service) vault(ctx context.Context, sc SiteContext, regimeCode string) (Category, error) {
	c := Category{Key: CatVault, Counted: true, Items: []Item{}}
	reg, err := s.docs.Register(ctx, sc.OrgID, sc.UserID, sc.Role, documents.Where{Scope: documents.ScopeOrg}, regimeCode)
	if err != nil {
		return c, fmt.Errorf("vault: %w", err)
	}
	c.Items = DocumentItems(reg, false)
	return c, nil
}

// DocumentItems: the H&S file counts missing required types (countMissing); the vault doesn't.
func DocumentItems(reg *documents.Register, countMissing bool) []Item {
	items := []Item{}
	byID := map[string]documents.DocumentSummary{}
	for _, d := range reg.Documents {
		byID[d.ID] = d
	}
	for _, r := range reg.Readiness {
		if !r.Required {
			continue
		}
		switch r.Status {
		case documents.StatusMissing:
			if countMissing {
				items = append(items, Item{SubjectType: SubjectDocumentType, State: ItemMissing, Colour: colour(Red),
					Label: r.Label, TypeKey: str(r.TypeKey)})
			}
		case documents.StatusExpired:
			for _, id := range r.DocumentIDs {
				d := byID[id]
				items = append(items, Item{SubjectType: SubjectDocument, SubjectID: str(id), State: ItemExpired, Colour: colour(Red),
					Label: d.Title, TypeKey: str(r.TypeKey), DueOn: d.Current.ExpiresOn})
			}
		}
	}
	for _, d := range reg.Documents {
		if d.Retraction == nil && d.Current.State == documents.StateExpiring {
			items = append(items, Item{SubjectType: SubjectDocument, SubjectID: str(d.ID), State: ItemExpiring, Colour: colour(Amber),
				Label: d.Title, TypeKey: str(d.TypeKey), DueOn: d.Current.ExpiresOn})
		}
	}
	return items
}

// Org ranks the caller's sites worst first (D3), archived ones left out.
func (s *Service) Org(ctx context.Context, orgID, userID, role string) (*Rollup, error) {
	list, err := s.sites.List(ctx, orgID, userID, role)
	if err != nil {
		return nil, fmt.Errorf("compliance rollup: %w", err)
	}
	out := &Rollup{ComputedAt: s.Clock.Now().UTC().Truncate(time.Microsecond), OrgItems: []Category{}, Sites: []SiteRank{}}
	vaults := map[string]Category{}
	for _, site := range list {
		if site.Status == sites.StatusArchived {
			continue
		}
		sum, err := s.site(ctx, orgID, userID, role, site.ID, vaults)
		if errors.Is(err, ErrSiteNotFound) {
			continue // archived or unassigned in between
		}
		if err != nil {
			return nil, err
		}
		r := SiteRank{SiteID: sum.SiteID, SiteName: sum.SiteName, SiteStatus: sum.SiteStatus, Status: sum.Status,
			RedItems: sum.RedItems, AmberItems: sum.AmberItems, Counts: map[string]int{}, Statuses: map[string]string{}}
		for _, c := range sum.Categories {
			r.Counts[c.Key] = c.Count
			if c.Status != nil {
				r.Statuses[c.Key] = *c.Status
			}
		}
		out.Sites = append(out.Sites, r)
	}
	slices.SortFunc(out.Sites, func(a, b SiteRank) int {
		return cmp.Or(cmp.Compare(rank(a.Status), rank(b.Status)), cmp.Compare(b.RedItems, a.RedItems),
			cmp.Compare(b.AmberItems, a.AmberItems), cmp.Compare(a.SiteName, b.SiteName), cmp.Compare(a.SiteID, b.SiteID))
	})

	// The organisation's own items, once: its vault as the sites read it, or
	// by the default regime when it has no active site.
	vault, ok := vaults[s.defaultRegime]
	if codes := slices.Sorted(maps.Keys(vaults)); !ok && len(codes) > 0 {
		vault, ok = vaults[codes[0]], true
	}
	if !ok {
		sc := SiteContext{OrgID: orgID, UserID: userID, Role: role}
		if vault, err = s.vault(ctx, sc, s.defaultRegime); err != nil {
			return nil, fmt.Errorf("compliance rollup: %w", err)
		}
	}
	vault.Items = slices.Clone(vault.Items)
	vault.Count = len(vault.Items)
	st := Green
	for _, it := range vault.Items {
		st = worse(st, *it.Colour)
	}
	vault.Status = &st
	out.OrgItems = append(out.OrgItems, vault)
	if isLead(role) {
		var files Category
		err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, func(tx pgx.Tx) error {
			var err error
			files, err = pendingUploads(ctx, tx, orgID, nil, s.Clock.Now())
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("compliance rollup: %w", err)
		}
		files.Count = len(files.Items)
		out.OrgItems = append(out.OrgItems, files)
	}
	return out, nil
}
