package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config es la configuración por entorno. No contiene secretos.
type Config struct {
	Addr            string
	DBPath          string
	LogLevel        slog.Level
	MaxBodyBytes    int64
	ShutdownTimeout time.Duration
	CORSOrigins     []string
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func parseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("AGENTBANK_LOG_LEVEL inválido: %q", value)
	}
}

func Load() (Config, error) {
	cfg := Config{
		Addr:            getenv("AGENTBANK_ADDR", "127.0.0.1:8090"),
		DBPath:          getenv("AGENTBANK_DB", "data/agentbank.db"),
		MaxBodyBytes:    1 << 20,
		ShutdownTimeout: 10 * time.Second,
	}
	level, err := parseLevel(getenv("AGENTBANK_LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = level

	if raw := strings.TrimSpace(os.Getenv("AGENTBANK_MAX_BODY_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("AGENTBANK_MAX_BODY_BYTES inválido: %q", raw)
		}
		cfg.MaxBodyBytes = value
	}
	if raw := strings.TrimSpace(os.Getenv("AGENTBANK_SHUTDOWN_TIMEOUT")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("AGENTBANK_SHUTDOWN_TIMEOUT inválido: %q", raw)
		}
		cfg.ShutdownTimeout = value
	}
	for _, origin := range strings.Split(os.Getenv("AGENTBANK_CORS_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			cfg.CORSOrigins = append(cfg.CORSOrigins, origin)
		}
	}
	return cfg, nil
}
