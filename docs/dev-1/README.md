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
- Использовать transactional outbox для надежной публикации событий
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
- Ограничить размер файлов через `10MB` file limit и multipart body overhead
- Добавить rate limiting для API
- Настроить CORS через явный allowlist origins из env
- Валидировать внутренний UUID пользователя из `X-User-ID`
- Не отдавать клиенту внутренние object keys, DSN и stack traces

## Безопасность API

Минимальный security-контракт спринта:

- Upload ограничивает multipart body через `http.MaxBytesReader`
- Максимальный размер файла остается `10MB`
- MIME из multipart header должен совпадать с magic bytes `JPEG`, `PNG` или `WebP`
- `X-User-ID` обязателен для защищенных операций и должен быть внутренним UUID
- Public lookup по email доступен только через `GET /api/v1/avatar?email=...`
- API rate limiting применяется ко всем routes с префиксом `/api/`
- Rate limiting использует `RemoteAddr`; proxy headers в MVP не считаются доверенными
- `/health` не зависит от API limiter
- CORS разрешает только origins из `CORS_ALLOWED_ORIGINS`
- Wildcard `*` не используется как допустимый CORS origin
- Внешние JSON-ошибки остаются обобщенными и не раскрывают S3 keys, DSN и stack traces

Переменные окружения:

```text
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:5173
API_RATE_LIMIT_RPS=20
API_RATE_LIMIT_BURST=40
```

## API

### Загрузка аватарки

```http
POST /api/v1/avatars
Content-Type: multipart/form-data
X-User-ID: uuid
```

Поля запроса:

```text
file: binary
```

Ограничения:

- `X-User-ID` обязателен и должен содержать внутренний UUID пользователя
- Пользователь с таким UUID должен существовать и быть активным
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

Ответ `404`:

```json
{
  "error": "User not found"
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
GET /api/v1/avatar?email={email}
```

Query-параметры опционально:

```text
size: string, values: "100x100", "300x300", "original"
format: string, values: "jpeg", "png", "webp"
```

`size` поддерживается для `original`, `100x100` и `300x300`. Если thumbnail еще
не готов, API возвращает `409 Avatar is still processing`.

`format` в MVP не выполняет конвертацию. Запрос разрешен только когда
запрошенный формат совпадает с фактически сохраненным MIME объекта. Иначе API
возвращает `400 Unsupported avatar format`.

Пример:

```http
GET /api/v1/avatars/{avatar_id}?size=300x300&format=png
```

Публичный lookup по email сначала находит активного пользователя через
`users.email`, затем использует внутренний `users.id` для поиска последней
активной avatar:

```http
GET /api/v1/avatar?email=user@example.com&size=100x100
```

Для локального MVP связка `email -> users.id` создается idempotent endpoint:

```http
POST /api/v1/users/resolve
Content-Type: application/json

{
  "email": "user@example.com"
}
```

Ответ:

```json
{
  "id": "uuid",
  "user_id": "uuid",
  "email": "user@example.com",
  "created_at": "2026-06-10T00:00:00Z",
  "updated_at": "2026-06-10T00:00:00Z"
}
```

В production эту связку должен создавать внешний user/profile service или auth
контур. GophProfile хранит `email` как атрибут пользователя, но все защищенные
операции upload/delete/list продолжают работать через внутренний `user_id`.

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

Для публичного lookup по email отсутствующий пользователь или отсутствующая
avatar возвращают стандартную PNG-заглушку с `200 OK`.

Ответ `409`:

```json
{
  "error": "Avatar is still processing"
}
```

### Удаление аватарки

```http
DELETE /api/v1/avatars/{avatar_id}
DELETE /api/v1/users/{user_id}/avatar
X-User-ID: uuid
```

Требования:

- `X-User-ID` обязателен и должен содержать внутренний UUID пользователя
- Мягкое удаление в БД выставляет `deleted_at` и `status = deleting`
- Событие удаления публикуется в Kafka topic `avatar.delete.v1`
- Worker асинхронно удаляет original и thumbnails из S3
- Отсутствующие S3 objects считаются успешным удалением
- После очистки S3 worker выставляет `status = deleted`
- Удалять можно только свои аватарки
- Повторный DELETE по уже удаленной avatar является no-op

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
  "file_name": "avatar.jpg",
  "mime_type": "image/jpeg",
  "size_bytes": 1024000,
  "width": 1920,
  "height": 1080,
  "status": "ready",
  "url": "/api/v1/avatars/{avatar_id}",
  "thumbnails": [
    {
      "size": "100x100",
      "url": "/api/v1/avatars/{avatar_id}?size=100x100"
    },
    {
      "size": "300x300",
      "url": "/api/v1/avatars/{avatar_id}?size=300x300"
    }
  ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

`thumbnails` содержит только уже созданные миниатюры. Для `processing` и
`failed` ответ сохраняет актуальный `status`, но может вернуть пустой список
миниатюр.

### Список аватарок пользователя

```http
GET /api/v1/users/{user_id}/avatars
```

Query-параметры:

```text
limit: integer, default 50, max 100
offset: integer, default 0
```

Ответ `200`:

```json
{
  "items": [
    {
      "id": "uuid",
      "user_id": "uuid",
      "file_name": "avatar.jpg",
      "mime_type": "image/jpeg",
      "size_bytes": 1024000,
      "width": 1920,
      "height": 1080,
      "status": "ready",
      "url": "/api/v1/avatars/{avatar_id}",
      "thumbnails": [],
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z"
    }
  ],
  "limit": 50,
  "offset": 0
}
```

Список фильтрует `deleted_at is null` и сортируется по `created_at desc`.

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

CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    topic TEXT NOT NULL,
    event_key TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX idx_outbox_events_topic_key_unique
    ON outbox_events(topic, event_key);
CREATE INDEX idx_outbox_events_pending_created_at
    ON outbox_events(created_at)
    WHERE status = 'pending';
```

Текущие защищенные API работают с внутренним UUID пользователя из `users.id`.
Email хранится как атрибут пользователя и используется только для публичного
lookup avatar: входной email сопоставляется с `users.id`, после чего API
работает с avatar по внутреннему `user_id`.

После успешной загрузки original в S3 запись `avatars` и событие
`outbox_events` создаются в одной транзакции. API делает best-effort publish в
Kafka после commit. Если Kafka недоступна, outbox событие остается `pending` и
может быть опубликовано повторным publisher-процессом без дублирования записи
avatar.

Worker периодически публикует pending outbox события. Интервал задается через
`OUTBOX_POLL_INTERVAL`, размер пачки через `OUTBOX_BATCH_SIZE`.

## События брокера

```go
type AvatarProcessEvent struct {
    AvatarID          string `json:"avatar_id"`
    UserID            string `json:"user_id"`
    OriginalObjectKey string `json:"original_object_key"`
    Thumb100ObjectKey string `json:"thumb_100_object_key"`
    Thumb300ObjectKey string `json:"thumb_300_object_key"`
    ContentType        string `json:"content_type"`
    Attempt            int    `json:"attempt"`
}

type AvatarDeleteEvent struct {
    AvatarID string `json:"avatar_id"`
    UserID   string `json:"user_id"`
}
```

Kafka topics:

- `avatar.process.v1`
- `avatar.process.retry.1m.v1`
- `avatar.process.retry.5m.v1`
- `avatar.process.retry.30m.v1`
- `avatar.process.dead-letter.v1`
- `avatar.delete.v1`

Требования к идемпотентности:

- Использовать уникальные идентификаторы сообщений
- Проверять статус обработки перед выполнением операций
- Реализовать retry с экспоненциальным backoff
- Использовать `avatar_id` как Kafka message key
- Коммитить offset только после успешной обработки, retry publish или dead-letter publish

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

- `server`
- `worker`
- `postgres`
- `kafka`
- `minio`
- `frontend-build`

Служебный сервис `migrate` применяет SQL-миграции до старта `server` и
`worker`.

Compose env:

```text
DATABASE_URL
S3_ENDPOINT
S3_BUCKET
S3_ACCESS_KEY
S3_SECRET_KEY
S3_USE_PATH_STYLE
KAFKA_BROKERS
KAFKA_CLIENT_ID
KAFKA_CONSUMER_GROUP
HTTP_ADDR
CORS_ALLOWED_ORIGINS
```

При `APP_ENV=local` `server` и `worker` создают MinIO bucket из `S3_BUCKET`,
если он еще отсутствует.

Полный локальный запуск выполняется одной командой:

```bash
./scripts/local-up.sh
```

Скрипт запускает Docker Compose, дожидается readiness ключевых сервисов и
проверяет `server /health`, frontend `/web/` и MinIO bucket.
