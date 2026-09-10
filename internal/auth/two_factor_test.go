package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestGenerateTOTPSecret_ReturnsValidSecretAndOtpauthURI(t *testing.T) {
	secret, uri, err := GenerateTOTPSecret("user@example.com", "Vane")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() returned unexpected error: %v", err)
	}
	if secret == "" {
		t.Error("GenerateTOTPSecret() secret is empty, want a non-empty base32 secret")
	}
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Errorf("GenerateTOTPSecret() uri = %q, want it to start with otpauth://totp/", uri)
	}
	if !strings.Contains(uri, "user%40example.com") && !strings.Contains(uri, "user@example.com") {
		t.Errorf("GenerateTOTPSecret() uri = %q, want it to contain the account name", uri)
	}
}

func TestValidateTOTPCode_CorrectCode_ReturnsTrue(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("user@example.com", "Vane")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() returned unexpected error: %v", err)
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode() returned unexpected error: %v", err)
	}

	if !ValidateTOTPCode(secret, code) {
		t.Error("ValidateTOTPCode() = false for a correctly computed code, want true")
	}
}

func TestValidateTOTPCode_WrongCode_ReturnsFalse(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("user@example.com", "Vane")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() returned unexpected error: %v", err)
	}

	if ValidateTOTPCode(secret, "000000") {
		t.Error("ValidateTOTPCode() = true for an arbitrary wrong code, want false (astronomically unlikely to collide)")
	}
}

func TestValidateTOTPCode_OneStepSkew_ReturnsTrue(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("user@example.com", "Vane")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() returned unexpected error: %v", err)
	}

	// One period (30s) in the past - within the library's default ±1 skew
	// tolerance (spec.md Edge Cases: standard ±1 time-step window).
	pastCode, err := totp.GenerateCode(secret, time.Now().Add(-30*time.Second))
	if err != nil {
		t.Fatalf("totp.GenerateCode() returned unexpected error: %v", err)
	}

	if !ValidateTOTPCode(secret, pastCode) {
		t.Error("ValidateTOTPCode() = false for a code one time-step in the past, want true (default ±1 skew)")
	}
}

func TestValidateTOTPCode_TwoStepSkew_ReturnsFalse(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("user@example.com", "Vane")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() returned unexpected error: %v", err)
	}

	// Two periods (60s) in the past - outside the default ±1 skew window.
	farPastCode, err := totp.GenerateCode(secret, time.Now().Add(-90*time.Second))
	if err != nil {
		t.Fatalf("totp.GenerateCode() returned unexpected error: %v", err)
	}

	if ValidateTOTPCode(secret, farPastCode) {
		t.Error("ValidateTOTPCode() = true for a code beyond the ±1 skew window, want false")
	}
}

func TestIssueVerifyTwoFactorChallenge_RoundTrip_ReturnsUserIDAndJTI(t *testing.T) {
	token, err := IssueTwoFactorChallenge("user-123", "jti-abc", testSecret)
	if err != nil {
		t.Fatalf("IssueTwoFactorChallenge() returned unexpected error: %v", err)
	}

	userID, jti, err := VerifyTwoFactorChallenge(token, testSecret)
	if err != nil {
		t.Fatalf("VerifyTwoFactorChallenge() returned unexpected error: %v", err)
	}
	if userID != "user-123" {
		t.Errorf("VerifyTwoFactorChallenge() userID = %q, want %q", userID, "user-123")
	}
	if jti != "jti-abc" {
		t.Errorf("VerifyTwoFactorChallenge() jti = %q, want %q", jti, "jti-abc")
	}
}

func TestVerifyTwoFactorChallenge_Expired_ErrInvalidToken(t *testing.T) {
	token, err := issueTwoFactorChallengeWithTTL("user-123", "jti-abc", testSecret, -1*time.Minute)
	if err != nil {
		t.Fatalf("issueTwoFactorChallengeWithTTL() returned unexpected error: %v", err)
	}

	_, _, err = VerifyTwoFactorChallenge(token, testSecret)
	if err != ErrInvalidToken {
		t.Errorf("VerifyTwoFactorChallenge() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyTwoFactorChallenge_Tampered_ErrInvalidToken(t *testing.T) {
	token, err := IssueTwoFactorChallenge("user-123", "jti-abc", testSecret)
	if err != nil {
		t.Fatalf("IssueTwoFactorChallenge() returned unexpected error: %v", err)
	}

	_, _, err = VerifyTwoFactorChallenge(token, "a-completely-different-secret-value")
	if err != ErrInvalidToken {
		t.Errorf("VerifyTwoFactorChallenge() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyTwoFactorChallenge_Malformed_ErrInvalidToken(t *testing.T) {
	_, _, err := VerifyTwoFactorChallenge("not-a-jwt-at-all", testSecret)
	if err != ErrInvalidToken {
		t.Errorf("VerifyTwoFactorChallenge() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyTwoFactorChallenge_MissingAudience_ErrInvalidToken(t *testing.T) {
	// A session token (no Audience claim ever set) must never verify as a
	// 2FA challenge, mirroring the reverse guarantee T3 adds to
	// VerifySessionClaims.
	sessionToken, err := IssueSession("user-123", testSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	_, _, err = VerifyTwoFactorChallenge(sessionToken, testSecret)
	if err != ErrInvalidToken {
		t.Errorf("VerifyTwoFactorChallenge() error = %v, want ErrInvalidToken", err)
	}
}
