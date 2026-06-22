package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

// TestBusinessGaugesRestoreFromReadersAfterRestart проверяет восстановление показателей из источников PostgreSQL
func TestBusinessGaugesRestoreFromReadersAfterRestart(t *testing.T) {
	outboxReader := staticOutboxStatsReader{pendingCount: 7, oldestAgeSeconds: 42}
	avatarReader := staticAvatarStatsReader{
		countByStatus:        map[avatar.Status]int64{avatar.StatusReady: 3, avatar.StatusFailed: 1},
		originalStorageBytes: 4096,
	}

	first := collectBusinessExposition(t, outboxReader, avatarReader)
	second := collectBusinessExposition(t, outboxReader, avatarReader)
	for _, exposition := range []string{first, second} {
		assertOperationalSeries(t, exposition, "app_outbox_pending_count", nil, "7")
		assertOperationalSeries(t, exposition, "app_outbox_oldest_age_seconds", nil, "42")
		assertOperationalSeries(t, exposition, "app_avatar_count", []string{`status="ready"`}, "3")
		assertOperationalSeries(t, exposition, "app_avatar_count", []string{`status="failed"`}, "1")
		assertOperationalSeries(t, exposition, "app_avatar_count", []string{`status="processing"`}, "0")
		assertOperationalSeries(t, exposition, "app_avatar_original_storage_bytes", nil, "4096")
		for _, forbidden := range []string{"550e8400-e29b-41d4-a716-446655440000", "user@example.com", "private.png", "secret-object-key"} {
			if strings.Contains(exposition, forbidden) {
				t.Fatalf("operational metrics contain %q", forbidden)
			}
		}
	}
}

// staticOutboxStatsReader возвращает неизменяемое состояние outbox
type staticOutboxStatsReader struct {
	pendingCount     int64
	oldestAgeSeconds float64
}

// ReadOutboxOperationalStats возвращает настроенное состояние outbox
func (r staticOutboxStatsReader) ReadOutboxOperationalStats(context.Context) (int64, float64, error) {
	return r.pendingCount, r.oldestAgeSeconds, nil
}

// staticAvatarStatsReader возвращает неизменяемое состояние аватаров
type staticAvatarStatsReader struct {
	countByStatus        map[avatar.Status]int64
	originalStorageBytes int64
}

// ReadAvatarOperationalStats возвращает настроенное состояние аватаров
func (r staticAvatarStatsReader) ReadAvatarOperationalStats(context.Context) (map[avatar.Status]int64, int64, error) {
	return r.countByStatus, r.originalStorageBytes, nil
}

// collectBusinessExposition создаёт новый провайдер и собирает показатели состояния
func collectBusinessExposition(t *testing.T, outboxReader OutboxOperationalStatsReader, avatarReader AvatarOperationalStatsReader) string {
	t.Helper()
	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		t.Fatalf("create Prometheus exporter: %v", err)
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	telemetry := &Telemetry{MeterProvider: provider}
	if err := telemetry.RegisterBusinessMetrics(outboxReader, avatarReader); err != nil {
		t.Fatalf("RegisterBusinessMetrics() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err := telemetry.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	return recorder.Body.String()
}

// assertOperationalSeries проверяет значение показателя состояния с заданными метками
func assertOperationalSeries(t *testing.T, exposition string, name string, labels []string, value string) {
	t.Helper()
	for _, line := range strings.Split(exposition, "\n") {
		if !strings.HasPrefix(line, name) || !strings.HasSuffix(line, " "+value) {
			continue
		}
		matched := true
		for _, label := range labels {
			if !strings.Contains(line, label) {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Fatalf("metric %s labels=%v value=%s is missing:\n%s", name, labels, value, exposition)
}
