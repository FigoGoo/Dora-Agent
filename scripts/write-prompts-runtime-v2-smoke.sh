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
business_http_port="${DORA_WRITE_PROMPTS_BUSINESS_HTTP_PORT:-38201}"
agent_http_port="${DORA_WRITE_PROMPTS_AGENT_HTTP_PORT:-38202}"
business_rpc_port="${DORA_WRITE_PROMPTS_BUSINESS_RPC_PORT:-39201}"
agent_rpc_port="${DORA_WRITE_PROMPTS_AGENT_RPC_PORT:-39202}"
vite_port="${DORA_WRITE_PROMPTS_VITE_PORT:-33310}"
evidence_file="${WRITE_PROMPTS_RUNTIME_EVIDENCE_FILE:-$repo_root/.local/smoke/write-prompts-runtime-v2.json}"
evidence_pending="${evidence_file}.pending"
run_label="$(date -u +%Y%m%dT%H%M%SZ)-$$"
work_dir=""
control_dir=""
business_pid=""
agent_pid=""
vite_pid=""
playwright_pid=""
evidence_published=false

fail() {
  printf 'write-prompts-runtime-v2 smoke failed: %s\n' "$1" >&2
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
  local host="$1" port="$2" label="$3"
  for _ in $(seq 1 80); do
    nc -z "$host" "$port" >/dev/null 2>&1 && return 0
    sleep 0.25
  done
  fail "$label is not reachable at ${host}:${port}"
}

discover_local_ipv4() {
  local address="" interface=""
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
  local port="$1" label="$2"
  nc -z 127.0.0.1 "$port" >/dev/null 2>&1 && fail "$label port $port is already in use"
  return 0
}

wait_port_closed() {
  local port="$1" label="$2"
  for _ in $(seq 1 80); do
    ! nc -z 127.0.0.1 "$port" >/dev/null 2>&1 && return 0
    sleep 0.25
  done
  fail "$label port $port remained open after shutdown"
}

wait_http_ready() {
  local port="$1" pid="$2" label="$3" log_file="$4"
  for _ in $(seq 1 160); do
    if ! kill -0 "$pid" 2>/dev/null; then
      sed -n '1,240p' "$log_file" >&2 || true
      fail "$label exited before readiness"
    fi
    curl --fail --silent --max-time 1 "http://127.0.0.1:${port}/readyz" >/dev/null && return 0
    sleep 0.25
  done
  sed -n '1,240p' "$log_file" >&2 || true
  fail "$label readiness timed out"
}

wait_vite_ready() {
  for _ in $(seq 1 160); do
    if ! kill -0 "$vite_pid" 2>/dev/null; then
      sed -n '1,200p' "$work_dir/Vite.log" >&2 || true
      fail 'Vite exited before readiness'
    fi
    curl --fail --silent --max-time 1 "http://127.0.0.1:${vite_port}/" >/dev/null && return 0
    sleep 0.25
  done
  fail 'Vite readiness timed out'
}

validate_test_database_url() {
  local dsn="$1" role="$2" database="$3"
  local prefix="postgres://${role}:" suffix="@${postgres_host}:${postgres_port}/${database}?sslmode=disable" password=""
  [[ "$dsn" == "$prefix"*"$suffix" ]] || \
    fail "refusing to use a PostgreSQL database other than $database on the canonical host port"
  password="${dsn#"$prefix"}"
  password="${password%"$suffix"}"
  [[ -n "$password" && "$password" != *['@/:?#']* ]] || fail 'PostgreSQL test DSN credentials are invalid'
}

reset_test_database() {
  local module="$1" dsn="$2"
  local reset_dir="$work_dir/reset-${module}"
  local reset_table="dora_${module}_write_prompts_smoke_reset"
  local reset_dsn="${dsn}&x-migrations-table=${reset_table}"
  mkdir -p "$reset_dir"
  chmod 700 "$reset_dir"
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
  local output="$1" source_file="" digest=""
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
  local range_end="${prefix%?}0" key64="" end64=""
  key64="$(printf '%s' "$prefix" | base64 | tr -d '\r\n')"
  end64="$(printf '%s' "$range_end" | base64 | tr -d '\r\n')"
  curl --fail --silent --show-error --max-time 2 -H 'Content-Type: application/json' \
    --data-binary "{\"key\":\"${key64}\",\"range_end\":\"${end64}\",\"count_only\":true}" \
    "http://${etcd_host}:${etcd_port}/v3/kv/range" | jq -er '(.count // 0) | tonumber'
}

wait_etcd_prefix_count() {
  local prefix="$1" expected="$2" label="$3" actual=""
  for _ in $(seq 1 120); do
    actual="$(etcd_prefix_count "$prefix")" || actual=""
    [[ "$actual" == "$expected" ]] && return 0
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
  local pid="$1" label="$2" stopped=false state=""
  [[ -n "$pid" ]] || fail "$label PID is missing before shutdown"
  kill -0 "$pid" 2>/dev/null || fail "$label exited before shutdown"
  kill -TERM "$pid" || fail "could not signal $label"
  for _ in $(seq 1 160); do
    state="$(ps -o stat= -p "$pid" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$pid" 2>/dev/null || [[ "$state" == Z* ]]; then stopped=true; break; fi
    sleep 0.25
  done
  [[ "$stopped" == 'true' ]] || fail "$label did not stop within deadline"
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

disable_all_preview_profiles() {
  export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false
  export DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false
  export DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=false
  export DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED=false
  export DORA_AGENT_WRITE_PROMPTS_RUNTIME_ENABLED=false
  export DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED=false
  export DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED=false
  export DORA_BUSINESS_PLAN_STORYBOARD_RUNTIME_ENABLED=false
  export DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_ENABLED=false
}

enable_creation_spec_profile() {
  disable_all_preview_profiles
  export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=true
}

enable_write_prompts_profile() {
  disable_all_preview_profiles
  export DORA_AGENT_WRITE_PROMPTS_RUNTIME_ENABLED=true
  export DORA_AGENT_WRITE_PROMPTS_RUNTIME_PROFILE=write_prompts.runtime.v2preview1
  export DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_ENABLED=true
  export DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_PROFILE=write_prompts.runtime.v2preview1
  export AGENT_SSE_MAX_CONNECTION_DURATION=20s
  export AGENT_SHUTDOWN_TIMEOUT=35s
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

configure_preview_loopback_addresses() {
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
  local project_id="$1" output="$2" status=""
  for _ in $(seq 1 160); do
    status="$(curl --silent --show-error --max-time 2 -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" \
      -o "$output" -w '%{http_code}' "http://127.0.0.1:${business_http_port}/api/v1/projects/${project_id}/bootstrap")"
    if [[ "$status" == 200 ]] && jq -e '.creation_status == "ready" and (.session_id | type == "string")' "$output" >/dev/null; then return 0; fi
    sleep 0.25
  done
  fail 'Project bootstrap did not become ready'
}

poll_creation_spec_draft() {
  local session_id="$1" output="$2" status=""
  for _ in $(seq 1 240); do
    status="$(curl --silent --show-error --max-time 2 -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" \
      -o "$output" -w '%{http_code}' "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/workspace")"
    if [[ "$status" == 200 ]] && jq -e '
      .schema_version == "session.workspace.v4"
      and .creation_spec_preview.status == "draft" and .creation_spec_preview.version == 1
      and (.creation_spec_preview.creation_spec_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and (.creation_spec_preview.content_digest | test("^[0-9a-f]{64}$"))
      and .plan_storyboard_preview == null and .write_prompts_preview == null
    ' "$output" >/dev/null; then return 0; fi
    sleep 0.25
  done
  fail 'CreationSpec Draft did not reach the Workspace Snapshot'
}

seed_storyboard_preview_fixture() {
  local user_id="$1" project_id="$2" session_id="$3" creation_spec_id="$4" creation_spec_digest="$5"
  local business_dir="$work_dir/storyboard-fixture-business" agent_dir="$work_dir/storyboard-fixture-agent"
  local fixture_time="" storyboard_content="" storyboard_card=""
  fixture_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  storyboard_preview_id='019f3000-0000-7000-8000-000000000103'
  storyboard_input_id='019f3000-0000-7000-8000-000000000101'
  storyboard_tool_call_id='019f3000-0000-7000-8000-000000000102'
  storyboard_turn_id='019f3000-0000-7000-8000-000000000105'
  storyboard_run_id='019f3000-0000-7000-8000-000000000106'
  storyboard_event_id='019f3000-0000-7000-8000-000000000104'
  storyboard_request_id='019f3000-0000-7000-8000-000000000107'
  storyboard_business_command_id='019f3000-0000-7000-8000-000000000108'
  storyboard_router_model_call_id='019f3000-0000-7000-8000-000000000109'
  storyboard_graph_model_call_id='019f3000-0000-7000-8000-000000000110'
  storyboard_content='{"title":"Write Prompts M4 Storyboard Source","summary":"隔离 Storyboard Preview fixture，仅用于验证全部媒体槽位的 Prompt Draft 生成。","sections":[{"key":"section_1","title":"开场与演示","objective":"用清晰画面建立主题并展示核心流程"}],"elements":[{"key":"element_1","section_key":"section_1","order":1,"element_type":"scene","title":"开场主视觉","narrative_purpose":"建立创作主题和视觉基调","duration_seconds":10,"source_phase_key":"phase_1","dependency_keys":[]},{"key":"element_2","section_key":"section_1","order":2,"element_type":"shot","title":"核心功能演示","narrative_purpose":"连续展示关键能力与反馈","duration_seconds":20,"source_phase_key":"phase_1","dependency_keys":["element_1"]}],"slots":[{"key":"slot_1","element_key":"element_1","slot_type":"image","purpose":"开场产品主视觉","required":true},{"key":"slot_2","element_key":"element_2","slot_type":"video","purpose":"核心功能演示画面","required":true}]}'
  storyboard_content_digest="$(printf '%s' "$storyboard_content" | shasum -a 256 | awk '{print $1}')"
  target_count=2
  storyboard_card="$(jq -cn --arg input "$storyboard_input_id" --arg turn "$storyboard_turn_id" \
    --arg run "$storyboard_run_id" --arg tool "$storyboard_tool_call_id" --arg updated "$fixture_time" \
    --arg storyboard "$storyboard_preview_id" --arg project "$project_id" --arg spec "$creation_spec_id" \
    --arg spec_digest "$creation_spec_digest" --arg digest "$storyboard_content_digest" --argjson content "$storyboard_content" '
    {schema_version:"storyboard.preview.card.v1",input_id:$input,turn_id:$turn,run_id:$run,tool_call_id:$tool,
     status:"completed",result_code:"STORYBOARD_PREVIEW_DRAFT_CREATED",updated_at:$updated,
     storyboard_preview_id:$storyboard,project_id:$project,
     creation_spec_ref:{id:$spec,version:1,content_digest:$spec_digest},version:1,content_digest:$digest,
     title:$content.title,summary:$content.summary,sections:$content.sections,elements:$content.elements,slots:$content.slots}
  ')"
  mkdir -m 700 "$business_dir" "$agent_dir"
  printf "INSERT INTO business.storyboard_preview_draft
    (id,project_id,user_id,creation_spec_id,creation_spec_version,creation_spec_content_digest,status,version,
     schema_version,content_json,content_digest,source_tool_call_id,source_prompt_version,source_validator_version,created_at,updated_at)
   VALUES ('%s','%s','%s','%s',1,decode('%s','hex'),'draft',1,'storyboard.preview.draft.v1','%s'::jsonb,
     decode('%s','hex'),'%s','fixture.storyboard.v1','fixture.validator.v1','%s'::timestamptz,'%s'::timestamptz);\n" \
    "$storyboard_preview_id" "$project_id" "$user_id" "$creation_spec_id" "$creation_spec_digest" \
    "$storyboard_content" "$storyboard_content_digest" "$storyboard_tool_call_id" "$fixture_time" "$fixture_time" \
    >"$business_dir/000001_seed_storyboard_preview_source.up.sql"
  printf 'SELECT 1;\n' >"$business_dir/000001_seed_storyboard_preview_source.down.sql"
  printf "WITH next_input AS (
     UPDATE agent.session_sequence_counter
     SET last_input_enqueue_seq = last_input_enqueue_seq + 1, updated_at = '%s'::timestamptz
     WHERE session_id = '%s'
     RETURNING last_input_enqueue_seq
   )
   INSERT INTO agent.session_input
    (id,session_id,source_type,source_id,message_id,status,enqueue_seq,attempts,available_at,lease_owner,lease_until,fence_token,created_at,updated_at)
   SELECT '%s','%s','plan_storyboard_preview','%s',NULL,'resolved',last_input_enqueue_seq,1,'%s'::timestamptz,NULL,NULL,1,'%s'::timestamptz,'%s'::timestamptz
   FROM next_input;\n" \
    "$fixture_time" "$session_id" "$storyboard_input_id" "$session_id" "$storyboard_input_id" \
    "$fixture_time" "$fixture_time" "$fixture_time" \
    >"$agent_dir/000001_seed_storyboard_preview_source.up.sql"
  printf "INSERT INTO agent.plan_storyboard_preview_turn_context
    (turn_id,profile,schema_version,request_id,session_id,input_id,run_id,tool_call_id,business_command_id,
     router_model_call_id,graph_model_call_id,user_id,project_id,intent_ciphertext,intent_key_version,intent_digest,
     creation_spec_id,creation_spec_version,creation_spec_content_digest,access_scope_ref,access_scope_digest,
     tool_registry_ref,tool_registry_digest,tool_definition_ref,tool_definition_digest,intent_schema_ref,
     candidate_schema_ref,result_schema_ref,prompt_ref,prompt_digest,validator_ref,validator_digest,
     dag_validator_ref,dag_validator_digest,router_model_route_ref,router_model_route_digest,
     planning_model_route_ref,planning_model_route_digest,runtime_policy_ref,runtime_policy_digest,
     budget_ref,budget_digest,context_digest,created_at)
   VALUES ('%s','plan_storyboard.runtime.v2preview1','plan_storyboard.turn_context.v2preview1','%s','%s','%s','%s','%s','%s',
     '%s','%s','%s','%s',decode('00','hex'),'fixture-key-v1','%s','%s',1,'%s',
     'fixture.access_scope.v1','%s','fixture.tool_registry.v1','%s','fixture.plan_storyboard.v1','%s',
     'fixture.plan_storyboard.intent.v1','fixture.plan_storyboard.candidate.v1','fixture.plan_storyboard.result.v1',
     'fixture.plan_storyboard.prompt.v1','%s','fixture.plan_storyboard.validator.v1','%s',
     'fixture.plan_storyboard.dag_validator.v1','%s','fixture.router.route.v1','%s',
     'fixture.planning.route.v1','%s','fixture.runtime_policy.v1','%s','fixture.budget.v1','%s','%s','%s'::timestamptz);\n" \
    "$storyboard_turn_id" "$storyboard_request_id" "$session_id" "$storyboard_input_id" "$storyboard_run_id" \
    "$storyboard_tool_call_id" "$storyboard_business_command_id" "$storyboard_router_model_call_id" \
    "$storyboard_graph_model_call_id" "$user_id" "$project_id" "$storyboard_content_digest" "$creation_spec_id" \
    "$creation_spec_digest" "$storyboard_content_digest" "$storyboard_content_digest" "$storyboard_content_digest" \
    "$storyboard_content_digest" "$storyboard_content_digest" "$storyboard_content_digest" "$storyboard_content_digest" \
    "$storyboard_content_digest" "$storyboard_content_digest" "$storyboard_content_digest" "$storyboard_content_digest" "$fixture_time" \
    >>"$agent_dir/000001_seed_storyboard_preview_source.up.sql"
  printf "WITH next_event AS (
     UPDATE agent.session_event_counter SET last_seq = last_seq + 1, updated_at = '%s'::timestamptz
     WHERE session_id = '%s' RETURNING last_seq
   )
   INSERT INTO agent.session_event_log
     (event_id,session_id,seq,event_type,schema_version,source_kind,source_id,projection_index,
      aggregate_type,aggregate_id,aggregate_version,payload,created_at)
   SELECT '%s','%s',last_seq,'plan_storyboard.preview.completed','session.event.v1','m4_storyboard_preview_fixture','%s',0,
     'plan_storyboard_preview','%s',1,'%s'::jsonb,'%s'::timestamptz FROM next_event;\n" \
    "$fixture_time" "$session_id" "$storyboard_event_id" "$session_id" "$storyboard_input_id" "$storyboard_input_id" \
    "$storyboard_card" "$fixture_time" >>"$agent_dir/000001_seed_storyboard_preview_source.up.sql"
  printf 'SELECT 1;\n' >"$agent_dir/000001_seed_storyboard_preview_source.down.sql"
  chmod 600 "$business_dir"/*.sql "$agent_dir"/*.sql
  "$migrate_bin" -path "$business_dir" -database "${BUSINESS_DATABASE_URL}&x-migrations-table=dora_business_storyboard_fixture" up >/dev/null
  "$migrate_bin" -path "$agent_dir" -database "${AGENT_DATABASE_URL}&x-migrations-table=dora_agent_storyboard_fixture" up >/dev/null
  "$migrate_bin" -path "$business_dir" -database "${BUSINESS_DATABASE_URL}&x-migrations-table=dora_business_storyboard_fixture" drop -f >/dev/null
  "$migrate_bin" -path "$agent_dir" -database "${AGENT_DATABASE_URL}&x-migrations-table=dora_agent_storyboard_fixture" drop -f >/dev/null
  rm -rf "$business_dir" "$agent_dir"
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
  local path="$1" jq_filter="$2" label="$3" state=""
  for _ in $(seq 1 2400); do
    if [[ -s "$path" ]] && jq -e "$jq_filter" "$path" >/dev/null 2>&1; then
      [[ "$(file_mode "$path")" == 600 ]] || fail "$label mode is not 0600"
      return 0
    fi
    state="$(ps -o stat= -p "$playwright_pid" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$playwright_pid" 2>/dev/null || [[ "$state" == Z* ]]; then
      sed -n '1,360p' "$work_dir/Playwright.log" >&2 || true
      fail "Playwright exited before $label"
    fi
    sleep 0.05
  done
  fail "timed out waiting for $label"
}

assert_runtime_database_contract() {
  local input_id="$1" tool_call_id="$2" prompt_preview_id="$3"
  local agent_dir="$work_dir/assert-agent" business_dir="$work_dir/assert-business"
  mkdir -m 700 "$agent_dir" "$business_dir"
  printf "DO \$\$ BEGIN
    IF (SELECT count(*) FROM agent.write_prompts_preview_run WHERE input_id = '%s' AND status = 'completed') <> 1 THEN RAISE EXCEPTION 'write prompts run count mismatch'; END IF;
    IF (SELECT count(*) FROM agent.write_prompts_preview_model_receipt WHERE input_id = '%s' AND call_kind = 'router' AND status = 'completed') <> 1 THEN RAISE EXCEPTION 'router receipt count mismatch'; END IF;
    IF (SELECT count(*) FROM agent.write_prompts_preview_model_receipt WHERE input_id = '%s' AND call_kind = 'graph_prompt' AND status = 'completed') <> 1 THEN RAISE EXCEPTION 'graph receipt count mismatch'; END IF;
    IF (SELECT count(*) FROM agent.write_prompts_preview_tool_receipt WHERE input_id = '%s' AND tool_call_id = '%s' AND status = 'completed') <> 1 THEN RAISE EXCEPTION 'tool receipt count mismatch'; END IF;
    IF (SELECT count(*) FROM agent.session_event_log WHERE aggregate_id = '%s' AND event_type IN ('write_prompts.preview.accepted','write_prompts.preview.completed')) <> 2 THEN RAISE EXCEPTION 'write prompts event count mismatch'; END IF;
    IF to_regclass('agent.approval') IS NOT NULL OR to_regclass('agent.operation') IS NOT NULL OR to_regclass('agent.batch') IS NOT NULL OR to_regclass('agent.job') IS NOT NULL THEN RAISE EXCEPTION 'forbidden production aggregate table exists'; END IF;
  END \$\$;\n" "$input_id" "$input_id" "$input_id" "$input_id" "$tool_call_id" "$input_id" \
    >"$agent_dir/000001_assert_write_prompts_runtime.up.sql"
  printf 'SELECT 1;\n' >"$agent_dir/000001_assert_write_prompts_runtime.down.sql"
  printf "DO \$\$ BEGIN
    IF (SELECT count(*) FROM business.prompt_preview_draft WHERE id = '%s' AND source_tool_call_id = '%s' AND status = 'draft') <> 1 THEN RAISE EXCEPTION 'prompt preview draft count mismatch'; END IF;
    IF (SELECT count(*) FROM business.prompt_preview_command_receipt WHERE prompt_preview_id = '%s' AND source_tool_call_id = '%s' AND result_status = 'draft') <> 1 THEN RAISE EXCEPTION 'prompt preview receipt count mismatch'; END IF;
    IF to_regclass('business.prompt_artifact') IS NOT NULL OR to_regclass('business.prompt_revision') IS NOT NULL THEN RAISE EXCEPTION 'forbidden production prompt table exists'; END IF;
  END \$\$;\n" "$prompt_preview_id" "$tool_call_id" "$prompt_preview_id" "$tool_call_id" \
    >"$business_dir/000001_assert_write_prompts_runtime.up.sql"
  printf 'SELECT 1;\n' >"$business_dir/000001_assert_write_prompts_runtime.down.sql"
  chmod 600 "$agent_dir"/*.sql "$business_dir"/*.sql
  "$migrate_bin" -path "$agent_dir" -database "${AGENT_DATABASE_URL}&x-migrations-table=dora_agent_write_prompts_assert" up >/dev/null
  "$migrate_bin" -path "$business_dir" -database "${BUSINESS_DATABASE_URL}&x-migrations-table=dora_business_write_prompts_assert" up >/dev/null
  "$migrate_bin" -path "$agent_dir" -database "${AGENT_DATABASE_URL}&x-migrations-table=dora_agent_write_prompts_assert" drop -f >/dev/null
  "$migrate_bin" -path "$business_dir" -database "${BUSINESS_DATABASE_URL}&x-migrations-table=dora_business_write_prompts_assert" drop -f >/dev/null
  rm -rf "$agent_dir" "$business_dir"
}

assert_evidence_redacted() {
  local value="" status=""
  for value in \
    "${DORA_SMOKE_USER_EMAIL:-}" "${DORA_SMOKE_USER_PASSWORD:-}" \
    "${BUSINESS_AUTH_CSRF_SECRET_BASE64:-}" "${BUSINESS_AGENT_SESSION_RPC_AUTH_SECRET_BASE64:-}" \
    "${BUSINESS_AGENT_HTTP_ASSERTION_SECRET_BASE64:-}" "${AGENT_HTTP_ASSERTION_ACTIVE_SECRET_BASE64:-}" \
    "${AGENT_SESSION_RPC_AUTH_SECRET_BASE64:-}" "${AGENT_CONTENT_KEY_BASE64:-}" \
    "${BUSINESS_DATABASE_URL:-}" "${AGENT_DATABASE_URL:-}"; do
    [[ -n "$value" ]] || continue
    if rg_with_pattern_stdin literal "$value" "$evidence_pending"; then
      fail 'Trial Evidence contains a protected runtime value'
    else
      status="$?"
      [[ "$status" == 1 ]] || fail 'Trial Evidence redaction scan failed'
    fi
  done
  if rg -ni '(authorization|cookie|csrf|password|secret|private_key|access_token|refresh_token|writing_instruction|positive_prompt|negative_constraints|ciphertext|nonce|database_url|postgres://)' "$evidence_pending" >/dev/null; then
    fail 'Trial Evidence contains a forbidden sensitive field'
  fi
}

[[ -r "$env_file" ]] || fail 'environment file is missing'
set -a
# shellcheck disable=SC1090
. "$env_file"
set +a

[[ "${DORA_ENV:-}" == local ]] || fail 'DORA_ENV must be local'
[[ "${AGENT_SSE_MAX_EVENT_BYTES:-}" =~ ^[0-9]+$ ]] || fail 'AGENT_SSE_MAX_EVENT_BYTES must be an integer'
(( AGENT_SSE_MAX_EVENT_BYTES >= 131072 )) || fail 'AGENT_SSE_MAX_EVENT_BYTES must be at least 131072 for Prompt Card envelopes'
[[ "$postgres_host" == 127.0.0.1 && "$postgres_port" == 15432 ]] || fail 'PostgreSQL must use 127.0.0.1:15432'
[[ "$redis_host" == 127.0.0.1 && "$redis_port" == 16379 ]] || fail 'Redis must use 127.0.0.1:16379'
[[ "$etcd_host" == 127.0.0.1 && "$etcd_port" == 12379 ]] || fail 'etcd must use 127.0.0.1:12379'
for command in curl jq nc rg shasum; do command -v "$command" >/dev/null 2>&1 || fail "$command is required"; done
[[ -x "$go_bin" ]] || fail 'Go SDK is required'
[[ -x "$migrate_bin" ]] || fail 'golang-migrate is required'
[[ -x "$repo_root/frontend/node_modules/.bin/vite" ]] || fail 'Vite dependencies are not installed'
[[ -x "$repo_root/frontend/node_modules/.bin/playwright" ]] || fail 'Playwright dependencies are not installed'
[[ -r "$repo_root/frontend/e2e/write-prompts-runtime.spec.js" ]] || fail 'canonical Chromium spec is missing'

BUSINESS_DATABASE_URL="${BUSINESS_CONTRACT_DATABASE_URL:-}"
AGENT_DATABASE_URL="${AGENT_CONTRACT_DATABASE_URL:-}"
export BUSINESS_DATABASE_URL AGENT_DATABASE_URL
validate_test_database_url "$BUSINESS_DATABASE_URL" dora_business_app dora_business_test
validate_test_database_url "$AGENT_DATABASE_URL" dora_agent_app dora_agent_test
export BUSINESS_REDIS_ADDR="${redis_host}:${redis_port}"
export AGENT_REDIS_ADDR="${redis_host}:${redis_port}"
export BUSINESS_ETCD_ENDPOINTS="${etcd_host}:${etcd_port}"
export AGENT_ETCD_ENDPOINTS="${etcd_host}:${etcd_port}"
export BUSINESS_INSTANCE_ID="business-write-prompts-${run_label}"
export AGENT_INSTANCE_ID="agent-write-prompts-${run_label}"

wait_tcp "$postgres_host" "$postgres_port" PostgreSQL
wait_tcp "$redis_host" "$redis_port" Redis
wait_tcp "$etcd_host" "$etcd_port" etcd
local_ipv4="$(discover_local_ipv4)" || fail 'a non-loopback local IPv4 is required for the CreationSpec source phase'
for port_label in "$business_http_port:Business-HTTP" "$agent_http_port:Agent-HTTP" \
  "$business_rpc_port:Business-RPC" "$agent_rpc_port:Agent-RPC" "$vite_port:Vite"; do
  assert_port_available "${port_label%%:*}" "${port_label#*:}"
done

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/dora-write-prompts-runtime.XXXXXX")"
chmod 700 "$work_dir"
control_dir="$work_dir/control"
mkdir -m 700 "$control_dir"
mkdir -p "$repo_root/.local/bin" "$(dirname "$evidence_file")"
rm -f "$evidence_file" "$evidence_pending" "${evidence_file}.tmp"
write_source_manifest "$work_dir/source-before.sha256" || fail 'could not freeze the source manifest'

GOWORK=off "$go_bin" -C "$repo_root/business" build -o "$repo_root/.local/bin/business-service" ./cmd/business-service
GOWORK=off "$go_bin" -C "$repo_root/agent" build -o "$repo_root/.local/bin/agent-service" ./cmd/agent-service
reset_test_database business "$BUSINESS_DATABASE_URL"
reset_test_database agent "$AGENT_DATABASE_URL"
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" business up
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" agent up
(
  cd "$repo_root/business"
  GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-seeder
) >"$work_dir/user-seed.json" 2>"$work_dir/user-seed.log" || fail 'local smoke user seed failed'

# 首先用目标 write_prompts Profile 创建空 Lane；没有 Storyboard Source 时不会执行 Tool。
enable_write_prompts_profile
configure_preview_loopback_addresses
start_business lane-bootstrap
start_agent lane-bootstrap
wait_etcd_prefix_count '/dora/services/dora-business-service/' 1 Business
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent

login_payload="$(build_login_json "$DORA_SMOKE_USER_EMAIL" "$DORA_SMOKE_USER_PASSWORD")"
login_status="$(curl_with_body_stdin "$login_payload" --silent --show-error --max-time 10 \
  -c "$work_dir/cookies.txt" -H 'Content-Type: application/json' -o "$work_dir/login.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/auth/session")"
[[ "$login_status" == 200 ]] || fail "login returned $login_status"
csrf_token="$(jq -er '.csrf_token' "$work_dir/login.json")"
user_id="$(jq -er '.principal.id' "$work_dir/login.json")"
write_curl_header_config "$work_dir/csrf.curl" X-CSRF-Token "$csrf_token" || fail 'could not freeze CSRF curl config'
quick_payload='{"initial_prompt":null}'
quick_status="$(curl_with_body_stdin "$quick_payload" --silent --show-error --max-time 10 \
  -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 019f3000-0000-7000-8000-000000000001' \
  -o "$work_dir/quick.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/projects:quick-create")"
[[ "$quick_status" == 201 ]] || fail "empty Lane Quick Create returned $quick_status"
project_id="$(jq -er '.project_id' "$work_dir/quick.json")"
poll_bootstrap_ready "$project_id" "$work_dir/bootstrap.json"
session_id="$(jq -er '.session_id' "$work_dir/bootstrap.json")"
empty_status="$(curl --silent --show-error --max-time 10 -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" \
  -o "$work_dir/empty-workspace.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$empty_status" == 200 ]] || fail "empty Lane Workspace returned $empty_status"
jq -e --arg project "$project_id" --arg session "$session_id" '
  .schema_version == "session.workspace.v4"
  and .session.id == $session and .session.project_id == $project
  and .messages == [] and .inputs == []
  and .creation_spec_preview == null and .plan_storyboard_preview == null and .write_prompts_preview == null
  and .event_high_watermark == 1
' "$work_dir/empty-workspace.json" >/dev/null || fail 'Quick Create did not produce an empty Session Lane'

# 使用已发布的 CreationSpec 公共入口创建真实上游 Draft；各 source-filtered Processor 串行独占。
stop_profile_runtimes
enable_creation_spec_profile
configure_creation_spec_addresses "$local_ipv4"
start_business creation-spec-source
start_agent creation-spec-source
wait_etcd_prefix_count '/dora/services/dora-business-service/' 1 Business
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent
creation_goal="Write Prompts M4 Source ${run_label}"
creation_payload="$(jq -cn --arg goal "$creation_goal" '{schema_version:"plan_creation_spec.preview.intent.v1",goal:$goal,deliverable_type:"video",audience:"M4 本地试跑",locale:"zh-CN",constraints:["时长 30 秒","保持叙事节奏"]}')"
creation_status="$(curl_with_body_stdin "$creation_payload" --silent --show-error --max-time 10 \
  -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 019f3000-0000-7000-8000-000000000002' \
  -o "$work_dir/creation-enqueue.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/creation-spec-previews")"
[[ "$creation_status" == 202 ]] || fail "CreationSpec Draft enqueue returned $creation_status"
poll_creation_spec_draft "$session_id" "$work_dir/creation-workspace.json"
creation_spec_id="$(jq -er '.creation_spec_preview.creation_spec_id' "$work_dir/creation-workspace.json")"
creation_spec_version="$(jq -er '.creation_spec_preview.version' "$work_dir/creation-workspace.json")"
creation_spec_digest="$(jq -er '.creation_spec_preview.content_digest' "$work_dir/creation-workspace.json")"

# 旧 Storyboard BFF 尚固定消费 Workspace v3；使用 M4 允许的安全 SQL fixture 建立隔离 Preview 双权威 Source。
stop_profile_runtimes
seed_storyboard_preview_fixture "$user_id" "$project_id" "$session_id" "$creation_spec_id" "$creation_spec_digest"

# Trial 主阶段只允许 write_prompts 双端 Profile；所有其他 Runtime 在同一函数中显式关闭。
enable_write_prompts_profile
configure_preview_loopback_addresses
start_business write-prompts
start_agent write-prompts
wait_etcd_prefix_count '/dora/services/dora-business-service/' 1 Business
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent

write_profile_status="$(curl --silent --show-error --max-time 10 -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" \
  -o "$work_dir/write-profile-workspace.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$write_profile_status" == 200 ]] || fail "write_prompts Profile Workspace returned $write_profile_status"
jq -e --arg storyboard "$storyboard_preview_id" --arg digest "$storyboard_content_digest" --argjson count "$target_count" '
  .schema_version == "session.workspace.v4"
  and .plan_storyboard_preview.storyboard_preview_id == $storyboard
  and .plan_storyboard_preview.content_digest == $digest
  and (.plan_storyboard_preview.slots | length) == $count
  and .write_prompts_preview == null
' "$work_dir/write-profile-workspace.json" >/dev/null || fail 'Profile switch did not preserve the authoritative Storyboard Source'
source_high_watermark="$(jq -er '.event_high_watermark' "$work_dir/write-profile-workspace.json")"
target_count="$(jq -er '.plan_storyboard_preview.slots | length' "$work_dir/write-profile-workspace.json")"

(
  cd "$repo_root/frontend"
  exec env VITE_BUSINESS_API_TARGET="http://127.0.0.1:${business_http_port}" \
    VITE_AGENT_API_TARGET="http://127.0.0.1:${agent_http_port}" \
    VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED=false \
    VITE_DORA_PLAN_STORYBOARD_RUNTIME_ENABLED=false \
    VITE_DORA_WRITE_PROMPTS_RUNTIME_ENABLED=true \
    ./node_modules/.bin/vite --host 127.0.0.1 --port "$vite_port" --strictPort
) >"$work_dir/Vite.log" 2>&1 &
vite_pid="$!"
wait_vite_ready

browser_result="$work_dir/browser-result.json"
writing_instruction='为每个媒体槽位编写清晰、可直接复用且与叙事目标一致的生成提示词'
(
  cd "$repo_root/frontend"
  DORA_E2E_WRITE_PROMPTS_RUNTIME=1 \
  DORA_E2E_EXTERNAL_SERVER=1 \
  DORA_E2E_BASE_URL="http://127.0.0.1:${vite_port}" \
  DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:${business_http_port}" \
  DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
  DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
  DORA_E2E_WRITE_PROMPTS_RESULT_PATH="$browser_result" \
  DORA_E2E_WRITE_PROMPTS_CONTROL_DIR="$control_dir" \
  DORA_E2E_WRITE_PROMPTS_INSTRUCTION="$writing_instruction" \
  DORA_E2E_WRITE_PROMPTS_OUTPUT_LANGUAGE=zh-CN \
  DORA_E2E_PROJECT_ID="$project_id" \
  DORA_E2E_SESSION_ID="$session_id" \
  DORA_E2E_STORYBOARD_PREVIEW_ID="$storyboard_preview_id" \
  DORA_E2E_STORYBOARD_PREVIEW_VERSION=1 \
  DORA_E2E_STORYBOARD_CONTENT_DIGEST="$storyboard_content_digest" \
  DORA_E2E_SOURCE_HIGH_WATERMARK="$source_high_watermark" \
    ./node_modules/.bin/playwright test e2e/write-prompts-runtime.spec.js --grep '@write-prompts-runtime'
) >"$work_dir/Playwright.log" 2>&1 &
playwright_pid="$!"

restart_request="$control_dir/agent-restart-request.json"
wait_for_control_file "$restart_request" '
  .schema_version == "write_prompts_runtime.restart_request.v1"
  and all(.project_id,.session_id,.input_id,.turn_id,.run_id,.tool_call_id,.prompt_preview_id;
    test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
' 'Agent restart request'
[[ "$(jq -er '.project_id' "$restart_request")" == "$project_id" \
  && "$(jq -er '.session_id' "$restart_request")" == "$session_id" ]] || fail 'Agent restart request identity drifted'
prompt_preview_id="$(jq -er '.prompt_preview_id' "$restart_request")"

stop_pid_strict "$agent_pid" Agent-restart-checkpoint
agent_pid=""
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 0 Agent
wait_port_closed "$agent_http_port" Agent-HTTP
wait_port_closed "$agent_rpc_port" Agent-RPC
disconnect_observed="$control_dir/agent-disconnect-observed.json"
wait_for_control_file "$disconnect_observed" '
  .schema_version == "write_prompts_runtime.disconnect_observed.v1" and .stream_state == "reconnecting"
  and (.session_id | test("^[0-9a-f-]{36}$")) and (.prompt_preview_id | test("^[0-9a-f-]{36}$"))
' 'browser Agent disconnect observation'
start_agent write-prompts-reconnect
wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent
write_atomic_json "$control_dir/agent-restart-ack.json" -n \
  --arg session_id "$session_id" --arg prompt_preview_id "$prompt_preview_id" '
  {schema_version:"write_prompts_runtime.restart_ack.v1",session_id:$session_id,
   prompt_preview_id:$prompt_preview_id,agent_ready:true}
'

set +e
wait "$playwright_pid"
playwright_status="$?"
set -e
playwright_pid=""
if [[ "$playwright_status" -ne 0 ]]; then
  sed -n '1,420p' "$work_dir/Playwright.log" >&2 || true
  fail 'Write Prompts Chromium smoke failed'
fi
[[ "$(file_mode "$browser_result")" == 600 ]] || fail 'browser result permissions are not 0600'
jq -e --arg project "$project_id" --arg session "$session_id" --arg storyboard "$storyboard_preview_id" \
  --arg storyboard_digest "$storyboard_content_digest" --arg prompt_preview "$prompt_preview_id" --argjson count "$target_count" '
  keys == ["assertions","event_high_watermark","input_id","project_id","prompt_content_digest","prompt_preview_id",
    "request_id","run_id","schema_version","session_id","status","storyboard_content_digest","storyboard_preview_id",
    "target_count","tool_call_id","turn_id"]
  and .schema_version == "write_prompts_runtime.browser_result.v1" and .status == "passed"
  and .project_id == $project and .session_id == $session
  and .storyboard_preview_id == $storyboard and .storyboard_content_digest == $storyboard_digest
  and .prompt_preview_id == $prompt_preview and .target_count == $count
  and (.prompt_content_digest | test("^[0-9a-f]{64}$"))
  and all(.input_id,.request_id,.turn_id,.run_id,.tool_call_id; test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
  and (.assertions | keys) == ["accepted_sse_observed","agent_disconnect_observed","agent_reconnect_recovered",
    "authoritative_storyboard_bound","chromium_browser","full_exact_target_set","hard_reload_recovered",
    "prompt_card_visible","same_origin_business_bff","static_catalog_unavailable","terminal_sse_observed",
    "write_prompts_form_submitted"]
  and all(.assertions[]; . == true)
' "$browser_result" >/dev/null || fail 'Write Prompts browser result is invalid'

set +e
curl --silent --show-error --no-buffer --max-time 8 -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/events?after_seq=${source_high_watermark}" \
  >"$work_dir/write-prompts-events.sse" 2>"$work_dir/write-prompts-events.log"
sse_status="$?"
set -e
[[ "$sse_status" == 0 || "$sse_status" == 28 ]] || fail "Write Prompts SSE replay failed with $sse_status"
grep -F 'event: write_prompts.preview.accepted' "$work_dir/write-prompts-events.sse" >/dev/null || fail 'SSE omitted write_prompts accepted event'
grep -F 'event: write_prompts.preview.completed' "$work_dir/write-prompts-events.sse" >/dev/null || fail 'SSE omitted write_prompts terminal event'
if rg -n 'intent_ciphertext|tool_intent|prompt_messages|provider_payload|access_scope|business_command_body|reasoning' \
  "$work_dir/write-prompts-events.sse" >/dev/null; then
  fail 'Write Prompts SSE exposed a forbidden payload field'
fi

final_snapshot_status="$(curl --silent --show-error --max-time 10 -b "$work_dir/cookies.txt" --config "$work_dir/csrf.curl" \
  -o "$work_dir/final-workspace.json" -w '%{http_code}' \
  "http://127.0.0.1:${business_http_port}/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$final_snapshot_status" == 200 ]] || fail "final Workspace Snapshot returned $final_snapshot_status"
jq -e --slurpfile browser "$browser_result" --arg storyboard "$storyboard_preview_id" --arg storyboard_digest "$storyboard_content_digest" '
  .schema_version == "session.workspace.v4"
  and .plan_storyboard_preview.storyboard_preview_id == $storyboard
  and .plan_storyboard_preview.content_digest == $storyboard_digest
  and .write_prompts_preview.schema_version == "prompt.preview.card.v1"
  and .write_prompts_preview.status == "completed"
  and .write_prompts_preview.prompt_preview_id == $browser[0].prompt_preview_id
  and .write_prompts_preview.input_id == $browser[0].input_id
  and .write_prompts_preview.turn_id == $browser[0].turn_id
  and .write_prompts_preview.run_id == $browser[0].run_id
  and .write_prompts_preview.tool_call_id == $browser[0].tool_call_id
  and .event_high_watermark == $browser[0].event_high_watermark
' "$work_dir/final-workspace.json" >/dev/null || fail 'final Snapshot lost Prompt frozen identity'

assert_runtime_database_contract "$(jq -er '.input_id' "$browser_result")" \
  "$(jq -er '.tool_call_id' "$browser_result")" "$prompt_preview_id"

write_source_manifest "$work_dir/source-after.sha256" || fail 'could not rebuild the source manifest'
cmp -s "$work_dir/source-before.sha256" "$work_dir/source-after.sha256" || fail 'source tree changed during canonical Write Prompts Trial'
source_digest="$(sha256_file "$work_dir/source-before.sha256")"

stop_pid_best_effort "$vite_pid"
vite_pid=""
stop_profile_runtimes
wait_port_closed "$vite_port" Vite

jq -n -S \
  --arg run_label "$run_label" --arg source_digest "sha256:${source_digest}" \
  --arg project_id "$project_id" --arg session_id "$session_id" \
  --arg storyboard_preview_id "$storyboard_preview_id" --arg storyboard_digest "$storyboard_content_digest" \
  --arg input_id "$(jq -er '.input_id' "$browser_result")" --arg turn_id "$(jq -er '.turn_id' "$browser_result")" \
  --arg run_id "$(jq -er '.run_id' "$browser_result")" --arg tool_call_id "$(jq -er '.tool_call_id' "$browser_result")" \
  --arg prompt_preview_id "$prompt_preview_id" --arg prompt_digest "$(jq -er '.prompt_content_digest' "$browser_result")" \
  --argjson postgres_port "$postgres_port" --argjson redis_port "$redis_port" --argjson etcd_port "$etcd_port" \
  --argjson target_count "$target_count" --argjson source_high_watermark "$source_high_watermark" \
  --argjson final_high_watermark "$(jq -er '.event_high_watermark' "$browser_result")" '
  {
    schema_version:"write_prompts_runtime_v2_smoke_evidence.v1",status:"pending",trial_run:$run_label,
    profile:"write_prompts.runtime.v2preview1",source_digest:$source_digest,
    infrastructure:{
      postgresql:{host:"127.0.0.1",port:$postgres_port,database:"dedicated_test"},
      redis:{host:"127.0.0.1",port:$redis_port,service_connected:true},
      etcd:{host:"127.0.0.1",port:$etcd_port,service_registered:true}
    },
    identity:{project_id:$project_id,session_id:$session_id,storyboard_preview_id:$storyboard_preview_id,
      input_id:$input_id,turn_id:$turn_id,run_id:$run_id,tool_call_id:$tool_call_id,
      prompt_preview_id:$prompt_preview_id},
    digests:{storyboard:$storyboard_digest,result:$prompt_digest},
    counts:{target_count:$target_count,router_model_calls:1,graph_model_calls:1,drafts:1,command_receipts:1,
      source_high_watermark:$source_high_watermark,final_high_watermark:$final_high_watermark},
    assertions:{authoritative_storyboard_source:true,exclusive_profile_switch:true,only_write_prompts_target_profile:true,
      chromium_form_submission:true,accepted_sse:true,terminal_sse:true,prompt_card:true,full_exact_target_set:true,
      hard_reload_recovered:true,agent_disconnect_observed:true,agent_reconnect_recovered:true,
      layered_receipts_once:true,business_draft_and_receipt_once:true,static_catalog_unavailable:true,
      no_production_prompt_or_async_aggregates:true,direct_host_infrastructure:true,source_unchanged:true,
      etcd_exact_instances_cleaned:true,runtime_cleanup:true,evidence_redacted:false}
  }
' >"$evidence_pending"
chmod 600 "$evidence_pending"
jq -e '
  .schema_version == "write_prompts_runtime_v2_smoke_evidence.v1" and .status == "pending"
  and .profile == "write_prompts.runtime.v2preview1"
  and .counts.router_model_calls == 1 and .counts.graph_model_calls == 1
  and .counts.drafts == 1 and .counts.command_receipts == 1
  and .counts.final_high_watermark == (.counts.source_high_watermark + 2)
  and .assertions.evidence_redacted == false
  and all(.assertions | to_entries[] | select(.key != "evidence_redacted"); .value == true)
' "$evidence_pending" >/dev/null || fail 'pending Trial Evidence is invalid'
assert_evidence_redacted
jq -S '.status = "passed" | .assertions.evidence_redacted = true' "$evidence_pending" >"${evidence_file}.tmp"
chmod 600 "${evidence_file}.tmp"
mv "${evidence_file}.tmp" "$evidence_file"
chmod 600 "$evidence_file"
rm -f "$evidence_pending"
[[ "$(file_mode "$evidence_file")" == 600 ]] || fail 'published Evidence permissions are not 0600'
jq -e '.status == "passed" and all(.assertions[]; . == true)' "$evidence_file" >/dev/null || fail 'published Evidence is invalid'
evidence_published=true

printf 'write-prompts-runtime-v2 direct-host canonical smoke passed: %s\n' "$evidence_file"
