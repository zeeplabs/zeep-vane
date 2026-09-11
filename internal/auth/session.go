package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionTTL is how long an issued session token stays valid. The spec does
// not define an explicit session lifetime, so this is a reasonable default,
// not a requirement derived from an AC. Exported so callers that set a
// cookie alongside the token (see api.AuthHandler.Login) can match its
// MaxAge to the token's actual expiry.
const SessionTTL = 24 * time.Hour

// IssueTestSessionID is a fixed sentinel sid (valid UUID) that integration
// tests pass when they want to exercise RequireAuth's JWT-parsing path
// without threading a real sessions-table row through every token
// issuance. RequireAuth's session lookup checks row existence and
// revoked_at only - it does NOT verify the session row's user_id against
// the JWT's sub claim - so a single fixture row with this sid (inserted
// by the api test-setup helper) is enough for every test that issues
// a token via IssueSessionWithTenant(... IssueTestSessionID, ...).
//
// Production code must NEVER use this constant - it uses real sids from
// db.SessionRepository.Create, which generates fresh UUIDs and persists
// them server-side per user-sessions spec.
const IssueTestSessionID = "00000000-0000-0000-0000-000000000001"

// IssueTestSessionUserID is the user_id the fixture sessions row is
// inserted with - any value works because the middleware doesn't compare
// session.user_id to JWT.sub, but a real user row must exist for the FK
// constraint. The api package's test-setup helper uses this to seed a
// shared fixture row that backs every token issued with IssueTestSessionID.
const IssueTestSessionUserID = "00000000-0000-0000-0000-000000000002"

// ErrInvalidToken is returned when a token fails to parse, fails signature
// verification, is expired, or is missing the required sid claim (see
// VerifySessionClaims).
var ErrInvalidToken = errors.New("auth: invalid or expired token")

// ErrMissingSessionID is returned by issueSessionWithTTL when a caller
// asks to issue a token without a sessions-table row id - the entire
// per-device revocation machinery (user-sessions spec) depends on every
// issued token carrying a sid claim, so the issuer refuses the empty
// case rather than producing a token that VerifySessionClaims will
// immediately reject.
var ErrMissingSessionID = errors.New("auth: session token requires non-empty sid")

// sessionClaims is the JWT payload for a session. SessionID is the
// sessions-table row id this token maps to - a session token that
// doesn't identify a row can't be individually revoked (user-sessions
// spec SESS-01/03). TenantID is the session's active tenant (multi-
// tenancy-core, AD-022) - empty for a session with no tenant selected
// yet, never decoded client-side (AD-004): the tenant-context middleware
// reads it server-side only, to set app.tenant_id for RLS.
type sessionClaims struct {
	jwt.RegisteredClaims
	TenantID  string `json:"tid,omitempty"`
	SessionID string `json:"sid"`
}

// IssueSession signs a session token for adminID with no active tenant,
// using secret, valid for SessionTTL. sessionID is the row id from the
// sessions table (created by SessionRepository.Create) this token maps
// to - non-empty, otherwise ErrMissingSessionID is returned.
func IssueSession(adminID, sessionID, secret string) (string, error) {
	return IssueSessionWithTenant(adminID, "", sessionID, secret)
}

// IssueSessionWithTenant signs a session token for adminID with tenantID as
// its active tenant (tenantID may be "" - no tenant selected yet) and
// sessionID as the corresponding sessions-table row id, using secret,
// valid for SessionTTL. ErrMissingSessionID is returned if sessionID is
// empty.
func IssueSessionWithTenant(adminID, tenantID, sessionID, secret string) (string, error) {
	return issueSessionWithTTL(adminID, tenantID, sessionID, secret, SessionTTL)
}

// issueSessionWithTTL is IssueSessionWithTenant with an explicit TTL, so
// tests can produce an already-expired token without waiting or mutating
// package state.
func issueSessionWithTTL(adminID, tenantID, sessionID, secret string, ttl time.Duration) (string, error) {
	if sessionID == "" {
		return "", ErrMissingSessionID
	}
	now := time.Now()
	claims := sessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   adminID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		TenantID:  tenantID,
		SessionID: sessionID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("auth: failed to sign session token: %w", err)
	}

	return signed, nil
}

// VerifySession validates tokenString against secret and returns the admin
// ID it was issued for. It returns ErrInvalidToken for any malformed,
// unsigned, tampered, or expired token, or one missing the required sid
// claim.
func VerifySession(tokenString, secret string) (string, error) {
	claims, err := VerifySessionClaims(tokenString, secret)
	if err != nil {
		return "", err
	}
	return claims.AdminID, nil
}

// SessionClaims is the subset of a verified session token's payload callers
// beyond the admin ID itself may need. IssuedAt backs the admin-dashboard
// feature's session revocation check (RequireAuth rejects a token issued
// before the admin's sessions were revoked). TenantID is the session's
// active tenant, "" if none selected yet. SessionID is the row id in
// the sessions table this token maps to (user-sessions), looked up by
// RequireAuth to verify the row still exists and is not revoked.
type SessionClaims struct {
	AdminID   string
	IssuedAt  time.Time
	TenantID  string
	SessionID string
}

// VerifySessionClaims validates tokenString against secret like
// VerifySession, additionally returning the token's IssuedAt, TenantID,
// and SessionID claims. It returns ErrInvalidToken for any malformed,
// unsigned, tampered, or expired token, or one missing a required
// claim.
func VerifySessionClaims(tokenString, secret string) (SessionClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &sessionClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return SessionClaims{}, ErrInvalidToken
	}

	claims, ok := token.Claims.(*sessionClaims)
	if !ok || claims.Subject == "" || claims.IssuedAt == nil {
		return SessionClaims{}, ErrInvalidToken
	}
	// A real session token never sets Audience. A 2FA challenge token
	// (internal/auth/two_factor.go) always does, so this rejects any
	// challenge token from ever being accepted as a session, even if a
	// future refactor changed how challenge tokens are parsed
	// (auth-2fa-totp design.md).
	if len(claims.Audience) > 0 {
		return SessionClaims{}, ErrInvalidToken
	}
	// user-sessions: every session token must carry a sid claim
	// identifying the sessions-table row it maps to. Tokens issued
	// before this feature shipped (no sid) cannot be individually
	// revoked, so reject them rather than treat them as sessions
	// whose row simply doesn't exist - the latter would be
	// indistinguishable from a logout in progress to RequireAuth's
	// lookup and would let a leaked token survive an "all sessions
	// revoked" attempt.
	if claims.SessionID == "" {
		return SessionClaims{}, ErrInvalidToken
	}

	return SessionClaims{
		AdminID:   claims.Subject,
		IssuedAt:  claims.IssuedAt.Time,
		TenantID:  claims.TenantID,
		SessionID: claims.SessionID,
	}, nil
}
