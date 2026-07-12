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
- [ТЗ третьего спринта](docs/dev-3/README.md)
- [Helm chart и Kubernetes деплой](deploy/helm/gophprofile/README.md)
- [OpenAPI спецификация](docs/openapi.yaml)
- [S3-слой](docs/dev-1/s3-storage.md)
- [Валидация загрузки avatar](docs/dev-1/upload-validation.md)
- [Локальное Docker-окружение](deploy/README.md)
- [Runbooks observability alerts](docs/runbooks/observability-alerts.md)
- [Vault для секретов](deploy/vault/README.md)
- [Сторонние материалы](THIRD_PARTY_NOTICES.md)

## Веб-интерфейс

Исходники фронтенда находятся в `web/frontend`.

Фронтенд собирается через Vite и Sass в директорию `web/static`. В локальном
Docker-окружении собранные файлы раздаёт отдельный Nginx-контейнер
`frontend-build` по адресу `/web/`.

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

Локальный запуск полного окружения:

```bash
./scripts/local-up.sh
```

Smoke-проверка observability после запуска:

```bash
./scripts/observability-smoke.sh
```

Остановка окружения с сохранением локальных volumes:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.observability.yml down
```

Сервисы:

- server API: `http://localhost:8080`
- frontend: `http://localhost/web/`
- PostgreSQL: `localhost:5432`
- Kafka: `kafka:9092` внутри compose-сети
- MinIO API: `http://localhost:9000`
- MinIO Console: `http://localhost:9001`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3001` (`admin` / `admin`)
- Jaeger: `http://localhost:16686`
- Loki API: `http://localhost:3100`
- Alertmanager: `http://localhost:9093`
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

| Переменная | Назначение |
| --- | --- |
| `OTEL_ENABLED` | включает экспорт traces и metrics, по умолчанию `false` |
| `OTEL_SERVICE_NAME` | имя процесса, например `gophprofile-server` или `gophprofile-worker` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | адрес OTLP collector, локально `jaeger:4317` |
| `OTEL_EXPORTER_OTLP_INSECURE` | отключает TLS для локального OTLP, по умолчанию `true` |
| `OTEL_TRACES_SAMPLER` | стратегия sampling, по умолчанию `parentbased_always_on` |
| `OTEL_TRACES_SAMPLER_ARG` | аргумент ratio-based sampling, по умолчанию `1` |
| `METRICS_ADDR` | отдельный HTTP-адрес метрик, `:9090` для server и `:9091` для worker |
| `LOG_LEVEL` | уровень логов, безопасный fallback `info` |
| `LOG_FORMAT` | `json` по умолчанию, `console` разрешён только при `APP_ENV=local` |

Metrics HTTP server запускается и корректно останавливается даже в no-op режиме.
Базовый `deploy/docker-compose.yml` содержит приложение и его хранилища, а
`deploy/docker-compose.observability.yml` добавляет Prometheus, Jaeger, Loki,
Alloy, Grafana, Kafka exporter, cAdvisor и Alertmanager. `scripts/local-up.sh`
объединяет оба файла одной командой. Для `server` и `worker` overlay включает
telemetry и задаёт разные service names.
Alloy читает только stdout контейнеров `server` и `worker` через Docker API и
отправляет записи в Loki. JSON pipeline использует только labels `service`,
`environment`, `level` и `container`; идентификаторы трассировки, запросов и
сущностей остаются полями записи. Grafana автоматически получает data sources
Prometheus, Loki, Jaeger и Alertmanager, а также dashboards `Service Overview`,
`Business KPIs` и `Dependencies and Resources`.

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

Prometheus также забирает consumer lag из Kafka exporter и container resource
metrics из cAdvisor. Loki хранит локальные данные семь дней. Alloy и cAdvisor
монтируют `/var/run/docker.sock` только для чтения; даже read-only socket даёт
широкую видимость Docker daemon, поэтому эта схема предназначена исключительно
для локальной разработки. В production Kubernetes следует использовать
discovery и сбор pod logs через Kubernetes API.

Для повторного запуска без пересборки образов:

```bash
./scripts/local-up.sh --no-build
```

### Типовое расследование инцидента

1. Открыть Grafana `Service Overview` и определить, что выросло: 5xx, p95,
   in-flight requests или WARN/ERROR logs
2. В панели последних error logs взять `trace_id`
3. Открыть trace в Jaeger и посмотреть, где задержка или ошибка: HTTP, DB, S3,
   Kafka producer, Kafka consumer или worker
4. По тому же `trace_id` найти все связанные logs в Loki
5. Проверить `Dependencies and Resources`: PostgreSQL pool, Kafka lag, S3
   latency, Go runtime и container metrics
6. Если сработал alert, открыть runbook из label `runbook`

Пример поиска логов по trace ID в Loki:

```logql
{service=~"server|worker"} |= "0721d079ec1bdab9194635684f0177b2"
```

Пример запроса trace через Jaeger API:

```bash
curl 'http://localhost:16686/api/traces/0721d079ec1bdab9194635684f0177b2'
```

Полезные API для smoke-проверок:

```bash
curl http://localhost:9090/api/v1/targets
curl http://localhost:16686/api/services
curl 'http://localhost:3100/loki/api/v1/label/service/values'
curl http://localhost:3001/api/health
```

### Regression commands

```bash
go test ./...
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
go vet ./...
golangci-lint run ./...
(cd web/frontend && npm run build)
docker compose -f deploy/docker-compose.yml config --quiet
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.observability.yml config --quiet
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.observability.yml run --rm --no-deps --entrypoint promtool -v "$PWD/deploy/observability:/etc/prometheus:ro" prometheus check config /etc/prometheus/prometheus.yml
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.observability.yml run --rm --no-deps --entrypoint promtool -v "$PWD/deploy/observability:/etc/prometheus:ro" prometheus check rules /etc/prometheus/prometheus-rules.yml
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.observability.yml run --rm --no-deps --entrypoint promtool -v "$PWD/deploy/observability:/etc/prometheus:ro" prometheus test rules /etc/prometheus/prometheus-rules.test.yml
```

### Локальные ограничения

Jaeger в локальном compose работает с in-memory storage, поэтому traces
исчезают после перезапуска контейнера. Loki, Prometheus, Grafana и Alertmanager
используют Docker volumes и предназначены только для разработки. Локальный
Alertmanager показывает alerts в UI без внешней отправки.

Alloy и cAdvisor получают read-only доступ к `/var/run/docker.sock`. Даже такой
доступ даёт широкую видимость Docker daemon, поэтому эту схему нельзя переносить
в production как есть. В Kubernetes нужен сбор logs через Kubernetes API с
минимальными правами и внешние хранилища для telemetry backends.

## Static Analysis

Статический анализ запускается обычным `golangci-lint`:

```bash
golangci-lint run ./...
```
