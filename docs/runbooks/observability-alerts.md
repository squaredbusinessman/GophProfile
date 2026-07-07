# Observability alerts

## HighErrorRate

Смысл: больше 10% HTTP-запросов к server завершаются ответами 5xx

Что проверить:

- открыть Service Overview и посмотреть маршруты с ростом 5xx
- по `trace_id` из error log открыть связанный trace в Jaeger
- проверить последние изменения в server и доступность PostgreSQL, S3, Kafka

## HighResponseTime

Смысл: p95 времени ответа HTTP API держится выше 5 секунд

Что проверить:

- сравнить HTTP latency с DB, S3 и Kafka latency на Dependencies and Resources
- проверить рост in-flight requests
- проверить saturation пула PostgreSQL и состояние контейнера server

## OutboxBacklogGrowing

Смысл: количество ожидающих событий outbox быстро растёт

Что проверить:

- убедиться, что worker жив и собирается Prometheus
- проверить Kafka produce errors и consumer lag
- посмотреть последние ошибки `outbox event publish failed` в Loki

## OutboxEventTooOld

Смысл: старейшее pending событие outbox ждёт публикации больше 5 минут

Что проверить:

- проверить доступность Kafka
- проверить ошибки публикации background outbox
- убедиться, что worker обрабатывает фоновый цикл публикации

## KafkaConsumerLagHigh

Смысл: consumer group отстаёт от topic больше чем на 100 сообщений

Что проверить:

- проверить, работает ли worker
- сравнить Kafka consume rate и processing duration
- проверить retry и dead-letter события обработки

## WorkerDown

Смысл: Prometheus не может собрать метрики worker больше 1 минуты

Что проверить:

- открыть `docker compose ps worker`
- проверить логи worker
- проверить, что metrics server worker слушает порт внутри compose network

## DatabasePoolSaturated

Смысл: занято больше 90% ограниченного пула соединений PostgreSQL

Что проверить:

- посмотреть DB query latency и error rate
- проверить медленные операции репозиториев
- оценить, не зависли ли запросы или транзакции

## AvatarDeadLetterDetected

Смысл: обработка аватара исчерпала попытки и дошла до dead-letter результата

Что проверить:

- найти error log worker с тем же trace
- проверить причину ошибки обработки изображения
- убедиться, что payload и object key не попали в logs или metrics
