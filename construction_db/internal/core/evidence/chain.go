package evidence

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// The byte encoding is specified, with test vectors, in CHAIN.md. Change it
// only by adding a new metadata format version.
const (
	metadataFormat   = 1
	maxDeviceIDBytes = 200
)

// GenesisHash is the prev_chain_hash of a site's first entry.
var GenesisHash [32]byte

// Metadata is what metadata_hash seals. Values must be read back from
// Postgres, not taken from a request.
type Metadata struct {
	EvidenceID       string
	ClientCapturedAt time.Time
	Lat, Lng         *float64 // both or neither
	UploadedBy       string
	DeviceID         *string
}

// Link is one chain entry's input. Hashes are 32 bytes, as scanned from bytea.
type Link struct {
	ContentSHA256    []byte
	ServerReceivedAt time.Time
	SiteID           string
	PrevChainHash    []byte
	MetadataHash     []byte
}

func MetadataHash(m Metadata) ([32]byte, error) {
	b, err := encodeMetadata(m)
	if err != nil {
		return [32]byte{}, fmt.Errorf("metadata hash: %w", err)
	}
	return sha256.Sum256(b), nil
}

func ChainHash(l Link) ([32]byte, error) {
	b, err := encodeLink(l)
	if err != nil {
		return [32]byte{}, fmt.Errorf("chain hash: %w", err)
	}
	return sha256.Sum256(b), nil
}

func encodeMetadata(m Metadata) ([]byte, error) {
	id, err := uuidBytes("evidence_id", m.EvidenceID)
	if err != nil {
		return nil, err
	}
	captured, err := micros("client_captured_at", m.ClientCapturedAt)
	if err != nil {
		return nil, err
	}
	uploader, err := uuidBytes("uploaded_by", m.UploadedBy)
	if err != nil {
		return nil, err
	}

	b := make([]byte, 0, 60+maxDeviceIDBytes)
	b = append(b, metadataFormat)
	b = append(b, id...)
	b = binary.BigEndian.AppendUint64(b, uint64(captured))

	switch {
	case m.Lat == nil && m.Lng == nil:
		// The flag keeps "no GPS" distinct from a fix at exactly (0, 0).
		b = append(b, 0)
		b = append(b, make([]byte, 16)...)
	case m.Lat != nil && m.Lng != nil:
		if !finite(*m.Lat) || !finite(*m.Lng) {
			return nil, errors.New("lat and lng must be finite")
		}
		b = append(b, 1)
		b = binary.BigEndian.AppendUint64(b, math.Float64bits(*m.Lat))
		b = binary.BigEndian.AppendUint64(b, math.Float64bits(*m.Lng))
	default:
		return nil, errors.New("lat and lng must both be set or both be absent")
	}

	b = append(b, uploader...)

	if m.DeviceID == nil {
		b = binary.BigEndian.AppendUint16(b, 0)
		return b, nil
	}
	d := *m.DeviceID
	// Empty is refused so that it can't collide with absent (length 0).
	if len(d) == 0 || len(d) > maxDeviceIDBytes || !utf8.ValidString(d) {
		return nil, fmt.Errorf("device_id must be 1-%d bytes of UTF-8", maxDeviceIDBytes)
	}
	b = binary.BigEndian.AppendUint16(b, uint16(len(d)))
	return append(b, d...), nil
}

func encodeLink(l Link) ([]byte, error) {
	for name, h := range map[string][]byte{
		"content_sha256":  l.ContentSHA256,
		"prev_chain_hash": l.PrevChainHash,
		"metadata_hash":   l.MetadataHash,
	} {
		if len(h) != 32 {
			return nil, fmt.Errorf("%s is %d bytes, want 32", name, len(h))
		}
	}
	received, err := micros("server_received_at", l.ServerReceivedAt)
	if err != nil {
		return nil, err
	}
	site, err := uuidBytes("site_id", l.SiteID)
	if err != nil {
		return nil, err
	}

	b := make([]byte, 0, 120)
	b = append(b, l.ContentSHA256...)
	b = binary.BigEndian.AppendUint64(b, uint64(received))
	b = append(b, site...)
	b = append(b, l.PrevChainHash...)
	return append(b, l.MetadataHash...), nil
}

func uuidBytes(name, s string) ([]byte, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return id[:], nil
}

// micros refuses sub-microsecond digits: Postgres keeps microseconds, so such
// a time didn't come from the database and would not hash the same there.
func micros(name string, t time.Time) (int64, error) {
	if t.Nanosecond()%1000 != 0 {
		return 0, fmt.Errorf("%s has sub-microsecond precision; read it back from Postgres", name)
	}
	return t.UnixMicro(), nil
}

func finite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
