package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

func HashPassword(password string) (string, error){
	hash, err := bcrypt.GenerateFromPassword([]byte(password),bcrypt.DefaultCost)
	if err != nil{
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func CheckPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func NewAccessToken(userID, orgID, sessionID, secret string, ttl time.Duration) (string, error){
	now := time.Now()

	claims := Claims{
		OrgID: orgID,
		SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: userID,
			IssuedAt: jwt.NewNumericDate(now),
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

func ParseAccessToken(tokenString, secret string)(*Claims, error){
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

func NewRefreshToken() (plain string, hash string, err error){
	b := make([]byte, 32)

	if _, err := rand.Read(b); err != nil {
return "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	plain = base64.RawURLEncoding.EncodeToString(b)
	return plain, HashRefreshToken(plain), nil
}

func HashRefreshToken(plain string) string{
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}