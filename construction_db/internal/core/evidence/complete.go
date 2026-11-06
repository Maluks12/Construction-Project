package evidence

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the decoders image.DecodeConfig uses
	_ "image/png"
	"io"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/sites"
	"construction_db/internal/platform/storage"
)

// Complete checks the bytes S3 stored for pending evidence and, if they are
// what was declared, seals the evidence into its site's chain. created is
// false when it was already verified (an idempotent retry).
func (s *Service) Complete(ctx context.Context, orgID, userID, role, siteID, evidenceID string) (ev *Evidence, created bool, err error) {
	ev, err = s.loadForComplete(ctx, orgID, userID, role, siteID, evidenceID)
	if err != nil || ev.Status == StatusVerified {
		return ev, false, err
	}

	// Outside any transaction: the chain lock must never wait on S3.
	info, err := s.store.Stat(ctx, ev.ObjectKey)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, false, ErrNotUploaded
	}
	if err != nil {
		return nil, false, fmt.Errorf("complete evidence: %w", err)
	}
	if info.LockMode == "" || info.RetainUntil == nil {
		return nil, false, fmt.Errorf("complete evidence: object %s has no Object Lock retention; check the bucket's default retention", ev.ObjectKey)
	}

	// Read exactly the version Stat saw, so what is checked is what is sealed.
	r, err := s.store.Open(ctx, ev.ObjectKey, info.VersionID)
	if err != nil {
		return nil, false, fmt.Errorf("complete evidence: %w", err)
	}
	rej, err := inspect(r, ev)
	r.Close()
	if err != nil {
		return nil, false, fmt.Errorf("complete evidence: %w", err)
	}
	if rej != nil {
		return s.reject(ctx, orgID, userID, ev, rej)
	}
	return s.appendToChain(ctx, orgID, userID, ev.ID, info)
}

func (s *Service) loadForComplete(ctx context.Context, orgID, userID, role, siteID, evidenceID string) (*Evidence, error) {
	var ev *Evidence
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		ev, err = GetSiteEvidence(ctx, tx, orgID, site.ID, evidenceID)
		// Only the uploader completes; anyone else can't tell it exists.
		if isNotFound(err) || (err == nil && ev.UploadedBy != userID) {
			return ErrEvidenceNotFound
		}
		if err == nil && ev.Status == StatusRejected {
			return ErrRejected
		}
		return err
	})
	if errors.Is(err, sites.ErrSiteNotFound) || errors.Is(err, ErrEvidenceNotFound) || errors.Is(err, ErrRejected) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("complete evidence: %w", err)
	}
	return ev, nil
}

// inspect reads the stored file once, in constant memory. Every byte passes
// through the hash and the counter exactly once (the tee sits below the
// bufio.Reader); DecodeConfig reads the image header from the same stream,
// however much metadata comes first, and the rest is then drained.
// It returns why the file is rejected, or nil if it passes.
func inspect(r io.Reader, ev *Evidence) (*RejectedError, error) {
	sum := sha256.New()
	var size countWriter
	// One byte past the limit is enough to know the file is too long.
	src := &readErrReader{r: io.LimitReader(r, MaxSizeBytes+1)}
	br := bufio.NewReader(io.TeeReader(src, io.MultiWriter(sum, &size)))

	cfg, format, decodeErr := image.DecodeConfig(br)
	if _, err := io.Copy(io.Discard, br); err != nil {
		return nil, fmt.Errorf("read stored file: %w", err)
	}
	// A read that failed partway (say, while DecodeConfig was reading) would
	// leave the count short; that's a storage error, never a rejection.
	if src.err != nil {
		return nil, fmt.Errorf("read stored file: %w", src.err)
	}

	if int64(size) != ev.SizeBytes {
		return rejected(ReasonSizeMismatch), nil
	}
	if hex.EncodeToString(sum.Sum(nil)) != ev.ContentSHA256 {
		return rejected(ReasonHashMismatch), nil
	}
	// The hash is the integrity check; this enforces the format and Rev 4's
	// 2048px rule.
	if decodeErr != nil {
		return rejected(ReasonNotAnImage), nil
	}
	if "image/"+format != ev.ContentType {
		return rejected(ReasonWrongType), nil
	}
	if max(cfg.Width, cfg.Height) > MaxImageEdge {
		return rejected(ReasonTooLargeDimensions), nil
	}
	return nil, nil
}

// reject records why the stored file failed. If another request finished this
// evidence first, its outcome is the answer instead.
func (s *Service) reject(ctx context.Context, orgID, userID string, ev *Evidence, rej *RejectedError) (*Evidence, bool, error) {
	var current *Evidence
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		marked, err := MarkRejected(ctx, tx, orgID, ev.ID, rej.Reason)
		if err != nil || marked {
			return err
		}
		current, err = GetEvidence(ctx, tx, orgID, ev.ID)
		return err
	})
	switch {
	case err != nil:
		return nil, false, fmt.Errorf("reject evidence: %w", err)
	case current != nil && current.Status == StatusVerified:
		return current, false, nil
	default:
		return nil, false, rej
	}
}

// appendToChain seals verified evidence as the next entry in its site's chain.
// The head row lock serialises appends per site; locks are taken head first,
// then evidence, everywhere, so two appends can't deadlock.
func (s *Service) appendToChain(ctx context.Context, orgID, userID, id string, info *storage.ObjectInfo) (ev *Evidence, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		cur, err := GetEvidence(ctx, tx, orgID, id)
		if err != nil {
			return err
		}
		if err := EnsureHead(ctx, tx, orgID, cur.SiteID); err != nil {
			return err
		}
		seq, prev, err := LockHead(ctx, tx, orgID, cur.SiteID)
		if err != nil {
			return err
		}
		// Re-read under the lock: a concurrent complete may have sealed it.
		cur, err = GetEvidenceForUpdate(ctx, tx, orgID, id)
		if err != nil {
			return err
		}
		if cur.Status == StatusVerified {
			ev = cur
			return nil
		}
		if cur.Status != StatusPending {
			return ErrRejected
		}

		// After the lock, so server_received_at never decreases along seq.
		received, err := Now(ctx, tx)
		if err != nil {
			return err
		}
		meta, err := MetadataHash(Metadata{
			EvidenceID:       cur.ID,
			ClientCapturedAt: cur.ClientCapturedAt,
			Lat:              cur.Lat,
			Lng:              cur.Lng,
			UploadedBy:       cur.UploadedBy,
			DeviceID:         cur.DeviceID,
		})
		if err != nil {
			return err
		}
		content, err := hex.DecodeString(cur.ContentSHA256)
		if err != nil {
			return err
		}
		link, err := ChainHash(Link{
			ContentSHA256:    content,
			ServerReceivedAt: received,
			SiteID:           cur.SiteID,
			PrevChainHash:    prev,
			MetadataHash:     meta[:],
		})
		if err != nil {
			return err
		}

		err = InsertChainEntry(ctx, tx, chainEntry{
			EvidenceID: cur.ID, OrgID: orgID, SiteID: cur.SiteID, Seq: seq + 1,
			ContentSHA256: content, ServerReceivedAt: received,
			PrevChainHash: prev, MetadataHash: meta[:], ChainHash: link[:],
		})
		if err != nil {
			return err
		}
		if ok, err := AdvanceHead(ctx, tx, orgID, cur.SiteID, seq+1, link[:]); err != nil || !ok {
			return errOrNoRow(err, "advance chain head")
		}
		if ok, err := MarkVerified(ctx, tx, orgID, cur.ID, info.VersionID, *info.RetainUntil); err != nil || !ok {
			return errOrNoRow(err, "mark verified")
		}
		created = true
		ev, err = GetEvidence(ctx, tx, orgID, id)
		return err
	})
	if errors.Is(err, ErrRejected) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("append to chain: %w", err)
	}
	return ev, created, nil
}

func errOrNoRow(err error, what string) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s: no row changed", what)
}

type countWriter int64

func (c *countWriter) Write(p []byte) (int, error) {
	*c += countWriter(len(p))
	return len(p), nil
}

// readErrReader remembers the first read error other than EOF, which a
// decoder reading through it might otherwise swallow.
type readErrReader struct {
	r   io.Reader
	err error
}

func (e *readErrReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}
