package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Clear env vars that might interfere
	for _, key := range []string{"RELAY_PORT", "RELAY_DATA_DIR", "RELAY_PING_INTERVAL_MS", "RELAY_PONG_TIMEOUT_MS", "RELAY_RECLAIM_DAYS", "RELAY_LOG_LEVEL", "RELAY_PORT_FILE"} {
		_ = os.Unsetenv(key)
	}

	cfg := Load()

	if cfg.Port != 4400 {
		t.Errorf("expected port 4400, got %d", cfg.Port)
	}
	if cfg.PingInterval != 60*time.Second {
		t.Errorf("expected ping interval 60s, got %v", cfg.PingInterval)
	}
	if cfg.PongTimeout != 30*time.Second {
		t.Errorf("expected pong timeout 30s, got %v", cfg.PongTimeout)
	}
	if cfg.ReclaimDays != 90 {
		t.Errorf("expected reclaim days 90, got %d", cfg.ReclaimDays)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected log level info, got %s", cfg.LogLevel)
	}
	if cfg.PortFile != "" {
		t.Errorf("expected empty port file, got %s", cfg.PortFile)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("RELAY_PORT", "5555")
	t.Setenv("RELAY_DATA_DIR", "/tmp/test-relay")
	t.Setenv("RELAY_PING_INTERVAL_MS", "10000")
	t.Setenv("RELAY_PONG_TIMEOUT_MS", "5000")
	t.Setenv("RELAY_RECLAIM_DAYS", "30")
	t.Setenv("RELAY_LOG_LEVEL", "debug")
	t.Setenv("RELAY_PORT_FILE", "/tmp/port")

	cfg := Load()

	if cfg.Port != 5555 {
		t.Errorf("expected port 5555, got %d", cfg.Port)
	}
	if cfg.DataDir != "/tmp/test-relay" {
		t.Errorf("expected data dir /tmp/test-relay, got %s", cfg.DataDir)
	}
	if cfg.PingInterval != 10*time.Second {
		t.Errorf("expected ping interval 10s, got %v", cfg.PingInterval)
	}
	if cfg.PongTimeout != 5*time.Second {
		t.Errorf("expected pong timeout 5s, got %v", cfg.PongTimeout)
	}
	if cfg.ReclaimDays != 30 {
		t.Errorf("expected reclaim days 30, got %d", cfg.ReclaimDays)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected log level debug, got %s", cfg.LogLevel)
	}
	if cfg.PortFile != "/tmp/port" {
		t.Errorf("expected port file /tmp/port, got %s", cfg.PortFile)
	}
}

func TestInvalidEnvInt(t *testing.T) {
	t.Setenv("RELAY_PORT", "not-a-number")

	cfg := Load()
	if cfg.Port != 4400 {
		t.Errorf("expected fallback port 4400 for invalid env, got %d", cfg.Port)
	}
}
