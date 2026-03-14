package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port             int
	PortFile         string
	DataDir          string
	PingInterval     time.Duration
	PongTimeout      time.Duration
	ReclaimDays      int
	LogLevel         string
	LogJSON          bool
	AdminToken       string
	ServerAuthToken  string
	AllowedOrigins   []string
	WSOriginPatterns []string

	// Redis
	RedisURL      string
	RedisPassword string
	RedisDB       int

	// JWT
	JWTSecret string

	// Rate limiting
	RateLimitConnectionsPerMin int
	RateLimitMessagesPerSec    int

	// Message buffer
	BufferSize int
}

func Load() *Config {
	home, _ := os.UserHomeDir()
	dataDir := envString("RELAY_DATA_DIR", filepath.Join(home, ".uvame-relay"))

	return &Config{
		Port:             envInt("RELAY_PORT", 4400),
		PortFile:         envString("RELAY_PORT_FILE", ""),
		DataDir:          dataDir,
		PingInterval:     time.Duration(envInt("RELAY_PING_INTERVAL_MS", 60000)) * time.Millisecond,
		PongTimeout:      time.Duration(envInt("RELAY_PONG_TIMEOUT_MS", 30000)) * time.Millisecond,
		ReclaimDays:      envInt("RELAY_RECLAIM_DAYS", 90),
		LogLevel:         envString("RELAY_LOG_LEVEL", "info"),
		LogJSON:          envString("RELAY_LOG_FORMAT", "text") == "json",
		AdminToken:       envString("RELAY_ADMIN_TOKEN", ""),
		ServerAuthToken:  envString("RELAY_SERVER_AUTH_TOKEN", ""),
		AllowedOrigins:   envCSV("RELAY_ALLOWED_ORIGINS", []string{"*"}),
		WSOriginPatterns: envCSV("RELAY_WS_ORIGIN_PATTERNS", []string{"*"}),

		RedisURL:      envString("RELAY_REDIS_URL", ""),
		RedisPassword: envString("RELAY_REDIS_PASSWORD", ""),
		RedisDB:       envInt("RELAY_REDIS_DB", 0),

		JWTSecret: envString("RELAY_JWT_SECRET", ""),

		RateLimitConnectionsPerMin: envInt("RELAY_RATE_LIMIT_CONNECTIONS_PER_MIN", 30),
		RateLimitMessagesPerSec:    envInt("RELAY_RATE_LIMIT_MESSAGES_PER_SEC", 1000),

		BufferSize: envInt("RELAY_BUFFER_SIZE", 100),
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

func envCSV(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return append([]string(nil), fallback...)
	}

	parts := strings.Split(v, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	if len(values) == 0 {
		return append([]string(nil), fallback...)
	}
	return values
}
