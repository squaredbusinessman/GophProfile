# ТЗ первого спринта GophProfile

Цель первого спринта - создать работающий MVP. Сервис должен принимать
картинки, сохранять их и отдавать обратно.

## Задачи спринта

Перед реализацией задач соблюдать [стандарты кода и документации](coding-standards.md).

Чувствительные данные проекта должны храниться через
[HashiCorp Vault](../../deploy/vault/README.md), а не в коде или публичных
конфигурационных файлах.

Object storage изолируется через [S3-слой](s3-storage.md).

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
- Валидировать `User-ID` из заголовков

## API

### Загрузка аватарки

```http
POST /api/v1/avatars
Content-Type: multipart/form-data
X-User-ID: string
```

Поля запроса:

```text
file: binary
```

Ограничения:

- `X-User-ID` обязателен
- `file` обязателен
- Максимальный размер файла `10MB`
- Форматы `JPEG`, `PNG`, `WebP` опционально
- Асинхронная обработка через брокер
- Создание миниатюр `100x100` и `300x300`

Ответ `201`:

```json
{
  "id": "uuid",
  "user_id": "string",
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
GET /api/v1/users/{user_id}/avatar
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
DELETE /api/v1/users/{user_id}/avatar
X-User-ID: string
```

Требования:

- `X-User-ID` обязателен
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
  "user_id": "string",
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
GET /api/v1/users/{user_id}/avatars
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
GET  /web/gallery/{user_id}
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
CREATE TABLE avatars (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(255) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL,
    s3_key VARCHAR(500) NOT NULL,
    thumbnail_s3_keys JSONB,
    upload_status VARCHAR(50) DEFAULT 'uploading',
    processing_status VARCHAR(50) DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_avatars_user_id ON avatars(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_avatars_status ON avatars(upload_status, processing_status);
```

## События брокера

```go
type AvatarUploadEvent struct {
    AvatarID string `json:"avatar_id"`
    UserID   string `json:"user_id"`
    S3Key    string `json:"s3_key"`
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
