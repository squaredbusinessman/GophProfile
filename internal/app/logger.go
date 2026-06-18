package app

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/squaredbusinessman/GophProfile/internal/config"
)

// NewLogger создает Zerolog logger с едиными полями сервиса и окружения
func NewLogger(cfg config.Config) zerolog.Logger {
	level, err := zerolog.ParseLevel(strings.ToLower(cfg.Observability.LogLevel))
	if err != nil {
		level = zerolog.DebugLevel
	}

	zerolog.SetGlobalLevel(level)
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.TimestampFieldName = "ts"
	zerolog.LevelFieldName = "level"
	zerolog.MessageFieldName = "message"
	zerolog.ErrorFieldName = "error"
	zerolog.CallerFieldName = "caller"

	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	if strings.EqualFold(cfg.Observability.LogFormat, "json") {
		logger := zerolog.New(os.Stdout).Hook(severityHook{})
		log.Logger = logger
		return logger.With().
			Timestamp().
			Caller().
			Str("service", cfg.ServiceName).
			Str("version", cfg.Version).
			Str("env", cfg.Env).
			Logger()
	}

	logger := zerolog.New(output).Hook(severityHook{})
	log.Logger = logger
	return logger.With().
		Timestamp().
		Caller().
		Str("service", cfg.ServiceName).
		Str("version", cfg.Version).
		Str("env", cfg.Env).
		Logger()
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
