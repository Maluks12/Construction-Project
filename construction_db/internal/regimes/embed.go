package regimes

import (
	"embed"
	"fmt"
	"io/fs"
)

// The data sets are compiled into the binary, so the API can never start with
// a missing or half-copied data directory. Changing regime data means a
// redeploy; see "Known spec issues" in construction_db/CLAUDE.md.
//
//go:embed data
var dataFS embed.FS

// Load loads the data sets embedded in the binary.
func Load() (*Registry, error) {
	sub, err := fs.Sub(dataFS, "data")
	if err != nil {
		return nil, fmt.Errorf("load regimes: %w", err)
	}
	return LoadFS(sub)
}
