package api

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
