# Этап 6. Runtime и сетевая безопасность

## Что внедрено

- `ServiceAccount` уже создается chart-ом через `templates/serviceaccount.yaml`.
- `Role` и `ClusterRole` не добавлены, потому что server, worker и migration job не обращаются к Kubernetes API.
- `podSecurityContext` задан в `values.yaml` и применяется к server, worker и migration job:
  - `runAsNonRoot: true`;
  - `runAsUser: 65532`;
  - `runAsGroup: 65532`;
  - `fsGroup: 65532`;
  - `seccompProfile.type: RuntimeDefault`.
- `containerSecurityContext` задан в `values.yaml`:
  - `allowPrivilegeEscalation: false`;
  - `readOnlyRootFilesystem: true`;
  - `capabilities.drop: [ALL]`.
- Dockerfile теперь тоже задает non-root пользователя `65532:65532`, чтобы image и Kubernetes security context были согласованы.
- Добавлены `NetworkPolicy` templates:
  - `networkpolicy-server.yaml`;
  - `networkpolicy-worker.yaml`.

## Как работают NetworkPolicy

`NetworkPolicy` включается через:

```yaml
networkPolicy:
  enabled: true
```

При включении server pod получает:

- ingress на named port `http` только от namespace ingress-controller;
- ingress на named port `metrics` только от monitoring namespace;
- egress по явно описанным правилам из `networkPolicy.egress`.

Worker pod получает:

- ingress только на named port `metrics` от monitoring namespace;
- egress по тем же явно описанным правилам.

## Важное ограничение Kubernetes

Обычный `NetworkPolicy` не умеет разрешать egress по DNS-имени Kubernetes Service, например `postgres.default.svc.cluster.local`.

Поэтому production values должны сужать egress одним из способов:

- `namespaceSelector` + `podSelector`, если зависимость находится в Kubernetes;
- `ipBlock`, если зависимость внешняя и имеет стабильные CIDR/IP;
- CNI-специфичные политики, например Cilium FQDN policy, если нужно разрешать egress по hostname.

В базовом chart egress описан по портам:

- DNS: `53/UDP`, `53/TCP`;
- Postgres: `5432/TCP`;
- Kafka: `9092/TCP`;
- S3-compatible storage: `9000/TCP`, `443/TCP`;
- OTLP: `4317/TCP`, `4318/TCP`.

Для строгого production окружения эти правила нужно переопределить в values-файле окружения и добавить `to`.

## Проверки

Выполнены:

```bash
helm lint deploy/helm/gophprofile
helm template gophprofile deploy/helm/gophprofile -n gophprofile --set networkPolicy.enabled=true
kubectl apply --dry-run=client -f /tmp/gophprofile-netpol-render.yaml
```

Результат: chart валиден, `NetworkPolicy` объекты принимаются Kubernetes dry-run.
