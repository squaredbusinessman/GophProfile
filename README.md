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
- [Стандарты кода и документации](docs/dev-1/coding-standards.md)
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
- frontend-build: `http://localhost:3000/web/`
- PostgreSQL: `localhost:5432`
- Kafka: `kafka:9092` внутри compose-сети
- MinIO API: `http://localhost:9000`
- MinIO Console: `http://localhost:9001`

В локальном compose `server` и `worker` автоматически создают MinIO bucket из
`S3_BUCKET`, если он еще отсутствует.

Для повторного запуска без пересборки образов:

```bash
./scripts/local-up.sh --no-build
```
