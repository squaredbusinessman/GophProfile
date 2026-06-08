# Локальное Docker-окружение

Основной compose-файл поднимает сервисы, нужные для разработки GophProfile:

- `frontend`
- `backend`
- `worker`
- `postgres`
- `migrate`
- `kafka`
- `minio`

## Запуск

```bash
docker compose -f deploy/docker-compose.yml up --build
```

## Адреса

- Backend API: `http://localhost:8080`
- Healthcheck: `http://localhost:8080/health`
- Frontend: `http://localhost:3000/web/`
- PostgreSQL: `localhost:5432`
- MinIO API: `http://localhost:9000`
- MinIO Console: `http://localhost:9001`

## Миграции

Сервис `migrate` применяет SQL-файлы из `migrations`.

Для локального compose миграции сделаны идемпотентными, чтобы повторный запуск
окружения не падал на уже созданной таблице.
