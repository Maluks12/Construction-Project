package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	OrgID     string `json:"org"`
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

var ErrInvalidToken = errors.New("invalid token")

// PasswordCost is bcrypt's cost for new hashes. Tests lower it (TestMain);
// the API refuses to start below the default (CheckPasswordCost), and
// archtest refuses it set under cmd/.
var PasswordCost = bcrypt.DefaultCost

// CheckPasswordCost fails unless new hashes use at least bcrypt's default cost.
func CheckPasswordCost() error {
	if PasswordCost < bcrypt.DefaultCost {
		return fmt.Errorf("password hashing cost %d is below bcrypt's default %d (a test setting)", PasswordCost, bcrypt.DefaultCost)
	}
	return nil
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func CheckPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func NewAccessToken(userID, orgID, sessionID, secret string, ttl time.Duration) (string, error) {
	now := time.Now()

	claims := Claims{
		OrgID:     orgID,
		SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

func ParseAccessToken(tokenString, secret string) (*Claims, error) {
	claims := &Claims{}

	_, err := jwt.ParseWithClaims(
		tokenString,
		claims,

		func(t *jwt.Token) (any, error) {
			return []byte(secret), nil
		},

		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),

		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	return claims, nil
}

func NewRefreshToken() (plain string, hash string, err error) {
	b := make([]byte, 32)

	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	plain = base64.RawURLEncoding.EncodeToString(b)
	return plain, HashRefreshToken(plain), nil
}

func HashRefreshToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// replayKey is derived from a refresh token's plaintext, which only its
// holder has (the database keeps its hash), so a sealed replay can be
// opened only by someone presenting that token again.
func replayKey(plain string) ([]byte, error) {
	return hkdf.Key(sha256.New, []byte(plain), []byte("siteproof refresh replay"), "v1", 32)
}

// sealReplay encrypts the pair a rotation answered (AES-256-GCM, a random
// nonce first).
func sealReplay(oldPlain string, pair *TokenPair) ([]byte, error) {
	key, err := replayKey(oldPlain)
	if err != nil {
		return nil, fmt.Errorf("seal replay: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, fmt.Errorf("seal replay: %w", err)
	}
	msg, err := json.Marshal(pair)
	if err != nil {
		return nil, fmt.Errorf("seal replay: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("seal replay: %w", err)
	}
	return gcm.Seal(nonce, nonce, msg, nil), nil
}

func openReplay(oldPlain string, box []byte) (*TokenPair, error) {
	key, err := replayKey(oldPlain)
	if err != nil {
		return nil, fmt.Errorf("open replay: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, fmt.Errorf("open replay: %w", err)
	}
	if len(box) < gcm.NonceSize() {
		return nil, errors.New("open replay: too short")
	}
	msg, err := gcm.Open(nil, box[:gcm.NonceSize()], box[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("open replay: %w", err)
	}
	var pair TokenPair
	if err := json.Unmarshal(msg, &pair); err != nil {
		return nil, fmt.Errorf("open replay: %w", err)
	}
	return &pair, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
