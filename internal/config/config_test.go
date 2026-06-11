package config

import (
	"reflect"
	"testing"
	"time"
)

// TestLoadUsesLocalDefaults проверяет локальные дефолты конфигурации
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
}

// TestLoadReadsEnvironment проверяет чтение конфигурации из env
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
