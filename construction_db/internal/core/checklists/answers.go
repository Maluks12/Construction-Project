package checklists

import (
	"context"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/evidence"
)

// Answer sets one item's whole answer: result, severity, comment and photos.
// Only the inspector who started the inspection may answer, while it is in
// progress; an archived site doesn't stop them (decided 10 Nov).
func (s *Service) Answer(ctx context.Context, orgID, userID, role, siteID, inspectionID, itemKey string, req AnswerRequest) (*InspectionItem, error) {
	if err := normaliseAnswer(&req, s.Clock.Now()); err != nil {
		return nil, err
	}

	var item *InspectionItem
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		// The row lock orders this against a completion or another answer:
		// one waits for the other, then sees what it committed.
		in, err := GetSiteInspectionForUpdate(ctx, tx, orgID, site.ID, inspectionID)
		if isNotFound(err) {
			return ErrInspectionNotFound
		}
		if err != nil {
			return err
		}
		if in.StartedBy != userID {
			return ErrNotInspector
		}
		if err := inProgress(in); err != nil {
			return err
		}

		stored, err := GetItem(ctx, tx, orgID, in.ID, itemKey)
		if isNotFound(err) {
			return ErrItemNotFound
		}
		if err != nil {
			return err
		}

		// Answers arrive from phones, maybe out of order. Keep the newest by
		// the device's clock; an exact repeat is a retry.
		if prev := stored.ClientAnsweredAt; prev != nil {
			switch {
			case req.ClientAnsweredAt.Before(*prev):
				return &StaleAnswerError{Err: ErrNewerAnswer, Stored: *stored}
			case req.ClientAnsweredAt.Equal(*prev) && sameAnswer(stored, req):
				item = stored
				return nil
			case req.ClientAnsweredAt.Equal(*prev):
				return &StaleAnswerError{Err: ErrDifferentAnswer, Stored: *stored}
			}
		}

		if err := checkPhotos(ctx, tx, orgID, site.ID, req.EvidenceIDs); err != nil {
			return err
		}
		if err := SaveAnswer(ctx, tx, orgID, site.ID, in.ID, itemKey, req); err != nil {
			return err
		}
		item, err = GetItem(ctx, tx, orgID, in.ID, itemKey)
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("answer inspection item: %w", err)
	}
	return item, nil
}

// inProgress refuses a change to an inspection that is no longer in progress.
func inProgress(in *Inspection) error {
	switch in.Status {
	case StatusCompleted:
		return ErrInspectionCompleted
	case StatusAbandoned:
		return ErrInspectionAbandoned
	}
	return nil
}

// normaliseAnswer checks what can be checked without the database, and puts
// the request in the form it is stored in, so a retry compares equal.
func normaliseAnswer(req *AnswerRequest, now time.Time) error {
	if req.ClientAnsweredAt.After(now.Add(maxClockSkew)) {
		return ErrAnswerInFuture
	}
	switch {
	case req.Severity != nil && !slices.ContainsFunc(Severities, func(s SeverityLevel) bool { return s.Key == *req.Severity }):
		return ErrBadSeverity
	case req.Result == ResultFail && req.Severity == nil:
		return ErrSeverityRequired
	case req.Result != ResultFail && req.Severity != nil:
		return ErrSeverityNotAllowed
	}
	// Characters, not bytes: the column's CHECK uses char_length.
	if req.Comment != nil && utf8.RuneCountInString(*req.Comment) > maxCommentChars {
		return ErrCommentTooLong
	}
	if len(req.EvidenceIDs) > maxPhotosPerItem {
		return ErrTooManyPhotos
	}
	ids := make([]string, 0, len(req.EvidenceIDs))
	for _, s := range req.EvidenceIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return ErrBadEvidenceIDs
		}
		if slices.Contains(ids, id.String()) {
			return ErrRepeatedEvidence
		}
		ids = append(ids, id.String())
	}
	req.EvidenceIDs = ids
	// Postgres keeps microseconds; truncating makes a retry compare equal.
	req.ClientAnsweredAt = req.ClientAnsweredAt.Truncate(time.Microsecond)
	return nil
}

// sameAnswer: the stored answer says exactly what req says, photos as a set.
func sameAnswer(stored *InspectionItem, req AnswerRequest) bool {
	if stored.Result == nil || *stored.Result != req.Result ||
		!equalPtr(stored.Severity, req.Severity) || !equalPtr(stored.Comment, req.Comment) {
		return false
	}
	have := make([]string, 0, len(stored.Evidence))
	for _, e := range stored.Evidence {
		have = append(have, e.EvidenceID)
	}
	return slices.Equal(slices.Sorted(slices.Values(have)), slices.Sorted(slices.Values(req.EvidenceIDs)))
}

// checkPhotos: every photo must be verified evidence on this site. The
// foreign keys refuse anything else anyway; this names the ids and tells a
// photo that is still uploading (409, retry later) from one that never will
// be attachable (400).
func checkPhotos(ctx context.Context, db DBTX, orgID, siteID string, ids []string) error {
	statuses, err := EvidenceStatuses(ctx, db, orgID, siteID, ids)
	if err != nil {
		return err
	}
	var missing, rejected, pending []string
	for _, id := range ids {
		switch statuses[id] {
		case "":
			missing = append(missing, id)
		case evidence.StatusRejected:
			rejected = append(rejected, id)
		case evidence.StatusPending:
			pending = append(pending, id)
		}
	}
	switch {
	case len(missing) > 0:
		return &EvidenceError{Err: ErrEvidenceNotFound, IDs: missing}
	case len(rejected) > 0:
		return &EvidenceError{Err: ErrEvidenceRejected, IDs: rejected}
	case len(pending) > 0:
		return &EvidenceError{Err: ErrEvidenceNotVerified, IDs: pending}
	}
	return nil
}
