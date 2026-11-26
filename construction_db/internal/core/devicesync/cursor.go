package devicesync

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
)

const cursorVersion = 1

// cursor is opaque to the phone. A finished pass carries only Lo: the next
// delta reads sync_xid >= Lo. A pass with more pages also carries its pinned
// Hi, whether it is bounded (a delta) or not (a full pass), and the last
// item sent (Kind, After).
type cursor struct {
	V       int    `json:"v"`
	Epoch   string `json:"e"`
	Scope   string `json:"s"`
	Org     string `json:"o"` // one person may belong to several organisations
	User    string `json:"u"`
	Role    string `json:"r"`
	Lo      string `json:"lo"`
	Hi      string `json:"hi,omitempty"`
	Bounded bool   `json:"b,omitempty"`
	Kind    string `json:"k,omitempty"`
	After   string `json:"a,omitempty"`
}

func (c cursor) paging() bool { return c.Hi != "" }

func (c cursor) encode() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, error) {
	var c cursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil || c.V != cursorVersion || c.Epoch == "" || c.Scope == "" {
		return cursor{}, ErrBadCursor
	}
	if !isXid(c.Lo) || (c.Hi != "" && !isXid(c.Hi)) || (c.Kind != "" && c.Hi == "") {
		return cursor{}, ErrBadCursor
	}
	return c, nil
}

func isXid(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

func xidLess(a, b string) bool {
	x, _ := strconv.ParseUint(a, 10, 64)
	y, _ := strconv.ParseUint(b, 10, 64)
	return x < y
}
