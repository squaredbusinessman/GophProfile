package app

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/squaredbusinessman/GophProfile/internal/config"
	"go.opentelemetry.io/otel/trace"
)

// defaultLogLevel задаёт безопасный уровень логирования по умолчанию
const defaultLogLevel = zerolog.InfoLevel

// requestIDContextKey задаёт приватный ключ идентификатора запроса в context
type requestIDContextKey struct{}

// NewLogger создает Zerolog logger с едиными полями сервиса и окружения
func NewLogger(cfg config.Config) zerolog.Logger {
	return newLogger(cfg, os.Stdout)
}

// newLogger создаёт Zerolog logger с указанным потоком вывода
func newLogger(cfg config.Config, output io.Writer) zerolog.Logger {
	level, err := zerolog.ParseLevel(strings.ToLower(cfg.Observability.LogLevel))
	if err != nil {
		level = defaultLogLevel
	}

	zerolog.SetGlobalLevel(level)
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.TimestampFieldName = "ts"
	zerolog.LevelFieldName = "level"
	zerolog.MessageFieldName = "message"
	zerolog.ErrorFieldName = "error"
	zerolog.CallerFieldName = "caller"

	writer := output
	if strings.EqualFold(cfg.Env, "local") && strings.EqualFold(cfg.Observability.LogFormat, "console") {
		writer = zerolog.ConsoleWriter{Out: output, TimeFormat: time.RFC3339}
	}

	logger := zerolog.New(writer).Hook(severityHook{}).With().
		Timestamp().
		Caller().
		Str("service", cfg.ServiceName).
		Str("version", cfg.Version).
		Str("env", cfg.Env).
		Logger()
	log.Logger = logger
	return logger
}

// ContextWithLogger сохраняет Zerolog logger в context
func ContextWithLogger(ctx context.Context, logger zerolog.Logger) context.Context {
	return logger.WithContext(ctx)
}

// ContextWithRequestID сохраняет идентификатор HTTP-запроса в context
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

// RequestIDFromContext возвращает идентификатор HTTP-запроса из context
func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

// LoggerFromContext возвращает дочерний logger с полями корреляции активного запроса и span
func LoggerFromContext(ctx context.Context) *zerolog.Logger {
	logger := *zerolog.Ctx(ctx)
	fields := logger.With()

	if requestID := RequestIDFromContext(ctx); requestID != "" {
		fields = fields.Str("request_id", requestID)
	}
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		fields = fields.
			Str("trace_id", spanContext.TraceID().String()).
			Str("span_id", spanContext.SpanID().String())
	}
	child := fields.Logger()
	return &child
}

// ErrorType возвращает тип первопричины ошибки без потенциально секретного текста
func ErrorType(err error) string {
	if err == nil {
		return ""
	}
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			break
		}
		err = unwrapped
	}
	return reflect.TypeOf(err).String()
}

// severityHook добавляет уровень события в формате внешних систем логирования
type severityHook struct{}

// Run добавляет поле severity к каждому событию Zerolog
func (severityHook) Run(event *zerolog.Event, level zerolog.Level, message string) {
	if level == zerolog.NoLevel {
		return
	}
	event.Str("severity", strings.ToUpper(level.String()))
}
