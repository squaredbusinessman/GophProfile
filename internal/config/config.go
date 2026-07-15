// Package config загружает и проверяет конфигурацию приложения
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

// Config содержит полную конфигурацию приложения
type Config struct {
	// ServiceName задаёт имя бизнес-сервиса
	ServiceName string
	// Version задаёт версию сборки приложения
	Version string
	// Env задаёт окружение развёртывания
	Env string
	// Observability содержит настройки телеметрии и логирования
	Observability ObservabilityConfig
	// HTTP содержит настройки HTTP-сервера
	HTTP HTTPConfig
	// Postgres содержит настройки PostgreSQL
	Postgres PostgresConfig
	// S3 содержит настройки объектного хранилища
	S3 S3Config
	// Kafka содержит настройки брокера сообщений
	Kafka KafkaConfig
	// Worker содержит настройки фонового процесса
	Worker WorkerConfig
	// Vault содержит настройки защищённого хранилища секретов
	Vault VaultConfig
	// CircuitBreaker содержит настройки защиты внешних зависимостей
	CircuitBreaker CircuitBreakerConfig
}

// ObservabilityConfig содержит настройки трассировки, метрик и логирования
type ObservabilityConfig struct {
	// Enabled включает экспорт телеметрии
	Enabled bool
	// ServiceName задаёт имя сервиса в атрибутах OpenTelemetry
	ServiceName string
	// OTLPEndpoint задаёт адрес OTLP-приёмника трассировок
	OTLPEndpoint string
	// OTLPInsecure разрешает подключение к OTLP-приёмнику без TLS
	OTLPInsecure bool
	// TracesSampler задаёт стратегию семплирования трассировок
	TracesSampler string
	// TracesSamplerArg задаёт долю трассировок для вероятностного семплирования
	TracesSamplerArg float64
	// MetricsAddr задаёт адрес отдельного HTTP-сервера метрик
	MetricsAddr string
	// LogLevel задаёт минимальный уровень логирования
	LogLevel string
	// LogFormat задаёт формат логов
	LogFormat string
}

// HTTPConfig содержит настройки HTTP-сервера приложения
type HTTPConfig struct {
	// Addr задаёт адрес HTTP-сервера
	Addr string
	// ReadTimeout ограничивает чтение HTTP-запроса
	ReadTimeout time.Duration
	// WriteTimeout ограничивает запись HTTP-ответа
	WriteTimeout time.Duration
	// IdleTimeout ограничивает время простоя соединения
	IdleTimeout time.Duration
	// ShutdownTimeout ограничивает корректную остановку сервера
	ShutdownTimeout time.Duration
	// DefaultAvatarPath задаёт путь к изображению по умолчанию
	DefaultAvatarPath string
	// CORSAllowedOrigins содержит разрешённые источники CORS
	CORSAllowedOrigins []string
	// RateLimitRPS задаёт допустимое число запросов в секунду
	RateLimitRPS int
	// RateLimitBurst задаёт размер кратковременного всплеска запросов
	RateLimitBurst int
}

// PostgresConfig содержит настройки подключения к PostgreSQL
type PostgresConfig struct {
	// DSN задаёт строку подключения к PostgreSQL
	DSN string
}

// S3Config содержит настройки подключения к S3-совместимому хранилищу
type S3Config struct {
	// Endpoint задаёт адрес объектного хранилища
	Endpoint string
	// Bucket задаёт имя контейнера для аватаров
	Bucket string
	// AccessKey задаёт идентификатор доступа
	AccessKey string
	// SecretKey задаёт секретный ключ доступа
	SecretKey string
	// UsePathStyle включает адресацию bucket через путь
	UsePathStyle bool
	// Region задаёт регион объектного хранилища
	Region string
}

// KafkaConfig содержит настройки подключения к Kafka
type KafkaConfig struct {
	// Brokers содержит адреса брокеров Kafka
	Brokers []string
	// ClientID задаёт идентификатор клиента Kafka
	ClientID string
	// ConsumerGroup задаёт группу потребителей Kafka
	ConsumerGroup string
}

// WorkerConfig содержит настройки фонового процесса
type WorkerConfig struct {
	// ShutdownTimeout ограничивает корректную остановку фонового процесса
	ShutdownTimeout time.Duration
	// OutboxPollInterval задаёт интервал опроса исходящих событий
	OutboxPollInterval time.Duration
	// OutboxBatchSize задаёт максимальный размер пакета исходящих событий
	OutboxBatchSize int
}

// VaultConfig содержит настройки подключения к Vault
type VaultConfig struct {
	// Addr задаёт адрес Vault
	Addr string
	// Token задаёт токен доступа к Vault
	Token string
	// Mount задаёт имя KV mount в Vault
	Mount string
	// ServicePath задаёт путь к секретам сервиса
	ServicePath string
	// Enabled включает загрузку секретов из Vault
	Enabled bool
	// Timeout ограничивает запрос к Vault
	Timeout time.Duration
}

// CircuitBreakerConfig содержит настройки circuit breaker для внешних зависимостей
type CircuitBreakerConfig struct {
	// Enabled включает circuit breaker
	Enabled bool
	// FailureThreshold задаёт число последовательных ошибок перед открытием
	FailureThreshold int
	// OpenTimeout задаёт время до пробного запроса после открытия
	OpenTimeout time.Duration
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
	return load("")
}

// LoadForProcess читает конфигурацию с разными настройками наблюдаемости для серверного и фонового процессов
func LoadForProcess(process string) Config {
	return load(strings.ToLower(strings.TrimSpace(process)))
}

// load читает конфигурацию с настройками для указанного процесса
func load(process string) Config {
	observabilityServiceName := defaultServiceName
	metricsAddr := ":9090"
	if process != "" {
		observabilityServiceName += "-" + process
	}
	if process == "worker" {
		metricsAddr = ":9091"
	}

	return Config{
		ServiceName: envString("SERVICE_NAME", defaultServiceName),
		Version:     envString("APP_VERSION", defaultVersion),
		Env:         envString("APP_ENV", defaultEnv),
		Observability: ObservabilityConfig{
			Enabled:          envBool("OTEL_ENABLED", false),
			ServiceName:      envString("OTEL_SERVICE_NAME", observabilityServiceName),
			OTLPEndpoint:     envString("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
			OTLPInsecure:     envBool("OTEL_EXPORTER_OTLP_INSECURE", true),
			TracesSampler:    envString("OTEL_TRACES_SAMPLER", "parentbased_always_on"),
			TracesSamplerArg: envFloat("OTEL_TRACES_SAMPLER_ARG", 1),
			MetricsAddr:      envString("METRICS_ADDR", metricsAddr),
			LogLevel:         envString("LOG_LEVEL", "info"),
			LogFormat:        envString("LOG_FORMAT", "json"),
		},
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
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          envBool("CIRCUIT_BREAKER_ENABLED", true),
			FailureThreshold: envInt("CIRCUIT_BREAKER_FAILURE_THRESHOLD", 5),
			OpenTimeout:      envDuration("CIRCUIT_BREAKER_OPEN_TIMEOUT", 30*time.Second),
		},
	}
}

// envFloat возвращает число с плавающей точкой из переменной окружения или значение по умолчанию
func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 || parsed > 1 {
		return fallback
	}
	return parsed
}

// envString возвращает строковое значение переменной окружения или значение по умолчанию
func envString(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// envDuration возвращает длительность из переменной окружения или значение по умолчанию
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

// envInt возвращает целое число из переменной окружения или значение по умолчанию
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

// envBool возвращает логическое значение из переменной окружения или значение по умолчанию
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

// envCSV возвращает список строк из CSV-переменной окружения или значение по умолчанию
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
