package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-key"

func TestIssueAndValidate(t *testing.T) {
	token, err := IssueToken(testSecret, "alice", "install-123")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	claims, err := ValidateToken(testSecret, token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}

	if claims.Username != "alice" {
		t.Errorf("username = %q, want %q", claims.Username, "alice")
	}
	if claims.InstallID != "install-123" {
		t.Errorf("installID = %q, want %q", claims.InstallID, "install-123")
	}
	if claims.ExpiresAt == nil {
		t.Fatal("ExpiresAt is nil")
	}
	// Expiry should be ~30 days from now
	expiry := time.Until(claims.ExpiresAt.Time)
	if expiry < 29*24*time.Hour || expiry > 31*24*time.Hour {
		t.Errorf("expiry = %v, want ~30 days", expiry)
	}
}

func TestExpiredToken(t *testing.T) {
	// Create a token that's already expired
	now := time.Now()
	claims := RelayClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Hour)),
		},
		Username:  "alice",
		InstallID: "install-123",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = ValidateToken(testSecret, tokenStr)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestInvalidSignature(t *testing.T) {
	token, err := IssueToken(testSecret, "alice", "install-123")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	_, err = ValidateToken("wrong-secret", token)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestRefreshToken(t *testing.T) {
	original, err := IssueToken(testSecret, "alice", "install-123")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	refreshed, err := RefreshToken(testSecret, original)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}

	claims, err := ValidateToken(testSecret, refreshed)
	if err != nil {
		t.Fatalf("ValidateToken refreshed: %v", err)
	}

	if claims.Username != "alice" {
		t.Errorf("username = %q, want %q", claims.Username, "alice")
	}
	if claims.InstallID != "install-123" {
		t.Errorf("installID = %q, want %q", claims.InstallID, "install-123")
	}
}

func TestClaimsExtraction(t *testing.T) {
	token, err := IssueToken(testSecret, "bob", "install-456")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	claims, err := ValidateToken(testSecret, token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}

	if claims.Username != "bob" {
		t.Errorf("username = %q, want %q", claims.Username, "bob")
	}
	if claims.InstallID != "install-456" {
		t.Errorf("installID = %q, want %q", claims.InstallID, "install-456")
	}
	if claims.IssuedAt == nil {
		t.Error("IssuedAt is nil")
	}
}
