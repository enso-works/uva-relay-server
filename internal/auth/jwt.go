package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenExpiry = 30 * 24 * time.Hour // 30 days

// RelayClaims holds JWT claims for agent authentication.
type RelayClaims struct {
	jwt.RegisteredClaims
	Username  string `json:"username"`
	InstallID string `json:"installId"`
}

// IssueToken creates a signed JWT with 30-day expiry.
func IssueToken(secret, username, installID string) (string, error) {
	now := time.Now()
	claims := RelayClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenExpiry)),
		},
		Username:  username,
		InstallID: installID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateToken parses and validates a JWT, returning the claims.
func ValidateToken(secret, tokenString string) (*RelayClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &RelayClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*RelayClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}
	return claims, nil
}

// RefreshToken validates an existing JWT and re-issues it with a fresh expiry.
func RefreshToken(secret, tokenString string) (string, error) {
	claims, err := ValidateToken(secret, tokenString)
	if err != nil {
		return "", err
	}
	return IssueToken(secret, claims.Username, claims.InstallID)
}
