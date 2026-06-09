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

- Оригинал: `avatars/{user_id}/{avatar_id}/original`
- Миниатюра 100x100: `avatars/{user_id}/{avatar_id}/100x100`
- Миниатюра 300x300: `avatars/{user_id}/{avatar_id}/300x300`

В текущей продуктовой модели `user_id` является внутренним UUID пользователя.
Email используется только для публичного поиска пользователя, а не для S3 object
key.

Перед попаданием в object key каждый сегмент проходит path escaping. Это
защищает структуру ключей от символов, которые могут трактоваться как
разделители пути, даже если в helper случайно попадет небезопасное значение.

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
