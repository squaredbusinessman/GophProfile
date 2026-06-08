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
	t.Setenv("DATABASE_URL", "")
	t.Setenv("KAFKA_BROKERS", "")

	cfg := Load()

	if cfg.ServiceName != "gophprofile" {
		t.Fatalf("ServiceName = %q, want %q", cfg.ServiceName, "gophprofile")
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("HTTP.Addr = %q, want %q", cfg.HTTP.Addr, ":8080")
	}
	if cfg.Postgres.DSN == "" {
		t.Fatal("Postgres.DSN should have a local default")
	}
	if !reflect.DeepEqual(cfg.Kafka.Brokers, []string{"localhost:9092"}) {
		t.Fatalf("Kafka.Brokers = %#v, want localhost:9092", cfg.Kafka.Brokers)
	}
}

// TestLoadReadsEnvironment проверяет чтение конфигурации из env
func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("SERVICE_NAME", "custom-profile")
	t.Setenv("APP_VERSION", "test-version")
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("HTTP_READ_TIMEOUT", "3s")
	t.Setenv("S3_USE_PATH_STYLE", "false")
	t.Setenv("KAFKA_BROKERS", "localhost:19092, localhost:29092")

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
	if cfg.S3.UsePathStyle {
		t.Fatal("S3.UsePathStyle = true, want false")
	}
	if !reflect.DeepEqual(cfg.Kafka.Brokers, []string{"localhost:19092", "localhost:29092"}) {
		t.Fatalf("Kafka.Brokers = %#v, want two configured brokers", cfg.Kafka.Brokers)
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
