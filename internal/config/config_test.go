package config

import (
	"reflect"
	"testing"
	"time"
)

// TestLoadUsesLocalDefaults проверяет локальные значения конфигурации по умолчанию
func TestLoadUsesLocalDefaults(t *testing.T) {
	t.Setenv("SERVICE_NAME", "")
	t.Setenv("APP_VERSION", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("API_RATE_LIMIT_RPS", "")
	t.Setenv("API_RATE_LIMIT_BURST", "")
	t.Setenv("DEFAULT_AVATAR_PATH", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("KAFKA_BROKERS", "")
	t.Setenv("OTEL_ENABLED", "")
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("METRICS_ADDR", "")
	t.Setenv("LOG_FORMAT", "")

	cfg := Load()

	if cfg.ServiceName != "gophprofile" {
		t.Fatalf("ServiceName = %q, want %q", cfg.ServiceName, "gophprofile")
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("HTTP.Addr = %q, want %q", cfg.HTTP.Addr, ":8080")
	}
	if !reflect.DeepEqual(cfg.HTTP.CORSAllowedOrigins, []string{"http://localhost:3000", "http://localhost:5173"}) {
		t.Fatalf("HTTP.CORSAllowedOrigins = %#v, want local frontend origins", cfg.HTTP.CORSAllowedOrigins)
	}
	if cfg.HTTP.RateLimitRPS != 20 {
		t.Fatalf("HTTP.RateLimitRPS = %d, want 20", cfg.HTTP.RateLimitRPS)
	}
	if cfg.HTTP.RateLimitBurst != 40 {
		t.Fatalf("HTTP.RateLimitBurst = %d, want 40", cfg.HTTP.RateLimitBurst)
	}
	if cfg.HTTP.DefaultAvatarPath != "web/frontend/src/assets/default_avatar.png" {
		t.Fatalf("HTTP.DefaultAvatarPath = %q, want frontend asset path", cfg.HTTP.DefaultAvatarPath)
	}
	if cfg.Postgres.DSN == "" {
		t.Fatal("Postgres.DSN should have a local default")
	}
	if !reflect.DeepEqual(cfg.Kafka.Brokers, []string{"localhost:9092"}) {
		t.Fatalf("Kafka.Brokers = %#v, want localhost:9092", cfg.Kafka.Brokers)
	}
	if cfg.Worker.OutboxPollInterval != 5*time.Second {
		t.Fatalf("Worker.OutboxPollInterval = %s, want 5s", cfg.Worker.OutboxPollInterval)
	}
	if cfg.Worker.OutboxBatchSize != 100 {
		t.Fatalf("Worker.OutboxBatchSize = %d, want 100", cfg.Worker.OutboxBatchSize)
	}
	if cfg.Observability.Enabled {
		t.Fatal("Observability.Enabled = true, want false")
	}
	if cfg.Observability.ServiceName != "gophprofile" || cfg.Observability.MetricsAddr != ":9090" {
		t.Fatalf("Observability defaults = %q/%q, want gophprofile/:9090", cfg.Observability.ServiceName, cfg.Observability.MetricsAddr)
	}
	if cfg.Observability.LogLevel != "info" || cfg.Observability.LogFormat != "json" {
		t.Fatalf("logging defaults = %q/%q, want info/json", cfg.Observability.LogLevel, cfg.Observability.LogFormat)
	}
	if !cfg.CircuitBreaker.Enabled || cfg.CircuitBreaker.FailureThreshold != 5 || cfg.CircuitBreaker.OpenTimeout != 30*time.Second {
		t.Fatalf("CircuitBreaker defaults = %#v, want enabled threshold 5 timeout 30s", cfg.CircuitBreaker)
	}
}

// TestLoadReadsEnvironment проверяет чтение конфигурации из переменных окружения
func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("SERVICE_NAME", "custom-profile")
	t.Setenv("APP_VERSION", "test-version")
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("HTTP_READ_TIMEOUT", "3s")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com, https://admin.example.com")
	t.Setenv("API_RATE_LIMIT_RPS", "7")
	t.Setenv("API_RATE_LIMIT_BURST", "9")
	t.Setenv("DEFAULT_AVATAR_PATH", "/app/default_avatar.png")
	t.Setenv("S3_USE_PATH_STYLE", "false")
	t.Setenv("KAFKA_BROKERS", "localhost:19092, localhost:29092")
	t.Setenv("OUTBOX_POLL_INTERVAL", "2s")
	t.Setenv("OUTBOX_BATCH_SIZE", "25")
	t.Setenv("OTEL_ENABLED", "true")
	t.Setenv("OTEL_SERVICE_NAME", "profile-api")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "false")
	t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	t.Setenv("METRICS_ADDR", "127.0.0.1:19090")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("CIRCUIT_BREAKER_ENABLED", "false")
	t.Setenv("CIRCUIT_BREAKER_FAILURE_THRESHOLD", "3")
	t.Setenv("CIRCUIT_BREAKER_OPEN_TIMEOUT", "15s")

	cfg := Load()

	if cfg.ServiceName != "custom-profile" {
		t.Fatalf("ServiceName = %q, want %q", cfg.ServiceName, "custom-profile")
	}
	if cfg.Version != "test-version" {
		t.Fatalf("Version = %q, want %q", cfg.Version, "test-version")
	}
	if cfg.HTTP.Addr != "127.0.0.1:9090" {
		t.Fatalf("HTTP.Addr = %q, want %q", cfg.HTTP.Addr, "127.0.0.1:9090")
	}
	if cfg.HTTP.ReadTimeout != 3*time.Second {
		t.Fatalf("HTTP.ReadTimeout = %s, want 3s", cfg.HTTP.ReadTimeout)
	}
	if !reflect.DeepEqual(cfg.HTTP.CORSAllowedOrigins, []string{"https://app.example.com", "https://admin.example.com"}) {
		t.Fatalf("HTTP.CORSAllowedOrigins = %#v, want configured origins", cfg.HTTP.CORSAllowedOrigins)
	}
	if cfg.HTTP.RateLimitRPS != 7 {
		t.Fatalf("HTTP.RateLimitRPS = %d, want 7", cfg.HTTP.RateLimitRPS)
	}
	if cfg.HTTP.RateLimitBurst != 9 {
		t.Fatalf("HTTP.RateLimitBurst = %d, want 9", cfg.HTTP.RateLimitBurst)
	}
	if cfg.HTTP.DefaultAvatarPath != "/app/default_avatar.png" {
		t.Fatalf("HTTP.DefaultAvatarPath = %q, want configured path", cfg.HTTP.DefaultAvatarPath)
	}
	if cfg.S3.UsePathStyle {
		t.Fatal("S3.UsePathStyle = true, want false")
	}
	if !reflect.DeepEqual(cfg.Kafka.Brokers, []string{"localhost:19092", "localhost:29092"}) {
		t.Fatalf("Kafka.Brokers = %#v, want two configured brokers", cfg.Kafka.Brokers)
	}
	if cfg.Worker.OutboxPollInterval != 2*time.Second {
		t.Fatalf("Worker.OutboxPollInterval = %s, want 2s", cfg.Worker.OutboxPollInterval)
	}
	if cfg.Worker.OutboxBatchSize != 25 {
		t.Fatalf("Worker.OutboxBatchSize = %d, want 25", cfg.Worker.OutboxBatchSize)
	}
	if !cfg.Observability.Enabled || cfg.Observability.ServiceName != "profile-api" {
		t.Fatalf("Observability enabled/name = %t/%q", cfg.Observability.Enabled, cfg.Observability.ServiceName)
	}
	if cfg.Observability.OTLPEndpoint != "collector:4317" || cfg.Observability.OTLPInsecure {
		t.Fatalf("OTLP endpoint/insecure = %q/%t", cfg.Observability.OTLPEndpoint, cfg.Observability.OTLPInsecure)
	}
	if cfg.Observability.TracesSampler != "traceidratio" || cfg.Observability.TracesSamplerArg != 0.25 {
		t.Fatalf("sampler = %q/%f", cfg.Observability.TracesSampler, cfg.Observability.TracesSamplerArg)
	}
	if cfg.Observability.MetricsAddr != "127.0.0.1:19090" || cfg.Observability.LogLevel != "warn" || cfg.Observability.LogFormat != "json" {
		t.Fatalf("metrics/logging overrides were not loaded: %#v", cfg.Observability)
	}
	if cfg.CircuitBreaker.Enabled || cfg.CircuitBreaker.FailureThreshold != 3 || cfg.CircuitBreaker.OpenTimeout != 15*time.Second {
		t.Fatalf("CircuitBreaker overrides = %#v, want disabled threshold 3 timeout 15s", cfg.CircuitBreaker)
	}
}

// TestLoadForProcessUsesDistinctObservabilityDefaults проверяет разные настройки серверного и фонового процессов
func TestLoadForProcessUsesDistinctObservabilityDefaults(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("METRICS_ADDR", "")

	server := LoadForProcess("server")
	worker := LoadForProcess("worker")

	if server.Observability.ServiceName != "gophprofile-server" || server.Observability.MetricsAddr != ":9090" {
		t.Fatalf("server defaults = %q/%q", server.Observability.ServiceName, server.Observability.MetricsAddr)
	}
	if worker.Observability.ServiceName != "gophprofile-worker" || worker.Observability.MetricsAddr != ":9091" {
		t.Fatalf("worker defaults = %q/%q", worker.Observability.ServiceName, worker.Observability.MetricsAddr)
	}
}

// TestApplySecretsOverridesSensitiveConfig проверяет применение секретов к конфигу
func TestApplySecretsOverridesSensitiveConfig(t *testing.T) {
	cfg := Load()

	cfg.ApplySecrets(map[string]string{
		"DATABASE_URL":         "postgres://from-vault",
		"S3_ACCESS_KEY":        "vault-access-key",
		"S3_SECRET_KEY":        "vault-secret-key",
		"KAFKA_BROKERS":        "kafka-1:9092,kafka-2:9092",
		"KAFKA_CONSUMER_GROUP": "vault-group",
	})

	if cfg.Postgres.DSN != "postgres://from-vault" {
		t.Fatalf("Postgres.DSN = %q, want vault value", cfg.Postgres.DSN)
	}
	if cfg.S3.AccessKey != "vault-access-key" {
		t.Fatalf("S3.AccessKey = %q, want vault value", cfg.S3.AccessKey)
	}
	if cfg.S3.SecretKey != "vault-secret-key" {
		t.Fatalf("S3.SecretKey = %q, want vault value", cfg.S3.SecretKey)
	}
	if !reflect.DeepEqual(cfg.Kafka.Brokers, []string{"kafka-1:9092", "kafka-2:9092"}) {
		t.Fatalf("Kafka.Brokers = %#v, want vault brokers", cfg.Kafka.Brokers)
	}
	if cfg.Kafka.ConsumerGroup != "vault-group" {
		t.Fatalf("Kafka.ConsumerGroup = %q, want vault-group", cfg.Kafka.ConsumerGroup)
	}
}
