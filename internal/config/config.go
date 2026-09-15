// Package config loads harvest-service configuration from environment
// variables, with sane defaults for local development.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all runtime configuration for the service.
type Config struct {
	Env string // "development" or "production"

	HTTPPort            string
	HTTPReadTimeout     time.Duration
	HTTPWriteTimeout    time.Duration
	HTTPIdleTimeout     time.Duration
	HTTPShutdownTimeout time.Duration

	DatabaseURL            string
	DatabaseConnectTimeout time.Duration

	// RedisAddr is the shared session store every BeeBase service checks on
	// every request, so an access token can be rejected the instant its
	// session is superseded by a newer one instead of staying valid until
	// its own JWT expiry.
	RedisAddr           string
	RedisConnectTimeout time.Duration

	LogLevel string // "debug", "info", "warn", "error"

	// AuthJWKSURL points at auth-service's public key endpoint
	// (GET /.well-known/jwks.json), used to verify access tokens without
	// ever holding a key that could mint one.
	AuthJWKSURL          string
	InternalServiceToken string

	// HiveServiceURL is hive-service's base URL. Harvest has no ownership
	// data of its own: every operation confirms the caller owns the
	// specified hive by forwarding their own access token to hive-service's
	// GET /api/v1/hives/{id}, on every call - not just once at creation
	// time (see application/harvest.HiveVerifier).
	HiveServiceURL         string
	NotificationServiceURL string
}

// Load builds a Config from environment variables, falling back to
// defaults suitable for local development where a variable is unset.
func Load() (*Config, error) {
	cfg := &Config{
		Env: getEnv("APP_ENV", "development"),

		HTTPPort:            getEnv("HTTP_PORT", "8080"),
		HTTPReadTimeout:     getDuration("HTTP_READ_TIMEOUT", 5*time.Second),
		HTTPWriteTimeout:    getDuration("HTTP_WRITE_TIMEOUT", 10*time.Second),
		HTTPIdleTimeout:     getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		HTTPShutdownTimeout: getDuration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),

		DatabaseURL:            getEnv("DATABASE_URL", ""),
		DatabaseConnectTimeout: getDuration("DATABASE_CONNECT_TIMEOUT", 5*time.Second),

		RedisAddr:           getEnv("REDIS_ADDR", ""),
		RedisConnectTimeout: getDuration("REDIS_CONNECT_TIMEOUT", 5*time.Second),

		LogLevel: getEnv("LOG_LEVEL", "info"),

		AuthJWKSURL: getEnv("AUTH_JWKS_URL", ""), InternalServiceToken: getEnv("INTERNAL_SERVICE_TOKEN", ""),
		HiveServiceURL:         getEnv("HIVE_SERVICE_URL", ""),
		NotificationServiceURL: getEnv("NOTIFICATION_SERVICE_URL", ""),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}
	if cfg.RedisAddr == "" {
		return nil, fmt.Errorf("config: REDIS_ADDR is required")
	}
	if cfg.AuthJWKSURL == "" {
		return nil, fmt.Errorf("config: AUTH_JWKS_URL is required")
	}
	if cfg.InternalServiceToken == "" {
		return nil, fmt.Errorf("config: INTERNAL_SERVICE_TOKEN is required")
	}
	if cfg.HiveServiceURL == "" {
		return nil, fmt.Errorf("config: HIVE_SERVICE_URL is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
