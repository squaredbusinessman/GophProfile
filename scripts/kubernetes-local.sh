#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
DEPENDENCIES_FILE="$ROOT_DIR/deploy/kubernetes/local/dependencies.yaml"
APP_VALUES_FILE="$ROOT_DIR/deploy/kubernetes/local/gophprofile-values.yaml"
MONITORING_VALUES_FILE="$ROOT_DIR/deploy/kubernetes/local/kube-prometheus-stack-values.yaml"
OBSERVABILITY_FILE="$ROOT_DIR/deploy/kubernetes/local/observability.yaml"
ALLOY_CONFIG_FILE="$ROOT_DIR/deploy/kubernetes/local/alloy.alloy"
LOKI_CONFIG_FILE="$ROOT_DIR/deploy/observability/loki.yml"
DASHBOARDS_DIR="$ROOT_DIR/deploy/observability/grafana/dashboards"
CHART_DIR="$ROOT_DIR/deploy/helm/gophprofile"

KUBE_CONTEXT="${KUBE_CONTEXT:-rancher-desktop}"
NAMESPACE="${KUBE_NAMESPACE:-gophprofile}"
RELEASE="${HELM_RELEASE:-gophprofile}"
MONITORING_NAMESPACE="${MONITORING_NAMESPACE:-monitoring}"
MONITORING_RELEASE="${MONITORING_RELEASE:-gophprofile-monitoring}"
PROMETHEUS_STACK_VERSION="${PROMETHEUS_STACK_VERSION:-87.15.1}"
APP_IMAGE="${APP_IMAGE:-gophprofile:local}"
FRONTEND_IMAGE="${FRONTEND_IMAGE:-gophprofile-frontend:local}"
KUBERNETES_TIMEOUT_SECONDS="${KUBERNETES_TIMEOUT_SECONDS:-600}"
HELM_TIMEOUT="${HELM_TIMEOUT:-10m}"
DOCKER_BUILD_ATTEMPTS="${DOCKER_BUILD_ATTEMPTS:-3}"
RANCHER_MEMORY_GB="${RANCHER_MEMORY_GB:-6}"
RANCHER_CPUS="${RANCHER_CPUS:-4}"

API_PORT="${API_PORT:-8080}"
MINIO_CONSOLE_PORT="${MINIO_CONSOLE_PORT:-9001}"
JAEGER_PORT="${JAEGER_PORT:-16686}"
PROMETHEUS_PORT="${PROMETHEUS_PORT:-9090}"
GRAFANA_PORT="${GRAFANA_PORT:-3001}"
LOKI_PORT="${LOKI_PORT:-3100}"
ALERTMANAGER_PORT="${ALERTMANAGER_PORT:-9093}"
ALLOY_PORT="${ALLOY_PORT:-12345}"
STATE_DIR="${TMPDIR:-/tmp}/gophprofile-kubernetes-$(id -u)"

ACTION=up
BUILD_IMAGE=1
INSTALL_MONITORING=1
START_PORT_FORWARDS=1
FOLLOW_LOGS=0
CLEANUP_PORT_FORWARDS=0

# Выводит информационное сообщение в стандартный поток
log() {
	printf '%s\n' "$*"
}

# Выводит сообщение об ошибке и завершает сценарий
die() {
	log "ОШИБКА: $*"
	exit 1
}

# Показывает команды, параметры и переменные окружения сценария
usage() {
	cat <<'EOF'
Использование:
  scripts/kubernetes-local.sh [up|down|status|logs] [параметры]

Команды:
  up                 запустить объединённый стенд трёх спринтов
  down               остановить стенд с сохранением локальных данных
  status             показать состояние ресурсов
  logs               показать журналы server и worker

Параметры:
  --no-build         использовать уже собранный образ gophprofile:local
  --no-monitoring    не устанавливать средства наблюдаемости
  --no-forward       не открывать локальные порты через kubectl port-forward
  --follow           продолжать вывод журналов для команды logs
  -h, --help         показать эту справку

Переменные окружения:
  KUBE_CONTEXT                  контекст Kubernetes, по умолчанию rancher-desktop
  KUBE_NAMESPACE                пространство приложения, по умолчанию gophprofile
  PROMETHEUS_STACK_VERSION      версия kube-prometheus-stack, по умолчанию 87.15.1
  API_PORT                      локальный порт API, по умолчанию 8080
  MINIO_CONSOLE_PORT            локальный порт MinIO Console, по умолчанию 9001
  JAEGER_PORT                   локальный порт Jaeger UI, по умолчанию 16686
  PROMETHEUS_PORT               локальный порт Prometheus, по умолчанию 9090
  GRAFANA_PORT                  локальный порт Grafana, по умолчанию 3001
  LOKI_PORT                     локальный порт Loki, по умолчанию 3100
  ALERTMANAGER_PORT             локальный порт Alertmanager, по умолчанию 9093
  ALLOY_PORT                    локальный порт Alloy, по умолчанию 12345
  KUBERNETES_TIMEOUT_SECONDS    ожидание Kubernetes, по умолчанию 600
  HELM_TIMEOUT                  ожидание Helm-релиза, по умолчанию 10m
  DOCKER_BUILD_ATTEMPTS         число попыток сборки образа, по умолчанию 3
  RANCHER_MEMORY_GB             минимум памяти Rancher Desktop, по умолчанию 6
  RANCHER_CPUS                  минимум CPU Rancher Desktop, по умолчанию 4
EOF
}

# Проверяет наличие обязательной команды в PATH
require_command() {
	command -v "$1" >/dev/null 2>&1 || die "не найдена команда $1"
}

# Выполняет kubectl в настроенном контексте Kubernetes
kube() {
	kubectl --context "$KUBE_CONTEXT" "$@"
}

# Выполняет Helm в настроенном контексте Kubernetes
helm_kube() {
	helm --kube-context "$KUBE_CONTEXT" "$@"
}

# Разбирает команду и параметры запуска
parse_arguments() {
	if [ "$#" -gt 0 ]; then
		case "$1" in
			up | down | status | logs)
				ACTION=$1
				shift
				;;
		esac
	fi

	while [ "$#" -gt 0 ]; do
		case "$1" in
			--no-build)
				BUILD_IMAGE=0
				;;
			--no-monitoring)
				INSTALL_MONITORING=0
				;;
			--no-forward)
				START_PORT_FORWARDS=0
				;;
			--follow)
				FOLLOW_LOGS=1
				;;
			-h | --help)
				usage
				exit 0
				;;
			*)
				die "неизвестный параметр $1"
				;;
		esac
		shift
	done
}

# Ожидает готовность API и всех узлов Kubernetes
wait_for_kubernetes() {
	deadline=$(( $(date +%s) + KUBERNETES_TIMEOUT_SECONDS ))
	while [ "$(date +%s)" -le "$deadline" ]; do
		if kubernetes_is_ready; then
			log "Kubernetes готов"
			return 0
		fi
		sleep 2
	done

	return 1
}

# Проверяет готовность API и узлов Kubernetes одним запросом
kubernetes_is_ready() {
	kube get --raw=/readyz --request-timeout=5s >/dev/null 2>&1 &&
		kube wait node --all --for=condition=Ready --timeout=5s >/dev/null 2>&1
}

# Обеспечивает минимальные ресурсы виртуальной машины Rancher Desktop
ensure_rancher_resources() {
	require_command jq
	if ! settings="$(rdctl list-settings 2>/dev/null)"; then
		log "Запускаю Rancher Desktop с Kubernetes и Moby"
		rdctl start \
			--kubernetes.enabled=true \
			--container-engine.name=moby \
			--no-modal-dialogs
		wait_for_kubernetes || die "Kubernetes не стал готов за ${KUBERNETES_TIMEOUT_SECONDS} секунд"
		settings="$(rdctl list-settings)"
	fi
	current_memory="$(printf '%s' "$settings" | jq -r '.virtualMachine.memoryInGB')"
	current_cpus="$(printf '%s' "$settings" | jq -r '.virtualMachine.numberCPUs')"

	if [ "$current_memory" -ge "$RANCHER_MEMORY_GB" ] && \
		[ "$current_cpus" -ge "$RANCHER_CPUS" ]; then
		return
	fi

	log "Увеличиваю ресурсы Rancher Desktop до ${RANCHER_MEMORY_GB} ГБ и ${RANCHER_CPUS} CPU"
	rdctl set \
		--virtual-machine.memory-in-gb "$RANCHER_MEMORY_GB" \
		--virtual-machine.number-cpus "$RANCHER_CPUS"

	log "Перезапускаю Rancher Desktop после изменения ресурсов"
	rdctl shutdown
	rdctl start \
		--kubernetes.enabled=true \
		--container-engine.name=moby \
		--no-modal-dialogs

	wait_for_kubernetes || die "Kubernetes не восстановился после изменения ресурсов"
}

# Запускает Kubernetes Rancher Desktop и дожидается его готовности
ensure_kubernetes() {
	require_command rdctl
	require_command kubectl
	ensure_rancher_resources

	if kubernetes_is_ready; then
		log "Kubernetes уже запущен"
		return
	fi

	log "Запускаю Rancher Desktop с Kubernetes и Moby"
	rdctl start \
		--kubernetes.enabled=true \
		--container-engine.name=moby \
		--no-modal-dialogs

	wait_for_kubernetes || die "Kubernetes не стал готов за ${KUBERNETES_TIMEOUT_SECONDS} секунд"
}

# Выводит состояние ресурсов, события и последние журналы при ошибке
print_diagnostics() {
	log ""
	log "Состояние ресурсов gophprofile:"
	kube -n "$NAMESPACE" get pods,deployments,services,jobs,hpa 2>/dev/null || true
	log ""
	log "Последние события gophprofile:"
	kube -n "$NAMESPACE" get events --sort-by=.metadata.creationTimestamp 2>/dev/null | tail -n 40 || true
	log ""
	log "Последние журналы server и worker:"
	kube -n "$NAMESPACE" logs deployment/gophprofile-server --all-containers --tail=40 2>/dev/null || true
	kube -n "$NAMESPACE" logs deployment/gophprofile-worker --all-containers --tail=40 2>/dev/null || true
	log ""
	log "Состояние ресурсов monitoring:"
	kube -n "$MONITORING_NAMESPACE" get pods,deployments,services,statefulsets 2>/dev/null || true
}

# Применяет манифесты PostgreSQL, Kafka, MinIO, frontend и Jaeger
apply_dependencies() {
	log "Применяю локальные зависимости"
	if ! kube apply --validate=false -f "$DEPENDENCIES_FILE"; then
		print_diagnostics
		die "не удалось применить манифест зависимостей"
	fi
	delete_stopped_pods "$NAMESPACE" "app.kubernetes.io/part-of=gophprofile-local"
}

# Удаляет завершённые Pod выбранной группы без ожидания контроллера
delete_stopped_pods() {
	namespace=$1
	labels=$2
	stopped_pods="$(kube -n "$namespace" get pods \
		-l "$labels" \
		-o json | jq -r '
			.items[]
			| select(
				.status.phase == "Failed"
				or any(.status.containerStatuses[]?; .state.terminated != null)
			)
			| "pod/\(.metadata.name)"
		')"
	[ -z "$stopped_pods" ] || kube -n "$namespace" delete \
		--wait=false \
		$stopped_pods >/dev/null
}

# Создаёт или обновляет ConfigMap из одного локального файла
apply_config_map_from_file() {
	namespace=$1
	name=$2
	key=$3
	path=$4

	kube -n "$namespace" create configmap "$name" \
		--from-file="$key=$path" \
		--dry-run=client \
		-o yaml | kube apply --validate=false -f - >/dev/null
}

# Применяет Loki, Alloy, Kafka Exporter и конфигурацию панелей Grafana
apply_observability() {
	[ "$INSTALL_MONITORING" = "1" ] || return 0

	log "Подготавливаю конфигурацию Loki, Alloy и Grafana"
	kube create namespace "$MONITORING_NAMESPACE" \
		--dry-run=client \
		-o yaml | kube apply --validate=false -f - >/dev/null

	apply_config_map_from_file "$MONITORING_NAMESPACE" \
		gophprofile-loki-config loki.yml "$LOKI_CONFIG_FILE"
	apply_config_map_from_file "$MONITORING_NAMESPACE" \
		gophprofile-alloy-config config.alloy "$ALLOY_CONFIG_FILE"

	for dashboard_path in "$DASHBOARDS_DIR"/*.json; do
		dashboard_file="$(basename "$dashboard_path")"
		dashboard_name="gophprofile-dashboard-${dashboard_file%.json}"
		apply_config_map_from_file "$MONITORING_NAMESPACE" \
			"$dashboard_name" "$dashboard_file" "$dashboard_path"
		kube -n "$MONITORING_NAMESPACE" label configmap "$dashboard_name" \
			gophprofile_dashboard=1 \
			--overwrite >/dev/null
	done

	if ! kube apply --validate=false -f "$OBSERVABILITY_FILE"; then
		print_diagnostics
		die "не удалось применить Loki, Alloy и Kafka exporter"
	fi
	delete_stopped_pods "$MONITORING_NAMESPACE" \
		"app.kubernetes.io/part-of=gophprofile-local"

	for deployment in \
		gophprofile-loki \
		gophprofile-alloy \
		gophprofile-kafka-exporter
	do
		log "Ожидаю готовность deployment/$deployment"
		if ! kube -n "$MONITORING_NAMESPACE" rollout status "deployment/$deployment" \
			--timeout="${KUBERNETES_TIMEOUT_SECONDS}s"; then
			print_diagnostics
			die "deployment/$deployment не стал готов"
		fi
	done
}

# Устанавливает Prometheus, Grafana и Alertmanager через Helm
install_monitoring() {
	[ "$INSTALL_MONITORING" = "1" ] || return 0

	require_command helm
	log "Обновляю репозиторий prometheus-community"
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts \
		--force-update >/dev/null
	helm repo update prometheus-community >/dev/null

	log "Устанавливаю Prometheus, Grafana и Alertmanager"
	if ! helm_kube upgrade --install "$MONITORING_RELEASE" \
		prometheus-community/kube-prometheus-stack \
		--version "$PROMETHEUS_STACK_VERSION" \
		--namespace "$MONITORING_NAMESPACE" \
		--create-namespace \
		--values "$MONITORING_VALUES_FILE" \
		--wait \
		--timeout "$HELM_TIMEOUT"; then
		print_diagnostics
		die "не удалось установить kube-prometheus-stack"
	fi

	kube wait --for=condition=Established \
		crd/servicemonitors.monitoring.coreos.com \
		crd/prometheusrules.monitoring.coreos.com \
		--timeout=120s >/dev/null

	log "Проверяю готовность Prometheus и Alertmanager"
	kube -n "$MONITORING_NAMESPACE" wait \
		--for=condition=Available \
		"prometheus/$MONITORING_RELEASE-prometheus" \
		--timeout="${KUBERNETES_TIMEOUT_SECONDS}s" >/dev/null || {
		print_diagnostics
		die "Prometheus не стал готов"
	}
	kube -n "$MONITORING_NAMESPACE" wait \
		--for=condition=Available \
		"alertmanager/$MONITORING_RELEASE-alertmanager" \
		--timeout="${KUBERNETES_TIMEOUT_SECONDS}s" >/dev/null || {
		print_diagnostics
		die "Alertmanager не стал готов"
	}
}

# Собирает контейнерный образ внутри виртуальной машины Rancher Desktop
build_image() {
	image=$1
	context=$2
	attempt=1

	while [ "$attempt" -le "$DOCKER_BUILD_ATTEMPTS" ]; do
		log "Собираю образ $image, попытка $attempt из $DOCKER_BUILD_ATTEMPTS"
		if rdctl shell docker build \
			--progress=plain \
			--tag "$image" \
			"$context"; then
			return
		fi

		attempt=$((attempt + 1))
		[ "$attempt" -le "$DOCKER_BUILD_ATTEMPTS" ] && sleep 2
	done

	die "не удалось собрать образ $image"
}

# Собирает образы backend и frontend при включённой сборке
build_application_image() {
	[ "$BUILD_IMAGE" = "1" ] || return 0

	build_image "$APP_IMAGE" "$ROOT_DIR"
	build_image "$FRONTEND_IMAGE" "$ROOT_DIR/web/frontend"
}

# Ожидает готовность всех прикладных зависимостей
wait_for_dependencies() {
	for deployment in \
		gophprofile-postgres \
		gophprofile-kafka \
		gophprofile-minio \
		gophprofile-frontend \
		gophprofile-jaeger
	do
		log "Ожидаю готовность deployment/$deployment"
		if ! kube -n "$NAMESPACE" rollout status "deployment/$deployment" \
			--timeout="${KUBERNETES_TIMEOUT_SECONDS}s"; then
			print_diagnostics
			die "deployment/$deployment не стал готов"
		fi
	done
}

# Проверяет и создаёт темы Kafka за одну попытку
create_kafka_topics_once() {
	kube --request-timeout=90s -n "$NAMESPACE" exec deployment/gophprofile-kafka -- \
		sh -ec '
			existing_topics="$(
				timeout 30s /opt/kafka/bin/kafka-topics.sh \
					--bootstrap-server gophprofile-kafka:9092 \
					--list
			)"
			for topic do
				if printf "%s\n" "$existing_topics" | grep -Fxq "$topic"; then
					continue
				fi
				timeout 30s /opt/kafka/bin/kafka-topics.sh \
					--bootstrap-server gophprofile-kafka:9092 \
					--create \
					--if-not-exists \
					--topic "$topic" \
					--partitions 3 \
					--replication-factor 1 >/dev/null
			done
		' sh \
		avatar.process.v1 \
		avatar.process.retry.1m.v1 \
		avatar.process.retry.5m.v1 \
		avatar.process.retry.30m.v1 \
		avatar.process.dead-letter.v1 \
		avatar.delete.v1
}

# Повторяет создание тем Kafka до готовности брокера
create_kafka_topics() {
	log "Создаю Kafka topics"
	attempt=1
	while [ "$attempt" -le 30 ]; do
		if create_kafka_topics_once; then
			log "Kafka topics готовы"
			return
		fi
		attempt=$((attempt + 1))
		sleep 2
	done

	print_diagnostics
	die "не удалось создать Kafka topics"
}

# Устанавливает или обновляет Helm-релиз server и worker
install_application() {
	require_command helm
	build_id="$(date -u +%Y%m%dT%H%M%SZ)"

	set -- upgrade --install "$RELEASE" "$CHART_DIR" \
		--namespace "$NAMESPACE" \
		--create-namespace \
		--values "$APP_VALUES_FILE" \
		--set-string "podAnnotations.gophprofile\\.io/local-build=$build_id" \
		--wait \
		--timeout "$HELM_TIMEOUT"

	if [ "$INSTALL_MONITORING" = "0" ]; then
		set -- "$@" \
			--set serviceMonitor.enabled=false \
			--set prometheusRule.enabled=false
	fi

	log "Устанавливаю Helm-релиз приложения"
	if ! helm_kube "$@"; then
		print_diagnostics
		die "не удалось установить Helm-релиз $RELEASE"
	fi
}

# Останавливает один ранее запущенный проброс порта
stop_port_forward() {
	name=$1
	pid_file="$STATE_DIR/$name.pid"
	[ -f "$pid_file" ] || return 0

	pid="$(cat "$pid_file")"
	if kill -0 "$pid" 2>/dev/null; then
		command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
		case "$command_line" in
			*kubectl*port-forward*)
				kill "$pid" 2>/dev/null || true
				;;
		esac
	fi
	rm -f "$pid_file"
}

# Останавливает все пробросы портов этого сценария
stop_port_forwards() {
	stop_port_forward frontend
	stop_port_forward minio
	stop_port_forward jaeger
	stop_port_forward prometheus
	stop_port_forward grafana
	stop_port_forward loki
	stop_port_forward alertmanager
	stop_port_forward alloy
}

# Очищает частично созданные пробросы портов после ошибки
cleanup_on_exit() {
	status=$?
	trap - 0
	if [ "$status" -ne 0 ] && [ "$CLEANUP_PORT_FORWARDS" = "1" ]; then
		stop_port_forwards
	fi
	exit "$status"
}

# Проверяет что локальный порт свободен
assert_port_available() {
	port=$1
	if command -v lsof >/dev/null 2>&1 && \
		lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
		die "локальный порт $port уже занят"
	fi
}

# Запускает проброс порта и дожидается готовности локального адреса
start_port_forward() {
	name=$1
	namespace=$2
	resource=$3
	mapping=$4
	readiness_url=$5
	local_port=${mapping%%:*}
	pid_file="$STATE_DIR/$name.pid"
	log_file="$STATE_DIR/$name.log"

	stop_port_forward "$name"
	assert_port_available "$local_port"

	nohup kubectl --context "$KUBE_CONTEXT" \
		-n "$namespace" \
		port-forward \
		--address 127.0.0.1 \
		"$resource" \
		"$mapping" >"$log_file" 2>&1 &
	pid=$!
	printf '%s\n' "$pid" >"$pid_file"

	attempt=1
	while [ "$attempt" -le 60 ]; do
		if ! kill -0 "$pid" 2>/dev/null; then
			log "Журнал port-forward $name:"
			cat "$log_file" 2>/dev/null || true
			die "port-forward $name завершился раньше времени"
		fi
		if curl -fsS --connect-timeout 2 --max-time 3 \
			"$readiness_url" >/dev/null 2>&1; then
			log "Локальный адрес готов: $readiness_url"
			return
		fi
		attempt=$((attempt + 1))
		sleep 1
	done

	die "локальный адрес $readiness_url не стал готов"
}

# Находит первый Service по ограниченному набору меток
find_service_by_labels() {
	namespace=$1
	labels=$2
	kube -n "$namespace" get service \
		-l "$labels" \
		-o name | sed -n '1p'
}

# Открывает локальные адреса приложения и средств наблюдаемости
start_port_forwards() {
	[ "$START_PORT_FORWARDS" = "1" ] || return 0
	require_command curl
	mkdir -p "$STATE_DIR"
	CLEANUP_PORT_FORWARDS=1

	start_port_forward frontend "$NAMESPACE" service/gophprofile-frontend \
		"$API_PORT:80" "http://127.0.0.1:$API_PORT/health"
	start_port_forward minio "$NAMESPACE" service/gophprofile-minio \
		"$MINIO_CONSOLE_PORT:9001" "http://127.0.0.1:$MINIO_CONSOLE_PORT/"
	start_port_forward jaeger "$NAMESPACE" service/gophprofile-jaeger \
		"$JAEGER_PORT:16686" "http://127.0.0.1:$JAEGER_PORT/"

	if [ "$INSTALL_MONITORING" = "1" ]; then
		prometheus_service="$(find_service_by_labels "$MONITORING_NAMESPACE" \
			"app.kubernetes.io/instance=$MONITORING_RELEASE,app=kube-prometheus-stack-prometheus")"
		[ -n "$prometheus_service" ] || die "не найден Service Prometheus"
		start_port_forward prometheus "$MONITORING_NAMESPACE" \
			"$prometheus_service" \
			"$PROMETHEUS_PORT:9090" \
			"http://127.0.0.1:$PROMETHEUS_PORT/-/ready"

		grafana_service="$(find_service_by_labels "$MONITORING_NAMESPACE" \
			"app.kubernetes.io/instance=$MONITORING_RELEASE,app.kubernetes.io/name=grafana")"
		[ -n "$grafana_service" ] || die "не найден Service Grafana"
		start_port_forward grafana "$MONITORING_NAMESPACE" \
			"$grafana_service" \
			"$GRAFANA_PORT:80" \
			"http://127.0.0.1:$GRAFANA_PORT/api/health"

		alertmanager_service="$(find_service_by_labels "$MONITORING_NAMESPACE" \
			"app.kubernetes.io/instance=$MONITORING_RELEASE,app=kube-prometheus-stack-alertmanager")"
		[ -n "$alertmanager_service" ] || die "не найден Service Alertmanager"
		start_port_forward alertmanager "$MONITORING_NAMESPACE" \
			"$alertmanager_service" \
			"$ALERTMANAGER_PORT:9093" \
			"http://127.0.0.1:$ALERTMANAGER_PORT/-/ready"

		start_port_forward loki "$MONITORING_NAMESPACE" service/gophprofile-loki \
			"$LOKI_PORT:3100" "http://127.0.0.1:$LOKI_PORT/ready"
		start_port_forward alloy "$MONITORING_NAMESPACE" service/gophprofile-alloy \
			"$ALLOY_PORT:12345" "http://127.0.0.1:$ALLOY_PORT/-/ready"
	fi
	CLEANUP_PORT_FORWARDS=0
}

# Показывает ресурсы приложения, наблюдаемости и Helm-релизы
show_status() {
	ensure_kubernetes
	log "Ресурсы приложения и зависимостей:"
	kube -n "$NAMESPACE" get pods,deployments,services,hpa,ingress
	if kube get namespace "$MONITORING_NAMESPACE" >/dev/null 2>&1; then
		log ""
		log "Ресурсы наблюдаемости:"
		kube -n "$MONITORING_NAMESPACE" get pods,deployments,statefulsets,services,persistentvolumeclaims
	fi
	log ""
	log "Helm-релизы:"
	helm_kube list --all-namespaces
	if kube get crd servicemonitors.monitoring.coreos.com >/dev/null 2>&1; then
		log ""
		log "Ресурсы мониторинга приложения:"
		kube -n "$NAMESPACE" get servicemonitor,prometheusrule
	fi
}

# Показывает объединённые журналы server и worker
show_logs() {
	ensure_kubernetes
	set -- -n "$NAMESPACE" logs \
		-l "app.kubernetes.io/instance=$RELEASE" \
		--all-containers=true \
		--prefix=true \
		--tail=100
	if [ "$FOLLOW_LOGS" = "1" ]; then
		set -- "$@" --follow
	fi
	kube "$@"
}

# Останавливает контроллеры стенда с сохранением постоянных данных
stop_environment() {
	ensure_kubernetes
	stop_port_forwards

	log "Удаляю Helm-релиз приложения"
	helm_kube uninstall "$RELEASE" --namespace "$NAMESPACE" 2>/dev/null || true

	log "Останавливаю локальные зависимости с сохранением PersistentVolumeClaim"
	kube -n "$NAMESPACE" scale deployment \
		-l app.kubernetes.io/part-of=gophprofile-local \
		--replicas=0 2>/dev/null || true

	log "Удаляю локальный релиз мониторинга"
	helm_kube uninstall "$MONITORING_RELEASE" \
		--namespace "$MONITORING_NAMESPACE" 2>/dev/null || true

	log "Останавливаю Loki, Alloy и Kafka exporter с сохранением данных Loki"
	kube -n "$MONITORING_NAMESPACE" scale deployment \
		-l app.kubernetes.io/part-of=gophprofile-local \
		--replicas=0 2>/dev/null || true

	log "Стенд остановлен, данные PostgreSQL, Kafka, MinIO и Loki сохранены"
}

# Выполняет полный запуск объединённого стенда трёх спринтов
start_environment() {
	require_command helm
	ensure_kubernetes
	build_application_image
	apply_dependencies
	wait_for_dependencies
	create_kafka_topics
	apply_observability
	install_monitoring
	install_application
	start_port_forwards

	log ""
	log "Локальный Kubernetes-стенд готов"
	if [ "$START_PORT_FORWARDS" = "1" ]; then
		log "Frontend:      http://127.0.0.1:$API_PORT/web/"
		log "API:           http://127.0.0.1:$API_PORT/api/"
		log "Healthcheck:   http://127.0.0.1:$API_PORT/health"
		log "MinIO Console: http://127.0.0.1:$MINIO_CONSOLE_PORT (minioadmin/minioadmin)"
		log "Jaeger:        http://127.0.0.1:$JAEGER_PORT"
		if [ "$INSTALL_MONITORING" = "1" ]; then
			log "Prometheus:    http://127.0.0.1:$PROMETHEUS_PORT"
			log "Grafana:       http://127.0.0.1:$GRAFANA_PORT (admin/admin)"
			log "Loki:          http://127.0.0.1:$LOKI_PORT/ready"
			log "Alertmanager:  http://127.0.0.1:$ALERTMANAGER_PORT"
			log "Alloy:         http://127.0.0.1:$ALLOY_PORT"
		fi
	fi
	log "Status:        ./scripts/kubernetes-local.sh status"
	log "Logs:          ./scripts/kubernetes-local.sh logs --follow"
	log "Stop:          ./scripts/kubernetes-local.sh down"
}

parse_arguments "$@"
trap cleanup_on_exit 0

case "$ACTION" in
	up)
		start_environment
		;;
	down)
		stop_environment
		;;
	status)
		show_status
		;;
	logs)
		show_logs
		;;
esac

exit 0
