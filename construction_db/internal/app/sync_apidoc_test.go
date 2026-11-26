package app

import (
	"strings"
	"testing"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/devicesync"
	"construction_db/internal/platform/apidoctest"
)

// The sync reference lives here, not in devicesync: its kinds union names
// the edition's kinds too, and core may not import an edition.
func TestSyncAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "sync")
	for name, v := range map[string]any{
		"PullItem": devicesync.Item{}, "SyncPull": devicesync.Pull{}, "OrgPull": devicesync.OrgPull{},
		"SyncSite": devicesync.SiteEntry{}, "Reference": devicesync.Reference{}, "PushResponse": devicesync.PushResponse{},
		"PushResult": devicesync.PushResult{}, "BatchProblem": devicesync.BatchProblem{}, "TokenPair": auth.TokenPair{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	for name, v := range map[string]any{"PushRequest": devicesync.PushRequest{}, "PushRecord": devicesync.PushRecord{}} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, true) })
	}
	var kinds []string
	for _, k := range syncKinds() {
		kinds = append(kinds, k.Name)
	}
	doc.CheckUnion(t, "SyncKind", kinds)
	for _, e := range devicesync.Sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
	for _, k := range syncKinds() {
		if !strings.Contains(doc.Text, "| `"+k.Name+"` |") {
			t.Errorf("kind %s has no row in the pull table", k.Name)
		}
	}
}
