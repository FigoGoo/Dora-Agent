#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/smoke-secret-transport.sh
. "$repo_root/scripts/lib/smoke-secret-transport.sh"
disable_shell_xtrace
umask 077

env_file="${ENV_FILE:-$repo_root/.env.example}"
default_go_bin="/Users/figo/sdk/go1.26.3/bin/go"
go_bin="${GO_BIN:-$default_go_bin}"
migrate_bin="${MIGRATE_BIN:-$repo_root/.local/tools/migrate}"
evidence_file="${TRIAL_BASIC_EVIDENCE_FILE:-$repo_root/.local/smoke/trial-basic.json}"
postgres_host="${DORA_TRIAL_BASIC_POSTGRES_HOST:-127.0.0.1}"
postgres_port="${DORA_TRIAL_BASIC_POSTGRES_PORT:-15432}"
redis_host="${DORA_TRIAL_BASIC_REDIS_HOST:-127.0.0.1}"
redis_port="${DORA_TRIAL_BASIC_REDIS_PORT:-16379}"
etcd_host="${DORA_TRIAL_BASIC_ETCD_HOST:-127.0.0.1}"
etcd_port="${DORA_TRIAL_BASIC_ETCD_PORT:-12379}"
business_http_port="${DORA_TRIAL_BASIC_BUSINESS_HTTP_PORT:-38301}"
agent_http_port="${DORA_TRIAL_BASIC_AGENT_HTTP_PORT:-38302}"
worker_http_port="${DORA_TRIAL_BASIC_WORKER_HTTP_PORT:-38303}"
business_rpc_port="${DORA_TRIAL_BASIC_BUSINESS_RPC_PORT:-39301}"
agent_rpc_port="${DORA_TRIAL_BASIC_AGENT_RPC_PORT:-39302}"
vite_port="${DORA_TRIAL_BASIC_VITE_PORT:-33320}"
run_label="$(date -u +%Y%m%dT%H%M%SZ)-$$"
work_dir=""
business_pid=""
agent_pid=""
worker_pid=""
vite_pid=""
playwright_pid=""
evidence_published=false

fail() {
  printf 'trial-basic failed: %s\n' "$1" >&2
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
    for _ in $(seq 1 160); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.25
    done
    kill -KILL "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
}

dump_failure_logs() {
  [[ -n "$work_dir" && -d "$work_dir" ]] || return 0
  local log_file=""
  for log_file in Business.log Agent.log Worker.log Vite.log Playwright.log Seed.log; do
    [[ -s "$work_dir/$log_file" ]] || continue
    printf '\n[%s]\n' "$log_file" >&2
    tail -n 240 "$work_dir/$log_file" >&2 || true
  done
}

cleanup() {
  local status="$?"
  trap - EXIT INT TERM
  if [[ "$status" -ne 0 ]]; then
    dump_failure_logs
  fi
  stop_pid_best_effort "$playwright_pid"
  stop_pid_best_effort "$vite_pid"
  stop_pid_best_effort "$worker_pid"
  stop_pid_best_effort "$agent_pid"
  stop_pid_best_effort "$business_pid"
  [[ -z "$work_dir" ]] || rm -rf "$work_dir"
  rm -f "${evidence_file}.pending" "${evidence_file}.tmp"
  if [[ "$status" -ne 0 || "$evidence_published" != true ]]; then
    rm -f "$evidence_file"
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

wait_tcp() {
  local host="$1" port="$2" label="$3"
  for _ in $(seq 1 120); do
    nc -z "$host" "$port" >/dev/null 2>&1 && return 0
    sleep 0.25
  done
  fail "$label is not reachable at ${host}:${port}"
}

assert_port_available() {
  local port="$1" label="$2"
  nc -z 127.0.0.1 "$port" >/dev/null 2>&1 && fail "$label port $port is already in use"
  return 0
}

wait_port_closed() {
  local port="$1" label="$2"
  for _ in $(seq 1 120); do
    ! nc -z 127.0.0.1 "$port" >/dev/null 2>&1 && return 0
    sleep 0.25
  done
  fail "$label port $port remained open after shutdown"
}

wait_http_ready() {
  local port="$1" pid="$2" label="$3" log_file="$4"
  for _ in $(seq 1 240); do
    if ! kill -0 "$pid" 2>/dev/null; then
      tail -n 240 "$log_file" >&2 || true
      fail "$label exited before readiness"
    fi
    curl --fail --silent --max-time 1 "http://127.0.0.1:${port}/readyz" >/dev/null && return 0
    sleep 0.25
  done
  tail -n 240 "$log_file" >&2 || true
  fail "$label readiness timed out"
}

wait_vite_ready() {
  for _ in $(seq 1 240); do
    if ! kill -0 "$vite_pid" 2>/dev/null; then
      tail -n 240 "$work_dir/Vite.log" >&2 || true
      fail 'Vite exited before readiness'
    fi
    curl --fail --silent --max-time 1 "http://127.0.0.1:${vite_port}/" >/dev/null && return 0
    sleep 0.25
  done
  fail 'Vite readiness timed out'
}

stop_pid_strict() {
	local pid="$1" label="$2" allow_sigterm_exit="${3:-false}" stopped=false state="" wait_status=0
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
  [[ "$stopped" == true ]] || fail "$label did not stop within deadline"
	set +e
	wait "$pid"
	wait_status="$?"
	set -e
	if [[ "$wait_status" -eq 0 ]]; then
		return 0
	fi
	[[ "$allow_sigterm_exit" == true && "$wait_status" -eq 143 ]] || \
		fail "$label returned a failure during graceful shutdown"
}

validate_test_database_url() {
  local dsn="$1" role="$2" database="$3"
  local prefix="postgres://${role}:" suffix="@${postgres_host}:${postgres_port}/${database}?sslmode=disable" password=""
  [[ "$dsn" == "$prefix"*"$suffix" ]] || \
    fail "refusing to use a PostgreSQL database other than $database on the canonical loopback port"
  password="${dsn#"$prefix"}"
  password="${password%"$suffix"}"
  [[ -n "$password" && "$password" != *['@/:?#']* ]] || fail 'PostgreSQL test DSN credentials are invalid'
}

reset_test_database() {
  local module="$1" dsn="$2" reset_dir="$work_dir/reset-$1"
  local reset_table="dora_${module}_trial_basic_reset"
  local reset_dsn="${dsn}&x-migrations-table=${reset_table}"
  mkdir -m 700 "$reset_dir"
  printf 'DROP SCHEMA IF EXISTS %s CASCADE;\nCREATE SCHEMA %s;\nDROP TABLE IF EXISTS public.schema_migrations;\n' \
    "$module" "$module" >"$reset_dir/000001_reset_contract_schema.up.sql"
  printf 'SELECT 1;\n' >"$reset_dir/000001_reset_contract_schema.down.sql"
  chmod 600 "$reset_dir"/*.sql
  "$migrate_bin" -path "$reset_dir" -database "$reset_dsn" up >/dev/null
  "$migrate_bin" -path "$reset_dir" -database "$reset_dsn" drop -f >/dev/null
  rm -rf "$reset_dir"
}

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

write_source_manifest() {
  local output="$1" source_file=""
  : >"$output"
  while IFS= read -r source_file; do
    [[ -f "$repo_root/$source_file" ]] || continue
    printf '%s  %s\n' "$(sha256_file "$repo_root/$source_file")" "$source_file" >>"$output"
  done < <(
    cd "$repo_root"
    git ls-files --cached --others --exclude-standard | LC_ALL=C sort -u
  )
  [[ -s "$output" ]]
}

etcd_prefix_count() {
  local prefix="$1" range_end="${1%?}0" key64="" end64=""
  key64="$(printf '%s' "$prefix" | base64 | tr -d '\r\n')"
  end64="$(printf '%s' "$range_end" | base64 | tr -d '\r\n')"
  curl --fail --silent --show-error --max-time 2 -H 'Content-Type: application/json' \
    --data-binary "{\"key\":\"${key64}\",\"range_end\":\"${end64}\",\"count_only\":true}" \
    "http://${etcd_host}:${etcd_port}/v3/kv/range" | jq -er '(.count // 0) | tonumber'
}

wait_etcd_prefix_count() {
  local prefix="$1" expected="$2" label="$3" actual=""
  for _ in $(seq 1 160); do
    actual="$(etcd_prefix_count "$prefix")" || actual=""
    [[ "$actual" == "$expected" ]] && return 0
    sleep 0.25
  done
  fail "$label etcd registration count did not become $expected"
}

assert_evidence_redacted() {
  local pending="$1" secret=""
  for secret in "$DORA_SMOKE_USER_PASSWORD" "$BUSINESS_AUTH_CSRF_SECRET_BASE64" \
    "$BUSINESS_PROJECT_PROMPT_KEY_BASE64" "$AGENT_CONTENT_KEY_BASE64"; do
    [[ -n "$secret" ]] || continue
    ! rg_with_pattern_stdin literal "$secret" "$pending" || fail 'Trial Evidence contains secret material'
  done
  if rg -ni '(authorization|cookie|csrf|password|secret|ciphertext|nonce|database_url|postgres://|object_root|object_key)' "$pending" >/dev/null; then
    fail 'Trial Evidence contains a forbidden field'
  fi
}

[[ -r "$env_file" ]] || fail "environment file is not readable: $env_file"
set -a
# shellcheck disable=SC1090
. "$env_file"
set +a

if [[ "$go_bin" != */* ]]; then
  resolved_go_bin="$(command -v "$go_bin" 2>/dev/null || true)"
  if [[ -n "$resolved_go_bin" ]]; then
    go_bin="$resolved_go_bin"
  else
    go_bin="$default_go_bin"
  fi
fi
if [[ "$migrate_bin" != */* ]]; then
  migrate_bin="$(command -v "$migrate_bin" 2>/dev/null || true)"
fi
for command_name in jq curl nc shasum git realpath; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ -x "$go_bin" ]] || fail 'Go toolchain is required'
[[ -x "$migrate_bin" ]] || fail 'golang-migrate is required'
[[ -x "$repo_root/frontend/node_modules/.bin/vite" ]] || fail 'Vite dependencies are not installed'
[[ -x "$repo_root/frontend/node_modules/.bin/playwright" ]] || fail 'Playwright dependencies are not installed'
[[ -r "$repo_root/frontend/e2e/trial-basic.spec.js" ]] || fail 'trial-basic Chromium spec is missing'

ffmpeg_path="$(realpath "${DORA_WORKER_FFMPEG_PATH:-$(command -v ffmpeg || true)}" 2>/dev/null || true)"
ffprobe_path="$(realpath "${DORA_WORKER_FFPROBE_PATH:-$(command -v ffprobe || true)}" 2>/dev/null || true)"
[[ -x "$ffmpeg_path" && ! -L "$ffmpeg_path" ]] || fail 'a real non-symlink ffmpeg executable is required'
[[ -x "$ffprobe_path" && ! -L "$ffprobe_path" ]] || fail 'a real non-symlink ffprobe executable is required'

BUSINESS_DATABASE_URL="${BUSINESS_CONTRACT_DATABASE_URL:-}"
AGENT_DATABASE_URL="${AGENT_CONTRACT_DATABASE_URL:-}"
WORKER_DATABASE_URL="${WORKER_CONTRACT_DATABASE_URL:-}"
export BUSINESS_DATABASE_URL AGENT_DATABASE_URL WORKER_DATABASE_URL
validate_test_database_url "$BUSINESS_DATABASE_URL" dora_business_app dora_business_test
validate_test_database_url "$AGENT_DATABASE_URL" dora_agent_app dora_agent_test
validate_test_database_url "$WORKER_DATABASE_URL" dora_worker_app dora_worker_test

for host_port_label in "$postgres_host:$postgres_port:PostgreSQL" "$redis_host:$redis_port:Redis" "$etcd_host:$etcd_port:etcd"; do
  IFS=: read -r host port label <<<"$host_port_label"
  wait_tcp "$host" "$port" "$label"
done
for port_label in "$business_http_port:Business-HTTP" "$agent_http_port:Agent-HTTP" \
  "$worker_http_port:Worker-HTTP" "$business_rpc_port:Business-RPC" "$agent_rpc_port:Agent-RPC" "$vite_port:Vite"; do
  assert_port_available "${port_label%%:*}" "${port_label#*:}"
done
[[ "$(etcd_prefix_count '/dora/services/dora-business-service/')" == 0 ]] || fail 'Business etcd registration prefix is not empty'
[[ "$(etcd_prefix_count '/dora/services/dora-agent-service/')" == 0 ]] || fail 'Agent etcd registration prefix is not empty'

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/dora-trial-basic.XXXXXX")"
chmod 700 "$work_dir"
mkdir -m 700 "$work_dir/go-cache"
export GOCACHE="$work_dir/go-cache"
object_root="$work_dir/media-objects"
mkdir -m 700 "$object_root"
mkdir -p "$repo_root/.local/bin" "$(dirname "$evidence_file")"
rm -f "$evidence_file" "${evidence_file}.pending" "${evidence_file}.tmp"
write_source_manifest "$work_dir/source-before.sha256" || fail 'could not freeze source manifest'

export DORA_ENV=local
export BUSINESS_INSTANCE_ID="business-trial-basic-$run_label"
export AGENT_INSTANCE_ID="agent-trial-basic-$run_label"
export WORKER_INSTANCE_ID="worker-trial-basic-$run_label"
export BUSINESS_HTTP_ADDR="127.0.0.1:$business_http_port"
export BUSINESS_ADVERTISED_ADDRESS="$BUSINESS_HTTP_ADDR"
export BUSINESS_RPC_LISTEN_ADDR="127.0.0.1:$business_rpc_port"
export BUSINESS_RPC_ADVERTISED_ADDRESS="$BUSINESS_RPC_LISTEN_ADDR"
export BUSINESS_AGENT_HTTP_BASE_URL="http://127.0.0.1:$agent_http_port"
export AGENT_HTTP_ADDR="127.0.0.1:$agent_http_port"
export AGENT_ADVERTISED_ADDRESS="$AGENT_HTTP_ADDR"
export AGENT_RPC_LISTEN_ADDR="127.0.0.1:$agent_rpc_port"
export AGENT_RPC_ADVERTISED_ADDRESS="$AGENT_RPC_LISTEN_ADDR"
export WORKER_HTTP_ADDR="127.0.0.1:$worker_http_port"
export BUSINESS_REDIS_ADDR="$redis_host:$redis_port"
export AGENT_REDIS_ADDR="$redis_host:$redis_port"
export WORKER_REDIS_ADDR="$redis_host:$redis_port"
export BUSINESS_ETCD_ENDPOINTS="$etcd_host:$etcd_port"
export AGENT_ETCD_ENDPOINTS="$etcd_host:$etcd_port"
export WORKER_ETCD_ENDPOINTS="$etcd_host:$etcd_port"

export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false
export DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false
export DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=false
export DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED=false
export DORA_AGENT_WRITE_PROMPTS_RUNTIME_ENABLED=false
export DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED=false
export DORA_BUSINESS_PLAN_STORYBOARD_RUNTIME_ENABLED=false
export DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_ENABLED=false
export DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED=false
export DORA_AGENT_RUNTIME_PROFILE=mvp_all_tools.runtime.v1preview1
export DORA_BUSINESS_RUNTIME_PROFILE=mvp_all_tools.runtime.v1preview1
export DORA_AGENT_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1
export DORA_BUSINESS_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1
export DORA_WORKER_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1
export DORA_AGENT_MEDIA_BUSINESS_BASE_URL="http://127.0.0.1:$business_http_port"
export DORA_BUSINESS_MEDIA_OBJECT_ROOT="$object_root"
export DORA_WORKER_MEDIA_OBJECT_ROOT="$object_root"
export DORA_WORKER_AGENT_CONSUMER_DSN="postgres://dora_worker_app:${DORA_WORKER_DB_PASSWORD}@${postgres_host}:${postgres_port}/dora_agent_test?sslmode=disable"
export DORA_WORKER_BUSINESS_BASE_URL="http://127.0.0.1:$business_http_port"
export DORA_WORKER_FFMPEG_PATH="$ffmpeg_path"
export DORA_WORKER_FFPROBE_PATH="$ffprobe_path"
export WORKER_CONCURRENCY=2
export WORKER_CLAIM_BATCH_SIZE=2
export WORKER_POLL_INTERVAL=250ms
export WORKER_LEASE_TTL=30s
export WORKER_HEARTBEAT_INTERVAL=5s
export WORKER_ATTEMPT_TIMEOUT=2m

GOWORK=off "$go_bin" -C "$repo_root/business" build -o "$repo_root/.local/bin/business-service" ./cmd/business-service
GOWORK=off "$go_bin" -C "$repo_root/agent" build -o "$repo_root/.local/bin/agent-service" ./cmd/agent-service
GOWORK=off "$go_bin" -C "$repo_root/worker" build -o "$repo_root/.local/bin/business-worker" ./cmd/business-worker

reset_test_database business "$BUSINESS_DATABASE_URL"
reset_test_database agent "$AGENT_DATABASE_URL"
reset_test_database worker "$WORKER_DATABASE_URL"
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" business up
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" agent up
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" worker up
(
  cd "$repo_root/business"
  GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-seeder
) >"$work_dir/Seed.json" 2>"$work_dir/Seed.log" || fail 'local smoke user seed failed'

"$repo_root/.local/bin/business-service" >"$work_dir/Business.log" 2>&1 &
business_pid="$!"
wait_http_ready "$business_http_port" "$business_pid" Business "$work_dir/Business.log"
"$repo_root/.local/bin/agent-service" >"$work_dir/Agent.log" 2>&1 &
agent_pid="$!"
wait_http_ready "$agent_http_port" "$agent_pid" Agent "$work_dir/Agent.log"
"$repo_root/.local/bin/business-worker" >"$work_dir/Worker.log" 2>&1 &
worker_pid="$!"
wait_http_ready "$worker_http_port" "$worker_pid" Worker "$work_dir/Worker.log"
wait_etcd_prefix_count '/dora/services/dora-business-service/' 1 Business
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent

(
  cd "$repo_root/frontend"
  exec env VITE_BUSINESS_API_TARGET="http://127.0.0.1:$business_http_port" \
    VITE_AGENT_API_TARGET="http://127.0.0.1:$agent_http_port" \
    VITE_DORA_RUNTIME_PROFILE=mvp_all_tools.runtime.v1preview1 \
    VITE_DORA_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1 \
    VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED=false \
    VITE_DORA_ANALYZE_MATERIALS_RUNTIME_ENABLED=false \
    VITE_DORA_PLAN_STORYBOARD_RUNTIME_ENABLED=false \
    VITE_DORA_WRITE_PROMPTS_RUNTIME_ENABLED=false \
    ./node_modules/.bin/vite --host 127.0.0.1 --port "$vite_port" --strictPort
) >"$work_dir/Vite.log" 2>&1 &
vite_pid="$!"
wait_vite_ready

browser_result="$work_dir/browser-result.json"
(
  cd "$repo_root/frontend"
  DORA_E2E_TRIAL_BASIC=1 \
  DORA_E2E_EXTERNAL_SERVER=1 \
  DORA_E2E_BASE_URL="http://127.0.0.1:$vite_port" \
  DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:$business_http_port" \
  DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
  DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
  DORA_E2E_TRIAL_BASIC_RESULT_PATH="$browser_result" \
  DORA_E2E_OUTPUT_DIR="$work_dir/playwright" \
    ./node_modules/.bin/playwright test e2e/trial-basic.spec.js --grep '@trial-basic'
) >"$work_dir/Playwright.log" 2>&1 &
playwright_pid="$!"
set +e
wait "$playwright_pid"
playwright_status="$?"
set -e
playwright_pid=""
[[ "$playwright_status" == 0 ]] || fail 'trial-basic Chromium chain failed'
[[ -s "$browser_result" && "$(file_mode "$browser_result")" == 600 ]] || fail 'browser result is missing or not 0600'
jq -e '
  .schema_version == "trial_basic.browser_result.v1" and .status == "passed"
  and (.tool_receipts | keys) == ["analyze_materials","assemble_output","generate_media","plan_creation_spec","plan_storyboard","write_prompts"]
  and (.asset_ids.png | test("^[0-9a-f-]{36}$")) and (.asset_ids.mp4 | test("^[0-9a-f-]{36}$"))
  and (.media_results | length) == 2
  and all(.media_results[];
    (.job_id | test("^[0-9a-f-]{36}$")) and (.content_digest | test("^[0-9a-f]{64}$"))
    and (.size_bytes > 0) and (.mime_type == "image/png" or .mime_type == "video/mp4"))
  and all(.assertions[]; . == true)
' "$browser_result" >/dev/null || fail 'browser result contract is invalid'

write_source_manifest "$work_dir/source-after.sha256" || fail 'could not rebuild source manifest'
cmp -s "$work_dir/source-before.sha256" "$work_dir/source-after.sha256" || fail 'source tree changed during trial-basic'
source_digest="$(sha256_file "$work_dir/source-before.sha256")"

stop_pid_strict "$worker_pid" Worker
worker_pid=""
stop_pid_strict "$agent_pid" Agent
agent_pid=""
stop_pid_strict "$business_pid" Business
business_pid=""
stop_pid_strict "$vite_pid" Vite true
vite_pid=""
wait_port_closed "$worker_http_port" Worker-HTTP
wait_port_closed "$agent_http_port" Agent-HTTP
wait_port_closed "$agent_rpc_port" Agent-RPC
wait_port_closed "$business_http_port" Business-HTTP
wait_port_closed "$business_rpc_port" Business-RPC
wait_port_closed "$vite_port" Vite
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 0 Agent
wait_etcd_prefix_count '/dora/services/dora-business-service/' 0 Business

jq -S -n --slurpfile browser "$browser_result" \
  --arg run_label "$run_label" --arg source_digest "sha256:$source_digest" \
  --argjson postgres_port "$postgres_port" --argjson redis_port "$redis_port" --argjson etcd_port "$etcd_port" '
  {
    schema_version:"trial_basic.evidence.v1",status:"pending",trial_run:$run_label,
    profiles:{base:"mvp_all_tools.runtime.v1preview1",media:"media.runtime.v3preview1"},
    source_digest:$source_digest,
    infrastructure:{postgresql:{host:"127.0.0.1",port:$postgres_port,database:"dedicated_test"},
      redis:{host:"127.0.0.1",port:$redis_port},etcd:{host:"127.0.0.1",port:$etcd_port},
      browser:"chromium",media_engine:"local_deterministic_preview"},
    result:$browser[0],
    assertions:{three_module_runtime:true,one_base_profile:true,six_graph_tools:true,worker_terminal:true,
      protected_png_and_mp4:true,range_200_206_416:true,workspace_v5_reload:true,
      graceful_runtime_cleanup:true,source_unchanged:true,evidence_redacted:false}
  }
' >"${evidence_file}.pending"
chmod 600 "${evidence_file}.pending"
jq -e '.status == "pending" and all(.result.assertions[]; . == true)
  and all(.assertions | to_entries[] | select(.key != "evidence_redacted"); .value == true)' \
  "${evidence_file}.pending" >/dev/null || fail 'pending Trial Evidence is invalid'
assert_evidence_redacted "${evidence_file}.pending"
jq -S '.status = "passed" | .assertions.evidence_redacted = true' \
  "${evidence_file}.pending" >"${evidence_file}.tmp"
chmod 600 "${evidence_file}.tmp"
mv "${evidence_file}.tmp" "$evidence_file"
chmod 600 "$evidence_file"
rm -f "${evidence_file}.pending"
[[ "$(file_mode "$evidence_file")" == 600 ]] || fail 'published Trial Evidence is not 0600'
jq -e '.status == "passed" and all(.assertions[]; . == true) and all(.result.assertions[]; . == true)' \
  "$evidence_file" >/dev/null || fail 'published Trial Evidence is invalid'
evidence_published=true

printf 'trial-basic passed: %s\n' "$evidence_file"
