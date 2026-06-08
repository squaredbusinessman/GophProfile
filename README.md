# GophProfile

GophProfile это микросервис для управления аватарками пользователей.

Пользователь загружает свою фотографию в GophProfile один раз. После этого
сторонние платформы, например блоги, форумы, сервисы комментариев и другие
приложения, могут запросить аватарку по email пользователя.

Если пользователь с таким email существует, сервис возвращает его аватарку.
Если пользователь не найден, сервис возвращает стандартное изображение-заглушку.

## Цель проекта

GophProfile решает практическую продуктовую задачу: централизованную загрузку,
хранение, обработку и раздачу пользовательских аватарок через REST API.

Проект строится на востребованном бэкенд-стеке:

- Go
- PostgreSQL
- S3-совместимое объектное хранилище
- Kafka
- Docker
- Kubernetes
- Prometheus
- Grafana
- Loki, ELK или OpenSearch
- Jaeger

## Документация

- [ТЗ первого спринта](docs/dev-1/README.md)
- [Стандарты кода и документации](docs/dev-1/coding-standards.md)
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
docker compose -f deploy/docker-compose.yml up --build
```

Сервисы:

- backend API: `http://localhost:8080`
- frontend: `http://localhost:3000/web/`
- PostgreSQL: `localhost:5432`
- Kafka: `kafka:9092` внутри compose-сети
- MinIO API: `http://localhost:9000`
- MinIO Console: `http://localhost:9001`

## Статус

Проект находится на этапе инициализации.

Текущее состояние репозитория:

- Инициализирован Go module
- Настроен `.gitignore`
- Подготовлен README
- Добавлен базовый backend-каркас: server, worker, config, Zerolog logger,
  healthcheck и заготовки модулей PostgreSQL, S3, Kafka, image processing
