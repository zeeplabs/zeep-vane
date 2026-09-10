package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp/totp"
)

// twoFactorChallengeAudience marks a token as a pre-2FA challenge, never a
// real session - VerifySessionClaims rejects any token carrying a non-empty
// Audience claim, so a challenge token can never be replayed as a session
// (auth-2fa-totp design.md).
const twoFactorChallengeAudience = "2fa_challenge"

// TwoFactorChallengeTTL is how long an issued 2FA challenge token stays
// valid (spec.md Assumptions: 5 minutes). Exported so a caller backing the
// token with a server-side row (internal/db.TwoFactorChallengeRepository.Create)
// can expire that row on the exact same schedule as the signed token itself.
const TwoFactorChallengeTTL = 5 * time.Minute

// GenerateTOTPSecret generates a new random TOTP secret for accountEmail,
// under issuer, returning the raw base32 secret (for manual entry) and the
// otpauth:// URI (for QR-code display). Uses the library's defaults: 30s
// period, 6 digits, SHA1, a 20-byte random secret.
func GenerateTOTPSecret(accountEmail, issuer string) (secret string, otpauthURI string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountEmail,
	})
	if err != nil {
		return "", "", fmt.Errorf("auth: failed to generate TOTP secret: %w", err)
	}

	return key.Secret(), key.String(), nil
}

// ValidateTOTPCode reports whether code is a valid TOTP code for secret at
// the current time, allowing the library's default ±1 time-step skew
// (totp.Validate's default) to tolerate minor clock drift between server
// and authenticator app (spec.md Edge Cases).
func ValidateTOTPCode(secret, code string) bool {
	return totp.Validate(code, secret)
}

// twoFactorChallengeClaims is the JWT payload for a 2FA challenge token -
// distinct from sessionClaims by always setting a non-empty Audience, which
// is what VerifySessionClaims checks to reject it as a session.
type twoFactorChallengeClaims struct {
	jwt.RegisteredClaims
}

// IssueTwoFactorChallenge signs a short-lived (5 minute) challenge token for
// userID, identified by jti (the caller's TwoFactorChallengeRepository row
// ID), using secret. The token carries Audience: ["2fa_challenge"], so
// VerifySessionClaims will always reject it as a session token.
func IssueTwoFactorChallenge(userID, jti, secret string) (string, error) {
	return issueTwoFactorChallengeWithTTL(userID, jti, secret, TwoFactorChallengeTTL)
}

// issueTwoFactorChallengeWithTTL is IssueTwoFactorChallenge with an explicit
// TTL, so tests can produce an already-expired challenge token without
// waiting or mutating package state.
func issueTwoFactorChallengeWithTTL(userID, jti, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := twoFactorChallengeClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ID:        jti,
			Audience:  jwt.ClaimStrings{twoFactorChallengeAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("auth: failed to sign 2FA challenge token: %w", err)
	}

	return signed, nil
}

// VerifyTwoFactorChallenge validates tokenString against secret and returns
// the userID (Subject) and jti (ID) it was issued for. It returns
// ErrInvalidToken for any malformed, unsigned, tampered, or expired token,
// or one not carrying the expected 2FA-challenge Audience claim.
func VerifyTwoFactorChallenge(tokenString, secret string) (userID, jti string, err error) {
	token, err := jwt.ParseWithClaims(tokenString, &twoFactorChallengeClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return "", "", ErrInvalidToken
	}

	claims, ok := token.Claims.(*twoFactorChallengeClaims)
	if !ok || claims.Subject == "" || claims.ID == "" {
		return "", "", ErrInvalidToken
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != twoFactorChallengeAudience {
		return "", "", ErrInvalidToken
	}

	return claims.Subject, claims.ID, nil
}
