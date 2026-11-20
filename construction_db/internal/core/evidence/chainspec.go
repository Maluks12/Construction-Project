package evidence

import (
	_ "embed"
	"regexp"
	"strings"
)

//go:embed CHAIN.md
var chainDoc string

var (
	repoOnly    = regexp.MustCompile(`(?s)<!-- repo-only -->.*?<!-- /repo-only -->\n?`)
	markerLine  = regexp.MustCompile(`(?m)^<!-- [a-z-]+ -->\n`)
	blankBlocks = regexp.MustCompile(`\n{3,}`)
)

// ChainSpec is CHAIN.md as an audit pack carries it: the specification
// without the blocks marked repo-only or the tests' markers, so the two
// can't drift.
func ChainSpec() string {
	doc := strings.ReplaceAll(chainDoc, "\r\n", "\n")
	doc = repoOnly.ReplaceAllString(doc, "")
	doc = markerLine.ReplaceAllString(doc, "")
	return blankBlocks.ReplaceAllString(doc, "\n\n")
}
