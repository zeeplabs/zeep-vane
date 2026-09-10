package auth

import (
	"testing"
	"time"
)

const testSecret = "test-secret-at-least-32-bytes-long!!"

func TestVerifySession_ValidToken_ReturnsAdminID(t *testing.T) {
	const adminID = "admin-123"

	token, err := IssueSession(adminID, testSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	got, err := VerifySession(token, testSecret)
	if err != nil {
		t.Fatalf("VerifySession() returned unexpected error: %v", err)
	}
	if got != adminID {
		t.Errorf("VerifySession() = %q, want %q", got, adminID)
	}
}

func TestVerifySession_ExpiredToken_ErrInvalidToken(t *testing.T) {
	token, err := issueSessionWithTTL("admin-123", "", testSecret, -1*time.Hour)
	if err != nil {
		t.Fatalf("issueSessionWithTTL() returned unexpected error: %v", err)
	}

	_, err = VerifySession(token, testSecret)
	if err != ErrInvalidToken {
		t.Errorf("VerifySession() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifySession_WrongSecret_ErrInvalidToken(t *testing.T) {
	token, err := IssueSession("admin-123", testSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	_, err = VerifySession(token, "a-completely-different-secret-value")
	if err != ErrInvalidToken {
		t.Errorf("VerifySession() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifySession_MalformedToken_ErrInvalidToken(t *testing.T) {
	_, err := VerifySession("not-a-jwt-at-all", testSecret)
	if err != ErrInvalidToken {
		t.Errorf("VerifySession() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifySessionClaims_AudienceClaimSet_Rejected(t *testing.T) {
	// A 2FA challenge token always sets Audience - VerifySessionClaims must
	// reject it as a session even though it is otherwise well-formed and
	// signed with the correct secret (auth-2fa-totp TOTP-05).
	token, err := IssueTwoFactorChallenge("admin-123", "jti-abc", testSecret)
	if err != nil {
		t.Fatalf("IssueTwoFactorChallenge() returned unexpected error: %v", err)
	}

	_, err = VerifySessionClaims(token, testSecret)
	if err != ErrInvalidToken {
		t.Errorf("VerifySessionClaims() error = %v, want ErrInvalidToken", err)
	}
}
