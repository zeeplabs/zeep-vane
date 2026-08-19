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
	Port                int
	PollIntervalSeconds int
}

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

	port, err := requireInt("PORT")
	if err != nil {
		return Config{}, err
	}

	pollIntervalSeconds, err := requireInt("POLL_INTERVAL_SECONDS")
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:         databaseURL,
		MasterKey:           masterKey,
		Port:                port,
		PollIntervalSeconds: pollIntervalSeconds,
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
