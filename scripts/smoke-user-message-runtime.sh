#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/smoke-secret-transport.sh
. "$repo_root/scripts/lib/smoke-secret-transport.sh"
disable_shell_xtrace
umask 077

env_file="${ENV_FILE:-$repo_root/.env.example}"
go_bin="${GO_BIN:-go}"
migrate_bin="${MIGRATE_BIN:-$repo_root/.local/tools/migrate}"
postgres_host="${DORA_SMOKE_POSTGRES_HOST:-127.0.0.1}"
postgres_port="${DORA_SMOKE_POSTGRES_PORT:-15432}"
redis_host="${DORA_SMOKE_REDIS_HOST:-127.0.0.1}"
redis_port="${DORA_SMOKE_REDIS_PORT:-16379}"
etcd_host="${DORA_SMOKE_ETCD_HOST:-127.0.0.1}"
etcd_port="${DORA_SMOKE_ETCD_PORT:-12379}"
postgres_container="${DORA_SMOKE_POSTGRES_CONTAINER:-dora-local-postgres-1}"
business_http_port="${DORA_USER_MESSAGE_BUSINESS_HTTP_PORT:-28081}"
agent_http_port="${DORA_USER_MESSAGE_AGENT_HTTP_PORT:-28082}"
business_rpc_port="${DORA_USER_MESSAGE_BUSINESS_RPC_PORT:-29081}"
agent_rpc_port="${DORA_USER_MESSAGE_AGENT_RPC_PORT:-29082}"
vite_port="${DORA_USER_MESSAGE_VITE_PORT:-3201}"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
evidence_file="${USER_MESSAGE_RUNTIME_EVIDENCE_FILE:-$repo_root/.local/smoke/user-message-runtime-trial-evidence.json}"
evidence_pending="${evidence_file}.pending"
work_dir=""
browser_result=""
authority_result=""
legacy_seed_result=""
legacy_authority_result=""
business_pid=""
agent_pid=""
vite_pid=""
playwright_pid=""
etcd_monitor_pid=""
etcd_monitor_stop=""
etcd_monitor_violation=""

fail() {
  printf 'user-message-runtime smoke failed: %s\n' "$1" >&2
  exit 1
}

file_mode() {
  stat -f '%Lp' "$1" 2>/dev/null || stat -c '%a' "$1"
}

sha256_file() {
  local output=""
  local digest=""
  output="$(shasum -a 256 "$1")" || return 1
  digest="${output%% *}"
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || return 1
  printf '%s' "$digest"
}

stop_pid_best_effort() {
  local pid="$1"
  [[ -n "$pid" ]] || return 0
  if kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
    for _ in $(seq 1 120); do
      local state=""
      state="$(ps -o stat= -p "$pid" 2>/dev/null | tr -d '[:space:]' || true)"
      if ! kill -0 "$pid" 2>/dev/null || [[ "$state" == Z* ]]; then
        break
      fi
      sleep 0.25
    done
    if kill -0 "$pid" 2>/dev/null; then
      kill -KILL "$pid" 2>/dev/null || true
    fi
  fi
  wait "$pid" 2>/dev/null || true
}

stop_etcd_monitor_best_effort() {
  [[ -n "$etcd_monitor_pid" ]] || return 0
  if [[ -n "$etcd_monitor_stop" ]]; then
    : >"$etcd_monitor_stop"
    chmod 600 "$etcd_monitor_stop" 2>/dev/null || true
  fi
  stop_pid_best_effort "$etcd_monitor_pid"
}

cleanup_on_exit() {
  local exit_code="$?"
  trap - EXIT INT TERM
  stop_pid_best_effort "$playwright_pid"
  stop_etcd_monitor_best_effort
  stop_pid_best_effort "$vite_pid"
  stop_pid_best_effort "$agent_pid"
  stop_pid_best_effort "$business_pid"
  rm -f "$evidence_pending" "${evidence_file}.tmp"
  if [[ "$exit_code" -ne 0 ]]; then
    rm -f "$evidence_file"
  fi
  if [[ -n "$work_dir" ]]; then
    rm -rf "$work_dir"
  fi
  exit "$exit_code"
}
trap cleanup_on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

wait_tcp() {
  local host="$1"
  local port="$2"
  local label="$3"
  for _ in $(seq 1 120); do
    if nc -z "$host" "$port" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  fail "$label is not reachable at the configured host port"
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
  local path="${4:-/readyz}"
  for _ in $(seq 1 200); do
    local state=""
    state="$(ps -o stat= -p "$pid" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$pid" 2>/dev/null || [[ "$state" == Z* ]]; then
      if [[ -n "$work_dir" && -r "$work_dir/${label}.log" ]]; then
        sed -n '1,220p' "$work_dir/${label}.log" >&2 || true
      fi
      fail "$label exited before readiness"
    fi
    if curl --fail --silent --max-time 1 "http://127.0.0.1:${port}${path}" >/dev/null; then
      return 0
    fi
    sleep 0.25
  done
  fail "$label readiness timed out"
}

discover_local_ipv4() {
  local candidate=""
  local interface=""
  if command -v route >/dev/null 2>&1 && command -v ipconfig >/dev/null 2>&1; then
    interface="$(route -n get default 2>/dev/null | awk '$1 == "interface:" {print $2; exit}')"
    if [[ -n "$interface" ]]; then
      candidate="$(ipconfig getifaddr "$interface" 2>/dev/null || true)"
    fi
  fi
  if [[ -z "$candidate" ]] && command -v ip >/dev/null 2>&1; then
    candidate="$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == "src") {print $(i + 1); exit}}')"
  fi
  if [[ -z "$candidate" ]] && command -v ifconfig >/dev/null 2>&1; then
    candidate="$(ifconfig | awk '$1 == "inet" && $2 !~ /^127\./ && $2 !~ /^169\.254\./ && $2 != "0.0.0.0" {print $2; exit}')"
  fi
  [[ -n "$candidate" && "$candidate" != 127.* && "$candidate" != "0.0.0.0" ]] || return 1
  printf '%s' "$candidate"
}

etcd_prefix_keys() {
  local prefix="$1"
  local range_end="${prefix%?}0"
  local request=""
  local response=""
  request="$(jq -cn --arg key "$prefix" --arg range_end "$range_end" \
    '{key:($key|@base64),range_end:($range_end|@base64),keys_only:true}')" || return 1
  response="$(curl --fail --silent --show-error --max-time 2 -X POST \
    -H 'Content-Type: application/json' --data-binary "$request" \
    "http://${etcd_host}:${etcd_port}/v3/kv/range")" || return 1
  jq -e '(.kvs? // []) | type == "array"' <<<"$response" >/dev/null || return 1
  jq -r '.kvs[]?.key | @base64d' <<<"$response"
}

wait_etcd_prefix_empty() {
  local prefix="$1"
  local label="$2"
  local keys=""
  for _ in $(seq 1 120); do
    keys="$(etcd_prefix_keys "$prefix")" || fail "$label etcd prefix query failed"
    if [[ -z "$keys" ]]; then
      return 0
    fi
    sleep 0.25
  done
  fail "$label etcd prefix contains another or stale Runtime instance"
}

assert_etcd_registration() {
  local prefix="$1"
  local instance_id="$2"
  local service="$3"
  local address="$4"
  local keys=""
  local request=""
  local response=""
  local registration=""
  keys="$(etcd_prefix_keys "$prefix")" || fail "$service etcd prefix query failed"
  [[ "$keys" == "${prefix}${instance_id}" ]] || fail "$service etcd provenance is not the exact managed instance"
  request="$(jq -cn --arg key "${prefix}${instance_id}" '{key:($key|@base64)}')" || \
    fail "$service etcd registration request encoding failed"
  response="$(curl --fail --silent --show-error --max-time 2 -X POST \
    -H 'Content-Type: application/json' --data-binary "$request" \
    "http://${etcd_host}:${etcd_port}/v3/kv/range")" || fail "$service etcd registration query failed"
  registration="$(jq -er 'if (.kvs | length) == 1 then .kvs[0].value | @base64d else error("not exact") end' \
    <<<"$response")" || fail "$service etcd registration is not unique"
  jq -e --arg service "$service" --arg instance_id "$instance_id" --arg address "$address" '
    .service == $service and .instance_id == $instance_id and .address == $address and (.version | length) > 0
  ' <<<"$registration" >/dev/null || fail "$service etcd registration payload is invalid"
}

monitor_etcd_provenance() {
  local business_prefix="/dora/services/dora.business.foundation.v1/"
  local agent_prefix="/dora/services/dora.agent.session.v1/"
  local business_key="${business_prefix}${BUSINESS_INSTANCE_ID}"
  local agent_key="${agent_prefix}${AGENT_INSTANCE_ID}"
  local business_keys=""
  local agent_keys=""
  while [[ ! -e "$etcd_monitor_stop" ]]; do
    business_keys="$(etcd_prefix_keys "$business_prefix")" || {
      printf 'business_prefix_query_failed\n' >"$etcd_monitor_violation"
      return 1
    }
    agent_keys="$(etcd_prefix_keys "$agent_prefix")" || {
      printf 'agent_prefix_query_failed\n' >"$etcd_monitor_violation"
      return 1
    }
    if [[ "$business_keys" != "$business_key" || "$agent_keys" != "$agent_key" ]]; then
      printf 'service_prefix_drift\n' >"$etcd_monitor_violation"
      return 1
    fi
    sleep 0.25
  done
}

write_source_manifest() {
  local output_file="$1"
  local source_file=""
  local digest=""
  : >"$output_file"
  while IFS= read -r source_file; do
    digest="$(sha256_file "$repo_root/$source_file")" || return 1
    printf '%s  %s\n' "$digest" "$source_file" >>"$output_file"
  done < <(
    cd "$repo_root"
    {
      find business agent -type f \( -name '*.go' -o -name '*.sql' -o -name '*.thrift' -o -name 'go.mod' -o -name 'go.sum' \) -print
      find frontend/src frontend/e2e frontend/scripts scripts -type f -print
      find frontend -maxdepth 1 -type f \( -name 'package.json' -o -name 'package-lock.json' \) -print
      printf '%s\n' Makefile frontend/playwright.config.js frontend/vite.config.js
    } | LC_ALL=C sort -u
  )
  [[ -s "$output_file" ]]
}

validate_agent_test_dsn() {
  local dsn="$1"
  local prefix="postgres://dora_agent_app:"
  local suffix="@${postgres_host}:${postgres_port}/dora_agent_test?sslmode=disable"
  local password=""

  [[ "$dsn" == "$prefix"*"$suffix" ]] || \
    fail "Agent PostgreSQL DSN must explicitly target the local dora_agent_test database"
  password="${dsn#"$prefix"}"
  password="${password%"$suffix"}"
  [[ -n "$password" && "$password" != *['@/:?#']* ]] || \
    fail "Agent PostgreSQL test DSN credentials are invalid"
}

reset_agent_test_schema() {
  local output_file="$1"
  local current_database=""

  current_database="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent_test -qAtc \
    'SELECT current_database()')" || fail "dedicated Agent test database identity query failed"
  [[ "$current_database" == "dora_agent_test" ]] || \
    fail "refusing to reset a PostgreSQL database other than dora_agent_test"
  docker exec -i "$postgres_container" psql -v ON_ERROR_STOP=1 -q \
    -U dora_admin -d dora_agent_test <<'SQL' >"$output_file"
DROP SCHEMA IF EXISTS agent CASCADE;
DROP TABLE IF EXISTS public.schema_migrations;
SQL
  chmod 600 "$output_file"
}

collect_postgresql_authority() {
  local project_id="$1"
  local session_id="$2"
  local input_id="$3"
  local turn_id="$4"
  local run_id="$5"
  local output_file="$6"

  docker exec -i "$postgres_container" psql -v ON_ERROR_STOP=1 -qAt \
    -U dora_admin -d dora_agent_test \
    -v project_id="$project_id" -v session_id="$session_id" -v input_id="$input_id" \
    -v turn_id="$turn_id" -v run_id="$run_id" <<'SQL' >"$output_file"
SELECT json_build_object(
  'session_count', (SELECT count(*) FROM agent.session WHERE id = :'session_id'::uuid AND project_id = :'project_id'::uuid),
  'user_message_count', (SELECT count(*) FROM agent.session_message WHERE session_id = :'session_id'::uuid AND role = 'user'),
  'non_user_message_count', (SELECT count(*) FROM agent.session_message WHERE session_id = :'session_id'::uuid AND role <> 'user'),
  'input_count', (SELECT count(*) FROM agent.session_input WHERE id = :'input_id'::uuid AND session_id = :'session_id'::uuid AND source_type = 'user_message' AND status = 'resolved' AND lease_owner IS NULL AND lease_until IS NULL),
  'turn_count', (SELECT count(*) FROM agent.session_user_message_turn WHERE turn_id = :'turn_id'::uuid AND input_id = :'input_id'::uuid AND session_id = :'session_id'::uuid AND project_id = :'project_id'::uuid AND status = 'completed'),
  'context_count', (SELECT count(*) FROM agent.session_user_message_turn_context WHERE turn_id = :'turn_id'::uuid AND input_id = :'input_id'::uuid AND schema_version = 'user_message.turn_context.v2preview1'),
  'context_digest', (SELECT context_digest FROM agent.session_user_message_turn_context WHERE turn_id = :'turn_id'::uuid),
  'tool_registry_ref', (SELECT tool_registry_ref FROM agent.session_user_message_turn_context WHERE turn_id = :'turn_id'::uuid),
  'model_route_ref', (SELECT model_route_ref FROM agent.session_user_message_turn_context WHERE turn_id = :'turn_id'::uuid),
  'run_count', (SELECT count(*) FROM agent.session_user_message_run WHERE run_id = :'run_id'::uuid AND turn_id = :'turn_id'::uuid AND input_id = :'input_id'::uuid AND status = 'completed'),
  'model_receipt_count', (SELECT count(*) FROM agent.session_user_message_model_receipt WHERE run_id = :'run_id'::uuid AND turn_id = :'turn_id'::uuid AND input_id = :'input_id'::uuid AND status = 'completed'),
  'model_execution_fence_consistent', EXISTS (
    SELECT 1 FROM agent.session_user_message_model_receipt AS model_receipt
    JOIN agent.session_user_message_run AS run_record ON run_record.run_id = model_receipt.run_id
    WHERE model_receipt.run_id = :'run_id'::uuid AND model_receipt.turn_id = :'turn_id'::uuid
      AND model_receipt.input_id = :'input_id'::uuid AND model_receipt.status = 'completed'
      AND model_receipt.execution_fence = run_record.owner_fence AND model_receipt.execution_fence > 0
  ),
  'output_receipt_count', (SELECT count(*) FROM agent.session_user_message_output_receipt WHERE run_id = :'run_id'::uuid AND turn_id = :'turn_id'::uuid AND input_id = :'input_id'::uuid AND schema_version = 'session.turn.direct_response.card.v1' AND status = 'completed'),
  'result_digest', (SELECT result_digest FROM agent.session_user_message_output_receipt WHERE turn_id = :'turn_id'::uuid),
  'projection_count', (SELECT count(*) FROM agent.session_user_message_output_projection WHERE session_id = :'session_id'::uuid AND source_input_id = :'input_id'::uuid AND turn_id = :'turn_id'::uuid AND run_id = :'run_id'::uuid AND schema_version = 'session.turn.direct_response.card.v1' AND status = 'completed'),
  'terminal_event_count', (SELECT count(*) FROM agent.session_event_log WHERE session_id = :'session_id'::uuid AND event_type = 'session.turn.completed' AND source_kind = 'user_message_runtime' AND aggregate_type = 'session_turn' AND aggregate_id = :'turn_id'::uuid),
  'event_high_watermark', (SELECT last_seq FROM agent.session_event_counter WHERE session_id = :'session_id'::uuid),
  'creation_spec_preview_run_count', (SELECT count(*) FROM agent.creation_spec_preview_run WHERE session_id = :'session_id'::uuid)
);
SQL
  chmod 600 "$output_file"
}

collect_legacy_postgresql_authority() {
  local session_id="$1"
  local input_id="$2"
  local message_id="$3"
  local command_id="$4"
  local output_file="$5"

  docker exec -i "$postgres_container" psql -v ON_ERROR_STOP=1 -qAt \
    -U dora_admin -d dora_agent_test \
    -v session_id="$session_id" -v input_id="$input_id" -v message_id="$message_id" \
    -v command_id="$command_id" <<'SQL' >"$output_file"
SELECT json_build_object(
  'session_count', (SELECT count(*) FROM agent.session WHERE id = :'session_id'::uuid AND status = 'active' AND archived_at IS NULL),
  'message_count', (SELECT count(*) FROM agent.session_message WHERE id = :'message_id'::uuid AND session_id = :'session_id'::uuid AND message_seq = 1 AND role = 'user' AND source_kind = 'ensure_project_session' AND source_id = :'command_id'::uuid),
  'non_user_message_count', (SELECT count(*) FROM agent.session_message WHERE session_id = :'session_id'::uuid AND role <> 'user'),
  'input_count', (SELECT count(*) FROM agent.session_input WHERE id = :'input_id'::uuid AND session_id = :'session_id'::uuid AND source_type = 'user_message' AND source_id = :'command_id'::uuid AND message_id = :'message_id'::uuid AND enqueue_seq = 1 AND status = 'resolved' AND lease_owner IS NULL AND lease_until IS NULL),
  'command_receipt_count', (SELECT count(*) FROM agent.session_command_receipt WHERE command_id = :'command_id'::uuid AND command_type = 'ensure_project_session_v1' AND session_id = :'session_id'::uuid AND message_id = :'message_id'::uuid AND input_id = :'input_id'::uuid AND result_version = 1),
  'ledger_count', (SELECT count(*) FROM agent.session_user_message_upgrade_ledger WHERE input_id = :'input_id'::uuid AND session_id = :'session_id'::uuid AND stage = 'verified' AND upgrade_generation = 1 AND version = 3),
  'ledger_stage', (SELECT stage FROM agent.session_user_message_upgrade_ledger WHERE input_id = :'input_id'::uuid),
  'upgrade_generation', (SELECT upgrade_generation FROM agent.session_user_message_upgrade_ledger WHERE input_id = :'input_id'::uuid),
  'ledger_version', (SELECT version FROM agent.session_user_message_upgrade_ledger WHERE input_id = :'input_id'::uuid),
  'turn_id', (SELECT turn_id::text FROM agent.session_user_message_turn WHERE input_id = :'input_id'::uuid),
  'turn_count', (SELECT count(*) FROM agent.session_user_message_turn WHERE input_id = :'input_id'::uuid AND session_id = :'session_id'::uuid AND message_id = :'message_id'::uuid AND status = 'completed'),
  'context_count', (SELECT count(*) FROM agent.session_user_message_turn_context WHERE input_id = :'input_id'::uuid AND session_id = :'session_id'::uuid AND message_id = :'message_id'::uuid AND schema_version = 'user_message.turn_context.v2preview1' AND access_scope_ref = ('ensure_command:' || :'command_id')),
  'tool_registry_ref', (SELECT tool_registry_ref FROM agent.session_user_message_turn_context WHERE input_id = :'input_id'::uuid),
  'model_route_ref', (SELECT model_route_ref FROM agent.session_user_message_turn_context WHERE input_id = :'input_id'::uuid),
  'run_id', (SELECT run_id::text FROM agent.session_user_message_run WHERE input_id = :'input_id'::uuid),
  'run_count', (SELECT count(*) FROM agent.session_user_message_run WHERE input_id = :'input_id'::uuid AND session_id = :'session_id'::uuid AND status = 'completed'),
  'model_receipt_count', (SELECT count(*) FROM agent.session_user_message_model_receipt WHERE input_id = :'input_id'::uuid AND status = 'completed'),
  'model_execution_fence_consistent', EXISTS (
    SELECT 1 FROM agent.session_user_message_model_receipt AS model_receipt
    JOIN agent.session_user_message_run AS run_record ON run_record.run_id = model_receipt.run_id
    WHERE model_receipt.input_id = :'input_id'::uuid AND model_receipt.status = 'completed'
      AND model_receipt.execution_fence = run_record.owner_fence AND model_receipt.execution_fence > 0
  ),
  'output_receipt_count', (SELECT count(*) FROM agent.session_user_message_output_receipt WHERE input_id = :'input_id'::uuid AND schema_version = 'session.turn.direct_response.card.v1' AND status = 'completed'),
  'projection_count', (SELECT count(*) FROM agent.session_user_message_output_projection WHERE session_id = :'session_id'::uuid AND source_input_id = :'input_id'::uuid AND schema_version = 'session.turn.direct_response.card.v1' AND status = 'completed'),
  'terminal_event_count', (SELECT count(*) FROM agent.session_event_log AS event_record JOIN agent.session_user_message_turn AS turn_record ON turn_record.terminal_event_id = event_record.event_id WHERE turn_record.input_id = :'input_id'::uuid AND event_record.session_id = :'session_id'::uuid AND event_record.event_type = 'session.turn.completed' AND event_record.source_kind = 'user_message_runtime' AND event_record.aggregate_type = 'session_turn' AND event_record.aggregate_id = turn_record.turn_id),
  'event_high_watermark', (SELECT last_seq FROM agent.session_event_counter WHERE session_id = :'session_id'::uuid),
  'creation_spec_preview_run_count', (SELECT count(*) FROM agent.creation_spec_preview_run WHERE session_id = :'session_id'::uuid)
);
SQL
  chmod 600 "$output_file"
}

wait_legacy_runtime_completed() {
  local session_id="$1"
  local input_id="$2"
  local message_id="$3"
  local command_id="$4"
  local output_file="$5"

  for _ in $(seq 1 200); do
    collect_legacy_postgresql_authority "$session_id" "$input_id" "$message_id" "$command_id" "$output_file" || \
      fail "legacy PostgreSQL authority query failed"
    if jq -e '
      .ledger_count == 1 and .ledger_stage == "verified" and .upgrade_generation == 1 and .ledger_version == 3
      and .input_count == 1 and .turn_count == 1 and .context_count == 1 and .run_count == 1
      and .model_receipt_count == 1 and .output_receipt_count == 1 and .projection_count == 1
      and .terminal_event_count == 1
    ' "$output_file" >/dev/null; then
      return 0
    fi
    sleep 0.25
  done
  fail "legacy user_message was not upgraded and consumed by the managed Agent Runtime"
}

assert_evidence_redacted() {
  local value=""
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
      local status="$?"
      [[ "$status" == "1" ]] || fail "Trial Evidence redaction scan failed"
    fi
  done
  if rg -ni '(authorization|cookie|csrf|password|secret|private_key|access_token|refresh_token|prompt|ciphertext|nonce|database_url|postgres://)' "$evidence_pending" >/dev/null; then
    fail "Trial Evidence contains a forbidden sensitive field"
  fi
}

mkdir -p "$(dirname "$evidence_file")"
rm -f "$evidence_file" "$evidence_pending" "${evidence_file}.tmp"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/dora-user-message-runtime.XXXXXX")"
chmod 700 "$work_dir"
browser_result="$work_dir/browser-result.json"
authority_result="$work_dir/postgresql-authority.json"
legacy_seed_result="$work_dir/legacy-seed-result.json"
legacy_authority_result="$work_dir/legacy-postgresql-authority.json"

[[ -r "$env_file" ]] || fail "ENV_FILE is not readable"
set -a
. "$env_file"
set +a

# Canonical 方案 A 必须覆盖 ENV_FILE 中的 V1 默认值，两个 Processor 不允许同时运行。
[[ -n "${AGENT_CONTRACT_DATABASE_URL:-}" ]] || \
  fail "AGENT_CONTRACT_DATABASE_URL must explicitly provide the dora_agent_test DSN"
export AGENT_DATABASE_URL="$AGENT_CONTRACT_DATABASE_URL"
export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false
export DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=true
export DORA_AGENT_USER_MESSAGE_RUNTIME_PROFILE=user_message.runtime.v2preview1
export BUSINESS_REDIS_ADDR="${redis_host}:${redis_port}"
export AGENT_REDIS_ADDR="${redis_host}:${redis_port}"
export BUSINESS_ETCD_ENDPOINTS="${etcd_host}:${etcd_port}"
export AGENT_ETCD_ENDPOINTS="${etcd_host}:${etcd_port}"

[[ "${DORA_ENV:-}" == "local" ]] || fail "DORA_ENV must be local"
[[ "$postgres_host" == "127.0.0.1" && "$redis_host" == "127.0.0.1" && "$etcd_host" == "127.0.0.1" ]] || \
  fail "canonical Trial only accepts loopback infrastructure"
[[ "$DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED" == "false" ]] || fail "CreationSpec Preview must be false"
[[ "$DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED" == "true" ]] || fail "User Message Runtime must be true"
[[ "$DORA_AGENT_USER_MESSAGE_RUNTIME_PROFILE" == "user_message.runtime.v2preview1" ]] || fail "runtime profile drifted"
[[ "${BUSINESS_DATABASE_URL:-}" == postgres://*"@${postgres_host}:${postgres_port}/"* ]] || fail "Business PostgreSQL DSN is not the local exposed port"
validate_agent_test_dsn "$AGENT_DATABASE_URL"

command -v "$go_bin" >/dev/null 2>&1 || fail "Go SDK not found"
[[ -x "$migrate_bin" ]] || fail "golang-migrate CLI not found"
command -v curl >/dev/null 2>&1 || fail "curl not found"
command -v jq >/dev/null 2>&1 || fail "jq not found"
command -v nc >/dev/null 2>&1 || fail "nc not found"
command -v shasum >/dev/null 2>&1 || fail "shasum not found"
command -v rg >/dev/null 2>&1 || fail "rg not found"
command -v docker >/dev/null 2>&1 || fail "Docker CLI is required for the dedicated Agent test schema reset and PostgreSQL authority query"
[[ -x "$repo_root/frontend/node_modules/.bin/playwright" ]] || fail "Playwright dependencies are not installed"
[[ -x "$repo_root/frontend/node_modules/.bin/vite" ]] || fail "Vite dependencies are not installed"
[[ -r "$repo_root/frontend/e2e/user-message-runtime.spec.js" ]] || fail "canonical Chromium spec is missing"

wait_tcp "$postgres_host" "$postgres_port" PostgreSQL
wait_tcp "$redis_host" "$redis_port" Redis
wait_tcp "$etcd_host" "$etcd_port" etcd
curl --fail --silent --show-error --max-time 2 "http://${etcd_host}:${etcd_port}/health" \
  | jq -e '.health == "true"' >/dev/null || fail "etcd direct health check failed"
wait_etcd_prefix_empty "/dora/services/dora.business.foundation.v1/" "Business Foundation"
wait_etcd_prefix_empty "/dora/services/dora.agent.session.v1/" "Agent Session"
for port_and_label in \
  "$business_http_port:Business-HTTP" "$agent_http_port:Agent-HTTP" \
  "$business_rpc_port:Business-RPC" "$agent_rpc_port:Agent-RPC" "$vite_port:Vite"; do
  assert_port_available "${port_and_label%%:*}" "${port_and_label#*:}"
done

write_source_manifest "$work_dir/source-before.manifest" || fail "could not build source manifest"
source_digest_before="$(sha256_file "$work_dir/source-before.manifest")" || fail "could not hash source manifest"

mkdir -p "$repo_root/.local/bin"
GOWORK=off "$go_bin" -C "$repo_root/business" build -o "$repo_root/.local/bin/business-service" ./cmd/business-service || \
  fail "Business Runtime build failed"
GOWORK=off "$go_bin" -C "$repo_root/agent" build -o "$repo_root/.local/bin/agent-service" ./cmd/agent-service || \
  fail "Agent Runtime build failed"
business_binary_sha256="$(sha256_file "$repo_root/.local/bin/business-service")" || fail "Business binary hash failed"
agent_binary_sha256="$(sha256_file "$repo_root/.local/bin/agent-service")" || fail "Agent binary hash failed"

reset_agent_test_schema "$work_dir/agent-test-reset.log"
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" business up
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" agent up
(
  cd "$repo_root/business"
  GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-seeder
) >"$work_dir/seeder.log" 2>&1 || fail "local smoke user seeding failed"
(
  cd "$repo_root/agent"
  GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-user-message-legacy-seeder
) >"$legacy_seed_result" 2>"$work_dir/legacy-seeder.log" || fail "local smoke legacy user_message seeding failed"
chmod 600 "$legacy_seed_result"
jq -e '
  keys == ["command_id","input_id","message_id","session_id"]
  and all(.session_id,.input_id,.message_id,.command_id;
    test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
' "$legacy_seed_result" >/dev/null || fail "legacy user_message seeder output contract is invalid"
legacy_session_id="$(jq -er '.session_id' "$legacy_seed_result")"
legacy_input_id="$(jq -er '.input_id' "$legacy_seed_result")"
legacy_message_id="$(jq -er '.message_id' "$legacy_seed_result")"
legacy_command_id="$(jq -er '.command_id' "$legacy_seed_result")"

local_ipv4="$(discover_local_ipv4)" || fail "a non-loopback local IPv4 is required for etcd service discovery"
export BUSINESS_INSTANCE_ID="business-umr-${run_id}"
export BUSINESS_HTTP_ADDR="127.0.0.1:${business_http_port}"
export BUSINESS_ADVERTISED_ADDRESS="${local_ipv4}:${business_http_port}"
export BUSINESS_RPC_LISTEN_ADDR="0.0.0.0:${business_rpc_port}"
export BUSINESS_RPC_ADVERTISED_ADDRESS="${local_ipv4}:${business_rpc_port}"
export BUSINESS_AGENT_HTTP_BASE_URL="http://127.0.0.1:${agent_http_port}"
export AGENT_INSTANCE_ID="agent-umr-${run_id}"
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
assert_etcd_registration "/dora/services/dora.business.foundation.v1/" "$BUSINESS_INSTANCE_ID" \
  "dora.business.foundation.v1" "${local_ipv4}:${business_rpc_port}"
assert_etcd_registration "/dora/services/dora.agent.session.v1/" "$AGENT_INSTANCE_ID" \
  "dora.agent.session.v1" "${local_ipv4}:${agent_rpc_port}"
etcd_monitor_stop="$work_dir/etcd-monitor.stop"
etcd_monitor_violation="$work_dir/etcd-monitor-violation.txt"
: >"$etcd_monitor_violation"
chmod 600 "$etcd_monitor_violation"
monitor_etcd_provenance &
etcd_monitor_pid="$!"
wait_legacy_runtime_completed "$legacy_session_id" "$legacy_input_id" "$legacy_message_id" \
  "$legacy_command_id" "$legacy_authority_result"
[[ "$(file_mode "$legacy_authority_result")" == "600" ]] || \
  fail "legacy PostgreSQL authority result mode is not 0600"
jq -e '
  keys == ["command_receipt_count","context_count","creation_spec_preview_run_count","event_high_watermark",
    "input_count","ledger_count","ledger_stage","ledger_version","message_count","model_execution_fence_consistent",
    "model_receipt_count","model_route_ref","non_user_message_count","output_receipt_count","projection_count",
    "run_count","run_id","session_count","terminal_event_count","tool_registry_ref","turn_count","turn_id",
    "upgrade_generation"]
  and .session_count == 1 and .message_count == 1 and .non_user_message_count == 0
  and .input_count == 1 and .command_receipt_count == 1
  and .ledger_count == 1 and .ledger_stage == "verified" and .upgrade_generation == 1 and .ledger_version == 3
  and .turn_count == 1 and .context_count == 1 and .run_count == 1
  and .model_receipt_count == 1 and .output_receipt_count == 1 and .projection_count == 1
  and .model_execution_fence_consistent == true
  and .terminal_event_count == 1 and .event_high_watermark == 3
  and .creation_spec_preview_run_count == 0
  and .tool_registry_ref == "user_message.empty_tools@v1"
  and .model_route_ref == "local.fake.user_message@v1"
  and all(.turn_id,.run_id;
    test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
' "$legacy_authority_result" >/dev/null || \
  fail "legacy PostgreSQL authority facts are incomplete, duplicated, or inconsistent"
legacy_turn_id="$(jq -er '.turn_id' "$legacy_authority_result")"
legacy_runtime_run_id="$(jq -er '.run_id' "$legacy_authority_result")"
(
  cd "$repo_root/frontend"
  exec env \
    VITE_BUSINESS_API_TARGET="http://127.0.0.1:${business_http_port}" \
    VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED=false \
    ./node_modules/.bin/vite --host 127.0.0.1 --port "$vite_port" --strictPort
) >"$work_dir/Vite.log" 2>&1 &
vite_pid="$!"
wait_http_ready "$vite_port" "$vite_pid" Vite "/"

browser_prompt="User Message Runtime canonical Trial ${run_id}"
(
  cd "$repo_root/frontend"
  CI=true \
  DORA_E2E_EXTERNAL_SERVER=1 \
  DORA_E2E_BASE_URL="http://127.0.0.1:${vite_port}" \
  DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:${business_http_port}" \
  DORA_E2E_USER_MESSAGE_RUNTIME=1 \
  DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
  DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
  DORA_E2E_USER_MESSAGE_PROMPT="$browser_prompt" \
  DORA_E2E_USER_MESSAGE_RESULT_PATH="$browser_result" \
  DORA_E2E_OUTPUT_DIR="$work_dir/playwright-output" \
  ./node_modules/.bin/playwright test --config=playwright.config.js \
    e2e/user-message-runtime.spec.js --grep '@user-message-runtime'
) >"$work_dir/playwright.log" 2>&1 &
playwright_pid="$!"
if wait "$playwright_pid"; then
  playwright_pid=""
else
  playwright_status="$?"
  playwright_pid=""
  sed -n '1,320p' "$work_dir/playwright.log" >&2 || true
  fail "real Chromium vertical slice failed with status $playwright_status"
fi

[[ -s "$browser_result" ]] || fail "browser result was not produced"
[[ "$(file_mode "$browser_result")" == "600" ]] || fail "browser result mode is not 0600"
jq -e '
  keys == ["assertions","input_id","produced_at","project_id","run_id","schema_version","session_id","status","terminal_delivery","turn_id"]
  and .schema_version == "user_message_runtime.browser_result.v1"
  and .status == "passed"
  and (.produced_at | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{1,9})?Z$"))
  and (.terminal_delivery == "sse" or .terminal_delivery == "snapshot")
  and all(.project_id,.session_id,.input_id,.turn_id,.run_id;
    test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
  and (.assertions | keys) == ["chromium_browser","direct_response_card_rendered","no_assistant_message",
    "open_toolbox_focus_only","quick_create_input_preserved","reload_same_turn_run_input",
    "same_origin_business_bff","user_messages_only","workspace_snapshot_v2","workspace_sse_live"]
  and all(.assertions[]; . == true)
' "$browser_result" >/dev/null || fail "browser result contract is invalid or contains a failed assertion"

project_id="$(jq -er '.project_id' "$browser_result")"
session_id="$(jq -er '.session_id' "$browser_result")"
input_id="$(jq -er '.input_id' "$browser_result")"
turn_id="$(jq -er '.turn_id' "$browser_result")"
runtime_run_id="$(jq -er '.run_id' "$browser_result")"
collect_postgresql_authority "$project_id" "$session_id" "$input_id" "$turn_id" "$runtime_run_id" "$authority_result" || \
  fail "PostgreSQL authority query failed"
[[ "$(file_mode "$authority_result")" == "600" ]] || fail "PostgreSQL authority result mode is not 0600"
jq -e '
  keys == ["context_count","context_digest","creation_spec_preview_run_count","event_high_watermark",
    "input_count","model_execution_fence_consistent","model_receipt_count","model_route_ref","non_user_message_count","output_receipt_count",
    "projection_count","result_digest","run_count","session_count","terminal_event_count","tool_registry_ref",
    "turn_count","user_message_count"]
  and .session_count == 1 and .user_message_count == 1 and .non_user_message_count == 0
  and .input_count == 1 and .turn_count == 1 and .context_count == 1 and .run_count == 1
  and .model_receipt_count == 1 and .output_receipt_count == 1 and .projection_count == 1
  and .model_execution_fence_consistent == true
  and .terminal_event_count == 1 and .event_high_watermark == 3
  and .creation_spec_preview_run_count == 0
  and .tool_registry_ref == "user_message.empty_tools@v1"
  and .model_route_ref == "local.fake.user_message@v1"
  and (.context_digest | test("^[0-9a-f]{64}$"))
  and (.result_digest | test("^[0-9a-f]{64}$"))
' "$authority_result" >/dev/null || fail "PostgreSQL authority facts are incomplete or inconsistent"

postgres_version_num="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent_test -qAtc 'SHOW server_version_num')" || \
  fail "PostgreSQL version query failed"
[[ "$postgres_version_num" =~ ^[0-9]+$ && "$postgres_version_num" -ge 160000 ]] || fail "PostgreSQL 16+ was not observed"

write_source_manifest "$work_dir/source-after.manifest" || fail "could not rebuild source manifest"
source_digest_after="$(sha256_file "$work_dir/source-after.manifest")" || fail "could not hash final source manifest"
[[ "$source_digest_before" == "$source_digest_after" ]] || fail "source tree changed during canonical Trial"

: >"$etcd_monitor_stop"
chmod 600 "$etcd_monitor_stop"
if wait "$etcd_monitor_pid"; then
  etcd_monitor_pid=""
else
  etcd_monitor_status="$?"
  etcd_monitor_pid=""
  fail "etcd continuous provenance monitor failed with status $etcd_monitor_status"
fi
[[ ! -s "$etcd_monitor_violation" ]] || fail "etcd continuous provenance monitor observed prefix drift"

# 正常路径在发布 passed Evidence 前主动关闭全部宿主机进程；EXIT trap 仍作为失败兜底。
stop_pid_best_effort "$vite_pid"
vite_pid=""
stop_pid_best_effort "$agent_pid"
agent_pid=""
stop_pid_best_effort "$business_pid"
business_pid=""
wait_etcd_prefix_empty "/dora/services/dora.business.foundation.v1/" "Business Foundation shutdown"
wait_etcd_prefix_empty "/dora/services/dora.agent.session.v1/" "Agent Session shutdown"
for port in "$business_http_port" "$agent_http_port" "$business_rpc_port" "$agent_rpc_port" "$vite_port"; do
  if nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
    fail "local Runtime cleanup left port $port open"
  fi
done

jq -n -S \
  --arg run_id "$run_id" --arg source_digest "$source_digest_before" \
  --arg project_id "$project_id" --arg session_id "$session_id" --arg input_id "$input_id" \
  --arg turn_id "$turn_id" --arg runtime_run_id "$runtime_run_id" \
  --arg legacy_session_id "$legacy_session_id" --arg legacy_input_id "$legacy_input_id" \
  --arg legacy_message_id "$legacy_message_id" --arg legacy_command_id "$legacy_command_id" \
  --arg legacy_turn_id "$legacy_turn_id" --arg legacy_runtime_run_id "$legacy_runtime_run_id" \
  --arg browser_delivery "$(jq -er '.terminal_delivery' "$browser_result")" \
  --arg business_binary_sha256 "$business_binary_sha256" --arg agent_binary_sha256 "$agent_binary_sha256" \
  --arg context_digest "$(jq -er '.context_digest' "$authority_result")" \
  --arg result_digest "$(jq -er '.result_digest' "$authority_result")" \
  --argjson postgres_version_num "$postgres_version_num" \
  --argjson postgres_port "$postgres_port" --argjson redis_port "$redis_port" --argjson etcd_port "$etcd_port" \
  --slurpfile browser "$browser_result" --slurpfile authority "$authority_result" \
  --slurpfile legacy_authority "$legacy_authority_result" '
  {
    schema_version:"user_message_runtime.trial_evidence.v1",
    status:"pending_redaction",
    run_id:$run_id,
    produced_at:(now | todateiso8601),
    profile:"user_message.runtime.v2preview1",
    source_digest:("sha256:" + $source_digest),
    runtime_flags:{creation_spec_preview:false,user_message_runtime:true},
    infrastructure:{
      postgresql:{host:"127.0.0.1",port:$postgres_port,database:"dora_agent_test",schema_reset:true,
        server_version_num:$postgres_version_num},
      redis:{host:"127.0.0.1",port:$redis_port,tcp_ready:true},
      etcd:{host:"127.0.0.1",port:$etcd_port,health:true}
    },
    runtime_binaries:{business_sha256:("sha256:" + $business_binary_sha256),agent_sha256:("sha256:" + $agent_binary_sha256)},
    identity:{project_id:$project_id,session_id:$session_id,input_id:$input_id,turn_id:$turn_id,run_id:$runtime_run_id},
    digests:{context_sha256:("sha256:" + $context_digest),result_sha256:("sha256:" + $result_digest)},
    terminal_delivery:$browser_delivery,
    counts:{
      sessions:$authority[0].session_count,user_messages:$authority[0].user_message_count,inputs:$authority[0].input_count,
      turns:$authority[0].turn_count,contexts:$authority[0].context_count,runs:$authority[0].run_count,
      model_receipts:$authority[0].model_receipt_count,output_receipts:$authority[0].output_receipt_count,
      projections:$authority[0].projection_count,terminal_events:$authority[0].terminal_event_count,
      creation_spec_preview_runs:$authority[0].creation_spec_preview_run_count
    },
    legacy:{
      identity:{session_id:$legacy_session_id,input_id:$legacy_input_id,message_id:$legacy_message_id,
        command_id:$legacy_command_id,turn_id:$legacy_turn_id,run_id:$legacy_runtime_run_id},
      upgrade_ledger:{stage:$legacy_authority[0].ledger_stage,generation:$legacy_authority[0].upgrade_generation,
        version:$legacy_authority[0].ledger_version},
      counts:{sessions:$legacy_authority[0].session_count,messages:$legacy_authority[0].message_count,
        inputs:$legacy_authority[0].input_count,command_receipts:$legacy_authority[0].command_receipt_count,
        ledger_rows:$legacy_authority[0].ledger_count,turns:$legacy_authority[0].turn_count,
        contexts:$legacy_authority[0].context_count,runs:$legacy_authority[0].run_count,
        model_receipts:$legacy_authority[0].model_receipt_count,
        output_receipts:$legacy_authority[0].output_receipt_count,
        projections:$legacy_authority[0].projection_count,terminal_events:$legacy_authority[0].terminal_event_count}
    },
    assertions:($browser[0].assertions + {
      local_only_activation:true,postgresql_16_plus:true,redis_exposed_port_reachable:true,etcd_health:true,
      dedicated_agent_test_database:true,dedicated_agent_test_schema_reset:true,
      etcd_registration_provenance:true,etcd_lease_removed:true,
      etcd_continuous_provenance:true,
      creation_spec_preview_disabled:true,user_message_runtime_enabled:true,empty_tool_registry:true,
      local_fake_model_route:true,postgresql_authority_unique:true,no_creation_spec_preview_run:true,
      model_execution_fence_consistent:true,
      legacy_ids_preserved:true,legacy_ledger_verified:true,legacy_runtime_authority_unique:true,
      source_zero_delta:true,runtime_cleanup:true,evidence_redacted:false
    })
  }
' >"$evidence_pending"
chmod 600 "$evidence_pending"

jq -e '
  .schema_version == "user_message_runtime.trial_evidence.v1"
  and .status == "pending_redaction"
  and .runtime_flags == {creation_spec_preview:false,user_message_runtime:true}
  and .counts == {sessions:1,user_messages:1,inputs:1,turns:1,contexts:1,runs:1,model_receipts:1,
    output_receipts:1,projections:1,terminal_events:1,creation_spec_preview_runs:0}
  and .infrastructure.postgresql.database == "dora_agent_test"
  and .infrastructure.postgresql.schema_reset == true
  and .legacy.upgrade_ledger == {stage:"verified",generation:1,version:3}
  and .legacy.counts == {sessions:1,messages:1,inputs:1,command_receipts:1,ledger_rows:1,turns:1,
    contexts:1,runs:1,model_receipts:1,output_receipts:1,projections:1,terminal_events:1}
  and all(.legacy.identity[];
    test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
  and .assertions.evidence_redacted == false
  and all(.assertions | to_entries[] | select(.key != "evidence_redacted"); .value == true)
' "$evidence_pending" >/dev/null || fail "pending Trial Evidence contains a failed assertion"
assert_evidence_redacted

jq -S '.status = "passed" | .assertions.evidence_redacted = true' "$evidence_pending" >"${evidence_file}.tmp"
chmod 600 "${evidence_file}.tmp"
mv "${evidence_file}.tmp" "$evidence_file"
chmod 600 "$evidence_file"
rm -f "$evidence_pending"
[[ "$(file_mode "$evidence_file")" == "600" ]] || fail "Trial Evidence mode is not 0600"
jq -e '.status == "passed" and all(.assertions[]; . == true)' "$evidence_file" >/dev/null || \
  fail "published Trial Evidence is invalid"
evidence_pending=""

printf 'user-message-runtime real PostgreSQL/Redis/etcd/Business/Agent/Vite/Chromium smoke passed: %s\n' "$evidence_file"
