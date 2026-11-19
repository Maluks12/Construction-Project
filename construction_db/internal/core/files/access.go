package files

import (
	"context"
	"fmt"

	"construction_db/internal/core/evidence"
)

// LogView records a view of a verified file's bytes, in the caller's
// transaction, after the caller has decided the user may see the record the
// file belongs to. A medical certificate is never opened by anyone but an
// owner or SHE officer (D6), whichever record it is reached through. The
// caller signs the URL with Presign only after its transaction commits, so no
// URL exists without a log row.
func LogView(ctx context.Context, tx DBTX, orgID, userID, role, fileID string, client evidence.Client) (*File, error) {
	f, err := GetFile(ctx, tx, orgID, fileID)
	if isNotFound(err) {
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, err
	}
	if f.Purpose == PurposeMedical && !IsLead(role) {
		return nil, ErrCantViewHealth
	}
	if f.Status != StatusVerified || f.objectVersion == nil {
		return nil, ErrNotVerified
	}
	if err := insertAccess(ctx, tx, orgID, f.ID, userID, *f.objectVersion, client.LogIP(), client.LogUserAgent()); err != nil {
		return nil, err
	}
	return f, nil
}

// Presign returns a short-lived URL for the sealed version LogView logged,
// served as the type it was verified to be. Only this file and evidence's
// access.go hand out bytes (internal/archtest).
func (s *Service) Presign(ctx context.Context, f *File) (*evidence.ViewURL, error) {
	p, err := s.store.PresignGet(ctx, f.objectKey, *f.objectVersion, f.ContentType, viewTTL)
	if err != nil {
		return nil, fmt.Errorf("presign file: %w", err)
	}
	return &evidence.ViewURL{Method: p.Method, URL: p.URL, ExpiresAt: p.ExpiresAt.UTC()}, nil
}
