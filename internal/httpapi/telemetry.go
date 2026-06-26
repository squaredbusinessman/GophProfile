package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// instrumentationName задаёт имя области инструментирования HTTP API
const instrumentationName = "github.com/squaredbusinessman/GophProfile/internal/httpapi"

// httpServerTelemetry содержит инструменты трассировки и RED-метрик HTTP-сервера
type httpServerTelemetry struct {
	// requests считает завершённые HTTP-запросы
	requests metric.Int64Counter
	// activeRequests считает выполняющиеся HTTP-запросы
	activeRequests metric.Int64UpDownCounter
}

// newHTTPServerTelemetry создаёт инструменты HTTP-телеметрии из global providers
func newHTTPServerTelemetry(meter metric.Meter) (httpServerTelemetry, error) {
	requests, err := meter.Int64Counter(
		"http.server.request.count",
		metric.WithDescription("Количество завершённых HTTP-запросов"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return httpServerTelemetry{}, fmt.Errorf("create HTTP request counter: %w", err)
	}
	activeRequests, err := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithDescription("Количество выполняющихся HTTP-запросов"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return httpServerTelemetry{}, fmt.Errorf("create HTTP active requests counter: %w", err)
	}
	return httpServerTelemetry{
		requests:       requests,
		activeRequests: activeRequests,
	}, nil
}

// serveObserved обрабатывает запрос внутри серверного span и записывает RED-метрики
func (r *Router) serveObserved(w http.ResponseWriter, req *http.Request, route string) {
	ctx := req.Context()
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(semconv.HTTPRoute(route))
	recorder := newStatusRecorder(w)
	activeAttributes := metric.WithAttributes(
		semconv.HTTPRequestMethodKey.String(req.Method),
		semconv.HTTPRoute(route),
	)
	r.telemetry.activeRequests.Add(ctx, 1, activeAttributes)

	defer func() {
		panicValue := recover()
		statusCode := recorder.statusCode
		if panicValue != nil {
			statusCode = http.StatusInternalServerError
			span.SetStatus(codes.Error, http.StatusText(statusCode))
			span.AddEvent("panic")
		} else if statusCode >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(statusCode))
		}

		span.SetAttributes(
			semconv.HTTPResponseStatusCode(statusCode),
			semconv.HTTPResponseBodySize(recorder.bytesWritten),
		)
		completedAttributes := metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(req.Method),
			semconv.HTTPRoute(route),
			semconv.HTTPResponseStatusCode(statusCode),
			attribute.String("status_class", httpStatusClass(statusCode)),
		)
		r.telemetry.requests.Add(ctx, 1, completedAttributes)
		r.telemetry.activeRequests.Add(ctx, -1, activeAttributes)

		if panicValue != nil {
			panic(panicValue)
		}
	}()

	r.serveRequest(recorder, req)
}

// httpStatusClass возвращает класс HTTP-ответа для правил и панелей
func httpStatusClass(statusCode int) string {
	if statusCode < 100 || statusCode > 599 {
		return "unknown"
	}
	return strconv.Itoa(statusCode/100) + "xx"
}

// shouldObserveHTTP исключает технические маршруты из tracing и RED-метрик
func shouldObserveHTTP(path string) bool {
	return path != "/health" && path != "/metrics"
}

// normalizedHTTPRoute возвращает шаблон маршрута с ограниченной кардинальностью
func normalizedHTTPRoute(path string) string {
	trimmedPath := strings.TrimSuffix(path, "/")
	if trimmedPath == "" {
		return "/"
	}

	switch trimmedPath {
	case "/health", "/metrics", "/api/v1/avatar", "/api/v1/avatars", "/api/v1/users/resolve":
		return trimmedPath
	}

	segments := strings.Split(strings.TrimPrefix(trimmedPath, "/"), "/")
	if len(segments) == 4 && segments[0] == "api" && segments[1] == "v1" && segments[2] == "avatars" {
		return "/api/v1/avatars/{avatar_id}"
	}
	if len(segments) == 5 && segments[0] == "api" && segments[1] == "v1" && segments[2] == "avatars" && segments[4] == "metadata" {
		return "/api/v1/avatars/{avatar_id}/metadata"
	}
	if len(segments) == 5 && segments[0] == "api" && segments[1] == "v1" && segments[2] == "users" {
		switch segments[4] {
		case "avatar":
			return "/api/v1/users/{user_id}/avatar"
		case "avatars":
			return "/api/v1/users/{user_id}/avatars"
		}
	}
	if strings.HasPrefix(trimmedPath, "/api/") {
		return "/api/{unmatched}"
	}
	return "/{unmatched}"
}
