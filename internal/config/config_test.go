package config

import (
	"strings"
	"testing"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"DATABASE_URL":             "postgres://user:pass@localhost/db",
		"REDIS_ADDR":               "localhost:6379",
		"AUTH_JWKS_URL":            "http://auth-service:8080/.well-known/jwks.json",
		"INTERNAL_SERVICE_TOKEN":   "test-token",
		"HIVE_SERVICE_URL":         "http://hive-service:8080",
		"NOTIFICATION_SERVICE_URL": "http://notification-service:8080",
	} {
		t.Setenv(key, value)
	}
}

func TestLoadAcceptsValidNotificationServiceURL(t *testing.T) {
	setRequiredEnv(t)
	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsInvalidNotificationServiceURL(t *testing.T) {
	for _, value := range []string{"", "://broken", "/internal", "ftp://notification-service:8080", "http://"} {
		t.Run(value, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("NOTIFICATION_SERVICE_URL", value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "NOTIFICATION_SERVICE_URL") {
				t.Fatalf("Load() error = %v, want notification URL validation error", err)
			}
		})
	}
}
