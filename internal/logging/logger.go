// Package logging builds vane's structured (JSON) application logger.
package logging

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New builds a JSON-structured zap.Logger at the given level
// ("debug"/"info"/"warn"/"error"). An unrecognized level is rejected.
func New(level string) (*zap.Logger, error) {
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("logging: invalid log level %q: %w", level, err)
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapLevel)

	logger, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("logging: failed to build logger: %w", err)
	}

	return logger, nil
}
