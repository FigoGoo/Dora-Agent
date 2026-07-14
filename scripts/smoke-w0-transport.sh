#!/usr/bin/env bash
set -euo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${ENV_FILE:-$repo_root/.env.example}"
go_bin="${GO_BIN:-/Users/figo/sdk/go1.26.3/bin/go}"
migrate_bin="${MIGRATE_BIN:-$repo_root/.local/tools/migrate}"
compose=(docker compose --env-file "$env_file" -f "$repo_root/deploy/local/compose.yaml")
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
evidence_dir="$repo_root/.local/smoke/w0-transport/runs/$run_id"
evidence_scan_root="$repo_root/.local/smoke/w0-transport/runs"
evidence_file="$repo_root/.local/smoke/w05-workspace-transport-evidence.json"
legacy_evidence_file="$repo_root/.local/smoke/w0-transport-evidence.json"
pending_evidence_file="$evidence_dir/evidence-summary.pending.json"
cookie_jar=""
login_response_temp=""
workspace_response_temp=""
owner_b_cookie_jar=""
owner_b_seed_response_temp=""
owner_b_login_response_temp=""
owner_b_denied_response_temp=""
owner_b_denied_headers_temp=""
source_manifest_temp=""
owner_b_password=""
owner_b_csrf_token=""
owner_b_cookie_token=""
business_pid=""
agent_pid=""
browser_smoke_ran=false

stop_processes() {
  for pid in "$business_pid" "$agent_pid"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid"
    fi
  done
  for pid in "$business_pid" "$agent_pid"; do
    if [[ -n "$pid" ]]; then
      wait "$pid" 2>/dev/null || true
    fi
  done
  if [[ -n "$cookie_jar" ]]; then
    rm -f "$cookie_jar"
  fi
  if [[ -n "$login_response_temp" ]]; then
    rm -f "$login_response_temp"
  fi
  if [[ -n "$workspace_response_temp" ]]; then
    rm -f "$workspace_response_temp"
  fi
  if [[ -n "$owner_b_cookie_jar" ]]; then
    rm -f "$owner_b_cookie_jar"
  fi
  if [[ -n "$owner_b_seed_response_temp" ]]; then
    rm -f "$owner_b_seed_response_temp"
  fi
  if [[ -n "$owner_b_login_response_temp" ]]; then
    rm -f "$owner_b_login_response_temp"
  fi
  if [[ -n "$owner_b_denied_response_temp" ]]; then
    rm -f "$owner_b_denied_response_temp"
  fi
  if [[ -n "$owner_b_denied_headers_temp" ]]; then
    rm -f "$owner_b_denied_headers_temp"
  fi
  if [[ -n "$source_manifest_temp" ]]; then
    rm -f "$source_manifest_temp"
  fi
}

fail() {
  echo "W0 Transport 冒烟失败: $1" >&2
  exit 1
}

wait_ready() {
  local port="$1"
  local process_id="$2"
  for _ in $(seq 1 120); do
    if ! kill -0 "$process_id" 2>/dev/null; then
      fail "端口 $port 对应 Runtime 提前退出"
    fi
    if curl --fail --silent --max-time 1 "http://127.0.0.1:${port}/readyz" >/dev/null; then
      return 0
    fi
    sleep 0.25
  done
  fail "端口 $port 的 Readiness 未在 30 秒内成功"
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
    candidate="$(ifconfig | awk '$1 == "inet" && $2 !~ /^127\./ && $2 !~ /^169\.254\./ && $2 !~ /^198\.1[89]\./ && $2 != "0.0.0.0" {print $2; exit}')"
  fi
  if [[ -z "$candidate" || "$candidate" == 127.* || "$candidate" == "0.0.0.0" ]]; then
    return 1
  fi
  printf '%s' "$candidate"
}

poll_bootstrap_ready() {
  local project_id="$1"
  local output_file="$2"
  local status=""
  for _ in $(seq 1 120); do
    status="$(curl --silent --show-error --max-time 3 -b "$cookie_jar" \
      -o "$output_file" -w '%{http_code}' \
      "http://127.0.0.1:18081/api/v1/projects/${project_id}/bootstrap")"
    if [[ "$status" == "200" ]] && jq -e '.creation_status == "ready"' "$output_file" >/dev/null; then
      return 0
    fi
    if [[ "$status" == "409" ]]; then
      return 1
    fi
    sleep 0.25
  done
  return 1
}

run_concurrent_quick_create() {
  local idempotency_key="$1"
  local payload="$2"
  local batch_dir="$3"
  local csrf_token="$4"
  local request_pids=()
  local index=""
  mkdir -p "$batch_dir"
  for index in $(seq 1 100); do
    curl --silent --show-error --max-time 10 -b "$cookie_jar" \
      -H 'Content-Type: application/json' \
      -H "X-CSRF-Token: $csrf_token" \
      -H "Idempotency-Key: $idempotency_key" \
      --data-binary "$payload" \
      -o "$batch_dir/$index.json" -w '%{http_code}' \
      'http://127.0.0.1:18081/api/v1/projects:quick-create' >"$batch_dir/$index.status" &
    request_pids+=("$!")
  done
  for index in "${request_pids[@]}"; do
    wait "$index"
  done

  local expected_project_id=""
  local created_count=0
  local response_status=""
  local response_project_id=""
  for index in $(seq 1 100); do
    response_status="$(tr -d '[:space:]' <"$batch_dir/$index.status")"
    if [[ "$response_status" != "200" && "$response_status" != "201" ]]; then
      fail "并发 Quick Create 第 $index 个响应状态为 $response_status"
    fi
    if [[ "$response_status" == "201" ]]; then
      created_count=$((created_count + 1))
    fi
    response_project_id="$(jq -er '.project_id | strings | select(length > 0)' "$batch_dir/$index.json")"
    if [[ -z "$expected_project_id" ]]; then
      expected_project_id="$response_project_id"
    elif [[ "$response_project_id" != "$expected_project_id" ]]; then
      fail "同键并发 Quick Create 返回了不同 Project"
    fi
  done
  if [[ "$created_count" -ne 1 ]]; then
    fail "同键并发 Quick Create 的 201 数量为 $created_count，期望 1"
  fi
  printf '%s' "$expected_project_id"
}

assert_owner_safe_error() {
  local response_file="$1"
  local expected_code="$2"
  local project_id="$3"
  local session_id="$4"
  local input_id="$5"
  local prompt="$6"
  jq -e --arg code "$expected_code" --arg project "$project_id" --arg session "$session_id" \
    --arg input "$input_id" --arg prompt "$prompt" \
    'keys == ["error"]
     and (.error | keys) == ["code", "details", "message", "request_id", "retryable"]
     and .error.code == $code
     and (.error.message | type) == "string"
     and (.error.request_id | type) == "string"
     and .error.retryable == false
     and .error.details == {}
     and all(.. | strings;
       ((contains($project) or contains($session) or contains($input) or contains($prompt)) | not))' \
    "$response_file" >/dev/null
}

assert_evidence_excludes_literal() {
  local value="$1"
  local label="$2"
  local scan_status=""
  if rg -F --quiet -- "$value" "$evidence_scan_root"; then
    fail "Evidence 中检测到${label}"
  else
    scan_status="$?"
    [[ "$scan_status" == "1" ]] || fail "Evidence ${label}脱敏扫描异常（rg exit=$scan_status）"
  fi
}

assert_evidence_excludes_regex() {
  local pattern="$1"
  local label="$2"
  local scan_status=""
  if rg --quiet -- "$pattern" "$evidence_scan_root"; then
    fail "Evidence 中检测到${label}"
  else
    scan_status="$?"
    [[ "$scan_status" == "1" ]] || fail "Evidence ${label}脱敏扫描异常（rg exit=$scan_status）"
  fi
}

sha256_file() {
  local file="$1"
  local output=""
  local digest=""
  output="$(shasum -a 256 "$file")" || return 1
  digest="${output%% *}"
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || return 1
  printf '%s' "$digest"
}

trap stop_processes EXIT
mkdir -p "$evidence_dir"
mkdir -p "$evidence_dir/responses"
# 新运行开始即撤销旧的 canonical summary；失败运行不得让消费者继续读取上一次 passed。
rm -f "$evidence_file" "$legacy_evidence_file"
cookie_jar="$(mktemp "${TMPDIR:-/tmp}/dora-w0-cookie.XXXXXX")"
owner_b_cookie_jar="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-cookie.XXXXXX")"
login_response_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w0-login.XXXXXX")"
workspace_response_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-workspace.XXXXXX")"
owner_b_seed_response_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-seed.XXXXXX")"
owner_b_login_response_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-login.XXXXXX")"
owner_b_denied_response_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-denied.XXXXXX")"
owner_b_denied_headers_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-denied-headers.XXXXXX")"
source_manifest_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-source-manifest.XXXXXX")"
chmod 600 "$cookie_jar" "$owner_b_cookie_jar" "$login_response_temp" "$workspace_response_temp" \
  "$owner_b_seed_response_temp" "$owner_b_login_response_temp" "$owner_b_denied_response_temp" \
  "$owner_b_denied_headers_temp" "$source_manifest_temp"

set -a
. "$env_file"
set +a

[[ "${DORA_ENV:-}" == "local" ]] || fail "DORA_ENV 必须为 local"
[[ -x "$go_bin" ]] || fail "未找到固定 Go SDK"
[[ -x "$migrate_bin" ]] || fail "未找到 golang-migrate CLI"
[[ -x "$repo_root/.local/bin/business-service" ]] || fail "未构建 business-service"
[[ -x "$repo_root/.local/bin/agent-service" ]] || fail "未构建 agent-service"
command -v shasum >/dev/null 2>&1 || fail "未找到 shasum"
[[ -d "$repo_root/frontend/src" && -d "$repo_root/frontend/e2e" ]] || fail "前端源码或 E2E 目录缺失"
business_binary_sha256="$(sha256_file "$repo_root/.local/bin/business-service")" || fail "Business Runtime SHA-256 计算失败"
agent_binary_sha256="$(sha256_file "$repo_root/.local/bin/agent-service")" || fail "Agent Runtime SHA-256 计算失败"
while IFS= read -r source_file; do
  source_file_sha256="$(sha256_file "$repo_root/$source_file")" || fail "Source SHA-256 计算失败: $source_file"
  printf '%s  %s\n' "$source_file_sha256" "$source_file" >>"$source_manifest_temp"
done < <(
  cd "$repo_root"
  {
    find business agent -type f \( -name '*.go' -o -name '*.sql' -o -name '*.thrift' -o -name '*.proto' \) -print
    find frontend/src frontend/e2e -type f -print
    find frontend -maxdepth 1 -type f \( -name 'package.json' -o -name 'package-lock.json' -o -name 'npm-shrinkwrap.json' \) -print
    printf '%s\n' 'scripts/smoke-w0-transport.sh'
  } | LC_ALL=C sort -u
)
[[ -s "$source_manifest_temp" ]] || fail "Source SHA-256 manifest 为空"
source_digest_sha256="$(sha256_file "$source_manifest_temp")" || fail "Source digest SHA-256 计算失败"
rm -f "$source_manifest_temp"
source_manifest_temp=""
owner_b_email="owner-b.${DORA_SMOKE_USER_EMAIL}"
owner_b_password="owner-b-${DORA_SMOKE_USER_PASSWORD}"
owner_b_display_name="本地冒烟权限用户"

"${compose[@]}" up -d
ENV_FILE="$env_file" "$repo_root/scripts/wait-for-local-infra.sh"
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" business up
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" agent up
(
  cd "$repo_root/business"
  GOWORK=off "$go_bin" run ./cmd/local-smoke-seeder
)
(
  cd "$repo_root/business"
  DORA_SMOKE_USER_EMAIL="$owner_b_email" DORA_SMOKE_USER_PASSWORD="$owner_b_password" \
    DORA_SMOKE_USER_DISPLAY_NAME="$owner_b_display_name" GOWORK=off "$go_bin" run ./cmd/local-smoke-seeder
) >"$owner_b_seed_response_temp"
owner_b_seed_user_id="$(jq -er 'select(.status == "ready") | .user_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
rm -f "$owner_b_seed_response_temp"
owner_b_seed_response_temp=""

if [[ "${BUSINESS_RPC_ADVERTISED_ADDRESS%:*}" == "host.docker.internal" ]]; then
  BUSINESS_RPC_ADVERTISED_ADDRESS="$(discover_local_ipv4):${BUSINESS_RPC_LISTEN_ADDR##*:}"
  export BUSINESS_RPC_ADVERTISED_ADDRESS
fi
if [[ "${AGENT_RPC_ADVERTISED_ADDRESS%:*}" == "host.docker.internal" ]]; then
  AGENT_RPC_ADVERTISED_ADDRESS="$(discover_local_ipv4):${AGENT_RPC_LISTEN_ADDR##*:}"
  export AGENT_RPC_ADVERTISED_ADDRESS
fi

"$repo_root/.local/bin/business-service" >"$evidence_dir/business.log" 2>&1 &
business_pid="$!"
"$repo_root/.local/bin/agent-service" >"$evidence_dir/agent.log" 2>&1 &
agent_pid="$!"
wait_ready 18081 "$business_pid"
wait_ready 18082 "$agent_pid"

login_status="$(curl --silent --show-error --max-time 10 -c "$cookie_jar" \
  -H 'Content-Type: application/json' \
  --data-binary "$(jq -cn --arg email "$DORA_SMOKE_USER_EMAIL" --arg password "$DORA_SMOKE_USER_PASSWORD" '{email:$email,password:$password}')" \
  -o "$login_response_temp" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/auth/session')"
[[ "$login_status" == "200" ]] || fail "登录状态为 $login_status"
csrf_token="$(jq -er '.csrf_token | strings | select(length > 0)' "$login_response_temp")"
user_id="$(jq -er '.principal.id | strings | select(length > 0)' "$login_response_temp")"
jq 'del(.csrf_token)' "$login_response_temp" >"$evidence_dir/responses/login.json"
rm -f "$login_response_temp"
login_response_temp=""

intent_key="w0-prompt-$(date +%s)-$$"
prompt_payload='{"initial_prompt":" W0 Transport é Smoke "}'
project_id="$(run_concurrent_quick_create "$intent_key" "$prompt_payload" "$evidence_dir/responses/prompt-batch" "$csrf_token")"
[[ "$project_id" =~ ^[0-9a-f-]{36}$ ]] || fail "Project ID 格式无效"
poll_bootstrap_ready "$project_id" "$evidence_dir/responses/prompt-bootstrap.json" || fail "非空 Prompt Project 未进入 ready"
session_id="$(jq -er '.session_id | strings | select(length > 0)' "$evidence_dir/responses/prompt-bootstrap.json")"
input_id="$(jq -er '.input_id | strings | select(length > 0)' "$evidence_dir/responses/prompt-bootstrap.json")"

replay_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf_token" -H "Idempotency-Key: $intent_key" \
  --data-binary "$prompt_payload" -o "$evidence_dir/responses/prompt-replay.json" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/projects:quick-create')"
[[ "$replay_status" == "200" ]] || fail "同义重放状态为 $replay_status"
jq -e --arg project "$project_id" --arg session "$session_id" --arg input "$input_id" \
  '.project_id == $project and .session_id == $session and .input_id == $input and .creation_status == "ready"' \
  "$evidence_dir/responses/prompt-replay.json" >/dev/null || fail "同义重放未返回冻结结果"

conflict_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf_token" -H "Idempotency-Key: $intent_key" \
  --data-binary '{"initial_prompt":"different semantic prompt"}' \
  -o "$evidence_dir/responses/prompt-conflict.json" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/projects:quick-create')"
[[ "$conflict_status" == "409" ]] || fail "同键异义状态为 $conflict_status"
jq -e '.error.code == "IDEMPOTENCY_CONFLICT"' "$evidence_dir/responses/prompt-conflict.json" >/dev/null || fail "同键异义错误码漂移"

blank_key="w0-blank-$(date +%s)-$$"
blank_payload='{"initial_prompt":" \t\n　"}'
blank_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf_token" -H "Idempotency-Key: $blank_key" \
  --data-binary "$blank_payload" -o "$evidence_dir/responses/blank-create.json" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/projects:quick-create')"
[[ "$blank_status" == "201" ]] || fail "空 Prompt 创建状态为 $blank_status"
blank_project_id="$(jq -er '.project_id | strings | select(length > 0)' "$evidence_dir/responses/blank-create.json")"
[[ "$blank_project_id" =~ ^[0-9a-f-]{36}$ ]] || fail "空 Prompt Project ID 格式无效"
poll_bootstrap_ready "$blank_project_id" "$evidence_dir/responses/blank-bootstrap.json" || fail "空 Prompt Project 未进入 ready"
blank_session_id="$(jq -er '.session_id | strings | select(length > 0)' "$evidence_dir/responses/blank-bootstrap.json")"
jq -e '.input_id == null and .initial_prompt_status == "absent"' "$evidence_dir/responses/blank-bootstrap.json" >/dev/null || fail "空 Prompt 创建了 Input 或错误状态"

postgres_container="$("${compose[@]}" ps -q postgres)"
[[ -n "$postgres_container" ]] || fail "未找到 PostgreSQL 容器"
business_assertion="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
  SELECT json_build_object(
    'owner_matches', project.owner_user_id = '$user_id'::uuid,
    'binding_status', binding.provisioning_status,
    'outbox_status', outbox.status,
    'receipt_count', (SELECT COUNT(*) FROM business.project_creation_receipt WHERE project_id = project.id),
    'prompt_ciphertext_cleared', outbox.payload_ciphertext IS NULL AND outbox.payload_nonce IS NULL AND outbox.payload_key_version IS NULL AND outbox.payload_cleared_at IS NOT NULL
  )
  FROM business.project AS project
  JOIN business.project_session_binding AS binding ON binding.project_id = project.id
  JOIN business.project_session_outbox AS outbox ON outbox.id = binding.command_id
  WHERE project.id = '$project_id'::uuid;")"
echo "$business_assertion" >"$evidence_dir/responses/business-prompt-assertion.json"
jq -e '.owner_matches and .binding_status == "ready" and .outbox_status == "delivered" and .receipt_count == 1 and .prompt_ciphertext_cleared' \
  "$evidence_dir/responses/business-prompt-assertion.json" >/dev/null || fail "Business 非空 Prompt 权威事实断言失败"

agent_assertion="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent -Atc "
  SELECT json_build_object(
    'session_count', (SELECT COUNT(*) FROM agent.session WHERE project_id = '$project_id'::uuid AND user_id = '$user_id'::uuid),
    'snapshot_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot WHERE session_id = '$session_id'::uuid),
    'receipt_count', (SELECT COUNT(*) FROM agent.session_command_receipt WHERE session_id = '$session_id'::uuid),
    'message_count', (SELECT COUNT(*) FROM agent.session_message WHERE session_id = '$session_id'::uuid),
    'input_count', (SELECT COUNT(*) FROM agent.session_input WHERE session_id = '$session_id'::uuid AND id = '$input_id'::uuid),
    'event_count', (SELECT COUNT(*) FROM agent.session_event_log WHERE session_id = '$session_id'::uuid)
  );")"
echo "$agent_assertion" >"$evidence_dir/responses/agent-prompt-assertion.json"
jq -e '.session_count == 1 and .snapshot_count == 1 and .receipt_count == 1 and .message_count == 1 and .input_count == 1 and .event_count == 2' \
  "$evidence_dir/responses/agent-prompt-assertion.json" >/dev/null || fail "Agent 非空 Prompt 权威事实断言失败"

blank_assertion="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent -Atc "
  SELECT json_build_object(
    'session_count', (SELECT COUNT(*) FROM agent.session WHERE id = '$blank_session_id'::uuid AND project_id = '$blank_project_id'::uuid),
    'message_count', (SELECT COUNT(*) FROM agent.session_message WHERE session_id = '$blank_session_id'::uuid),
    'input_count', (SELECT COUNT(*) FROM agent.session_input WHERE session_id = '$blank_session_id'::uuid),
    'receipt_count', (SELECT COUNT(*) FROM agent.session_command_receipt WHERE session_id = '$blank_session_id'::uuid AND message_id IS NULL AND input_id IS NULL),
    'event_count', (SELECT COUNT(*) FROM agent.session_event_log WHERE session_id = '$blank_session_id'::uuid)
  );")"
echo "$blank_assertion" >"$evidence_dir/responses/agent-blank-assertion.json"
jq -e '.session_count == 1 and .message_count == 0 and .input_count == 0 and .receipt_count == 1 and .event_count == 1' \
  "$evidence_dir/responses/agent-blank-assertion.json" >/dev/null || fail "Agent 空 Prompt 负向副作用断言失败"

workspace_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
  -o "$workspace_response_temp" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$workspace_status" == "200" ]] || fail "Business BFF Workspace Snapshot 状态为 $workspace_status"
jq -e --arg project "$project_id" --arg session "$session_id" --arg input "$input_id" \
  '.schema_version == "session.workspace.v1"
   and .session.id == $session
   and .session.project_id == $project
   and .session.status == "active"
   and (.messages | type) == "array"
   and (.messages | length) == 1
   and (.messages[0].content | length) > 0
   and (.inputs | type) == "array"
   and (.inputs | length) == 1
   and .inputs[0].id == $input
   and .event_high_watermark == 2
   and .min_available_seq == 1' \
  "$workspace_response_temp" >/dev/null || fail "Workspace Snapshot DTO 或权威内容断言失败"
# 敏感 Snapshot 从不直接写入 Evidence；只从权限为 0600 的临时文件生成删除完整正文后的诊断投影。
jq '(.messages[]? |= del(.content))' "$workspace_response_temp" \
  >"$evidence_dir/responses/workspace-snapshot.json"
rm -f "$workspace_response_temp"
workspace_response_temp=""

blank_workspace_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
  -o "$evidence_dir/responses/blank-workspace-snapshot.json" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/agent/sessions/${blank_session_id}/workspace")"
[[ "$blank_workspace_status" == "200" ]] || fail "空 Prompt Workspace Snapshot 状态为 $blank_workspace_status"
jq -e --arg project "$blank_project_id" --arg session "$blank_session_id" \
  '.schema_version == "session.workspace.v1"
   and .session.id == $session
   and .session.project_id == $project
   and (.messages | type) == "array"
   and (.messages | length) == 0
   and (.inputs | type) == "array"
   and (.inputs | length) == 0
   and .event_high_watermark == 1
   and .min_available_seq == 1' \
  "$evidence_dir/responses/blank-workspace-snapshot.json" >/dev/null || fail "空 Prompt Workspace Snapshot 伪造了 Message/Input"

unknown_session_id="019f0000-0000-7000-8000-ffffffffffff"
unknown_workspace_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
  -o "$evidence_dir/responses/unknown-workspace.json" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/agent/sessions/${unknown_session_id}/workspace")"
[[ "$unknown_workspace_status" == "404" ]] || fail "未知 Session 未按 Owner-safe 404 关闭，状态为 $unknown_workspace_status"
jq -e '.error.code == "SESSION_NOT_FOUND"' "$evidence_dir/responses/unknown-workspace.json" >/dev/null || fail "未知 Session 错误码漂移"

# 第二个真实用户使用独立 Web Session 访问用户 A 的 Project/Agent Session；两个授权边界都必须返回不泄漏资源事实的 404。
owner_b_login_status="$(curl --silent --show-error --max-time 10 -c "$owner_b_cookie_jar" \
  -H 'Content-Type: application/json' \
  --data-binary "$(jq -cn --arg email "$owner_b_email" --arg password "$owner_b_password" '{email:$email,password:$password}')" \
  -o "$owner_b_login_response_temp" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/auth/session')"
[[ "$owner_b_login_status" == "200" ]] || fail "第二用户登录状态为 $owner_b_login_status"
owner_b_user_id="$(jq -er '.principal.id | strings | select(length > 0)' "$owner_b_login_response_temp")"
owner_b_csrf_token="$(jq -er '.csrf_token | strings | select(length > 0)' "$owner_b_login_response_temp")"
[[ "$owner_b_user_id" == "$owner_b_seed_user_id" && "$owner_b_user_id" != "$user_id" ]] || fail "第二用户身份未与用户 A 隔离"
owner_b_cookie_token="$(awk 'NF >= 7 {value=$7} END {print value}' "$owner_b_cookie_jar")"
[[ -n "$owner_b_cookie_token" ]] || fail "第二用户 Cookie 会话未建立"
rm -f "$owner_b_login_response_temp"
owner_b_login_response_temp=""

owner_b_project_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
  -o "$owner_b_denied_response_temp" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/projects/${project_id}/bootstrap")"
[[ "$owner_b_project_status" == "404" ]] || fail "第二用户访问用户 A Project 未按 Owner-safe 404 关闭，状态为 $owner_b_project_status"
assert_owner_safe_error "$owner_b_denied_response_temp" "PROJECT_NOT_FOUND" \
  "$project_id" "$session_id" "$input_id" "W0 Transport é Smoke" || fail "跨 Owner Project 错误响应泄漏了权威资源事实"

: >"$owner_b_denied_response_temp"
owner_b_workspace_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
  -o "$owner_b_denied_response_temp" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$owner_b_workspace_status" == "404" ]] || fail "第二用户访问用户 A Session 未按 Owner-safe 404 关闭，状态为 $owner_b_workspace_status"
assert_owner_safe_error "$owner_b_denied_response_temp" "SESSION_NOT_FOUND" \
  "$project_id" "$session_id" "$input_id" "W0 Transport é Smoke" || fail "跨 Owner Session 错误响应泄漏了权威资源事实"

: >"$owner_b_denied_response_temp"
: >"$owner_b_denied_headers_temp"
owner_b_events_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
  -H 'Accept: text/event-stream' -D "$owner_b_denied_headers_temp" \
  -o "$owner_b_denied_response_temp" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/agent/sessions/${session_id}/events?after_seq=0")"
[[ "$owner_b_events_status" == "404" ]] || fail "第二用户访问用户 A Events 未在 SSE Header 前按 Owner-safe 404 关闭，状态为 $owner_b_events_status"
owner_b_events_content_type="$(tr -d '\r' <"$owner_b_denied_headers_temp" | awk -F ': *' 'tolower($1) == "content-type" {print tolower($2); exit}')"
[[ "$owner_b_events_content_type" == application/json* ]] || fail "跨 Owner Events 未返回普通 JSON 错误，Content-Type=$owner_b_events_content_type"
if tr -d '\r' <"$owner_b_denied_headers_temp" | grep -Eiq '^X-Accel-Buffering:'; then
  fail "跨 Owner Events 在授权失败前提交了 SSE Header"
fi
assert_owner_safe_error "$owner_b_denied_response_temp" "SESSION_NOT_FOUND" \
  "$project_id" "$session_id" "$input_id" "W0 Transport é Smoke" || fail "跨 Owner Events 错误响应泄漏了权威资源事实"
rm -f "$owner_b_denied_response_temp"
owner_b_denied_response_temp=""
rm -f "$owner_b_denied_headers_temp"
owner_b_denied_headers_temp=""

owner_b_logout_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" -c "$owner_b_cookie_jar" \
  -X DELETE -H "X-CSRF-Token: $owner_b_csrf_token" -o /dev/null -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/auth/session')"
[[ "$owner_b_logout_status" == "204" ]] || fail "第二用户退出状态为 $owner_b_logout_status"
jq -n \
  '{project_bootstrap:{status:404,code:"PROJECT_NOT_FOUND"},session_workspace:{status:404,code:"SESSION_NOT_FOUND"},session_events:{status:404,code:"SESSION_NOT_FOUND",content_type:"application/json",sse_headers_committed:false},distinct_principals:true}' \
  >"$evidence_dir/responses/cross-owner-access.json"

direct_agent_status="$(curl --silent --show-error --max-time 10 \
  -o "$evidence_dir/responses/direct-agent-workspace.json" -w '%{http_code}' \
  "http://127.0.0.1:18082/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$direct_agent_status" == "401" ]] || fail "无内部身份断言直连 Agent 未被拒绝，状态为 $direct_agent_status"
jq -e '.error.code == "INTERNAL_IDENTITY_INVALID"' "$evidence_dir/responses/direct-agent-workspace.json" >/dev/null || fail "Agent 内部身份错误码漂移"

sse_status_file="$evidence_dir/responses/workspace-events.status"
sse_exit=0
curl --silent --show-error --no-buffer --max-time 3 -b "$cookie_jar" \
  -H 'Accept: text/event-stream' \
  -o "$evidence_dir/responses/workspace-events.sse" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/agent/sessions/${session_id}/events?after_seq=1" \
  >"$sse_status_file" || sse_exit="$?"
[[ "$sse_exit" == "0" || "$sse_exit" == "28" ]] || fail "Workspace SSE curl 退出码为 $sse_exit"
[[ "$(tr -d '[:space:]' <"$sse_status_file")" == "200" ]] || fail "Workspace SSE 未返回 200"
sed -n 's/^data: //p' "$evidence_dir/responses/workspace-events.sse" \
  | jq -s -e --arg project "$project_id" --arg session "$session_id" --arg input "$input_id" \
    'any(.[]; .schema_version == "workspace.event.v1"
      and .event == "session.input.accepted"
      and .session_id == $session
      and .project_id == $project
      and .seq == 2
      and .aggregate_id == $input)
     and any(.[]; .schema_version == "workspace.stream-control.v1"
      and .event == "stream.ready"
      and .session_id == $session
      and .cursor == 2)' >/dev/null || fail "Workspace SSE 补读或 Ready 控制帧断言失败"
grep -Eq '^id: 2$' "$evidence_dir/responses/workspace-events.sse" || fail "Workspace SSE id 未与 Seq 对齐"

reset_status="$(curl --silent --show-error --no-buffer --max-time 5 -b "$cookie_jar" \
  -H 'Accept: text/event-stream' \
  -o "$evidence_dir/responses/workspace-reset.sse" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/agent/sessions/${session_id}/events?after_seq=0")"
[[ "$reset_status" == "200" ]] || fail "过期 Cursor SSE 状态为 $reset_status"
sed -n 's/^data: //p' "$evidence_dir/responses/workspace-reset.sse" \
  | jq -s -e --arg session "$session_id" \
    'length == 1
     and .[0].schema_version == "workspace.stream-control.v1"
     and .[0].event == "stream.reset"
     and .[0].session_id == $session
     and .[0].reason == "cursor_expired"
     and .[0].snapshot_required == true
     and .[0].min_available_seq == 1
     and .[0].latest_seq == 2' >/dev/null || fail "过期 Cursor 未返回冻结 stream.reset"
if grep -Eq '^id:' "$evidence_dir/responses/workspace-reset.sse"; then
  fail "stream.reset 错误携带了 SSE id"
fi

if [[ "${W0_RUN_BROWSER_SMOKE:-0}" == "1" ]]; then
  [[ -x "$repo_root/frontend/node_modules/.bin/playwright" ]] || fail "未安装前端 Playwright 依赖，请先在 frontend 执行 npm install"
  (
    cd "$repo_root/frontend"
    DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
    DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
    DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:18081" \
    npm run test:e2e:w0
  ) >"$evidence_dir/frontend-playwright.log" 2>&1 || {
    sed -n '1,240p' "$evidence_dir/frontend-playwright.log" >&2
    fail "W0 浏览器页面链路失败"
  }
  browser_smoke_ran=true
fi

logout_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" -c "$cookie_jar" \
  -X DELETE -H "X-CSRF-Token: $csrf_token" -o /dev/null -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/auth/session')"
[[ "$logout_status" == "204" ]] || fail "退出状态为 $logout_status"
after_logout_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
  -o "$evidence_dir/responses/after-logout.json" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/auth/session')"
[[ "$after_logout_status" == "401" ]] || fail "退出后旧会话仍可用，状态为 $after_logout_status"
after_logout_workspace_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
  -o "$evidence_dir/responses/after-logout-workspace.json" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/agent/sessions/${session_id}/workspace")"
[[ "$after_logout_workspace_status" == "401" ]] || fail "退出后旧会话仍可读取 Workspace，状态为 $after_logout_workspace_status"

produced_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
jq -n \
  --arg schema_version "w05.workspace-transport.smoke.evidence.v1" \
  --arg run_id "$run_id" --arg produced_at "$produced_at" \
  --arg source_digest_sha256 "$source_digest_sha256" \
  --arg business_binary_sha256 "$business_binary_sha256" --arg agent_binary_sha256 "$agent_binary_sha256" \
  --arg project_id "$project_id" --arg session_id "$session_id" --arg input_id "$input_id" \
  --arg blank_project_id "$blank_project_id" --arg blank_session_id "$blank_session_id" \
  --argjson browser_ui "$browser_smoke_ran" \
  '{schema_version:$schema_version,status:"pending",run_id:$run_id,produced_at:$produced_at,source_digest_sha256:$source_digest_sha256,business_binary_sha256:$business_binary_sha256,agent_binary_sha256:$agent_binary_sha256,prompt_project:{project_id:$project_id,session_id:$session_id,input_id:$input_id},blank_project:{project_id:$blank_project_id,session_id:$blank_session_id,input_id:null},assertions:{concurrent_requests:100,idempotent_replay:true,idempotency_conflict:true,business_prompt_cleared:true,agent_unique_facts:true,blank_negative_side_effects:true,workspace_snapshot:true,workspace_empty_arrays:true,workspace_owner_safe_not_found:true,workspace_cross_owner_not_found:true,events_cross_owner_not_found:true,agent_direct_access_denied:true,sse_replay_and_ready:true,sse_cursor_reset:true,browser_ui:$browser_ui,logout_revoked:true,logout_workspace_denied:true}}' \
  >"$pending_evidence_file"

assert_evidence_excludes_literal "$csrf_token" "用户 A CSRF"
assert_evidence_excludes_literal "$DORA_SMOKE_USER_PASSWORD" "用户 A 密码"
assert_evidence_excludes_literal "$owner_b_password" "用户 B 密码"
assert_evidence_excludes_literal "$owner_b_csrf_token" "用户 B CSRF"
assert_evidence_excludes_literal "$owner_b_cookie_token" "用户 B Cookie"
assert_evidence_excludes_literal 'W0 Transport é Smoke' "完整 Prompt"
assert_evidence_excludes_regex '"csrf_token"[[:space:]]*:[[:space:]]*"[^"[:space:]][^"]*"' "任意非空 CSRF JSON 字段"
assert_evidence_excludes_regex 'X-Dora-Identity-(Assertion|Signature):' "内部身份断言材料"

stop_processes
business_pid=""
agent_pid=""

etcd_container="$("${compose[@]}" ps -q etcd)"
remaining_session_keys="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 get /dora/services/dora.agent.session.v1/ --prefix --keys-only)"
[[ -z "$remaining_session_keys" ]] || fail "Agent 退出后仍残留 Session RPC 注册键"

# 只有脱敏扫描、Runtime 退出和 etcd 租约摘除全部成功后，才原子发布 passed summary。
jq '.status = "passed"' "$pending_evidence_file" >"${evidence_file}.tmp"
rm -f "$pending_evidence_file"
# canonical W0.5 summary 是最后一次可失败写操作；旧 W0 summary 已在运行开始撤销，避免双真源假绿。
mv "${evidence_file}.tmp" "$evidence_file"
trap - EXIT

if [[ "$browser_smoke_ran" == "true" ]]; then
  echo "W0.5 Transport API/Snapshot/SSE 与真实浏览器登录、Quick Create、正式工作台、退出冒烟通过"
else
  echo "W0.5 Transport 真实登录、100 并发 Quick Create、Business generated Kitex→Agent、Workspace Snapshot/SSE、空 Prompt 与退出冒烟通过"
fi
