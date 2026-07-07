package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestRepositoryDBErrorMarksSpanWithoutSecrets проверяет безопасную запись ошибки PostgreSQL
func TestRepositoryDBErrorMarksSpanWithoutSecrets(t *testing.T) {
	recorder := installPostgresSpanRecorder(t)
	db, mock := newMockDB(t)
	repo := newUserRepositoryForTest(t, db)
	const email = "secret@example.com"
	const dsn = "postgres://secret:password@database:5432/gophprofile"

	mock.ExpectQuery("FROM users").
		WithArgs(email).
		WillReturnError(errors.New("connect " + dsn))

	_, err := repo.GetUserByEmail(context.Background(), email)
	if err == nil {
		t.Fatal("GetUserByEmail() error = nil")
	}
	assertExpectations(t, mock)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Status().Code != codes.Error {
		t.Fatalf("span status = %v, want Error", span.Status().Code)
	}
	if span.SpanKind() != trace.SpanKindClient {
		t.Fatalf("span kind = %v, want Client", span.SpanKind())
	}

	for _, attr := range span.Attributes() {
		value := attr.Value.String()
		if strings.Contains(value, email) || strings.Contains(value, dsn) || strings.Contains(value, "password") {
			t.Fatalf("unsafe span attribute %s=%q", attr.Key, value)
		}
	}
	if len(span.Events()) != 0 {
		t.Fatalf("span events = %d, error text must not be recorded", len(span.Events()))
	}
}

// installPostgresSpanRecorder устанавливает тестовый global TracerProvider
func installPostgresSpanRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	previous := otel.GetTracerProvider()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})
	return recorder
}

// eventNames возвращает множество имён событий span
func eventNames(events []sdktrace.Event) map[string]bool {
	names := make(map[string]bool, len(events))
	for _, event := range events {
		names[event.Name] = true
	}
	return names
}
