# ТЗ первого спринта GophProfile

Цель первого спринта - создать работающий MVP. Сервис должен принимать
картинки, сохранять их и отдавать обратно.

## Задачи спринта

Перед реализацией задач соблюдать [стандарты кода и документации](coding-standards.md).

Чувствительные данные проекта должны храниться через
[HashiCorp Vault](../../deploy/vault/README.md), а не в коде или публичных
конфигурационных файлах.

Object storage изолируется через [S3-слой](s3-storage.md).

Загрузка avatar проверяется через [валидацию upload request](upload-validation.md).

### 1. Реализовать ядро сервиса и REST API

- Создать HTTP-сервер на Go
- Выбрать HTTP-роутер: Echo или Chi
- Подключить PostgreSQL для хранения метаданных
- Подключить S3-совместимое хранилище для файлов: MinIO или AWS S3
- Реализовать загрузку аватарки
- Реализовать получение аватарки
- Реализовать удаление аватарки
- Реализовать получение метаданных аватарки
- Реализовать получение списка аватарок пользователя
- Реализовать healthcheck
- Интегрировать готовый фронтенд

Фронтенд проекта размещается в `web/frontend` и собирается в `web/static`.

### 2. Настроить асинхронную обработку изображений

- Выбрать брокер сообщений: RabbitMQ или Kafka
- Реализовать worker-сервис для фоновой обработки изображений
- Создавать миниатюры `100x100` и `300x300`
- Реализовать асинхронное удаление файлов из S3
- Обеспечить идемпотентность обработки
- Реализовать retry с экспоненциальным backoff
- Для работы с Kafka использовать Confluent Kafka Go client

### 3. Обеспечить качество и контейнеризацию сервиса

- Написать unit-тесты
- Достичь покрытия тестами больше `50%`
- Подготовить Dockerfile с multi-stage build
- Настроить Docker Compose для локального окружения
- Запускать через Docker Compose server, worker, PostgreSQL, брокер сообщений и MinIO

### 4. Бонусная задача: обеспечить безопасность

- Валидировать MIME-типы и magic bytes
- Ограничить размер файлов
- Добавить rate limiting для API
- Настроить CORS
- Валидировать email пользователя из `X-User-ID` и сопоставлять его с внутренним `user_id`

## API

### Загрузка аватарки

```http
POST /api/v1/avatars
Content-Type: multipart/form-data
X-User-ID: email
```

Поля запроса:

```text
file: binary
```

Ограничения:

- `X-User-ID` обязателен и должен содержать email пользователя
- `file` обязателен
- Максимальный размер файла `10MB`
- Форматы `JPEG`, `PNG`, `WebP` опционально
- Асинхронная обработка через брокер
- Создание миниатюр `100x100` и `300x300`

Ответ `201`:

```json
{
  "id": "uuid",
  "user_id": "uuid",
  "email": "user@example.com",
  "url": "string",
  "status": "processing",
  "created_at": "2024-01-01T00:00:00Z"
}
```

Ответ `400`:

```json
{
  "error": "Invalid file format",
  "details": "Supported formats: jpeg, png, webp"
}
```

Ответ `413`:

```json
{
  "error": "File too large",
  "max_size": 10485760
}
```

### Получение аватарки

```http
GET /api/v1/avatars/{avatar_id}
GET /api/v1/users/{user_email}/avatar
```

Query-параметры опционально:

```text
size: string, values: "100x100", "300x300", "original"
format: string, values: "jpeg", "png", "webp"
```

Пример:

```http
GET /api/v1/avatars/{avatar_id}?size=300x300&format=webp
```

Ответ `200`:

```text
Binary image data
```

Заголовки ответа:

```http
Content-Type: image/jpeg|png|webp
Cache-Control: max-age=86400
ETag: "hash"
```

Ответ `404`:

```json
{
  "error": "Avatar not found"
}
```

### Удаление аватарки

```http
DELETE /api/v1/avatars/{avatar_id}
DELETE /api/v1/users/{user_email}/avatar
X-User-ID: email
```

Требования:

- `X-User-ID` обязателен и должен содержать email пользователя
- Мягкое удаление в БД
- Асинхронное удаление из S3
- Удалять можно только свои аватарки

Ответ `204`:

```text
No Content
```

Ответ `403`:

```json
{
  "error": "Forbidden",
  "details": "You can only delete your own avatars"
}
```

### Получение метаданных аватарки

```http
GET /api/v1/avatars/{avatar_id}/metadata
```

Ответ `200`:

```json
{
  "id": "uuid",
  "user_id": "uuid",
  "email": "user@example.com",
  "file_name": "avatar.jpg",
  "mime_type": "image/jpeg",
  "size": 1024000,
  "dimensions": {
    "width": 1920,
    "height": 1080
  },
  "thumbnails": [
    {
      "size": "100x100",
      "url": "..."
    },
    {
      "size": "300x300",
      "url": "..."
    }
  ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

### Список аватарок пользователя

```http
GET /api/v1/users/{user_email}/avatars
```

### Проверка работоспособности

```http
GET /health
```

Healthcheck должен проверять:

- PostgreSQL
- S3
- Брокер сообщений

Ответ должен быть в JSON и содержать статусы компонентов.

### Веб-интерфейс

```http
GET  /web/upload
POST /web/upload
GET  /web/gallery/{user_email}
```

Веб-интерфейс должен включать:

- Форму загрузки
- Превью изображения
- Галерею аватарок пользователя
- Drag and drop опционально
- Прогресс загрузки опционально

Фронтенд уже готов. В проект добавлен Vite-фронтенд на основе шаблона
[Yandex-Practicum/go-avatar-service-template](https://github.com/Yandex-Practicum/go-avatar-service-template).

## Архитектура

Компоненты системы:

- HTTP-сервер с REST API и веб-интерфейсом
- Сервис обработки изображений
- Брокер сообщений RabbitMQ или Kafka
- База данных PostgreSQL для метаданных
- S3-совместимое хранилище для файлов
- Worker для асинхронной обработки

Возможная структура проекта:

```text
avatars-service/
├── cmd/
│   ├── server/
│   └── worker/
├── internal/
│   ├── api/
│   ├── config/
│   ├── domain/
│   ├── handlers/
│   ├── repository/
│   ├── services/
│   └── worker/
├── pkg/
├── web/
├── migrations/
├── docker/
├── k8s/
└── tests/
```

## Технические требования

Основной стек:

- Go 1.21+
- Echo или Chi
- PostgreSQL
- MinIO или AWS S3
- RabbitMQ или Kafka
- Docker
- Docker Compose

Требования к брокеру сообщений:

- Для RabbitMQ использовать exchange типа `direct` или `topic`
- Для Kafka создать топики для обработки изображений

Требования к базе данных:

- Таблицы для метаданных
- Индексы
- Миграции

## Модель данных

Пример схемы PostgreSQL:

```sql
CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX idx_users_email_active_unique
    ON users (lower(email))
    WHERE deleted_at IS NULL;

CREATE TABLE avatars (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    file_name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    width INTEGER NULL,
    height INTEGER NULL,
    status TEXT NOT NULL,
    original_object_key TEXT NOT NULL,
    thumb_100_object_key TEXT NULL,
    thumb_300_object_key TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX idx_avatars_user_id_created_at ON avatars(user_id, created_at DESC);
CREATE INDEX idx_avatars_user_id_active ON avatars(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_avatars_status ON avatars(status);
```

Email используется как публичный ключ поиска avatar. Внутри системы email
сначала сопоставляется с записью `users`, после чего вся работа с avatar и S3
идет через стабильный UUID из `users.id`.

## События брокера

```go
type AvatarUploadEvent struct {
    AvatarID          string `json:"avatar_id"`
    UserID            string `json:"user_id"`
    Email             string `json:"email"`
    OriginalObjectKey string `json:"original_object_key"`
    Thumb100ObjectKey string `json:"thumb_100_object_key"`
    Thumb300ObjectKey string `json:"thumb_300_object_key"`
    ContentType        string `json:"content_type"`
}

type AvatarProcessEvent struct {
    AvatarID   string         `json:"avatar_id"`
    Operations []ProcessingOp `json:"operations"`
}

type AvatarDeleteEvent struct {
    AvatarID string   `json:"avatar_id"`
    S3Keys   []string `json:"s3_keys"`
}
```

Требования к идемпотентности:

- Использовать уникальные идентификаторы сообщений
- Проверять статус обработки перед выполнением операций
- Реализовать retry с экспоненциальным backoff

## Тестирование

Unit-тесты:

- HTTP-обработчики
- Сервисные слои
- Репозитории
- Утилиты для работы с изображениями

Инструменты:

- `go test` для unit-тестов
- `testify/suite` для интеграционных тестов
- `testcontainers-go` для тест-окружения
- `golangci-lint` для статического анализа

Цель покрытия:

```text
>50%
```

## Докеризация

Dockerfile должен использовать multi-stage build.

Образы:

- `server`
- `worker`

Docker Compose для разработки должен запускать:

- Приложение `server`
- Приложение `worker`
- PostgreSQL
- RabbitMQ или Kafka
- MinIO
