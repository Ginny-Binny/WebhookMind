package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

type IngestionConfig struct {
	Port         int
	MaxBodyBytes int64
}

type OrchestratorConfig struct {
	Workers             int
	MaxWorkers          int
	QueueScaleThreshold int64
}

type DeliveryConfig struct {
	Workers    int
	MaxWorkers int
	MaxRetries int
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type ScyllaConfig struct {
	Hosts    []string
	Keyspace string
}

type PostgresConfig struct {
	DSN string
}

type Config struct {
	Ingestion    IngestionConfig
	Orchestrator OrchestratorConfig
	Delivery     DeliveryConfig
	Redis        RedisConfig
	Scylla       ScyllaConfig
	Postgres     PostgresConfig
	LogLevel     slog.Level
}

func Load() (*Config, error) {
	postgresDSN := os.Getenv("POSTGRES_DSN")
	if postgresDSN == "" {
		return nil, fmt.Errorf("POSTGRES_DSN is required")
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		return nil, fmt.Errorf("REDIS_ADDR is required")
	}

	scyllaHosts := envOrDefault("SCYLLA_HOSTS", "127.0.0.1")

	cfg := &Config{
		Ingestion: IngestionConfig{
			Port:         envOrDefaultInt("INGESTION_PORT", 8080),
			MaxBodyBytes: int64(envOrDefaultInt("INGESTION_MAX_BODY_BYTES", 10485760)),
		},
		Orchestrator: OrchestratorConfig{
			Workers:             envOrDefaultInt("ORCHESTRATOR_WORKERS", 10),
			MaxWorkers:          envOrDefaultInt("ORCHESTRATOR_MAX_WORKERS", 50),
			QueueScaleThreshold: int64(envOrDefaultInt("ORCHESTRATOR_QUEUE_SCALE_THRESHOLD", 1000)),
		},
		Delivery: DeliveryConfig{
			Workers:    envOrDefaultInt("DELIVERY_WORKERS", 20),
			MaxWorkers: envOrDefaultInt("DELIVERY_MAX_WORKERS", 100),
			MaxRetries: envOrDefaultInt("DELIVERY_MAX_RETRIES", 4),
		},
		Redis: RedisConfig{
			Addr:     redisAddr,
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       envOrDefaultInt("REDIS_DB", 0),
		},
		Scylla: ScyllaConfig{
			Hosts:    strings.Split(scyllaHosts, ","),
			Keyspace: envOrDefault("SCYLLA_KEYSPACE", "webhookmind"),
		},
		Postgres: PostgresConfig{
			DSN: postgresDSN,
		},
		LogLevel: parseLogLevel(envOrDefault("LOG_LEVEL", "info")),
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
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

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
