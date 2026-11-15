package incidents

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RecordStatement stores a witness statement under an id the device chose.
// Statements can be taken any time after the incident, on an archived site
// too. created is false for an identical repeat.
func (s *Service) RecordStatement(ctx context.Context, orgID, userID, role, siteID, incidentID, statementID string, req StatementRequest) (*WitnessStatement, bool, error) {
	id, err := uuid.Parse(statementID)
	if err != nil {
		return nil, false, ErrBadStatementID
	}
	if err := normaliseStatement(&req, time.Now()); err != nil {
		return nil, false, err
	}

	var (
		st      *WitnessStatement
		created bool
	)
	for attempt := 1; ; attempt++ {
		st, created, err = s.recordStatementOnce(ctx, orgID, userID, role, siteID, incidentID, id.String(), req)
		if !errors.Is(err, errIDTaken) || attempt == 2 {
			break
		}
	}
	if errors.Is(err, errIDTaken) {
		err = ErrStatementIDReused
	}
	if isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("record statement: %w", err)
	}
	return st, created, nil
}

func (s *Service) recordStatementOnce(ctx context.Context, orgID, userID, role, siteID, incidentID, id string, req StatementRequest) (st *WitnessStatement, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		h, err := getHeader(ctx, tx, orgID, site.ID, incidentID)
		if isNotFound(err) {
			return ErrIncidentNotFound
		}
		if err != nil {
			return err
		}

		existing, incident, err := getStatement(ctx, tx, orgID, id)
		switch {
		case err == nil && (incident != h.ID || !sameStatement(existing, userID, req)):
			return ErrStatementIDReused
		case err == nil:
			st = &existing
			return nil
		case !isNotFound(err):
			return err
		}

		if req.SupersedesID != nil {
			old, incident, err := getStatement(ctx, tx, orgID, *req.SupersedesID)
			if isNotFound(err) || (err == nil && incident != h.ID) {
				return ErrBadSupersedes
			}
			if err != nil {
				return err
			}
			if old.SupersededBy != nil {
				return ErrStatementCorrected
			}
		}
		if req.EvidenceID != nil {
			if err := checkPhotos(ctx, tx, orgID, site.ID, []string{*req.EvidenceID}); err != nil {
				return err
			}
		}

		err = insertStatement(ctx, tx, orgID, site.ID, h.ID, id, userID, req)
		switch {
		case isUniqueViolation(err, "witness_statements_pkey"):
			return errIDTaken
		case isUniqueViolation(err, "witness_statements_supersedes_once"):
			return ErrStatementCorrected // a concurrent correction won
		case err != nil:
			return err
		}
		created = true
		stored, _, err := getStatement(ctx, tx, orgID, id)
		st = &stored
		return err
	})
	return st, created, err
}

func normaliseStatement(req *StatementRequest, now time.Time) error {
	if n := utf8.RuneCountInString(req.WitnessName); n < 1 || n > 200 || !utf8.ValidString(req.WitnessName) {
		return ErrBadWitnessName
	}
	if !textOK(req.WitnessContact, 200) {
		return ErrBadWitnessContact
	}
	if n := utf8.RuneCountInString(req.Statement); n < 1 || n > 20000 || !utf8.ValidString(req.Statement) {
		return ErrBadStatement
	}
	if req.TakenAt.IsZero() {
		return ErrNoTakenAt
	}
	req.TakenAt = micro(req.TakenAt)
	if req.TakenAt.After(now.Add(clockSkew)) {
		return ErrTakenInFuture
	}
	if d := req.DeviceID; d != nil && (len(*d) == 0 || len(*d) > 200 || !utf8.ValidString(*d)) {
		return ErrBadDeviceID
	}
	if req.EvidenceID != nil {
		e, err := uuid.Parse(*req.EvidenceID)
		if err != nil {
			return ErrBadEvidenceID
		}
		canonical := e.String()
		req.EvidenceID = &canonical
	}
	if req.SupersedesID != nil {
		e, err := uuid.Parse(*req.SupersedesID)
		if err != nil {
			return ErrBadSupersedes
		}
		canonical := e.String()
		req.SupersedesID = &canonical
	}
	return nil
}

func sameStatement(w WitnessStatement, by string, req StatementRequest) bool {
	var evidenceID *string
	if w.Evidence != nil {
		evidenceID = &w.Evidence.EvidenceID
	}
	return w.TakenBy == by && w.WitnessName == req.WitnessName && equalPtr(w.WitnessContact, req.WitnessContact) &&
		w.Statement == req.Statement && w.TakenAt.Equal(req.TakenAt) && equalPtr(w.DeviceID, req.DeviceID) &&
		equalPtr(evidenceID, req.EvidenceID) && equalPtr(w.SupersedesID, req.SupersedesID)
}
