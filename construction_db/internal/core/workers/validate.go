package workers

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"construction_db/internal/core/toolbox"
	"construction_db/internal/regimes"
)

// A device clock may run this far ahead of the server; the database allows
// the same.
const clockSkew = 5 * time.Minute

var practicePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ./-]{0,29}$`)

// ValidateWorker checks one worker without the database. The API and
// ONBOARD's CSV import (row by row) run exactly this (D10).
func ValidateWorker(in WorkerRequest) []FieldProblem {
	var out []FieldProblem
	check := func(ok bool, field, msg string) {
		if !ok {
			out = append(out, FieldProblem{Field: field, Error: msg})
		}
	}
	check(textOK(&in.Name, 200), "name", MsgName)
	check(textOK(in.Firm, 200), "firm", MsgFirm)
	check(textOK(in.ClockNumber, 50), "clock_number", MsgClock)
	check(textOK(in.Trade, 100), "trade", MsgTrade)
	return out
}

// ValidateCompetency checks a competency against the regime it names.
func ValidateCompetency(reg *regimes.Registry, in CompetencyRequest, now time.Time) []FieldProblem {
	var out []FieldProblem
	add := func(field, msg string) { out = append(out, FieldProblem{Field: field, Error: msg}) }
	ds, ok := reg.Get(in.RegimeCode)
	switch {
	case !ok:
		add("regime_code", MsgRegime)
	default:
		ct, ok := ds.CompetencyType(in.TypeKey)
		switch {
		case !ok:
			add("type_key", MsgType)
		case ct.Expiry == regimes.ExpiryRequired && in.ExpiresOn == nil:
			add("expires_on", MsgExpiryNeed)
		case ct.Expiry == regimes.ExpiryNone && in.ExpiresOn != nil:
			add("expires_on", MsgExpiryNone)
		}
	}
	if !textOK(in.Number, 100) {
		add("number", MsgNumber)
	}
	if !textOK(in.Issuer, 200) {
		add("issuer", MsgIssuer)
	}
	issued, err := time.Parse(time.DateOnly, in.IssuedOn)
	if err != nil {
		add("issued_on", MsgIssuedOn)
	}
	if in.ExpiresOn != nil {
		exp, err2 := time.Parse(time.DateOnly, *in.ExpiresOn)
		if err2 != nil || (err == nil && exp.Before(issued)) {
			add("expires_on", MsgExpiresOn)
		}
	}
	out = append(out, recordCommon(in.FileID, in.SupersedesID, in.ClientRecordedAt, in.DeviceID, now)...)
	return out
}

// ValidateMedical checks a medical: an outcome and dates, nothing else (D6).
func ValidateMedical(reg *regimes.Registry, in MedicalRequest, now time.Time) []FieldProblem {
	var out []FieldProblem
	add := func(field, msg string) { out = append(out, FieldProblem{Field: field, Error: msg}) }
	if _, ok := reg.Get(in.RegimeCode); !ok {
		add("regime_code", MsgRegime)
	}
	if !slices.Contains(Outcomes, in.Outcome) {
		add("outcome", MsgOutcome)
	}
	examined, err := time.Parse(time.DateOnly, in.ExaminedOn)
	if err != nil {
		add("examined_on", MsgExaminedOn)
	}
	if exp, err2 := time.Parse(time.DateOnly, in.ExpiresOn); err2 != nil || (err == nil && exp.Before(examined)) {
		add("expires_on", MsgExpiresOn)
	}
	if in.PracticeNumber != nil && !practicePattern.MatchString(*in.PracticeNumber) {
		add("practice_number", MsgPractice)
	}
	out = append(out, recordCommon(in.FileID, in.SupersedesID, in.ClientRecordedAt, in.DeviceID, now)...)
	return out
}

func recordCommon(fileID string, supersedes *string, at time.Time, device *string, now time.Time) []FieldProblem {
	var out []FieldProblem
	add := func(field, msg string) { out = append(out, FieldProblem{Field: field, Error: msg}) }
	if _, err := uuid.Parse(fileID); err != nil {
		add("file_id", MsgFileID)
	}
	if supersedes != nil {
		if _, err := uuid.Parse(*supersedes); err != nil {
			add("supersedes_id", MsgSupersedes)
		}
	}
	switch {
	case at.IsZero():
		add("client_recorded_at", MsgRecordedAt)
	case at.After(now.Add(clockSkew)):
		add("client_recorded_at", MsgFuture)
	}
	if device != nil && (len(*device) == 0 || len(*device) > 200) {
		add("device_id", MsgDevice)
	}
	return out
}

// RowProblem is a CSV row's problem: the row as the spreadsheet numbers it
// (the header is row 1), the field, the message.
type RowProblem struct {
	Row int `json:"row"`
	FieldProblem
}

// CheckRows validates many workers for an all-or-nothing import, in the
// caller's transaction: each row alone, then identity keys repeated within
// the rows or already on the register. numbers[i] is rows[i]'s row number
// (blank rows are skipped, so they needn't run on); the problems come
// sorted by it.
func CheckRows(ctx context.Context, tx DBTX, orgID string, rows []WorkerRequest, numbers []int) ([]RowProblem, error) {
	var out []RowProblem
	seen := map[string]int{}
	keys := make([]string, len(rows))
	for i, r := range rows {
		for _, p := range ValidateWorker(r) {
			out = append(out, RowProblem{Row: numbers[i], FieldProblem: p})
		}
		keys[i] = IdentityKey(r)
		if first, ok := seen[keys[i]]; ok {
			out = append(out, RowProblem{Row: numbers[i], FieldProblem: FieldProblem{Field: "name", Error: MsgSameAsRow + itoa(numbers[first])}})
		} else {
			seen[keys[i]] = i
		}
	}
	taken, err := keysOnRegister(ctx, tx, orgID, keys)
	if err != nil {
		return nil, err
	}
	for i, k := range keys {
		if taken[k] {
			out = append(out, RowProblem{Row: numbers[i], FieldProblem: FieldProblem{Field: "name", Error: ErrIdentityTaken.Error()}})
		}
	}
	slices.SortStableFunc(out, func(a, b RowProblem) int { return a.Row - b.Row })
	return out, nil
}

// IdentityKey is workers.identity_key in Go: the M10 key (toolbox.IdentityKey).
func IdentityKey(r WorkerRequest) string { return toolbox.IdentityKey(r.Name, r.Firm, r.ClockNumber) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func textOK(s *string, max int) bool {
	if s == nil {
		return true
	}
	n := utf8.RuneCountInString(*s)
	return utf8.ValidString(*s) && n >= 1 && n <= max && !strings.ContainsRune(*s, 0) && strings.TrimSpace(*s) != ""
}
