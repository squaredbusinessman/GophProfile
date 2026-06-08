package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultServiceName = "gophprofile"
	defaultVersion     = "dev"
	defaultEnv         = "local"
	defaultHTTPAddr    = ":8080"
)

type Config struct {
	ServiceName string
	Version     string
	Env         string
	LogLevel    string
	HTTP        HTTPConfig
	Postgres    PostgresConfig
	S3          S3Config
	Kafka       KafkaConfig
	Worker      WorkerConfig
}

type HTTPConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

type PostgresConfig struct {
	DSN string
}

type S3Config struct {
	Endpoint     string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	Region       string
}

type KafkaConfig struct {
	Brokers       []string
	ClientID      string
	ConsumerGroup string
}

type WorkerConfig struct {
	ShutdownTimeout time.Duration
}

// Load читает конфигурацию приложения из переменных окружения
func Load() Config {
	return Config{
		ServiceName: envString("SERVICE_NAME", defaultServiceName),
		Version:     envString("APP_VERSION", defaultVersion),
		Env:         envString("APP_ENV", defaultEnv),
		LogLevel:    envString("LOG_LEVEL", "debug"),
		HTTP: HTTPConfig{
			Addr:            envString("HTTP_ADDR", defaultHTTPAddr),
			ReadTimeout:     envDuration("HTTP_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:    envDuration("HTTP_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:     envDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: envDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		Postgres: PostgresConfig{
			DSN: envString("DATABASE_URL", "postgres://gophprofile:gophprofile@localhost:5432/gophprofile?sslmode=disable"),
		},
		S3: S3Config{
			Endpoint:     envString("S3_ENDPOINT", "http://localhost:9000"),
			Bucket:       envString("S3_BUCKET", "gophprofile-avatars"),
			AccessKey:    envString("S3_ACCESS_KEY", "minioadmin"),
			SecretKey:    envString("S3_SECRET_KEY", "minioadmin"),
			UsePathStyle: envBool("S3_USE_PATH_STYLE", true),
			Region:       envString("S3_REGION", "us-east-1"),
		},
		Kafka: KafkaConfig{
			Brokers:       envCSV("KAFKA_BROKERS", []string{"localhost:9092"}),
			ClientID:      envString("KAFKA_CLIENT_ID", defaultServiceName),
			ConsumerGroup: envString("KAFKA_CONSUMER_GROUP", "gophprofile-avatar-worker"),
		},
		Worker: WorkerConfig{
			ShutdownTimeout: envDuration("WORKER_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
	}
}

// envString возвращает строковое значение переменной окружения или дефолт
func envString(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// envDuration возвращает duration из переменной окружения или дефолт
func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return duration
}

// envBool возвращает boolean из переменной окружения или дефолт
func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// envCSV возвращает список строк из CSV-переменной окружения или дефолт
func envCSV(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return fallback
	}
	return items
}
