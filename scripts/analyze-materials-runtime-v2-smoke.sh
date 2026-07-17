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
business_http_port="${DORA_ANALYZE_MATERIALS_BUSINESS_HTTP_PORT:-38081}"
agent_http_port="${DORA_ANALYZE_MATERIALS_AGENT_HTTP_PORT:-38082}"
business_rpc_port="${DORA_ANALYZE_MATERIALS_BUSINESS_RPC_PORT:-39081}"
agent_rpc_port="${DORA_ANALYZE_MATERIALS_AGENT_RPC_PORT:-39082}"
vite_port="${DORA_ANALYZE_MATERIALS_VITE_PORT:-33200}"
evidence_file="${ANALYZE_MATERIALS_RUNTIME_EVIDENCE_FILE:-$repo_root/.local/smoke/analyze-materials-runtime-v2.json}"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
work_dir=""
business_pid=""
agent_pid=""
vite_pid=""
playwright_pid=""

fail() {
  printf 'analyze-materials-runtime-v2 smoke failed: %s\n' "$1" >&2
  exit 1
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
  if [[ "$status" -ne 0 ]]; then
    rm -f "$evidence_file" "${evidence_file}.tmp"
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

assert_port_available() {
  local port="$1"
  local label="$2"
  if nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
    fail "$label port $port is already in use"
  fi
}

wait_http_ready() {
  local port="$1"
  local pid="$2"
  local label="$3"
  for _ in $(seq 1 160); do
    if ! kill -0 "$pid" 2>/dev/null; then
      sed -n '1,200p' "$work_dir/${label}.log" >&2 || true
      fail "$label exited before readiness"
    fi
    if curl --fail --silent --max-time 1 "http://127.0.0.1:${port}/readyz" >/dev/null; then
      return 0
    fi
    sleep 0.25
  done
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

validate_test_database_url() {
  local dsn="$1"
  local role="$2"
  local database="$3"
  local prefix="postgres://${role}:"
  local suffix="@${postgres_host}:${postgres_port}/${database}?sslmode=disable"
  local password=""
  [[ "$dsn" == "$prefix"*"$suffix" ]] || fail "refusing to use a PostgreSQL database other than $database on the canonical host port"
  password="${dsn#"$prefix"}"
  password="${password%"$suffix"}"
  [[ -n "$password" && "$password" != *['@/:?#']* ]] || fail "PostgreSQL test DSN credentials are invalid"
}

reset_test_database() {
  local module="$1"
  local dsn="$2"
  local reset_dir="$work_dir/reset-${module}"
  local reset_table="dora_${module}_analyze_materials_smoke_reset"
  local reset_dsn="${dsn}&x-migrations-table=${reset_table}"
  mkdir -p "$reset_dir"
  chmod 700 "$reset_dir"
  printf 'DROP SCHEMA IF EXISTS %s CASCADE;\nCREATE SCHEMA %s;\nDROP TABLE IF EXISTS public.schema_migrations;\n' \
    "$module" "$module" >"$reset_dir/000001_reset_contract_schema.up.sql"
  printf 'SELECT 1;\n' >"$reset_dir/000001_reset_contract_schema.down.sql"
  chmod 600 "$reset_dir/000001_reset_contract_schema.up.sql"
  chmod 600 "$reset_dir/000001_reset_contract_schema.down.sql"
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
  for _ in $(seq 1 80); do
    actual="$(etcd_prefix_count "$prefix")" || actual=""
    if [[ "$actual" == "$expected" ]]; then
      return 0
    fi
    sleep 0.25
  done
  fail "$label etcd prefix count did not become $expected"
}

poll_bootstrap_ready() {
  local project_id="$1"
  local output="$2"
  local status=""
  for _ in $(seq 1 120); do
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

poll_runtime_terminal() {
  local session_id="$1"
  local input_id="$2"
  local turn_id="$3"
  local runtime_run_id="$4"
  local tool_call_id="$5"
  for _ in $(seq 1 160); do
    if DORA_SMOKE_ANALYZE_MATERIALS_MODE=authority \
      DORA_SMOKE_SESSION_ID="$session_id" DORA_SMOKE_INPUT_ID="$input_id" \
      DORA_SMOKE_TURN_ID="$turn_id" DORA_SMOKE_RUN_ID="$runtime_run_id" \
      DORA_SMOKE_TOOL_CALL_ID="$tool_call_id" \
      "$repo_root/.local/bin/local-smoke-analyze-materials-authority" \
      >"$work_dir/authority-poll.json" 2>"$work_dir/authority-poll.log"; then
      if jq -e '.counts.input_count == 1' "$work_dir/authority-poll.json" >/dev/null; then
        return 0
      fi
      if jq -e '.counts.run_count == 0 and .counts.projection_count == 1 and .counts.terminal_event_count == 1' "$work_dir/authority-poll.json" >/dev/null; then
        fail "Analyze Materials input reached a non-completed terminal state"
      fi
    else
      sed -n '1,80p' "$work_dir/authority-poll.log" >&2 || true
      fail "Analyze Materials authority probe failed"
    fi
    sleep 0.25
  done
  fail "Analyze Materials Runtime did not reach a terminal state"
}

collect_authority() {
  local session_id="$1"
  local input_id="$2"
  local turn_id="$3"
  local runtime_run_id="$4"
  local tool_call_id="$5"
  local output="$6"
  DORA_SMOKE_ANALYZE_MATERIALS_MODE=authority \
  DORA_SMOKE_SESSION_ID="$session_id" DORA_SMOKE_INPUT_ID="$input_id" \
  DORA_SMOKE_TURN_ID="$turn_id" DORA_SMOKE_RUN_ID="$runtime_run_id" \
  DORA_SMOKE_TOOL_CALL_ID="$tool_call_id" \
    "$repo_root/.local/bin/local-smoke-analyze-materials-authority" >"$output"
  chmod 600 "$output"
}

[[ -r "$env_file" ]] || fail "environment file is missing"
set -a
# shellcheck disable=SC1090
. "$env_file"
set +a

[[ "${DORA_ENV:-}" == "local" ]] || fail "DORA_ENV must be local"
[[ "${AGENT_SSE_MAX_EVENT_BYTES:-}" == "131072" ]] || fail "AGENT_SSE_MAX_EVENT_BYTES must be 131072 for the Analyze Materials Card envelope"
[[ "$postgres_host" == "127.0.0.1" && "$postgres_port" == "15432" ]] || fail "PostgreSQL must use 127.0.0.1:15432"
[[ "$redis_host" == "127.0.0.1" && "$redis_port" == "16379" ]] || fail "Redis must use 127.0.0.1:16379"
[[ "$etcd_host" == "127.0.0.1" && "$etcd_port" == "12379" ]] || fail "etcd must use 127.0.0.1:12379"
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"
command -v nc >/dev/null 2>&1 || fail "nc is required"
command -v shasum >/dev/null 2>&1 || fail "shasum is required"
[[ -x "$go_bin" ]] || fail "Go SDK is required"
[[ -x "$migrate_bin" ]] || fail "golang-migrate is required"

BUSINESS_DATABASE_URL="${BUSINESS_CONTRACT_DATABASE_URL:-}"
AGENT_DATABASE_URL="${AGENT_CONTRACT_DATABASE_URL:-}"
export BUSINESS_DATABASE_URL AGENT_DATABASE_URL
validate_test_database_url "$BUSINESS_DATABASE_URL" dora_business_app dora_business_test
validate_test_database_url "$AGENT_DATABASE_URL" dora_agent_app dora_agent_test
wait_tcp "$postgres_host" "$postgres_port" PostgreSQL
wait_tcp "$redis_host" "$redis_port" Redis
wait_tcp "$etcd_host" "$etcd_port" etcd
curl --fail --silent --show-error --max-time 2 "http://${etcd_host}:${etcd_port}/health" | jq -e '.health == "true"' >/dev/null || fail "etcd direct health failed"
etcd_prefix_count '/dora/services/' >/dev/null || fail "etcd direct range query failed"

for port_label in "$business_http_port:Business-HTTP" "$agent_http_port:Agent-HTTP" "$business_rpc_port:Business-RPC" "$agent_rpc_port:Agent-RPC" "$vite_port:Vite"; do
  assert_port_available "${port_label%%:*}" "${port_label#*:}"
done

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/dora-analyze-materials-smoke.XXXXXX")"
chmod 700 "$work_dir"
mkdir -p "$repo_root/.local/bin" "$(dirname "$evidence_file")"
write_source_manifest "$work_dir/source-before.sha256" || fail "could not freeze the source manifest"

GOWORK=off "$go_bin" -C "$repo_root/business" build -o "$repo_root/.local/bin/business-service" ./cmd/business-service
GOWORK=off "$go_bin" -C "$repo_root/agent" build -o "$repo_root/.local/bin/agent-service" ./cmd/agent-service
GOWORK=off "$go_bin" -C "$repo_root/agent" build -tags localsmoke \
  -o "$repo_root/.local/bin/local-smoke-analyze-materials-authority" ./cmd/local-smoke-analyze-materials-authority
GOWORK=off "$go_bin" -C "$repo_root/business" build -tags localsmoke \
  -o "$repo_root/.local/bin/local-smoke-analyze-materials-fixture" ./cmd/local-smoke-analyze-materials-fixture
DORA_SMOKE_ANALYZE_MATERIALS_MODE=probe \
  "$repo_root/.local/bin/local-smoke-analyze-materials-authority" >"$work_dir/direct-host-probe.json" || \
  fail "direct host PostgreSQL/Redis Go client probe failed"
jq -e '.schema_version == "analyze_materials_runtime.authority.v1" and .mode == "probe" and .postgresql_direct == true and .redis_direct == true' \
  "$work_dir/direct-host-probe.json" >/dev/null || fail "direct host probe output is invalid"
reset_test_database business "$BUSINESS_DATABASE_URL"
reset_test_database agent "$AGENT_DATABASE_URL"
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" business up
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" agent up
(
  cd "$repo_root/business"
  GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-seeder
) >"$work_dir/user-seed.json" 2>"$work_dir/user-seed.log" || fail "local smoke user seed failed"

local_ipv4="$(discover_local_ipv4)" || fail "a non-loopback local IPv4 is required for service discovery"
export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false
export DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false
export DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=true
export DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_PROFILE=analyze_materials.runtime.v2preview1
export DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED=true
export DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED=true
export BUSINESS_INSTANCE_ID="business-amr-${run_id}"
export BUSINESS_HTTP_ADDR="127.0.0.1:${business_http_port}"
export BUSINESS_ADVERTISED_ADDRESS="${local_ipv4}:${business_http_port}"
export BUSINESS_RPC_LISTEN_ADDR="0.0.0.0:${business_rpc_port}"
export BUSINESS_RPC_ADVERTISED_ADDRESS="${local_ipv4}:${business_rpc_port}"
export BUSINESS_AGENT_HTTP_BASE_URL="http://127.0.0.1:${agent_http_port}"
export AGENT_INSTANCE_ID="agent-amr-${run_id}"
export AGENT_HTTP_ADDR="127.0.0.1:${agent_http_port}"
export AGENT_ADVERTISED_ADDRESS="${local_ipv4}:${agent_http_port}"
export AGENT_RPC_LISTEN_ADDR="0.0.0.0:${agent_rpc_port}"
export AGENT_RPC_ADVERTISED_ADDRESS="${local_ipv4}:${agent_rpc_port}"

"$repo_root/.local/bin/business-service" >"$work_dir/Business.log" 2>&1 &
business_pid="$!"
wait_http_ready "$business_http_port" "$business_pid" Business
"$repo_root/.local/bin/agent-service" >"$work_dir/Agent.log" 2>&1 &
agent_pid="$!"
wait_http_ready "$agent_http_port" "$agent_pid" Agent
wait_etcd_prefix_count '/dora/services/dora-business-service/' 1 Business
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent

login_payload="$(build_login_json "$DORA_SMOKE_USER_EMAIL" "$DORA_SMOKE_USER_PASSWORD")"
login_status="$(curl_with_body_stdin "$login_payload" --silent --show-error --max-time 10 \
  -c "$work_dir/cookies.txt" -H 'Content-Type: application/json' -o "$work_dir/login.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/auth/session")"
[[ "$login_status" == "200" ]] || fail "login returned $login_status"
user_id="$(jq -er '.principal.id' "$work_dir/login.json")"
csrf_token="$(jq -er '.csrf_token' "$work_dir/login.json")"
write_curl_header_config "$work_dir/csrf.curl" X-CSRF-Token "$csrf_token" || fail "could not freeze CSRF curl config"

quick_key="amr-quick-${run_id}"
quick_payload='{"initial_prompt":""}'
quick_status="$(curl_with_body_stdin "$quick_payload" --silent --show-error --max-time 10 \
  -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $quick_key" -o "$work_dir/quick.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/projects:quick-create")"
[[ "$quick_status" == "201" ]] || fail "blank Quick Create returned $quick_status"
project_id="$(jq -er '.project_id' "$work_dir/quick.json")"
poll_bootstrap_ready "$project_id" "$work_dir/bootstrap.json"
session_id="$(jq -er '.session_id' "$work_dir/bootstrap.json")"

idempotency_key='019f1000-0000-7000-8000-000000000003'
DORA_SMOKE_ANALYZE_MATERIALS_MODE=seed DORA_SMOKE_PROJECT_ID="$project_id" \
DORA_SMOKE_OWNER_USER_ID="$user_id" \
  "$repo_root/.local/bin/local-smoke-analyze-materials-fixture" >"$work_dir/business-fixture.json" || \
  fail "Business Analyze Materials fixture seed failed"
jq -e '
  .schema_version == "analyze_materials.local_smoke.fixture.v1" and .mode == "seed"
  and .asset_version == 1 and .counts.asset_count == 1 and .counts.evidence_count == 1
  and .counts.creation_spec_count == 0 and .counts.creation_spec_receipt_count == 0
  and (.digests.content_sha256 | test("^[0-9a-f]{64}$"))
  and (.digests.authority_sha256 | test("^[0-9a-f]{64}$"))
' "$work_dir/business-fixture.json" >/dev/null || fail "Business Analyze Materials fixture output is invalid"
asset_id="$(jq -er '.asset_id' "$work_dir/business-fixture.json")"
evidence_id="$(jq -er '.evidence_id' "$work_dir/business-fixture.json")"

intent="$(jq -cn --arg asset "$asset_id" '{schema_version:"analyze_materials.preview.intent.v1",asset_ids:[$asset],analysis_goal:"识别素材主题和可复用元素",focus_dimensions:["visual"],output_language:"zh-CN",expected_assets:[{asset_id:$asset,asset_version:1}]}')"
enqueue_status="$(curl_with_body_stdin "$intent" --silent --show-error --max-time 10 \
  -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $idempotency_key" -o "$work_dir/enqueue.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/analyze-materials-previews")"
[[ "$enqueue_status" == "202" ]] || fail "Analyze Materials enqueue returned $enqueue_status"
jq -e '
  keys == ["input_id","replayed","request_id","run_id","schema_version","session_id","status","tool_call_id","turn_id"]
  and .schema_version == "analyze_materials.preview.enqueue.v1" and .status == "pending" and .replayed == false
  and all(.request_id,.session_id,.input_id,.turn_id,.run_id,.tool_call_id; test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
' "$work_dir/enqueue.json" >/dev/null || fail "enqueue 202 exact DTO is invalid"

input_id="$(jq -er '.input_id' "$work_dir/enqueue.json")"
turn_id="$(jq -er '.turn_id' "$work_dir/enqueue.json")"
runtime_run_id="$(jq -er '.run_id' "$work_dir/enqueue.json")"
tool_call_id="$(jq -er '.tool_call_id' "$work_dir/enqueue.json")"

replay_status="$(curl_with_body_stdin "$intent" --silent --show-error --max-time 10 \
  -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $idempotency_key" -o "$work_dir/replay.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/analyze-materials-previews")"
[[ "$replay_status" == "202" ]] || fail "idempotent replay returned $replay_status"
jq -e --slurpfile first "$work_dir/enqueue.json" '
  .replayed == true and .session_id == $first[0].session_id and .input_id == $first[0].input_id
  and .turn_id == $first[0].turn_id and .run_id == $first[0].run_id and .tool_call_id == $first[0].tool_call_id
' "$work_dir/replay.json" >/dev/null || fail "idempotent replay changed stable identities"

conflict_intent="$(jq -c '.analysis_goal="不同语义"' <<<"$intent")"
conflict_status="$(curl_with_body_stdin "$conflict_intent" --silent --show-error --max-time 10 \
  -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $idempotency_key" -o "$work_dir/conflict.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/analyze-materials-previews")"
[[ "$conflict_status" == "409" ]] || fail "same-key semantic conflict returned $conflict_status"
jq -e '.error.code == "IDEMPOTENCY_CONFLICT"' "$work_dir/conflict.json" >/dev/null || fail "conflict code drifted"

poll_runtime_terminal "$session_id" "$input_id" "$turn_id" "$runtime_run_id" "$tool_call_id"
collect_authority "$session_id" "$input_id" "$turn_id" "$runtime_run_id" "$tool_call_id" "$work_dir/authority-before.json"
jq -e '
  .schema_version == "analyze_materials_runtime.authority.v1" and .mode == "authority"
  and .counts.input_count == 1 and .counts.message_count == 0 and .counts.run_count == 1 and .counts.context_count == 1
  and .counts.context_pins_valid == true and .counts.context_digests_valid == true
  and .counts.model_receipt_count == 2 and .counts.router_model_receipt_count == 1 and .counts.graph_model_receipt_count == 1
  and .counts.tool_receipt_count == 1 and .counts.projection_count == 1
  and .counts.accepted_event_count == 1 and .counts.terminal_event_count == 1 and .counts.event_high_watermark == 3
  and .counts.creation_spec_preview_count == 0 and .counts.user_message_turn_count == 0 and .counts.unsafe_event_payload_count == 0
' "$work_dir/authority-before.json" >/dev/null || fail "PostgreSQL authority invariants failed"

workspace_status="$(curl --silent --show-error --max-time 10 -b "$work_dir/cookies.txt" \
  --config "$work_dir/csrf.curl" -o "$work_dir/workspace.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$workspace_status" == "200" ]] || fail "Workspace Snapshot returned $workspace_status"
jq -e --arg input "$input_id" --arg turn "$turn_id" --arg run "$runtime_run_id" --arg tool "$tool_call_id" '
  .schema_version == "session.workspace.v2" and .messages == []
  and ([.inputs[] | select(.id == $input and .message_id == null and .source_type == "analyze_materials_preview" and .status == "resolved")] | length) == 1
  and .analyze_materials_preview.schema_version == "analyze_materials.preview.card.v1"
  and .analyze_materials_preview.input_id == $input and .analyze_materials_preview.turn_id == $turn
  and .analyze_materials_preview.run_id == $run and .analyze_materials_preview.tool_call_id == $tool
  and .analyze_materials_preview.status == "completed"
  and .analyze_materials_preview.result_code == "MATERIAL_ANALYSIS_PREVIEW_COMPLETED"
  and .analyze_materials_preview.analysis != null and .analyze_materials_preview.coverage.status == "completed"
  and (.analyze_materials_preview.evidence_refs | length) == 1
  and .event_high_watermark == 3
' "$work_dir/workspace.json" >/dev/null || fail "Workspace Snapshot omitted or corrupted the safe Card"

set +e
curl --silent --show-error --no-buffer --max-time 8 -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/events?after_seq=1" \
  >"$work_dir/events.sse" 2>"$work_dir/events-curl.log"
sse_status="$?"
set -e
[[ "$sse_status" == "0" || "$sse_status" == "28" ]] || fail "Workspace SSE curl failed with $sse_status"
grep -F 'event: analyze_materials.preview.accepted' "$work_dir/events.sse" >/dev/null || fail "SSE omitted accepted event"
grep -F 'event: analyze_materials.preview.completed' "$work_dir/events.sse" >/dev/null || fail "SSE omitted terminal event"
grep -F '"source_type":"analyze_materials_preview"' "$work_dir/events.sse" >/dev/null || fail "SSE accepted payload source drifted"
if rg -n 'message_id|intent_ciphertext|provider_payload|reasoning|画面展示一辆红色自行车' "$work_dir/events.sse" >/dev/null; then
  fail "SSE exposed a forbidden payload field or Evidence content"
fi

collect_authority "$session_id" "$input_id" "$turn_id" "$runtime_run_id" "$tool_call_id" "$work_dir/authority-after.json"
cmp -s "$work_dir/authority-before.json" "$work_dir/authority-after.json" || fail "replay/read paths changed PostgreSQL authority facts"
DORA_SMOKE_ANALYZE_MATERIALS_MODE=authority DORA_SMOKE_PROJECT_ID="$project_id" \
DORA_SMOKE_OWNER_USER_ID="$user_id" \
  "$repo_root/.local/bin/local-smoke-analyze-materials-fixture" >"$work_dir/business-authority.json" || \
  fail "Business Analyze Materials authority probe failed"
jq -e --slurpfile seeded "$work_dir/business-fixture.json" '
  .schema_version == "analyze_materials.local_smoke.fixture.v1" and .mode == "authority"
  and .asset_id == $seeded[0].asset_id and .evidence_id == $seeded[0].evidence_id
  and .asset_version == $seeded[0].asset_version and .counts == $seeded[0].counts and .digests == $seeded[0].digests
  and .counts.asset_count == 1 and .counts.evidence_count == 1
  and .counts.creation_spec_count == 0 and .counts.creation_spec_receipt_count == 0
' "$work_dir/business-authority.json" >/dev/null || fail "read-only material analysis changed Business authority facts"

(
  cd "$repo_root/frontend"
  exec env VITE_BUSINESS_API_TARGET="http://127.0.0.1:${business_http_port}" \
    VITE_AGENT_API_TARGET="http://127.0.0.1:${agent_http_port}" \
    VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED=false \
    ./node_modules/.bin/vite --host 127.0.0.1 --port "$vite_port" --strictPort
) >"$work_dir/Vite.log" 2>&1 &
vite_pid="$!"
wait_vite_ready

browser_result="$work_dir/browser-result.json"
(
  cd "$repo_root/frontend"
  DORA_E2E_ANALYZE_MATERIALS_RUNTIME=1 \
  DORA_E2E_EXTERNAL_SERVER=1 \
  DORA_E2E_BASE_URL="http://127.0.0.1:${vite_port}" \
  DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:${business_http_port}" \
  DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
  DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
  DORA_E2E_ANALYZE_MATERIALS_RESULT_PATH="$browser_result" \
  DORA_E2E_PROJECT_ID="$project_id" DORA_E2E_SESSION_ID="$session_id" \
  DORA_E2E_INPUT_ID="$input_id" DORA_E2E_TURN_ID="$turn_id" \
  DORA_E2E_RUN_ID="$runtime_run_id" DORA_E2E_TOOL_CALL_ID="$tool_call_id" \
    ./node_modules/.bin/playwright test e2e/analyze-materials-runtime.spec.js \
      --grep '@analyze-materials-runtime'
) >"$work_dir/Playwright.log" 2>&1 &
playwright_pid="$!"
set +e
wait "$playwright_pid"
playwright_status="$?"
set -e
playwright_pid=""
if [[ "$playwright_status" -ne 0 ]]; then
  sed -n '1,240p' "$work_dir/Playwright.log" >&2 || true
  fail "Analyze Materials Chromium smoke failed"
fi
jq -e '
  .schema_version == "analyze_materials_runtime.browser_result.v1" and .status == "passed"
  and all(.assertions[]; . == true)
' "$browser_result" >/dev/null || fail "Analyze Materials browser result is invalid"
[[ "$(stat -f '%Lp' "$browser_result" 2>/dev/null || stat -c '%a' "$browser_result")" == "600" ]] || fail "browser result permissions are not 0600"

collect_authority "$session_id" "$input_id" "$turn_id" "$runtime_run_id" "$tool_call_id" "$work_dir/authority-browser.json"
cmp -s "$work_dir/authority-before.json" "$work_dir/authority-browser.json" || fail "browser read paths changed Agent authority facts"
write_source_manifest "$work_dir/source-after.sha256" || fail "could not rebuild the source manifest"
cmp -s "$work_dir/source-before.sha256" "$work_dir/source-after.sha256" || fail "source tree changed during canonical Analyze Materials Trial"
source_digest="$(sha256_file "$work_dir/source-before.sha256")"

stop_pid_best_effort "$vite_pid"
vite_pid=""

stop_pid_best_effort "$agent_pid"
agent_pid=""
stop_pid_best_effort "$business_pid"
business_pid=""
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 0 Agent
wait_etcd_prefix_count '/dora/services/dora-business-service/' 0 Business
for port in "$business_http_port" "$agent_http_port" "$business_rpc_port" "$agent_rpc_port" "$vite_port"; do
  nc -z 127.0.0.1 "$port" >/dev/null 2>&1 && fail "local Runtime cleanup left port $port open"
done

jq -n -S \
  --arg run_id "$run_id" --arg project_id "$project_id" --arg session_id "$session_id" \
  --arg input_id "$input_id" --arg turn_id "$turn_id" --arg runtime_run_id "$runtime_run_id" \
  --arg tool_call_id "$tool_call_id" --arg asset_id "$asset_id" \
  --arg source_digest "sha256:${source_digest}" \
  --argjson postgres_port "$postgres_port" --argjson redis_port "$redis_port" --argjson etcd_port "$etcd_port" \
  --slurpfile authority "$work_dir/authority-before.json" '
  {
    schema_version:"analyze_materials_runtime_v2_smoke_evidence.v1",status:"passed",run_id:$run_id,
    profile:"analyze_materials.runtime.v2preview1",
    source_digest:$source_digest,
    infrastructure:{postgresql:{host:"127.0.0.1",port:$postgres_port,database:"dora_agent_test"},redis:{host:"127.0.0.1",port:$redis_port,direct_ping:true},etcd:{host:"127.0.0.1",port:$etcd_port,direct_health:true}},
    identity:{project_id:$project_id,session_id:$session_id,input_id:$input_id,turn_id:$turn_id,run_id:$runtime_run_id,tool_call_id:$tool_call_id,asset_id:$asset_id},
    counts:$authority[0].counts,
    assertions:{enqueue_202:true,idempotent_replay:true,semantic_conflict_409:true,accepted_sse:true,terminal_sse:true,workspace_snapshot:true,context_frozen:true,model_receipts_two_layers:true,tool_receipt_frozen:true,projection_unique:true,event_exact_set:true,no_message:true,no_business_write:true,no_other_runtime_facts:true,direct_host_infrastructure:true,chromium_read_only_card:true,static_catalog_unavailable:true,hard_reload_recovered:true,source_unchanged:true,etcd_exact_instances_cleaned:true,runtime_cleanup:true,evidence_redacted:true}
  }
' >"${evidence_file}.tmp"
chmod 600 "${evidence_file}.tmp"
mv "${evidence_file}.tmp" "$evidence_file"
chmod 600 "$evidence_file"
jq -e '.status == "passed" and all(.assertions[]; . == true)' "$evidence_file" >/dev/null || fail "published Evidence is invalid"
[[ "$(stat -f '%Lp' "$evidence_file" 2>/dev/null || stat -c '%a' "$evidence_file")" == "600" ]] || fail "published Evidence permissions are not 0600"

printf 'analyze-materials-runtime-v2 direct-host smoke passed: %s\n' "$evidence_file"
