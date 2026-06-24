# Локальное Docker-окружение

Основной compose-файл поднимает сервисы, нужные для разработки GophProfile:

- `server`
- `worker`
- `frontend-build`
- `postgres`
- `migrate`
- `kafka`
- `minio`

Observability overlay `docker-compose.observability.yml` добавляет:

- `prometheus`
- `grafana`
- `loki`
- `alloy`
- `jaeger`
- `kafka-exporter`
- `cadvisor`
- `alertmanager`

## Запуск

```bash
./scripts/local-up.sh
```

Скрипт объединяет base и observability overlay, собирает образы, поднимает стек
в фоне и дожидается healthchecks всех сервисов. Дополнительно он проверяет `UP`
targets server/worker в Prometheus, readiness Loki и Alloy, JSON logs обоих
процессов в Loki, HTTP API, frontend и наличие MinIO bucket.

Повторный запуск без пересборки образов:

```bash
./scripts/local-up.sh --no-build
```

Запуск с переходом в логи после readiness-проверок:

```bash
./scripts/local-up.sh --logs
```

Smoke-проверка полного пути observability:

```bash
./scripts/observability-smoke.sh
```

Низкоуровневый compose-запуск из директории `deploy`:

```bash
cd deploy
docker compose -f docker-compose.yml -f docker-compose.observability.yml up --build
```

Остановка с сохранением локальных volumes:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.observability.yml down
```

## Адреса

- Server API: `http://localhost:8080`
- Healthcheck: `http://localhost:8080/health`
- Frontend: `http://localhost/web/`
- PostgreSQL: `localhost:5432`
- Kafka: `localhost:9092`
- MinIO API: `http://localhost:9000`
- MinIO Console: `http://localhost:9001`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3001` (`admin` / `admin`)
- Jaeger: `http://localhost:16686`
- Loki API: `http://localhost:3100`
- Alertmanager: `http://localhost:9093`
- Alloy UI: `http://localhost:12345`

## Observability

Prometheus забирает метрики напрямую с отдельных metrics endpoints `server` и
`worker` внутри compose network, а также собирает Kafka consumer lag через
Kafka exporter и container resource metrics через cAdvisor. Jaeger принимает
OTLP по gRPC на `jaeger:4317` и HTTP на `jaeger:4318`. Alloy обнаруживает
контейнеры через Docker API, выбирает только application logs от `server` и
`worker`, разбирает JSON и отправляет записи в Loki.

Grafana provisioning автоматически создает data sources Prometheus, Loki,
Jaeger и Alertmanager, а также dashboards `Service Overview`, `Business KPIs`
и `Dependencies and Resources`. Связи Loki -> Jaeger и Jaeger -> Loki используют
поля `trace_id` и `span_id` без превращения их в Loki labels.

Service Overview показывает доступность, HTTP RED, WARN/ERROR logs и последние
ошибки со ссылкой на trace. Метрики PostgreSQL pool собираются приложениями из
`sql.DBStats()` и включают open, used, idle, maximum connections, waits и
суммарную продолжительность ожиданий.

S3-панели показывают operation rate, error ratio и p95 latency по ограниченным
labels operation/result. Object key не экспортируется в Prometheus labels.

Kafka-панели показывают send/process/commit rate, error ratio и p95 latency.
Outbox сохраняет W3C carrier в `headers` JSONB, поэтому публикация после restart
продолжает исходный trace. Payload и message key не экспортируются в telemetry.

Business-панели показывают accepted uploads, ready/failed processing и
completed deletes. Outbox backlog, возраст старейшего pending event, количество
avatar по status и original storage bytes вычисляются запросами к PostgreSQL при
каждом scrape, поэтому gauges корректно восстанавливаются после restart.

Prometheus загружает alert rules из `observability/prometheus-rules.yml` и
отправляет alerts в локальный Alertmanager. В local Alertmanager ничего не
отправляет наружу, поэтому alerts видны только в UI `http://localhost:9093`.
Для production receiver есть шаблоны
`observability/alertmanager.production.env.example` и
`observability/alertmanager.production.yml.example`; реальные Telegram, Slack
или email credentials в репозиторий не добавляются.

Jaeger работает в all-in-one режиме с in-memory storage. Это осознанная
настройка локальной разработки: при перезапуске Jaeger трейсы удаляются. Loki,
Prometheus и Grafana используют именованные Docker volumes.

Alloy использует labels только `service`, `environment`, `level` и `container`.
Alloy и cAdvisor получают read-only доступ к Docker socket, но такой mount всё
равно даёт широкую видимость Docker daemon. Для production Kubernetes нужен
сбор pod logs через Kubernetes API с минимально необходимыми правами и внешние
persistent storage для telemetry backends.

## Env

`server` и `worker` получают локальные настройки через env:

```text
DATABASE_URL=postgres://gophprofile:gophprofile@postgres:5432/gophprofile?sslmode=disable
S3_ENDPOINT=http://minio:9000
S3_BUCKET=gophprofile-avatars
S3_ACCESS_KEY=minioadmin
S3_SECRET_KEY=minioadmin
S3_USE_PATH_STYLE=true
KAFKA_BROKERS=kafka:9092
KAFKA_CLIENT_ID=gophprofile-server|gophprofile-worker
KAFKA_CONSUMER_GROUP=gophprofile-avatar-worker
HTTP_ADDR=:8080
DEFAULT_AVATAR_PATH=/app/default_avatar.png
CORS_ALLOWED_ORIGINS=http://localhost,http://localhost:3000,http://localhost:5173
```

Observability env:

```text
APP_ENV=local
LOG_LEVEL=info
LOG_FORMAT=json
OTEL_ENABLED=true
OTEL_SERVICE_NAME=gophprofile-server|gophprofile-worker
OTEL_EXPORTER_OTLP_ENDPOINT=jaeger:4317
OTEL_EXPORTER_OTLP_INSECURE=true
OTEL_TRACES_SAMPLER=parentbased_always_on
OTEL_TRACES_SAMPLER_ARG=1
METRICS_ADDR=:9090|:9091
```

При `APP_ENV=local` приложения `server` и `worker` создают bucket
`S3_BUCKET`, если он еще отсутствует.

## Security env

`server` в compose получает локальные security-настройки:

```text
CORS_ALLOWED_ORIGINS=http://localhost,http://localhost:3000,http://localhost:5173
API_RATE_LIMIT_RPS=20
API_RATE_LIMIT_BURST=40
```

`CORS_ALLOWED_ORIGINS` должен оставаться явным списком origins без wildcard.

Фронтенд по умолчанию публикуется на host port `80`, поэтому открывается как
`http://localhost/web/`. Если порт `80` занят, можно поднять его на другом порту:

```bash
FRONTEND_PORT=3000 ./scripts/local-up.sh
```

Тогда адрес будет `http://localhost:3000/web/`.

## Smoke и диагностика

`scripts/observability-smoke.sh` выполняет полный пользовательский путь:

- проверяет readiness Prometheus, Jaeger, Loki и Grafana
- создаёт пользователя
- загружает маленький валидный PNG
- ждёт status `ready`
- читает avatar
- удаляет avatar
- проверяет application metrics в Prometheus
- ищет trace `server -> worker` в Jaeger
- ищет log с тем же `trace_id` в Loki

Полезные API:

```bash
curl http://localhost:9090/api/v1/targets
curl 'http://localhost:9090/api/v1/query?query=up'
curl http://localhost:16686/api/services
curl 'http://localhost:16686/api/traces?service=gophprofile-server&limit=20&lookback=1h'
curl http://localhost:3100/loki/api/v1/label/service/values
curl 'http://localhost:3100/loki/api/v1/query_range?query=%7Bservice%3D~%22server%7Cworker%22%7D'
curl http://localhost:3001/api/health
```

Поиск logs по trace ID:

```logql
{service=~"server|worker"} |= "0721d079ec1bdab9194635684f0177b2"
```

Типовой путь расследования:

1. Открыть Grafana `Service Overview`
2. Найти рост 5xx, p95 latency, in-flight requests или ERROR logs
3. Взять `trace_id` из error log
4. Открыть trace в Jaeger и найти медленный или ошибочный span
5. По тому же `trace_id` посмотреть связанные logs в Loki
6. Проверить `Dependencies and Resources`: PostgreSQL pool, Kafka lag, S3 и container metrics
7. При alert открыть runbook из label `runbook`

## Проверки

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

## Миграции

Сервис `migrate` применяет SQL-файлы из `migrations`.

Для локального compose миграции сделаны идемпотентными, чтобы повторный запуск
окружения не падал на уже созданной таблице.
