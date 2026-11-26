package deadlines

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"construction_db/internal/regimes"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// What a deadline is about: one foreign key column per kind (000017, 000046).
type SubjectKind string

const (
	SubjectIncident SubjectKind = "incident"
	SubjectSiteWork SubjectKind = "site_work"
)

var SubjectKinds = []string{string(SubjectIncident), string(SubjectSiteWork)}

// Deadline is one stored computation of a duty, as the API shows it.
type Deadline struct {
	ID          string      `json:"id"`
	DutyID      string      `json:"duty_id"`
	SubjectType SubjectKind `json:"subject_type"`
	IncidentID  *string     `json:"incident_id"`
	SiteWorkID  *string     `json:"site_work_id"`
	RuleKey     string      `json:"rule_key"`
	Label       string      `json:"label"`
	Kind        string      `json:"kind"`
	Period      int         `json:"period"`
	Unit        string      `json:"unit"`
	Direction   string      `json:"direction"`
	Basis       string      `json:"basis"`
	SourceRef   string      `json:"source_ref"`
	Caveat      *string     `json:"caveat"`
	// H&S file document types a satisfaction may link as proof.
	ProofDocumentTypes []string `json:"proof_document_types"`
	// When the clock started, and the zone its days are counted in.
	BasisAt  time.Time `json:"basis_at"`
	TimeZone string    `json:"time_zone"`
	// due_at is the first instant after the deadline: on time means before
	// it. due_on is the last local day for day-based rules, else null.
	DueAt        time.Time `json:"due_at"`
	DueOn        *string   `json:"due_on"`
	ExcludedDays []string  `json:"excluded_days"`
	// The latest date the law allows when the last day is a Sunday or public
	// holiday (Interpretation Act s4); null when no extension applies.
	// Shown next to due_at, never used for alerts.
	LegalLatestOn        *string    `json:"legal_latest_on"`
	LegalLatestAt        *time.Time `json:"legal_latest_at"`
	LegalLatestSourceRef *string    `json:"legal_latest_source_ref"`
	ComputedInVersion    int        `json:"computed_in_version"`
	ComputedAt           time.Time  `json:"computed_at"`
	// Set when recording that satisfaction added the row (a death after a
	// notice), not a version.
	AddedBySatisfactionID *string `json:"added_by_satisfaction_id"`
	// The deadline had already passed when it was computed (e.g. a type
	// that applies from the incident was added days later).
	OverdueWhenComputed bool   `json:"overdue_when_computed"`
	State               string `json:"state"`
	// How the row stopped being live, and who, when and why (from the
	// version that did it). All null while live.
	ClosedAs        *string    `json:"closed_as"`
	SupersededBy    *string    `json:"superseded_by"`
	ClosedInVersion *int       `json:"closed_in_version"`
	ClosedBy        *string    `json:"closed_by"`
	ClosedAt        *time.Time `json:"closed_at"`
	ClosedReason    *string    `json:"closed_reason"`
	// The duty's current satisfaction, shared by every row of the duty.
	Satisfaction *Satisfaction `json:"satisfaction"`
}

// Satisfaction records that a duty was met.
type Satisfaction struct {
	ID          string    `json:"id"`
	DeadlineID  string    `json:"deadline_id"`
	SatisfiedAt time.Time `json:"satisfied_at"`
	Method      string    `json:"method"`
	Reference   *string   `json:"reference"`
	Note        *string   `json:"note"`
	EvidenceID  *string   `json:"evidence_id"`
	// A version of a site H&S file document, as proof (REG4 D12).
	DocumentID      *string   `json:"document_id"`
	DocumentVersion *int      `json:"document_version"`
	RecordedBy      string    `json:"recorded_by"`
	RecordedAt      time.Time `json:"recorded_at"`
	DeviceID        *string   `json:"device_id"`
	// Met in time but recorded after the due time (satisfied_at < due_at <=
	// recorded_at), e.g. a phone that synced late: an alert may have shown
	// meanwhile. Judged against the deadline it is shown with.
	RecordedAfterDue bool `json:"recorded_after_due"`
	// Set when an owner or SHE officer took it back; the duty is open again.
	Retraction *Retraction `json:"retraction"`
}

type Retraction struct {
	RetractedBy string    `json:"retracted_by"`
	RetractedAt time.Time `json:"retracted_at"`
	Reason      string    `json:"reason"`
}

// The values of Satisfaction.Method.
var Methods = []string{"phone", "email", "fax", "in_person", "online", "post", "other"}

const deadlineColumns = `
	d.id, d.duty_id, d.incident_id, d.site_work_id, d.rule_key, d.rule_label, d.kind, d.period, d.unit, d.direction, d.basis, d.source_ref,
	d.caveat, d.proof_document_types, d.basis_at, d.time_zone, d.due_at, d.due_on::text, d.excluded_days::text[], d.legal_latest_on::text,
	d.legal_latest_at, d.legal_latest_source_ref, d.computed_in_version, d.computed_at, d.added_by_satisfaction_id, d.closed_as, d.superseded_by,
	d.closed_in_version, d.closed_by, d.closed_at, d.closed_reason,
	s.id, s.deadline_id, s.satisfied_at, s.method, s.reference, s.note, s.evidence_id, s.document_id, s.document_version, s.recorded_by, s.recorded_at, s.device_id`

// selectDeadlines joins each row to its duty's current satisfaction.
const selectDeadlines = `SELECT` + deadlineColumns + `
	FROM deadlines d
	JOIN deadline_duties du ON du.id = d.duty_id
	LEFT JOIN deadline_satisfactions s ON s.id = du.satisfaction_id
`

func scanDeadline(row pgx.Row, now time.Time) (Deadline, error) {
	var (
		d                                    Deadline
		satID, satDeadline, satMethod, satBy *string
		satRef, satNote, satEvidence, satDoc *string
		satDevice                            *string
		satDocVersion                        *int
		satAt, satRecorded                   *time.Time
	)
	err := row.Scan(&d.ID, &d.DutyID, &d.IncidentID, &d.SiteWorkID, &d.RuleKey, &d.Label, &d.Kind, &d.Period, &d.Unit, &d.Direction, &d.Basis,
		&d.SourceRef, &d.Caveat, &d.ProofDocumentTypes, &d.BasisAt, &d.TimeZone, &d.DueAt, &d.DueOn, &d.ExcludedDays, &d.LegalLatestOn,
		&d.LegalLatestAt, &d.LegalLatestSourceRef, &d.ComputedInVersion, &d.ComputedAt, &d.AddedBySatisfactionID, &d.ClosedAs, &d.SupersededBy,
		&d.ClosedInVersion, &d.ClosedBy, &d.ClosedAt, &d.ClosedReason,
		&satID, &satDeadline, &satAt, &satMethod, &satRef, &satNote, &satEvidence, &satDoc, &satDocVersion, &satBy, &satRecorded, &satDevice)
	if err != nil {
		return Deadline{}, err
	}
	d.SubjectType = SubjectIncident
	if d.SiteWorkID != nil {
		d.SubjectType = SubjectSiteWork
	}
	if d.ProofDocumentTypes == nil {
		d.ProofDocumentTypes = []string{}
	}
	d.BasisAt, d.DueAt, d.ComputedAt = d.BasisAt.UTC(), d.DueAt.UTC(), d.ComputedAt.UTC()
	d.LegalLatestAt, d.ClosedAt = utc(d.LegalLatestAt), utc(d.ClosedAt)
	if d.ExcludedDays == nil {
		d.ExcludedDays = []string{}
	}
	if satID != nil {
		d.Satisfaction = &Satisfaction{ID: *satID, DeadlineID: *satDeadline, SatisfiedAt: satAt.UTC(), Method: *satMethod,
			Reference: satRef, Note: satNote, EvidenceID: satEvidence, DocumentID: satDoc, DocumentVersion: satDocVersion,
			RecordedBy: *satBy, RecordedAt: satRecorded.UTC(), DeviceID: satDevice}
		d.Satisfaction.RecordedAfterDue = RecordedAfterDue(d.Unit, d.DueAt, *d.Satisfaction)
	}
	d.OverdueWhenComputed = d.Unit != "immediately" && !d.ComputedAt.Before(d.DueAt)
	closed := ""
	if d.ClosedAs != nil {
		closed = *d.ClosedAs
	}
	var satisfiedAt *time.Time
	if d.Satisfaction != nil {
		satisfiedAt = &d.Satisfaction.SatisfiedAt
	}
	d.State = State(closed, d.Unit, d.DueAt, satisfiedAt, now)
	return d, nil
}

// RecordedAfterDue: s was in time for a deadline due at dueAt but recorded
// after it. A call-now rule has no due time.
func RecordedAfterDue(unit string, dueAt time.Time, s Satisfaction) bool {
	return unit != "immediately" && s.SatisfiedAt.Before(dueAt) && !s.RecordedAt.Before(dueAt)
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func collect(rows pgx.Rows, now time.Time) ([]Deadline, error) {
	defer rows.Close()
	out := []Deadline{}
	for rows.Next() {
		d, err := scanDeadline(rows, now)
		if err != nil {
			return nil, fmt.Errorf("scan deadline: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list deadlines: %w", err)
	}
	return out, nil
}

// Ref names one subject.
type Ref struct {
	Kind SubjectKind
	ID   string
}

// columns are the two subject columns' values: the ref's id in its own, NULL
// in the other.
func (r Ref) columns() (incident, siteWork *string) {
	id := r.ID
	if r.Kind == SubjectSiteWork {
		return nil, &id
	}
	return &id, nil
}

// ListForSubject returns every row of a subject's duties, live and closed,
// grouped by duty in the order the duties were created, oldest computation
// first.
func ListForSubject(ctx context.Context, db DBTX, orgID string, ref Ref, now time.Time) ([]Deadline, error) {
	inc, work := ref.columns()
	rows, err := db.Query(ctx, selectDeadlines+`
		WHERE d.org_id = $1 AND (d.incident_id = $2 OR d.site_work_id = $3)
		ORDER BY du.created_at, du.rule_key, d.computed_in_version, d.computed_at`, orgID, inc, work)
	if err != nil {
		return nil, fmt.Errorf("list subject deadlines: %w", err)
	}
	return collect(rows, now)
}

// ListLiveForSite returns a site's live deadlines, soonest due first.
func ListLiveForSite(ctx context.Context, db DBTX, orgID, siteID string, now time.Time) ([]Deadline, error) {
	rows, err := db.Query(ctx, selectDeadlines+`
		WHERE d.org_id = $1 AND d.site_id = $2 AND d.closed_as IS NULL
		ORDER BY d.due_at, d.id`, orgID, siteID)
	if err != nil {
		return nil, fmt.Errorf("list site deadlines: %w", err)
	}
	return collect(rows, now)
}

// GetForSite returns one deadline row on a site.
func GetForSite(ctx context.Context, db DBTX, orgID, siteID, id string, now time.Time) (Deadline, error) {
	d, err := scanDeadline(db.QueryRow(ctx, selectDeadlines+`WHERE d.org_id = $1 AND d.site_id = $2 AND d.id = $3`, orgID, siteID, id), now)
	if err != nil {
		return Deadline{}, fmt.Errorf("get deadline: %w", err)
	}
	return d, nil
}

// GetLiveForDuty returns the duty's live row; pgx.ErrNoRows if withdrawn.
func GetLiveForDuty(ctx context.Context, db DBTX, orgID, dutyID string, now time.Time) (Deadline, error) {
	d, err := scanDeadline(db.QueryRow(ctx, selectDeadlines+`WHERE d.org_id = $1 AND d.duty_id = $2 AND d.closed_as IS NULL`, orgID, dutyID), now)
	if err != nil {
		return Deadline{}, fmt.Errorf("get live deadline: %w", err)
	}
	return d, nil
}

// ListSatisfactions returns every satisfaction of a duty, retracted ones
// included, oldest first.
func ListSatisfactions(ctx context.Context, db DBTX, orgID, dutyID string) ([]Satisfaction, error) {
	rows, err := db.Query(ctx, `
		SELECT s.id, s.deadline_id, s.satisfied_at, s.method, s.reference, s.note, s.evidence_id, s.document_id, s.document_version,
		       s.recorded_by, s.recorded_at, s.device_id, r.retracted_by, r.retracted_at, r.reason
		FROM deadline_satisfactions s
		LEFT JOIN deadline_satisfaction_retractions r ON r.satisfaction_id = s.id
		WHERE s.org_id = $1 AND s.duty_id = $2
		ORDER BY s.recorded_at, s.id`, orgID, dutyID)
	if err != nil {
		return nil, fmt.Errorf("list satisfactions: %w", err)
	}
	defer rows.Close()
	out := []Satisfaction{}
	for rows.Next() {
		var (
			s          Satisfaction
			by, reason *string
			at         *time.Time
		)
		if err := rows.Scan(&s.ID, &s.DeadlineID, &s.SatisfiedAt, &s.Method, &s.Reference, &s.Note, &s.EvidenceID, &s.DocumentID,
			&s.DocumentVersion, &s.RecordedBy, &s.RecordedAt, &s.DeviceID, &by, &at, &reason); err != nil {
			return nil, fmt.Errorf("scan satisfaction: %w", err)
		}
		s.SatisfiedAt, s.RecordedAt = s.SatisfiedAt.UTC(), s.RecordedAt.UTC()
		if by != nil {
			s.Retraction = &Retraction{RetractedBy: *by, RetractedAt: at.UTC(), Reason: *reason}
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list satisfactions: %w", err)
	}
	return out, nil
}

// GetSatisfaction returns one satisfaction with its retraction, if any.
func GetSatisfaction(ctx context.Context, db DBTX, orgID, id string) (*Satisfaction, string, error) {
	var (
		s          Satisfaction
		dutyID     string
		by, reason *string
		at         *time.Time
	)
	err := db.QueryRow(ctx, `
		SELECT s.id, s.deadline_id, s.satisfied_at, s.method, s.reference, s.note, s.evidence_id, s.document_id, s.document_version,
		       s.recorded_by, s.recorded_at, s.device_id, r.retracted_by, r.retracted_at, r.reason, s.duty_id
		FROM deadline_satisfactions s
		LEFT JOIN deadline_satisfaction_retractions r ON r.satisfaction_id = s.id
		WHERE s.org_id = $1 AND s.id = $2`, orgID, id).
		Scan(&s.ID, &s.DeadlineID, &s.SatisfiedAt, &s.Method, &s.Reference, &s.Note, &s.EvidenceID, &s.DocumentID, &s.DocumentVersion,
			&s.RecordedBy, &s.RecordedAt, &s.DeviceID, &by, &at, &reason, &dutyID)
	if err != nil {
		return nil, "", fmt.Errorf("get satisfaction: %w", err)
	}
	s.SatisfiedAt, s.RecordedAt = s.SatisfiedAt.UTC(), s.RecordedAt.UTC()
	if by != nil {
		s.Retraction = &Retraction{RetractedBy: *by, RetractedAt: at.UTC(), Reason: *reason}
	}
	return &s, dutyID, nil
}

// Duties returns a subject's duties with their live rows and current
// satisfactions: the engine's view of what is already stored.
func Duties(ctx context.Context, db DBTX, orgID string, ref Ref) ([]Duty, error) {
	inc, work := ref.columns()
	rows, err := db.Query(ctx, `
		SELECT du.id, du.rule_key, d.id, d.basis_at, s.satisfied_at
		FROM deadline_duties du
		LEFT JOIN deadlines d ON d.duty_id = du.id AND d.closed_as IS NULL
		LEFT JOIN deadline_satisfactions s ON s.id = du.satisfaction_id
		WHERE du.org_id = $1 AND (du.incident_id = $2 OR du.site_work_id = $3)
		ORDER BY du.created_at, du.rule_key`, orgID, inc, work)
	if err != nil {
		return nil, fmt.Errorf("list duties: %w", err)
	}
	defer rows.Close()
	out := []Duty{}
	for rows.Next() {
		var (
			d       Duty
			liveID  *string
			basisAt *time.Time
		)
		if err := rows.Scan(&d.ID, &d.RuleKey, &liveID, &basisAt, &d.SatisfiedAt); err != nil {
			return nil, fmt.Errorf("scan duty: %w", err)
		}
		if liveID != nil {
			d.Live = &LiveDeadline{ID: *liveID, BasisAt: basisAt.UTC()}
		}
		d.SatisfiedAt = utc(d.SatisfiedAt)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list duties: %w", err)
	}
	return out, nil
}

// Subject is what a deadline is about, and where its clock is kept.
type Subject struct {
	OrgID, SiteID, RegimeID string
	Ref                     Ref
	Regime                  regimes.Regime
	Version                 int // the subject's version computing or closing rows
	// The satisfaction whose recording adds the rows, when it isn't a version.
	AddedBySatisfaction *string
}

// DueAt is what deadline_due gives for rule from basisAt: used to tell
// whether a recomputation moves a deadline later, with the same function the
// insert trigger uses.
func DueAt(ctx context.Context, db DBTX, sub Subject, rule regimes.DeadlineRule, basisAt time.Time) (time.Time, error) {
	var due time.Time
	err := db.QueryRow(ctx, `SELECT due_at FROM deadline_due($1, $2, $3, $4, $5, $6)`,
		basisAt, rule.Period, rule.Unit, rule.Direction, sub.Regime.TimeZone, sub.Regime.Jurisdiction).Scan(&due)
	if err != nil {
		return time.Time{}, fmt.Errorf("deadline due: %w", err)
	}
	return due.UTC(), nil
}

// Apply carries out a plan for sub in the caller's transaction. A
// Supersede closes the live row before inserting its replacement (one live
// row per duty); the replacement's id is chosen here so the close can name
// it.
func Apply(ctx context.Context, db DBTX, sub Subject, steps []Step) error {
	for _, st := range steps {
		switch st.Action {
		case Carry:
		case Withdraw:
			if err := closeRow(ctx, db, sub, st.LiveID, "withdrawn", nil); err != nil {
				return err
			}
		case Supersede:
			next := uuid.NewString()
			if err := closeRow(ctx, db, sub, st.LiveID, "superseded", &next); err != nil {
				return err
			}
			if err := insertRow(ctx, db, sub, next, st.DutyID, st.Rule, st.BasisAt); err != nil {
				return err
			}
		case Add:
			duty := st.DutyID
			if duty == "" {
				inc, work := sub.Ref.columns()
				if err := db.QueryRow(ctx, `
					INSERT INTO deadline_duties (org_id, site_id, incident_id, site_work_id, rule_key) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
					sub.OrgID, sub.SiteID, inc, work, st.Rule.Key).Scan(&duty); err != nil {
					return fmt.Errorf("insert duty: %w", err)
				}
			}
			if err := insertRow(ctx, db, sub, uuid.NewString(), duty, st.Rule, st.BasisAt); err != nil {
				return err
			}
		default:
			return fmt.Errorf("apply deadlines: unknown action %q", st.Action)
		}
	}
	return nil
}

func closeRow(ctx context.Context, db DBTX, sub Subject, id, as string, supersededBy *string) error {
	tag, err := db.Exec(ctx, `UPDATE deadlines SET closed_as = $3, superseded_by = $4, closed_in_version = $5 WHERE org_id = $1 AND id = $2`,
		sub.OrgID, id, as, supersededBy, sub.Version)
	if err != nil {
		return fmt.Errorf("close deadline: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("close deadline %s: %d rows", id, tag.RowsAffected())
	}
	return nil
}

// insertRow stores a copy of the rule; the insert trigger computes the due
// values (the placeholders here are overwritten).
func insertRow(ctx context.Context, db DBTX, sub Subject, id, dutyID string, r regimes.DeadlineRule, basisAt time.Time) error {
	var caveat *string
	if r.Caveat != "" {
		caveat = &r.Caveat
	}
	proof := r.ProofDocumentTypes
	if proof == nil {
		proof = []string{}
	}
	inc, work := sub.Ref.columns()
	_, err := db.Exec(ctx, `
		INSERT INTO deadlines (id, org_id, site_id, duty_id, incident_id, site_work_id, regime_id, rule_key, rule_label, kind, period, unit,
		                       direction, basis, source_ref, caveat, proof_document_types, basis_at, time_zone, jurisdiction, due_at,
		                       excluded_days, computed_in_version, added_by_satisfaction_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, now(), '{}', $21, $22)`,
		id, sub.OrgID, sub.SiteID, dutyID, inc, work, sub.RegimeID, r.Key, r.Label, r.Kind, r.Period, r.Unit,
		r.Direction, r.Basis, r.SourceRef, caveat, proof, basisAt, sub.Regime.TimeZone, sub.Regime.Jurisdiction, sub.Version,
		sub.AddedBySatisfaction)
	if err != nil {
		return fmt.Errorf("insert deadline: %w", err)
	}
	return nil
}

// NewSatisfaction is what InsertSatisfaction stores.
type NewSatisfaction struct {
	ID, DutyID, DeadlineID    string
	SatisfiedAt               time.Time
	Method                    string
	Reference, Note, Evidence *string
	Document                  *string
	DocumentVersion           *int
	RecordedBy                string
	DeviceID                  *string
}

// InsertSatisfaction records a satisfaction against the duty's live row.
func InsertSatisfaction(ctx context.Context, db DBTX, orgID, siteID string, s NewSatisfaction) error {
	_, err := db.Exec(ctx, `
		INSERT INTO deadline_satisfactions (id, org_id, site_id, duty_id, deadline_id, satisfied_at, method, reference, note,
		                                    evidence_id, document_id, document_version, recorded_by, device_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		s.ID, orgID, siteID, s.DutyID, s.DeadlineID, s.SatisfiedAt, s.Method, s.Reference, s.Note, s.Evidence,
		s.Document, s.DocumentVersion, s.RecordedBy, s.DeviceID)
	if err != nil {
		return fmt.Errorf("insert satisfaction: %w", err)
	}
	return nil
}

// InsertRetraction takes a satisfaction back; the trigger reopens the duty.
func InsertRetraction(ctx context.Context, db DBTX, orgID, siteID, satisfactionID, dutyID, by, reason string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO deadline_satisfaction_retractions (satisfaction_id, org_id, site_id, duty_id, retracted_by, reason)
		VALUES ($1, $2, $3, $4, $5, $6)`, satisfactionID, orgID, siteID, dutyID, by, reason)
	if err != nil {
		return fmt.Errorf("insert retraction: %w", err)
	}
	return nil
}
