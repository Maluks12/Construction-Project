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

// The replay box opens only with the refresh token that sealed it, and a
// tampered box doesn't open at all.
func TestReplaySeal(t *testing.T) {
	old, _, err := NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	other, _, _ := NewRefreshToken()
	pair := &TokenPair{AccessToken: "a", RefreshToken: "r", ExpiresIn: 900, TokenType: "Bearer", RefreshExpiresAt: time.Now().UTC().Truncate(time.Microsecond)}
	box, err := sealReplay(old, pair)
	if err != nil {
		t.Fatal(err)
	}
	got, err := openReplay(old, box)
	if err != nil || *got != *pair {
		t.Fatalf("open: %+v, %v", got, err)
	}
	if _, err := openReplay(other, box); err == nil {
		t.Error("another token opened the box")
	}
	box[len(box)-1] ^= 1
	if _, err := openReplay(old, box); err == nil {
		t.Error("a tampered box opened")
	}
	if _, err := openReplay(old, box[:5]); err == nil {
		t.Error("a short box opened")
	}
	again, _ := sealReplay(old, pair)
	if string(again) == string(box) {
		t.Error("two seals share a nonce")
	}
}
