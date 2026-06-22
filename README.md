# GophProfile

GophProfile это микросервис для управления аватарками пользователей.

Пользователь загружает свою фотографию в GophProfile один раз. После этого
сторонние платформы, например блоги, форумы, сервисы комментариев и другие
приложения, могут запросить аватарку по email пользователя.

Если пользователь с таким email существует, сервис возвращает его аватарку.
Если пользователь не найден, сервис возвращает стандартное изображение-заглушку.

Внутри GophProfile avatar хранится по стабильному `user_id`, а `email`
используется как внешний атрибут для публичного поиска. Для локального MVP
frontend создает или находит связку `email -> user_id` через
`POST /api/v1/users/resolve`, после чего upload выполняется с header
`X-User-ID`.

## Цель проекта

GophProfile решает практическую продуктовую задачу: централизованную загрузку,
хранение, обработку и раздачу пользовательских аватарок через REST API.

Проект строится на востребованном бэкенд-стеке:

- Go
- PostgreSQL
- S3-совместимое объектное хранилище
- Kafka через Confluent client
- Docker
- Kubernetes
- Prometheus
- Grafana
- Loki, ELK или OpenSearch
- Jaeger

## Документация

- [ТЗ первого спринта](docs/dev-1/README.md)
- [ТЗ второго спринта](docs/dev-2/README.md)
- [S3-слой](docs/dev-1/s3-storage.md)
- [Валидация загрузки avatar](docs/dev-1/upload-validation.md)
- [Vault для секретов](deploy/vault/README.md)
- [Сторонние материалы](THIRD_PARTY_NOTICES.md)

## Веб-интерфейс

Исходники фронтенда находятся в `web/frontend`.

Фронтенд собирается через Vite и Sass в директорию `web/static`, которую позже
будет раздавать Go-сервер.

Команды:

```bash
cd web/frontend
npm install
npm run dev
npm run build
```

## Разработка

Требования:

- Go 1.26.3 или новее

Запуск HTTP-сервера:

```bash
go run ./cmd/server
```

Проверка healthcheck:

```bash
curl http://localhost:8080/health
```

Запуск worker:

```bash
go run ./cmd/worker
```

Запуск тестов:

```bash
go test ./...
```

Проверка покрытия:

```bash
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

Быстрый набор тестов не требует ручного запуска PostgreSQL, Kafka или MinIO:
HTTP, app, image processing, S3 и repository-слои проверяются unit-тестами с
fake-реализациями или `sqlmock`. Локальная интеграционная проверка полной
системы выполняется через `./scripts/local-up.sh`.

Интеграционные repository-тесты с реальным PostgreSQL запускаются одной
командой:

```bash
sh scripts/test-integration.sh
```

Миграции БД лежат в `migrations`.

Локальный запуск Vault с PostgreSQL storage backend:

```bash
docker compose -f deploy/vault/docker-compose.yml up -d
```

Локальный запуск основного окружения:

```bash
./scripts/local-up.sh
```

Сервисы:

- server API: `http://localhost:8080`
- server metrics: `http://localhost:9464/metrics`
- worker metrics: `http://localhost:9465/metrics`
- frontend-build: `http://localhost:3000/web/`
- PostgreSQL: `localhost:5432`
- Kafka: `kafka:9092` внутри compose-сети
- MinIO API: `http://localhost:9000`
- MinIO Console: `http://localhost:9001`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3001` (`admin` / `admin`)
- Jaeger: `http://localhost:16686`
- Loki API: `http://localhost:3100`
- Alloy UI: `http://localhost:12345`

В локальном compose `server` и `worker` автоматически создают MinIO bucket из
`S3_BUCKET`, если он еще отсутствует.

## Observability

Оба процесса используют единый OpenTelemetry bootstrap. Конфигурация:

Потоки observability разделены по назначению:

- Zerolog пишет структурированные JSON logs в stdout, инфраструктурный агент
  отправляет их в Loki, визуализация выполняется в Grafana
- OpenTelemetry metrics публикуются в Prometheus format через `/metrics`,
  Prometheus выполняет scrape, визуализация выполняется в Grafana
- OpenTelemetry traces отправляются по OTLP в Jaeger

Приложение не отправляет логи напрямую в Loki: это сохраняет бизнес-процессы
независимыми от доступности logging backend и оставляет доставку логов на
уровне инфраструктуры

- `OTEL_ENABLED` (по умолчанию `false`)
- `OTEL_SERVICE_NAME` (`gophprofile-server` или `gophprofile-worker`)
- `OTEL_EXPORTER_OTLP_ENDPOINT` (по умолчанию `localhost:4317`)
- `OTEL_EXPORTER_OTLP_INSECURE` (по умолчанию `true`)
- `OTEL_TRACES_SAMPLER` (по умолчанию `parentbased_always_on`)
- `OTEL_TRACES_SAMPLER_ARG` (по умолчанию `1`)
- `METRICS_ADDR` (`:9090` для server, `:9091` для worker)
- `LOG_LEVEL` (по умолчанию `info`, некорректное значение также даёт `info`)
- `LOG_FORMAT` (по умолчанию `json`; `console` разрешён только при `APP_ENV=local`)

Metrics HTTP server запускается и корректно останавливается даже в no-op режиме.
Локальный compose включает Prometheus, Grafana, Loki, Alloy и Jaeger. Для
`server` и `worker` telemetry включена, а service names заданы раздельно.
Alloy читает только stdout контейнеров `server` и `worker` через Docker API и
отправляет записи в Loki. Grafana автоматически получает data sources и
dashboard `GophProfile Observability`.

HTTP API инструментирован через `otelhttp`. Для API-запросов экспортируются
server spans и RED-метрики `http.server.request.count`,
`http.server.request.duration` и `http.server.active_requests`. Динамические
идентификаторы заменяются шаблонами `{avatar_id}` и `{user_id}` в `http.route`.
Маршруты `/health` и `/metrics` исключены из tracing и RED-метрик, чтобы probe
и scrape-трафик не искажали RPS, error ratio и p95 пользовательского API.

PostgreSQL repositories создают ручные client spans с атрибутами
`db.system.name`, `db.operation.name` и `db.collection.name`. SQL text, DSN и
параметры запросов в telemetry не записываются. Outbox-транзакции добавляют
события `db.transaction.begin`, `db.transaction.commit` и
`db.transaction.rollback`, не меняя порядок SQL-операций.

Состояние `database/sql` pool экспортируется через `sql.DBStats()`: Grafana
показывает open, used, idle и max connections, количество ожиданий свободного
соединения и суммарное wait time отдельно для `server` и `worker`.
Worker создаёт business spans для обработки avatar messages и outbox polling,
поэтому PostgreSQL spans сохраняют корректную иерархию до Kafka instrumentation.

S3-compatible storage создаёт client spans для `Put`, `Stat`, `Get`, `Delete`,
`Exists` и `EnsureBucket`. Spans содержат только operation, result, известный
размер и content type. Object key не записывается в spans, metrics или error
text. `Get` измеряется до EOF или `Close` исходного body без дополнительного
чтения, а отсутствие объекта при `Delete` считается успешным результатом.

Prometheus получает `s3.client.operation.count` и
`s3.client.operation.duration`; Grafana показывает S3 operation rate, error
ratio и p95 latency отдельно для `server` и `worker`.

Kafka использует W3C `traceparent`, `tracestate` и baggage в message headers.
Carrier сохраняется вместе с outbox event в PostgreSQL JSONB, поэтому delayed
publish после restart продолжает исходный HTTP trace. Producer повторно
инжектирует context своего `PRODUCER` span, consumer извлекает remote parent и
создаёт `CONSUMER` span, а commit offset записывается отдельным client span и
event после успешного handler.

Retry и dead-letter публикации наследуют trace context и неизвестные headers из
consumer context. Payload и message key не записываются в logs, spans или
metric labels. Kafka operation count, client duration и process duration
доступны в Prometheus и на Kafka-панелях Grafana.

Business metrics используют только фиксированные labels `result`, `phase` и
`mode`. Принятая загрузка учитывается в `app.avatar.upload.count` с результатом
`accepted` после атомарной записи avatar и outbox. Ошибка immediate publish не
превращает её в upload error и отдельно попадает в `app.outbox.publish.count`.
Результаты обработки `ready`, `failed`, `retry_scheduled`, `idempotent_skip` и
`error` экспортируются через `app.avatar.processing.count`; повторная обработка
не увеличивает upload counter. Планирование и фактическое выполнение удаления
разделены label `phase` в `app.avatar.delete.count`.

Operational gauges `app.outbox.pending.count`, `app.outbox.oldest.age`,
`app.avatar.count` и `app.avatar.original.storage` читаются из PostgreSQL при
scrape. Поэтому backlog, статусы аватаров и размер оригиналов восстанавливаются
после перезапуска процесса и не зависят от локального состояния счётчиков.
Dashboard показывает accepted uploads, ready/failed processing, completed
deletes, outbox backlog, возраст старейшего события и объём оригиналов.

Для повторного запуска без пересборки образов:

```bash
./scripts/local-up.sh --no-build
```
