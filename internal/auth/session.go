package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// sessionTTL is how long an issued session token stays valid. The spec does
// not define an explicit session lifetime, so this is a reasonable default,
// not a requirement derived from an AC.
const sessionTTL = 24 * time.Hour

// ErrInvalidToken is returned when a token fails to parse, fails signature
// verification, or is expired.
var ErrInvalidToken = errors.New("auth: invalid or expired token")

// sessionClaims is the JWT payload for an admin session.
type sessionClaims struct {
	jwt.RegisteredClaims
}

// IssueSession signs a session token for adminID using secret, valid for
// sessionTTL.
func IssueSession(adminID, secret string) (string, error) {
	return issueSessionWithTTL(adminID, secret, sessionTTL)
}

// issueSessionWithTTL is IssueSession with an explicit TTL, so tests can
// produce an already-expired token without waiting or mutating package
// state.
func issueSessionWithTTL(adminID, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := sessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   adminID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
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
	token, err := jwt.ParseWithClaims(tokenString, &sessionClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return "", ErrInvalidToken
	}

	claims, ok := token.Claims.(*sessionClaims)
	if !ok || claims.Subject == "" {
		return "", ErrInvalidToken
	}

	return claims.Subject, nil
}
