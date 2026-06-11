package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultServiceName        = "gophprofile"
	defaultVersion            = "dev"
	defaultEnv                = "local"
	defaultHTTPAddr           = ":8080"
	defaultRateLimitRPS       = 20
	defaultRateLimitBurst     = 40
	defaultCORSAllowedOrigins = "http://localhost:3000,http://localhost:5173"
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
	Vault       VaultConfig
}

type HTTPConfig struct {
	Addr               string
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	ShutdownTimeout    time.Duration
	DefaultAvatarPath  string
	CORSAllowedOrigins []string
	RateLimitRPS       int
	RateLimitBurst     int
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
	ShutdownTimeout    time.Duration
	OutboxPollInterval time.Duration
	OutboxBatchSize    int
}

type VaultConfig struct {
	Addr        string
	Token       string
	Mount       string
	ServicePath string
	Enabled     bool
	Timeout     time.Duration
}

// ApplySecrets применяет секреты из защищенного хранилища к конфигурации
func (c *Config) ApplySecrets(secrets map[string]string) {
	if value := strings.TrimSpace(secrets["DATABASE_URL"]); value != "" {
		c.Postgres.DSN = value
	}
	if value := strings.TrimSpace(secrets["S3_ENDPOINT"]); value != "" {
		c.S3.Endpoint = value
	}
	if value := strings.TrimSpace(secrets["S3_BUCKET"]); value != "" {
		c.S3.Bucket = value
	}
	if value := strings.TrimSpace(secrets["S3_ACCESS_KEY"]); value != "" {
		c.S3.AccessKey = value
	}
	if value := strings.TrimSpace(secrets["S3_SECRET_KEY"]); value != "" {
		c.S3.SecretKey = value
	}
	if value := strings.TrimSpace(secrets["S3_REGION"]); value != "" {
		c.S3.Region = value
	}
	if value := strings.TrimSpace(secrets["KAFKA_BROKERS"]); value != "" {
		c.Kafka.Brokers = splitCSV(value)
	}
	if value := strings.TrimSpace(secrets["KAFKA_CLIENT_ID"]); value != "" {
		c.Kafka.ClientID = value
	}
	if value := strings.TrimSpace(secrets["KAFKA_CONSUMER_GROUP"]); value != "" {
		c.Kafka.ConsumerGroup = value
	}
}

// Load читает конфигурацию приложения из переменных окружения
func Load() Config {
	return Config{
		ServiceName: envString("SERVICE_NAME", defaultServiceName),
		Version:     envString("APP_VERSION", defaultVersion),
		Env:         envString("APP_ENV", defaultEnv),
		LogLevel:    envString("LOG_LEVEL", "debug"),
		HTTP: HTTPConfig{
			Addr:               envString("HTTP_ADDR", defaultHTTPAddr),
			ReadTimeout:        envDuration("HTTP_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:       envDuration("HTTP_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:        envDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout:    envDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
			DefaultAvatarPath:  envString("DEFAULT_AVATAR_PATH", "web/frontend/src/assets/default_avatar.png"),
			CORSAllowedOrigins: envCSV("CORS_ALLOWED_ORIGINS", splitCSV(defaultCORSAllowedOrigins)),
			RateLimitRPS:       envInt("API_RATE_LIMIT_RPS", defaultRateLimitRPS),
			RateLimitBurst:     envInt("API_RATE_LIMIT_BURST", defaultRateLimitBurst),
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
			ShutdownTimeout:    envDuration("WORKER_SHUTDOWN_TIMEOUT", 10*time.Second),
			OutboxPollInterval: envDuration("OUTBOX_POLL_INTERVAL", 5*time.Second),
			OutboxBatchSize:    envInt("OUTBOX_BATCH_SIZE", 100),
		},
		Vault: VaultConfig{
			Addr:        envString("VAULT_ADDR", "http://localhost:8200"),
			Token:       envString("VAULT_TOKEN", ""),
			Mount:       envString("VAULT_KV_MOUNT", "secret"),
			ServicePath: envString("VAULT_SERVICE_PATH", "gophprofile"),
			Enabled:     envBool("VAULT_ENABLED", false),
			Timeout:     envDuration("VAULT_TIMEOUT", 5*time.Second),
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

// envInt возвращает int из переменной окружения или дефолт
func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
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

	items := splitCSV(value)
	if len(items) == 0 {
		return fallback
	}
	return items
}

// splitCSV разбирает CSV-строку без пустых значений
func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}
