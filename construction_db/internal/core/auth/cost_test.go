package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// The API refuses to start with a test's cheap bcrypt cost (cmd/api calls
// CheckPasswordCost; archtest checks it does).
func TestCheckPasswordCost(t *testing.T) {
	defer func(c int) { PasswordCost = c }(PasswordCost)
	for _, tc := range []struct {
		cost int
		ok   bool
	}{{bcrypt.DefaultCost, true}, {bcrypt.DefaultCost + 2, true}, {bcrypt.DefaultCost - 1, false}, {bcrypt.MinCost, false}} {
		PasswordCost = tc.cost
		if err := CheckPasswordCost(); (err == nil) != tc.ok {
			t.Errorf("cost %d: %v, want ok %v", tc.cost, err, tc.ok)
		}
	}
}
