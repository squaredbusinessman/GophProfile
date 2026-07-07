package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/config"
	"go.opentelemetry.io/otel/trace"
)

// TestLoggerWritesStructuredJSONWithCorrectLevels проверяет JSON и уровни событий
func TestLoggerWritesStructuredJSONWithCorrectLevels(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(loggerTestConfig("debug", "json", "local"), &output)
	logger.Info().Msg("info event")
	logger.Warn().Msg("warn event")
	logger.Error().Msg("error event")

	decoder := json.NewDecoder(&output)
	wantLevels := []string{"info", "warn", "error"}
	for _, wantLevel := range wantLevels {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			t.Fatalf("не удалось разобрать JSON log: %v", err)
		}
		if event["level"] != wantLevel {
			t.Fatalf("level = %v, ожидался %s", event["level"], wantLevel)
		}
	}
}

// TestLoggerFallsBackToInfoLevel проверяет безопасный уровень при некорректной настройке
func TestLoggerFallsBackToInfoLevel(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(loggerTestConfig("invalid", "json", "local"), &output)
	logger.Debug().Msg("debug event")
	logger.Info().Msg("info event")

	var event map[string]any
	if err := json.NewDecoder(&output).Decode(&event); err != nil {
		t.Fatalf("не удалось разобрать JSON log: %v", err)
	}
	if event["level"] != "info" || strings.Contains(output.String(), "debug event") {
		t.Fatalf("небезопасный fallback уровня: %s", output.String())
	}
}

// TestConsoleFormatIsAllowedOnlyForLocal проверяет ограничение текстового формата окружением local
func TestConsoleFormatIsAllowedOnlyForLocal(t *testing.T) {
	var productionOutput bytes.Buffer
	productionLogger := newLogger(loggerTestConfig("info", "console", "production"), &productionOutput)
	productionLogger.Info().Msg("production event")
	if !json.Valid(productionOutput.Bytes()) {
		t.Fatalf("production log должен быть JSON: %s", productionOutput.String())
	}

	var localOutput bytes.Buffer
	localLogger := newLogger(loggerTestConfig("info", "console", "local"), &localOutput)
	localLogger.Info().Msg("local event")
	if json.Valid(localOutput.Bytes()) || !strings.Contains(localOutput.String(), "local event") {
		t.Fatalf("local console log имеет неверный формат: %s", localOutput.String())
	}
}

// TestLoggerFromContextAddsTraceCorrelation проверяет идентификаторы активного span
func TestLoggerFromContextAddsTraceCorrelation(t *testing.T) {
	var output bytes.Buffer
	logger := zerolog.New(&output)
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := ContextWithLogger(context.Background(), logger)
	ctx = trace.ContextWithSpanContext(ctx, spanContext)
	LoggerFromContext(ctx).Info().Msg("correlated event")

	var event map[string]any
	if err := json.NewDecoder(&output).Decode(&event); err != nil {
		t.Fatalf("не удалось разобрать JSON log: %v", err)
	}
	if event["trace_id"] != spanContext.TraceID().String() || event["span_id"] != spanContext.SpanID().String() {
		t.Fatalf("неверные поля корреляции: %#v", event)
	}
}

// TestLoggerFromContextOmitsInvalidCorrelation проверяет отсутствие пустых полей корреляции
func TestLoggerFromContextOmitsInvalidCorrelation(t *testing.T) {
	var output bytes.Buffer
	ctx := ContextWithLogger(context.Background(), zerolog.New(&output))
	LoggerFromContext(ctx).Info().Msg("uncorrelated event")

	var event map[string]any
	if err := json.NewDecoder(&output).Decode(&event); err != nil {
		t.Fatalf("не удалось разобрать JSON log: %v", err)
	}
	if _, ok := event["trace_id"]; ok {
		t.Fatalf("trace_id не должен присутствовать: %#v", event)
	}
	if _, ok := event["span_id"]; ok {
		t.Fatalf("span_id не должен присутствовать: %#v", event)
	}
}

// TestErrorTypeDoesNotExposeSecret проверяет отсутствие текста ошибки в безопасном поле
func TestErrorTypeDoesNotExposeSecret(t *testing.T) {
	secret := "postgres://user:password@db:5432/gophprofile"
	errorType := ErrorType(errors.New(secret))
	if strings.Contains(errorType, secret) || strings.Contains(errorType, "password") {
		t.Fatalf("тип ошибки содержит секрет: %q", errorType)
	}
}

// loggerTestConfig создаёт конфигурацию logger для тестов
func loggerTestConfig(level string, format string, env string) config.Config {
	return config.Config{
		ServiceName: "test-service",
		Version:     "test-version",
		Env:         env,
		Observability: config.ObservabilityConfig{
			LogLevel:  level,
			LogFormat: format,
		},
	}
}
