// Package config loads process configuration from environment variables
// (12-factor). OpenTelemetry exporters read the standard OTEL_* variables themselves.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	LogLevel       string
	GRPCAddr       string
	HTTPAddr       string
	// WorkerHTTPAddr serves the worker's /healthz and /readyz.
	WorkerHTTPAddr  string
	DatabaseURL     string
	RabbitMQURL     string
	ShutdownTimeout time.Duration
	// TelemetryEnabled is true when OTEL_EXPORTER_OTLP_ENDPOINT is set.
	TelemetryEnabled bool

	OutboxPollInterval time.Duration
	OutboxBatchSize    int
	ConsumerPrefetch   int
}

// Load reads the environment. Required values without a safe default fail fast.
func Load(serviceName string) (Config, error) {
	cfg := Config{
		ServiceName:      envOr("OTEL_SERVICE_NAME", serviceName),
		ServiceVersion:   envOr("SERVICE_VERSION", "dev"),
		Environment:      envOr("ENVIRONMENT", "local"),
		LogLevel:         envOr("LOG_LEVEL", "info"),
		GRPCAddr:         envOr("GRPC_ADDR", ":9090"),
		HTTPAddr:         envOr("HTTP_ADDR", ":8080"),
		WorkerHTTPAddr:   envOr("WORKER_HTTP_ADDR", ":8081"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		RabbitMQURL:      os.Getenv("RABBITMQ_URL"),
		TelemetryEnabled: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "",
	}
	var err error
	if cfg.ShutdownTimeout, err = durationOr("SHUTDOWN_TIMEOUT", 20*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.OutboxPollInterval, err = durationOr("OUTBOX_POLL_INTERVAL", 250*time.Millisecond); err != nil {
		return Config{}, err
	}
	if cfg.OutboxBatchSize, err = intOr("OUTBOX_BATCH_SIZE", 100); err != nil {
		return Config{}, err
	}
	if cfg.ConsumerPrefetch, err = intOr("CONSUMER_PREFETCH", 20); err != nil {
		return Config{}, err
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationOr(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

func intOr(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
}
