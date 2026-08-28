// Package config loads and validates vane's runtime configuration from
// environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all environment-derived settings vane needs to run.
type Config struct {
	DatabaseURL         string
	MasterKey           string
	SessionSecret       string
	Port                int
	PollIntervalSeconds int
	LogLevel            string
	CORSAllowedOrigin   string
	PublicDNSTarget     string
	DevTokenLogging     bool
	HTTPSEnabled        bool
	SecureCookies       bool
}

// defaultCORSAllowedOrigin is the Vite dev server's origin - the CORS
// allowlist entry when CORS_ALLOWED_ORIGIN is unset (local development).
const defaultCORSAllowedOrigin = "http://localhost:5173"

// PublicDNSTarget has no default: an unset PUBLIC_DNS_TARGET means the
// operator hasn't configured the value this server's admins should point
// their DNS records at. Config.PublicDNSTarget stays "" in that case - the
// attach-domain screen (SPD-10) surfaces this as "not configured" rather
// than blocking anything, since self-hosted infra is arbitrary and the app
// cannot discover its own public hostname reliably.

// Load reads and validates the required environment variables, returning a
// clear error if any is missing or malformed. A .env file is loaded on a
// best-effort basis; its absence is not an error.
func Load() (Config, error) {
	_ = godotenv.Load()

	databaseURL, err := requireString("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}

	masterKey, err := requireSecret("VANE_MASTER_KEY")
	if err != nil {
		return Config{}, err
	}

	sessionSecret, err := requireSecret("VANE_SESSION_SECRET")
	if err != nil {
		return Config{}, err
	}

	port, err := requireInt("PORT")
	if err != nil {
		return Config{}, err
	}

	pollIntervalSeconds, err := requireInt("POLL_INTERVAL_SECONDS")
	if err != nil {
		return Config{}, err
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	corsAllowedOrigin := os.Getenv("CORS_ALLOWED_ORIGIN")
	if corsAllowedOrigin == "" {
		corsAllowedOrigin = defaultCORSAllowedOrigin
	}

	publicDNSTarget := os.Getenv("PUBLIC_DNS_TARGET")

	// devTokenLogging gates logging the raw password-reset/admin-invite
	// token, which stands in for real email delivery (out of scope for the
	// MVP - see internal/api/password_reset_handler.go and admins.go).
	// Defaults to false: without it, an admin has no way to retrieve those
	// tokens, but the alternative default - writing a bearer token capable
	// of an owner takeover to whatever log sink LOG_LEVEL=info reaches -
	// is worse for a self-hosted deploy whose operator hasn't set up email.
	devTokenLogging := os.Getenv("VANE_DEV_TOKEN_LOGGING") == "true"

	// httpsEnabled defaults to true (unchanged behavior: the on-demand-TLS
	// listener for custom status-page domains always starts). An operator
	// with no custom domain to serve - or whose environment can't bind :443
	// (unprivileged container, port already taken by a reverse proxy) -
	// sets VANE_HTTPS_ENABLED=false so that failure doesn't take the admin
	// HTTP listener down with it (H8).
	httpsEnabled := os.Getenv("VANE_HTTPS_ENABLED") != "false"

	// secureCookies defaults to true (unchanged behavior: vane_session is
	// Secure-only). A browser only sends a Secure cookie back over HTTPS -
	// with one exception it treats as a secure context anyway: the literal
	// host "localhost". Any other HTTP-only deployment (an internal IP or
	// hostname, common for a self-hosted instance without a reverse-proxy
	// TLS terminator) gets a 200 on login and a silent 401 on every request
	// after, because the browser never sends the cookie back (H9). An
	// operator in that situation sets VANE_SECURE_COOKIES=false - understanding
	// the session token then travels in the clear on their own network.
	secureCookies := os.Getenv("VANE_SECURE_COOKIES") != "false"

	return Config{
		DatabaseURL:         databaseURL,
		MasterKey:           masterKey,
		SessionSecret:       sessionSecret,
		Port:                port,
		PollIntervalSeconds: pollIntervalSeconds,
		LogLevel:            logLevel,
		CORSAllowedOrigin:   corsAllowedOrigin,
		PublicDNSTarget:     publicDNSTarget,
		DevTokenLogging:     devTokenLogging,
		HTTPSEnabled:        httpsEnabled,
		SecureCookies:       secureCookies,
	}, nil
}

func requireString(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("config: required environment variable %s is not set", name)
	}
	return value, nil
}

// minSecretLength is the shortest value accepted for a secret-bearing
// variable (VANE_MASTER_KEY, VANE_SESSION_SECRET). 32 bytes matches the
// key size secretbox.go derives via SHA-256 and gives the session-signing
// secret comparable entropy.
const minSecretLength = 32

// knownPlaceholderSecrets are values that have shipped in this repo's own
// docker-compose.yml/README as example secrets. A deploy that never
// overrides them would sign sessions and encrypt integration credentials
// with a key anyone can read on GitHub, so Load rejects them outright
// instead of only checking for non-empty.
var knownPlaceholderSecrets = map[string]bool{
	"change-me-master-key":     true,
	"change-me-session-secret": true,
}

// requireSecret behaves like requireString but additionally rejects
// values that are too short to be a real key or that match a known
// placeholder shipped in this repo's own example configs.
func requireSecret(name string) (string, error) {
	value, err := requireString(name)
	if err != nil {
		return "", err
	}
	if len(value) < minSecretLength {
		return "", fmt.Errorf("config: environment variable %s must be at least %d characters, got %d", name, minSecretLength, len(value))
	}
	if knownPlaceholderSecrets[value] {
		return "", fmt.Errorf("config: environment variable %s is still set to the example placeholder value - generate a real secret before starting", name)
	}
	return value, nil
}

func requireInt(name string) (int, error) {
	raw, err := requireString(name)
	if err != nil {
		return 0, err
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: environment variable %s must be an integer, got %q", name, raw)
	}

	return value, nil
}
