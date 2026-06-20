package s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const secretObjectKey = "avatars/private-user/private-avatar/original"

// TestFakeAdapterCreatesSpansForEveryOperation проверяет spans всех S3 operations
func TestFakeAdapterCreatesSpansForEveryOperation(t *testing.T) {
	recorder, _ := installS3TestProviders(t)
	body := &countingReadCloser{Reader: strings.NewReader("image")}
	api := &fakeObjectStorageAPI{
		getBody:      body,
		statMetadata: ObjectMetadata{Size: 5, ContentType: "image/png"},
		bucketExists: true,
	}
	client := newClientWithRegion("avatars", "us-east-1", api)

	if err := client.Put(context.Background(), secretObjectKey, strings.NewReader("image"), 5, "image/png"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	object, _, err := client.Get(context.Background(), secretObjectKey)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if body.reads != 0 {
		t.Fatalf("body reads before caller read = %d, want 0", body.reads)
	}
	_ = object.Close()
	if err := client.Delete(context.Background(), secretObjectKey); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := client.Exists(context.Background(), secretObjectKey); err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if err := client.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket() error = %v", err)
	}

	want := map[string]bool{
		"S3 Put":          false,
		"S3 Stat":         false,
		"S3 Get":          false,
		"S3 Delete":       false,
		"S3 Exists":       false,
		"S3 EnsureBucket": false,
	}
	for _, span := range recorder.Ended() {
		if _, ok := want[span.Name()]; ok {
			want[span.Name()] = true
		}
		assertSpanDoesNotContainKey(t, span, secretObjectKey)
	}
	for name, found := range want {
		if !found {
			t.Errorf("span %q was not recorded", name)
		}
	}
}

// TestNotFoundHasExpectedResults проверяет ожидаемые результаты отсутствующего объекта
func TestNotFoundHasExpectedResults(t *testing.T) {
	recorder, metricsHandler := installS3TestProviders(t)
	notFound := minio.ErrorResponse{Code: "NoSuchKey", StatusCode: http.StatusNotFound}
	client := newClientWithRegion("avatars", "", &fakeObjectStorageAPI{
		statErr:   notFound,
		removeErr: notFound,
	})

	exists, err := client.Exists(context.Background(), secretObjectKey)
	if err != nil || exists {
		t.Fatalf("Exists() = %t, %v, want false, nil", exists, err)
	}
	if err := client.Delete(context.Background(), secretObjectKey); err != nil {
		t.Fatalf("Delete() error = %v, want idempotent success", err)
	}

	results := map[string]string{}
	for _, span := range recorder.Ended() {
		for _, attr := range span.Attributes() {
			if attr.Key == s3OperationResultKey {
				results[span.Name()] = attr.Value.AsString()
			}
		}
		if span.Status().Code == codes.Error {
			t.Errorf("expected result span %q has error status", span.Name())
		}
	}
	if results["S3 Exists"] != s3ResultNotFound || results["S3 Delete"] != s3ResultSuccess {
		t.Fatalf("operation results = %v", results)
	}

	body := scrapeMetrics(t, metricsHandler)
	if !strings.Contains(body, `s3_operation_name="Exists",s3_operation_result="not_found"`) ||
		!strings.Contains(body, `s3_operation_name="Delete",s3_operation_result="success"`) {
		t.Fatalf("unexpected S3 result metrics: %s", body)
	}
}

// TestS3ErrorIncrementsErrorCounter проверяет error span и operation counter
func TestS3ErrorIncrementsErrorCounter(t *testing.T) {
	recorder, metricsHandler := installS3TestProviders(t)
	client := newClientWithRegion("avatars", "", &fakeObjectStorageAPI{putErr: errors.New("storage unavailable")})

	err := client.Put(context.Background(), secretObjectKey, strings.NewReader("image"), 5, "image/jpeg")
	if err == nil {
		t.Fatal("Put() error = nil")
	}
	if strings.Contains(err.Error(), secretObjectKey) {
		t.Fatalf("error contains object key: %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Status().Code != codes.Error {
		t.Fatalf("unexpected error spans: %#v", spans)
	}

	body := scrapeMetrics(t, metricsHandler)
	if !strings.Contains(body, `s3_operation_name="Put",s3_operation_result="error"`) {
		t.Fatalf("error counter is missing: %s", body)
	}
	if strings.Contains(body, secretObjectKey) {
		t.Fatalf("metrics contain object key: %s", body)
	}
}

// installS3TestProviders устанавливает тестовые trace и metric providers
func installS3TestProviders(t *testing.T) (*tracetest.SpanRecorder, http.Handler) {
	t.Helper()
	previousTracer := otel.GetTracerProvider()
	previousMeter := otel.GetMeterProvider()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	registry := prometheus.NewRegistry()
	prometheusExporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		t.Fatalf("create Prometheus exporter: %v", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(prometheusExporter))
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		_ = tracerProvider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
		otel.SetTracerProvider(previousTracer)
		otel.SetMeterProvider(previousMeter)
	})
	return spanRecorder, promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// scrapeMetrics возвращает Prometheus exposition для проверки
func scrapeMetrics(t *testing.T, handler http.Handler) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", recorder.Code)
	}
	return recorder.Body.String()
}

// assertSpanDoesNotContainKey проверяет отсутствие object key в span attributes и events
func assertSpanDoesNotContainKey(t *testing.T, span sdktrace.ReadOnlySpan, key string) {
	t.Helper()
	for _, attr := range span.Attributes() {
		if strings.Contains(attr.Value.String(), key) {
			t.Fatalf("span %q contains object key in %s", span.Name(), attr.Key)
		}
	}
	for _, event := range span.Events() {
		for _, attr := range event.Attributes {
			if strings.Contains(attr.Value.String(), key) {
				t.Fatalf("span event %q contains object key in %s", event.Name, attr.Key)
			}
		}
	}
}

// countingReadCloser считает чтения body независимо от Close
type countingReadCloser struct {
	io.Reader
	reads int
}

// Read учитывает чтение body вызывающим кодом
func (r *countingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

// Close завершает fake body
func (*countingReadCloser) Close() error { return nil }
