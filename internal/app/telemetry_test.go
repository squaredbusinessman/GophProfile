package app

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
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

// TestUploadMetricsTreatPersistedOutboxAsAccepted проверяет независимость загрузки от немедленной публикации
func TestUploadMetricsTreatPersistedOutboxAsAccepted(t *testing.T) {
	handler := installBusinessMetricProvider(t)
	publisher := &fakeEventPublisher{publishErr: errors.New("kafka unavailable")}
	service := NewAvatarUploadService(
		&fakeUserLookup{item: user.User{ID: uploadTestUserID}},
		&fakeAvatarOutboxStore{},
		&fakeObjectStore{},
		publisher,
	)

	_, err := service.UploadAvatar(context.Background(), AvatarUploadRequest{
		UserID: uploadTestUserID, FileName: "private-name.png", ContentType: "image/png", Reader: bytes.NewBufferString("body"),
	})
	if err != nil {
		t.Fatalf("UploadAvatar() error = %v", err)
	}

	exposition := scrapeBusinessMetrics(t, handler)
	assertMetricValue(t, exposition, `app_avatar_upload_count_total{result="accepted"}`, "1")
	assertMetricValue(t, exposition, `app_avatar_upload_count_total{result="error"}`, "0")
	assertMetricValue(t, exposition, `app_outbox_publish_count_total{mode="immediate",result="error"}`, "1")
	assertNoSensitiveMetricData(t, exposition)
}

// TestProcessingRetryDoesNotIncrementUpload проверяет независимый учёт повторной обработки
func TestProcessingRetryDoesNotIncrementUpload(t *testing.T) {
	handler := installBusinessMetricProvider(t)
	service := NewAvatarProcessService(
		&fakeAvatarMetadataStore{getErr: errors.New("temporary database error")},
		&fakeAvatarObjectStore{},
		&fakeEventPublisher{},
	)

	err := service.HandleProcessMessage(context.Background(), []byte(`{"avatar_id":"550e8400-e29b-41d4-a716-446655440000","attempt":1}`))
	if err != nil {
		t.Fatalf("HandleProcessMessage() error = %v", err)
	}

	exposition := scrapeBusinessMetrics(t, handler)
	assertMetricValue(t, exposition, `app_avatar_processing_count_total{result="retry_scheduled"}`, "1")
	assertMetricValue(t, exposition, `app_avatar_upload_count_total{result="accepted"}`, "0")
	assertNoSensitiveMetricData(t, exposition)
}

// TestDeleteMetricsRecordIdempotentSkip проверяет отдельный результат повторного удаления
func TestDeleteMetricsRecordIdempotentSkip(t *testing.T) {
	handler := installBusinessMetricProvider(t)
	service := NewAvatarDeleteWorkerService(
		&fakeAvatarDeleteRepository{item: avatar.Avatar{ID: "550e8400-e29b-41d4-a716-446655440000", Status: avatar.StatusDeleted}},
		&fakeAvatarDeleteObjectStore{},
	)

	err := service.HandleDeleteMessage(context.Background(), []byte(`{"avatar_id":"550e8400-e29b-41d4-a716-446655440000"}`))
	if err != nil {
		t.Fatalf("HandleDeleteMessage() error = %v", err)
	}

	exposition := scrapeBusinessMetrics(t, handler)
	assertMetricValue(t, exposition, `app_avatar_delete_count_total{phase="execute",result="idempotent_skip"}`, "1")
	assertMetricValue(t, exposition, `app_avatar_delete_count_total{phase="execute",result="completed"}`, "0")
	assertNoSensitiveMetricData(t, exposition)
}

// TestBusinessMetricsRecordSuccessAndErrorBranches проверяет основные результаты бизнес-операций
func TestBusinessMetricsRecordSuccessAndErrorBranches(t *testing.T) {
	handler := installBusinessMetricProvider(t)
	upload := NewAvatarUploadService(
		&fakeUserLookup{item: user.User{ID: uploadTestUserID}},
		&fakeAvatarOutboxStore{},
		&fakeObjectStore{putErr: errors.New("storage unavailable")},
		&fakeEventPublisher{},
	)
	_, _ = upload.UploadAvatar(context.Background(), AvatarUploadRequest{
		UserID: uploadTestUserID, ContentType: "image/png", Reader: bytes.NewBufferString("body"),
	})

	ready := NewAvatarProcessService(
		&fakeAvatarMetadataStore{item: avatar.Avatar{ID: "ready-avatar", UserID: "user", Status: avatar.StatusProcessing, OriginalObjectKey: "original"}},
		&fakeAvatarObjectStore{getBody: processTestPNG(t, 2, 2)},
		&fakeEventPublisher{},
	)
	if err := ready.HandleProcessMessage(context.Background(), []byte(`{"avatar_id":"ready-avatar","attempt":1}`)); err != nil {
		t.Fatalf("ready HandleProcessMessage() error = %v", err)
	}

	failed := NewAvatarProcessService(
		&fakeAvatarMetadataStore{item: avatar.Avatar{ID: "failed-avatar", UserID: "user", Status: avatar.StatusProcessing, OriginalObjectKey: "original"}},
		&fakeAvatarObjectStore{getBody: []byte("invalid image")},
		&fakeEventPublisher{},
	)
	if err := failed.HandleProcessMessage(context.Background(), []byte(`{"avatar_id":"failed-avatar","attempt":1}`)); err != nil {
		t.Fatalf("failed HandleProcessMessage() error = %v", err)
	}

	deleteService := NewAvatarDeleteWorkerService(
		&fakeAvatarDeleteRepository{item: avatar.Avatar{ID: "delete-avatar", Status: avatar.StatusDeleting, OriginalObjectKey: "original"}},
		&fakeAvatarDeleteObjectStore{},
	)
	if err := deleteService.HandleDeleteMessage(context.Background(), []byte(`{"avatar_id":"delete-avatar"}`)); err != nil {
		t.Fatalf("HandleDeleteMessage() error = %v", err)
	}

	exposition := scrapeBusinessMetrics(t, handler)
	assertMetricValue(t, exposition, `app_avatar_upload_count_total{result="error"}`, "1")
	assertMetricValue(t, exposition, `app_avatar_processing_count_total{result="ready"}`, "1")
	assertMetricValue(t, exposition, `app_avatar_processing_count_total{result="failed"}`, "1")
	assertMetricValue(t, exposition, `app_avatar_delete_count_total{phase="execute",result="completed"}`, "1")
}

// installBusinessMetricProvider устанавливает изолированный Prometheus exporter
func installBusinessMetricProvider(t *testing.T) http.Handler {
	t.Helper()
	previous := otel.GetMeterProvider()
	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		t.Fatalf("create Prometheus exporter: %v", err)
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(previous)
	})
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// scrapeBusinessMetrics возвращает exposition текущего тестового провайдера
func scrapeBusinessMetrics(t *testing.T, handler http.Handler) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", recorder.Code)
	}
	return recorder.Body.String()
}

// assertMetricValue проверяет значение временного ряда Prometheus
func assertMetricValue(t *testing.T, exposition string, series string, value string) {
	t.Helper()
	brace := strings.IndexByte(series, '{')
	metricName := series[:brace]
	labels := strings.Split(strings.TrimSuffix(series[brace+1:], "}"), ",")
	for _, line := range strings.Split(exposition, "\n") {
		if !strings.HasPrefix(line, metricName+"{") || !strings.HasSuffix(line, " "+value) {
			continue
		}
		matches := true
		for _, label := range labels {
			if !strings.Contains(line, label) {
				matches = false
				break
			}
		}
		if matches {
			return
		}
	}
	t.Fatalf("metric %s = %s is missing:\n%s", series, value, exposition)
}

// assertNoSensitiveMetricData проверяет отсутствие высококардинальных данных
func assertNoSensitiveMetricData(t *testing.T, exposition string) {
	t.Helper()
	for _, forbidden := range []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"private-name.png",
		"user@example.com",
		"original/secret-object-key",
	} {
		if strings.Contains(exposition, forbidden) {
			t.Fatalf("metrics contain sensitive value %q", forbidden)
		}
	}
}
