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
}

// defaultCORSAllowedOrigin is the Vite dev server's origin - the CORS
// allowlist entry when CORS_ALLOWED_ORIGIN is unset (local development).
const defaultCORSAllowedOrigin = "http://localhost:5173"

// Load reads and validates the required environment variables, returning a
// clear error if any is missing or malformed. A .env file is loaded on a
// best-effort basis; its absence is not an error.
func Load() (Config, error) {
	_ = godotenv.Load()

	databaseURL, err := requireString("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}

	masterKey, err := requireString("VANE_MASTER_KEY")
	if err != nil {
		return Config{}, err
	}

	sessionSecret, err := requireString("VANE_SESSION_SECRET")
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

	return Config{
		DatabaseURL:         databaseURL,
		MasterKey:           masterKey,
		SessionSecret:       sessionSecret,
		Port:                port,
		PollIntervalSeconds: pollIntervalSeconds,
		LogLevel:            logLevel,
		CORSAllowedOrigin:   corsAllowedOrigin,
	}, nil
}

func requireString(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("config: required environment variable %s is not set", name)
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
