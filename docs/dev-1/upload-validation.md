# Валидация загрузки avatar

Валидация загрузки выполняется до обращения к S3. Header `X-User-ID`
содержит внутренний UUID пользователя и используется напрямую как `user_id`.

Основная функция: `ValidateAvatarUploadRequest`.

## Правила

- Header `X-User-ID` обязателен
- Значение `X-User-ID` трактуется как внутренний UUID пользователя
- UUID нормализуется через `trim`, parse и canonical lowercase format
- Слой приложения проверяет, что пользователь с этим UUID существует и активен
- Multipart field `file` обязателен
- Пустой файл запрещен
- Максимальный размер файла: `10MB`
- Разрешенные MIME-типы: `image/jpeg`, `image/png`, `image/webp`
- MIME из multipart header должен совпадать с magic bytes файла
- Имя файла сохраняется как metadata, но не используется для S3 object key

## Ошибки

- `400 Missing X-User-ID` для отсутствующего header
- `400 Invalid X-User-ID` для некорректного UUID
- `400 Missing file` для отсутствующего multipart field `file`
- `400 Invalid file format` для пустого файла, неверного MIME, неверных magic
  bytes или несовпадения MIME и содержимого
- `413 File too large` для файла или multipart body больше лимита

## Ограничение памяти

Validator использует:

- `http.MaxBytesReader` для ограничения всего multipart body
- `ParseMultipartForm` с memory limit `1MB`

Это позволяет отсекать слишком большие запросы и не держать крупные файлы
целиком в памяти.

## Решение по идентификатору пользователя

Header `X-User-ID` используется буквально как внутренний `user_id`. Validator
возвращает `UserID`, а слой приложения сохраняет его в таблицу `avatars`,
события worker и S3 object key. Email не участвует в upload-контракте.
