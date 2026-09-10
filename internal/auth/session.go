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

// ErrInvalidToken is returned when a token fails to parse, fails signature
// verification, or is expired.
var ErrInvalidToken = errors.New("auth: invalid or expired token")

// sessionClaims is the JWT payload for an admin session. TenantID is the
// session's active tenant (multi-tenancy-core, AD-022) - empty for a
// session with no tenant selected yet (e.g. an account with more than one
// tenant_membership, pending the tenant-selection screen), never decoded
// client-side (AD-004): the tenant-context middleware reads it server-side
// only, to set app.tenant_id for RLS.
type sessionClaims struct {
	jwt.RegisteredClaims
	TenantID string `json:"tid,omitempty"`
}

// IssueSession signs a session token for adminID with no active tenant,
// using secret, valid for SessionTTL. Equivalent to
// IssueSessionWithTenant(adminID, "", secret) - kept as the original,
// tenant-agnostic entry point so existing callers that predate
// multi-tenancy-core don't need to change.
func IssueSession(adminID, secret string) (string, error) {
	return IssueSessionWithTenant(adminID, "", secret)
}

// IssueSessionWithTenant signs a session token for adminID with tenantID as
// its active tenant (tenantID may be "" - no tenant selected yet), using
// secret, valid for SessionTTL.
func IssueSessionWithTenant(adminID, tenantID, secret string) (string, error) {
	return issueSessionWithTTL(adminID, tenantID, secret, SessionTTL)
}

// issueSessionWithTTL is IssueSessionWithTenant with an explicit TTL, so
// tests can produce an already-expired token without waiting or mutating
// package state.
func issueSessionWithTTL(adminID, tenantID, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := sessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   adminID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		TenantID: tenantID,
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
// unsigned, tampered, or expired token.
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
// active tenant, "" if none selected yet.
type SessionClaims struct {
	AdminID  string
	IssuedAt time.Time
	TenantID string
}

// VerifySessionClaims validates tokenString against secret like
// VerifySession, additionally returning the token's IssuedAt claim. It
// returns ErrInvalidToken for any malformed, unsigned, tampered, or expired
// token, or one missing an IssuedAt claim.
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

	return SessionClaims{AdminID: claims.Subject, IssuedAt: claims.IssuedAt.Time, TenantID: claims.TenantID}, nil
}
