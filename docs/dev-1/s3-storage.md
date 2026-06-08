# S3-слой

S3-слой изолирует объектное хранилище от бизнес-логики.

Пакеты API и доменной логики не должны импортировать MinIO SDK или AWS SDK
напрямую. Для работы с объектами используется контракт `internal/storage/s3.Store`.

## Контракт

```go
type Store interface {
    Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
    Get(ctx context.Context, key string) (io.ReadCloser, ObjectMetadata, error)
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
}
```

## Object keys

- Оригинал: `avatars/{user_email}/{avatar_id}/original`
- Миниатюра 100x100: `avatars/{user_email}/{avatar_id}/100x100`
- Миниатюра 300x300: `avatars/{user_email}/{avatar_id}/300x300`

В текущей продуктовой модели `user_id` в БД и API является нормализованным email
пользователя из header `X-User-ID`. В object key используем имя сегмента
`user_email`, чтобы не путать его с внутренним числовым идентификатором.

Перед попаданием в object key email проходит path escaping как отдельный сегмент
пути. Это защищает структуру ключей от символов, которые могут трактоваться как
разделители пути.

Для создания ключей использовать функции:

- `OriginalObjectKey`
- `Thumb100ObjectKey`
- `Thumb300ObjectKey`
- `ThumbnailObjectKey`

## Ошибки

S3 SDK-ошибки мапятся в ошибки S3-слоя:

- `ErrNotFound`
- `ErrInvalidKey`
- `ErrInvalidConfig`

Удаление отсутствующего объекта считается успешным результатом. Это нужно для
идемпотентной обработки повторных задач удаления.
