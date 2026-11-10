package regimes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Checklist templates are append-only: a key appears once per version, oldest
// first, and every version that has shipped stays in the data. An inspection
// records (key, version, ContentHash), so an old inspection can always be
// checked against the wording its inspector saw, and an inspection started
// offline before a new version shipped can still be validated.

// templateContent is exactly what ContentHash covers: what an inspector reads.
// It's a separate struct so adding a field to ChecklistTemplate doesn't change
// every hash; a new field that inspectors see belongs here too, in a new
// version of each template it affects.
type templateContent struct {
	Key       string        `json:"key"`
	Title     string        `json:"title"`
	Cadence   string        `json:"cadence"`
	SourceRef string        `json:"source_ref"`
	Items     []itemContent `json:"items"`
}

type itemContent struct {
	Key          string `json:"key"`
	Prompt       string `json:"prompt"`
	ResponseType string `json:"response_type"`
	SourceRef    string `json:"source_ref"`
}

// ContentHash is the lowercase hex SHA-256 of the template's inspector-visible
// content. The version isn't part of it: the lock in testdata/shipped.json
// pairs each version with its hash.
func (t ChecklistTemplate) ContentHash() string {
	c := templateContent{Key: t.Key, Title: t.Title, Cadence: t.Cadence, SourceRef: t.SourceRef, Items: []itemContent{}}
	for _, it := range t.Items {
		c.Items = append(c.Items, itemContent{Key: it.Key, Prompt: it.Prompt, ResponseType: it.ResponseType, SourceRef: it.SourceRef})
	}
	b, err := json.Marshal(c)
	if err != nil {
		panic(err) // plain strings and slices always marshal
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Latest returns the highest version of each template, in the order the keys
// first appear. This is what new inspections are offered.
func (ds *DataSet) Latest() []ChecklistTemplate {
	out := []ChecklistTemplate{}
	at := map[string]int{}
	for _, t := range ds.Checklists {
		if i, ok := at[t.Key]; ok {
			if t.Version > out[i].Version {
				out[i] = t
			}
			continue
		}
		at[t.Key] = len(out)
		out = append(out, t)
	}
	return out
}

// Checklist returns one exact version of a template. Keys are only unique
// within a regime, so look it up on the site's data set.
func (ds *DataSet) Checklist(key string, version int) (ChecklistTemplate, bool) {
	for _, t := range ds.Checklists {
		if t.Key == key && t.Version == version {
			return t, true
		}
	}
	return ChecklistTemplate{}, false
}
