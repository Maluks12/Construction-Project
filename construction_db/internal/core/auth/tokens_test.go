package auth

import (
	"errors"
	"testing"
	"time"
)

const testSecret = "test-secret-that-is-at-least-32-characters"

func TestPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-1")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !CheckPassword(hash, "correct-horse-1") {
		t.Error("right password should match")
	}
	if CheckPassword(hash, "wrong-password") {
		t.Error("wrong password should not match")
	}
}

func TestAccessTokenRoundTrip(t *testing.T) {
	token, err := NewAccessToken("user-1", "org-1", "sess-1", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("NewAccessToken: %v", err)
	}

	claims, err := ParseAccessToken(token, testSecret)
	if err != nil {
		t.Fatalf("ParseAccessToken: %v", err)
	}
	if claims.Subject != "user-1" || claims.OrgID != "org-1" || claims.SessionID != "sess-1" {
		t.Errorf("claims came back wrong: %+v", claims)
	}
}

// A table-driven test: one test body, run once per entry in the map.
func TestAccessTokenRejected(t *testing.T) {
	expired, _ := NewAccessToken("u", "o", "s", testSecret, -time.Minute)
	valid, _ := NewAccessToken("u", "o", "s", testSecret, time.Minute)

	tests := map[string]struct {
		token  string
		secret string
	}{
		"expired":      {expired, testSecret},
		"wrong secret": {valid, "a-completely-different-secret-value-here"},
		"garbage":      {"not.a.token", testSecret},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseAccessToken(tc.token, tc.secret)
			if !errors.Is(err, ErrInvalidToken) {
				t.Errorf("want ErrInvalidToken, got %v", err)
			}
		})
	}
}

func TestRefreshToken(t *testing.T) {
	plain1, hash1, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	plain2, _, _ := NewRefreshToken()

	if plain1 == plain2 {
		t.Error("two refresh tokens should never be equal")
	}
	if HashRefreshToken(plain1) != hash1 {
		t.Error("HashRefreshToken should reproduce the returned hash")
	}
}
