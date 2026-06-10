# Локальное Docker-окружение

Основной compose-файл поднимает сервисы, нужные для разработки GophProfile:

- `server`
- `worker`
- `frontend-build`
- `postgres`
- `migrate`
- `kafka`
- `minio`

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
