// Package auth provides password hashing and session primitives for admin
// authentication.
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLength is the shortest password ValidatePassword accepts.
// NIST SP 800-63B recommends a minimum length over forced complexity rules
// (uppercase/digit/symbol requirements push users toward predictable
// substitutions without meaningfully raising entropy) - 8 characters is its
// baseline minimum for user-chosen passwords (H11).
const MinPasswordLength = 8

// MaxPasswordLength matches bcrypt's own hard limit: GenerateFromPassword
// errors on any input over 72 bytes. Rejecting it here, before hashing,
// gives a clear validation error instead of surfacing bcrypt's own error as
// an unrelated 500.
const MaxPasswordLength = 72

// ValidatePassword rejects a password that's too short or too long to
// safely hash (H11) - bootstrap, invite-accept, and password-reset-confirm
// all set a new password and must all call this before HashPassword.
func ValidatePassword(plain string) error {
	if len(plain) < MinPasswordLength {
		return fmt.Errorf("auth: password must be at least %d characters", MinPasswordLength)
	}
	if len(plain) > MaxPasswordLength {
		return fmt.Errorf("auth: password must be at most %d characters", MaxPasswordLength)
	}
	return nil
}

// HashPassword returns a bcrypt hash of plain, never the plain text itself.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: failed to hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword reports whether plain matches hash.
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
