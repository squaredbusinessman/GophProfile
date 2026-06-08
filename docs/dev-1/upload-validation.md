# Валидация загрузки avatar

Валидация загрузки выполняется до обращения к S3 и БД.

Основная функция: `ValidateAvatarUploadRequest`.

## Правила

- Header `X-User-ID` обязателен
- Значение `X-User-ID` трактуется как email пользователя
- Email нормализуется через `trim` и `lowercase`
- Максимальная длина email: `254` символа
- Email должен содержать local-part, `@` и домен с точкой
- Multipart field `file` обязателен
- Пустой файл запрещен
- Максимальный размер файла: `10MB`
- Разрешенные MIME-типы: `image/jpeg`, `image/png`, `image/webp`
- MIME из multipart header должен совпадать с magic bytes файла
- Имя файла сохраняется как metadata, но не используется для S3 object key

## Ошибки

- `400 Missing X-User-ID` для отсутствующего header
- `400 Invalid X-User-ID` для некорректного email
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

В ТЗ header называется `X-User-ID`, но продуктовая модель GophProfile
привязывает avatar к email пользователя. Поэтому в рамках MVP сохраняем имя
header из ТЗ, но валидируем его значение именно как email.
