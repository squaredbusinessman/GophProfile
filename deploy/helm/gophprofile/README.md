# GophProfile Helm chart

Этот chart деплоит два процесса приложения:

- `server` - HTTP API на named port `http`, метрики на named port `metrics`;
- `worker` - фоновая обработка Kafka/outbox, метрики на named port `metrics`.

Chart не устанавливает PostgreSQL, Kafka, S3-compatible storage, ingress
controller или Prometheus Operator. Эти зависимости считаются внешними для
application chart.

## Архитектура

```mermaid
flowchart TB
  user["User / Client"] --> ingress["Ingress Controller"]
  ingress --> svcServer["Service: gophprofile-server"]
  svcServer --> server["Deployment: server"]

  server --> postgres["External PostgreSQL"]
  server --> kafka["External Kafka"]
  server --> s3["External S3 / MinIO"]
  server --> otlp["Optional OTLP Collector"]

  worker["Deployment: worker"] --> postgres
  worker --> kafka
  worker --> s3
  worker --> otlp

  migrate["Helm hook Job: migrate"] --> postgres

  prometheus["Prometheus Operator / Prometheus"] --> smServer["ServiceMonitor: server"]
  prometheus --> smWorker["ServiceMonitor: worker"]
  smServer --> svcServer
  smWorker --> svcWorker["Service: gophprofile-worker-metrics"]
  svcWorker --> worker

  rules["PrometheusRule"] --> prometheus
```

## Предварительные требования

- Kubernetes cluster.
- Helm 3.
- Image приложения в registry или локальном runtime кластера.
- PostgreSQL с применимыми миграциями или доступом для migration hook.
- Kafka broker.
- S3-compatible storage.
- Prometheus Operator, если нужны `ServiceMonitor` и `PrometheusRule`.

## Сборка image

Для обычного registry:

```bash
docker build -t registry.example.com/gophprofile:0.1.0 .
docker push registry.example.com/gophprofile:0.1.0
```

Для Rancher Desktop локальный Kubernetes часто использует runtime внутри VM.
Чтобы pod увидел локальный image, соберите image внутри Rancher Desktop:

Из корня репозитория:

```bash
rdctl shell docker build -t gophprofile:local "$(pwd)"
```

И используйте:

```yaml
image:
  repository: gophprofile
  tag: local
  pullPolicy: Never
```

## Минимальный install

Перед установкой нужно передать значения, которые зависят от окружения. Без них
pod сможет отрендериться, но приложение не подключится к внешним зависимостям:

- `image.repository`, `image.tag`, `image.pullPolicy`;
- `config.kafka.brokers`;
- `config.s3.endpoint`, `config.s3.bucket`, `config.s3.usePathStyle`;
- `secret.values.databaseUrl` или ключ `DATABASE_URL` в `secret.existingSecret`;
- `secret.values.s3AccessKey` / `secret.values.s3SecretKey` или ключи
  `S3_ACCESS_KEY` / `S3_SECRET_KEY` в `secret.existingSecret`;
- `config.otel.*`, если включён экспорт трассировок;
- `ingress.*`, если API должен быть доступен извне через Ingress.

```bash
helm upgrade --install gophprofile deploy/helm/gophprofile \
  --namespace gophprofile \
  --create-namespace \
  -f values.local.yaml
```

Проверка ресурсов:

```bash
kubectl -n gophprofile get deploy,svc,cm,secret,job
kubectl -n gophprofile get pods
kubectl -n gophprofile describe deploy gophprofile-server
```

Проверка API:

```bash
kubectl -n gophprofile port-forward svc/gophprofile-server 8080:80
curl http://127.0.0.1:8080/health
```

Проверка метрик:

```bash
kubectl -n gophprofile port-forward svc/gophprofile-server 9090:9090
curl http://127.0.0.1:9090/metrics

kubectl -n gophprofile port-forward svc/gophprofile-worker-metrics 9091:9091
curl http://127.0.0.1:9091/metrics
```

## Локальный деплой в Rancher Desktop

В репозитории есть воспроизводимый сценарий, который объединяет результаты всех
трёх спринтов: запускает Rancher Desktop, прикладные зависимости, frontend,
Helm-релиз приложения и полный набор наблюдаемости:

```bash
./scripts/kubernetes-local.sh up
```

Сценарий использует:

- `deploy/kubernetes/local/dependencies.yaml` для PostgreSQL, Kafka, MinIO,
  Jaeger и frontend;
- `deploy/kubernetes/local/gophprofile-values.yaml` для локальной настройки
  application chart;
- `deploy/kubernetes/local/observability.yaml` для Loki, Alloy и Kafka Exporter;
- `deploy/kubernetes/local/alloy.alloy` для безопасного сбора JSON-журналов
  server и worker через Kubernetes API;
- `deploy/kubernetes/local/kube-prometheus-stack-values.yaml` для Prometheus
  Operator, Prometheus, Grafana, Alertmanager и сбора метрик cAdvisor;
- `deploy/observability/grafana/dashboards` для версионируемых панелей Grafana.

Источники данных Grafana имеют стабильные UID `prometheus`, `loki`, `jaeger` и
`alertmanager`. Отдельные метки ConfigMap не позволяют конфигурации другого
Grafana-релиза перезаписать источники или панели GophProfile.

Секрет `gophprofile-local-secrets` создаётся до установки Helm chart. Это
необходимо, потому что migration Job выполняется как `pre-install` hook и должен
получить `DATABASE_URL` до создания обычных ресурсов релиза. Application chart
подключает этот Secret через `secret.existingSecret`.

Сценарий всегда передаёт `--context rancher-desktop` в `kubectl` и
`--kube-context rancher-desktop` в Helm. Глобальный выбранный контекст
пользователя не меняется.

Проверка ресурсов и журналов:

```bash
./scripts/kubernetes-local.sh status
./scripts/kubernetes-local.sh logs --follow
```

Остановка с сохранением данных PostgreSQL, Kafka, MinIO и Loki:

```bash
./scripts/kubernetes-local.sh down
```

Для отладки прикладной части набор наблюдаемости можно не устанавливать:

```bash
./scripts/kubernetes-local.sh up --no-monitoring
```

В этом режиме сценарий явно отключает `ServiceMonitor` и `PrometheusRule`, чтобы
Helm chart устанавливался без соответствующих CRD. Для приёмки объединённого
результата трёх спринтов нужно запускать обычный режим без этого параметра.

## Production values

Минимальный production values должен переопределить:

```yaml
image:
  repository: registry.example.com/gophprofile
  tag: 0.1.0
  pullPolicy: IfNotPresent

config:
  appEnv: production
  logLevel: info
  logFormat: json
  otel:
    enabled: true
    exporterOtlpEndpoint: otel-collector.observability.svc.cluster.local:4317
    exporterOtlpInsecure: false
  kafka:
    brokers: kafka-bootstrap.kafka.svc.cluster.local:9092
    consumerGroup: gophprofile-avatar-worker
  s3:
    endpoint: https://s3.example.com
    bucket: gophprofile-avatars
    usePathStyle: false
    region: us-east-1
  circuitBreaker:
    enabled: true
    failureThreshold: "5"
    openTimeout: 30s

secret:
  values:
    databaseUrl: postgres://user:password@postgres.example.com:5432/gophprofile?sslmode=require
    s3AccessKey: change-me
    s3SecretKey: change-me

server:
  replicaCount: 2
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi

worker:
  replicaCount: 1
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: gophprofile.example.com
      paths:
        - path: /
          pathType: Prefix

serviceMonitor:
  enabled: true
  labels:
    release: kube-prometheus-stack

prometheusRule:
  enabled: true
  labels:
    release: kube-prometheus-stack

networkPolicy:
  enabled: true
```

Для production лучше использовать `secret.existingSecret`, чтобы не хранить
секреты в values-файле:

```yaml
secret:
  create: false
  existingSecret: gophprofile-runtime-secret
```

Secret должен содержать ключи:

- `DATABASE_URL`;
- `S3_ACCESS_KEY`;
- `S3_SECRET_KEY`;
- опционально `VAULT_TOKEN`.

## Monitoring

`ServiceMonitor` и `PrometheusRule` работают только в кластере с Prometheus
Operator. Для kube-prometheus-stack обычно нужны labels:

```yaml
serviceMonitor:
  enabled: true
  labels:
    release: kube-prometheus-stack

prometheusRule:
  enabled: true
  labels:
    release: kube-prometheus-stack
```

Проверка, что operator подхватил ресурсы:

```bash
kubectl -n gophprofile get servicemonitor,prometheusrule
kubectl -n monitoring get prometheus kube-prometheus-stack-prometheus -o yaml
```

## Security

Chart создаёт `ServiceAccount`, но не создаёт `Role` или `ClusterRole`.
Приложение не использует Kubernetes API, поэтому дополнительные RBAC-права не
нужны.

Runtime hardening:

- image запускается от `USER 65532:65532`;
- pod `runAsNonRoot: true`;
- `seccompProfile: RuntimeDefault`;
- `readOnlyRootFilesystem: true`;
- `allowPrivilegeEscalation: false`;
- Linux capabilities dropped через `capabilities.drop: [ALL]`.

`NetworkPolicy` включается через `networkPolicy.enabled=true`. Стандартный
Kubernetes `NetworkPolicy` не умеет выбирать egress destination по DNS-имени
Service, поэтому production values должны сузить egress через
`namespaceSelector`, `podSelector` или `ipBlock`.

## Production readiness

Chart задаёт resource requests/limits для `server`, `worker` и migration job,
а HPA включается через `server.autoscaling.enabled` и
`worker.autoscaling.enabled`. Когда HPA выключен, используется
`replicaCount`, и Kubernetes Service балансирует трафик между готовыми
репликами `server`.

Приложение поддерживает graceful shutdown через `SIGTERM`: HTTP server
завершает входящие запросы в пределах `HTTP_SHUTDOWN_TIMEOUT`, worker ждёт
остановки consumer loop в пределах `WORKER_SHUTDOWN_TIMEOUT`, telemetry
отправляет накопленные spans/metrics перед выходом.

Circuit breaker включается через `config.circuitBreaker.enabled` и защищает
runtime-обращения к PostgreSQL repositories, Kafka producer/healthcheck и
S3-compatible storage. Значения `failureThreshold` и `openTimeout` попадают в
env `CIRCUIT_BREAKER_FAILURE_THRESHOLD` и `CIRCUIT_BREAKER_OPEN_TIMEOUT`.
Бизнес-результаты `not found` не открывают breaker.

## Known limitations

- Chart не устанавливает PostgreSQL, Kafka, S3, ingress controller и Prometheus
  Operator.
- `/health` server проверяет Postgres, Kafka и S3. При деградации внешней
  зависимости liveness probe может вызвать лишние рестарты. Более зрелый
  вариант - добавить отдельный lightweight `/live`.
- Worker readiness/liveness использует `/metrics`. Это допустимый компромисс
  для спринта, но отдельный health endpoint был бы точнее.
- Migration hook выполняет `.up.sql` на `pre-install` и `pre-upgrade`.
  SQL-файлы должны быть идемпотентными.
- NetworkPolicy egress в базовом values описывает порты. Production окружение
  должно сузить destination selectors/IP ranges.
- OpenAPI спецификация находится в `docs/openapi.yaml` и описывает текущий
  REST API без внутреннего worker/Kafka контракта.
