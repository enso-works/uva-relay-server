package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	Port          int
	PortFile      string
	DataDir       string
	PingInterval  time.Duration
	PongTimeout   time.Duration
	ReclaimDays   int
	LogLevel      string
	LogJSON       bool
}

func Load() *Config {
	home, _ := os.UserHomeDir()
	dataDir := envString("RELAY_DATA_DIR", filepath.Join(home, ".uvame-relay"))

	return &Config{
		Port:         envInt("RELAY_PORT", 4400),
		PortFile:     envString("RELAY_PORT_FILE", ""),
		DataDir:      dataDir,
		PingInterval: time.Duration(envInt("RELAY_PING_INTERVAL_MS", 60000)) * time.Millisecond,
		PongTimeout:  time.Duration(envInt("RELAY_PONG_TIMEOUT_MS", 30000)) * time.Millisecond,
		ReclaimDays:  envInt("RELAY_RECLAIM_DAYS", 90),
		LogLevel:     envString("RELAY_LOG_LEVEL", "info"),
		LogJSON:      envString("RELAY_LOG_FORMAT", "text") == "json",
	}
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
