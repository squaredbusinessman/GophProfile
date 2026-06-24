#!/usr/bin/env bash
set -euo pipefail

SERVER_URL=${SERVER_URL:-http://localhost:8080}
PROMETHEUS_URL=${PROMETHEUS_URL:-http://localhost:9090}
JAEGER_URL=${JAEGER_URL:-http://localhost:16686}
LOKI_URL=${LOKI_URL:-http://localhost:3100}
GRAFANA_URL=${GRAFANA_URL:-http://localhost:3001}
SMOKE_TIMEOUT_SECONDS=${SMOKE_TIMEOUT_SECONDS:-120}
SMOKE_POLL_SECONDS=${SMOKE_POLL_SECONDS:-2}

TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/gophprofile-observability-smoke.XXXXXX")
trap 'rm -rf "$TMP_DIR"' EXIT

log() {
	printf '%s\n' "$1"
}

fail() {
	printf 'ERROR: %s\n' "$1" >&2
	exit 1
}

require_command() {
	command -v "$1" >/dev/null 2>&1 || fail "required command is missing: $1"
}

http_get() {
	curl -fsS "$1"
}

wait_for() {
	label=$1
	shift
	started_at=$(date +%s)
	deadline=$((started_at + SMOKE_TIMEOUT_SECONDS))

	while [ "$(date +%s)" -le "$deadline" ]; do
		if "$@"; then
			log "OK: $label"
			return 0
		fi
		sleep "$SMOKE_POLL_SECONDS"
	done

	fail "timeout waiting for $label"
}

decode_png() {
	png_base64='iVBORw0KGgoAAAANSUhEUgAAAIAAAACACAIAAABMXPacAAABNUlEQVR4nOzRUQnAABTDwDKefzUTOKYiP3eUGsht77NZtfuPjgAxAWICxASICRATICZATICYADEBYgLEBIgJEBMgJkBMgJgAMQFiAsQEiAkQEyAmQEyAmAAxAWICxASICRATICZATICYADEBYgLEBIgJEBMgJkBMgJgAMQFiAsQEiAkQEyAmQEyAmAAxAWICxASICRATICZATICYADEBYgLEBIgJEBMgJkBMgJgAMQFiAsQEiAkQEyAmQEyAmAAxAWICxASICRATICZATICYADEBYgLEBIgJEBMgJkBMgJgAMQFiAsQEiAkQEyAmQEyAmAAxAWICxASICRATICZATICYADEBYgLEBIgJEBMgJkBMgJgAMQFiAsQEiAkQEyAmQEyAmAAxAWICxASICRATIPYFAAD///pzBLH6X0glAAAAAElFTkSuQmCC'
	if printf '%s' "$png_base64" | base64 --decode >"$1" 2>/dev/null; then
		return 0
	fi
	printf '%s' "$png_base64" | base64 -D >"$1"
}

prometheus_ready() {
	http_get "$PROMETHEUS_URL/-/ready" >/dev/null
}

jaeger_ready() {
	http_get "$JAEGER_URL/api/services" | jq -e '.data | index("gophprofile-server")' >/dev/null
}

loki_ready() {
	http_get "$LOKI_URL/ready" >/dev/null
}

grafana_ready() {
	http_get "$GRAFANA_URL/api/health" | jq -e '.database == "ok"' >/dev/null
}

server_ready() {
	http_get "$SERVER_URL/health" | jq -e '.status == "ok"' >/dev/null
}

avatar_ready() {
	response=$(http_get "$SERVER_URL/api/v1/avatars/$AVATAR_ID/metadata")
	avatar_status=$(printf '%s' "$response" | jq -r '.status // .avatar.status // empty')
	[ "$avatar_status" = "ready" ]
}

prometheus_has_application_metrics() {
	query='sum(app_avatar_upload_count_total) > 0 and sum(app_avatar_processing_count_total) > 0 and sum(gophprofile_http_server_requests_total) > 0'
	encoded_query=$(jq -rn --arg q "$query" '$q|@uri')
	http_get "$PROMETHEUS_URL/api/v1/query?query=$encoded_query" | jq -e '.data.result | length > 0' >/dev/null
}

jaeger_has_server_worker_trace() {
	trace_response=$(http_get "$JAEGER_URL/api/traces?service=gophprofile-server&limit=100&lookback=30m")
	TRACE_ID=$(printf '%s' "$trace_response" | jq -r '
		.data[]
		| select(([.spans[].operationName] | index("POST /api/v1/avatars")))
		| select(([.spans[].operationName] | index("send avatar.process.v1")))
		| select(([.spans[].operationName] | index("process avatar.process.v1")))
		| select(([.spans[].operationName] | index("worker.avatar.process")))
		| .traceID
	' | head -n 1)
	[ -n "$TRACE_ID" ]
}

loki_has_trace_log() {
	log_query="{service=~\"server|worker\"} |= \"$TRACE_ID\""
	query=$(jq -rn --arg q "$log_query" '$q|@uri')
	http_get "$LOKI_URL/loki/api/v1/query_range?query=$query&limit=20" | jq -e '.data.result | length > 0' >/dev/null
}

require_command curl
require_command jq
require_command base64

wait_for "server readiness" server_ready
wait_for "Prometheus readiness" prometheus_ready
wait_for "Jaeger services API" jaeger_ready
wait_for "Loki readiness" loki_ready
wait_for "Grafana health API" grafana_ready

PNG_FILE="$TMP_DIR/avatar.png"
decode_png "$PNG_FILE"

SMOKE_EMAIL="smoke-$(date +%s)@example.com"
log "Creating user $SMOKE_EMAIL"
user_response=$(curl -fsS -X POST "$SERVER_URL/api/v1/users/resolve" \
	-H 'Content-Type: application/json' \
	-d "{\"email\":\"$SMOKE_EMAIL\"}")
USER_ID=$(printf '%s' "$user_response" | jq -r '.user_id // .id')
[ -n "$USER_ID" ] && [ "$USER_ID" != "null" ] || fail "user_id was not returned"

log "Uploading PNG"
upload_response=$(curl -fsS -X POST "$SERVER_URL/api/v1/avatars" \
	-H "X-User-ID: $USER_ID" \
	-F "file=@$PNG_FILE;type=image/png")
AVATAR_ID=$(printf '%s' "$upload_response" | jq -r '.id')
[ -n "$AVATAR_ID" ] && [ "$AVATAR_ID" != "null" ] || fail "avatar id was not returned"

wait_for "avatar ready" avatar_ready

log "Reading avatar $AVATAR_ID"
curl -fsS -o "$TMP_DIR/read.png" "$SERVER_URL/api/v1/avatars/$AVATAR_ID?size=original&format=png"
[ -s "$TMP_DIR/read.png" ] || fail "read avatar response is empty"

log "Deleting avatar $AVATAR_ID"
delete_status=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE "$SERVER_URL/api/v1/avatars/$AVATAR_ID" \
	-H "X-User-ID: $USER_ID")
[ "$delete_status" = "204" ] || fail "delete returned HTTP $delete_status"

wait_for "application metrics in Prometheus" prometheus_has_application_metrics
wait_for "server to worker trace in Jaeger" jaeger_has_server_worker_trace
wait_for "trace-correlated log in Loki" loki_has_trace_log

log "OK: observability smoke passed"
log "Trace ID: $TRACE_ID"
