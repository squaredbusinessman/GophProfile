# ТЗ второго спринта GophProfile

Цель второго спринта - внедрить observability в работающий MVP GophProfile.

Сервис уже принимает запросы и выполняет бизнес-логику, но для эксплуатации в
production необходимо понимать причины замедлений, ошибок `5xx` и сбоев
внешних зависимостей.

Во втором спринте необходимо добавить:

- мониторинг через Prometheus;
- распределенный трейсинг через Jaeger;
- централизованный сбор логов через Grafana Loki или OpenSearch/ELK;
- визуализацию через Grafana;
- опциональный alerting через Prometheus Alertmanager.

## 1. Внедрить инструменты наблюдаемости

Инструментировать приложение с помощью OpenTelemetry:

- реализовать распределенный трейсинг HTTP, БД, S3 и брокера;
- реализовать технические и бизнес-метрики;
- настроить структурированное логирование;
- добавить корреляцию логов и трейсов.

В проекте используется Zerolog. Конкретная logging-библиотека не является
ограничением спринта, если выполняются требования к JSON-формату, уровням,
корреляции и централизованному сбору логов.

### 1.1. Трейсинг

Требования:

- инструментирование HTTP-запросов;
- трейсы для работы с БД;
- трейсы для S3-операций;
- трейсы для брокера сообщений;
- context propagation между сервисами.

Пример:

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
)

func (s *AvatarService) UploadAvatar(ctx context.Context, req *UploadRequest) error {
    ctx, span := otel.Tracer("avatar-service").Start(ctx, "upload_avatar")
    defer span.End()

    span.SetAttributes(
        attribute.String("user_id", req.UserID),
        attribute.String("file_name", req.FileName),
        attribute.Int64("file_size", req.Size),
    )

    // Бизнес-логика upload
    return nil
}
```

Пример демонстрирует tracing API, но production instrumentation не должна
передавать персональные данные или высококардинальные значения в Prometheus
labels.

### 1.2. Метрики

Пример:

```go
var (
    uploadsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "avatars_uploads_total",
            Help: "Total number of avatar uploads",
        },
        []string{"status"},
    )

    uploadDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "avatars_upload_duration_seconds",
            Help: "Avatar upload duration",
        },
        []string{"status"},
    )

    storageUsage = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "avatars_storage_bytes",
            Help: "Total storage used by avatars",
        },
    )
)
```

`user_id`, email, avatar ID, trace ID и raw URL не должны использоваться как
Prometheus labels, потому что создают неконтролируемую cardinality.

### 1.3. Логирование

Требования:

- структурированные JSON-логи;
- корреляция с `trace_id` и `span_id`;
- корректные уровни логирования;
- единые поля service, version и environment;
- отсутствие секретов и персональных данных;
- централизованный сбор и поиск логов.

Пример с Zerolog:

```go
spanContext := trace.SpanFromContext(ctx).SpanContext()

logger := baseLogger.With().
    Str("service", "gophprofile-server").
    Str("trace_id", spanContext.TraceID().String()).
    Str("span_id", spanContext.SpanID().String()).
    Logger()

logger.Info().
    Str("user_id", userID).
    Int64("file_size", fileSize).
    Str("mime_type", mimeType).
    Msg("avatar upload started")
```

## 2. Развернуть инфраструктуру мониторинга и логирования

Подключить и настроить внешний стек сервисов:

- Prometheus для сбора метрик;
- Jaeger для трейсинга;
- Grafana для визуализации;
- Grafana Loki или OpenSearch/ELK для централизованных логов.

Для GophProfile выбран стек Grafana Loki и Grafana Alloy.

### 2.1. Метрики Prometheus

Необходимо собирать:

- HTTP-метрики: количество запросов, duration, ошибки;
- бизнес-метрики: загрузки avatar, обработка, удаление и storage usage;
- инфраструктурные метрики: DB connections, queue depth и resource usage.

### 2.2. Jaeger

Необходимо обеспечить:

- distributed tracing;
- performance-анализ;
- dependency mapping;
- поиск trace по service, operation и trace ID;
- связь HTTP request с асинхронной обработкой worker.

### 2.3. Grafana Loki

Необходимо обеспечить:

- централизованное логирование server и worker;
- агрегацию и поиск логов;
- фильтрацию по service, environment и level;
- поиск по `trace_id`;
- переход от log к trace в Jaeger;
- возможность создавать alerts на ошибки.

## 3. Настроить визуализацию

Создать информативные dashboards в Grafana.

### Service Overview

- доступность server и worker;
- request rate;
- error rate;
- request duration;
- in-flight requests;
- error logs.

### RED metrics

- Rate - количество запросов в секунду;
- Errors - доля ошибочных запросов;
- Duration - p50, p95 и p99 latency.

### Resource Utilization

- CPU и memory процессов/контейнеров;
- goroutines и Go heap;
- PostgreSQL connection pool;
- S3 operation latency;
- Kafka consumer lag и queue depth.

### Business KPIs

- количество upload операций;
- количество успешно обработанных avatar;
- retry, failed и dead-letter события;
- количество delete операций;
- storage usage;
- pending outbox events.

## 4. Бонусная задача: настроить alerting

Настроить Prometheus alert rules и Alertmanager для критических показателей:

- высокий процент ошибок;
- увеличенное время ответа.

Пример:

```yaml
groups:
  - name: avatar-service
    rules:
      - alert: HighErrorRate
        expr: |
          sum(rate(gophprofile_http_server_requests_total{status_class="5xx"}[5m]))
          /
          clamp_min(sum(rate(gophprofile_http_server_requests_total[5m])), 0.001)
          > 0.10
        for: 5m
        labels:
          severity: warning

      - alert: HighResponseTime
        expr: |
          histogram_quantile(
            0.95,
            sum by (le) (
              rate(gophprofile_http_server_request_duration_seconds_bucket[5m])
            )
          ) > 5
        for: 2m
        labels:
          severity: critical
```

## Ожидаемый результат

После завершения второго спринта разработчик должен иметь возможность:

1. Увидеть состояние server и worker в Grafana.
2. Определить request rate, error rate и latency API.
3. Найти trace HTTP-запроса и его дочерние операции PostgreSQL, S3 и Kafka.
4. Проследить context от server через Kafka до worker.
5. Найти JSON-лог по `trace_id` и перейти от лога к trace.
6. Увидеть бизнес-метрики загрузки, обработки и удаления avatar.
7. Увидеть DB connections, outbox backlog и Kafka consumer lag.
8. Получить alert при превышении error rate или latency threshold.

## Критерии готовности

### Tracing

- HTTP, PostgreSQL, S3 и Kafka операции создают spans.
- Context передается через Kafka headers.
- Transactional outbox не теряет исходный trace context.
- Jaeger показывает server и worker в одном trace.

### Metrics

- Prometheus успешно собирает metrics server и worker.
- Доступны HTTP RED metrics.
- Доступны технические и бизнес-метрики.
- Metrics не содержат high-cardinality identifiers в labels.

### Logs

- Zerolog пишет структурированные JSON-логи.
- Logs содержат корректные levels.
- При active span logs содержат `trace_id` и `span_id`.
- Loki принимает logs server и worker.
- Grafana позволяет перейти от log к trace.

### Infrastructure

- Prometheus, Jaeger, Loki, Alloy и Grafana поднимаются через Docker Compose.
- Grafana datasources и dashboards создаются автоматически.
- Локальный запуск не требует ручной настройки через UI.

### Quality

- `go test ./...` проходит.
- Покрытие остается больше 50%.
- Линтер проходит без ошибок.
- Observability не меняет бизнес-контракты первого спринта.
- Недоступность telemetry backend не ломает API и worker.
