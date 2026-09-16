package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-at-least-32-bytes-long!!"

const testSessionID = "test-session-id-abc-123"

func TestVerifySession_ValidToken_ReturnsAdminID(t *testing.T) {
	const adminID = "admin-123"

	token, err := IssueSession(adminID, testSessionID, testSecret)
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
	token, err := issueSessionWithTTL("admin-123", "", testSessionID, testSecret, -1*time.Hour)
	if err != nil {
		t.Fatalf("issueSessionWithTTL() returned unexpected error: %v", err)
	}

	_, err = VerifySession(token, testSecret)
	if err != ErrInvalidToken {
		t.Errorf("VerifySession() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifySession_WrongSecret_ErrInvalidToken(t *testing.T) {
	token, err := IssueSession("admin-123", testSessionID, testSecret)
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

// TestIssueSessionWithTenant_EmitsSIDClaim verifies the new sid claim
// (user-sessions spec SESS-01): every issued session token carries the
// session row id it was issued for, so RequireAuth can later look up
// the row by that claim to enforce per-session revocation.
func TestIssueSessionWithTenant_EmitsSIDClaim(t *testing.T) {
	const adminID = "admin-123"
	const sid = "11111111-2222-3333-4444-555555555555"

	token, err := IssueSessionWithTenant(adminID, "tenant-a", sid, testSecret)
	if err != nil {
		t.Fatalf("IssueSessionWithTenant() returned unexpected error: %v", err)
	}

	parsed, err := jwt.ParseWithClaims(token, &sessionClaims{}, func(tok *jwt.Token) (interface{}, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			t.Fatalf("unexpected signing method %v", tok.Header["alg"])
		}
		return []byte(testSecret), nil
	})
	if err != nil {
		t.Fatalf("jwt.ParseWithClaims() returned unexpected error: %v", err)
	}
	claims, ok := parsed.Claims.(*sessionClaims)
	if !ok {
		t.Fatalf("token claims are not *sessionClaims, got %T", parsed.Claims)
	}
	if claims.SessionID != sid {
		t.Errorf("issued sid claim = %q, want %q", claims.SessionID, sid)
	}
	if claims.Subject != adminID {
		t.Errorf("issued subject = %q, want %q", claims.Subject, adminID)
	}
	if claims.TenantID != "tenant-a" {
		t.Errorf("issued tid claim = %q, want %q", claims.TenantID, "tenant-a")
	}
}

// TestVerifySessionClaims_ReturnsSessionID proves the parsed SessionID
// round-trips back to the caller via SessionClaims.SessionID - the
// shape RequireAuth reads to look up the sessions-table row.
func TestVerifySessionClaims_ReturnsSessionID(t *testing.T) {
	const sid = "99999999-aaaa-bbbb-cccc-dddddddddddd"

	token, err := IssueSession("admin-123", sid, testSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	got, err := VerifySessionClaims(token, testSecret)
	if err != nil {
		t.Fatalf("VerifySessionClaims() returned unexpected error: %v", err)
	}
	if got.SessionID != sid {
		t.Errorf("SessionClaims.SessionID = %q, want %q", got.SessionID, sid)
	}
}

// TestVerifySessionClaims_MissingSID_ErrInvalidToken is the
// pre-deploy-defence acceptance test (user-sessions SESS-03): a token
// without a sid claim cannot be accepted as a session. We hand-craft
// the token via jwt.NewWithClaims (bypassing IssueSessionWithTenant)
// so the test exercises the parser path, not the issuer guard.
func TestVerifySessionClaims_MissingSID_ErrInvalidToken(t *testing.T) {
	now := time.Now()
	tokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, &sessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "admin-123",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(SessionTTL)),
		},
		TenantID:  "tenant-a",
		SessionID: "", // no sid - the case under test
	})
	signed, err := tokenObj.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("manual SignedString() returned unexpected error: %v", err)
	}

	_, err = VerifySessionClaims(signed, testSecret)
	if err != ErrInvalidToken {
		t.Errorf("VerifySessionClaims() error = %v, want ErrInvalidToken", err)
	}
}

// TestIssueSession_EmptySID_ErrMissingSessionID proves the issuer
// refuses to produce a token without a sid - catches the caller bug
// at issue time rather than waiting for VerifySessionClaims to
// reject the resulting token at request time.
func TestIssueSession_EmptySID_ErrMissingSessionID(t *testing.T) {
	if _, err := IssueSession("admin-123", "", testSecret); err != ErrMissingSessionID {
		t.Errorf("IssueSession(empty sid) error = %v, want ErrMissingSessionID", err)
	}
	if _, err := IssueSessionWithTenant("admin-123", "tenant-a", "", testSecret); err != ErrMissingSessionID {
		t.Errorf("IssueSessionWithTenant(empty sid) error = %v, want ErrMissingSessionID", err)
	}
}

// TestVerifySessionClaims_2FAChallengeStillRejected is the regression
// guard for the audience check (auth-2fa-totp design.md): adding the
// sid-missing rejection must not weaken the audience check that keeps
// challenge tokens from ever being accepted as session tokens.
func TestVerifySessionClaims_2FAChallengeStillRejected(t *testing.T) {
	token, err := IssueTwoFactorChallenge("admin-123", "jti-xyz", testSecret)
	if err != nil {
		t.Fatalf("IssueTwoFactorChallenge() returned unexpected error: %v", err)
	}

	_, err = VerifySessionClaims(token, testSecret)
	if err != ErrInvalidToken {
		t.Errorf("VerifySessionClaims() on challenge token: err = %v, want ErrInvalidToken", err)
	}
}
