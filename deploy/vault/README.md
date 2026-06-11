# Vault для секретов GophProfile

Vault используется как единое хранилище чувствительных данных проекта:

- `DATABASE_URL`
- `S3_ACCESS_KEY`
- `S3_SECRET_KEY`
- `KAFKA_BROKERS`
- другие секреты сервисов по мере появления

## Принятый вариант для проекта

Для учебного проекта Vault хранит свои зашифрованные данные в PostgreSQL через
storage backend `postgresql`.

Важное ограничение: PostgreSQL storage backend для Vault является community
supported. В production HashiCorp чаще рекомендует integrated storage, но здесь
PostgreSQL выбран осознанно, чтобы изучить связку Vault и PostgreSQL.

Официальная документация:

- [PostgreSQL storage backend](https://developer.hashicorp.com/vault/docs/configuration/storage/postgresql)
- [Storage backend overview](https://developer.hashicorp.com/vault/docs/configuration/storage)

## Локальный запуск

Локально Vault запускается сразу в целевом для проекта режиме с PostgreSQL
storage backend.

```bash
docker compose -f deploy/vault/docker-compose.yml up -d
```

После первого запуска Vault нужно инициализировать и распечатать unseal keys:

```bash
vault operator init
vault operator unseal
vault login
```

## Файлы

- `vault.hcl` - конфигурация Vault с PostgreSQL storage backend
- `postgresql-storage-schema.sql` - таблицы, которые Vault требует в PostgreSQL
- `gophprofile-policy.hcl` - минимальная policy для чтения секретов приложения
- `docker-compose.yml` - локальный запуск Vault и PostgreSQL storage backend

KV v2 engine для приложения:

```bash
vault secrets enable -path=secret kv-v2
vault policy write gophprofile deploy/vault/gophprofile-policy.hcl
```

## Bootstrap env

Приложению нужны только bootstrap-переменные для доступа к Vault:

```bash
VAULT_ENABLED=true
VAULT_ADDR=http://localhost:8200
VAULT_TOKEN=dev-root-token
VAULT_KV_MOUNT=secret
VAULT_SERVICE_PATH=gophprofile
```

Сами чувствительные значения должны лежать в Vault, а не в `.env`:

```bash
vault kv put secret/gophprofile \
  DATABASE_URL='postgres://gophprofile:gophprofile@postgres:5432/gophprofile?sslmode=disable' \
  S3_ACCESS_KEY='minioadmin' \
  S3_SECRET_KEY='minioadmin' \
  KAFKA_BROKERS='kafka:9092'
```

## Что нельзя делать

- Не коммитить реальные root tokens
- Не коммитить unseal keys
- Не хранить production-секреты в `.env`
- Не логировать значения секретов
