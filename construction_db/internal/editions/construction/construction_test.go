package construction

import (
	"testing"

	"construction_db/internal/regimes"
)

func TestEdition(t *testing.T) {
	ed := New()
	if ed.Name() != "construction" {
		t.Errorf("Name() = %q", ed.Name())
	}
	if ed.DefaultRegime() != "za_construction_2014" {
		t.Errorf("DefaultRegime() = %q", ed.DefaultRegime())
	}

	reg, err := regimes.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := reg.CheckDefault(ed.DefaultRegime(), ed.Name()); err != nil {
		t.Fatalf("CheckDefault: %v", err)
	}
}
