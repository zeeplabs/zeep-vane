package api

import "fmt"

// unconfiguredAdminBaseURLHost is the scheme+host used to build an
// admin-facing email link (password-reset, admin-invite) when the operator
// hasn't set VANE_ADMIN_BASE_URL. Deliberately never falls back to the
// incoming request's Host header - see cfg.AdminBaseURL's doc comment for
// why (host-header injection into an unauthenticated endpoint's emailed
// link is an account-takeover primitive, not a cosmetic bug). A visibly
// broken placeholder link fails loudly (the recipient can't click through,
// and it's obviously wrong in a support screenshot) instead of failing
// open onto whatever host an attacker supplied.
const unconfiguredAdminBaseURLPlaceholder = "http://vane-admin-base-url-not-configured.invalid"

// adminBaseURL returns adminBaseURL if the operator configured one, or the
// placeholder above otherwise. adminBaseURL is expected to already have any
// trailing slash trimmed (config.Load does this).
func adminBaseURL(configured string) string {
	if configured == "" {
		return unconfiguredAdminBaseURLPlaceholder
	}
	return configured
}

// nilIfEmpty normalizes an optional string field (e.g. an admin's phone
// number) so an empty request value is stored as SQL NULL rather than an
// empty string - "never given" and "given, blank" would otherwise be
// indistinguishable in the admins/admin_invites tables.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// maxPhoneLength bounds an admin's phone field. The frontend's PhoneField
// emits values like "+55 (11) 98765-4321" (dial code + masked local
// number) for any of the ~200 countries in @zeeptech/toolkit's list - 32
// characters comfortably covers the longest of those, since this is a
// contact field, not a number Vane ever dials itself.
const maxPhoneLength = 32

// ValidatePhone rejects a phone value the frontend could never have
// produced - not an authoritative "is this a real number" check (Vane
// doesn't send SMS/calls, so that's not this field's job), a defense
// against a client that skips PhoneField entirely and posts arbitrary
// JSON straight to the API, where phone otherwise has no server-side
// constraint. An empty string (phone is optional) always passes.
func ValidatePhone(phone string) error {
	if phone == "" {
		return nil
	}
	if len(phone) > maxPhoneLength {
		return fmt.Errorf("phone must be at most %d characters", maxPhoneLength)
	}
	hasDigit := false
	for _, r := range phone {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')':
			// allowed formatting characters
		default:
			return fmt.Errorf("phone contains invalid characters")
		}
	}
	if !hasDigit {
		return fmt.Errorf("phone must contain at least one digit")
	}
	return nil
}
