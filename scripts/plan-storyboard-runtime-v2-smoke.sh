#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/smoke-secret-transport.sh
. "$repo_root/scripts/lib/smoke-secret-transport.sh"
disable_shell_xtrace
umask 077

env_file="${ENV_FILE:-$repo_root/.env.example}"
go_bin="${GO_BIN:-/Users/figo/sdk/go1.26.3/bin/go}"
migrate_bin="${MIGRATE_BIN:-$repo_root/.local/tools/migrate}"
postgres_host="${DORA_SMOKE_POSTGRES_HOST:-127.0.0.1}"
postgres_port="${DORA_SMOKE_POSTGRES_PORT:-15432}"
redis_host="${DORA_SMOKE_REDIS_HOST:-127.0.0.1}"
redis_port="${DORA_SMOKE_REDIS_PORT:-16379}"
etcd_host="${DORA_SMOKE_ETCD_HOST:-127.0.0.1}"
etcd_port="${DORA_SMOKE_ETCD_PORT:-12379}"
business_http_port="${DORA_PLAN_STORYBOARD_BUSINESS_HTTP_PORT:-38101}"
agent_http_port="${DORA_PLAN_STORYBOARD_AGENT_HTTP_PORT:-38102}"
business_rpc_port="${DORA_PLAN_STORYBOARD_BUSINESS_RPC_PORT:-39101}"
agent_rpc_port="${DORA_PLAN_STORYBOARD_AGENT_RPC_PORT:-39102}"
vite_port="${DORA_PLAN_STORYBOARD_VITE_PORT:-33210}"
evidence_file="${PLAN_STORYBOARD_RUNTIME_EVIDENCE_FILE:-$repo_root/.local/smoke/plan-storyboard-runtime-v2.json}"
evidence_pending="${evidence_file}.pending"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
work_dir=""
control_dir=""
business_pid=""
agent_pid=""
vite_pid=""
playwright_pid=""
evidence_published=false

fail() {
  printf 'plan-storyboard-runtime-v2 smoke failed: %s\n' "$1" >&2
  exit 1
}

file_mode() {
  stat -f '%Lp' "$1" 2>/dev/null || stat -c '%a' "$1"
}

stop_pid_best_effort() {
  local pid="$1"
  [[ -n "$pid" ]] || return 0
  if kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
    for _ in $(seq 1 80); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.25
    done
    kill -KILL "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
}

cleanup() {
  local status="$?"
  trap - EXIT INT TERM
  stop_pid_best_effort "$playwright_pid"
  stop_pid_best_effort "$vite_pid"
  stop_pid_best_effort "$agent_pid"
  stop_pid_best_effort "$business_pid"
  [[ -z "$work_dir" ]] || rm -rf "$work_dir"
  rm -f "$evidence_pending" "${evidence_file}.tmp"
  if [[ "$status" -ne 0 || "$evidence_published" != "true" ]]; then
    rm -f "$evidence_file"
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

wait_tcp() {
  local host="$1"
  local port="$2"
  local label="$3"
  for _ in $(seq 1 80); do
    if nc -z "$host" "$port" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  fail "$label is not reachable at ${host}:${port}"
}

discover_local_ipv4() {
  local address=""
  local interface=""
  if command -v route >/dev/null 2>&1 && command -v ipconfig >/dev/null 2>&1; then
    interface="$(route -n get default 2>/dev/null | awk '$1 == "interface:" {print $2; exit}')"
    [[ -z "$interface" ]] || address="$(ipconfig getifaddr "$interface" 2>/dev/null || true)"
  fi
  if [[ -z "$address" ]] && command -v ifconfig >/dev/null 2>&1; then
    address="$(ifconfig 2>/dev/null | awk '$1 == "inet" && $2 !~ /^127\./ && /broadcast/ {print $2; exit}')"
  fi
  if [[ -z "$address" ]] && command -v ip >/dev/null 2>&1; then
    address="$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i=="src") {print $(i+1); exit}}')"
  fi
  [[ "$address" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ && "$address" != 127.* ]] || return 1
  printf '%s' "$address"
}

assert_port_available() {
  local port="$1"
  local label="$2"
  if nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
    fail "$label port $port is already in use"
  fi
}

wait_port_closed() {
  local port="$1"
  local label="$2"
  for _ in $(seq 1 80); do
    if ! nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  fail "$label port $port remained open after shutdown"
}

wait_http_ready() {
  local port="$1"
  local pid="$2"
  local label="$3"
  local log_file="$4"
  for _ in $(seq 1 160); do
    if ! kill -0 "$pid" 2>/dev/null; then
      sed -n '1,240p' "$log_file" >&2 || true
      fail "$label exited before readiness"
    fi
    if curl --fail --silent --max-time 1 "http://127.0.0.1:${port}/readyz" >/dev/null; then
      return 0
    fi
    sleep 0.25
  done
  sed -n '1,240p' "$log_file" >&2 || true
  fail "$label readiness timed out"
}

wait_vite_ready() {
  for _ in $(seq 1 160); do
    if ! kill -0 "$vite_pid" 2>/dev/null; then
      sed -n '1,200p' "$work_dir/Vite.log" >&2 || true
      fail "Vite exited before readiness"
    fi
    if curl --fail --silent --max-time 1 "http://127.0.0.1:${vite_port}/" >/dev/null; then
      return 0
    fi
    sleep 0.25
  done
  fail "Vite readiness timed out"
}

validate_test_database_url() {
  local dsn="$1"
  local role="$2"
  local database="$3"
  local prefix="postgres://${role}:"
  local suffix="@${postgres_host}:${postgres_port}/${database}?sslmode=disable"
  local password=""
  [[ "$dsn" == "$prefix"*"$suffix" ]] || \
    fail "refusing to use a PostgreSQL database other than $database on the canonical host port"
  password="${dsn#"$prefix"}"
  password="${password%"$suffix"}"
  [[ -n "$password" && "$password" != *['@/:?#']* ]] || fail "PostgreSQL test DSN credentials are invalid"
}

reset_test_database() {
  local module="$1"
  local dsn="$2"
  local reset_dir="$work_dir/reset-${module}"
  local reset_table="dora_${module}_plan_storyboard_smoke_reset"
  local reset_dsn="${dsn}&x-migrations-table=${reset_table}"
  mkdir -p "$reset_dir"
  chmod 700 "$reset_dir"
  printf 'DROP SCHEMA IF EXISTS %s CASCADE;\nCREATE SCHEMA %s;\nDROP TABLE IF EXISTS public.schema_migrations;\n' \
    "$module" "$module" >"$reset_dir/000001_reset_contract_schema.up.sql"
  printf 'SELECT 1;\n' >"$reset_dir/000001_reset_contract_schema.down.sql"
  chmod 600 "$reset_dir/000001_reset_contract_schema.up.sql" "$reset_dir/000001_reset_contract_schema.down.sql"
  "$migrate_bin" -path "$reset_dir" -database "$reset_dsn" up >/dev/null
  "$migrate_bin" -path "$reset_dir" -database "$reset_dsn" drop -f >/dev/null
  rm -rf "$reset_dir"
}

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

write_source_manifest() {
  local output="$1"
  local source_file=""
  local digest=""
  : >"$output"
  while IFS= read -r source_file; do
    digest="$(sha256_file "$repo_root/$source_file")" || return 1
    printf '%s  %s\n' "$digest" "$source_file" >>"$output"
  done < <(
    cd "$repo_root"
    {
      find business agent -type f \( -name '*.go' -o -name '*.sql' -o -name '*.thrift' -o -name 'go.mod' -o -name 'go.sum' \) -print
      find frontend/src frontend/e2e frontend/scripts scripts -type f -print
      find frontend -maxdepth 1 -type f \( -name 'package.json' -o -name 'package-lock.json' \) -print
      printf '%s\n' Makefile frontend/playwright.config.js frontend/vite.config.js
    } | LC_ALL=C sort -u
  )
  [[ -s "$output" ]]
}

etcd_prefix_count() {
  local prefix="$1"
  local range_end="${prefix%?}0"
  local key64=""
  local end64=""
  key64="$(printf '%s' "$prefix" | base64 | tr -d '\r\n')"
  end64="$(printf '%s' "$range_end" | base64 | tr -d '\r\n')"
  curl --fail --silent --show-error --max-time 2 \
    -H 'Content-Type: application/json' \
    --data-binary "{\"key\":\"${key64}\",\"range_end\":\"${end64}\",\"count_only\":true}" \
    "http://${etcd_host}:${etcd_port}/v3/kv/range" | jq -er '(.count // 0) | tonumber'
}

wait_etcd_prefix_count() {
  local prefix="$1"
  local expected="$2"
  local label="$3"
  local actual=""
  for _ in $(seq 1 120); do
    actual="$(etcd_prefix_count "$prefix")" || actual=""
    if [[ "$actual" == "$expected" ]]; then
      return 0
    fi
    sleep 0.25
  done
  fail "$label etcd prefix count did not become $expected"
}

start_business() {
  local phase="$1"
  local log_file="$work_dir/Business-${phase}.log"
  [[ -z "$business_pid" ]] || fail "Business PID already exists before $phase start"
  "$repo_root/.local/bin/business-service" >"$log_file" 2>&1 &
  business_pid="$!"
  wait_http_ready "$business_http_port" "$business_pid" "Business $phase" "$log_file"
}

start_agent() {
  local phase="$1"
  local log_file="$work_dir/Agent-${phase}.log"
  [[ -z "$agent_pid" ]] || fail "Agent PID already exists before $phase start"
  "$repo_root/.local/bin/agent-service" >"$log_file" 2>&1 &
  agent_pid="$!"
  wait_http_ready "$agent_http_port" "$agent_pid" "Agent $phase" "$log_file"
}

stop_pid_strict() {
  local pid="$1"
  local label="$2"
  local stopped=false
  local state=""
  [[ -n "$pid" ]] || fail "$label PID is missing before shutdown"
  kill -0 "$pid" 2>/dev/null || fail "$label exited before shutdown"
  kill -TERM "$pid" || fail "could not signal $label"
  for _ in $(seq 1 160); do
    state="$(ps -o stat= -p "$pid" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$pid" 2>/dev/null || [[ "$state" == Z* ]]; then
      stopped=true
      break
    fi
    sleep 0.25
  done
  [[ "$stopped" == "true" ]] || fail "$label did not stop within deadline"
  wait "$pid" || fail "$label returned a failure during graceful shutdown"
}

stop_profile_runtimes() {
  stop_pid_strict "$agent_pid" Agent
  agent_pid=""
  stop_pid_strict "$business_pid" Business
  business_pid=""
  wait_etcd_prefix_count '/dora/services/dora-agent-service/' 0 Agent
  wait_etcd_prefix_count '/dora/services/dora-business-service/' 0 Business
  wait_port_closed "$agent_http_port" Agent-HTTP
  wait_port_closed "$agent_rpc_port" Agent-RPC
  wait_port_closed "$business_http_port" Business-HTTP
  wait_port_closed "$business_rpc_port" Business-RPC
}

enable_creation_spec_profile() {
  export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=true
  export DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false
  export DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=false
  export DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED=false
  export DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED=false
  export DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED=false
  export DORA_BUSINESS_PLAN_STORYBOARD_RUNTIME_ENABLED=false
}

enable_storyboard_profile() {
  export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false
  export DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false
  export DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=false
  export DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED=true
  export DORA_AGENT_PLAN_STORYBOARD_RUNTIME_PROFILE=plan_storyboard.runtime.v2preview1
  # Chromium 断线检查会保留真实 EventSource；退出窗口必须覆盖最长 SSE 连接并留出收尾余量。
  export AGENT_SSE_MAX_CONNECTION_DURATION=20s
  export AGENT_SHUTDOWN_TIMEOUT=35s
  export DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED=false
  export DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED=false
  export DORA_BUSINESS_PLAN_STORYBOARD_RUNTIME_ENABLED=true
}

configure_creation_spec_addresses() {
  local local_ipv4="$1"
  export BUSINESS_HTTP_ADDR="0.0.0.0:${business_http_port}"
  export BUSINESS_ADVERTISED_ADDRESS="${local_ipv4}:${business_http_port}"
  export BUSINESS_RPC_LISTEN_ADDR="${local_ipv4}:${business_rpc_port}"
  export BUSINESS_RPC_ADVERTISED_ADDRESS="${local_ipv4}:${business_rpc_port}"
  export BUSINESS_AGENT_HTTP_BASE_URL="http://${local_ipv4}:${agent_http_port}"
  export AGENT_HTTP_ADDR="0.0.0.0:${agent_http_port}"
  export AGENT_ADVERTISED_ADDRESS="${local_ipv4}:${agent_http_port}"
  export AGENT_RPC_LISTEN_ADDR="${local_ipv4}:${agent_rpc_port}"
  export AGENT_RPC_ADVERTISED_ADDRESS="${local_ipv4}:${agent_rpc_port}"
}

configure_storyboard_addresses() {
  export BUSINESS_HTTP_ADDR="127.0.0.1:${business_http_port}"
  export BUSINESS_ADVERTISED_ADDRESS="127.0.0.1:${business_http_port}"
  export BUSINESS_RPC_LISTEN_ADDR="127.0.0.1:${business_rpc_port}"
  export BUSINESS_RPC_ADVERTISED_ADDRESS="127.0.0.1:${business_rpc_port}"
  export BUSINESS_AGENT_HTTP_BASE_URL="http://127.0.0.1:${agent_http_port}"
  export AGENT_HTTP_ADDR="127.0.0.1:${agent_http_port}"
  export AGENT_ADVERTISED_ADDRESS="127.0.0.1:${agent_http_port}"
  export AGENT_RPC_LISTEN_ADDR="127.0.0.1:${agent_rpc_port}"
  export AGENT_RPC_ADVERTISED_ADDRESS="127.0.0.1:${agent_rpc_port}"
}

poll_bootstrap_ready() {
  local project_id="$1"
  local output="$2"
  local status=""
  for _ in $(seq 1 160); do
    status="$(curl --silent --show-error --max-time 2 -b "$work_dir/cookies.txt" \
      --config "$work_dir/csrf.curl" -o "$output" -w '%{http_code}' \
      "http://127.0.0.1:${business_http_port}/api/v1/projects/${project_id}/bootstrap")"
    if [[ "$status" == "200" ]] && jq -e '.creation_status == "ready" and (.session_id | type == "string")' "$output" >/dev/null; then
      return 0
    fi
    sleep 0.25
  done
  fail "Project bootstrap did not become ready"
}

poll_creation_spec_draft() {
  local session_id="$1"
  local output="$2"
  local status=""
  for _ in $(seq 1 240); do
    status="$(curl --silent --show-error --max-time 2 -b "$work_dir/cookies.txt" \
      --config "$work_dir/csrf.curl" -o "$output" -w '%{http_code}' \
      "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/workspace")"
    if [[ "$status" == "200" ]] && jq -e '
      .schema_version == "session.workspace.v3"
      and .creation_spec_preview.status == "draft"
      and .creation_spec_preview.version == 1
      and (.creation_spec_preview.creation_spec_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and (.creation_spec_preview.content_digest | test("^[0-9a-f]{64}$"))
      and .plan_storyboard_preview == null
    ' "$output" >/dev/null; then
      return 0
    fi
    sleep 0.25
  done
  fail "CreationSpec Draft did not reach the Workspace Snapshot"
}

write_atomic_json() {
  local target="$1"
  local temporary="${target}.$$.tmp"
  shift
  jq "$@" >"$temporary"
  chmod 600 "$temporary"
  mv "$temporary" "$target"
  chmod 600 "$target"
}

wait_for_control_file() {
  local path="$1"
  local jq_filter="$2"
  local label="$3"
  for _ in $(seq 1 2400); do
    if [[ -s "$path" ]] && jq -e "$jq_filter" "$path" >/dev/null 2>&1; then
      [[ "$(file_mode "$path")" == "600" ]] || fail "$label mode is not 0600"
      return 0
    fi
    local state=""
    state="$(ps -o stat= -p "$playwright_pid" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$playwright_pid" 2>/dev/null || [[ "$state" == Z* ]]; then
      sed -n '1,320p' "$work_dir/Playwright.log" >&2 || true
      fail "Playwright exited before $label"
    fi
    sleep 0.05
  done
  fail "timed out waiting for $label"
}

assert_evidence_redacted() {
  local value=""
  local status=""
  for value in \
    "${DORA_SMOKE_USER_EMAIL:-}" "${DORA_SMOKE_USER_PASSWORD:-}" \
    "${BUSINESS_AUTH_CSRF_SECRET_BASE64:-}" "${BUSINESS_AGENT_SESSION_RPC_AUTH_SECRET_BASE64:-}" \
    "${BUSINESS_AGENT_HTTP_ASSERTION_SECRET_BASE64:-}" "${AGENT_HTTP_ASSERTION_ACTIVE_SECRET_BASE64:-}" \
    "${AGENT_SESSION_RPC_AUTH_SECRET_BASE64:-}" "${AGENT_CONTENT_KEY_BASE64:-}" \
    "${BUSINESS_DATABASE_URL:-}" "${AGENT_DATABASE_URL:-}"; do
    [[ -n "$value" ]] || continue
    if rg_with_pattern_stdin literal "$value" "$evidence_pending"; then
      fail "Trial Evidence contains a protected runtime value"
    else
      status="$?"
      [[ "$status" == "1" ]] || fail "Trial Evidence redaction scan failed"
    fi
  done
  if rg -ni '(authorization|cookie|csrf|password|secret|private_key|access_token|refresh_token|prompt|ciphertext|nonce|database_url|postgres://)' "$evidence_pending" >/dev/null; then
    fail "Trial Evidence contains a forbidden sensitive field"
  fi
}

[[ -r "$env_file" ]] || fail "environment file is missing"
set -a
# shellcheck disable=SC1090
. "$env_file"
set +a

[[ "${DORA_ENV:-}" == "local" ]] || fail "DORA_ENV must be local"
[[ "${AGENT_SSE_MAX_EVENT_BYTES:-}" == "131072" ]] || fail "AGENT_SSE_MAX_EVENT_BYTES must be 131072 for Storyboard Card envelopes"
[[ "$postgres_host" == "127.0.0.1" && "$postgres_port" == "15432" ]] || fail "PostgreSQL must use 127.0.0.1:15432"
[[ "$redis_host" == "127.0.0.1" && "$redis_port" == "16379" ]] || fail "Redis must use 127.0.0.1:16379"
[[ "$etcd_host" == "127.0.0.1" && "$etcd_port" == "12379" ]] || fail "etcd must use 127.0.0.1:12379"
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"
command -v nc >/dev/null 2>&1 || fail "nc is required"
command -v rg >/dev/null 2>&1 || fail "rg is required"
command -v shasum >/dev/null 2>&1 || fail "shasum is required"
[[ -x "$go_bin" ]] || fail "Go SDK is required"
[[ -x "$migrate_bin" ]] || fail "golang-migrate is required"
[[ -x "$repo_root/frontend/node_modules/.bin/vite" ]] || fail "Vite dependencies are not installed"
[[ -x "$repo_root/frontend/node_modules/.bin/playwright" ]] || fail "Playwright dependencies are not installed"
[[ -r "$repo_root/frontend/e2e/plan-storyboard-runtime.spec.js" ]] || fail "canonical Chromium spec is missing"

BUSINESS_DATABASE_URL="${BUSINESS_CONTRACT_DATABASE_URL:-}"
AGENT_DATABASE_URL="${AGENT_CONTRACT_DATABASE_URL:-}"
export BUSINESS_DATABASE_URL AGENT_DATABASE_URL
validate_test_database_url "$BUSINESS_DATABASE_URL" dora_business_app dora_business_test
validate_test_database_url "$AGENT_DATABASE_URL" dora_agent_app dora_agent_test

export BUSINESS_REDIS_ADDR="${redis_host}:${redis_port}"
export AGENT_REDIS_ADDR="${redis_host}:${redis_port}"
export BUSINESS_ETCD_ENDPOINTS="${etcd_host}:${etcd_port}"
export AGENT_ETCD_ENDPOINTS="${etcd_host}:${etcd_port}"
export BUSINESS_INSTANCE_ID="business-storyboard-${run_id}"
export AGENT_INSTANCE_ID="agent-storyboard-${run_id}"

wait_tcp "$postgres_host" "$postgres_port" PostgreSQL
wait_tcp "$redis_host" "$redis_port" Redis
wait_tcp "$etcd_host" "$etcd_port" etcd
local_ipv4="$(discover_local_ipv4)" || fail "a non-loopback local IPv4 is required for the CreationSpec profile"
for port_label in "$business_http_port:Business-HTTP" "$agent_http_port:Agent-HTTP" \
  "$business_rpc_port:Business-RPC" "$agent_rpc_port:Agent-RPC" "$vite_port:Vite"; do
  assert_port_available "${port_label%%:*}" "${port_label#*:}"
done

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/dora-plan-storyboard-runtime.XXXXXX")"
chmod 700 "$work_dir"
control_dir="$work_dir/control"
mkdir -m 700 "$control_dir"
mkdir -p "$repo_root/.local/bin" "$(dirname "$evidence_file")"
rm -f "$evidence_file" "$evidence_pending" "${evidence_file}.tmp"
write_source_manifest "$work_dir/source-before.sha256" || fail "could not freeze the source manifest"

GOWORK=off "$go_bin" -C "$repo_root/business" build -o "$repo_root/.local/bin/business-service" ./cmd/business-service
GOWORK=off "$go_bin" -C "$repo_root/agent" build -o "$repo_root/.local/bin/agent-service" ./cmd/agent-service
reset_test_database business "$BUSINESS_DATABASE_URL"
reset_test_database agent "$AGENT_DATABASE_URL"
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" business up
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" agent up
(
  cd "$repo_root/business"
  GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-seeder
) >"$work_dir/user-seed.json" 2>"$work_dir/user-seed.log" || fail "local smoke user seed failed"

# 先以 Storyboard exact-loopback Profile 创建空 Lane，避免在未批准的统一 Dispatcher 下同时启动多个 Processor。
enable_storyboard_profile
configure_storyboard_addresses
start_business lane-bootstrap
start_agent lane-bootstrap
wait_etcd_prefix_count '/dora/services/dora-business-service/' 1 Business
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent

login_payload="$(build_login_json "$DORA_SMOKE_USER_EMAIL" "$DORA_SMOKE_USER_PASSWORD")"
login_status="$(curl_with_body_stdin "$login_payload" --silent --show-error --max-time 10 \
  -c "$work_dir/cookies.txt" -H 'Content-Type: application/json' -o "$work_dir/login.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/auth/session")"
[[ "$login_status" == "200" ]] || fail "login returned $login_status"
csrf_token="$(jq -er '.csrf_token' "$work_dir/login.json")"
write_curl_header_config "$work_dir/csrf.curl" X-CSRF-Token "$csrf_token" || fail "could not freeze CSRF curl config"

quick_payload='{"initial_prompt":null}'
quick_status="$(curl_with_body_stdin "$quick_payload" --silent --show-error --max-time 10 \
  -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 019f2000-0000-7000-8000-000000000001' \
  -o "$work_dir/quick.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/projects:quick-create")"
[[ "$quick_status" == "201" ]] || fail "empty Lane Quick Create returned $quick_status"
project_id="$(jq -er '.project_id' "$work_dir/quick.json")"
poll_bootstrap_ready "$project_id" "$work_dir/bootstrap.json"
session_id="$(jq -er '.session_id' "$work_dir/bootstrap.json")"

empty_snapshot_status="$(curl --silent --show-error --max-time 10 -b "$work_dir/cookies.txt" \
  --config "$work_dir/csrf.curl" -o "$work_dir/empty-workspace.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$empty_snapshot_status" == "200" ]] || fail "empty Lane Workspace returned $empty_snapshot_status"
jq -e --arg project "$project_id" --arg session "$session_id" '
  .schema_version == "session.workspace.v3"
  and .session.id == $session and .session.project_id == $project
  and .messages == [] and .inputs == []
  and .creation_spec_preview == null and .plan_storyboard_preview == null
  and .event_high_watermark == 1
' "$work_dir/empty-workspace.json" >/dev/null || fail "Quick Create did not produce an empty Session Lane"

# CreationSpec Profile 的现有 Resolver 要求可路由的宿主机 IPv4；完整停止 Storyboard 后再独占启动。
stop_profile_runtimes
enable_creation_spec_profile
configure_creation_spec_addresses "$local_ipv4"
start_business creation-spec
start_agent creation-spec
wait_etcd_prefix_count '/dora/services/dora-business-service/' 1 Business
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent

creation_goal="Storyboard M4 CreationSpec ${run_id}"
creation_payload="$(jq -cn --arg goal "$creation_goal" '{schema_version:"plan_creation_spec.preview.intent.v1",goal:$goal,deliverable_type:"video",audience:"M4 本地试跑",locale:"zh-CN",constraints:["时长 30 秒","保持叙事节奏"]}')"
creation_status="$(curl_with_body_stdin "$creation_payload" --silent --show-error --max-time 10 \
  -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 019f2000-0000-7000-8000-000000000002' \
  -o "$work_dir/creation-enqueue.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/creation-spec-previews")"
[[ "$creation_status" == "202" ]] || fail "CreationSpec Draft enqueue returned $creation_status"
jq -e --arg session "$session_id" '
  keys == ["input_id","request_id","schema_version","session_id","status"]
  and .schema_version == "plan_creation_spec.preview.enqueue.v1"
  and .session_id == $session and .status == "pending"
  and all(.request_id,.input_id; test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
' "$work_dir/creation-enqueue.json" >/dev/null || fail "CreationSpec enqueue exact DTO is invalid"
creation_input_id="$(jq -er '.input_id' "$work_dir/creation-enqueue.json")"
poll_creation_spec_draft "$session_id" "$work_dir/creation-workspace.json"
creation_spec_id="$(jq -er '.creation_spec_preview.creation_spec_id' "$work_dir/creation-workspace.json")"
creation_spec_version="$(jq -er '.creation_spec_preview.version' "$work_dir/creation-workspace.json")"
creation_spec_content_digest="$(jq -er '.creation_spec_preview.content_digest' "$work_dir/creation-workspace.json")"
creation_high_watermark="$(jq -er '.event_high_watermark' "$work_dir/creation-workspace.json")"

# 统一 Dispatcher 未完成前两个 source-filtered Processor 不得并行；先完整停止再切换到单 Tool Storyboard Profile。
stop_profile_runtimes
enable_storyboard_profile
configure_storyboard_addresses
start_business storyboard
start_agent storyboard
wait_etcd_prefix_count '/dora/services/dora-business-service/' 1 Business
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent

storyboard_profile_status="$(curl --silent --show-error --max-time 10 -b "$work_dir/cookies.txt" \
  --config "$work_dir/csrf.curl" -o "$work_dir/storyboard-profile-workspace.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$storyboard_profile_status" == "200" ]] || fail "Storyboard Profile Workspace returned $storyboard_profile_status"
jq -e --arg spec "$creation_spec_id" --arg digest "$creation_spec_content_digest" '
  .schema_version == "session.workspace.v3"
  and .creation_spec_preview.creation_spec_id == $spec
  and .creation_spec_preview.version == 1
  and .creation_spec_preview.content_digest == $digest
  and .plan_storyboard_preview == null
' "$work_dir/storyboard-profile-workspace.json" >/dev/null || fail "Profile switch did not preserve the CreationSpec Draft"

(
  cd "$repo_root/frontend"
  exec env VITE_BUSINESS_API_TARGET="http://127.0.0.1:${business_http_port}" \
    VITE_AGENT_API_TARGET="http://127.0.0.1:${agent_http_port}" \
    VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED=false \
    VITE_DORA_PLAN_STORYBOARD_RUNTIME_ENABLED=true \
    ./node_modules/.bin/vite --host 127.0.0.1 --port "$vite_port" --strictPort
) >"$work_dir/Vite.log" 2>&1 &
vite_pid="$!"
wait_vite_ready

browser_result="$work_dir/browser-result.json"
planning_instruction="按开场、核心演示和收尾行动号召规划一条节奏清晰的故事板"
(
  cd "$repo_root/frontend"
  DORA_E2E_PLAN_STORYBOARD_RUNTIME=1 \
  DORA_E2E_EXTERNAL_SERVER=1 \
  DORA_E2E_BASE_URL="http://127.0.0.1:${vite_port}" \
  DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:${business_http_port}" \
  DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
  DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
  DORA_E2E_PLAN_STORYBOARD_RESULT_PATH="$browser_result" \
  DORA_E2E_PLAN_STORYBOARD_CONTROL_DIR="$control_dir" \
  DORA_E2E_PLAN_STORYBOARD_INSTRUCTION="$planning_instruction" \
  DORA_E2E_PLAN_STORYBOARD_DURATION=30 \
  DORA_E2E_PROJECT_ID="$project_id" \
  DORA_E2E_SESSION_ID="$session_id" \
  DORA_E2E_CREATION_SPEC_ID="$creation_spec_id" \
  DORA_E2E_CREATION_SPEC_VERSION="$creation_spec_version" \
  DORA_E2E_CREATION_SPEC_CONTENT_DIGEST="$creation_spec_content_digest" \
  DORA_E2E_CREATION_HIGH_WATERMARK="$creation_high_watermark" \
    ./node_modules/.bin/playwright test e2e/plan-storyboard-runtime.spec.js \
      --grep '@plan-storyboard-runtime'
) >"$work_dir/Playwright.log" 2>&1 &
playwright_pid="$!"

restart_request="$control_dir/agent-restart-request.json"
wait_for_control_file "$restart_request" '
  .schema_version == "plan_storyboard_runtime.restart_request.v1"
  and all(.project_id,.session_id,.input_id,.turn_id,.run_id,.tool_call_id,.storyboard_preview_id;
    test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
' "Agent restart request"
[[ "$(jq -er '.project_id' "$restart_request")" == "$project_id" \
  && "$(jq -er '.session_id' "$restart_request")" == "$session_id" ]] || fail "Agent restart request identity drifted"
storyboard_preview_id="$(jq -er '.storyboard_preview_id' "$restart_request")"

# 真实终止 Agent 进程使 Business 同源 SSE 断开；浏览器确认 reconnecting 后才启动同一 Profile 新进程。
stop_pid_strict "$agent_pid" Agent-restart-checkpoint
agent_pid=""
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 0 Agent
wait_port_closed "$agent_http_port" Agent-HTTP
wait_port_closed "$agent_rpc_port" Agent-RPC
disconnect_observed="$control_dir/agent-disconnect-observed.json"
wait_for_control_file "$disconnect_observed" '
  .schema_version == "plan_storyboard_runtime.disconnect_observed.v1"
  and .stream_state == "reconnecting"
  and (.session_id | test("^[0-9a-f-]{36}$"))
  and (.storyboard_preview_id | test("^[0-9a-f-]{36}$"))
' "browser Agent disconnect observation"
start_agent storyboard-reconnect
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent
write_atomic_json "$control_dir/agent-restart-ack.json" -n \
  --arg session_id "$session_id" --arg storyboard_preview_id "$storyboard_preview_id" '
  {schema_version:"plan_storyboard_runtime.restart_ack.v1",session_id:$session_id,
   storyboard_preview_id:$storyboard_preview_id,agent_ready:true}
'

set +e
wait "$playwright_pid"
playwright_status="$?"
set -e
playwright_pid=""
if [[ "$playwright_status" -ne 0 ]]; then
  sed -n '1,360p' "$work_dir/Playwright.log" >&2 || true
  fail "Plan Storyboard Chromium smoke failed"
fi
[[ "$(file_mode "$browser_result")" == "600" ]] || fail "browser result permissions are not 0600"
jq -e --arg project "$project_id" --arg session "$session_id" --arg spec "$creation_spec_id" \
  --arg spec_digest "$creation_spec_content_digest" --arg storyboard "$storyboard_preview_id" '
  keys == ["assertions","creation_spec_content_digest","creation_spec_id","creation_spec_version",
    "event_high_watermark","input_id","project_id","request_id","run_id","schema_version","session_id",
    "status","storyboard_content_digest","storyboard_preview_id","tool_call_id","turn_id"]
  and .schema_version == "plan_storyboard_runtime.browser_result.v1" and .status == "passed"
  and .project_id == $project and .session_id == $session
  and .creation_spec_id == $spec and .creation_spec_version == 1
  and .creation_spec_content_digest == $spec_digest and .storyboard_preview_id == $storyboard
  and (.storyboard_content_digest | test("^[0-9a-f]{64}$"))
  and all(.input_id,.request_id,.turn_id,.run_id,.tool_call_id; test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
  and (.assertions | keys) == ["accepted_sse_observed","agent_disconnect_observed","agent_reconnect_recovered",
    "chromium_browser","existing_creation_spec_bound","hard_reload_recovered","same_origin_business_bff",
    "static_catalog_unavailable","storyboard_card_visible","storyboard_form_submitted","terminal_sse_observed"]
  and all(.assertions[]; . == true)
' "$browser_result" >/dev/null || fail "Plan Storyboard browser result is invalid"

set +e
curl --silent --show-error --no-buffer --max-time 8 -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/events?after_seq=${creation_high_watermark}" \
  >"$work_dir/storyboard-events.sse" 2>"$work_dir/storyboard-events.log"
sse_status="$?"
set -e
[[ "$sse_status" == "0" || "$sse_status" == "28" ]] || fail "Storyboard SSE replay failed with $sse_status"
grep -F 'event: plan_storyboard.preview.accepted' "$work_dir/storyboard-events.sse" >/dev/null || fail "SSE omitted Storyboard accepted event"
grep -F 'event: plan_storyboard.preview.completed' "$work_dir/storyboard-events.sse" >/dev/null || fail "SSE omitted Storyboard terminal event"
if rg -n 'intent_ciphertext|tool_intent|prompt_messages|provider_payload|access_scope|business_command_body|reasoning' \
  "$work_dir/storyboard-events.sse" >/dev/null; then
  fail "Storyboard SSE exposed a forbidden payload field"
fi

final_snapshot_status="$(curl --silent --show-error --max-time 10 -b "$work_dir/cookies.txt" \
  --config "$work_dir/csrf.curl" -o "$work_dir/final-workspace.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$final_snapshot_status" == "200" ]] || fail "final Workspace Snapshot returned $final_snapshot_status"
jq -e --slurpfile browser "$browser_result" --arg spec "$creation_spec_id" --arg spec_digest "$creation_spec_content_digest" '
  .schema_version == "session.workspace.v3"
  and .creation_spec_preview.creation_spec_id == $spec
  and .creation_spec_preview.content_digest == $spec_digest
  and .plan_storyboard_preview.schema_version == "storyboard.preview.card.v1"
  and .plan_storyboard_preview.status == "completed"
  and .plan_storyboard_preview.storyboard_preview_id == $browser[0].storyboard_preview_id
  and .plan_storyboard_preview.input_id == $browser[0].input_id
  and .plan_storyboard_preview.turn_id == $browser[0].turn_id
  and .plan_storyboard_preview.run_id == $browser[0].run_id
  and .plan_storyboard_preview.tool_call_id == $browser[0].tool_call_id
  and .event_high_watermark == $browser[0].event_high_watermark
' "$work_dir/final-workspace.json" >/dev/null || fail "final Snapshot lost Storyboard frozen identity"

write_source_manifest "$work_dir/source-after.sha256" || fail "could not rebuild the source manifest"
cmp -s "$work_dir/source-before.sha256" "$work_dir/source-after.sha256" || fail "source tree changed during canonical Plan Storyboard Trial"
source_digest="$(sha256_file "$work_dir/source-before.sha256")"

stop_pid_best_effort "$vite_pid"
vite_pid=""
stop_profile_runtimes
wait_port_closed "$vite_port" Vite

jq -n -S \
  --arg run_id "$run_id" --arg source_digest "sha256:${source_digest}" \
  --arg project_id "$project_id" --arg session_id "$session_id" \
  --arg creation_input_id "$creation_input_id" --arg creation_spec_id "$creation_spec_id" \
  --arg creation_spec_digest "$creation_spec_content_digest" \
  --arg storyboard_input_id "$(jq -er '.input_id' "$browser_result")" \
  --arg storyboard_turn_id "$(jq -er '.turn_id' "$browser_result")" \
  --arg storyboard_run_id "$(jq -er '.run_id' "$browser_result")" \
  --arg storyboard_tool_call_id "$(jq -er '.tool_call_id' "$browser_result")" \
  --arg storyboard_preview_id "$storyboard_preview_id" \
  --arg storyboard_content_digest "$(jq -er '.storyboard_content_digest' "$browser_result")" \
  --argjson postgres_port "$postgres_port" --argjson redis_port "$redis_port" --argjson etcd_port "$etcd_port" \
  --argjson creation_high_watermark "$creation_high_watermark" \
  --argjson final_high_watermark "$(jq -er '.event_high_watermark' "$browser_result")" '
  {
    schema_version:"plan_storyboard_runtime_v2_smoke_evidence.v1",
    status:"pending",
    run_id:$run_id,
    profile:"plan_storyboard.runtime.v2preview1",
    source_digest:$source_digest,
    infrastructure:{
      postgresql:{host:"127.0.0.1",port:$postgres_port,database:"dora_agent_test"},
      redis:{host:"127.0.0.1",port:$redis_port,service_connected:true},
      etcd:{host:"127.0.0.1",port:$etcd_port,service_registered:true}
    },
    identity:{
      project_id:$project_id,session_id:$session_id,
      creation_input_id:$creation_input_id,creation_spec_id:$creation_spec_id,
      storyboard_input_id:$storyboard_input_id,turn_id:$storyboard_turn_id,run_id:$storyboard_run_id,
      tool_call_id:$storyboard_tool_call_id,storyboard_preview_id:$storyboard_preview_id
    },
    digests:{creation_spec:$creation_spec_digest,storyboard:$storyboard_content_digest},
    counts:{creation_high_watermark:$creation_high_watermark,final_high_watermark:$final_high_watermark},
    assertions:{
      empty_lane:true,creation_spec_draft_prepared:true,exclusive_profile_switch:true,
      chromium_form_submission:true,accepted_sse:true,terminal_sse:true,storyboard_card:true,
      hard_reload_recovered:true,agent_disconnect_observed:true,agent_reconnect_recovered:true,
      static_catalog_unavailable:true,direct_host_infrastructure:true,source_unchanged:true,
      etcd_exact_instances_cleaned:true,runtime_cleanup:true,evidence_redacted:false
    }
  }
' >"$evidence_pending"
chmod 600 "$evidence_pending"
jq -e '
  .schema_version == "plan_storyboard_runtime_v2_smoke_evidence.v1"
  and .status == "pending"
  and .profile == "plan_storyboard.runtime.v2preview1"
  and .counts.final_high_watermark == (.counts.creation_high_watermark + 2)
  and .assertions.evidence_redacted == false
  and all(.assertions | to_entries[] | select(.key != "evidence_redacted"); .value == true)
' "$evidence_pending" >/dev/null || fail "pending Trial Evidence is invalid"
assert_evidence_redacted
jq -S '.status = "passed" | .assertions.evidence_redacted = true' "$evidence_pending" >"${evidence_file}.tmp"
chmod 600 "${evidence_file}.tmp"
mv "${evidence_file}.tmp" "$evidence_file"
chmod 600 "$evidence_file"
rm -f "$evidence_pending"
[[ "$(file_mode "$evidence_file")" == "600" ]] || fail "published Evidence permissions are not 0600"
jq -e '.status == "passed" and all(.assertions[]; . == true)' "$evidence_file" >/dev/null || fail "published Evidence is invalid"
evidence_published=true

printf 'plan-storyboard-runtime-v2 direct-host canonical smoke passed: %s\n' "$evidence_file"
