# ТЗ третьего спринта GophProfile

Цель третьего спринта - подготовить GophProfile к запуску в Kubernetes и
упаковать инфраструктурную конфигурацию проекта в Helm Chart.

В уроках основным локальным окружением будет Rancher Desktop, но результат
должен быть переносимым: те же Kubernetes-манифесты и Helm chart должны
подходить для любого совместимого K8s-кластера после настройки values.

## Контекст

GophProfile нужно подготовить к реальным нагрузкам:

- перенести инфраструктуру приложения в Kubernetes;
- описать deployment, service discovery и входящий трафик через стандартные
  ресурсы K8s;
- добавить масштабирование, probes, мониторинг, сетевые ограничения и базовую
  runtime-безопасность;
- завернуть все ресурсы в Helm Chart для удобного управления релизами;
- обновить документацию и схему архитектуры.

## 1. Развернуть базовую инфраструктуру приложения в Kubernetes

Необходимо разработать манифесты для деплоя приложения:

- `Deployment` с настройкой ресурсов и переменных окружения;
- `Service` для внутреннего доступа к подам;
- `Ingress` для маршрутизации внешнего HTTP-трафика;
- `ConfigMap` для нечувствительной конфигурации;
- `Secret` для безопасного хранения секретов.

### Пример Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: avatar-service
spec:
  replicas: 3
  selector:
    matchLabels:
      app: avatar-service
  template:
    metadata:
      labels:
        app: avatar-service
    spec:
      containers:
        - name: server
          image: avatar-service:latest
          ports:
            - containerPort: 8080
          env:
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: avatar-secrets
                  key: database-url
          resources:
            requests:
              memory: "128Mi"
              cpu: "100m"
            limits:
              memory: "512Mi"
              cpu: "500m"
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 30
          readinessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 5
```

### Пример Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: avatar-service
spec:
  selector:
    app: avatar-service
  ports:
    - port: 80
      targetPort: 8080
  type: ClusterIP
```

### Пример Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: avatar-ingress
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "10m"
spec:
  rules:
    - host: avatars.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: avatar-service
                port:
                  number: 80
```

### Пример ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: avatar-config
data:
  max_file_size: "10485760"
  allowed_mime_types: "image/jpeg,image/png,image/webp"
  s3_bucket: "avatars"
```

### Пример Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: avatar-secrets
type: Opaque
data:
  database-url: <base64-encoded>
  s3-access-key: <base64-encoded>
  s3-secret-key: <base64-encoded>
  rabbitmq-url: <base64-encoded>
```

## 2. Обеспечить масштабируемость

Необходимо внедрить горизонтальное автомасштабирование и настроить probes:

- `HorizontalPodAutoscaler` по CPU и RAM;
- `LivenessProbe` для проверки жизнеспособности контейнера;
- `ReadinessProbe` для исключения неготовых подов из балансировки трафика.

### Пример HorizontalPodAutoscaler

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: avatar-service-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: avatar-service
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
```

## 3. Обеспечить мониторинг в Kubernetes

Необходимо создать `ServiceMonitor`, чтобы Prometheus автоматически обнаруживал
поды приложения и собирал метрики с endpoint `/metrics`, реализованного во
втором спринте.

### Пример ServiceMonitor

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: avatar-service
spec:
  selector:
    matchLabels:
      app: avatar-service
  endpoints:
    - port: metrics
      interval: 30s
```

## 4. Обеспечить безопасность

Необходимо настроить сетевые политики и ограничения прав доступа:

- `NetworkPolicy`;
- `RBAC`;
- `ServiceAccount` с минимальными правами;
- `SecurityContext` с non-root пользователем;
- ограничения на уровне pod/container security context.

### Пример NetworkPolicy

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: avatar-service-netpol
spec:
  podSelector:
    matchLabels:
      app: avatar-service
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              name: ingress-nginx
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              name: database
```

### Pod security и RBAC

Требования:

- ограничения на уровне подов;
- service account с минимальными правами;
- `SecurityContext` с non-root пользователем.

Важно: `PodSecurityPolicy` в современных версиях Kubernetes удалён. Для
актуального production-подхода нужно использовать `Pod Security Admission`,
`securityContext`, admission policies и политики кластера вроде Kyverno или
OPA Gatekeeper, если они доступны в окружении.

### Реализация в Helm chart

Chart создаёт `ServiceAccount`, но не создаёт `Role` или `ClusterRole`, потому
что server, worker и migration job не используют Kubernetes API. Это минимальная
RBAC-модель: отдельный identity есть, лишних разрешений нет.

Runtime security задан двумя слоями:

- в Dockerfile image запускается от пользователя `65532:65532`;
- в Helm values заданы `podSecurityContext` и `containerSecurityContext`.

Контейнеры запускаются с `runAsNonRoot`, `seccompProfile: RuntimeDefault`,
`readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false` и `drop:
[ALL]` для Linux capabilities.

NetworkPolicy включается через `networkPolicy.enabled=true`. Для server
разрешён входящий HTTP-трафик от namespace ingress-controller и metrics-трафик
от monitoring namespace. Для worker входящий business HTTP не открыт, разрешён
только metrics-трафик от monitoring namespace. Egress описан отдельным списком
правил в values: DNS, Postgres, Kafka, S3-compatible storage и OTLP. В
production values эти правила нужно сузить через `namespaceSelector`,
`podSelector` или `ipBlock`, потому что стандартный Kubernetes `NetworkPolicy`
не умеет выбирать destination по DNS-имени Service.

## 5. Упаковать проект в Helm Chart

Необходимо упаковать конфигурацию всех компонентов в Helm Chart для удобного
управления релизами:

- templates для всех Kubernetes-ресурсов;
- values-файлы для разных окружений;
- hooks для миграций БД.

## 6. Подготовить проект к production

Необходимо обеспечить graceful shutdown и подготовить итоговую документацию для
сдачи проекта:

- обновить `README.md`;
- описать локальный запуск;
- описать деплой в Kubernetes;
- убедиться, что Swagger/OpenAPI спецификация актуальна;
- добавить схему архитектуры, включая Kubernetes-компоненты.

## Проектная расшифровка для GophProfile

Для текущего проекта это означает, что в Kubernetes нужно отдельно описать как
минимум два application workload:

- `server` - HTTP API на `8080`, metrics endpoint на `9090`;
- `worker` - фоновая обработка Kafka/outbox, metrics endpoint на `9091`.

Внешние зависимости:

- PostgreSQL;
- Kafka;
- S3-compatible storage, локально MinIO;
- OTLP collector или Jaeger endpoint;
- Prometheus Operator для `ServiceMonitor`, если выбран именно этот способ
  service discovery.

Уже реализовано в коде и должно быть использовано в K8s:

- `/health` проверяет Postgres, S3 и Kafka для server;
- `/metrics` публикуется отдельным metrics HTTP server;
- `SIGTERM` обрабатывается через graceful shutdown;
- `HTTP_SHUTDOWN_TIMEOUT` и `WORKER_SHUTDOWN_TIMEOUT` задают timeout остановки;
- конфигурация читается из env;
- миграции лежат в `migrations`;
- Dockerfile собирает оба бинарника: `/app/server` и `/app/worker`.

Главная задача спринта - аккуратно перенести эти контракты в Kubernetes и Helm,
а не менять бизнес-логику сервиса без необходимости.
