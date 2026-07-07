package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/squaredbusinessman/GophProfile/internal/config"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestDisabledTelemetryUsesNoopProviders проверяет неактивные провайдеры при отключённой телеметрии
func TestDisabledTelemetryUsesNoopProviders(t *testing.T) {
	cfg := testConfig(false)
	telemetry, err := NewTelemetry(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewTelemetry() error = %v", err)
	}
	t.Cleanup(func() { _ = telemetry.Shutdown(context.Background()) })

	_, span := telemetry.TracerProvider.Tracer("test").Start(context.Background(), "disabled")
	if span.IsRecording() {
		t.Fatal("disabled tracer recorded a span")
	}
	if telemetry.MeterProvider == nil || telemetry.MetricsHandler == nil {
		t.Fatal("disabled telemetry returned nil providers or handler")
	}
}

// TestResourceContainsServiceAttributes проверяет обязательные атрибуты ресурса
func TestResourceContainsServiceAttributes(t *testing.T) {
	cfg := testConfig(false)
	telemetry, err := NewTelemetry(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewTelemetry() error = %v", err)
	}
	t.Cleanup(func() { _ = telemetry.Shutdown(context.Background()) })

	attrs := map[string]string{}
	for _, item := range telemetry.Resource.Attributes() {
		attrs[string(item.Key)] = item.Value.AsString()
	}
	for key, want := range map[string]string{
		"service.name":           "test-service",
		"service.version":        "1.2.3",
		"deployment.environment": "test",
	} {
		if attrs[key] != want {
			t.Errorf("resource %s = %q, want %q", key, attrs[key], want)
		}
	}
	if attrs["service.instance.id"] == "" {
		t.Error("resource service.instance.id is empty")
	}
}

// TestMetricsHandlerServesPrometheusFormat проверяет выдачу метрик в формате Prometheus
func TestMetricsHandlerServesPrometheusFormat(t *testing.T) {
	cfg := testConfig(true)
	telemetry, err := NewTelemetry(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewTelemetry() error = %v", err)
	}
	t.Cleanup(func() { _ = telemetry.Shutdown(context.Background()) })

	counter, err := telemetry.MeterProvider.Meter("test").Int64Counter("stage_one_requests")
	if err != nil {
		t.Fatalf("Int64Counter() error = %v", err)
	}
	counter.Add(context.Background(), 3, otelmetric.WithAttributes(attribute.String("route", "test")))

	recorder := httptest.NewRecorder()
	telemetry.MetricsHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Header().Get("Content-Type"), "text/plain") || !strings.Contains(recorder.Body.String(), "stage_one_requests_total") {
		t.Fatalf("unexpected Prometheus response: content-type=%q body=%q", recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "go_goroutines") {
		t.Fatalf("Prometheus response does not contain Go runtime collector: body=%q", recorder.Body.String())
	}
}

// TestDBPoolMetricsAppearInPrometheus проверяет экспорт состояния database/sql pool
func TestDBPoolMetricsAppearInPrometheus(t *testing.T) {
	cfg := testConfig(true)
	telemetry, err := NewTelemetry(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewTelemetry() error = %v", err)
	}
	t.Cleanup(func() { _ = telemetry.Shutdown(context.Background()) })

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(7)
	if err := telemetry.RegisterDBPool(db, "postgres"); err != nil {
		t.Fatalf("RegisterDBPool() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	telemetry.MetricsHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	for _, metricName := range []string{
		"db_client_connection_count",
		"db_client_connection_max",
		"db_client_connection_wait_count",
		"db_client_connection_wait_duration_seconds",
	} {
		if !strings.Contains(body, metricName) {
			t.Errorf("metrics body does not contain %q", metricName)
		}
	}
	if !strings.Contains(body, `db_client_connection_pool_name="postgres"`) ||
		!strings.Contains(body, `db_client_connection_state="idle"`) ||
		!strings.Contains(body, `db_client_connection_state="used"`) {
		t.Fatalf("metrics body does not contain pool labels: %s", body)
	}
}

// TestShutdownFlushesWithoutHanging проверяет отправку накопленных интервалов трассировки при остановке
func TestShutdownFlushesWithoutHanging(t *testing.T) {
	exporter := &countingExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
	telemetry := &Telemetry{TracerProvider: tp}
	_, span := tp.Tracer("test").Start(context.Background(), "buffered")
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := telemetry.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if exporter.exported.Load() == 0 {
		t.Fatal("Shutdown() did not export buffered spans")
	}
}

// TestEndpointURLDetection проверяет различение URL и адреса OTLP gRPC
func TestEndpointURLDetection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		endpoint string
		want     bool
	}{
		{name: "host and port", endpoint: "jaeger:4317", want: false},
		{name: "HTTP URL", endpoint: "http://jaeger:4317", want: true},
		{name: "HTTPS URL", endpoint: "https://collector.example.com:4317", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEndpointURL(tc.endpoint); got != tc.want {
				t.Fatalf("isEndpointURL(%q) = %t, want %t", tc.endpoint, got, tc.want)
			}
		})
	}
}

// countingExporter подсчитывает экспортированные интервалы трассировки
type countingExporter struct {
	// exported хранит количество экспортированных интервалов трассировки
	exported atomic.Int64
}

// ExportSpans учитывает количество переданных на экспорт интервалов трассировки
func (e *countingExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.exported.Add(int64(len(spans)))
	return nil
}

// Shutdown завершает тестовый экспортёр
func (*countingExporter) Shutdown(context.Context) error { return nil }

// testConfig создаёт минимальную конфигурацию для тестов телеметрии
func testConfig(enabled bool) config.Config {
	return config.Config{
		Version: "1.2.3",
		Env:     "test",
		Observability: config.ObservabilityConfig{
			Enabled:          enabled,
			ServiceName:      "test-service",
			OTLPEndpoint:     "localhost:4317",
			OTLPInsecure:     true,
			TracesSampler:    "always_on",
			TracesSamplerArg: 1,
		},
	}
}
