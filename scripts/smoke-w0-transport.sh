#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/smoke-secret-transport.sh
. "$repo_root/scripts/lib/smoke-secret-transport.sh"
disable_shell_xtrace
umask 077
# shellcheck source=lib/w1-smoke-mode.sh
. "$repo_root/scripts/lib/w1-smoke-mode.sh"
env_file="${ENV_FILE:-$repo_root/.env.example}"
go_bin="${GO_BIN:-/Users/figo/sdk/go1.26.3/bin/go}"
migrate_bin="${MIGRATE_BIN:-$repo_root/.local/tools/migrate}"
compose=(docker compose --env-file "$env_file" -f "$repo_root/deploy/local/compose.yaml")
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
w05_browser_prompt="W05 Browser Recovery ${run_id}"
w1_skill_smoke_enabled="${W1_RUN_SKILL_SMOKE:-0}"
w1_browser_smoke_enabled="${W1_RUN_BROWSER_SMOKE:-0}"
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  evidence_dir="$repo_root/.local/smoke/w1-skill-foundation/runs/$run_id"
  evidence_scan_root="$repo_root/.local/smoke/w1-skill-foundation/runs"
  evidence_file="$repo_root/.local/smoke/w1-skill-foundation-evidence.json"
  legacy_evidence_file=""
else
  evidence_dir="$repo_root/.local/smoke/w0-transport/runs/$run_id"
  evidence_scan_root="$repo_root/.local/smoke/w0-transport/runs"
  evidence_file="$repo_root/.local/smoke/w05-workspace-transport-evidence.json"
  legacy_evidence_file="$repo_root/.local/smoke/w0-transport-evidence.json"
fi
pending_evidence_file="$evidence_dir/evidence-summary.pending.json"
cookie_jar=""
user_curl_config=""
login_response_temp=""
workspace_response_temp=""
owner_b_cookie_jar=""
owner_b_curl_config=""
owner_b_seed_response_temp=""
owner_b_login_response_temp=""
owner_b_denied_response_temp=""
owner_b_denied_headers_temp=""
source_manifest_temp=""
w05_browser_result_temp=""
agent_restart_workspace_temp=""
agent_restart_sse_temp=""
agent_restart_sse_status_temp=""
w1_temp_dir=""
owner_b_password=""
owner_b_csrf_token=""
owner_b_cookie_token=""
provisioner_password=""
reviewer_assignment_id=""
reviewer_seed_creator_user_id=""
provisioner_user_id=""
user_cookie_token=""
business_pid=""
agent_pid=""
browser_smoke_ran=false
w1_skill_smoke_ran=false
w1_browser_smoke_ran=false
w1_skill_binding_smoke_ran=false
w1_reviewer_rbac_smoke_ran=false
w1_reviewer_revocation_smoke_ran=false
w1_skill_id=""
w1_review_id=""
w1_skill_name=""
w1_updated_skill_name=""
w1_binding_prompt=""
w1_binding_project_id=""
w1_binding_session_id=""
w1_binding_input_id=""

stop_processes() {
  for pid in "$business_pid" "$agent_pid"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done
  for pid in "$business_pid" "$agent_pid"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      process_stopped=false
      for _ in $(seq 1 120); do
        process_state="$(ps -o stat= -p "$pid" 2>/dev/null | tr -d '[:space:]' || true)"
        if ! kill -0 "$pid" 2>/dev/null || [[ "$process_state" == Z* ]]; then
          process_stopped=true
          break
        fi
        sleep 0.25
      done
      if [[ "$process_stopped" != "true" ]]; then
        kill -KILL "$pid" 2>/dev/null || true
      fi
    fi
    if [[ -n "$pid" ]]; then
      wait "$pid" 2>/dev/null || true
    fi
  done
  if [[ -n "$cookie_jar" ]]; then
    rm -f "$cookie_jar"
  fi
  if [[ -n "$user_curl_config" ]]; then
    rm -f "$user_curl_config"
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
  if [[ -n "$owner_b_curl_config" ]]; then
    rm -f "$owner_b_curl_config"
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
  if [[ -n "$w05_browser_result_temp" ]]; then
    rm -f "$w05_browser_result_temp"
  fi
  if [[ -n "$agent_restart_workspace_temp" ]]; then
    rm -f "$agent_restart_workspace_temp"
  fi
  if [[ -n "$agent_restart_sse_temp" ]]; then
    rm -f "$agent_restart_sse_temp"
  fi
  if [[ -n "$agent_restart_sse_status_temp" ]]; then
    rm -f "$agent_restart_sse_status_temp"
  fi
  if [[ -n "$w1_temp_dir" ]]; then
    rm -rf "$w1_temp_dir"
  fi
}

cleanup_on_exit() {
  local exit_code="$?"
  trap - EXIT
  stop_processes
  if [[ "$exit_code" -ne 0 && -n "$evidence_dir" && "$evidence_dir" == "$repo_root/.local/smoke/"* ]]; then
    rm -rf "$evidence_dir"
  fi
  exit "$exit_code"
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
  local curl_config="$4"
  local request_pids=()
  local index=""
  mkdir -p "$batch_dir"
  for index in $(seq 1 100); do
    curl_with_body_stdin "$payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
      --config "$curl_config" \
      -H 'Content-Type: application/json' \
      -H "Idempotency-Key: $idempotency_key" \
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
  if rg_with_pattern_stdin literal "$value" "$evidence_scan_root"; then
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
  if rg_with_pattern_stdin regex "$pattern" "$evidence_scan_root"; then
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

response_header_value() {
  local headers_file="$1"
  local header_name="$2"
  tr -d '\r' <"$headers_file" | awk -F ': *' -v name="$header_name" '
    tolower($1) == tolower(name) { print substr($0, index($0, ":") + 2); exit }
  '
}

build_w1_skill_payload() {
  local name="$1"
  local suffix="$2"
  local public_tool_refs="${3:-[]}"
  jq -cn --arg name "$name" --arg suffix "$suffix" --argjson public_tool_refs "$public_tool_refs" '
    {definition:{
      schema_version:"skill_definition.v1",
      name:$name,
      summary:("真实 W1 Skill Foundation 冒烟摘要 " + $suffix),
      category:"smoke",
      tags:["beta","alpha","alpha"],
      input_description:"输入真实业务目标与素材约束",
      output_description:"输出可审核的结构化创作方案",
      invocation_rules:"仅在用户明确需要完整创作规划时调用",
      plan_creation_spec:{applicability:"enabled",guidance:"先确认目标，再形成可执行创作规格",not_applicable_reason:""},
      analyze_materials:{applicability:"enabled",guidance:"只分析用户已授权的真实素材",not_applicable_reason:""},
      plan_storyboard:{applicability:"enabled",guidance:"按镜头目标组织故事板和依赖",not_applicable_reason:""},
      generate_media:{applicability:"enabled",guidance:"生成前确认规格、范围与资源引用",not_applicable_reason:""},
      write_prompts:{applicability:"enabled",guidance:"生成可追踪且与目标一致的提示词",not_applicable_reason:""},
      assemble_output:{applicability:"enabled",guidance:"按审核通过的时间线组织最终输出",not_applicable_reason:""},
      examples:[
        {input:"制作品牌介绍短片",output:"输出结构化短片创作方案"},
        {input:"分析已有素材",output:"输出带来源约束的素材分析"}
      ],
      starter_prompts:["分析这批素材","帮我规划介绍视频","分析这批素材"],
      market_listing:{cover_asset_id:null,detail:"用于 W1 真实链路验收",copyright_notice:"仅用于本地冒烟",user_notice:"不得用于生产内容"},
      public_tool_refs:$public_tool_refs
    }}'
}

run_w1_skill_smoke() {
  local postgres_container="$1"
  local response_file="$w1_temp_dir/response.json"
  local headers_file="$w1_temp_dir/headers.txt"
  local create_key="w1-skill-create-${run_id}"
  local review_key="w1-skill-review-${run_id}"
  local create_payload=""
  local conflict_payload=""
  local missing_array_payload=""
  local null_array_payload=""
  local cover_asset_payload=""
  local updated_payload=""
  local tool_payload=""
  local status=""
  local initial_etag=""
  local updated_etag=""
  local response_etag=""
  local replay_skill_id=""
  local replay_review_id=""
  local database_assertion=""
  local create_first_status=""
  local create_replay_status=""
  local create_conflict_status=""
  local create_conflict_code=""
  local create_response_etag=""
  local missing_array_status=""
  local missing_array_code=""
  local null_array_status=""
  local null_array_code=""
  local cover_asset_status=""
  local cover_asset_code=""
  local owner_list_status=""
  local owner_detail_status=""
  local update_status=""
  local update_response_etag=""
  local stale_update_status=""
  local stale_update_code=""
  local public_tool_status=""
  local public_tool_code=""
  local review_first_status=""
  local review_replay_status=""

  w1_skill_name="W1 Skill API smoke ${run_id}"
  w1_updated_skill_name="W1 Skill API smoke updated ${run_id}"
  create_payload="$(build_w1_skill_payload "$w1_skill_name" "create")"

  : >"$headers_file"
  status="$(curl_with_body_stdin "$create_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: $create_key" \
    -D "$headers_file" -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/skills')"
  create_first_status="$status"
  [[ "$status" == "201" ]] || fail "W1 Skill 首次创建状态为 $status"
  w1_skill_id="$(jq -er '.skill.skill_id | strings | select(test("^[0-9a-f-]{36}$"))' "$response_file")"
  initial_etag="$(jq -er '.skill.draft_etag | strings | select(test("^\\\"[^\\\"]+\\\"$"))' "$response_file")"
  response_etag="$(response_header_value "$headers_file" 'ETag')"
  create_response_etag="$response_etag"
  [[ "$response_etag" == "$initial_etag" ]] || fail "W1 Skill 创建响应头与 draft_etag 不一致"
  jq -e --arg id "$w1_skill_id" --arg name "$w1_skill_name" '
    .skill.skill_id == $id
    and .skill.definition.name == $name
    and .skill.definition.tags == ["alpha","beta"]
    and .skill.definition.starter_prompts == ["分析这批素材","帮我规划介绍视频"]
    and .skill.definition.public_tool_refs == []
    and .skill.content_status == "draft"
    and .skill.has_unpublished_changes == true
    and .skill.review_status == null
    and .skill.allowed_actions == ["edit_draft","submit_review"]' \
    "$response_file" >/dev/null || fail "W1 Skill 创建投影或 Canonical 规范化结果漂移"

  status="$(curl_with_body_stdin "$create_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: $create_key" \
    -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/skills')"
  create_replay_status="$status"
  [[ "$status" == "200" ]] || fail "W1 Skill 同键同义重放状态为 $status"
  replay_skill_id="$(jq -er '.skill.skill_id' "$response_file")"
  [[ "$replay_skill_id" == "$w1_skill_id" && "$(jq -er '.skill.draft_etag' "$response_file")" == "$initial_etag" ]] || \
    fail "W1 Skill 同义重放未返回首次冻结结果"

  conflict_payload="$(build_w1_skill_payload "W1 Skill conflicting ${run_id}" "conflict")"
  status="$(curl_with_body_stdin "$conflict_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: $create_key" \
    -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/skills')"
  create_conflict_status="$status"
  [[ "$status" == "409" ]] || fail "W1 Skill 同键异义状态为 $status"
  create_conflict_code="$(jq -er '.error.code' "$response_file")"
  jq -e '.error.code == "IDEMPOTENCY_CONFLICT"' "$response_file" >/dev/null || fail "W1 Skill 同键异义错误码漂移"

  missing_array_payload="$(jq -c 'del(.definition.tags)' <<<"$create_payload")"
  status="$(curl_with_body_stdin "$missing_array_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: w1-shape-missing-${run_id}" \
    -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/skills')"
  missing_array_status="$status"
  [[ "$status" == "400" ]] || fail "W1 Skill 缺失数组字段未失败关闭，状态为 $status"
  missing_array_code="$(jq -er '.error.code' "$response_file")"
  jq -e '
    .error.code == "SKILL_INVALID_DEFINITION"
    and any(.error.details.field_errors[]; .field == "definition.tags" and .code == "REQUIRED")' \
    "$response_file" >/dev/null || fail "W1 Skill 缺失数组字段错误契约漂移"

  null_array_payload="$(jq -c '.definition.examples = null' <<<"$create_payload")"
  status="$(curl_with_body_stdin "$null_array_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: w1-shape-null-${run_id}" \
    -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/skills')"
  null_array_status="$status"
  [[ "$status" == "400" ]] || fail "W1 Skill null 数组字段未失败关闭，状态为 $status"
  null_array_code="$(jq -er '.error.code' "$response_file")"
  jq -e '
    .error.code == "SKILL_INVALID_DEFINITION"
    and any(.error.details.field_errors[]; .field == "definition.examples" and .code == "REQUIRED")' \
    "$response_file" >/dev/null || fail "W1 Skill null 数组字段错误契约漂移"

  cover_asset_payload="$(jq -c '.definition.market_listing.cover_asset_id = "019f0000-0000-7000-8000-000000000099"' <<<"$create_payload")"
  status="$(curl_with_body_stdin "$cover_asset_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: w1-cover-unavailable-${run_id}" \
    -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/skills')"
  cover_asset_status="$status"
  [[ "$status" == "400" ]] || fail "W1 Skill 非 null cover_asset_id 未失败关闭，状态为 $status"
  cover_asset_code="$(jq -er '.error.code' "$response_file")"
  jq -e '
    .error.code == "SKILL_INVALID_DEFINITION"
    and any(.error.details.field_errors[]; .field == "definition.market_listing.cover_asset_id" and .code == "ASSET_REFERENCE_UNAVAILABLE")' \
    "$response_file" >/dev/null || fail "W1 Skill cover_asset_id null-only 错误契约漂移"

  status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
    -o "$response_file" -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/skills?scope=mine')"
  owner_list_status="$status"
  [[ "$status" == "200" ]] || fail "W1 Skill Owner 列表状态为 $status"
  jq -e --arg id "$w1_skill_id" 'any(.items[]; .skill_id == $id)' "$response_file" >/dev/null || \
    fail "W1 Skill Owner 列表未包含新建 Skill"

  status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
    -o "$response_file" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}")"
  owner_detail_status="$status"
  [[ "$status" == "200" ]] || fail "W1 Skill Owner 详情状态为 $status"
  jq -e --arg id "$w1_skill_id" --arg name "$w1_skill_name" \
    '.skill.skill_id == $id and .skill.definition.name == $name' "$response_file" >/dev/null || \
    fail "W1 Skill Owner 详情事实漂移"

  updated_payload="$(build_w1_skill_payload "$w1_updated_skill_name" "updated")"
  : >"$headers_file"
  status="$(curl_with_body_stdin "$updated_payload" --silent --show-error --max-time 10 -b "$cookie_jar" -X PUT \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "If-Match: $initial_etag" \
    -D "$headers_file" -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}/draft")"
  update_status="$status"
  [[ "$status" == "200" ]] || fail "W1 Skill If-Match 更新状态为 $status"
  updated_etag="$(jq -er '.skill.draft_etag | strings | select(test("^\\\"[^\\\"]+\\\"$"))' "$response_file")"
  [[ "$updated_etag" != "$initial_etag" ]] || fail "W1 Skill 更新后 draft_etag 未变化"
  response_etag="$(response_header_value "$headers_file" 'ETag')"
  update_response_etag="$response_etag"
  [[ "$response_etag" == "$updated_etag" ]] || fail "W1 Skill 更新响应头与 draft_etag 不一致"
  jq -e --arg id "$w1_skill_id" --arg name "$w1_updated_skill_name" \
    '.skill.skill_id == $id and .skill.definition.name == $name and .skill.allowed_actions == ["edit_draft","submit_review"]' \
    "$response_file" >/dev/null || fail "W1 Skill 更新后 Owner 投影漂移"

  status="$(curl_with_body_stdin "$updated_payload" --silent --show-error --max-time 10 -b "$cookie_jar" -X PUT \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "If-Match: $initial_etag" \
    -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}/draft")"
  stale_update_status="$status"
  [[ "$status" == "409" ]] || fail "W1 Skill 过期 ETag 更新状态为 $status"
  stale_update_code="$(jq -er '.error.code' "$response_file")"
  jq -e '.error.code == "SKILL_DRAFT_CONFLICT"' "$response_file" >/dev/null || fail "W1 Skill 过期 ETag 错误码漂移"

  tool_payload="$(jq -c '.definition.public_tool_refs = [{"tool_key":"unavailable"}]' <<<"$updated_payload")"
  status="$(curl_with_body_stdin "$tool_payload" --silent --show-error --max-time 10 -b "$cookie_jar" -X PUT \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "If-Match: $updated_etag" \
    -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}/draft")"
  public_tool_status="$status"
  [[ "$status" == "400" ]] || fail "W1 Skill 非空 public_tool_refs 未失败关闭，状态为 $status"
  public_tool_code="$(jq -er '.error.code' "$response_file")"
  jq -e '
    .error.code == "SKILL_TOOL_REFERENCE_UNAVAILABLE"
    and any(.error.details.field_errors[]; .field == "definition.public_tool_refs" and .code == "SKILL_TOOL_REFERENCE_UNAVAILABLE")' \
    "$response_file" >/dev/null || fail "W1 Skill public_tool_refs 失败关闭错误契约漂移"

  status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" -X POST \
    --config "$user_curl_config" -H "Idempotency-Key: $review_key" -H "If-Match: $updated_etag" \
    -o "$response_file" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}/reviews")"
  review_first_status="$status"
  [[ "$status" == "201" ]] || fail "W1 Skill 首次提交审核状态为 $status"
  w1_review_id="$(jq -er '.review_id | strings | select(test("^[0-9a-f-]{36}$"))' "$response_file")"
  jq -e --arg id "$w1_skill_id" '
    .skill.skill_id == $id
    and .skill.review_status == "reviewing"
    and .skill.allowed_actions == ["edit_draft"]' "$response_file" >/dev/null || \
    fail "W1 Skill 提交审核后 Owner 投影漂移"

  status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" -X POST \
    --config "$user_curl_config" -H "Idempotency-Key: $review_key" -H "If-Match: $updated_etag" \
    -o "$response_file" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}/reviews")"
  review_replay_status="$status"
  [[ "$status" == "200" ]] || fail "W1 Skill 提交审核同义重放状态为 $status"
  replay_review_id="$(jq -er '.review_id' "$response_file")"
  [[ "$replay_review_id" == "$w1_review_id" ]] || fail "W1 Skill 提交审核重放产生不同 Review"

  database_assertion="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'owner_matches', EXISTS (
        SELECT 1 FROM business.skill
        WHERE id = '$w1_skill_id'::uuid AND owner_user_id = '$user_id'::uuid
      ),
      'revision_count', (SELECT COUNT(*) FROM business.skill_content_revision WHERE skill_id = '$w1_skill_id'::uuid),
      'review_count', (SELECT COUNT(*) FROM business.skill_review_submission WHERE skill_id = '$w1_skill_id'::uuid),
      'reviewing_count', (SELECT COUNT(*) FROM business.skill_review_submission WHERE skill_id = '$w1_skill_id'::uuid AND status = 'reviewing'),
      'receipt_count', (SELECT COUNT(*) FROM business.skill_command_receipt WHERE result_skill_id = '$w1_skill_id'::uuid),
      'published_count', (SELECT COUNT(*) FROM business.skill_published_snapshot WHERE skill_id = '$w1_skill_id'::uuid),
      'audit_count', (SELECT COUNT(*) FROM business.skill_governance_audit WHERE skill_id = '$w1_skill_id'::uuid),
      'same_name_skill_count', (
        SELECT COUNT(DISTINCT revision.skill_id)
        FROM business.skill_content_revision AS revision
        WHERE revision.revision_no = 1 AND revision.definition_json->>'name' = '$w1_skill_name'
      ),
      'draft_pointer_matches', EXISTS (
        SELECT 1
        FROM business.skill AS skill_record
        JOIN business.skill_content_revision AS revision
          ON revision.id = skill_record.current_draft_revision_id AND revision.skill_id = skill_record.id
        WHERE skill_record.id = '$w1_skill_id'::uuid AND revision.revision_no = 2
      ),
      'review_revision_matches', EXISTS (
        SELECT 1
        FROM business.skill_review_submission AS review
        JOIN business.skill_content_revision AS revision
          ON revision.id = review.content_revision_id AND revision.skill_id = review.skill_id
        WHERE review.id = '$w1_review_id'::uuid AND review.skill_id = '$w1_skill_id'::uuid AND revision.revision_no = 2
      ),
      'physical_fk_count', (
        SELECT COUNT(*)
        FROM pg_constraint AS constraint_record
        JOIN pg_namespace AS namespace ON namespace.oid = constraint_record.connamespace
        WHERE namespace.nspname = 'business' AND constraint_record.contype = 'f'
      )
    );")"
  jq -e '
    .owner_matches
    and .revision_count == 2
    and .review_count == 1
    and .reviewing_count == 1
    and .receipt_count == 2
    and .published_count == 0
    and .audit_count == 0
    and .same_name_skill_count == 1
    and .draft_pointer_matches
    and .review_revision_matches
    and .physical_fk_count == 0' <<<"$database_assertion" >/dev/null || \
    fail "W1 Skill 数据库 Revision/Review/Receipt 或无物理外键断言失败"

  printf '%s\n' "$database_assertion" >"$evidence_dir/responses/w1-skill-database.json"
  jq -n --arg skill_id "$w1_skill_id" --arg review_id "$w1_review_id" \
    --arg replay_skill_id "$replay_skill_id" --arg replay_review_id "$replay_review_id" \
    --arg initial_etag "$initial_etag" --arg updated_etag "$updated_etag" \
    --arg create_response_etag "$create_response_etag" --arg update_response_etag "$update_response_etag" \
    --arg create_conflict_code "$create_conflict_code" --arg missing_array_code "$missing_array_code" \
    --arg null_array_code "$null_array_code" --arg cover_asset_code "$cover_asset_code" \
    --arg stale_update_code "$stale_update_code" --arg public_tool_code "$public_tool_code" \
    --argjson create_first_status "$create_first_status" --argjson create_replay_status "$create_replay_status" \
    --argjson create_conflict_status "$create_conflict_status" --argjson missing_array_status "$missing_array_status" \
    --argjson null_array_status "$null_array_status" --argjson cover_asset_status "$cover_asset_status" \
    --argjson owner_list_status "$owner_list_status" --argjson owner_detail_status "$owner_detail_status" \
    --argjson update_status "$update_status" --argjson stale_update_status "$stale_update_status" \
    --argjson public_tool_status "$public_tool_status" --argjson review_first_status "$review_first_status" \
    --argjson review_replay_status "$review_replay_status" --argjson database_fact "$database_assertion" '
    {skill_id:$skill_id,review_id:$review_id,
     create:{first_status:$create_first_status,replay_status:$create_replay_status,conflict_status:$create_conflict_status,
       conflict_code:$create_conflict_code,response_etag_matches:($create_response_etag == $initial_etag),
       replay_result_matches:($replay_skill_id == $skill_id)},
     strict_shape:{missing_array_status:$missing_array_status,null_array_status:$null_array_status,
       cover_asset_non_null_status:$cover_asset_status,
       failed_closed_without_side_effects:($missing_array_code == "SKILL_INVALID_DEFINITION"
         and $null_array_code == "SKILL_INVALID_DEFINITION" and $cover_asset_code == "SKILL_INVALID_DEFINITION"
         and $database_fact.revision_count == 2 and $database_fact.same_name_skill_count == 1)},
     owner_read:{list_status:$owner_list_status,detail_status:$owner_detail_status},
     update:{status:$update_status,stale_status:$stale_update_status,stale_code:$stale_update_code,
       response_etag_matches:($update_response_etag == $updated_etag),etag_changed:($initial_etag != $updated_etag)},
     public_tool_refs:{status:$public_tool_status,code:$public_tool_code,
       failed_closed:($public_tool_status == 400 and $database_fact.revision_count == 2)},
     review:{first_status:$review_first_status,replay_status:$review_replay_status,
       if_match:($database_fact.review_revision_matches == true),frozen_result:($replay_review_id == $review_id)}}' \
    >"$evidence_dir/responses/w1-skill-api.json"

  local review_queue="$w1_temp_dir/review-queue.json"
  local review_detail="$w1_temp_dir/review-detail.json"
  local review_detail_headers="$w1_temp_dir/review-detail-headers.txt"
  local publish_first="$w1_temp_dir/publish-first.json"
  local publish_replay="$w1_temp_dir/publish-replay.json"
  local review_etag=""
  local decision_key="w1-skill-approve-${run_id}"
  local first_request_id=""
  local first_snapshot_id=""
  local first_decided_at=""
  local queue_status=""
  local detail_status=""
  local decision_status=""
  local decision_replay_status=""
  local detail_header_etag=""

  status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    -o "$review_queue" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/admin/skill-reviews?status=reviewing')"
  queue_status="$status"
  [[ "$status" == "200" ]] || fail "W1 Reviewer 待审队列状态为 $status"
  jq -e --arg review "$w1_review_id" --arg skill "$w1_skill_id" '
    any(.items[];
      .review_id == $review and .skill_id == $skill and .status == "reviewing"
      and .allowed_actions == ["approve_and_publish"])
    and (.next_cursor == null or (.next_cursor | type) == "string")' \
    "$review_queue" >/dev/null || fail "W1 Reviewer 待审队列未返回冻结审核项"

  : >"$review_detail_headers"
  status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    -D "$review_detail_headers" -o "$review_detail" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/admin/skill-reviews/${w1_review_id}")"
  detail_status="$status"
  [[ "$status" == "200" ]] || fail "W1 Reviewer 冻结详情状态为 $status"
  review_etag="$(jq -er '.review.review_etag | strings | select(test("^\\\"[^\\\"]+\\\"$"))' "$review_detail")"
  detail_header_etag="$(response_header_value "$review_detail_headers" 'ETag')"
  [[ "$detail_header_etag" == "$review_etag" ]] || \
    fail "W1 Reviewer Detail Header/Body ETag 不一致"
  jq -e --arg review "$w1_review_id" --arg skill "$w1_skill_id" --arg owner "$user_id" --arg name "$w1_updated_skill_name" '
    .review.review_id == $review and .review.skill_id == $skill and .review.owner_user_id == $owner
    and .review.status == "reviewing" and .review.definition.name == $name
    and .review.current_published == null
    and .review.comparison == {has_current_published:false,same_content:false}
    and .review.allowed_actions == ["approve_and_publish"]' \
    "$review_detail" >/dev/null || fail "W1 Reviewer 详情未使用提交时冻结 Definition"

  status="$(curl_with_body_stdin '{"decision":"approved"}' --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" -X POST \
    --config "$owner_b_curl_config" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: $decision_key" -H "If-Match: $review_etag" \
    -o "$publish_first" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/admin/skill-reviews/${w1_review_id}/decisions")"
  decision_status="$status"
  [[ "$status" == "200" ]] || fail "W1 Reviewer 首次批准状态为 $status"
  first_request_id="$(jq -er '.request_id | strings | select(test("^[0-9a-f-]{36}$"))' "$publish_first")"
  first_snapshot_id="$(jq -er '.review.published_snapshot_id | strings | select(test("^[0-9a-f-]{36}$"))' "$publish_first")"
  first_decided_at="$(jq -er '.review.decided_at | strings | select(length > 0)' "$publish_first")"
  jq -e --arg review "$w1_review_id" --arg skill "$w1_skill_id" '
    .review.review_id == $review and .review.skill_id == $skill
    and .review.status == "approved" and .review.allowed_actions == []' \
    "$publish_first" >/dev/null || fail "W1 Reviewer 首次批准结果漂移"

  status="$(curl_with_body_stdin '{"decision":"approved"}' --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" -X POST \
    --config "$owner_b_curl_config" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: $decision_key" -H "If-Match: $review_etag" \
    -o "$publish_replay" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/admin/skill-reviews/${w1_review_id}/decisions")"
  decision_replay_status="$status"
  [[ "$status" == "200" ]] || fail "W1 Reviewer 批准同义重放状态为 $status"
  jq -e --arg review "$w1_review_id" --arg skill "$w1_skill_id" --arg snapshot "$first_snapshot_id" --arg decided "$first_decided_at" --arg first_request "$first_request_id" '
    .review.review_id == $review and .review.skill_id == $skill and .review.status == "approved"
    and .review.published_snapshot_id == $snapshot and .review.decided_at == $decided
    and .review.allowed_actions == [] and .request_id != $first_request' \
    "$publish_replay" >/dev/null || fail "W1 Reviewer 批准重放未返回首次冻结业务结果"
  jq -n --arg review_id "$w1_review_id" --arg skill_id "$w1_skill_id" --arg owner_id "$user_id" \
    --arg snapshot_id "$first_snapshot_id" --arg expected_name "$w1_updated_skill_name" \
    --arg review_etag "$review_etag" --arg detail_header_etag "$detail_header_etag" \
    --arg first_request_id "$first_request_id" --arg first_decided_at "$first_decided_at" \
    --argjson queue_status "$queue_status" --argjson detail_status "$detail_status" \
    --argjson decision_status "$decision_status" --argjson replay_status "$decision_replay_status" \
    --slurpfile queue "$review_queue" --slurpfile detail "$review_detail" \
    --slurpfile first "$publish_first" --slurpfile replay "$publish_replay" '
    {review_id:$review_id,skill_id:$skill_id,published_snapshot_id:$snapshot_id,
      queue_status:$queue_status,detail_status:$detail_status,decision_status:$decision_status,replay_status:$replay_status,
      reviewer_rbac:($queue_status == 200 and any($queue[0].items[];
        .review_id == $review_id and .skill_id == $skill_id and .status == "reviewing"
        and .allowed_actions == ["approve_and_publish"])),
      strong_etag:($detail_status == 200 and $review_etag == $detail_header_etag),
      frozen_definition:($detail[0].review.review_id == $review_id and $detail[0].review.skill_id == $skill_id
        and $detail[0].review.owner_user_id == $owner_id and $detail[0].review.definition.name == $expected_name),
      idempotent_business_result:($decision_status == 200 and $replay_status == 200
        and $first[0].review.published_snapshot_id == $snapshot_id
        and $replay[0].review.published_snapshot_id == $snapshot_id
        and $first[0].review.decided_at == $first_decided_at
        and $replay[0].review.decided_at == $first_decided_at
        and $first[0].request_id == $first_request_id and $replay[0].request_id != $first_request_id)}' \
    >"$evidence_dir/responses/w1-skill-publish.json"

  database_assertion="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'approved_review_count', (
        SELECT COUNT(*) FROM business.skill_review_submission
        WHERE id = '$w1_review_id'::uuid AND status = 'approved'
          AND submitted_by_user_id = '$user_id'::uuid
          AND decided_by_user_id = '$owner_b_seed_user_id'::uuid
          AND submitted_by_user_id <> decided_by_user_id
      ),
      'published_count', (
        SELECT COUNT(*) FROM business.skill_published_snapshot
        WHERE skill_id = '$w1_skill_id'::uuid AND published_by_user_id = '$owner_b_seed_user_id'::uuid
      ),
      'published_pointer_matches', EXISTS (
        SELECT 1 FROM business.skill AS skill_record
        JOIN business.skill_published_snapshot AS published ON published.id = skill_record.current_published_snapshot_id
        WHERE skill_record.id = '$w1_skill_id'::uuid AND skill_record.publication_revision = 1
          AND published.source_content_revision_id = skill_record.current_draft_revision_id
      ),
      'approval_receipt_count', (
        SELECT COUNT(*) FROM business.skill_command_receipt
        WHERE result_skill_id = '$w1_skill_id'::uuid AND command_type = 'approve_and_publish'
          AND actor_user_id = '$owner_b_seed_user_id'::uuid
          AND scope_id = '$w1_review_id'::uuid
          AND request_id = '$first_request_id'::uuid
      ),
      'governance_audit_count', (
        SELECT COUNT(*) FROM business.skill_governance_audit
        WHERE skill_id = '$w1_skill_id'::uuid AND action = 'review_approved_and_published'
          AND actor_user_id = '$owner_b_seed_user_id'::uuid
          AND request_id = '$first_request_id'::uuid
      ),
      'receipt_audit_request_id_matches', EXISTS (
        SELECT 1
        FROM business.skill_command_receipt AS receipt
        JOIN business.skill_governance_audit AS audit
          ON audit.skill_id = receipt.result_skill_id
         AND audit.review_submission_id = receipt.scope_id
         AND audit.actor_user_id = receipt.actor_user_id
         AND audit.request_id = receipt.request_id
        WHERE receipt.command_type = 'approve_and_publish'
          AND receipt.actor_user_id = '$owner_b_seed_user_id'::uuid
          AND receipt.scope_id = '$w1_review_id'::uuid
          AND receipt.request_id = '$first_request_id'::uuid
      )
    );")"
  jq -e '
    .approved_review_count == 1
    and .published_count == 1
    and .published_pointer_matches
    and .approval_receipt_count == 1
    and .governance_audit_count == 1
    and .receipt_audit_request_id_matches' <<<"$database_assertion" >/dev/null || \
    fail "W1 Skill 发布指针、审核、回执或治理审计断言失败"
  printf '%s\n' "$database_assertion" >"$evidence_dir/responses/w1-skill-publish-database.json"
  w1_reviewer_rbac_smoke_ran=true
}

run_w1_skill_binding_smoke() {
  local postgres_container="$1"
  local intent_key="w1-binding-${run_id}"
  local batch_dir="$evidence_dir/responses/w1-binding-batch"
  local payload=""
  local conflict_payload=""
  local replay_status=""
  local conflict_status=""
  local batch_request_count=""
  local batch_success_count=""
  local batch_created_count=""
  local business_assertion=""
  local agent_assertion=""
  local verifier_file="$evidence_dir/responses/w1-binding-agent-verified.json"
  local consistency_file="$evidence_dir/responses/w1-binding-consistency.json"

  w1_binding_prompt="W1 Skill Snapshot transport ${run_id}"
  payload="$(jq -cn --arg prompt "$w1_binding_prompt" --arg skill "$w1_skill_id" \
    '{schema_version:"project_quick_create.v2",initial_prompt:$prompt,enabled_skill_ids:[$skill]}')"
  w1_binding_project_id="$(run_concurrent_quick_create \
    "$intent_key" "$payload" "$batch_dir" "$user_curl_config")"
  [[ "$w1_binding_project_id" =~ ^[0-9a-f-]{36}$ ]] || fail "W1 Binding Project ID 格式无效"
  batch_request_count="$(find "$batch_dir" -type f -name '*.status' | wc -l | tr -d '[:space:]')"
  batch_success_count="$(awk '$1 == "200" || $1 == "201" { count++ } END { print count + 0 }' "$batch_dir"/*.status)"
  batch_created_count="$(awk '$1 == "201" { count++ } END { print count + 0 }' "$batch_dir"/*.status)"
  poll_bootstrap_ready "$w1_binding_project_id" "$evidence_dir/responses/w1-binding-bootstrap.json" || \
    fail "W1 非空 Skill Snapshot Project 未进入 ready"
  w1_binding_session_id="$(jq -er '.session_id | strings | select(length > 0)' "$evidence_dir/responses/w1-binding-bootstrap.json")"
  w1_binding_input_id="$(jq -er '.input_id | strings | select(length > 0)' "$evidence_dir/responses/w1-binding-bootstrap.json")"

  replay_status="$(curl_with_body_stdin "$payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: $intent_key" \
    -o "$evidence_dir/responses/w1-binding-replay.json" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/projects:quick-create')"
  [[ "$replay_status" == "200" ]] || fail "W1 Binding 同义重放状态为 $replay_status"
  jq -e --arg project "$w1_binding_project_id" --arg session "$w1_binding_session_id" --arg input "$w1_binding_input_id" \
    '.project_id == $project and .session_id == $session and .input_id == $input and .creation_status == "ready"' \
    "$evidence_dir/responses/w1-binding-replay.json" >/dev/null || fail "W1 Binding 同义重放未返回冻结结果"

  conflict_payload="$(jq -cn --arg prompt "$w1_binding_prompt" \
    '{schema_version:"project_quick_create.v2",initial_prompt:$prompt,enabled_skill_ids:[]}')"
  conflict_status="$(curl_with_body_stdin "$conflict_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: $intent_key" \
    -o "$evidence_dir/responses/w1-binding-conflict.json" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/projects:quick-create')"
  [[ "$conflict_status" == "409" ]] || fail "W1 Binding 同键异义状态为 $conflict_status"
  jq -e '.error.code == "IDEMPOTENCY_CONFLICT"' "$evidence_dir/responses/w1-binding-conflict.json" >/dev/null || \
    fail "W1 Binding 同键异义错误码漂移"
  jq -n --arg project "$w1_binding_project_id" --arg session "$w1_binding_session_id" --arg input "$w1_binding_input_id" \
    --argjson concurrent_requests "$batch_request_count" --argjson successful_requests "$batch_success_count" \
    --argjson created_requests "$batch_created_count" --argjson replay_status "$replay_status" \
    --argjson conflict_status "$conflict_status" --slurpfile replay "$evidence_dir/responses/w1-binding-replay.json" \
    --slurpfile conflict "$evidence_dir/responses/w1-binding-conflict.json" '
    {concurrent_requests:$concurrent_requests,successful_requests:$successful_requests,created_requests:$created_requests,
      replay_status:$replay_status,conflict_status:$conflict_status,
      replay_result_matches:($replay[0].project_id == $project and $replay[0].session_id == $session
        and $replay[0].input_id == $input and $replay[0].creation_status == "ready"),
      conflict_code:$conflict[0].error.code}' >"$evidence_dir/responses/w1-binding-api.json"
  jq -e '.concurrent_requests == 100 and .successful_requests == 100 and .created_requests == 1
    and .replay_status == 200 and .replay_result_matches
    and .conflict_status == 409 and .conflict_code == "IDEMPOTENCY_CONFLICT"' \
    "$evidence_dir/responses/w1-binding-api.json" >/dev/null || \
    fail "W1 Binding 并发、重放或异义派生证据不成立"

  business_assertion="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'request_schema_version', binding.request_schema_version,
      'outbox_schema_version', outbox.schema_version,
      'provisioning_status', binding.provisioning_status,
      'outbox_status', outbox.status,
      'creation_receipt_skill_count', receipt.skill_count,
      'binding_skill_count', binding.skill_count,
      'outbox_skill_count', outbox.skill_count,
      'resolution_skill_count', resolution.skill_count,
      'binding_count', (
        SELECT COUNT(*) FROM business.project_skill_binding
        WHERE project_id = project.id AND skill_id = '$w1_skill_id'::uuid AND status = 'enabled'
      ),
      'resolution_item_count', (
        SELECT COUNT(*) FROM business.project_session_skill_resolution_item
        WHERE resolution_id = outbox.resolution_id AND skill_id = '$w1_skill_id'::uuid
      ),
      'creation_receipt_snapshot_digest', encode(receipt.skill_snapshot_digest, 'hex'),
      'binding_snapshot_digest', encode(binding.skill_snapshot_digest, 'hex'),
      'outbox_snapshot_digest', encode(outbox.skill_snapshot_digest, 'hex'),
      'resolution_snapshot_digest', encode(resolution.snapshot_set_digest, 'hex'),
      'resolution_runtime_digest', (
        SELECT encode(item.runtime_content_digest, 'hex')
        FROM business.project_session_skill_resolution_item AS item
        WHERE item.resolution_id = resolution.id AND item.skill_id = '$w1_skill_id'::uuid
      ),
      'resolution_content_digest', (
        SELECT encode(item.content_digest, 'hex')
        FROM business.project_session_skill_resolution_item AS item
        WHERE item.resolution_id = resolution.id AND item.skill_id = '$w1_skill_id'::uuid
      ),
      'envelope_cleared', outbox.payload_encryption_algorithm IS NULL
        AND outbox.payload_key_version IS NULL
        AND outbox.payload_nonce IS NULL
        AND outbox.payload_ciphertext IS NULL
        AND outbox.payload_cleared_at IS NOT NULL
    )
    FROM business.project AS project
    JOIN business.project_creation_receipt AS receipt ON receipt.project_id = project.id
    JOIN business.project_session_binding AS binding ON binding.project_id = project.id
    JOIN business.project_session_outbox AS outbox ON outbox.id = binding.command_id
    JOIN business.project_session_skill_resolution AS resolution ON resolution.id = outbox.resolution_id
    WHERE project.id = '$w1_binding_project_id'::uuid;")"
  jq -e '
    .request_schema_version == "ensure_project_session.v2"
    and .outbox_schema_version == "session_bootstrap_outbox_payload.v2"
    and .provisioning_status == "ready"
    and .outbox_status == "delivered"
    and .creation_receipt_skill_count == 1
    and .binding_skill_count == 1
    and .outbox_skill_count == 1
    and .resolution_skill_count == 1
    and .binding_count == 1
    and .resolution_item_count == 1
    and (.creation_receipt_snapshot_digest | test("^[0-9a-f]{64}$"))
    and (.binding_snapshot_digest | test("^[0-9a-f]{64}$"))
    and (.outbox_snapshot_digest | test("^[0-9a-f]{64}$"))
    and (.resolution_snapshot_digest | test("^[0-9a-f]{64}$"))
    and (.resolution_runtime_digest | test("^[0-9a-f]{64}$"))
    and (.resolution_content_digest | test("^[0-9a-f]{64}$"))
    and .creation_receipt_snapshot_digest == .binding_snapshot_digest
    and .binding_snapshot_digest == .outbox_snapshot_digest
    and .outbox_snapshot_digest == .resolution_snapshot_digest
    and .envelope_cleared' <<<"$business_assertion" >/dev/null || \
    fail "W1 Binding Business 权威事实或密文清理断言失败"
  printf '%s\n' "$business_assertion" >"$evidence_dir/responses/w1-binding-business.json"

  agent_assertion="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent -Atc "
    SELECT json_build_object(
      'session_count', (SELECT COUNT(*) FROM agent.session WHERE id = '$w1_binding_session_id'::uuid AND project_id = '$w1_binding_project_id'::uuid),
      'new_snapshot_kind', (SELECT snapshot_kind FROM agent.session_skill_snapshot WHERE session_id = '$w1_binding_session_id'::uuid),
      'new_skill_count', (SELECT skill_count FROM agent.session_skill_snapshot WHERE session_id = '$w1_binding_session_id'::uuid),
      'new_snapshot_digest', (SELECT snapshot_digest FROM agent.session_skill_snapshot WHERE session_id = '$w1_binding_session_id'::uuid),
      'new_item_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_binding_session_id'::uuid),
      'new_runtime_digest', (
        SELECT runtime_content_digest FROM agent.session_skill_snapshot_item
        WHERE session_id = '$w1_binding_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid
      ),
      'new_content_digest', (
        SELECT content_digest FROM agent.session_skill_snapshot_item
        WHERE session_id = '$w1_binding_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid
      ),
      'new_skill_matches', EXISTS (
        SELECT 1 FROM agent.session_skill_snapshot_item
        WHERE session_id = '$w1_binding_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid
          AND runtime_content_schema_version = 'skill_runtime_content.v1'
          AND public_tool_refs = '[]'::jsonb
      ),
      'runtime_plaintext_absent', NOT EXISTS (
        SELECT 1 FROM agent.session_skill_snapshot_item
        WHERE session_id = '$w1_binding_session_id'::uuid
          AND position(convert_to('$w1_updated_skill_name', 'UTF8') IN runtime_content_ciphertext) > 0
      ),
      'receipt_count', (
        SELECT COUNT(*) FROM agent.session_command_receipt
        WHERE session_id = '$w1_binding_session_id'::uuid AND command_type = 'ensure_project_session_v2' AND skill_count = 1
      ),
      'receipt_skill_count', (
        SELECT skill_count FROM agent.session_command_receipt
        WHERE session_id = '$w1_binding_session_id'::uuid AND command_type = 'ensure_project_session_v2'
      ),
      'receipt_snapshot_digest', (
        SELECT skill_snapshot_digest FROM agent.session_command_receipt
        WHERE session_id = '$w1_binding_session_id'::uuid AND command_type = 'ensure_project_session_v2'
      ),
      'input_count', (SELECT COUNT(*) FROM agent.session_input WHERE session_id = '$w1_binding_session_id'::uuid AND id = '$w1_binding_input_id'::uuid),
      'old_snapshot_kind', (SELECT snapshot_kind FROM agent.session_skill_snapshot WHERE session_id = '$session_id'::uuid),
      'old_skill_count', (SELECT skill_count FROM agent.session_skill_snapshot WHERE session_id = '$session_id'::uuid),
      'old_item_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot_item WHERE session_id = '$session_id'::uuid)
    );")"
  jq -e '
    .session_count == 1
    and .new_snapshot_kind == "published_refs"
    and .new_skill_count == 1
    and .new_item_count == 1
    and (.new_snapshot_digest | test("^[0-9a-f]{64}$"))
    and (.new_runtime_digest | test("^[0-9a-f]{64}$"))
    and (.new_content_digest | test("^[0-9a-f]{64}$"))
    and .new_skill_matches
    and .runtime_plaintext_absent
    and .receipt_count == 1
    and .receipt_skill_count == 1
    and (.receipt_snapshot_digest | test("^[0-9a-f]{64}$"))
    and .receipt_snapshot_digest == .new_snapshot_digest
    and .input_count == 1
    and .old_snapshot_kind == "empty"
    and .old_skill_count == 0
    and .old_item_count == 0' <<<"$agent_assertion" >/dev/null || \
    fail "W1 Binding Agent Snapshot、密文或新旧 Session 隔离断言失败"
  printf '%s\n' "$agent_assertion" >"$evidence_dir/responses/w1-binding-agent.json"

  # 数据库形状不能代替完整性校验；调用仅 local 可编译的验证器，通过正式 Service Load 路径完成 AEAD 解密、Canonical 和摘要重算。
  if ! (
    cd "$repo_root/agent"
    DORA_SMOKE_AGENT_SESSION_ID="$w1_binding_session_id" GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-snapshot-verifier
  ) >"$verifier_file"; then
    fail "W1 Binding Agent Snapshot 正式 Load 路径校验失败"
  fi
  jq -e --arg session "$w1_binding_session_id" --arg skill "$w1_skill_id" '
    keys == ["session_id","skill_count","skills","snapshot_digest","status"]
    and .status == "verified"
    and .session_id == $session
    and .skill_count == 1
    and (.snapshot_digest | test("^[0-9a-f]{64}$"))
    and (.skills | length) == 1
    and (.skills[0] | keys) == ["content_digest","load_order","published_snapshot_id","runtime_content_digest","skill_id"]
    and .skills[0].load_order == 1
    and .skills[0].skill_id == $skill
    and (.skills[0].published_snapshot_id | test("^[0-9a-f-]{36}$"))
    and (.skills[0].runtime_content_digest | test("^[0-9a-f]{64}$"))
    and (.skills[0].content_digest | test("^[0-9a-f]{64}$"))' \
    "$verifier_file" >/dev/null || fail "W1 Binding Agent Snapshot 真实解密或 Canonical 校验失败"

  # 一致性 evidence 的布尔值必须由三份独立事实计算，不得用长度或预设 true 冒充跨模块一致。
  jq -n \
    --slurpfile business "$evidence_dir/responses/w1-binding-business.json" \
    --slurpfile agent "$evidence_dir/responses/w1-binding-agent.json" \
    --slurpfile verifier "$verifier_file" '
    $business[0] as $business_fact
    | $agent[0] as $agent_fact
    | $verifier[0] as $verified_fact
    | {
        skill_count:$verified_fact.skill_count,
        snapshot_digest_business_agent_verifier_consistent:(
          $business_fact.creation_receipt_snapshot_digest == $business_fact.binding_snapshot_digest
          and $business_fact.binding_snapshot_digest == $business_fact.outbox_snapshot_digest
          and $business_fact.outbox_snapshot_digest == $business_fact.resolution_snapshot_digest
          and $business_fact.resolution_snapshot_digest == $agent_fact.new_snapshot_digest
          and $agent_fact.new_snapshot_digest == $agent_fact.receipt_snapshot_digest
          and $agent_fact.receipt_snapshot_digest == $verified_fact.snapshot_digest
        ),
        runtime_content_digest_business_agent_verifier_consistent:(
          $business_fact.resolution_runtime_digest == $agent_fact.new_runtime_digest
          and $agent_fact.new_runtime_digest == $verified_fact.skills[0].runtime_content_digest
        ),
        content_digest_business_agent_verifier_consistent:(
          $business_fact.resolution_content_digest == $agent_fact.new_content_digest
          and $agent_fact.new_content_digest == $verified_fact.skills[0].content_digest
        ),
        skill_count_business_agent_verifier_consistent:(
          $business_fact.creation_receipt_skill_count == $business_fact.binding_skill_count
          and $business_fact.binding_skill_count == $business_fact.outbox_skill_count
          and $business_fact.outbox_skill_count == $business_fact.resolution_skill_count
          and $business_fact.resolution_skill_count == $business_fact.binding_count
          and $business_fact.binding_count == $business_fact.resolution_item_count
          and $business_fact.resolution_item_count == $agent_fact.new_skill_count
          and $agent_fact.new_skill_count == $agent_fact.new_item_count
          and $agent_fact.new_item_count == $agent_fact.receipt_skill_count
          and $agent_fact.receipt_skill_count == $verified_fact.skill_count
        ),
        business_v2_envelope_cleared:($business_fact.envelope_cleared == true),
        agent_v2_snapshot_encrypted_and_decryptable:(
          $agent_fact.runtime_plaintext_absent == true and $verified_fact.status == "verified"
        ),
        v1_v2_session_isolation:(
          $agent_fact.old_snapshot_kind == "empty"
          and $agent_fact.old_skill_count == 0
          and $agent_fact.old_item_count == 0
        )
      }' >"$consistency_file"
  jq -e '
    .skill_count == 1
    and .snapshot_digest_business_agent_verifier_consistent
    and .runtime_content_digest_business_agent_verifier_consistent
    and .content_digest_business_agent_verifier_consistent
    and .skill_count_business_agent_verifier_consistent
    and .business_v2_envelope_cleared
    and .agent_v2_snapshot_encrypted_and_decryptable
    and .v1_v2_session_isolation' "$consistency_file" >/dev/null || \
    fail "W1 Binding Business/Agent/解密校验器摘要或数量不一致"
  w1_skill_binding_smoke_ran=true
}

run_w1_browser_frozen_smoke() {
  local postgres_container="$1"
  local browser_result_file="$2"
  local owner_api_raw="$w1_temp_dir/browser-owner-detail.json"
  local review_api_raw="$w1_temp_dir/browser-review-detail.json"
  local bootstrap_file="$evidence_dir/responses/w1-browser-bootstrap.json"
  local verifier_file="$evidence_dir/responses/w1-browser-frozen-agent-verified.json"
  local browser_creator_id=""
  local browser_reviewer_id=""
  local browser_skill_id=""
  local browser_review_id=""
  local browser_snapshot_id=""
  local browser_project_id=""
  local browser_catalog_session_id=""
  local browser_catalog_request_id=""
  local browser_catalog_exact_unavailable=""
  local submitted_summary=""
  local current_draft_summary=""
  local submitted_summary_b64=""
  local current_draft_summary_b64=""
  local owner_api_status=""
  local review_api_status=""
  local browser_session_id=""
  local browser_input_id=""
  local business_fact=""
  local agent_fact=""

  [[ -s "$browser_result_file" ]] || fail "W1 浏览器未产出结构化真实结果"
  jq -e --arg creator "$user_id" --arg reviewer "$owner_b_seed_user_id" '
    keys == ["creator_id","current_draft_summary","project_id","published_snapshot_id","review_id","reviewer_id","schema_version","skill_id","submitted_summary","tool_catalog_exact_unavailable","tool_catalog_request_id","tool_catalog_session_id"]
    and .schema_version == "w1.real-review-result.v1"
    and .creator_id == $creator and .reviewer_id == $reviewer and .creator_id != .reviewer_id
    and ([.creator_id,.reviewer_id,.skill_id,.review_id,.published_snapshot_id,.project_id,.tool_catalog_session_id,.tool_catalog_request_id]
      | all(.[]; test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")))
    and .tool_catalog_exact_unavailable == true
    and (.submitted_summary | type) == "string" and (.submitted_summary | length) > 0
    and (.current_draft_summary | type) == "string" and (.current_draft_summary | length) > 0
    and .submitted_summary != .current_draft_summary' "$browser_result_file" >/dev/null || \
    fail "W1 浏览器结构化结果契约或 Creator/Reviewer 身份漂移"

  browser_creator_id="$(jq -er '.creator_id' "$browser_result_file")"
  browser_reviewer_id="$(jq -er '.reviewer_id' "$browser_result_file")"
  browser_skill_id="$(jq -er '.skill_id' "$browser_result_file")"
  browser_review_id="$(jq -er '.review_id' "$browser_result_file")"
  browser_snapshot_id="$(jq -er '.published_snapshot_id' "$browser_result_file")"
  browser_project_id="$(jq -er '.project_id' "$browser_result_file")"
  browser_catalog_session_id="$(jq -er '.tool_catalog_session_id' "$browser_result_file")"
  browser_catalog_request_id="$(jq -er '.tool_catalog_request_id' "$browser_result_file")"
  browser_catalog_exact_unavailable="$(jq -er '.tool_catalog_exact_unavailable' "$browser_result_file")"
  submitted_summary="$(jq -er '.submitted_summary' "$browser_result_file")"
  current_draft_summary="$(jq -er '.current_draft_summary' "$browser_result_file")"
  submitted_summary_b64="$(printf '%s' "$submitted_summary" | base64 | tr -d '\n')"
  current_draft_summary_b64="$(printf '%s' "$current_draft_summary" | base64 | tr -d '\n')"

  owner_api_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
    -o "$owner_api_raw" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/skills/${browser_skill_id}")"
  [[ "$owner_api_status" == "200" ]] || fail "W1 Browser Skill 正式 Owner API 状态为 $owner_api_status"
  review_api_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    -o "$review_api_raw" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/admin/skill-reviews/${browser_review_id}")"
  [[ "$review_api_status" == "200" ]] || fail "W1 Browser Review 正式 Reviewer API 状态为 $review_api_status"

  jq -n --argjson owner_status "$owner_api_status" --argjson review_status "$review_api_status" \
    --argjson tool_catalog_exact_unavailable "$browser_catalog_exact_unavailable" \
    --arg creator "$browser_creator_id" --arg skill "$browser_skill_id" --arg review "$browser_review_id" \
    --arg snapshot "$browser_snapshot_id" --arg submitted "$submitted_summary" --arg current "$current_draft_summary" \
    --arg tool_catalog_session_id "$browser_catalog_session_id" --arg tool_catalog_request_id "$browser_catalog_request_id" \
    --slurpfile owner "$owner_api_raw" --slurpfile detail "$review_api_raw" '
    {owner_status:$owner_status,review_status:$review_status,skill_id:$skill,review_id:$review,published_snapshot_id:$snapshot,
      tool_catalog_session_id:$tool_catalog_session_id,tool_catalog_request_id:$tool_catalog_request_id,
      tool_catalog_exact_unavailable:$tool_catalog_exact_unavailable,
      owner_current_draft_is_b:($owner[0].skill.skill_id == $skill
        and $owner[0].skill.definition.summary == $current
        and $owner[0].skill.content_status == "published"
        and $owner[0].skill.has_unpublished_changes == true
        and $owner[0].skill.review_status == "approved"),
      review_frozen_submission_is_a:($detail[0].review.review_id == $review
        and $detail[0].review.skill_id == $skill
        and $detail[0].review.owner_user_id == $creator
        and $detail[0].review.status == "approved"
        and $detail[0].review.definition.summary == $submitted
        and $detail[0].review.definition.summary != $current),
      review_current_published_is_a:($detail[0].review.current_published.published_snapshot_id == $snapshot
        and $detail[0].review.current_published.definition.summary == $submitted
        and $detail[0].review.current_published.definition.summary != $current
        and $detail[0].review.comparison == {has_current_published:true,same_content:true}
        and $detail[0].review.allowed_actions == [])}' \
    >"$evidence_dir/responses/w1-browser-frozen-api.json"
  jq -e '.owner_status == 200 and .review_status == 200 and .owner_current_draft_is_b
    and .review_frozen_submission_is_a and .review_current_published_is_a' \
    "$evidence_dir/responses/w1-browser-frozen-api.json" >/dev/null || \
    fail "W1 Browser 正式 API 未证明提交 A、当前草稿 B、发布 A"

  poll_bootstrap_ready "$browser_project_id" "$bootstrap_file" || fail "W1 Browser QuickCreate Project 未进入 ready"
  browser_session_id="$(jq -er '.session_id | strings | select(test("^[0-9a-f-]{36}$"))' "$bootstrap_file")"
  browser_input_id="$(jq -er '.input_id | strings | select(test("^[0-9a-f-]{36}$"))' "$bootstrap_file")"
  [[ "$browser_catalog_session_id" == "$browser_session_id" ]] || fail "W1 Browser Tool Catalog 未绑定 ready Session"

  business_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'review_fact_count', (SELECT COUNT(*) FROM business.skill_review_submission AS review
        WHERE review.id = '$browser_review_id'::uuid AND review.skill_id = '$browser_skill_id'::uuid
          AND review.status = 'approved' AND review.submitted_by_user_id = '$browser_creator_id'::uuid
          AND review.decided_by_user_id = '$browser_reviewer_id'::uuid),
      'published_fact_count', (SELECT COUNT(*) FROM business.skill_published_snapshot AS published
        WHERE published.id = '$browser_snapshot_id'::uuid AND published.skill_id = '$browser_skill_id'::uuid
          AND published.review_submission_id = '$browser_review_id'::uuid
          AND published.published_by_user_id = '$browser_reviewer_id'::uuid),
      'frozen_publication_matches', EXISTS (SELECT 1
        FROM business.skill AS skill_record
        JOIN business.skill_review_submission AS review ON review.id = '$browser_review_id'::uuid AND review.skill_id = skill_record.id
        JOIN business.skill_published_snapshot AS published ON published.id = skill_record.current_published_snapshot_id
        WHERE skill_record.id = '$browser_skill_id'::uuid AND published.id = '$browser_snapshot_id'::uuid
          AND published.source_content_revision_id = review.content_revision_id
          AND published.content_digest = review.content_digest
          AND skill_record.current_draft_revision_id <> review.content_revision_id),
      'submitted_summary_is_a', EXISTS (SELECT 1 FROM business.skill_review_submission AS review
        JOIN business.skill_content_revision AS revision ON revision.id = review.content_revision_id
        WHERE review.id = '$browser_review_id'::uuid
          AND revision.definition_json->>'summary' = convert_from(decode('$submitted_summary_b64','base64'),'UTF8')),
      'current_draft_summary_is_b', EXISTS (SELECT 1 FROM business.skill AS skill_record
        JOIN business.skill_content_revision AS revision ON revision.id = skill_record.current_draft_revision_id
        WHERE skill_record.id = '$browser_skill_id'::uuid
          AND revision.definition_json->>'summary' = convert_from(decode('$current_draft_summary_b64','base64'),'UTF8')),
      'published_summary_is_a', EXISTS (SELECT 1 FROM business.skill_published_snapshot AS published
        WHERE published.id = '$browser_snapshot_id'::uuid
          AND published.definition_json->>'summary' = convert_from(decode('$submitted_summary_b64','base64'),'UTF8')),
      'project_owner_matches', EXISTS (SELECT 1 FROM business.project
        WHERE id = '$browser_project_id'::uuid AND owner_user_id = '$browser_creator_id'::uuid),
      'session_binding_matches', EXISTS (SELECT 1 FROM business.project_session_binding
        WHERE project_id = '$browser_project_id'::uuid AND agent_session_id = '$browser_session_id'::uuid
          AND agent_input_id = '$browser_input_id'::uuid AND provisioning_status = 'ready'),
      'resolution_item_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution_item
        WHERE project_id = '$browser_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND published_snapshot_id = '$browser_snapshot_id'::uuid),
      'published_content_digest', (SELECT encode(content_digest,'hex') FROM business.skill_published_snapshot
        WHERE id = '$browser_snapshot_id'::uuid),
      'resolution_content_digest', (SELECT encode(content_digest,'hex') FROM business.project_session_skill_resolution_item
        WHERE project_id = '$browser_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid),
      'resolution_runtime_digest', (SELECT encode(runtime_content_digest,'hex') FROM business.project_session_skill_resolution_item
        WHERE project_id = '$browser_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid)
    );")"
  printf '%s\n' "$business_fact" >"$evidence_dir/responses/w1-browser-frozen-business.json"
  jq -e '.review_fact_count == 1 and .published_fact_count == 1 and .frozen_publication_matches
    and .submitted_summary_is_a and .current_draft_summary_is_b and .published_summary_is_a
    and .project_owner_matches and .session_binding_matches and .resolution_item_count == 1
    and (.published_content_digest | test("^[0-9a-f]{64}$"))
    and (.resolution_content_digest | test("^[0-9a-f]{64}$"))
    and (.resolution_runtime_digest | test("^[0-9a-f]{64}$"))' \
    "$evidence_dir/responses/w1-browser-frozen-business.json" >/dev/null || \
    fail "W1 Browser Business DB 未证明冻结 A/草稿 B 或 Project Snapshot"

  agent_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent -Atc "
    SELECT json_build_object(
      'session_count', (SELECT COUNT(*) FROM agent.session WHERE id = '$browser_session_id'::uuid
        AND project_id = '$browser_project_id'::uuid AND user_id = '$browser_creator_id'::uuid),
      'snapshot_kind', (SELECT snapshot_kind FROM agent.session_skill_snapshot WHERE session_id = '$browser_session_id'::uuid),
      'skill_count', (SELECT skill_count FROM agent.session_skill_snapshot WHERE session_id = '$browser_session_id'::uuid),
      'snapshot_digest', (SELECT snapshot_digest FROM agent.session_skill_snapshot WHERE session_id = '$browser_session_id'::uuid),
      'item_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot_item WHERE session_id = '$browser_session_id'::uuid),
      'item_matches', EXISTS (SELECT 1 FROM agent.session_skill_snapshot_item
        WHERE session_id = '$browser_session_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND published_snapshot_id = '$browser_snapshot_id'::uuid),
      'content_digest', (SELECT content_digest FROM agent.session_skill_snapshot_item
        WHERE session_id = '$browser_session_id'::uuid AND skill_id = '$browser_skill_id'::uuid),
      'runtime_content_digest', (SELECT runtime_content_digest FROM agent.session_skill_snapshot_item
        WHERE session_id = '$browser_session_id'::uuid AND skill_id = '$browser_skill_id'::uuid)
    );")"
  printf '%s\n' "$agent_fact" >"$evidence_dir/responses/w1-browser-frozen-agent.json"
  jq -e '.session_count == 1 and .snapshot_kind == "published_refs" and .skill_count == 1
    and .item_count == 1 and .item_matches and (.snapshot_digest | test("^[0-9a-f]{64}$"))
    and (.content_digest | test("^[0-9a-f]{64}$")) and (.runtime_content_digest | test("^[0-9a-f]{64}$"))' \
    "$evidence_dir/responses/w1-browser-frozen-agent.json" >/dev/null || \
    fail "W1 Browser Agent DB Snapshot 事实漂移"

  if ! (
    cd "$repo_root/agent"
    DORA_SMOKE_AGENT_SESSION_ID="$browser_session_id" GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-snapshot-verifier
  ) >"$verifier_file"; then
    fail "W1 Browser Agent Snapshot 正式 Load 路径校验失败"
  fi
  jq -e --arg session "$browser_session_id" --arg skill "$browser_skill_id" --arg snapshot "$browser_snapshot_id" '
    .status == "verified" and .session_id == $session and .skill_count == 1 and (.skills | length) == 1
    and .skills[0].skill_id == $skill and .skills[0].published_snapshot_id == $snapshot' \
    "$verifier_file" >/dev/null || fail "W1 Browser Agent Snapshot 解密验证结果漂移"

  jq -n --slurpfile api "$evidence_dir/responses/w1-browser-frozen-api.json" \
    --slurpfile business "$evidence_dir/responses/w1-browser-frozen-business.json" \
    --slurpfile agent "$evidence_dir/responses/w1-browser-frozen-agent.json" \
    --slurpfile verifier "$verifier_file" '
    {skill_id:$api[0].skill_id,review_id:$api[0].review_id,published_snapshot_id:$api[0].published_snapshot_id,
      browser_result_contract:($api[0].owner_status == 200 and $api[0].review_status == 200),
      formal_api_frozen_revision:($api[0].owner_current_draft_is_b
        and $api[0].review_frozen_submission_is_a and $api[0].review_current_published_is_a),
      business_frozen_revision:($business[0].review_fact_count == 1 and $business[0].published_fact_count == 1
        and $business[0].frozen_publication_matches and $business[0].submitted_summary_is_a
        and $business[0].current_draft_summary_is_b and $business[0].published_summary_is_a),
      agent_snapshot_matches_published:($business[0].project_owner_matches and $business[0].session_binding_matches
        and $business[0].resolution_item_count == 1 and $agent[0].session_count == 1
        and $agent[0].snapshot_kind == "published_refs" and $agent[0].skill_count == 1
        and $agent[0].item_count == 1 and $agent[0].item_matches and $verifier[0].status == "verified"
        and $verifier[0].skills[0].published_snapshot_id == $api[0].published_snapshot_id),
      digest_business_agent_verifier_consistent:($business[0].published_content_digest == $business[0].resolution_content_digest
        and $business[0].resolution_content_digest == $agent[0].content_digest
        and $agent[0].content_digest == $verifier[0].skills[0].content_digest
        and $business[0].resolution_runtime_digest == $agent[0].runtime_content_digest
        and $agent[0].runtime_content_digest == $verifier[0].skills[0].runtime_content_digest),
      browser_tool_catalog_static_unavailable:($api[0].tool_catalog_exact_unavailable
        and ($api[0].tool_catalog_session_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
        and ($api[0].tool_catalog_request_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")))}
    | .browser_review_publish_quickcreate_v2 = (.browser_result_contract and .formal_api_frozen_revision
      and .business_frozen_revision and .agent_snapshot_matches_published
      and .digest_business_agent_verifier_consistent and .browser_tool_catalog_static_unavailable)' \
    >"$evidence_dir/responses/w1-browser-frozen-consistency.json"
  jq -e '.browser_result_contract and .formal_api_frozen_revision and .business_frozen_revision
    and .agent_snapshot_matches_published and .digest_business_agent_verifier_consistent
    and .browser_tool_catalog_static_unavailable and .browser_review_publish_quickcreate_v2' \
    "$evidence_dir/responses/w1-browser-frozen-consistency.json" >/dev/null || \
    fail "W1 Browser API/Business/Agent/verifier 冻结事实不一致"
}

trap cleanup_on_exit EXIT
mkdir -p "$evidence_dir"
mkdir -p "$evidence_dir/responses"
if ! w1_smoke_mode_error="$(validate_w1_smoke_mode "$w1_skill_smoke_enabled" "$w1_browser_smoke_enabled")"; then
  fail "$w1_smoke_mode_error"
fi
if [[ "$w1_skill_smoke_enabled" == "1" && "${W0_RUN_BROWSER_SMOKE:-0}" == "1" ]]; then
  fail "W1 canonical 门禁不得叠加 W0_RUN_BROWSER_SMOKE；请分别运行 w05-browser-smoke 与 w1-browser-smoke"
fi
# 新运行开始即撤销旧的 canonical summary；失败运行不得让消费者继续读取上一次 passed。
rm -f "$evidence_file"
if [[ -n "$legacy_evidence_file" ]]; then
  rm -f "$legacy_evidence_file"
fi
cookie_jar="$(mktemp "${TMPDIR:-/tmp}/dora-w0-cookie.XXXXXX")"
user_curl_config="$(mktemp "${TMPDIR:-/tmp}/dora-w0-curl-config.XXXXXX")"
owner_b_cookie_jar="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-cookie.XXXXXX")"
owner_b_curl_config="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-curl-config.XXXXXX")"
login_response_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w0-login.XXXXXX")"
workspace_response_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-workspace.XXXXXX")"
owner_b_seed_response_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-seed.XXXXXX")"
owner_b_login_response_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-login.XXXXXX")"
owner_b_denied_response_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-denied.XXXXXX")"
owner_b_denied_headers_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-owner-b-denied-headers.XXXXXX")"
source_manifest_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-source-manifest.XXXXXX")"
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  w1_temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/dora-w1-skill.XXXXXX")"
  chmod 700 "$w1_temp_dir"
else
  w05_browser_result_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-browser-result.XXXXXX")"
  agent_restart_workspace_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-agent-restart-workspace.XXXXXX")"
  agent_restart_sse_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-agent-restart-sse.XXXXXX")"
  agent_restart_sse_status_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-agent-restart-sse-status.XXXXXX")"
fi
chmod 600 "$cookie_jar" "$user_curl_config" "$owner_b_cookie_jar" "$owner_b_curl_config" \
  "$login_response_temp" "$workspace_response_temp" \
  "$owner_b_seed_response_temp" "$owner_b_login_response_temp" "$owner_b_denied_response_temp" \
  "$owner_b_denied_headers_temp" "$source_manifest_temp"
if [[ "$w1_skill_smoke_enabled" != "1" ]]; then
  chmod 600 "$w05_browser_result_temp" "$agent_restart_workspace_temp" \
    "$agent_restart_sse_temp" "$agent_restart_sse_status_temp"
fi

set -a
. "$env_file"
set +a

if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  # W1 冒烟显式确认同一批本地 Agent binary 已支持 v2；模板和普通启动仍保持默认关闭。
  BUSINESS_PROJECT_SKILL_SNAPSHOT_V2_ENABLED=true
  BUSINESS_AGENT_SESSION_V2_CAPABILITY_CONFIRMED=true
  export BUSINESS_PROJECT_SKILL_SNAPSHOT_V2_ENABLED BUSINESS_AGENT_SESSION_V2_CAPABILITY_CONFIRMED
fi

[[ "${DORA_ENV:-}" == "local" ]] || fail "DORA_ENV 必须为 local"
[[ -x "$go_bin" ]] || fail "未找到固定 Go SDK"
[[ -x "$migrate_bin" ]] || fail "未找到 golang-migrate CLI"
command -v shasum >/dev/null 2>&1 || fail "未找到 shasum"
[[ -d "$repo_root/frontend/src" && -d "$repo_root/frontend/e2e" ]] || fail "前端源码或 E2E 目录缺失"

# 脚本可被直接运行，不能依赖 Makefile 前置任务留下的旧二进制；在计算指纹和启动 Runtime 前从当前 worktree 重新构建。
mkdir -p "$repo_root/.local/bin"
GOWORK=off "$go_bin" -C "$repo_root/business" build -o "$repo_root/.local/bin/business-service" ./cmd/business-service || \
  fail "从当前 worktree 构建 business-service 失败"
GOWORK=off "$go_bin" -C "$repo_root/agent" build -o "$repo_root/.local/bin/agent-service" ./cmd/agent-service || \
  fail "从当前 worktree 构建 agent-service 失败"
business_binary_sha256="$(sha256_file "$repo_root/.local/bin/business-service")" || fail "Business Runtime SHA-256 计算失败"
agent_binary_sha256="$(sha256_file "$repo_root/.local/bin/agent-service")" || fail "Agent Runtime SHA-256 计算失败"
while IFS= read -r source_file; do
  source_file_sha256="$(sha256_file "$repo_root/$source_file")" || fail "Source SHA-256 计算失败: $source_file"
  printf '%s  %s\n' "$source_file_sha256" "$source_file" >>"$source_manifest_temp"
done < <(
  cd "$repo_root"
  {
    find business agent -type f \( -name '*.go' -o -name '*.sql' -o -name '*.thrift' -o -name '*.proto' \) -print
    find frontend/src frontend/e2e frontend/scripts -type f -print
    find scripts -type f -print
    find frontend -maxdepth 1 -type f \( -name 'package.json' -o -name 'package-lock.json' -o -name 'npm-shrinkwrap.json' \) -print
    printf '%s\n' \
      'frontend/playwright.config.js' \
      'frontend/vite.config.js'
  } | LC_ALL=C sort -u
)
[[ -s "$source_manifest_temp" ]] || fail "Source SHA-256 manifest 为空"
source_digest_sha256="$(sha256_file "$source_manifest_temp")" || fail "Source digest SHA-256 计算失败"
rm -f "$source_manifest_temp"
source_manifest_temp=""
owner_b_email="owner-b.${DORA_SMOKE_USER_EMAIL}"
owner_b_password="owner-b-${DORA_SMOKE_USER_PASSWORD}"
owner_b_display_name="本地冒烟权限用户"
provisioner_email="provisioner.${DORA_SMOKE_USER_EMAIL}"
provisioner_password="provisioner-${DORA_SMOKE_USER_PASSWORD}"
provisioner_display_name="本地冒烟角色赋权人"

"${compose[@]}" up -d
ENV_FILE="$env_file" "$repo_root/scripts/wait-for-local-infra.sh"
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" business up
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" agent up
(
  cd "$repo_root/business"
  GOWORK=off "$go_bin" run ./cmd/local-smoke-seeder
)
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  (
    cd "$repo_root/business"
    DORA_SMOKE_REVIEWER_EMAIL="$owner_b_email" DORA_SMOKE_REVIEWER_PASSWORD="$owner_b_password" \
      DORA_SMOKE_REVIEWER_DISPLAY_NAME="$owner_b_display_name" \
      DORA_SMOKE_PROVISIONER_EMAIL="$provisioner_email" DORA_SMOKE_PROVISIONER_PASSWORD="$provisioner_password" \
      DORA_SMOKE_PROVISIONER_DISPLAY_NAME="$provisioner_display_name" \
      GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-reviewer-seeder
  ) >"$owner_b_seed_response_temp"
  reviewer_assignment_id="$(jq -er '.assignment_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
  owner_b_seed_user_id="$(jq -er '.reviewer_user_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
  reviewer_seed_creator_user_id="$(jq -er '.creator_user_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
  provisioner_user_id="$(jq -er '.provisioner_user_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
  jq -e '
    .role == "skill_reviewer" and .reason == "local_smoke_fixture"
    and .creator_user_id != .reviewer_user_id
    and .creator_user_id != .provisioner_user_id
    and .reviewer_user_id != .provisioner_user_id' "$owner_b_seed_response_temp" >/dev/null || \
    fail "Reviewer Seeder 未创建三身份隔离的正式角色分配"
else
  (
    cd "$repo_root/business"
    DORA_SMOKE_USER_EMAIL="$owner_b_email" DORA_SMOKE_USER_PASSWORD="$owner_b_password" \
      DORA_SMOKE_USER_DISPLAY_NAME="$owner_b_display_name" GOWORK=off "$go_bin" run ./cmd/local-smoke-seeder
  ) >"$owner_b_seed_response_temp"
  owner_b_seed_user_id="$(jq -er 'select(.status == "ready") | .user_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
fi
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

login_payload="$(build_login_json "$DORA_SMOKE_USER_EMAIL" "$DORA_SMOKE_USER_PASSWORD")" || fail "用户 A 登录请求构造失败"
login_status="$(curl_with_body_stdin "$login_payload" --silent --show-error --max-time 10 -c "$cookie_jar" \
  -H 'Content-Type: application/json' \
  -o "$login_response_temp" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/auth/session')"
unset login_payload
[[ "$login_status" == "200" ]] || fail "登录状态为 $login_status"
csrf_token="$(jq -er '.csrf_token | strings | select(length > 0)' "$login_response_temp")"
write_curl_header_config "$user_curl_config" 'X-CSRF-Token' "$csrf_token" || fail "用户 A curl 安全配置写入失败"
user_id="$(jq -er '.principal.id | strings | select(length > 0)' "$login_response_temp")"
user_cookie_token="$(awk 'NF >= 7 {value=$7} END {print value}' "$cookie_jar")"
[[ -n "$user_cookie_token" ]] || fail "用户 A Cookie 会话未建立"
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  [[ "$user_id" == "$reviewer_seed_creator_user_id" && "$user_id" != "$owner_b_seed_user_id" && "$user_id" != "$provisioner_user_id" ]] || \
    fail "Reviewer Seeder Creator/Reviewer/Provisioner 身份未与登录主体一致隔离"
fi
jq 'del(.csrf_token)' "$login_response_temp" >"$evidence_dir/responses/login.json"
rm -f "$login_response_temp"
login_response_temp=""

intent_key="w0-prompt-$(date +%s)-$$"
prompt_payload='{"initial_prompt":" W0 Transport é Smoke "}'
project_id="$(run_concurrent_quick_create "$intent_key" "$prompt_payload" "$evidence_dir/responses/prompt-batch" "$user_curl_config")"
[[ "$project_id" =~ ^[0-9a-f-]{36}$ ]] || fail "Project ID 格式无效"
poll_bootstrap_ready "$project_id" "$evidence_dir/responses/prompt-bootstrap.json" || fail "非空 Prompt Project 未进入 ready"
session_id="$(jq -er '.session_id | strings | select(length > 0)' "$evidence_dir/responses/prompt-bootstrap.json")"
input_id="$(jq -er '.input_id | strings | select(length > 0)' "$evidence_dir/responses/prompt-bootstrap.json")"

replay_status="$(curl_with_body_stdin "$prompt_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
  --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: $intent_key" \
  -o "$evidence_dir/responses/prompt-replay.json" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/projects:quick-create')"
[[ "$replay_status" == "200" ]] || fail "同义重放状态为 $replay_status"
jq -e --arg project "$project_id" --arg session "$session_id" --arg input "$input_id" \
  '.project_id == $project and .session_id == $session and .input_id == $input and .creation_status == "ready"' \
  "$evidence_dir/responses/prompt-replay.json" >/dev/null || fail "同义重放未返回冻结结果"

conflict_status="$(curl_with_body_stdin '{"initial_prompt":"different semantic prompt"}' --silent --show-error --max-time 10 -b "$cookie_jar" \
  --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: $intent_key" \
  -o "$evidence_dir/responses/prompt-conflict.json" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/projects:quick-create')"
[[ "$conflict_status" == "409" ]] || fail "同键异义状态为 $conflict_status"
jq -e '.error.code == "IDEMPOTENCY_CONFLICT"' "$evidence_dir/responses/prompt-conflict.json" >/dev/null || fail "同键异义错误码漂移"

blank_key="w0-blank-$(date +%s)-$$"
blank_payload='{"initial_prompt":" \t\n　"}'
blank_status="$(curl_with_body_stdin "$blank_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
  --config "$user_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: $blank_key" \
  -o "$evidence_dir/responses/blank-create.json" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/projects:quick-create')"
[[ "$blank_status" == "201" ]] || fail "空 Prompt 创建状态为 $blank_status"
blank_project_id="$(jq -er '.project_id | strings | select(length > 0)' "$evidence_dir/responses/blank-create.json")"
[[ "$blank_project_id" =~ ^[0-9a-f-]{36}$ ]] || fail "空 Prompt Project ID 格式无效"
poll_bootstrap_ready "$blank_project_id" "$evidence_dir/responses/blank-bootstrap.json" || fail "空 Prompt Project 未进入 ready"
blank_session_id="$(jq -er '.session_id | strings | select(length > 0)' "$evidence_dir/responses/blank-bootstrap.json")"
jq -e '.input_id == null and .initial_prompt_status == "absent"' "$evidence_dir/responses/blank-bootstrap.json" >/dev/null || fail "空 Prompt 创建了 Input 或错误状态"

# 第二个真实用户在 W1-C2 中同时作为正式 Reviewer 和跨 Owner 负向主体；登录 Principal 必须来自动态角色解析。
owner_b_login_payload="$(build_login_json "$owner_b_email" "$owner_b_password")" || fail "第二用户登录请求构造失败"
owner_b_login_status="$(curl_with_body_stdin "$owner_b_login_payload" --silent --show-error --max-time 10 -c "$owner_b_cookie_jar" \
  -H 'Content-Type: application/json' \
  -o "$owner_b_login_response_temp" -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/auth/session')"
unset owner_b_login_payload
[[ "$owner_b_login_status" == "200" ]] || fail "第二用户登录状态为 $owner_b_login_status"
owner_b_user_id="$(jq -er '.principal.id | strings | select(length > 0)' "$owner_b_login_response_temp")"
owner_b_csrf_token="$(jq -er '.csrf_token | strings | select(length > 0)' "$owner_b_login_response_temp")"
write_curl_header_config "$owner_b_curl_config" 'X-CSRF-Token' "$owner_b_csrf_token" || fail "第二用户 curl 安全配置写入失败"
[[ "$owner_b_user_id" == "$owner_b_seed_user_id" && "$owner_b_user_id" != "$user_id" ]] || fail "第二用户身份未与用户 A 隔离"
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  jq -e '.principal.roles == ["skill_reviewer"] and .principal.capabilities == ["skill.review"]' \
    "$owner_b_login_response_temp" >/dev/null || fail "Reviewer 登录未返回权威角色与 capability"
fi
owner_b_cookie_token="$(awk 'NF >= 7 {value=$7} END {print value}' "$owner_b_cookie_jar")"
[[ -n "$owner_b_cookie_token" ]] || fail "第二用户 Cookie 会话未建立"
rm -f "$owner_b_login_response_temp"
owner_b_login_response_temp=""

postgres_container="$("${compose[@]}" ps -q postgres)"
[[ -n "$postgres_container" ]] || fail "未找到 PostgreSQL 容器"
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  run_w1_skill_smoke "$postgres_container"
  run_w1_skill_binding_smoke "$postgres_container"
fi
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

tool_catalog_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
  -o "$evidence_dir/responses/tool-definition-catalog.json" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/agent/sessions/${session_id}/tools")"
[[ "$tool_catalog_status" == "200" ]] || fail "Tool Definition Catalog 状态为 $tool_catalog_status"
jq -e '
  keys == ["items","request_id","schema_version"]
  and .schema_version == "tool_definition_catalog.v1"
  and (.request_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
  and (.items | length) == 6
  and ([.items[].tool_key] == ["plan_creation_spec","analyze_materials","plan_storyboard","generate_media","write_prompts","assemble_output"])
  and ([.items[].display_name] == ["流程规划","素材分析","故事板设计","媒体生成","提示词写法","视频剪辑"])
  and ([.items[].order] == [1,2,3,4,5,6])
  and all(.items[];
    (keys == ["availability","display_name","order","reason_code","tool_key"])
    and .availability == "unavailable"
    and .reason_code == "DESIGN_REVIEW_PENDING")' \
  "$evidence_dir/responses/tool-definition-catalog.json" >/dev/null || \
  fail "Tool Definition Catalog exact-set、顺序或不可用原因漂移"

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
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  : >"$w1_temp_dir/response.json"
  owner_b_skill_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    -o "$w1_temp_dir/response.json" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}")"
  [[ "$owner_b_skill_status" == "404" ]] || fail "第二用户访问用户 A Skill 未按 Owner-safe 404 关闭，状态为 $owner_b_skill_status"
  assert_owner_safe_error "$w1_temp_dir/response.json" "SKILL_NOT_FOUND" \
    "$w1_skill_id" "$w1_review_id" "$w1_skill_name" "$w1_updated_skill_name" || \
    fail "跨 Owner Skill 错误响应泄漏了权威资源事实"
  jq -n --argjson status "$owner_b_skill_status" --arg creator "$user_id" --arg reviewer "$owner_b_user_id" \
    --arg skill "$w1_skill_id" --arg review "$w1_review_id" --arg original_name "$w1_skill_name" \
    --arg updated_name "$w1_updated_skill_name" --slurpfile response "$w1_temp_dir/response.json" '
    ($response[0] | [.. | strings]
      | any(.[]; contains($skill) or contains($review) or contains($original_name) or contains($updated_name))) as $disclosed
    | {skill_detail:{status:$status,code:$response[0].error.code},
       distinct_principals:($creator != $reviewer),resource_facts_disclosed:$disclosed,
       cross_owner_not_found:($status == 404 and $response[0].error.code == "SKILL_NOT_FOUND"
         and $creator != $reviewer and ($disclosed | not))}' \
    >"$evidence_dir/responses/w1-skill-cross-owner.json"
  jq -e '.cross_owner_not_found' "$evidence_dir/responses/w1-skill-cross-owner.json" >/dev/null || \
    fail "W1 Skill 跨 Owner 派生证据不成立"
  w1_skill_smoke_ran=true
fi

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
owner_b_tools_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
  -o "$owner_b_denied_response_temp" -w '%{http_code}' \
  "http://127.0.0.1:18081/api/v1/agent/sessions/${session_id}/tools")"
[[ "$owner_b_tools_status" == "404" ]] || fail "第二用户访问用户 A Tool Catalog 未按 Owner-safe 404 关闭，状态为 $owner_b_tools_status"
assert_owner_safe_error "$owner_b_denied_response_temp" "SESSION_NOT_FOUND" \
  "$project_id" "$session_id" "$input_id" "plan_creation_spec" || fail "跨 Owner Tool Catalog 错误响应泄漏了资源或目录事实"
owner_b_tools_code="$(jq -er '.error.code' "$owner_b_denied_response_temp")"
owner_b_tools_facts_disclosed="$(jq -r \
  --arg project "$project_id" --arg session "$session_id" --arg input "$input_id" \
  '[.. | strings] | any(.[]; contains($project) or contains($session) or contains($input) or contains("plan_creation_spec"))' \
  "$owner_b_denied_response_temp")"
[[ "$owner_b_tools_facts_disclosed" == "false" ]] || fail "跨 Owner Tool Catalog 派生证据发现资源或目录事实"

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

jq -n \
  --argjson tools_status "$owner_b_tools_status" --arg tools_code "$owner_b_tools_code" \
  --argjson tools_facts_disclosed "$owner_b_tools_facts_disclosed" \
  '{project_bootstrap:{status:404,code:"PROJECT_NOT_FOUND"},session_workspace:{status:404,code:"SESSION_NOT_FOUND"},
    session_tools:{status:$tools_status,code:$tools_code},
    session_events:{status:404,code:"SESSION_NOT_FOUND",content_type:"application/json",sse_headers_committed:false},
    distinct_principals:true,tool_catalog_resource_facts_disclosed:$tools_facts_disclosed,
    tool_catalog_cross_owner_not_found:($tools_status == 404 and $tools_code == "SESSION_NOT_FOUND" and ($tools_facts_disclosed | not))}' \
  >"$evidence_dir/responses/cross-owner-access.json"
jq -e '.tool_catalog_cross_owner_not_found' "$evidence_dir/responses/cross-owner-access.json" >/dev/null || \
  fail "跨 Owner Tool Catalog canonical 派生证据不成立"

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

if [[ "$w1_skill_smoke_enabled" != "1" ]]; then
  old_agent_pid="$agent_pid"
  kill -TERM "$old_agent_pid" || fail "Agent 真实重启未能发送 TERM"
  agent_stopped=false
  for _ in $(seq 1 120); do
    agent_process_state="$(ps -o stat= -p "$old_agent_pid" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$old_agent_pid" 2>/dev/null || [[ "$agent_process_state" == Z* ]]; then
      agent_stopped=true
      break
    fi
    sleep 0.25
  done
  [[ "$agent_stopped" == "true" ]] || fail "Agent 收到 TERM 后未在 30 秒内退出"
  if ! wait "$old_agent_pid"; then
    fail "Agent TERM 后未完成正常 wait"
  fi
  if kill -0 "$old_agent_pid" 2>/dev/null; then
    fail "Agent wait 后进程仍存活"
  fi
  agent_pid=""

  "$repo_root/.local/bin/agent-service" >"$evidence_dir/agent-restart.log" 2>&1 &
  agent_pid="$!"
  wait_ready 18082 "$agent_pid"

  restart_workspace_status=""
  for _ in $(seq 1 60); do
    restart_workspace_status="$(curl --silent --show-error --max-time 2 -b "$cookie_jar" \
      -o "$agent_restart_workspace_temp" -w '%{http_code}' \
      "http://127.0.0.1:18081/api/v1/agent/sessions/${session_id}/workspace")"
    if [[ "$restart_workspace_status" == "200" ]]; then
      break
    fi
    [[ "$restart_workspace_status" == "502" || "$restart_workspace_status" == "503" ]] || \
      fail "Agent 重启后 Business BFF Workspace 返回非恢复态状态 $restart_workspace_status"
    sleep 0.25
  done
  [[ "$restart_workspace_status" == "200" ]] || fail "Agent 重启后 Business BFF Workspace 未恢复"
  jq -e --arg project "$project_id" --arg session "$session_id" --arg input "$input_id" '
    .schema_version == "session.workspace.v1"
    and .session.id == $session
    and .session.project_id == $project
    and .session.status == "active"
    and (.messages | type) == "array" and (.messages | length) == 1
    and (.messages[0].content | length) > 0
    and (.inputs | type) == "array" and (.inputs | length) == 1
    and .inputs[0].id == $input
    and .event_high_watermark == 2
    and .min_available_seq == 1' "$agent_restart_workspace_temp" >/dev/null || \
    fail "Agent 重启后 Workspace Snapshot 未恢复同一权威状态"

  restart_sse_exit=0
  curl --silent --show-error --no-buffer --max-time 3 -b "$cookie_jar" \
    -H 'Accept: text/event-stream' \
    -o "$agent_restart_sse_temp" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/agent/sessions/${session_id}/events?after_seq=1" \
    >"$agent_restart_sse_status_temp" || restart_sse_exit="$?"
  [[ "$restart_sse_exit" == "0" || "$restart_sse_exit" == "28" ]] || \
    fail "Agent 重启后 Workspace SSE curl 退出码为 $restart_sse_exit"
  [[ "$(tr -d '[:space:]' <"$agent_restart_sse_status_temp")" == "200" ]] || \
    fail "Agent 重启后 Workspace SSE 未返回 200"
  sed -n 's/^data: //p' "$agent_restart_sse_temp" \
    | jq -s -e --arg project "$project_id" --arg session "$session_id" --arg input "$input_id" '
      any(.[]; .schema_version == "workspace.event.v1"
        and .event == "session.input.accepted"
        and .session_id == $session and .project_id == $project
        and .seq == 2 and .aggregate_id == $input)
      and any(.[]; .schema_version == "workspace.stream-control.v1"
        and .event == "stream.ready" and .session_id == $session and .cursor == 2)' >/dev/null || \
    fail "Agent 重启后 Workspace SSE 未从 PostgreSQL 补读原 Event/Ready"
  grep -Eq '^id: 2$' "$agent_restart_sse_temp" || fail "Agent 重启后 SSE id 未与原 Seq 对齐"

  jq -n --arg project_id "$project_id" --arg session_id "$session_id" --arg input_id "$input_id" '
    {schema_version:"w05.agent-restart-recovery.v1",project_id:$project_id,session_id:$session_id,input_id:$input_id,
      event_seq:2,event_high_watermark:2,ready_cursor:2,
      agent_restart_hit:true,snapshot_after_restart:true,sse_after_restart:true}' \
    >"$evidence_dir/responses/agent-restart-recovery.json"
fi

if [[ "${W0_RUN_BROWSER_SMOKE:-0}" == "1" ]]; then
  [[ -x "$repo_root/frontend/node_modules/.bin/playwright" ]] || fail "未安装前端 Playwright 依赖，请先在 frontend 执行 npm install"
  if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
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
  else
    : >"$w05_browser_result_temp"
    (
      cd "$repo_root/frontend"
      DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
      DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
      DORA_E2E_OWNER_B_EMAIL="$owner_b_email" \
      DORA_E2E_OWNER_B_PASSWORD="$owner_b_password" \
      DORA_E2E_PROMPT="$w05_browser_prompt" \
      DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:18081" \
      DORA_E2E_W05_RESULT_PATH="$w05_browser_result_temp" \
      npm run test:e2e:w0
    ) >"$evidence_dir/frontend-playwright.log" 2>&1 || {
      sed -n '1,240p' "$evidence_dir/frontend-playwright.log" >&2
      fail "W0 浏览器页面链路失败"
    }
    jq -e --arg creator "$user_id" --arg owner_b "$owner_b_user_id" '
      keys == ["controlled_disconnect","creator_user_id","cross_owner_agent_blocked","cross_owner_not_found","cross_owner_user_id","project_id","resource_facts_not_disclosed","same_session_recovery","schema_version","session_id"]
      and .schema_version == "w05.workspace-browser.smoke.result.v1"
      and .creator_user_id == $creator and .cross_owner_user_id == $owner_b
      and .creator_user_id != .cross_owner_user_id
      and (.project_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and (.session_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and .controlled_disconnect == true
      and .same_session_recovery == true
      and .cross_owner_not_found == true
      and .cross_owner_agent_blocked == true
      and .resource_facts_not_disclosed == true' "$w05_browser_result_temp" >/dev/null || \
      fail "W0.5 Playwright 结构化恢复/跨 Owner 结果不满足严格契约"
  fi
  browser_smoke_ran=true
fi

if [[ "$w1_browser_smoke_enabled" == "1" ]]; then
  [[ -x "$repo_root/frontend/node_modules/.bin/playwright" ]] || fail "未安装前端 Playwright 依赖，请先在 frontend 执行 npm install"
  [[ -n "$DORA_SMOKE_USER_EMAIL" && -n "$DORA_SMOKE_USER_PASSWORD" && -n "$owner_b_email" && -n "$owner_b_password" ]] || \
    fail "W1 Reviewer 浏览器门禁缺少 Creator/Reviewer 凭据"
  w1_browser_result="$w1_temp_dir/browser-real-review-result.json"
  rm -f "$w1_browser_result"
  (
    cd "$repo_root/frontend"
    DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
    DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
    DORA_E2E_REVIEWER_EMAIL="$owner_b_email" \
    DORA_E2E_REVIEWER_PASSWORD="$owner_b_password" \
    DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:18081" \
    DORA_E2E_OUTPUT_DIR="../.local/playwright/w1-skill-foundation" \
    DORA_E2E_W1_RESULT_PATH="$w1_browser_result" \
    npm run test:e2e:w1-real-review
  ) >"$evidence_dir/frontend-w1-playwright.log" 2>&1 || {
    sed -n '1,240p' "$evidence_dir/frontend-w1-playwright.log" >&2
    fail "W1 Creator→Reviewer→QuickCreate v2 浏览器真实链路失败"
  }
  run_w1_browser_frozen_smoke "$postgres_container" "$w1_browser_result"
  w1_browser_smoke_ran=true
fi

if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  reviewer_revoke_output="$w1_temp_dir/reviewer-revoke.json"
  reviewer_after_revoke="$w1_temp_dir/reviewer-after-revoke.json"
  reviewer_denied_after_revoke="$w1_temp_dir/reviewer-denied-after-revoke.json"
  (
    cd "$repo_root/business"
    DORA_ROLE_ADMIN_POSTGRES_DSN="$BUSINESS_DATABASE_URL" GOWORK=off "$go_bin" run ./cmd/business-role-admin \
      -action revoke -assignment-id "$reviewer_assignment_id" -expected-version 1 \
      -target-user-id "$owner_b_seed_user_id" -actor-user-id "$provisioner_user_id" \
      -role skill_reviewer -reason local_smoke_cleanup \
      -approval-reference "local-smoke-reviewer-revoke-${run_id}"
  ) >"$reviewer_revoke_output"
  jq -e --arg assignment "$reviewer_assignment_id" --arg reviewer "$owner_b_seed_user_id" '
    .action == "revoke" and .assignment_id == $assignment and .target_user_id == $reviewer
    and .role == "skill_reviewer" and .status == "revoked" and .version == 2' \
    "$reviewer_revoke_output" >/dev/null || fail "Reviewer 正式撤权结果漂移"

  reviewer_session_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    -o "$reviewer_after_revoke" -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/auth/session')"
  [[ "$reviewer_session_status" == "200" ]] || fail "Reviewer 撤权后同一 Session 未重新解析，状态为 $reviewer_session_status"
  jq -e --arg reviewer "$owner_b_seed_user_id" '
    .principal.id == $reviewer and .principal.roles == [] and .principal.capabilities == []' \
    "$reviewer_after_revoke" >/dev/null || fail "Reviewer 撤权后同一 Cookie 仍保留 capability"
  reviewer_denied_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    -o "$reviewer_denied_after_revoke" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/admin/skill-reviews?status=reviewing')"
  [[ "$reviewer_denied_status" == "403" ]] || fail "Reviewer 撤权后管理 API 状态为 $reviewer_denied_status"
  jq -e '.error.code == "SKILL_REVIEW_CAPABILITY_REQUIRED"' "$reviewer_denied_after_revoke" >/dev/null || \
    fail "Reviewer 撤权后管理 API 错误码漂移"
  jq -n --arg assignment_id "$reviewer_assignment_id" --arg reviewer "$owner_b_seed_user_id" \
    --argjson session_status "$reviewer_session_status" --argjson denied_status "$reviewer_denied_status" \
    --slurpfile revoke "$reviewer_revoke_output" --slurpfile session "$reviewer_after_revoke" \
    --slurpfile denied "$reviewer_denied_after_revoke" '
    {assignment_id:$assignment_id,revoke_status:$revoke[0].status,assignment_version:$revoke[0].version,
      same_cookie_roles_empty:($session[0].principal.roles == []),
      same_cookie_capabilities_empty:($session[0].principal.capabilities == []),
      admin_api_status:$denied_status,admin_api_code:$denied[0].error.code,
      reviewer_revocation:($revoke[0].assignment_id == $assignment_id and $revoke[0].target_user_id == $reviewer
        and $revoke[0].status == "revoked" and $revoke[0].version == 2 and $session_status == 200
        and $session[0].principal.id == $reviewer and $session[0].principal.roles == []
        and $session[0].principal.capabilities == [] and $denied_status == 403
        and $denied[0].error.code == "SKILL_REVIEW_CAPABILITY_REQUIRED")}' \
    >"$evidence_dir/responses/w1-reviewer-revocation.json"
  jq -e '.reviewer_revocation' "$evidence_dir/responses/w1-reviewer-revocation.json" >/dev/null || \
    fail "Reviewer 撤权派生证据不成立"
  w1_reviewer_revocation_smoke_ran=true
fi

owner_b_logout_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" -c "$owner_b_cookie_jar" \
  --config "$owner_b_curl_config" -X DELETE -o /dev/null -w '%{http_code}' \
  'http://127.0.0.1:18081/api/v1/auth/session')"
[[ "$owner_b_logout_status" == "204" ]] || fail "第二用户退出状态为 $owner_b_logout_status"

logout_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" -c "$cookie_jar" \
  --config "$user_curl_config" -X DELETE -o /dev/null -w '%{http_code}' \
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
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  [[ "$w1_skill_smoke_ran" == "true" ]] || fail "W1 Skill API/数据库/跨 Owner 门禁未完整执行"
  [[ "$w1_skill_binding_smoke_ran" == "true" ]] || fail "W1 Project Skill Binding 跨模块门禁未完整执行"
  [[ "$w1_reviewer_rbac_smoke_ran" == "true" ]] || fail "W1 Reviewer RBAC/HTTP 发布门禁未完整执行"
  [[ "$w1_reviewer_revocation_smoke_ran" == "true" ]] || fail "W1 Reviewer 撤权即时失效门禁未完整执行"
  [[ "$w1_browser_smoke_ran" == "true" ]] || fail "W1-C2 @w1-real-review 真实浏览器门禁未完整执行"
  jq -n \
    --arg schema_version "w1.skill-foundation.smoke.evidence.v3" \
    --arg run_id "$run_id" --arg produced_at "$produced_at" \
    --arg source_digest_sha256 "$source_digest_sha256" \
    --arg business_binary_sha256 "$business_binary_sha256" --arg agent_binary_sha256 "$agent_binary_sha256" \
    --arg project_id "$project_id" --arg session_id "$session_id" --arg input_id "$input_id" \
    --arg blank_project_id "$blank_project_id" --arg blank_session_id "$blank_session_id" \
    --arg skill_id "$w1_skill_id" --arg review_id "$w1_review_id" \
    --arg binding_project_id "$w1_binding_project_id" --arg binding_session_id "$w1_binding_session_id" --arg binding_input_id "$w1_binding_input_id" \
    --argjson logout_status "$after_logout_status" --argjson logout_workspace_status "$after_logout_workspace_status" \
    --slurpfile skill_api "$evidence_dir/responses/w1-skill-api.json" \
    --slurpfile skill_db "$evidence_dir/responses/w1-skill-database.json" \
    --slurpfile publish "$evidence_dir/responses/w1-skill-publish.json" \
    --slurpfile publish_db "$evidence_dir/responses/w1-skill-publish-database.json" \
    --slurpfile cross_owner "$evidence_dir/responses/w1-skill-cross-owner.json" \
    --slurpfile cross_owner_access "$evidence_dir/responses/cross-owner-access.json" \
    --slurpfile binding_api "$evidence_dir/responses/w1-binding-api.json" \
    --slurpfile binding_consistency "$evidence_dir/responses/w1-binding-consistency.json" \
    --slurpfile browser "$evidence_dir/responses/w1-browser-frozen-consistency.json" \
    --slurpfile tool_catalog "$evidence_dir/responses/tool-definition-catalog.json" \
    --slurpfile revocation "$evidence_dir/responses/w1-reviewer-revocation.json" \
    --slurpfile transport_business "$evidence_dir/responses/business-prompt-assertion.json" \
    --slurpfile transport_agent "$evidence_dir/responses/agent-prompt-assertion.json" \
    --slurpfile transport_blank "$evidence_dir/responses/agent-blank-assertion.json" \
    --slurpfile logout "$evidence_dir/responses/after-logout.json" \
    --slurpfile logout_workspace "$evidence_dir/responses/after-logout-workspace.json" '
    {schema_version:$schema_version,status:"pending",run_id:$run_id,produced_at:$produced_at,source_digest_sha256:$source_digest_sha256,business_binary_sha256:$business_binary_sha256,agent_binary_sha256:$agent_binary_sha256,
      transport_prerequisite:{prompt_project:{project_id:$project_id,session_id:$session_id,input_id:$input_id},blank_project:{project_id:$blank_project_id,session_id:$blank_session_id,input_id:null}},
      skill_foundation:{skill_id:$skill_id,review_id:$review_id},
      project_skill_binding:{project_id:$binding_project_id,session_id:$binding_session_id,input_id:$binding_input_id,skill_count:$binding_consistency[0].skill_count},
      assertions:{
        transport_prerequisite:($transport_business[0].owner_matches and $transport_business[0].binding_status == "ready"
          and $transport_business[0].outbox_status == "delivered" and $transport_business[0].receipt_count == 1
          and $transport_business[0].prompt_ciphertext_cleared and $transport_agent[0].session_count == 1
          and $transport_agent[0].snapshot_count == 1 and $transport_agent[0].receipt_count == 1
          and $transport_agent[0].message_count == 1 and $transport_agent[0].input_count == 1
          and $transport_agent[0].event_count == 2 and $transport_blank[0].session_count == 1
          and $transport_blank[0].message_count == 0 and $transport_blank[0].input_count == 0
          and $transport_blank[0].receipt_count == 1 and $transport_blank[0].event_count == 1),
        skill_create_201:($skill_api[0].create.first_status == 201),
        skill_create_replay_200:($skill_api[0].create.replay_status == 200 and $skill_api[0].create.replay_result_matches),
        skill_create_conflict_409:($skill_api[0].create.conflict_status == 409 and $skill_api[0].create.conflict_code == "IDEMPOTENCY_CONFLICT"),
        missing_array_failed_closed:($skill_api[0].strict_shape.missing_array_status == 400 and $skill_api[0].strict_shape.failed_closed_without_side_effects),
        null_array_failed_closed:($skill_api[0].strict_shape.null_array_status == 400 and $skill_api[0].strict_shape.failed_closed_without_side_effects),
        cover_asset_null_only:($skill_api[0].strict_shape.cover_asset_non_null_status == 400 and $skill_api[0].strict_shape.failed_closed_without_side_effects),
        owner_list_and_detail:($skill_api[0].owner_read.list_status == 200 and $skill_api[0].owner_read.detail_status == 200),
        if_match_update:($skill_api[0].update.status == 200 and $skill_api[0].update.response_etag_matches and $skill_api[0].update.etag_changed),
        stale_etag_conflict:($skill_api[0].update.stale_status == 409 and $skill_api[0].update.stale_code == "SKILL_DRAFT_CONFLICT"),
        public_tool_refs_failed_closed:($skill_api[0].public_tool_refs.status == 400 and $skill_api[0].public_tool_refs.failed_closed),
        review_submit_201:($skill_api[0].review.first_status == 201),
        review_if_match:$skill_api[0].review.if_match,
        review_replay_200:($skill_api[0].review.replay_status == 200 and $skill_api[0].review.frozen_result),
        reviewer_rbac:$publish[0].reviewer_rbac,
        reviewer_revocation:$revocation[0].reviewer_revocation,
        review_approve_and_publish:($publish[0].decision_status == 200 and $publish_db[0].approved_review_count == 1
          and $publish_db[0].published_pointer_matches and $publish_db[0].receipt_audit_request_id_matches),
        review_approve_replay:($publish[0].replay_status == 200 and $publish[0].idempotent_business_result),
        review_strong_etag:$publish[0].strong_etag,
        review_frozen_definition:$publish[0].frozen_definition,
        receipt_audit_request_id_consistent:$publish_db[0].receipt_audit_request_id_matches,
        cross_owner_not_found:$cross_owner[0].cross_owner_not_found,
        tool_catalog_cross_owner_not_found:($cross_owner_access[0].tool_catalog_cross_owner_not_found
          and $cross_owner_access[0].session_tools.status == 404
          and $cross_owner_access[0].session_tools.code == "SESSION_NOT_FOUND"
          and ($cross_owner_access[0].tool_catalog_resource_facts_disclosed | not)),
        revision_count:$skill_db[0].revision_count,
        review_count:$skill_db[0].review_count,
        published_snapshot_count:$publish_db[0].published_count,
        governance_audit_count:$publish_db[0].governance_audit_count,
        no_physical_foreign_keys:($skill_db[0].physical_fk_count == 0),
        quick_create_v2_concurrent_requests:$binding_api[0].concurrent_requests,
        quick_create_v2_replay:($binding_api[0].replay_status == 200 and $binding_api[0].replay_result_matches),
        quick_create_v2_conflict:($binding_api[0].conflict_status == 409 and $binding_api[0].conflict_code == "IDEMPOTENCY_CONFLICT"),
        business_v2_envelope_cleared:$binding_consistency[0].business_v2_envelope_cleared,
        agent_v2_snapshot_encrypted:$binding_consistency[0].agent_v2_snapshot_encrypted_and_decryptable,
        v1_v2_session_isolation:$binding_consistency[0].v1_v2_session_isolation,
        snapshot_digest_business_agent_verifier_consistent:$binding_consistency[0].snapshot_digest_business_agent_verifier_consistent,
        runtime_content_digest_business_agent_verifier_consistent:$binding_consistency[0].runtime_content_digest_business_agent_verifier_consistent,
        content_digest_business_agent_verifier_consistent:$binding_consistency[0].content_digest_business_agent_verifier_consistent,
        skill_count_business_agent_verifier_consistent:$binding_consistency[0].skill_count_business_agent_verifier_consistent,
        browser_ui:$browser[0].browser_result_contract,
        browser_tool_catalog_static_unavailable:($browser[0].browser_tool_catalog_static_unavailable
          and $tool_catalog[0].schema_version == "tool_definition_catalog.v1"
          and ($tool_catalog[0].items | length) == 6
          and all($tool_catalog[0].items[]; .availability == "unavailable" and .reason_code == "DESIGN_REVIEW_PENDING")),
        browser_formal_api_frozen_revision:$browser[0].formal_api_frozen_revision,
        browser_business_frozen_revision:$browser[0].business_frozen_revision,
        browser_agent_snapshot_matches_published:$browser[0].agent_snapshot_matches_published,
        browser_digest_business_agent_verifier_consistent:$browser[0].digest_business_agent_verifier_consistent,
        browser_review_publish_quickcreate_v2:$browser[0].browser_review_publish_quickcreate_v2,
        logout_revoked:($logout_status == 401 and $logout[0].error.code == "UNAUTHENTICATED"),
        logout_workspace_denied:($logout_workspace_status == 401 and $logout_workspace[0].error.code == "UNAUTHENTICATED")
      }}' \
    >"$pending_evidence_file"
else
  if [[ "$browser_smoke_ran" == "true" ]]; then
    jq -n \
      --arg schema_version "w05.workspace-transport.smoke.evidence.v2" \
      --arg run_id "$run_id" --arg produced_at "$produced_at" \
      --arg source_digest_sha256 "$source_digest_sha256" \
      --arg business_binary_sha256 "$business_binary_sha256" --arg agent_binary_sha256 "$agent_binary_sha256" \
      --arg project_id "$project_id" --arg session_id "$session_id" --arg input_id "$input_id" \
      --arg blank_project_id "$blank_project_id" --arg blank_session_id "$blank_session_id" \
      --argjson browser_ui "$browser_smoke_ran" \
      --slurpfile restart "$evidence_dir/responses/agent-restart-recovery.json" \
      --slurpfile browser "$w05_browser_result_temp" \
      '{schema_version:$schema_version,status:"pending",run_id:$run_id,produced_at:$produced_at,source_digest_sha256:$source_digest_sha256,business_binary_sha256:$business_binary_sha256,agent_binary_sha256:$agent_binary_sha256,prompt_project:{project_id:$project_id,session_id:$session_id,input_id:$input_id},blank_project:{project_id:$blank_project_id,session_id:$blank_session_id,input_id:null},browser_workspace:{project_id:$browser[0].project_id,session_id:$browser[0].session_id},assertions:{concurrent_requests:100,idempotent_replay:true,idempotency_conflict:true,business_prompt_cleared:true,agent_unique_facts:true,blank_negative_side_effects:true,workspace_snapshot:true,workspace_empty_arrays:true,workspace_owner_safe_not_found:true,workspace_cross_owner_not_found:true,events_cross_owner_not_found:true,agent_direct_access_denied:true,sse_replay_and_ready:true,sse_cursor_reset:true,browser_ui:$browser_ui,logout_revoked:true,logout_workspace_denied:true,agent_restart_hit:$restart[0].agent_restart_hit,snapshot_after_restart:$restart[0].snapshot_after_restart,sse_after_restart:$restart[0].sse_after_restart,browser_controlled_disconnect:$browser[0].controlled_disconnect,browser_same_session_recovery:$browser[0].same_session_recovery,browser_cross_owner_not_found:$browser[0].cross_owner_not_found,browser_cross_owner_agent_blocked:$browser[0].cross_owner_agent_blocked,browser_resource_facts_not_disclosed:$browser[0].resource_facts_not_disclosed}}' \
      >"$pending_evidence_file"
  else
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
  fi
fi

if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  jq -e '
    .schema_version == "w1.skill-foundation.smoke.evidence.v3"
    and .status == "pending"
    and .assertions.revision_count == 2
    and .assertions.review_count == 1
    and .assertions.published_snapshot_count == 1
    and .assertions.governance_audit_count == 1
    and .assertions.quick_create_v2_concurrent_requests == 100
    and (.assertions | length) == 47
    and ([.assertions | to_entries[]
      | select(.key != "revision_count"
        and .key != "review_count"
        and .key != "published_snapshot_count"
        and .key != "governance_audit_count"
        and .key != "quick_create_v2_concurrent_requests")] as $boolean_assertions
      | ($boolean_assertions | length) == 42
      and all($boolean_assertions[]; ((.value | type) == "boolean" and .value == true)))' \
    "$pending_evidence_file" >/dev/null || fail "W1 canonical Evidence 含未通过断言，禁止发布 passed summary"
elif [[ "$browser_smoke_ran" == "true" ]]; then
  jq -e '
    .schema_version == "w05.workspace-transport.smoke.evidence.v2"
    and .status == "pending"
    and (keys == ["agent_binary_sha256","assertions","blank_project","browser_workspace","business_binary_sha256","produced_at","prompt_project","run_id","schema_version","source_digest_sha256","status"])
    and (.browser_workspace.project_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
    and (.browser_workspace.session_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
    and .assertions.concurrent_requests == 100
    and (.assertions | length) == 25
    and (.assertions | keys) == ["agent_direct_access_denied","agent_restart_hit","agent_unique_facts","blank_negative_side_effects","browser_controlled_disconnect","browser_cross_owner_agent_blocked","browser_cross_owner_not_found","browser_resource_facts_not_disclosed","browser_same_session_recovery","browser_ui","business_prompt_cleared","concurrent_requests","events_cross_owner_not_found","idempotency_conflict","idempotent_replay","logout_revoked","logout_workspace_denied","snapshot_after_restart","sse_after_restart","sse_cursor_reset","sse_replay_and_ready","workspace_cross_owner_not_found","workspace_empty_arrays","workspace_owner_safe_not_found","workspace_snapshot"]
    and ([.assertions | to_entries[] | select(.key != "concurrent_requests")] | length) == 24
    and all(.assertions | to_entries[] | select(.key != "concurrent_requests");
      ((.value | type) == "boolean" and .value == true))' \
    "$pending_evidence_file" >/dev/null || fail "W0.5 Recovery Evidence v2 含未通过或非布尔断言，禁止发布 passed summary"
fi

stop_processes
business_pid=""
agent_pid=""

etcd_container="$("${compose[@]}" ps -q etcd)"
remaining_session_keys="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 get /dora/services/dora.agent.session.v1/ --prefix --keys-only)"
[[ -z "$remaining_session_keys" ]] || fail "Agent 退出后仍残留 Session RPC 注册键"

# Runtime 已全部停止，下面扫描的是不会再追加内容的闭合 Evidence 集合。
assert_evidence_excludes_literal "$csrf_token" "用户 A CSRF"
assert_evidence_excludes_literal "$DORA_SMOKE_USER_PASSWORD" "用户 A 密码"
assert_evidence_excludes_literal "$user_cookie_token" "用户 A Cookie"
assert_evidence_excludes_literal "$owner_b_password" "用户 B 密码"
assert_evidence_excludes_literal "$owner_b_csrf_token" "用户 B CSRF"
assert_evidence_excludes_literal "$owner_b_cookie_token" "用户 B Cookie"
assert_evidence_excludes_literal 'W0 Transport é Smoke' "完整 Prompt"
assert_evidence_excludes_literal "$w05_browser_prompt" "W0.5 浏览器完整 Prompt"
assert_evidence_excludes_regex '"csrf_token"[[:space:]]*:[[:space:]]*"[^"[:space:]][^"]*"' "任意非空 CSRF JSON 字段"
assert_evidence_excludes_regex 'X-Dora-Identity-(Assertion|Signature):' "内部身份断言材料"
assert_evidence_excludes_regex '(?i)(cookie|set-cookie)[[:space:]]*:' "Cookie Header"
assert_evidence_excludes_regex '(?i)x-csrf-token[[:space:]]*:' "CSRF Header"
assert_evidence_excludes_regex '(?i)"password"[[:space:]]*:' "密码字段"
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  assert_evidence_excludes_literal "$provisioner_password" "Reviewer Provisioner 密码"
  assert_evidence_excludes_literal "$w1_skill_name" "W1 Skill 原始名称"
  assert_evidence_excludes_literal "$w1_updated_skill_name" "W1 Skill 更新后原始名称"
  assert_evidence_excludes_literal "$w1_binding_prompt" "W1 Binding 完整 Prompt"
  assert_evidence_excludes_regex '"definition"[[:space:]]*:' "W1 Skill 完整定义正文"
  assert_evidence_excludes_regex '(?i)"(payload_nonce|payload_ciphertext|runtime_content_ciphertext)"[[:space:]]*:' "W1 密文或 Nonce 字段"
  if [[ -n "${BUSINESS_PROJECT_PROMPT_KEY_BASE64:-}" ]]; then
    assert_evidence_excludes_literal "$BUSINESS_PROJECT_PROMPT_KEY_BASE64" "Business Prompt 密钥材料"
  fi
  if [[ -n "${AGENT_CONTENT_KEY_BASE64:-}" ]]; then
    assert_evidence_excludes_literal "$AGENT_CONTENT_KEY_BASE64" "Agent Content 密钥材料"
  fi
fi

# 只有脱敏扫描、Runtime 退出和 etcd 租约摘除全部成功后，才原子发布 passed summary。
jq '.status = "passed"' "$pending_evidence_file" >"${evidence_file}.tmp"
rm -f "$pending_evidence_file"
# canonical W0.5 summary 是最后一次可失败写操作；旧 W0 summary 已在运行开始撤销，避免双真源假绿。
mv "${evidence_file}.tmp" "$evidence_file"
trap - EXIT

if [[ "$w1_skill_smoke_enabled" == "1" && "$w1_browser_smoke_ran" == "true" ]]; then
  echo "W1 Skill 发布、Project Binding、Session Snapshot v2、跨 Owner 与浏览器冒烟通过"
elif [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  echo "W1 Skill 发布、Project Binding、Session Snapshot v2、数据库一致性与跨 Owner 冒烟通过"
elif [[ "$browser_smoke_ran" == "true" ]]; then
  echo "W0.5 Transport API/Snapshot/SSE 与真实浏览器登录、Quick Create、正式工作台、退出冒烟通过"
else
  echo "W0.5 Transport 真实登录、100 并发 Quick Create、Business generated Kitex→Agent、Workspace Snapshot/SSE、空 Prompt 与退出冒烟通过"
fi
