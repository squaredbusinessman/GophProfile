# Локальное Docker-окружение

Основной compose-файл поднимает сервисы, нужные для разработки GophProfile:

- `server`
- `worker`
- `frontend-build`
- `postgres`
- `migrate`
- `kafka`
- `minio`
- `prometheus`
- `grafana`
- `loki`
- `alloy`
- `jaeger`

## Запуск

```bash
./scripts/local-up.sh
```

Скрипт собирает образы, поднимает compose в фоне, дожидается готовности
`postgres`, `kafka`, `server`, `worker`, `frontend-build`, проверяет
`/health`, frontend и наличие MinIO bucket.

Повторный запуск без пересборки образов:

```bash
./scripts/local-up.sh --no-build
```

Запуск с переходом в логи после readiness-проверок:

```bash
./scripts/local-up.sh --logs
```

Низкоуровневый compose-запуск из директории `deploy`:

```bash
cd deploy
docker compose up --build
```

Остановка с очисткой локальных volumes:

```bash
docker compose -f deploy/docker-compose.yml down -v
```

## Адреса

- Server API: `http://localhost:8080`
- Healthcheck: `http://localhost:8080/health`
- Frontend: `http://localhost:3000/web/`
- PostgreSQL: `localhost:5432`
- Kafka: `localhost:9092`
- MinIO API: `http://localhost:9000`
- MinIO Console: `http://localhost:9001`
- Server metrics: `http://localhost:9464/metrics`
- Worker metrics: `http://localhost:9465/metrics`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3001` (`admin` / `admin`)
- Jaeger: `http://localhost:16686`
- Loki API: `http://localhost:3100`
- Alloy UI: `http://localhost:12345`

## Observability

Prometheus забирает метрики напрямую с отдельных metrics endpoints `server` и
`worker`. Jaeger принимает OTLP по gRPC на `jaeger:4317`. Alloy обнаруживает
контейнеры через Docker API, выбирает только application logs от `server` и
`worker` и отправляет их в Loki.

Grafana provisioning автоматически создает data sources Prometheus, Loki и
Jaeger, а также dashboard `GophProfile Observability`. Связи Loki -> Jaeger и
Jaeger -> Loki используют поля `trace_id`, `span_id` и `service_name`.

Dashboard объединяет HTTP RED и состояние PostgreSQL connection pool. Метрики
pool собираются приложениями из `sql.DBStats()` и включают open, used, idle,
maximum connections, waits и суммарную продолжительность ожиданий.

S3-панели показывают operation rate, error ratio и p95 latency по ограниченным
labels operation/result. Object key не экспортируется в Prometheus labels.

Jaeger работает в all-in-one режиме с in-memory storage. Это осознанная
настройка локальной разработки: при перезапуске Jaeger трейсы удаляются. Loki,
Prometheus и Grafana используют именованные Docker volumes.

Alloy получает read-only доступ к Docker socket. Для production вместо этого
нужен инфраструктурный log collector с минимально необходимыми правами и
внешние persistent storage для telemetry backends.

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
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:5173
```

При `APP_ENV=local` приложения `server` и `worker` создают bucket
`S3_BUCKET`, если он еще отсутствует.

## Security env

`server` в compose получает локальные security-настройки:

```text
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:5173
API_RATE_LIMIT_RPS=20
API_RATE_LIMIT_BURST=40
```

`CORS_ALLOWED_ORIGINS` должен оставаться явным списком origins без wildcard.

## Миграции

Сервис `migrate` применяет SQL-файлы из `migrations`.

Для локального compose миграции сделаны идемпотентными, чтобы повторный запуск
окружения не падал на уже созданной таблице.
