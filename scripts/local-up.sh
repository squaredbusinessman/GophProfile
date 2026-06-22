#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd "$SCRIPT_DIR/.." && pwd)
COMPOSE_BASE_FILE=${COMPOSE_BASE_FILE:-${COMPOSE_FILE:-"$ROOT_DIR/deploy/docker-compose.yml"}}
COMPOSE_OBSERVABILITY_FILE=${COMPOSE_OBSERVABILITY_FILE:-"$ROOT_DIR/deploy/docker-compose.observability.yml"}
TIMEOUT_SECONDS=${LOCAL_UP_TIMEOUT:-300}
S3_BUCKET=${S3_BUCKET:-gophprofile-avatars}
BUILD=1
FOLLOW_LOGS=0

usage() {
	cat <<USAGE
Usage: scripts/local-up.sh [--no-build] [--logs]

Options:
  --no-build   Start existing images without rebuilding
  --logs       Follow server and worker logs after successful startup

Environment:
  COMPOSE_BASE_FILE           Path to the base compose file
  COMPOSE_OBSERVABILITY_FILE  Path to the observability overlay
  LOCAL_UP_TIMEOUT            Wait timeout in seconds, default 300
  S3_BUCKET                   Expected local MinIO bucket, default gophprofile-avatars
USAGE
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--no-build)
			BUILD=0
			;;
		--logs)
			FOLLOW_LOGS=1
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			printf 'Unknown option: %s\n\n' "$1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

log() {
	printf '%s\n' "$1"
}

compose() {
	docker compose -f "$COMPOSE_BASE_FILE" -f "$COMPOSE_OBSERVABILITY_FILE" "$@"
}

require_docker() {
	if ! command -v docker >/dev/null 2>&1; then
		printf 'ERROR: docker is not installed or not available in PATH\n' >&2
		exit 1
	fi
	if ! docker info >/dev/null 2>&1; then
		printf 'ERROR: Docker daemon is not available\n' >&2
		exit 1
	fi
}

service_container_id() {
	compose ps -q "$1"
}

service_container_id_all() {
	compose ps -a -q "$1"
}

service_status() {
	container_id=$(service_container_id "$1")
	if [ -z "$container_id" ]; then
		printf 'missing'
		return
	fi
	docker inspect -f '{{.State.Status}}' "$container_id"
}

service_health() {
	container_id=$(service_container_id "$1")
	if [ -z "$container_id" ]; then
		printf 'missing'
		return
	fi
	docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container_id"
}

is_running() {
	[ "$(service_status "$1")" = "running" ]
}

is_healthy() {
	[ "$(service_health "$1")" = "healthy" ]
}

migrate_completed() {
	container_id=$(service_container_id_all migrate)
	if [ -z "$container_id" ]; then
		return 1
	fi
	status=$(docker inspect -f '{{.State.Status}}' "$container_id")
	exit_code=$(docker inspect -f '{{.State.ExitCode}}' "$container_id")
	[ "$status" = "exited" ] && [ "$exit_code" = "0" ]
}

server_health_ok() {
	compose exec -T server wget -qO- http://127.0.0.1:8080/health >/dev/null 2>&1
}

frontend_ok() {
	compose exec -T frontend-build wget -qO- http://127.0.0.1/web/ >/dev/null 2>&1
}

frontend_proxy_health_ok() {
	compose exec -T frontend-build wget -qO- http://127.0.0.1/health >/dev/null 2>&1
}

bucket_exists() {
	compose exec -T minio sh -c "test -d /data/$S3_BUCKET" >/dev/null 2>&1
}

prometheus_ready() {
	compose exec -T prometheus wget -qO- http://127.0.0.1:9090/-/ready >/dev/null 2>&1
}

prometheus_application_targets_up() {
	targets=$(compose exec -T prometheus wget -qO- http://127.0.0.1:9090/api/v1/targets 2>/dev/null) || return 1
	printf '%s' "$targets" | grep -q '"scrapeUrl":"http://server:9090/metrics".*"health":"up"' || return 1
	printf '%s' "$targets" | grep -q '"scrapeUrl":"http://worker:9091/metrics".*"health":"up"'
}

kafka_lag_metrics_ready() {
	compose exec -T prometheus wget -qO- http://kafka-exporter:9308/metrics 2>/dev/null | grep -q '^kafka_consumergroup_'
}

cadvisor_container_metrics_ready() {
	response=$(compose exec -T prometheus wget -qO- 'http://127.0.0.1:9090/api/v1/query?query=count(container_cpu_usage_seconds_total%7Bid!%3D%22%2F%22%7D)' 2>/dev/null) || return 1
	printf '%s' "$response" | grep -q '"value":\[[^]]*,"[1-9][0-9]*"\]'
}

loki_ready() {
	compose exec -T grafana wget -qO- http://loki:3100/ready >/dev/null 2>&1
}

alloy_ready() {
	compose exec -T grafana wget -qO- http://alloy:12345/-/ready >/dev/null 2>&1
}

loki_application_logs_ready() {
	services=$(compose exec -T grafana wget -qO- http://loki:3100/loki/api/v1/label/service/values 2>/dev/null) || return 1
	printf '%s' "$services" | grep -q 'server' || return 1
	printf '%s' "$services" | grep -q 'worker'
}

grafana_ready() {
	compose exec -T grafana wget -qO- http://127.0.0.1:3000/api/health >/dev/null 2>&1
}

wait_for() {
	label=$1
	shift
	started_at=$(date +%s)
	deadline=$((started_at + TIMEOUT_SECONDS))

	while [ "$(date +%s)" -le "$deadline" ]; do
		if "$@"; then
			log "OK: $label"
			return 0
		fi
		sleep 2
	done

	log "ERROR: timeout waiting for $label"
	return 1
}

print_diagnostics() {
	log ""
	log "Docker Compose status:"
	compose ps || true
	log ""
	log "Recent service logs:"
	compose logs --tail=80 server worker kafka postgres minio || true
}

fail_wait() {
	print_diagnostics
	exit 1
}

require_docker

log "Starting local GophProfile environment"
if [ "$BUILD" = "1" ]; then
	compose up --build --remove-orphans -d
else
	compose up --remove-orphans -d
fi

wait_for "postgres healthy" is_healthy postgres || fail_wait
wait_for "kafka healthy" is_healthy kafka || fail_wait
wait_for "migrations completed" migrate_completed || fail_wait
wait_for "minio healthy" is_healthy minio || fail_wait
wait_for "server healthy" is_healthy server || fail_wait
wait_for "worker healthy" is_healthy worker || fail_wait
wait_for "frontend healthy" is_healthy frontend-build || fail_wait
wait_for "prometheus ready" prometheus_ready || fail_wait
wait_for "prometheus application targets up" prometheus_application_targets_up || fail_wait
wait_for "loki healthy" is_healthy loki || fail_wait
wait_for "loki ready" loki_ready || fail_wait
wait_for "alloy healthy" is_healthy alloy || fail_wait
wait_for "alloy ready" alloy_ready || fail_wait
wait_for "jaeger healthy" is_healthy jaeger || fail_wait
wait_for "grafana ready" grafana_ready || fail_wait
wait_for "grafana healthy" is_healthy grafana || fail_wait
wait_for "kafka exporter healthy" is_healthy kafka-exporter || fail_wait
wait_for "cadvisor healthy" is_healthy cadvisor || fail_wait
wait_for "alertmanager healthy" is_healthy alertmanager || fail_wait
wait_for "kafka consumer lag metrics" kafka_lag_metrics_ready || fail_wait
wait_for "cadvisor container metrics" cadvisor_container_metrics_ready || fail_wait
wait_for "loki application logs" loki_application_logs_ready || fail_wait
wait_for "server healthcheck" server_health_ok || fail_wait
wait_for "frontend /web/" frontend_ok || fail_wait
wait_for "frontend proxy /health" frontend_proxy_health_ok || fail_wait
wait_for "minio bucket $S3_BUCKET" bucket_exists || fail_wait

log ""
log "Local environment is ready"
log "Server API:      http://localhost:8080"
log "Healthcheck:     http://localhost:8080/health"
log "Frontend:        http://localhost:3000/web/"
log "MinIO Console:   http://localhost:9001"
log "PostgreSQL:      localhost:5432"
log "Kafka:           localhost:9092"
log "Prometheus:      http://localhost:9090"
log "Grafana:         http://localhost:3001 (admin/admin)"
log "Jaeger:          http://localhost:16686"
log "Loki API:        http://localhost:3100"
log "Alertmanager:    http://localhost:9093"
log "Alloy UI:        http://localhost:12345"
log ""
log "Stop:            docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.observability.yml down"
log "Logs:            docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.observability.yml logs -f server worker"

if [ "$FOLLOW_LOGS" = "1" ]; then
	compose logs -f server worker
fi
