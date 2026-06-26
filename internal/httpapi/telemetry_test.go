package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/app"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestHTTPInstrumentationCreatesOneServerSpan проверяет один корневой span и correlation access log
func TestHTTPInstrumentationCreatesOneServerSpan(t *testing.T) {
	spanRecorder, cleanup := installHTTPTestProviders(t)
	defer cleanup()
	var logs bytes.Buffer
	resolver := &tracedUserResolver{}
	handler := newRouterForTest(t, RouterConfig{
		Logger:       zerolog.New(&logs),
		UserResolver: resolver,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/resolve", strings.NewReader(`{"email":"user@example.com"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("создано spans = %d, ожидался один", len(spans))
	}
	span := spans[0]
	if span.SpanKind() != trace.SpanKindServer || span.Name() != "POST /api/v1/users/resolve" {
		t.Fatalf("неверный server span: kind=%s name=%q", span.SpanKind(), span.Name())
	}
	if !resolver.spanContext.IsValid() || resolver.spanContext.TraceID() != span.SpanContext().TraceID() {
		t.Fatalf("handler не получил traced context: %#v", resolver.spanContext)
	}
	if !strings.Contains(logs.String(), `"trace_id":"`+span.SpanContext().TraceID().String()+`"`) {
		t.Fatalf("access log не содержит trace_id: %s", logs.String())
	}
}

// TestHTTPInstrumentationMarksServerError проверяет error status для ответа 5xx
func TestHTTPInstrumentationMarksServerError(t *testing.T) {
	spanRecorder, cleanup := installHTTPTestProviders(t)
	defer cleanup()
	handler := newRouterForTest(t, RouterConfig{
		Logger:       zerolog.Nop(),
		UserResolver: &tracedUserResolver{err: errors.New("database unavailable")},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/resolve", strings.NewReader(`{"email":"user@example.com"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, ожидался 500", rec.Code)
	}
	spans := spanRecorder.Ended()
	if len(spans) != 1 || spans[0].Status().Code != codes.Error {
		t.Fatalf("5xx span не отмечен как error: %#v", spans)
	}
	attributes := spanAttributes(spans[0])
	if attributes["http.response.status_code"] != int64(http.StatusInternalServerError) {
		t.Fatalf("span status attribute = %v, ожидался 500", attributes["http.response.status_code"])
	}
	if size, ok := attributes["http.response.body.size"].(int64); !ok || size <= 0 {
		t.Fatalf("span response size attribute = %v, ожидался положительный размер", attributes["http.response.body.size"])
	}
}

// TestHTTPInstrumentationKeepsClientErrorUnset проверяет корректный результат 400
func TestHTTPInstrumentationKeepsClientErrorUnset(t *testing.T) {
	spanRecorder, cleanup := installHTTPTestProviders(t)
	defer cleanup()
	handler := newRouterForTest(t, RouterConfig{
		Logger:       zerolog.Nop(),
		UserResolver: &tracedUserResolver{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/resolve", strings.NewReader(`{"unknown":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, ожидался 400", rec.Code)
	}
	spans := spanRecorder.Ended()
	if len(spans) != 1 || spans[0].Status().Code == codes.Error {
		t.Fatalf("400 ошибочно отмечен как server error: %#v", spans)
	}
}

// TestHTTPMetricsUseNormalizedRoute проверяет bounded route labels в Prometheus exposition
func TestHTTPMetricsUseNormalizedRoute(t *testing.T) {
	const avatarID = "550e8400-e29b-41d4-a716-446655440000"
	spanRecorder, metricsHandler, cleanup := installHTTPPrometheusProviders(t)
	defer cleanup()
	handler := newRouterForTest(t, RouterConfig{
		Logger:       zerolog.Nop(),
		AvatarReader: &fakeAvatarReader{err: app.ErrAvatarNotFound},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+avatarID, nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	metricsRecorder := httptest.NewRecorder()
	metricsHandler.ServeHTTP(metricsRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	exposition := metricsRecorder.Body.String()
	if !strings.Contains(exposition, `http_route="/api/v1/avatars/{avatar_id}"`) {
		t.Fatalf("normalized route отсутствует в метриках: %s", exposition)
	}
	if strings.Contains(exposition, avatarID) {
		t.Fatalf("UUID попал в metric exposition: %s", exposition)
	}
	for _, metricName := range []string{
		"http_server_request_count",
		"http_server_request_duration",
		"http_server_active_requests",
	} {
		if !strings.Contains(exposition, metricName) {
			t.Fatalf("метрика %s отсутствует в exposition: %s", metricName, exposition)
		}
	}
	spans := spanRecorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "GET /api/v1/avatars/{avatar_id}" {
		t.Fatalf("span использует ненормализованный route: %#v", spans)
	}
}

// TestHealthIsExcludedFromHTTPObservability проверяет исключение probe-трафика
func TestHealthIsExcludedFromHTTPObservability(t *testing.T) {
	spanRecorder, metricsHandler, cleanup := installHTTPPrometheusProviders(t)
	defer cleanup()
	handler := newRouterForTest(t, RouterConfig{Logger: zerolog.Nop()})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	metricsRecorder := httptest.NewRecorder()
	metricsHandler.ServeHTTP(metricsRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if len(spanRecorder.Ended()) != 0 {
		t.Fatalf("health создал spans: %#v", spanRecorder.Ended())
	}
	if strings.Contains(metricsRecorder.Body.String(), `http_route="/health"`) {
		t.Fatalf("health попал в RED metrics: %s", metricsRecorder.Body.String())
	}
}

// TestHTTPInstrumentationAccountsPanicAsServerError проверяет учёт panic как 5xx без его подавления
func TestHTTPInstrumentationAccountsPanicAsServerError(t *testing.T) {
	spanRecorder, metricsHandler, cleanup := installHTTPPrometheusProviders(t)
	defer cleanup()
	handler := newRouterForTest(t, RouterConfig{
		Logger:       zerolog.Nop(),
		UserResolver: panicUserResolver{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/resolve", strings.NewReader(`{"email":"user@example.com"}`))

	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}()

	if panicValue == nil {
		t.Fatal("instrumentation подавила panic")
	}
	spans := spanRecorder.Ended()
	if len(spans) != 1 || spans[0].Status().Code != codes.Error {
		t.Fatalf("panic span не отмечен как error: %#v", spans)
	}
	metricsRecorder := httptest.NewRecorder()
	metricsHandler.ServeHTTP(metricsRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(metricsRecorder.Body.String(), `http_response_status_code="500"`) {
		t.Fatalf("panic не учтён как 500 в метриках: %s", metricsRecorder.Body.String())
	}
	if !strings.Contains(metricsRecorder.Body.String(), `status_class="5xx"`) {
		t.Fatalf("panic не учтён как 5xx в метриках: %s", metricsRecorder.Body.String())
	}
}

// TestNormalizedHTTPRoute проверяет шаблоны всех текущих маршрутов
func TestNormalizedHTTPRoute(t *testing.T) {
	tests := map[string]string{
		"/api/v1/avatar":  "/api/v1/avatar",
		"/api/v1/avatars": "/api/v1/avatars",
		"/api/v1/avatars/550e8400-e29b-41d4-a716-446655440000":      "/api/v1/avatars/{avatar_id}",
		"/api/v1/avatars/avatar-1/metadata":                         "/api/v1/avatars/{avatar_id}/metadata",
		"/api/v1/users/550e8400-e29b-41d4-a716-446655440000/avatar": "/api/v1/users/{user_id}/avatar",
		"/api/v1/users/user-1/avatars":                              "/api/v1/users/{user_id}/avatars",
		"/api/v1/users/resolve":                                     "/api/v1/users/resolve",
		"/api/v1/unknown/value":                                     "/api/{unmatched}",
	}
	for path, want := range tests {
		if got := normalizedHTTPRoute(path); got != want {
			t.Errorf("normalizedHTTPRoute(%q) = %q, ожидался %q", path, got, want)
		}
	}
}

// tracedUserResolver сохраняет span context полученный HTTP handler
type tracedUserResolver struct {
	// spanContext содержит контекст серверного span
	spanContext trace.SpanContext
	// err задаёт возвращаемую ошибку
	err error
}

// panicUserResolver имитирует panic внутри application handler
type panicUserResolver struct{}

// ResolveUserByEmail создаёт тестовый panic
func (panicUserResolver) ResolveUserByEmail(context.Context, string) (app.UserResolveResult, error) {
	panic("test panic")
}

// ResolveUserByEmail сохраняет span context и возвращает настроенный результат
func (r *tracedUserResolver) ResolveUserByEmail(ctx context.Context, email string) (app.UserResolveResult, error) {
	r.spanContext = trace.SpanContextFromContext(ctx)
	return app.UserResolveResult{Email: email}, r.err
}

// installHTTPTestProviders устанавливает тестовые tracing и metric providers
func installHTTPTestProviders(t *testing.T) (*tracetest.SpanRecorder, func()) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	cleanup := installGlobalProviders(t, tracerProvider, meterProvider)
	return spanRecorder, cleanup
}

// installHTTPPrometheusProviders устанавливает tracing и Prometheus providers
func installHTTPPrometheusProviders(t *testing.T) (*tracetest.SpanRecorder, http.Handler, func()) {
	t.Helper()
	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		t.Fatalf("не удалось создать Prometheus exporter: %v", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	cleanup := installGlobalProviders(t, tracerProvider, meterProvider)
	return spanRecorder, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}), cleanup
}

// installGlobalProviders заменяет global providers и возвращает функцию восстановления
func installGlobalProviders(t *testing.T, tracerProvider *sdktrace.TracerProvider, meterProvider *sdkmetric.MeterProvider) func() {
	t.Helper()
	previousTracerProvider := otel.GetTracerProvider()
	previousMeterProvider := otel.GetMeterProvider()
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	return func() {
		otel.SetTracerProvider(previousTracerProvider)
		otel.SetMeterProvider(previousMeterProvider)
		_ = tracerProvider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
	}
}

// spanAttributes преобразует атрибуты span в map для проверок
func spanAttributes(span sdktrace.ReadOnlySpan) map[string]any {
	attributes := make(map[string]any, len(span.Attributes()))
	for _, item := range span.Attributes() {
		attributes[string(item.Key)] = item.Value.AsInterface()
	}
	return attributes
}
