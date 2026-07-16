#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/smoke-secret-transport.sh
. "$repo_root/scripts/lib/smoke-secret-transport.sh"
disable_shell_xtrace
umask 077
# shellcheck source=lib/w1-smoke-mode.sh
. "$repo_root/scripts/lib/w1-smoke-mode.sh"
# shellcheck source=lib/w1-evidence-release.sh
. "$repo_root/scripts/lib/w1-evidence-release.sh"
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
  w1_evidence_release_root="$repo_root/.local/smoke/w1-evidence-releases"
  w1_evidence_release_dir="$w1_evidence_release_root/$run_id"
  w1_evidence_current_manifest="$w1_evidence_release_root/current.json"
  evidence_file="$w1_evidence_release_dir/w1-skill-foundation-evidence.json"
  governance_evidence_file="$w1_evidence_release_dir/w1-skill-governance-evidence.json"
  skill_market_evidence_file="$w1_evidence_release_dir/w1-skill-market-evidence.json"
  skill_market_binding_evidence_file="$w1_evidence_release_dir/w1-skill-market-binding-evidence.json"
  skill_republish_evidence_file="$w1_evidence_release_dir/w1-skill-republish-session-isolation-evidence.json"
  legacy_evidence_file=""
else
  evidence_dir="$repo_root/.local/smoke/w0-transport/runs/$run_id"
  evidence_scan_root="$repo_root/.local/smoke/w0-transport/runs"
  evidence_file="$repo_root/.local/smoke/w05-workspace-transport-evidence.json"
  governance_evidence_file=""
  skill_market_evidence_file=""
  skill_market_binding_evidence_file=""
  skill_republish_evidence_file=""
  w1_evidence_release_root=""
  w1_evidence_release_dir=""
  w1_evidence_current_manifest=""
  legacy_evidence_file="$repo_root/.local/smoke/w0-transport-evidence.json"
fi
pending_evidence_file="$evidence_dir/evidence-summary.pending.json"
governance_pending_evidence_file=""
skill_market_pending_evidence_file=""
skill_market_binding_pending_evidence_file=""
skill_republish_pending_evidence_file=""
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
w05_retention_control_dir=""
w05_retention_injector_pid=""
w1_public_market_control_dir=""
w1_browser_playwright_pid=""
w1_toctou_lock_shell_pid=""
w1_toctou_lock_database_pid=""
w1_toctou_governance_shell_pid=""
w1_toctou_quick_shell_pid=""
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
governor_password=""
governor_assignment_id=""
governor_user_id=""
governor_cookie_jar=""
governor_curl_config=""
governor_login_response_temp=""
governor_csrf_token=""
governor_cookie_token=""
user_cookie_token=""
business_pid=""
agent_pid=""
postgres_container=""
browser_smoke_ran=false
w1_skill_smoke_ran=false
w1_browser_smoke_ran=false
w1_skill_binding_smoke_ran=false
w1_reviewer_rbac_smoke_ran=false
w1_reviewer_revocation_smoke_ran=false
w1_skill_governance_smoke_ran=false
w1_skill_market_smoke_ran=false
w1_skill_market_binding_smoke_ran=false
w1_skill_republish_smoke_ran=false
w1_skill_market_fixtures_present=false
w1_skill_market_public_read=false
w1_skill_market_safe_projection=false
w1_skill_market_keyset_pagination=false
w1_skill_market_governance_visibility=false
w1_skill_market_cursor_fail_closed=false
w1_skill_market_stale_selection_fail_closed=false
w1_public_market_quickcreate=false
w1_public_market_permission_identity_separation=false
w1_public_market_publisher_snapshot_frozen=false
w1_public_market_governance_toctou_closed=false
w1_public_market_mixed_binding_atomicity=false
w1_public_market_mixed_success=false
w1_public_market_login_preselection_recovered=false
w1_public_market_idempotency_frozen_replay=false
w1_skill_id=""
w1_review_id=""
w1_skill_name=""
w1_updated_skill_name=""
w1_market_draft_name=""
w1_offline_draft_name=""
w1_binding_prompt=""
w1_binding_project_id=""
w1_binding_session_id=""
w1_binding_input_id=""
w1_public_market_project_id=""
w1_public_market_session_id=""
w1_public_market_snapshot_before=""
w1_owner_private_skill_id=""
w1_owner_private_skill_name=""

stop_processes() {
  if [[ -n "$w1_browser_playwright_pid" ]] && kill -0 "$w1_browser_playwright_pid" 2>/dev/null; then
    kill -TERM "$w1_browser_playwright_pid" 2>/dev/null || true
    for _ in $(seq 1 40); do
      process_state="$(ps -o stat= -p "$w1_browser_playwright_pid" 2>/dev/null | tr -d '[:space:]' || true)"
      if ! kill -0 "$w1_browser_playwright_pid" 2>/dev/null || [[ "$process_state" == Z* ]]; then
        break
      fi
      sleep 0.25
    done
    if kill -0 "$w1_browser_playwright_pid" 2>/dev/null; then
      kill -KILL "$w1_browser_playwright_pid" 2>/dev/null || true
    fi
  fi
  if [[ -n "$w1_browser_playwright_pid" ]]; then
    wait "$w1_browser_playwright_pid" 2>/dev/null || true
    w1_browser_playwright_pid=""
  fi
  if [[ -n "$w05_retention_injector_pid" ]] && kill -0 "$w05_retention_injector_pid" 2>/dev/null; then
    kill -TERM "$w05_retention_injector_pid" 2>/dev/null || true
    for _ in $(seq 1 40); do
      if ! kill -0 "$w05_retention_injector_pid" 2>/dev/null; then
        break
      fi
      sleep 0.25
    done
    if kill -0 "$w05_retention_injector_pid" 2>/dev/null; then
      kill -KILL "$w05_retention_injector_pid" 2>/dev/null || true
    fi
  fi
  if [[ -n "$w05_retention_injector_pid" ]]; then
    wait "$w05_retention_injector_pid" 2>/dev/null || true
    w05_retention_injector_pid=""
  fi
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
  if [[ -n "$governor_cookie_jar" ]]; then
    rm -f "$governor_cookie_jar"
  fi
  if [[ -n "$governor_curl_config" ]]; then
    rm -f "$governor_curl_config"
  fi
  if [[ -n "$governor_login_response_temp" ]]; then
    rm -f "$governor_login_response_temp"
  fi
  if [[ -n "$source_manifest_temp" ]]; then
    rm -f "$source_manifest_temp"
  fi
  if [[ -n "$w05_browser_result_temp" ]]; then
    rm -f "$w05_browser_result_temp"
  fi
  if [[ -n "$w05_retention_control_dir" ]]; then
    rm -rf "$w05_retention_control_dir"
  fi
  if [[ -n "$w1_public_market_control_dir" ]]; then
    rm -rf "$w1_public_market_control_dir"
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
  local background_pid=""
  trap - EXIT
  if [[ "$w1_toctou_lock_database_pid" =~ ^[0-9]+$ && -n "$postgres_container" ]]; then
    docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc \
      "SELECT pg_terminate_backend($w1_toctou_lock_database_pid);" >/dev/null 2>&1 || true
  fi
  for background_pid in "$w1_toctou_quick_shell_pid" "$w1_toctou_governance_shell_pid" "$w1_toctou_lock_shell_pid"; do
    if [[ "$background_pid" =~ ^[0-9]+$ ]]; then
      kill "$background_pid" >/dev/null 2>&1 || true
      wait "$background_pid" >/dev/null 2>&1 || true
    fi
  done
  if [[ "$w1_skill_market_fixtures_present" == "true" && -n "$postgres_container" ]]; then
    cleanup_w1_skill_market_fixtures "$postgres_container" >/dev/null 2>&1 || true
  fi
  stop_processes
  if [[ "$exit_code" -ne 0 ]]; then
    # 本次运行已在启动时撤销 current manifest；失败时清除未提交 release，避免部分 sidecar 假绿。
    rm -f "$evidence_file" "${evidence_file}.tmp"
    if [[ -n "$governance_evidence_file" ]]; then
      rm -f "$governance_evidence_file" "${governance_evidence_file}.tmp"
    fi
    if [[ -n "$skill_market_evidence_file" ]]; then
      rm -f "$skill_market_evidence_file" "${skill_market_evidence_file}.tmp"
    fi
    if [[ -n "$skill_market_binding_evidence_file" ]]; then
      rm -f "$skill_market_binding_evidence_file" "${skill_market_binding_evidence_file}.tmp"
    fi
    if [[ -n "$skill_republish_evidence_file" ]]; then
      rm -f "$skill_republish_evidence_file" "${skill_republish_evidence_file}.tmp"
    fi
    if [[ -n "$w1_evidence_current_manifest" ]]; then
      rm -f "$w1_evidence_current_manifest" "$w1_evidence_release_root/.current-${run_id}.tmp"
    fi
    if [[ -n "$w1_evidence_release_dir" ]]; then
      rm -rf "$w1_evidence_release_dir" "$w1_evidence_release_root/.${run_id}.staging"
    fi
    if [[ -n "$evidence_dir" && "$evidence_dir" == "$repo_root/.local/smoke/"* ]]; then
      rm -rf "$evidence_dir"
    fi
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

run_w05_retention_window_injector() {
  local request_file="$w05_retention_control_dir/request.json"
  local ack_file="$w05_retention_control_dir/ack.json"
  local ack_temp="$ack_file.$$.tmp"
  local request_ready=false
  local project_id=""
  local session_id=""
  local input_id=""
  local event_id=""
  local injection_fact=""
  local final_fact=""
  local inserted_events=""
  local pruned_events=""
  local advanced_rows=""
  local last_seq=""
  local min_available_seq=""
  local retained_events=""
  local retained_min_seq=""
  local retained_max_seq=""

  for _ in $(seq 1 600); do
    if [[ -s "$request_file" ]] && jq -e '
      keys == ["event_id","input_id","project_id","schema_version","session_id"]
      and .schema_version == "w05.retention-window.fixture.request.v1"
      and all(.project_id,.session_id,.input_id,.event_id;
        test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))' \
      "$request_file" >/dev/null 2>&1; then
      request_ready=true
      break
    fi
    sleep 0.05
  done
  [[ "$request_ready" == "true" ]] || return 41

  project_id="$(jq -er '.project_id' "$request_file")"
  session_id="$(jq -er '.session_id' "$request_file")"
  input_id="$(jq -er '.input_id' "$request_file")"
  event_id="$(jq -er '.event_id' "$request_file")"

  injection_fact="$(docker exec -i -e PGOPTIONS='-c statement_timeout=10000 -c lock_timeout=5000' \
    "$postgres_container" psql -v ON_ERROR_STOP=1 -qAtF '|' \
    -U dora_admin -d dora_agent \
    -v project_id="$project_id" -v session_id="$session_id" -v input_id="$input_id" -v event_id="$event_id" <<'SQL'
      WITH target AS MATERIALIZED (
        SELECT counter.session_id
        FROM agent.session_event_counter AS counter
        JOIN agent.session AS session_record ON session_record.id = counter.session_id
        WHERE counter.session_id = :'session_id'::uuid
          AND session_record.project_id = :'project_id'::uuid
          AND counter.last_seq = 2
          AND counter.min_available_seq = 1
          AND ARRAY(
            SELECT retained.seq
            FROM agent.session_event_log AS retained
            WHERE retained.session_id = counter.session_id
            ORDER BY retained.seq
          ) = ARRAY[1::bigint, 2::bigint]
        FOR UPDATE OF counter
      ), source_event AS MATERIALIZED (
        SELECT event_record.*
        FROM agent.session_event_log AS event_record
        JOIN target ON target.session_id = event_record.session_id
        WHERE event_record.seq = 2
          AND event_record.event_type = 'session.input.accepted'
          AND event_record.schema_version = 'session.event.v1'
          AND event_record.aggregate_type = 'session_input'
          AND event_record.aggregate_id = :'input_id'::uuid
          AND event_record.aggregate_version = 1
      ), inserted AS (
        INSERT INTO agent.session_event_log (
          event_id, session_id, seq, event_type, schema_version, source_kind, source_id,
          projection_index, aggregate_type, aggregate_id, aggregate_version, payload, created_at
        )
        SELECT :'event_id'::uuid, source_event.session_id, 3, source_event.event_type,
          source_event.schema_version, 'w05_retention_fixture', :'event_id'::uuid, 0,
          source_event.aggregate_type, source_event.aggregate_id, source_event.aggregate_version,
          source_event.payload, clock_timestamp()
        FROM source_event
        RETURNING session_id, seq
      ), pruned AS (
        DELETE FROM agent.session_event_log AS event_record
        USING inserted
        WHERE event_record.session_id = inserted.session_id AND event_record.seq < 3
        RETURNING event_record.seq
      ), advanced AS (
        UPDATE agent.session_event_counter AS counter
        SET last_seq = 3, min_available_seq = 3, updated_at = clock_timestamp()
        FROM inserted
        WHERE counter.session_id = inserted.session_id
          AND counter.last_seq = 2
          AND counter.min_available_seq = 1
        RETURNING counter.last_seq, counter.min_available_seq
      )
      SELECT (SELECT count(*) FROM inserted), (SELECT count(*) FROM pruned),
        (SELECT count(*) FROM advanced), (SELECT last_seq FROM advanced),
        (SELECT min_available_seq FROM advanced);
SQL
  )" || return 42
  IFS='|' read -r inserted_events pruned_events advanced_rows last_seq min_available_seq <<<"$injection_fact"
  [[ "$inserted_events" == "1" && "$pruned_events" == "2" && "$advanced_rows" == "1" && \
    "$last_seq" == "3" && "$min_available_seq" == "3" ]] || return 43

  final_fact="$(docker exec -i -e PGOPTIONS='-c statement_timeout=10000 -c lock_timeout=5000' \
    "$postgres_container" psql -v ON_ERROR_STOP=1 -qAtF '|' \
    -U dora_admin -d dora_agent -v session_id="$session_id" <<'SQL'
      SELECT count(event_record.seq), min(event_record.seq), max(event_record.seq),
        counter.last_seq, counter.min_available_seq
      FROM agent.session_event_counter AS counter
      JOIN agent.session_event_log AS event_record ON event_record.session_id = counter.session_id
      WHERE counter.session_id = :'session_id'::uuid
      GROUP BY counter.last_seq, counter.min_available_seq;
SQL
  )" || return 44
  IFS='|' read -r retained_events retained_min_seq retained_max_seq last_seq min_available_seq <<<"$final_fact"
  [[ "$retained_events" == "1" && "$retained_min_seq" == "3" && "$retained_max_seq" == "3" && \
    "$last_seq" == "3" && "$min_available_seq" == "3" ]] || return 45

  jq -n --arg project_id "$project_id" --arg session_id "$session_id" --arg input_id "$input_id" \
    --arg event_id "$event_id" \
    '{schema_version:"w05.retention-window.fixture.ack.v1",project_id:$project_id,session_id:$session_id,
      input_id:$input_id,event_id:$event_id,inserted_events:1,pruned_events:2,last_seq:3,
      min_available_seq:3,retained_event_seq:3}' >"$ack_temp" || return 46
  chmod 600 "$ack_temp"
  mv "$ack_temp" "$ack_file"
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
  local request_cookie_jar="${3:-$cookie_jar}"
  local status=""
  for _ in $(seq 1 120); do
    status="$(curl --silent --show-error --max-time 3 -b "$request_cookie_jar" \
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
  local request_cookie_jar="${5:-$cookie_jar}"
  local request_pids=()
  local index=""
  mkdir -p "$batch_dir"
  for index in $(seq 1 100); do
    curl_with_body_stdin "$payload" --silent --show-error --max-time 10 -b "$request_cookie_jar" \
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
  local extra_literals="${7:-[]}"
  jq -e --arg code "$expected_code" --arg project "$project_id" --arg session "$session_id" \
    --arg input "$input_id" --arg prompt "$prompt" --argjson extra_literals "$extra_literals" \
    'keys == ["error"]
     and (.error | keys) == ["code", "details", "message", "request_id", "retryable"]
     and .error.code == $code
     and (.error.message | type) == "string"
     and (.error.request_id | type) == "string"
     and .error.retryable == false
     and .error.details == {}
     and all(.. | strings; . as $value
       | ((contains($project) or contains($session) or contains($input) or contains($prompt)
         or any($extra_literals[]; . as $literal | $value | contains($literal))) | not))' \
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

sha256_text() {
  local value="$1"
  local output=""
  local digest=""
  output="$(printf '%s' "$value" | shasum -a 256)" || return 1
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

# write_conditional_curl_config 把 CSRF、If-Match 与幂等键写入 0600 curl 配置，避免原值进入进程参数和 Evidence。
write_conditional_curl_config() {
  local output_file="$1"
  local base_config="$2"
  local if_match="${3:-}"
  local idempotency_key="${4:-}"
  local if_match_file="${output_file}.if-match"
  local idempotency_file="${output_file}.idempotency"
  [[ -f "$base_config" ]] || return 1
  if [[ -n "$if_match" ]]; then
    write_curl_header_config "$if_match_file" 'If-Match' "$if_match" || return 1
  else
    (umask 077; : >"$if_match_file") || return 1
    chmod 600 "$if_match_file" || return 1
  fi
  if [[ -n "$idempotency_key" ]]; then
    write_curl_header_config "$idempotency_file" 'Idempotency-Key' "$idempotency_key" || return 1
  else
    (umask 077; : >"$idempotency_file") || return 1
    chmod 600 "$idempotency_file" || return 1
  fi
  (umask 077; cat "$base_config" "$if_match_file" "$idempotency_file" >"$output_file") || return 1
  chmod 600 "$output_file" || return 1
  rm -f "$if_match_file" "$idempotency_file"
}

# assert_governance_decision_response 验证治理成功响应 exact-set、安全字段、Strong ETag 与 no-store Header。
assert_governance_decision_response() {
  local response_file="$1"
  local headers_file="$2"
  local expected_status="$3"
  local expected_epoch="$4"
  local expected_actions="$5"
  local body_etag=""
  body_etag="$(jq -er --arg skill "$w1_skill_id" --arg status "$expected_status" \
    --argjson epoch "$expected_epoch" --argjson actions "$expected_actions" '
      (keys == ["request_id","skill"]
      and (.request_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and (.skill | keys) == ["allowed_actions","governance_epoch","governance_etag","governance_status","skill_id","transitioned_at"]
      and .skill.skill_id == $skill
      and .skill.governance_status == $status
      and .skill.governance_epoch == $epoch
      and .skill.allowed_actions == $actions
      and (.skill.transitioned_at | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{1,9})?Z$"))
      and (.skill.governance_etag | test("^\\\"sg1-[A-Za-z0-9_-]{43}\\\"$")))
      as $valid
      | if $valid then .skill.governance_etag else error("invalid governance decision response") end' "$response_file")" || return 1
  [[ "$(response_header_value "$headers_file" 'ETag')" == "$body_etag" ]] || return 1
  [[ "$(response_header_value "$headers_file" 'Cache-Control')" == "no-store" ]] || return 1
}

# assert_governance_error_response 验证治理失败使用统一安全 Envelope、UUIDv7 request_id 与 no-store。
assert_governance_error_response() {
  local response_file="$1"
  local headers_file="$2"
  local expected_code="$3"
  jq -e --arg code "$expected_code" '
    keys == ["error"]
    and (.error | keys) == ["code","details","message","request_id","retryable"]
    and .error.code == $code
    and (.error.message | type) == "string" and (.error.message | length) > 0
    and (.error.request_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
    and .error.retryable == false and .error.details == {}' "$response_file" >/dev/null || return 1
  [[ "$(response_header_value "$headers_file" 'Cache-Control')" == "no-store" ]] || return 1
}

# read_governance_quickcreate_counts 用固定 SQL 读取候选 Project 全事实计数，验证治理失败没有部分提交。
read_governance_quickcreate_counts() {
  local postgres_container="$1"
  docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'projects', (SELECT COUNT(*) FROM business.project),
      'receipts', (SELECT COUNT(*) FROM business.project_creation_receipt),
      'binding_sets', (SELECT COUNT(*) FROM business.project_skill_binding_set),
      'bindings', (SELECT COUNT(*) FROM business.project_skill_binding),
      'binding_audits', (SELECT COUNT(*) FROM business.project_skill_binding_audit),
      'resolutions', (SELECT COUNT(*) FROM business.project_session_skill_resolution),
      'resolution_items', (SELECT COUNT(*) FROM business.project_session_skill_resolution_item),
      'session_bindings', (SELECT COUNT(*) FROM business.project_session_binding),
      'outboxes', (SELECT COUNT(*) FROM business.project_session_outbox)
    );"
}

# wait_w1_public_market_checkpoint 等待浏览器原子发布阶段检查点，并校验 exact-set、身份与 0600 权限。
wait_w1_public_market_checkpoint() {
  local checkpoint_file="$1"
  local expected_phase="$2"
  local playwright_pid="$3"
  local expected_skill_id="${4:-}"
  local process_state=""
  local file_mode=""
  local max_attempts="600"

  # 首个 checkpoint 需要覆盖 Playwright 的 180s 全局动作预算；第二阶段仍保持 30s 快速失败。
  if [[ "$expected_phase" == "before_login" ]]; then
    max_attempts="3600"
  fi

  for _ in $(seq 1 "$max_attempts"); do
    if [[ -s "$checkpoint_file" ]]; then
      file_mode="$(stat -c '%a' "$checkpoint_file" 2>/dev/null || stat -f '%Lp' "$checkpoint_file" 2>/dev/null || true)"
      [[ "$file_mode" == "600" ]] || fail "W1 Public Market ${expected_phase} checkpoint 权限不是 0600"
      jq -e --arg phase "$expected_phase" --arg consumer "$owner_b_user_id" --arg skill "$expected_skill_id" '
        keys == ["consumer_id","phase","quickcreate_count","schema_version","skill_id"]
        and .schema_version == "w1.public-market-preselection.checkpoint.v1"
        and .phase == $phase and .consumer_id == $consumer
        and (if $skill == "" then (.skill_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")) else .skill_id == $skill end)
        and .quickcreate_count == 0' "$checkpoint_file" >/dev/null || \
        fail "W1 Public Market ${expected_phase} checkpoint 契约、身份或零请求事实漂移"
      return 0
    fi
    process_state="$(ps -o stat= -p "$playwright_pid" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$playwright_pid" 2>/dev/null || [[ "$process_state" == Z* ]]; then
      sed -n '1,240p' "$evidence_dir/frontend-w1-playwright.log" >&2 || true
      fail "W1 Playwright 在 ${expected_phase} checkpoint 前提前退出"
    fi
    sleep 0.05
  done
  fail "等待 W1 Public Market ${expected_phase} checkpoint 超时"
}

# write_w1_public_market_ack 仅在数据库检查通过后原子放行浏览器下一阶段。
write_w1_public_market_ack() {
  local phase="$1"
  local counts="$2"
  local skill_id="$3"
  local ack_file="$w1_public_market_control_dir/${phase}.ack.json"
  local ack_temp="$ack_file.$$.tmp"
  local file_mode=""

  jq -n --arg phase "$phase" --arg consumer "$owner_b_user_id" --arg skill "$skill_id" \
    --argjson counts "$counts" '
    {schema_version:"w1.public-market-preselection.database-ack.v1",phase:$phase,
      consumer_id:$consumer,skill_id:$skill,quickcreate_count:0,database_counts:$counts,accepted:true}' \
    >"$ack_temp" || fail "W1 Public Market ${phase} ACK 生成失败"
  chmod 600 "$ack_temp"
  mv "$ack_temp" "$ack_file"
  file_mode="$(stat -c '%a' "$ack_file" 2>/dev/null || stat -f '%Lp' "$ack_file" 2>/dev/null || true)"
  [[ "$file_mode" == "600" ]] || fail "W1 Public Market ${phase} ACK 权限不是 0600"
  jq -e --arg phase "$phase" --arg consumer "$owner_b_user_id" --arg skill "$skill_id" '
    keys == ["accepted","consumer_id","database_counts","phase","quickcreate_count","schema_version","skill_id"]
    and .schema_version == "w1.public-market-preselection.database-ack.v1"
    and .phase == $phase and .consumer_id == $consumer and .skill_id == $skill
    and .quickcreate_count == 0 and .accepted == true
    and (.database_counts | keys) == ["binding_audits","binding_sets","bindings","outboxes","projects","receipts","resolution_items","resolutions","session_bindings"]' \
    "$ack_file" >/dev/null || fail "W1 Public Market ${phase} ACK 契约漂移"
}

# run_w1_public_market_preselection_controller 用两个浏览器阶段点证明登录恢复未创建九类 QuickCreate 事实。
run_w1_public_market_preselection_controller() {
  local playwright_pid="$1"
  local before_login_checkpoint="$w1_public_market_control_dir/before_login.checkpoint.json"
  local before_submit_checkpoint="$w1_public_market_control_dir/before_submit.checkpoint.json"
  local fact_file="$evidence_dir/responses/w1-browser-public-market-preselection-database.json"
  local before_login_counts=""
  local before_submit_counts=""
  local browser_skill_id=""

  wait_w1_public_market_checkpoint "$before_login_checkpoint" "before_login" "$playwright_pid"
  browser_skill_id="$(jq -er '.skill_id' "$before_login_checkpoint")" || \
    fail "W1 Public Market 登录前 checkpoint 缺少浏览器现场 Skill"
  before_login_counts="$(read_governance_quickcreate_counts "$postgres_container")" || \
    fail "W1 Public Market 登录前九类数据库计数读取失败"
  jq -e '
    keys == ["binding_audits","binding_sets","bindings","outboxes","projects","receipts","resolution_items","resolutions","session_bindings"]
    and all(.[]; ((. | type) == "number" and . >= 0 and . == floor))' <<<"$before_login_counts" >/dev/null || \
    fail "W1 Public Market 登录前九类数据库计数契约漂移"
  write_w1_public_market_ack "before_login" "$before_login_counts" "$browser_skill_id"

  wait_w1_public_market_checkpoint "$before_submit_checkpoint" "before_submit" "$playwright_pid" "$browser_skill_id"
  before_submit_counts="$(read_governance_quickcreate_counts "$postgres_container")" || \
    fail "W1 Public Market 显式提交前九类数据库计数读取失败"
  jq -ne --argjson before "$before_login_counts" --argjson after "$before_submit_counts" \
    '$before == $after' >/dev/null || \
    fail "W1 Public Market 登录预选在显式提交前留下九类数据库增量"

  jq -n --arg consumer "$owner_b_user_id" --arg skill "$browser_skill_id" \
    --argjson before "$before_login_counts" --argjson after "$before_submit_counts" '
    {schema_version:"w1.public-market-preselection.database-fact.v1",consumer_id:$consumer,skill_id:$skill,
      before_login:$before,before_submit:$after,database_counts_unchanged:($before == $after)}' >"$fact_file"
  jq -e --arg consumer "$owner_b_user_id" --arg skill "$browser_skill_id" '
    keys == ["before_login","before_submit","consumer_id","database_counts_unchanged","schema_version","skill_id"]
    and .schema_version == "w1.public-market-preselection.database-fact.v1"
    and .consumer_id == $consumer and .skill_id == $skill and .database_counts_unchanged
    and .before_login == .before_submit
    and (.before_login | keys) == ["binding_audits","binding_sets","bindings","outboxes","projects","receipts","resolution_items","resolutions","session_bindings"]
    and all(.before_login[]; ((. | type) == "number" and . >= 0 and . == floor))' "$fact_file" >/dev/null || \
    fail "W1 Public Market 登录预选数据库双阶段事实漂移"
  write_w1_public_market_ack "before_submit" "$before_submit_counts" "$browser_skill_id"
}

# assert_w1_skill_market_list_contract 验证公开列表 exact-set、发布快照投影与 no-store。
assert_w1_skill_market_list_contract() {
  local response_file="$1"
  local headers_file="$2"
  local expected_visible="$3"
  jq -e --arg skill "$w1_skill_id" --arg name "$w1_updated_skill_name" \
    --arg owner "$user_id" --arg display_name "$DORA_SMOKE_USER_DISPLAY_NAME" \
    --argjson visible "$expected_visible" '
    keys == ["items","next_cursor","request_id"]
    and (.request_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
    and (.next_cursor == null or ((.next_cursor | type) == "string" and (.next_cursor | length) > 0))
    and (.items | type) == "array"
    and all(.items[];
      (keys == ["category","cover_asset","declared_capability_keys","name","published_at","publisher","skill_id","summary","tags"])
      and (.publisher | keys) == ["display_name","publisher_id"]
      and (.skill_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and (.publisher.publisher_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and (.publisher.display_name | type) == "string" and (.publisher.display_name | length) > 0
      and (.published_at | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{1,9})?Z$"))
      and .cover_asset == null and (.tags | type) == "array" and (.declared_capability_keys | type) == "array")
    and (if $visible == null then true
      else ([.items[] | select(.skill_id == $skill)] | length) == (if $visible then 1 else 0 end) end)
    and (if $visible == true then
      ([.items[] | select(.skill_id == $skill)][0]
        | .name == $name
        and .summary == "真实 W1 Skill Foundation 冒烟摘要 updated"
        and .category == "smoke" and .tags == ["alpha","beta"] and .cover_asset == null
        and .publisher == {publisher_id:$owner,display_name:$display_name}
        and .declared_capability_keys == ["plan_creation_spec","analyze_materials","plan_storyboard","generate_media","write_prompts","assemble_output"])
      else true end)' "$response_file" >/dev/null || return 1
  [[ "$(response_header_value "$headers_file" 'Cache-Control')" == "no-store" ]]
}

# assert_w1_skill_market_detail_contract 验证详情只包含冻结白名单且来自 current published snapshot。
assert_w1_skill_market_detail_contract() {
  local response_file="$1"
  local headers_file="$2"
  jq -e --arg skill "$w1_skill_id" --arg name "$w1_updated_skill_name" \
    --arg owner "$user_id" --arg display_name "$DORA_SMOKE_USER_DISPLAY_NAME" '
    keys == ["request_id","skill"]
    and (.request_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
    and (.skill | keys) == ["category","copyright_notice","cover_asset","declared_capability_keys","examples","input_description","market_detail","name","output_description","published_at","publisher","skill_id","starter_prompts","summary","tags","user_notice"]
    and .skill.skill_id == $skill and .skill.name == $name
    and .skill.summary == "真实 W1 Skill Foundation 冒烟摘要 updated"
    and .skill.category == "smoke" and .skill.tags == ["alpha","beta"] and .skill.cover_asset == null
    and .skill.publisher == {publisher_id:$owner,display_name:$display_name}
    and (.skill.published_at | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{1,9})?Z$"))
    and .skill.declared_capability_keys == ["plan_creation_spec","analyze_materials","plan_storyboard","generate_media","write_prompts","assemble_output"]
    and .skill.input_description == "输入真实业务目标与素材约束"
    and .skill.output_description == "输出可审核的结构化创作方案"
    and (.skill.examples | type) == "array" and (.skill.examples | length) == 2
    and (.skill.starter_prompts | type) == "array" and (.skill.starter_prompts | length) == 2
    and .skill.market_detail == "用于 W1 真实链路验收"
    and .skill.copyright_notice == "仅用于本地冒烟"
    and .skill.user_notice == "不得用于生产内容"' "$response_file" >/dev/null || return 1
  [[ "$(response_header_value "$headers_file" 'Cache-Control')" == "no-store" ]]
}

# assert_w1_skill_market_error_response 验证公开 Market 的 fail-closed 安全错误 Envelope。
assert_w1_skill_market_error_response() {
  local response_file="$1"
  local headers_file="$2"
  local expected_code="$3"
  jq -e --arg code "$expected_code" '
    keys == ["error"]
    and (.error | keys) == ["code","details","message","request_id","retryable"]
    and .error.code == $code and (.error.message | type) == "string" and (.error.message | length) > 0
    and (.error.request_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
    and .error.retryable == false and .error.details == {}' "$response_file" >/dev/null || return 1
  [[ "$(response_header_value "$headers_file" 'Cache-Control')" == "no-store" ]]
}

# assert_w1_skill_market_visibility 在治理转换后从匿名真实 HTTP 路径确认列表与详情同步可见性。
assert_w1_skill_market_visibility() {
  local state="$1"
  local expected_visible="$2"
  local list_response="$w1_temp_dir/market-${state}-list.json"
  local list_headers="$w1_temp_dir/market-${state}-list.headers"
  local detail_response="$w1_temp_dir/market-${state}-detail.json"
  local detail_headers="$w1_temp_dir/market-${state}-detail.headers"
  local list_status=""
  local detail_status=""
  : >"$list_headers"
  list_status="$(curl --silent --show-error --max-time 10 -D "$list_headers" -o "$list_response" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/skill-market')"
  [[ "$list_status" == "200" ]] || return 1
  assert_w1_skill_market_list_contract "$list_response" "$list_headers" "$expected_visible" || return 1
  : >"$detail_headers"
  detail_status="$(curl --silent --show-error --max-time 10 -D "$detail_headers" -o "$detail_response" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/skill-market/${w1_skill_id}")"
  if [[ "$expected_visible" == "true" ]]; then
    [[ "$detail_status" == "200" ]] || return 1
    assert_w1_skill_market_detail_contract "$detail_response" "$detail_headers" || return 1
  else
    [[ "$detail_status" == "404" ]] || return 1
    assert_w1_skill_market_error_response "$detail_response" "$detail_headers" "SKILL_MARKET_NOT_FOUND" || return 1
  fi
}

# seed_w1_skill_market_fixtures 从真实发布快照克隆 21 个逻辑一致的 active current-published fixture。
seed_w1_skill_market_fixtures() {
  local postgres_container="$1"
  docker exec -i "$postgres_container" psql -U dora_admin -d dora_business -qAt -v ON_ERROR_STOP=1 \
    -v smoke_run_id="$run_id" -v source_skill_id="$w1_skill_id" -v owner_user_id="$user_id" \
    -v reviewer_user_id="$owner_b_seed_user_id" <<'SQL'
WITH source AS (
  SELECT published.definition_schema_version, published.definition_json, published.content_digest
  FROM business.skill AS skill_record
  JOIN business.skill_published_snapshot AS published
    ON published.id = skill_record.current_published_snapshot_id AND published.skill_id = skill_record.id
  WHERE skill_record.id = :'source_skill_id'::uuid
), raw_fixture AS (
  SELECT fixture_no,
    md5(:'smoke_run_id' || ':market-skill:' || fixture_no::text) AS skill_hash,
    md5(:'smoke_run_id' || ':market-revision:' || fixture_no::text) AS revision_hash,
    md5(:'smoke_run_id' || ':market-review:' || fixture_no::text) AS review_hash,
    md5(:'smoke_run_id' || ':market-snapshot:' || fixture_no::text) AS snapshot_hash
  FROM generate_series(1, 21) AS fixture_no
), fixture AS (
  SELECT fixture_no,
    format('%s-%s-7%s-8%s-%s', substr(skill_hash,1,8), substr(skill_hash,9,4), substr(skill_hash,14,3), substr(skill_hash,18,3), substr(skill_hash,21,12))::uuid AS skill_id,
    format('%s-%s-7%s-8%s-%s', substr(revision_hash,1,8), substr(revision_hash,9,4), substr(revision_hash,14,3), substr(revision_hash,18,3), substr(revision_hash,21,12))::uuid AS revision_id,
    format('%s-%s-7%s-8%s-%s', substr(review_hash,1,8), substr(review_hash,9,4), substr(review_hash,14,3), substr(review_hash,18,3), substr(review_hash,21,12))::uuid AS review_id,
    format('%s-%s-7%s-8%s-%s', substr(snapshot_hash,1,8), substr(snapshot_hash,9,4), substr(snapshot_hash,14,3), substr(snapshot_hash,18,3), substr(snapshot_hash,21,12))::uuid AS snapshot_id
  FROM raw_fixture
), inserted_revision AS (
  INSERT INTO business.skill_content_revision
    (id, skill_id, revision_no, definition_schema_version, definition_json, content_digest, created_by_user_id, created_at)
  SELECT fixture.revision_id, fixture.skill_id, 1, source.definition_schema_version, source.definition_json,
    source.content_digest, :'owner_user_id'::uuid, '2099-01-01T00:00:00Z'::timestamptz
  FROM fixture CROSS JOIN source
  RETURNING skill_id
), inserted_review AS (
  INSERT INTO business.skill_review_submission
    (id, skill_id, content_revision_id, content_digest, status, safe_reason_code, version,
      submitted_by_user_id, decided_by_user_id, submitted_at, decided_at, updated_at)
  SELECT fixture.review_id, fixture.skill_id, fixture.revision_id, source.content_digest, 'approved', NULL, 2,
    :'owner_user_id'::uuid, :'reviewer_user_id'::uuid, '2099-01-01T00:00:00Z'::timestamptz,
    '2099-01-01T00:00:00Z'::timestamptz, '2099-01-01T00:00:00Z'::timestamptz
  FROM fixture CROSS JOIN source
  RETURNING skill_id
), inserted_snapshot AS (
  INSERT INTO business.skill_published_snapshot
    (id, skill_id, source_content_revision_id, review_submission_id, publication_revision,
      definition_schema_version, definition_json, content_digest, published_by_user_id, published_at)
  SELECT fixture.snapshot_id, fixture.skill_id, fixture.revision_id, fixture.review_id, 1,
    source.definition_schema_version, source.definition_json, source.content_digest,
    :'reviewer_user_id'::uuid, '2099-01-01T00:00:00Z'::timestamptz
  FROM fixture CROSS JOIN source
  RETURNING skill_id
), inserted_skill AS (
  INSERT INTO business.skill
    (id, owner_user_id, current_draft_revision_id, current_published_snapshot_id,
      publication_revision, governance_status, version, created_at, updated_at)
  SELECT fixture.skill_id, :'owner_user_id'::uuid, fixture.revision_id, fixture.snapshot_id,
    1, 'active', 1, '2099-01-01T00:00:00Z'::timestamptz, '2099-01-01T00:00:00Z'::timestamptz
  FROM fixture CROSS JOIN source
  RETURNING id
)
SELECT COALESCE(json_agg(id::text ORDER BY id DESC), '[]'::json) FROM inserted_skill;
SQL
}

# cleanup_w1_skill_market_fixtures 精确删除本次 21 个 fixture；仅本地 Smoke 管理会话绕过 immutable 删除触发器。
cleanup_w1_skill_market_fixtures() {
  local postgres_container="$1"
  local cleanup_fact=""
  cleanup_fact="$(docker exec -i "$postgres_container" psql -U dora_admin -d dora_business -qAt -v ON_ERROR_STOP=1 \
    -v smoke_run_id="$run_id" <<'SQL'
BEGIN;
SET LOCAL session_replication_role = replica;
WITH raw_fixture AS (
  SELECT fixture_no,
    md5(:'smoke_run_id' || ':market-skill:' || fixture_no::text) AS skill_hash,
    md5(:'smoke_run_id' || ':market-revision:' || fixture_no::text) AS revision_hash,
    md5(:'smoke_run_id' || ':market-review:' || fixture_no::text) AS review_hash,
    md5(:'smoke_run_id' || ':market-snapshot:' || fixture_no::text) AS snapshot_hash
  FROM generate_series(1, 21) AS fixture_no
), fixture AS (
  SELECT
    format('%s-%s-7%s-8%s-%s', substr(skill_hash,1,8), substr(skill_hash,9,4), substr(skill_hash,14,3), substr(skill_hash,18,3), substr(skill_hash,21,12))::uuid AS skill_id,
    format('%s-%s-7%s-8%s-%s', substr(revision_hash,1,8), substr(revision_hash,9,4), substr(revision_hash,14,3), substr(revision_hash,18,3), substr(revision_hash,21,12))::uuid AS revision_id,
    format('%s-%s-7%s-8%s-%s', substr(review_hash,1,8), substr(review_hash,9,4), substr(review_hash,14,3), substr(review_hash,18,3), substr(review_hash,21,12))::uuid AS review_id,
    format('%s-%s-7%s-8%s-%s', substr(snapshot_hash,1,8), substr(snapshot_hash,9,4), substr(snapshot_hash,14,3), substr(snapshot_hash,18,3), substr(snapshot_hash,21,12))::uuid AS snapshot_id
  FROM raw_fixture
), deleted_skill AS (
  DELETE FROM business.skill WHERE id IN (SELECT skill_id FROM fixture) RETURNING id
), deleted_snapshot AS (
  DELETE FROM business.skill_published_snapshot WHERE id IN (SELECT snapshot_id FROM fixture) RETURNING id
), deleted_review AS (
  DELETE FROM business.skill_review_submission WHERE id IN (SELECT review_id FROM fixture) RETURNING id
), deleted_revision AS (
  DELETE FROM business.skill_content_revision WHERE id IN (SELECT revision_id FROM fixture) RETURNING id
)
SELECT json_build_object(
  'skills', (SELECT count(*) FROM deleted_skill),
  'snapshots', (SELECT count(*) FROM deleted_snapshot),
  'reviews', (SELECT count(*) FROM deleted_review),
  'revisions', (SELECT count(*) FROM deleted_revision));
COMMIT;
SQL
)" || return 1
  printf '%s' "$cleanup_fact"
}

# run_w1_skill_market_keyset_smoke 通过真实 HTTP 两页读取验证 20 条边界、同时间 ID 倒序和无重无漏。
run_w1_skill_market_keyset_smoke() {
  local postgres_container="$1"
  local first_response="$w1_temp_dir/market-keyset-page-one.json"
  local first_headers="$w1_temp_dir/market-keyset-page-one.headers"
  local second_response="$w1_temp_dir/market-keyset-page-two.json"
  local second_headers="$w1_temp_dir/market-keyset-page-two.headers"
  local expected_fixture_ids=""
  local next_cursor=""
  local cleanup_fact=""
  local status=""

  w1_skill_market_fixtures_present=true
  expected_fixture_ids="$(seed_w1_skill_market_fixtures "$postgres_container")" || fail "Market Keyset fixture 写入失败"
  jq -e 'type == "array" and length == 21 and (unique | length) == 21
    and all(.[]; test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-8[0-9a-f]{3}-[0-9a-f]{12}$"))' \
    <<<"$expected_fixture_ids" >/dev/null || fail "Market Keyset fixture ID 闭集漂移"

  : >"$first_headers"
  status="$(curl --silent --show-error --max-time 10 -D "$first_headers" -o "$first_response" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/skill-market')"
  [[ "$status" == "200" ]] || fail "Market Keyset 第一页状态为 $status"
  assert_w1_skill_market_list_contract "$first_response" "$first_headers" null || fail "Market Keyset 第一页安全契约漂移"
  jq -e --argjson expected "$expected_fixture_ids" '
    (.items | length) == 20 and (.next_cursor | type) == "string" and (.next_cursor | length) > 0
    and (.items | map(.skill_id)) == $expected[0:20]
    and ([.items[].published_at] | unique) == ["2099-01-01T00:00:00Z"]' "$first_response" >/dev/null || \
    fail "Market Keyset 第一页未满足 limit=20、同时间 ID 倒序或 cursor 边界"
  next_cursor="$(jq -er '.next_cursor' "$first_response")"

  : >"$second_headers"
  status="$(curl --silent --show-error --max-time 10 --get --data-urlencode "cursor=${next_cursor}" \
    -D "$second_headers" -o "$second_response" -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/skill-market')"
  [[ "$status" == "200" ]] || fail "Market Keyset 第二页状态为 $status"
  assert_w1_skill_market_list_contract "$second_response" "$second_headers" null || fail "Market Keyset 第二页安全契约漂移"
  jq -ne --argjson expected "$expected_fixture_ids" --slurpfile first "$first_response" --slurpfile second "$second_response" '
    (($first[0].items | map(.skill_id)) + ($second[0].items | map(.skill_id))) as $all_ids
    | ($all_ids[0:21] == $expected)
      and ($second[0].items[0].skill_id == $expected[20])
      and (([$first[0].items[].published_at] + [$second[0].items[0].published_at] | unique) == ["2099-01-01T00:00:00Z"])
      and (($all_ids | length) == ($all_ids | unique | length))' >/dev/null || \
    fail "Market Keyset 跨页发生重复、遗漏或顺序漂移"

  cleanup_fact="$(cleanup_w1_skill_market_fixtures "$postgres_container")" || fail "Market Keyset fixture 清理失败"
  jq -e '.skills == 21 and .snapshots == 21 and .reviews == 21 and .revisions == 21' \
    <<<"$cleanup_fact" >/dev/null || fail "Market Keyset fixture 未被精确清理"
  w1_skill_market_fixtures_present=false
  w1_skill_market_keyset_pagination=true
}

# read_governance_skill_state_fact 读取 offline 终态命令前后的最小聚合、回执与审计事实。
read_governance_skill_state_fact() {
  local postgres_container="$1"
  docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'version', skill_record.version,
      'governance_status', skill_record.governance_status,
      'governance_epoch', skill_record.governance_epoch,
      'current_published_snapshot_id', skill_record.current_published_snapshot_id,
      'publication_revision', skill_record.publication_revision,
      'governance_receipts', (SELECT COUNT(*) FROM business.skill_command_receipt WHERE result_skill_id = skill_record.id AND command_type = 'governance_transition'),
      'governance_audits', (SELECT COUNT(*) FROM business.skill_governance_audit WHERE skill_id = skill_record.id AND action IN ('governance_suspended','governance_resumed','governance_offlined'))
    ) FROM business.skill AS skill_record WHERE skill_record.id = '$w1_skill_id'::uuid;"
}

# read_existing_w1_session_snapshot_fact 读取治理前后既有 Session 的 Business resolution 与 Agent immutable snapshot 摘要。
read_existing_w1_session_snapshot_fact() {
  local postgres_container="$1"
  local business_fact=""
  local agent_fact=""
  business_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'resolution_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution WHERE project_id = '$w1_binding_project_id'::uuid),
      'item_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution_item AS item JOIN business.project_session_skill_resolution AS resolution ON resolution.id = item.resolution_id WHERE resolution.project_id = '$w1_binding_project_id'::uuid),
      'resolution_id', (SELECT id FROM business.project_session_skill_resolution WHERE project_id = '$w1_binding_project_id'::uuid),
      'snapshot_digest', (SELECT encode(snapshot_set_digest, 'hex') FROM business.project_session_skill_resolution WHERE project_id = '$w1_binding_project_id'::uuid),
      'published_snapshot_id', (SELECT item.published_snapshot_id FROM business.project_session_skill_resolution_item AS item JOIN business.project_session_skill_resolution AS resolution ON resolution.id = item.resolution_id WHERE resolution.project_id = '$w1_binding_project_id'::uuid AND item.skill_id = '$w1_skill_id'::uuid),
      'governance_epoch', (SELECT item.governance_epoch FROM business.project_session_skill_resolution_item AS item JOIN business.project_session_skill_resolution AS resolution ON resolution.id = item.resolution_id WHERE resolution.project_id = '$w1_binding_project_id'::uuid AND item.skill_id = '$w1_skill_id'::uuid),
      'content_digest', (SELECT encode(item.content_digest, 'hex') FROM business.project_session_skill_resolution_item AS item JOIN business.project_session_skill_resolution AS resolution ON resolution.id = item.resolution_id WHERE resolution.project_id = '$w1_binding_project_id'::uuid AND item.skill_id = '$w1_skill_id'::uuid),
      'runtime_content_digest', (SELECT encode(item.runtime_content_digest, 'hex') FROM business.project_session_skill_resolution_item AS item JOIN business.project_session_skill_resolution AS resolution ON resolution.id = item.resolution_id WHERE resolution.project_id = '$w1_binding_project_id'::uuid AND item.skill_id = '$w1_skill_id'::uuid)
    );")" || return 1
  agent_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent -Atc "
    SELECT json_build_object(
      'session_count', (SELECT COUNT(*) FROM agent.session WHERE id = '$w1_binding_session_id'::uuid AND project_id = '$w1_binding_project_id'::uuid),
      'snapshot_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot WHERE session_id = '$w1_binding_session_id'::uuid),
      'snapshot_kind', (SELECT snapshot_kind FROM agent.session_skill_snapshot WHERE session_id = '$w1_binding_session_id'::uuid),
      'snapshot_digest', (SELECT snapshot_digest FROM agent.session_skill_snapshot WHERE session_id = '$w1_binding_session_id'::uuid),
      'skill_count', (SELECT skill_count FROM agent.session_skill_snapshot WHERE session_id = '$w1_binding_session_id'::uuid),
      'item_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_binding_session_id'::uuid),
      'published_snapshot_id', (SELECT published_snapshot_id FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_binding_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'governance_epoch', (SELECT governance_epoch FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_binding_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'content_digest', (SELECT content_digest FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_binding_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'runtime_content_digest', (SELECT runtime_content_digest FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_binding_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid)
    );")" || return 1
  jq -cn --argjson business "$business_fact" --argjson agent "$agent_fact" '{business:$business,agent:$agent}'
}

# read_public_market_session_snapshot_fact 读取跨发布者 Project 的消费者、Publisher、权限与不可变 Snapshot 事实。
read_public_market_session_snapshot_fact() {
  local postgres_container="$1"
  local business_fact=""
  local agent_fact=""
  business_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'project_count', (SELECT COUNT(*) FROM business.project WHERE id = '$w1_public_market_project_id'::uuid AND owner_user_id = '$owner_b_user_id'::uuid),
      'receipt_count', (SELECT COUNT(*) FROM business.project_creation_receipt WHERE project_id = '$w1_public_market_project_id'::uuid),
      'binding_set_count', (SELECT COUNT(*) FROM business.project_skill_binding_set WHERE project_id = '$w1_public_market_project_id'::uuid AND owner_user_id = '$owner_b_user_id'::uuid),
      'binding_set_version', (SELECT set_version FROM business.project_skill_binding_set WHERE project_id = '$w1_public_market_project_id'::uuid),
      'binding_count', (SELECT COUNT(*) FROM business.project_skill_binding WHERE project_id = '$w1_public_market_project_id'::uuid AND skill_id = '$w1_skill_id'::uuid AND status = 'enabled'),
      'binding_id', (SELECT id FROM business.project_skill_binding WHERE project_id = '$w1_public_market_project_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'binding_version', (SELECT version FROM business.project_skill_binding WHERE project_id = '$w1_public_market_project_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'resolution_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution WHERE project_id = '$w1_public_market_project_id'::uuid AND owner_user_id = '$owner_b_user_id'::uuid),
      'resolution_item_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution_item WHERE project_id = '$w1_public_market_project_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'publisher_user_id', (SELECT publisher_user_id FROM business.project_session_skill_resolution_item WHERE project_id = '$w1_public_market_project_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'published_snapshot_id', (SELECT published_snapshot_id FROM business.project_session_skill_resolution_item WHERE project_id = '$w1_public_market_project_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'permission_snapshot_digest', (SELECT encode(permission_snapshot_digest, 'hex') FROM business.project_session_skill_resolution_item WHERE project_id = '$w1_public_market_project_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'governance_epoch', (SELECT governance_epoch FROM business.project_session_skill_resolution_item WHERE project_id = '$w1_public_market_project_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'snapshot_digest', (SELECT encode(snapshot_set_digest, 'hex') FROM business.project_session_skill_resolution WHERE project_id = '$w1_public_market_project_id'::uuid),
      'content_digest', (SELECT encode(content_digest, 'hex') FROM business.project_session_skill_resolution_item WHERE project_id = '$w1_public_market_project_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'runtime_content_digest', (SELECT encode(runtime_content_digest, 'hex') FROM business.project_session_skill_resolution_item WHERE project_id = '$w1_public_market_project_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'session_binding_ready', EXISTS (SELECT 1 FROM business.project_session_binding WHERE project_id = '$w1_public_market_project_id'::uuid AND agent_session_id = '$w1_public_market_session_id'::uuid AND provisioning_status = 'ready'),
      'outbox_delivered', EXISTS (SELECT 1 FROM business.project_session_binding AS binding JOIN business.project_session_outbox AS outbox ON outbox.id = binding.command_id WHERE binding.project_id = '$w1_public_market_project_id'::uuid AND outbox.status = 'delivered')
    );")" || return 1
  agent_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent -Atc "
    SELECT json_build_object(
      'session_count', (SELECT COUNT(*) FROM agent.session WHERE id = '$w1_public_market_session_id'::uuid AND project_id = '$w1_public_market_project_id'::uuid AND user_id = '$owner_b_user_id'::uuid),
      'snapshot_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot WHERE session_id = '$w1_public_market_session_id'::uuid),
      'snapshot_kind', (SELECT snapshot_kind FROM agent.session_skill_snapshot WHERE session_id = '$w1_public_market_session_id'::uuid),
      'skill_count', (SELECT skill_count FROM agent.session_skill_snapshot WHERE session_id = '$w1_public_market_session_id'::uuid),
      'snapshot_digest', (SELECT snapshot_digest FROM agent.session_skill_snapshot WHERE session_id = '$w1_public_market_session_id'::uuid),
      'item_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_public_market_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'publisher_user_id', (SELECT publisher_user_id FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_public_market_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'published_snapshot_id', (SELECT published_snapshot_id FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_public_market_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'permission_snapshot_digest', (SELECT permission_snapshot_digest FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_public_market_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'governance_epoch', (SELECT governance_epoch FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_public_market_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'content_digest', (SELECT content_digest FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_public_market_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid),
      'runtime_content_digest', (SELECT runtime_content_digest FROM agent.session_skill_snapshot_item WHERE session_id = '$w1_public_market_session_id'::uuid AND skill_id = '$w1_skill_id'::uuid)
    );")" || return 1
  jq -cn --argjson business "$business_fact" --argjson agent "$agent_fact" '{business:$business,agent:$agent}'
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

# create_w1_owner_private_published_skill 通过真实 Owner/Reviewer HTTP 链创建 Consumer 自有 Published+Active Skill。
create_w1_owner_private_published_skill() {
  local postgres_container="$1"
  local create_response="$w1_temp_dir/mixed-owner-private-create.json"
  local create_headers="$w1_temp_dir/mixed-owner-private-create.headers"
  local submit_response="$w1_temp_dir/mixed-owner-private-submit.json"
  local review_detail="$w1_temp_dir/mixed-owner-private-review-detail.json"
  local review_headers="$w1_temp_dir/mixed-owner-private-review-detail.headers"
  local approve_response="$w1_temp_dir/mixed-owner-private-approve.json"
  local payload=""
  local draft_etag=""
  local review_id=""
  local review_etag=""
  local status=""
  local database_fact=""

  w1_owner_private_skill_name="W1 Owner Private mixed ${run_id}"
  payload="$(build_w1_skill_payload "$w1_owner_private_skill_name" "owner-private-mixed")"
  : >"$create_headers"
  status="$(curl_with_body_stdin "$payload" --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    --config "$owner_b_curl_config" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: mixed-owner-private-create-${run_id}" -D "$create_headers" \
    -o "$create_response" -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/skills')"
  [[ "$status" == "201" ]] || fail "Mixed owner-private Skill 创建状态为 $status"
  w1_owner_private_skill_id="$(jq -er '.skill.skill_id | strings | select(test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))' "$create_response")"
  draft_etag="$(jq -er '.skill.draft_etag | strings | select(test("^\\\"[^\\\"]+\\\"$"))' "$create_response")"
  [[ "$(response_header_value "$create_headers" 'ETag')" == "$draft_etag" ]] || \
    fail "Mixed owner-private Skill 创建 ETag 漂移"

  status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" -X POST \
    --config "$owner_b_curl_config" -H "Idempotency-Key: mixed-owner-private-review-${run_id}" \
    -H "If-Match: $draft_etag" -o "$submit_response" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/skills/${w1_owner_private_skill_id}/reviews")"
  [[ "$status" == "201" ]] || fail "Mixed owner-private Skill 提交审核状态为 $status"
  review_id="$(jq -er '.review_id | strings | select(test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))' "$submit_response")"

  : >"$review_headers"
  status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" -D "$review_headers" \
    -o "$review_detail" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/admin/skill-reviews/${review_id}")"
  [[ "$status" == "200" ]] || fail "Mixed owner-private Skill Reviewer 详情状态为 $status"
  review_etag="$(jq -er '.review.review_etag | strings | select(test("^\\\"[^\\\"]+\\\"$"))' "$review_detail")"
  [[ "$(response_header_value "$review_headers" 'ETag')" == "$review_etag" ]] || \
    fail "Mixed owner-private Skill Review ETag 漂移"
  jq -e --arg review "$review_id" --arg skill "$w1_owner_private_skill_id" --arg owner "$owner_b_user_id" '
    .review.review_id == $review and .review.skill_id == $skill and .review.owner_user_id == $owner
    and .review.status == "reviewing" and .review.allowed_actions == ["approve_and_publish"]' \
    "$review_detail" >/dev/null || fail "Mixed owner-private Skill Reviewer 详情事实漂移"

  status="$(curl_with_body_stdin '{"decision":"approved"}' --silent --show-error --max-time 10 \
    -b "$owner_b_cookie_jar" -X POST --config "$owner_b_curl_config" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: mixed-owner-private-approve-${run_id}" -H "If-Match: $review_etag" \
    -o "$approve_response" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/admin/skill-reviews/${review_id}/decisions")"
  [[ "$status" == "200" ]] || fail "Mixed owner-private Skill 批准状态为 $status"
  jq -e --arg review "$review_id" --arg skill "$w1_owner_private_skill_id" '
    .review.review_id == $review and .review.skill_id == $skill
    and .review.status == "approved" and .review.allowed_actions == []' \
    "$approve_response" >/dev/null || fail "Mixed owner-private Skill 批准结果漂移"

  database_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'skill_count', COUNT(*),
      'owner_matches', bool_and(owner_user_id = '$owner_b_user_id'::uuid),
      'published_active', bool_and(current_published_snapshot_id IS NOT NULL AND governance_status = 'active' AND governance_epoch = 1),
      'published_count', (SELECT COUNT(*) FROM business.skill_published_snapshot WHERE skill_id = '$w1_owner_private_skill_id'::uuid),
      'approved_review_count', (SELECT COUNT(*) FROM business.skill_review_submission WHERE skill_id = '$w1_owner_private_skill_id'::uuid AND status = 'approved')
    ) FROM business.skill WHERE id = '$w1_owner_private_skill_id'::uuid;")" || \
    fail "Mixed owner-private Skill 数据库事实读取失败"
  jq -e '
    keys == ["approved_review_count","owner_matches","published_active","published_count","skill_count"]
    and .skill_count == 1 and .owner_matches and .published_active
    and .published_count == 1 and .approved_review_count == 1' <<<"$database_fact" >/dev/null || \
    fail "Mixed owner-private Skill 未形成真实 Published+Active 权威事实"
}

# run_w1_public_market_mixed_success_smoke 证明同一有效集合同时冻结 owner_private v1 与 public_market v2。
run_w1_public_market_mixed_success_smoke() {
  local postgres_container="$1"
  local response_file="$w1_temp_dir/public-market-mixed-success.json"
  local bootstrap_file="$w1_temp_dir/public-market-mixed-success-bootstrap.json"
  local payload=""
  local status=""
  local project_id=""
  local session_id=""
  local business_fact=""
  local agent_fact=""
  local mixed_fact=""
  local owner_item=""
  local public_item=""
  local owner_permission=""
  local public_permission=""
  local owner_permission_digest=""
  local public_permission_digest=""

  create_w1_owner_private_published_skill "$postgres_container"
  payload="$(jq -cn --arg owner_skill "$w1_owner_private_skill_id" --arg public_skill "$w1_skill_id" '
    {schema_version:"project_quick_create.v2",initial_prompt:"",enabled_skill_ids:[$owner_skill,$public_skill]}')"
  status="$(curl_with_body_stdin "$payload" --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    --config "$owner_b_curl_config" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: public-market-mixed-success-${run_id}" -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/projects:quick-create')"
  [[ "$status" == "201" ]] || fail "Public Market 有效 mixed QuickCreate 状态为 $status"
  project_id="$(jq -er '.project_id | strings | select(test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))' "$response_file")"
  poll_bootstrap_ready "$project_id" "$bootstrap_file" "$owner_b_cookie_jar" || \
    fail "Public Market 有效 mixed Project 未进入 ready"
  session_id="$(jq -er '.session_id | strings | select(test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))' "$bootstrap_file")"

  business_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'project_count', (SELECT COUNT(*) FROM business.project WHERE id = '$project_id'::uuid AND owner_user_id = '$owner_b_user_id'::uuid),
      'receipt_count', (SELECT COUNT(*) FROM business.project_creation_receipt WHERE project_id = '$project_id'::uuid),
      'binding_set_count', (SELECT COUNT(*) FROM business.project_skill_binding_set WHERE project_id = '$project_id'::uuid AND owner_user_id = '$owner_b_user_id'::uuid),
      'binding_count', (SELECT COUNT(*) FROM business.project_skill_binding WHERE project_id = '$project_id'::uuid AND status = 'enabled'),
      'binding_audit_count', (SELECT COUNT(*) FROM business.project_skill_binding_audit WHERE project_id = '$project_id'::uuid),
      'resolution_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution WHERE project_id = '$project_id'::uuid AND owner_user_id = '$owner_b_user_id'::uuid AND skill_count = 2),
      'binding_set_version', (SELECT binding_set_version FROM business.project_session_skill_resolution WHERE project_id = '$project_id'::uuid),
      'snapshot_digest', (SELECT encode(snapshot_set_digest, 'hex') FROM business.project_session_skill_resolution WHERE project_id = '$project_id'::uuid),
      'session_binding_ready', EXISTS (SELECT 1 FROM business.project_session_binding WHERE project_id = '$project_id'::uuid AND agent_session_id = '$session_id'::uuid AND provisioning_status = 'ready'),
      'outbox_delivered', EXISTS (SELECT 1 FROM business.project_session_binding AS session_binding JOIN business.project_session_outbox AS outbox ON outbox.id = session_binding.command_id WHERE session_binding.project_id = '$project_id'::uuid AND outbox.status = 'delivered'),
      'items', COALESCE((SELECT json_agg(json_build_object(
        'skill_id', item.skill_id,
        'publisher_user_id', item.publisher_user_id,
        'binding_id', item.binding_id,
        'binding_version', item.binding_version,
        'published_snapshot_id', item.published_snapshot_id,
        'permission_snapshot_digest', encode(item.permission_snapshot_digest, 'hex'),
        'governance_epoch', item.governance_epoch
      ) ORDER BY item.skill_id)
        FROM business.project_session_skill_resolution_item AS item
        WHERE item.project_id = '$project_id'::uuid), '[]'::json)
    );")" || fail "Public Market 有效 mixed Business 事实读取失败"
  agent_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent -Atc "
    SELECT json_build_object(
      'session_count', (SELECT COUNT(*) FROM agent.session WHERE id = '$session_id'::uuid AND project_id = '$project_id'::uuid AND user_id = '$owner_b_user_id'::uuid),
      'snapshot_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot WHERE session_id = '$session_id'::uuid AND skill_count = 2),
      'snapshot_digest', (SELECT snapshot_digest FROM agent.session_skill_snapshot WHERE session_id = '$session_id'::uuid),
      'items', COALESCE((SELECT json_agg(json_build_object(
        'skill_id', item.skill_id,
        'publisher_user_id', item.publisher_user_id,
        'published_snapshot_id', item.published_snapshot_id,
        'permission_snapshot_digest', item.permission_snapshot_digest,
        'governance_epoch', item.governance_epoch
      ) ORDER BY item.skill_id)
        FROM agent.session_skill_snapshot_item AS item WHERE item.session_id = '$session_id'::uuid), '[]'::json)
    );")" || fail "Public Market 有效 mixed Agent 事实读取失败"
  mixed_fact="$(jq -cn --argjson business "$business_fact" --argjson agent "$agent_fact" '{business:$business,agent:$agent}')"
  owner_item="$(jq -cer --arg skill "$w1_owner_private_skill_id" '.business.items[] | select(.skill_id == $skill)' <<<"$mixed_fact")"
  public_item="$(jq -cer --arg skill "$w1_skill_id" '.business.items[] | select(.skill_id == $skill)' <<<"$mixed_fact")"
  owner_permission="$(jq -cn --arg subject "$owner_b_user_id" --arg project "$project_id" \
    --argjson item "$owner_item" --argjson set_version "$(jq -er '.business.binding_set_version' <<<"$mixed_fact")" '
    {schema_version:"project_skill_permission_snapshot.v1",decision:"allow",basis:"owner_private",
      subject_user_id:$subject,project_id:$project,project_owner_user_id:$subject,
      binding_id:$item.binding_id,binding_version:$item.binding_version,binding_set_version:$set_version,
      namespace:"user",skill_id:$item.skill_id,skill_owner_user_id:$subject,
      published_snapshot_id:$item.published_snapshot_id,allowed_actions:["session_snapshot"],
      policy_ref:"project-skill-permission:owner-private:v1"}')"
  public_permission="$(jq -cn --arg subject "$owner_b_user_id" --arg project "$project_id" \
    --arg publisher "$user_id" --argjson item "$public_item" \
    --argjson set_version "$(jq -er '.business.binding_set_version' <<<"$mixed_fact")" '
    {schema_version:"project_skill_permission_snapshot.v2",decision:"allow",basis:"public_market",
      subject_user_id:$subject,project_id:$project,project_owner_user_id:$subject,
      binding_id:$item.binding_id,binding_version:$item.binding_version,binding_set_version:$set_version,
      namespace:"user",skill_id:$item.skill_id,skill_owner_user_id:$publisher,
      published_snapshot_id:$item.published_snapshot_id,allowed_actions:["session_snapshot"],
      policy_ref:"project-skill-permission:public-market:v1"}')"
  owner_permission_digest="$(sha256_text "$owner_permission")" || fail "Mixed owner_private Permission digest 计算失败"
  public_permission_digest="$(sha256_text "$public_permission")" || fail "Mixed public_market Permission digest 计算失败"
  unset owner_permission public_permission

  jq -e --arg owner_skill "$w1_owner_private_skill_id" --arg public_skill "$w1_skill_id" \
    --arg consumer "$owner_b_user_id" --arg publisher "$user_id" \
    --arg owner_digest "$owner_permission_digest" --arg public_digest "$public_permission_digest" '
    .business.project_count == 1 and .business.receipt_count == 1
    and .business.binding_set_count == 1 and .business.binding_count == 2 and .business.binding_audit_count == 2
    and .business.resolution_count == 1 and .business.binding_set_version == 1
    and .business.session_binding_ready and .business.outbox_delivered
    and (.business.snapshot_digest | test("^[0-9a-f]{64}$"))
    and (.business.items | length) == 2
    and ([.business.items[].skill_id] | sort) == ([$owner_skill,$public_skill] | sort)
    and any(.business.items[]; .skill_id == $owner_skill and .publisher_user_id == $consumer
      and .permission_snapshot_digest == $owner_digest and .governance_epoch == 1)
    and any(.business.items[]; .skill_id == $public_skill and .publisher_user_id == $publisher
      and .publisher_user_id != $consumer and .permission_snapshot_digest == $public_digest and .governance_epoch == 1)
    and .agent.session_count == 1 and .agent.snapshot_count == 1 and (.agent.items | length) == 2
    and .agent.snapshot_digest == .business.snapshot_digest
    and (. as $fact | all($fact.business.items[]; . as $business_item
      | any($fact.agent.items[]; .skill_id == $business_item.skill_id
          and .publisher_user_id == $business_item.publisher_user_id
          and .published_snapshot_id == $business_item.published_snapshot_id
          and .permission_snapshot_digest == $business_item.permission_snapshot_digest
          and .governance_epoch == $business_item.governance_epoch)))' <<<"$mixed_fact" >/dev/null || \
    fail "Public Market 有效 mixed 未同时形成 owner_private v1 与 public_market v2"
  jq -n --arg project_id "$project_id" --arg session_id "$session_id" \
    '{schema_version:"w1.public-market-mixed-binding-success.database-fact.v1",
      project_id:$project_id,session_id:$session_id,skill_count:2,
      owner_private_v1:true,public_market_v2:true,business_agent_consistent:true}' \
    >"$evidence_dir/responses/w1-public-market-mixed-binding-success.json"
  w1_public_market_mixed_success=true
}

# run_w1_public_market_mixed_failure_smoke 使用同两个真实 Skill 证明任一不可用时 mixed 集合全量回滚。
run_w1_public_market_mixed_failure_smoke() {
  local postgres_container="$1"
  local response_file="$w1_temp_dir/public-market-mixed-suspended-failure.json"
  local payload=""
  local status=""
  local before_counts=""
  local after_counts=""
  local skill_state_fact=""

  [[ "$w1_public_market_mixed_success" == "true" && -n "$w1_owner_private_skill_id" ]] || \
    fail "Public Market mixed 失败门禁缺少同批有效 mixed 成功事实"
  skill_state_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'owner_private_exists', EXISTS (SELECT 1 FROM business.skill WHERE id = '$w1_owner_private_skill_id'::uuid AND owner_user_id = '$owner_b_user_id'::uuid AND current_published_snapshot_id IS NOT NULL AND governance_status = 'active'),
      'public_market_exists', EXISTS (SELECT 1 FROM business.skill WHERE id = '$w1_skill_id'::uuid AND owner_user_id = '$user_id'::uuid AND current_published_snapshot_id IS NOT NULL AND governance_status = 'suspended')
    );")" || fail "Public Market mixed 失败前 Skill 权威状态读取失败"
  jq -e 'keys == ["owner_private_exists","public_market_exists"]
    and .owner_private_exists and .public_market_exists' <<<"$skill_state_fact" >/dev/null || \
    fail "Public Market mixed 失败门禁没有使用真实 owner-private active + public-market suspended Skill"

  payload="$(jq -cn --arg owner_skill "$w1_owner_private_skill_id" --arg public_skill "$w1_skill_id" '
    {schema_version:"project_quick_create.v2",initial_prompt:"",enabled_skill_ids:[$owner_skill,$public_skill]}')"
  before_counts="$(read_governance_quickcreate_counts "$postgres_container")" || \
    fail "Public Market mixed 失败前九类数据库计数读取失败"
  status="$(curl_with_body_stdin "$payload" --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    --config "$owner_b_curl_config" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: public-market-mixed-suspended-${run_id}" -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/projects:quick-create')"
  [[ "$status" == "409" ]] || fail "Public Market suspended mixed QuickCreate 状态为 $status"
  jq -e '.error.code == "PROJECT_SKILL_UNAVAILABLE" and .error.retryable == false' "$response_file" >/dev/null || \
    fail "Public Market suspended mixed QuickCreate 错误契约漂移"
  after_counts="$(read_governance_quickcreate_counts "$postgres_container")" || \
    fail "Public Market mixed 失败后九类数据库计数读取失败"
  jq -ne --argjson before "$before_counts" --argjson after "$after_counts" '$before == $after' >/dev/null || \
    fail "Public Market suspended mixed 失败留下九类部分事实"
  jq -n --arg owner_skill_id "$w1_owner_private_skill_id" --arg public_skill_id "$w1_skill_id" \
    '{schema_version:"w1.public-market-mixed-binding-failure.database-fact.v1",
      owner_skill_id:$owner_skill_id,public_skill_id:$public_skill_id,http_status:409,
      owner_private_active:true,public_market_suspended:true,database_counts_unchanged:true}' \
    >"$evidence_dir/responses/w1-public-market-mixed-binding-failure.json"
  w1_public_market_mixed_binding_atomicity=true
}

# run_w1_public_market_binding_active_smoke 在 active current Published 状态验证真实跨发布者创建、权限身份与幂等。
run_w1_public_market_binding_active_smoke() {
  local postgres_container="$1"
  local intent_key="public-market-binding-${run_id}"
  local batch_dir="$evidence_dir/responses/w1-public-market-binding-batch"
  local bootstrap_file="$w1_temp_dir/public-market-binding-bootstrap.json"
  local replay_file="$w1_temp_dir/public-market-binding-replay.json"
  local conflict_file="$w1_temp_dir/public-market-binding-conflict.json"
  local payload=""
  local conflict_payload=""
  local replay_status=""
  local conflict_status=""
  local batch_request_count=""
  local batch_success_count=""
  local batch_created_count=""
  local snapshot_fact=""
  local permission_canonical=""
  local calculated_permission_digest=""

  payload="$(jq -cn --arg skill "$w1_skill_id" \
    '{schema_version:"project_quick_create.v2",initial_prompt:"",enabled_skill_ids:[$skill]}')"
  w1_public_market_project_id="$(run_concurrent_quick_create \
    "$intent_key" "$payload" "$batch_dir" "$owner_b_curl_config" "$owner_b_cookie_jar")"
  [[ "$w1_public_market_project_id" =~ ^[0-9a-f-]{36}$ ]] || fail "Public Market Binding Project ID 格式无效"
  batch_request_count="$(find "$batch_dir" -type f -name '*.status' | wc -l | tr -d '[:space:]')"
  batch_success_count="$(awk '$1 == "200" || $1 == "201" { count++ } END { print count + 0 }' "$batch_dir"/*.status)"
  batch_created_count="$(awk '$1 == "201" { count++ } END { print count + 0 }' "$batch_dir"/*.status)"
  poll_bootstrap_ready "$w1_public_market_project_id" "$bootstrap_file" "$owner_b_cookie_jar" || \
    fail "Public Market Binding Project 未进入 ready"
  w1_public_market_session_id="$(jq -er '.session_id | strings | select(test("^[0-9a-f-]{36}$"))' "$bootstrap_file")"
  jq -e --arg project "$w1_public_market_project_id" --arg session "$w1_public_market_session_id" '
    .project_id == $project and .session_id == $session and .creation_status == "ready"
    and .input_id == null and .initial_prompt_status == "absent"' "$bootstrap_file" >/dev/null || \
    fail "Public Market Binding ready 响应漂移"

  replay_status="$(curl_with_body_stdin "$payload" --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    --config "$owner_b_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: $intent_key" \
    -o "$replay_file" -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/projects:quick-create')"
  [[ "$replay_status" == "200" ]] || fail "Public Market Binding 同义重放状态为 $replay_status"
  jq -e --arg project "$w1_public_market_project_id" --arg session "$w1_public_market_session_id" '
    .project_id == $project and .session_id == $session and .creation_status == "ready" and .input_id == null' \
    "$replay_file" >/dev/null || fail "Public Market Binding 重放未返回冻结 Project/Session"
  conflict_payload="$(jq -cn '{schema_version:"project_quick_create.v2",initial_prompt:"",enabled_skill_ids:[]}')"
  conflict_status="$(curl_with_body_stdin "$conflict_payload" --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    --config "$owner_b_curl_config" -H 'Content-Type: application/json' -H "Idempotency-Key: $intent_key" \
    -o "$conflict_file" -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/projects:quick-create')"
  [[ "$conflict_status" == "409" ]] || fail "Public Market Binding 同键异义状态为 $conflict_status"
  jq -e '.error.code == "IDEMPOTENCY_CONFLICT" and .error.retryable == false' "$conflict_file" >/dev/null || \
    fail "Public Market Binding 同键异义错误契约漂移"
  [[ "$batch_request_count" == "100" && "$batch_success_count" == "100" && "$batch_created_count" == "1" ]] || \
    fail "Public Market Binding 并发数量漂移"
  w1_public_market_idempotency_frozen_replay=true

  run_w1_public_market_mixed_success_smoke "$postgres_container"

  snapshot_fact="$(read_public_market_session_snapshot_fact "$postgres_container")" || \
    fail "Public Market Binding 消费者/Publisher Snapshot 事实读取失败"
  permission_canonical="$(jq -cn \
    --arg subject "$owner_b_user_id" --arg project "$w1_public_market_project_id" \
    --arg binding "$(jq -er '.business.binding_id' <<<"$snapshot_fact")" \
    --arg skill "$w1_skill_id" --arg publisher "$user_id" \
    --arg snapshot "$(jq -er '.business.published_snapshot_id' <<<"$snapshot_fact")" \
    --argjson binding_version "$(jq -er '.business.binding_version' <<<"$snapshot_fact")" \
    --argjson binding_set_version "$(jq -er '.business.binding_set_version' <<<"$snapshot_fact")" '
    {schema_version:"project_skill_permission_snapshot.v2",decision:"allow",basis:"public_market",
      subject_user_id:$subject,project_id:$project,project_owner_user_id:$subject,
      binding_id:$binding,binding_version:$binding_version,binding_set_version:$binding_set_version,
      namespace:"user",skill_id:$skill,skill_owner_user_id:$publisher,published_snapshot_id:$snapshot,
      allowed_actions:["session_snapshot"],policy_ref:"project-skill-permission:public-market:v1"}')"
  calculated_permission_digest="$(sha256_text "$permission_canonical")" || \
    fail "Public Market Permission Canonical digest 计算失败"
  unset permission_canonical
  jq -e --arg consumer "$owner_b_user_id" --arg publisher "$user_id" \
    --arg permission_digest "$calculated_permission_digest" '
    .business.project_count == 1 and .business.receipt_count == 1
    and .business.binding_set_count == 1 and .business.binding_set_version == 1
    and .business.binding_count == 1 and .business.binding_version == 1
    and .business.resolution_count == 1 and .business.resolution_item_count == 1
    and .business.publisher_user_id == $publisher and .business.publisher_user_id != $consumer
    and .business.permission_snapshot_digest == $permission_digest
    and .business.governance_epoch == 1 and .business.session_binding_ready and .business.outbox_delivered
    and (.business.snapshot_digest | test("^[0-9a-f]{64}$"))
    and (.business.content_digest | test("^[0-9a-f]{64}$"))
    and (.business.runtime_content_digest | test("^[0-9a-f]{64}$"))
    and .agent.session_count == 1 and .agent.snapshot_count == 1
    and .agent.snapshot_kind == "published_refs" and .agent.skill_count == 1 and .agent.item_count == 1
    and .agent.publisher_user_id == $publisher and .agent.publisher_user_id != $consumer
    and .agent.permission_snapshot_digest == $permission_digest and .agent.governance_epoch == 1
    and .business.published_snapshot_id == .agent.published_snapshot_id
    and .business.permission_snapshot_digest == .agent.permission_snapshot_digest
    and .business.snapshot_digest == .agent.snapshot_digest
    and .business.content_digest == .agent.content_digest
    and .business.runtime_content_digest == .agent.runtime_content_digest' <<<"$snapshot_fact" >/dev/null || \
    fail "Public Market Binding 消费者/Publisher/Permission/Agent Snapshot 事实不一致"
  printf '%s\n' "$snapshot_fact" >"$evidence_dir/responses/w1-public-market-binding-snapshot.json"
  w1_public_market_snapshot_before="$snapshot_fact"
  w1_public_market_permission_identity_separation=true
  w1_public_market_quickcreate=true
}

# run_w1_skill_market_active_smoke 验证匿名/带 Cookie 同投影、发布快照隔离、坏游标与 Public Market Binding 入口。
run_w1_skill_market_active_smoke() {
  local postgres_container="$1"
  local owner_response="$w1_temp_dir/market-owner-detail.json"
  local owner_headers="$w1_temp_dir/market-owner-detail.headers"
  local draft_response="$w1_temp_dir/market-draft-update.json"
  local draft_headers="$w1_temp_dir/market-draft-update.headers"
  local anon_list="$w1_temp_dir/market-active-anonymous-list.json"
  local anon_list_headers="$w1_temp_dir/market-active-anonymous-list.headers"
  local cookie_list="$w1_temp_dir/market-active-cookie-list.json"
  local cookie_list_headers="$w1_temp_dir/market-active-cookie-list.headers"
  local anon_detail="$w1_temp_dir/market-active-anonymous-detail.json"
  local anon_detail_headers="$w1_temp_dir/market-active-anonymous-detail.headers"
  local cookie_detail="$w1_temp_dir/market-active-cookie-detail.json"
  local cookie_detail_headers="$w1_temp_dir/market-active-cookie-detail.headers"
  local invalid_response="$w1_temp_dir/market-invalid-cursor.json"
  local invalid_headers="$w1_temp_dir/market-invalid-cursor.headers"
  local draft_config="$w1_temp_dir/market-draft-update.curl"
  local status=""
  local draft_etag=""
  local draft_payload=""

  : >"$owner_headers"
  status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" -D "$owner_headers" \
    -o "$owner_response" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}")"
  [[ "$status" == "200" ]] || fail "Market 隔离草稿前 Owner 详情状态为 $status"
  [[ "$(response_header_value "$owner_headers" 'Cache-Control')" == "no-store" ]] || fail "Market 隔离草稿前 Owner 详情缺少 no-store"
  draft_etag="$(jq -er '.skill.draft_etag | strings | select(test("^\\\"[^\\\"]+\\\"$"))' "$owner_response")"
  w1_market_draft_name="W1 Skill market draft ${run_id}"
  draft_payload="$(build_w1_skill_payload "$w1_market_draft_name" "market-draft")"
  write_conditional_curl_config "$draft_config" "$user_curl_config" "$draft_etag" "" || fail "Market 隔离草稿 curl 配置写入失败"
  : >"$draft_headers"
  status="$(curl_with_body_stdin "$draft_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$draft_config" -X PUT -H 'Content-Type: application/json' -D "$draft_headers" \
    -o "$draft_response" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}/draft")"
  [[ "$status" == "200" ]] || fail "Market 隔离草稿更新状态为 $status"
  [[ "$(response_header_value "$draft_headers" 'Cache-Control')" == "no-store" ]] || fail "Market 隔离草稿更新缺少 no-store"
  jq -e --arg name "$w1_market_draft_name" '.skill.definition.name == $name' "$draft_response" >/dev/null || \
    fail "Market 隔离草稿未成为 current draft"

  : >"$anon_list_headers"
  status="$(curl --silent --show-error --max-time 10 -D "$anon_list_headers" -o "$anon_list" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/skill-market')"
  [[ "$status" == "200" ]] || fail "匿名 Market active 列表状态为 $status"
  assert_w1_skill_market_list_contract "$anon_list" "$anon_list_headers" true || fail "匿名 Market active 列表契约漂移"
  : >"$anon_detail_headers"
  status="$(curl --silent --show-error --max-time 10 -D "$anon_detail_headers" -o "$anon_detail" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/skill-market/${w1_skill_id}")"
  [[ "$status" == "200" ]] || fail "匿名 Market active 详情状态为 $status"
  assert_w1_skill_market_detail_contract "$anon_detail" "$anon_detail_headers" || fail "匿名 Market active 详情契约漂移"

  : >"$cookie_list_headers"
  status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" -D "$cookie_list_headers" \
    -o "$cookie_list" -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/skill-market')"
  [[ "$status" == "200" ]] || fail "带 Cookie Market active 列表状态为 $status"
  assert_w1_skill_market_list_contract "$cookie_list" "$cookie_list_headers" true || fail "带 Cookie Market active 列表契约漂移"
  : >"$cookie_detail_headers"
  status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" -D "$cookie_detail_headers" \
    -o "$cookie_detail" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/skill-market/${w1_skill_id}")"
  [[ "$status" == "200" ]] || fail "带 Cookie Market active 详情状态为 $status"
  assert_w1_skill_market_detail_contract "$cookie_detail" "$cookie_detail_headers" || fail "带 Cookie Market active 详情契约漂移"
  jq -ne --slurpfile anonymous "$anon_list" --slurpfile cookie "$cookie_list" \
    '($anonymous[0] | del(.request_id)) == ($cookie[0] | del(.request_id))' >/dev/null || \
    fail "Market 列表发生 Session 个性化"
  jq -ne --slurpfile anonymous "$anon_detail" --slurpfile cookie "$cookie_detail" \
    '($anonymous[0] | del(.request_id)) == ($cookie[0] | del(.request_id))' >/dev/null || \
    fail "Market 详情发生 Session 个性化"
  w1_skill_market_public_read=true
  w1_skill_market_safe_projection=true

  run_w1_skill_market_keyset_smoke "$postgres_container"

  : >"$invalid_headers"
  status="$(curl --silent --show-error --max-time 10 -D "$invalid_headers" -o "$invalid_response" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/skill-market?cursor=abc%3D')"
  [[ "$status" == "400" ]] || fail "Market 非法 cursor 状态为 $status"
  assert_w1_skill_market_error_response "$invalid_response" "$invalid_headers" "INVALID_REQUEST" || \
    fail "Market 非法 cursor 未 fail-closed"
  w1_skill_market_cursor_fail_closed=true

  run_w1_public_market_binding_active_smoke "$postgres_container"
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
  local review_queue_page="$w1_temp_dir/review-queue-page.json"
  local review_queue_seen="$w1_temp_dir/review-queue-seen-cursors.txt"
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
  local review_queue_cursor=""
  local review_queue_next_cursor=""
  local review_queue_found="false"
  local review_queue_url=""
  local review_queue_pages=0

  : >"$review_queue_seen"
  while (( review_queue_pages < 100 )); do
    review_queue_url='http://127.0.0.1:18081/api/v1/admin/skill-reviews?status=reviewing'
    if [[ -n "$review_queue_cursor" ]]; then
      review_queue_url="${review_queue_url}&cursor=${review_queue_cursor}"
    fi
    status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
      -o "$review_queue_page" -w '%{http_code}' "$review_queue_url")"
    queue_status="$status"
    [[ "$status" == "200" ]] || fail "W1 Reviewer 待审队列第 $((review_queue_pages + 1)) 页状态为 $status"
    jq -e '
      keys == ["items","next_cursor","request_id"] and (.items | type) == "array"
      and (.next_cursor == null or ((.next_cursor | type) == "string" and (.next_cursor | length) > 0))' \
      "$review_queue_page" >/dev/null || fail "W1 Reviewer 待审队列分页契约漂移"
    if jq -e --arg review "$w1_review_id" --arg skill "$w1_skill_id" '
      any(.items[];
        .review_id == $review and .skill_id == $skill and .status == "reviewing"
        and .allowed_actions == ["approve_and_publish"])' "$review_queue_page" >/dev/null; then
      mv "$review_queue_page" "$review_queue"
      review_queue_found="true"
      break
    fi
    review_queue_next_cursor="$(jq -r '.next_cursor // empty' "$review_queue_page")"
    [[ -n "$review_queue_next_cursor" ]] || break
    if grep -Fqx -- "$review_queue_next_cursor" "$review_queue_seen"; then
      fail "W1 Reviewer 待审队列返回重复 cursor"
    fi
    printf '%s\n' "$review_queue_next_cursor" >>"$review_queue_seen"
    review_queue_cursor="$review_queue_next_cursor"
    review_queue_pages=$((review_queue_pages + 1))
  done
  [[ "$review_queue_found" == "true" ]] || fail "W1 Reviewer 待审队列 100 页内未返回冻结审核项"

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

# run_w1_skill_governance_smoke 使用四个真实账号验证治理状态机、项目门禁、终态、撤权和数据库原子事实。
run_w1_skill_governance_smoke() {
  local postgres_container="$1"
  local response_file="$w1_temp_dir/governance-response.json"
  local headers_file="$w1_temp_dir/governance-headers.txt"
  local conditional_config="$w1_temp_dir/governance-command.curl"
  local quick_config="$w1_temp_dir/governance-quick-create.curl"
  local stale_quick_config="$w1_temp_dir/public-market-stale-selection.curl"
  local stale_quick_response="$w1_temp_dir/public-market-stale-selection.json"
  local toctou_suspend_response="$w1_temp_dir/public-market-toctou-suspend.json"
  local toctou_suspend_headers="$w1_temp_dir/public-market-toctou-suspend.headers"
  local toctou_suspend_status_file="$w1_temp_dir/public-market-toctou-suspend.status"
  local toctou_quick_status_file="$w1_temp_dir/public-market-toctou-quick.status"
  local toctou_lock_log="$w1_temp_dir/public-market-toctou-lock.log"
  local toctou_lock_application="w1-toctou-audit-${run_id}"
  local baseline_fact=""
  local final_fact=""
  local existing_session_snapshot_before=""
  local existing_session_snapshot_after=""
  local offline_resume_before_fact=""
  local offline_resume_after_fact=""
  local before_counts=""
  local during_counts=""
  local after_counts=""
  local status=""
  local active_etag=""
  local suspended_etag=""
  local resumed_etag=""
  local offline_etag=""
  local suspend_time=""
  local suspend_request_id=""
  local resumed_project_id=""
  local resumed_bootstrap="$w1_temp_dir/governance-resumed-bootstrap.json"
  local offline_review_id=""
  local offline_review_etag=""
  local offline_draft_etag=""
  local offline_updated_etag=""
  local creator_forbidden_status=""
  local reviewer_forbidden_status=""
  local governor_review_forbidden_status=""
  local queue_status=""
  local detail_status=""
  local suspend_status=""
  local suspend_replay_status=""
  local existing_session_status=""
  local offline_existing_session_status=""
  local suspended_quick_status=""
  local stale_quick_status=""
  local toctou_lock_shell_pid=""
  local toctou_lock_database_pid=""
  local toctou_governance_shell_pid=""
  local toctou_governance_database_pid=""
  local toctou_quick_shell_pid=""
  local toctou_quick_database_pid=""
  local quick_payload=""
  local resume_status=""
  local resumed_quick_status=""
  local offline_status=""
  local offline_quick_status=""
  local offline_resume_status=""
  local offline_update_status=""
  local offline_submit_status=""
  local offline_review_detail_status=""
  local offline_approve_status=""
  local governor_session_status=""
  local governor_denied_status=""
  local governance_produced_at=""
  local final_ready_status=""
  local governor_role=""
  local governor_capability=""
  local creator_forbidden_code=""
  local reviewer_forbidden_code=""
  local governor_review_forbidden_code=""
  local governor_denied_code=""
  local revoked_role_count=""
  local revoked_capability_count=""
  local suspend_replay_matches="false"
  local offline_resume_state_unchanged="false"
  local existing_session_snapshot_unchanged="false"
  local public_market_snapshot_after=""

  local governor_login_payload=""
  governor_login_payload="$(build_login_json "$governor_email" "$governor_password")" || fail "Governor 登录请求构造失败"
  status="$(curl_with_body_stdin "$governor_login_payload" --silent --show-error --max-time 10 \
    -c "$governor_cookie_jar" -H 'Content-Type: application/json' -o "$governor_login_response_temp" \
    -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/auth/session')"
  unset governor_login_payload
  [[ "$status" == "200" ]] || fail "Governor 登录状态为 $status"
  governor_csrf_token="$(jq -er '.csrf_token | strings | select(length > 0)' "$governor_login_response_temp")"
  write_curl_header_config "$governor_curl_config" 'X-CSRF-Token' "$governor_csrf_token" || fail "Governor curl 安全配置写入失败"
  governor_cookie_token="$(awk 'NF >= 7 {value=$7} END {print value}' "$governor_cookie_jar")"
  [[ -n "$governor_cookie_token" ]] || fail "Governor Cookie 会话未建立"
  governor_role="$(jq -er '.principal.roles | if length == 1 then .[0] else error("role count") end' "$governor_login_response_temp")"
  governor_capability="$(jq -er '.principal.capabilities | if length == 1 then .[0] else error("capability count") end' "$governor_login_response_temp")"
  jq -e --arg governor "$governor_user_id" --arg creator "$user_id" --arg reviewer "$owner_b_seed_user_id" --arg provisioner "$provisioner_user_id" '
    .principal.id == $governor
    and .principal.roles == ["skill_governor"]
    and .principal.capabilities == ["skill.govern"]
    and $governor != $creator and $governor != $reviewer and $governor != $provisioner' \
    "$governor_login_response_temp" >/dev/null || fail "Governor 登录未返回隔离的权威角色与 capability"
  rm -f "$governor_login_response_temp"
  governor_login_response_temp=""

  # Market 隔离草稿会推进 Skill 聚合版本，必须先于治理 baseline 与 Strong ETag 读取完成。
  run_w1_skill_market_active_smoke "$postgres_container"

  baseline_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'version', skill_record.version,
      'published_snapshot_id', skill_record.current_published_snapshot_id,
      'publication_revision', skill_record.publication_revision,
      'published_count', (SELECT COUNT(*) FROM business.skill_published_snapshot WHERE skill_id = skill_record.id),
      'review_count', (SELECT COUNT(*) FROM business.skill_review_submission WHERE skill_id = skill_record.id),
      'governance_receipt_count', (SELECT COUNT(*) FROM business.skill_command_receipt WHERE result_skill_id = skill_record.id AND command_type = 'governance_transition'),
      'governance_audit_count', (SELECT COUNT(*) FROM business.skill_governance_audit WHERE skill_id = skill_record.id AND action IN ('governance_suspended','governance_resumed','governance_offlined'))
    ) FROM business.skill AS skill_record WHERE skill_record.id = '$w1_skill_id'::uuid;")"
  jq -e '.version >= 1 and (.published_snapshot_id | type) == "string" and .publication_revision == 1
    and .published_count == 1 and .review_count == 1
    and .governance_receipt_count == 0 and .governance_audit_count == 0' <<<"$baseline_fact" >/dev/null || \
    fail "治理前 Skill 基线事实漂移"
  existing_session_snapshot_before="$(read_existing_w1_session_snapshot_fact "$postgres_container")" || \
    fail "治理前既有 Session Snapshot 事实读取失败"
  jq -e '
    .business.resolution_count == 1 and .business.item_count == 1 and .business.governance_epoch == 1
    and .agent.session_count == 1 and .agent.snapshot_count == 1 and .agent.snapshot_kind == "published_refs"
    and .agent.skill_count == 1 and .agent.item_count == 1 and .agent.governance_epoch == 1
    and .business.published_snapshot_id == .agent.published_snapshot_id
    and .business.snapshot_digest == .agent.snapshot_digest
    and .business.content_digest == .agent.content_digest
    and .business.runtime_content_digest == .agent.runtime_content_digest' \
    <<<"$existing_session_snapshot_before" >/dev/null || fail "治理前既有 Session Snapshot 基线漂移"

  creator_forbidden_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
    -o "$response_file" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/admin/skill-governance/${w1_skill_id}")"
  [[ "$creator_forbidden_status" == "403" ]] || fail "Creator 调用治理 API 状态为 $creator_forbidden_status"
  creator_forbidden_code="$(jq -er '.error.code' "$response_file")"
  [[ "$creator_forbidden_code" == "SKILL_GOVERNANCE_CAPABILITY_REQUIRED" ]] || fail "Creator 治理拒绝码漂移"

  reviewer_forbidden_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    -o "$response_file" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/admin/skill-governance/${w1_skill_id}")"
  [[ "$reviewer_forbidden_status" == "403" ]] || fail "Reviewer 调用治理 API 状态为 $reviewer_forbidden_status"
  reviewer_forbidden_code="$(jq -er '.error.code' "$response_file")"
  [[ "$reviewer_forbidden_code" == "SKILL_GOVERNANCE_CAPABILITY_REQUIRED" ]] || fail "Reviewer 治理拒绝码漂移"

  governor_review_forbidden_status="$(curl --silent --show-error --max-time 10 -b "$governor_cookie_jar" \
    -o "$response_file" -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/admin/skill-reviews?status=reviewing')"
  [[ "$governor_review_forbidden_status" == "403" ]] || fail "Governor 调用 Reviewer API 状态为 $governor_review_forbidden_status"
  governor_review_forbidden_code="$(jq -er '.error.code' "$response_file")"
  [[ "$governor_review_forbidden_code" == "SKILL_REVIEW_CAPABILITY_REQUIRED" ]] || fail "Governor Reviewer 拒绝码漂移"

  queue_status="$(curl --silent --show-error --max-time 10 -b "$governor_cookie_jar" \
    -o "$response_file" -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/admin/skill-governance?status=active')"
  [[ "$queue_status" == "200" ]] || fail "Governor active 队列状态为 $queue_status"
  jq -e --arg skill "$w1_skill_id" 'any(.items[]; .skill_id == $skill and .governance_status == "active"
    and .governance_epoch == 1 and .allowed_actions == ["suspend","offline"])' "$response_file" >/dev/null || \
    fail "Governor active 队列未返回目标 Skill"

  : >"$headers_file"
  detail_status="$(curl --silent --show-error --max-time 10 -b "$governor_cookie_jar" -D "$headers_file" \
    -o "$response_file" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/admin/skill-governance/${w1_skill_id}")"
  [[ "$detail_status" == "200" ]] || fail "Governor active 详情状态为 $detail_status"
  active_etag="$(jq -er '.skill.governance_etag | strings | select(test("^\\\"[^\\\"]+\\\"$"))' "$response_file")"
  [[ "$(response_header_value "$headers_file" 'ETag')" == "$active_etag" ]] || fail "治理 active Header/Body ETag 不一致"
  [[ "$(response_header_value "$headers_file" 'Cache-Control')" == "no-store" ]] || fail "治理 active 详情缺少 no-store"
  jq -e --arg skill "$w1_skill_id" '
    keys == ["request_id","skill"]
    and (.request_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
    and (.skill | keys) == ["allowed_actions","definition","governance_epoch","governance_etag","governance_status","published_at","skill_id"]
    and (.skill.definition | type) == "object" and (.skill.published_at | type) == "string"
    and .skill.skill_id == $skill and .skill.governance_status == "active"
    and .skill.governance_epoch == 1 and .skill.allowed_actions == ["suspend","offline"]' "$response_file" >/dev/null || \
    fail "治理 active 详情投影漂移"

  write_conditional_curl_config "$conditional_config" "$governor_curl_config" "$active_etag" "governance-suspend-${run_id}" || \
    fail "治理 suspend curl 配置写入失败"
  quick_payload="$(jq -cn --arg skill "$w1_skill_id" '{schema_version:"project_quick_create.v2",initial_prompt:"",enabled_skill_ids:[$skill]}')"
  write_conditional_curl_config "$stale_quick_config" "$owner_b_curl_config" "" "public-market-stale-${run_id}" || \
    fail "Public Market 陈旧选择 curl 配置写入失败"
  before_counts="$(read_governance_quickcreate_counts "$postgres_container")" || \
    fail "治理 TOCTOU 前九类数据库计数读取失败"

  # 先锁住治理事务最后写入的 append-only audit 表，使真实治理 HTTP 已更新 Skill 行但尚未提交。
  # 随后的真实 QuickCreate 必须在 FOR SHARE 上直接等待该治理事务；释放阻塞后治理先提交，QuickCreate 返回 409。
  docker exec -e PGAPPNAME="$toctou_lock_application" -e PGOPTIONS='-c statement_timeout=60000' \
    "$postgres_container" psql -U dora_admin -d dora_business -v ON_ERROR_STOP=1 -qAtc \
    'BEGIN; LOCK TABLE business.skill_governance_audit IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(45); ROLLBACK;' \
    >"$toctou_lock_log" 2>&1 &
  toctou_lock_shell_pid="$!"
  w1_toctou_lock_shell_pid="$toctou_lock_shell_pid"
  for _ in $(seq 1 200); do
    toctou_lock_database_pid="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc \
      "SELECT pid FROM pg_stat_activity WHERE application_name = '$toctou_lock_application' AND wait_event = 'PgSleep' LIMIT 1;")"
    w1_toctou_lock_database_pid="$toctou_lock_database_pid"
    [[ "$toctou_lock_database_pid" =~ ^[0-9]+$ ]] && break
    kill -0 "$toctou_lock_shell_pid" 2>/dev/null || fail "治理 TOCTOU audit 锁持有进程提前退出"
    sleep 0.05
  done
  [[ "$toctou_lock_database_pid" =~ ^[0-9]+$ ]] || fail "治理 TOCTOU audit 锁未在预算内建立"

  : >"$toctou_suspend_headers"
  (
    curl_with_body_stdin "{\"action\":\"suspend\",\"reason_code\":\"incident_containment\",\"approval_reference\":\"SMOKE-SUSPEND-${run_id}\"}" \
      --silent --show-error --max-time 30 -b "$governor_cookie_jar" --config "$conditional_config" -X POST \
      -H 'Content-Type: application/json' -D "$toctou_suspend_headers" -o "$toctou_suspend_response" -w '%{http_code}' \
      "http://127.0.0.1:18081/api/v1/admin/skill-governance/${w1_skill_id}/decisions" \
      >"$toctou_suspend_status_file"
  ) &
  toctou_governance_shell_pid="$!"
  w1_toctou_governance_shell_pid="$toctou_governance_shell_pid"
  for _ in $(seq 1 200); do
    toctou_governance_database_pid="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
      SELECT blocked.pid
      FROM pg_stat_activity AS blocked
      WHERE $toctou_lock_database_pid = ANY(pg_blocking_pids(blocked.pid))
        AND blocked.query ILIKE '%skill_governance_audit%'
      ORDER BY blocked.pid
      LIMIT 1;")"
    [[ "$toctou_governance_database_pid" =~ ^[0-9]+$ ]] && break
    kill -0 "$toctou_governance_shell_pid" 2>/dev/null || fail "真实治理请求在形成未提交行锁前退出"
    sleep 0.05
  done
  [[ "$toctou_governance_database_pid" =~ ^[0-9]+$ ]] || fail "未观察到真实治理事务等待 audit 锁"

  (
    curl_with_body_stdin "$quick_payload" --silent --show-error --max-time 30 -b "$owner_b_cookie_jar" \
      --config "$stale_quick_config" -H 'Content-Type: application/json' -o "$stale_quick_response" \
      -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/projects:quick-create' \
      >"$toctou_quick_status_file"
  ) &
  toctou_quick_shell_pid="$!"
  w1_toctou_quick_shell_pid="$toctou_quick_shell_pid"
  for _ in $(seq 1 200); do
    toctou_quick_database_pid="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
      SELECT blocked.pid
      FROM pg_stat_activity AS blocked
      WHERE $toctou_governance_database_pid = ANY(pg_blocking_pids(blocked.pid))
        AND blocked.pid <> $toctou_lock_database_pid
      ORDER BY blocked.pid
      LIMIT 1;")"
    [[ "$toctou_quick_database_pid" =~ ^[0-9]+$ ]] && break
    kill -0 "$toctou_quick_shell_pid" 2>/dev/null || fail "真实 QuickCreate 在治理行锁等待前退出"
    sleep 0.05
  done
  [[ "$toctou_quick_database_pid" =~ ^[0-9]+$ ]] || fail "未观察到 QuickCreate 直接等待未提交治理行锁"
  kill -0 "$toctou_quick_shell_pid" 2>/dev/null || fail "真实 QuickCreate 未在治理事务提交前保持等待"
  [[ "$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT COUNT(*)
    FROM pg_stat_activity AS blocked
    WHERE $toctou_governance_database_pid = ANY(pg_blocking_pids(blocked.pid));")" == "1" ]] || \
    fail "治理事务没有形成唯一的 QuickCreate 数据库等待者"
  during_counts="$(read_governance_quickcreate_counts "$postgres_container")" || \
    fail "治理 TOCTOU 锁竞争期间九类数据库计数读取失败"
  jq -ne --argjson before "$before_counts" --argjson during "$during_counts" '$before == $during' >/dev/null || \
    fail "治理 TOCTOU 锁竞争期间出现已提交的部分 Project 事实"

  [[ "$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc \
    "SELECT pg_terminate_backend($toctou_lock_database_pid);")" == "t" ]] || \
    fail "治理 TOCTOU audit 锁释放失败"
  wait "$toctou_lock_shell_pid" || true
  toctou_lock_shell_pid=""
  w1_toctou_lock_shell_pid=""
  w1_toctou_lock_database_pid=""
  wait "$toctou_governance_shell_pid" || fail "真实治理锁竞争请求执行失败"
  toctou_governance_shell_pid=""
  w1_toctou_governance_shell_pid=""
  wait "$toctou_quick_shell_pid" || fail "真实 QuickCreate 锁竞争请求执行失败"
  toctou_quick_shell_pid=""
  w1_toctou_quick_shell_pid=""
  suspend_status="$(tr -d '[:space:]' <"$toctou_suspend_status_file")"
  stale_quick_status="$(tr -d '[:space:]' <"$toctou_quick_status_file")"
  [[ "$suspend_status" == "200" ]] || fail "治理 TOCTOU suspend 状态为 $suspend_status"
  [[ "$stale_quick_status" == "409" ]] || fail "治理 TOCTOU QuickCreate 状态为 $stale_quick_status"
  jq -e '.error.code == "PROJECT_SKILL_UNAVAILABLE" and .error.retryable == false' "$stale_quick_response" >/dev/null || \
    fail "治理 TOCTOU QuickCreate 错误契约漂移"
  after_counts="$(read_governance_quickcreate_counts "$postgres_container")" || \
    fail "治理 TOCTOU 后九类数据库计数读取失败"
  jq -ne --argjson before "$before_counts" --argjson after "$after_counts" '$before == $after' >/dev/null || \
    fail "治理 TOCTOU QuickCreate 回滚后留下九类部分事实"
  jq -n '
    {schema_version:"w1.public-market-governance-toctou.database-fact.v1",
      governance_audit_lock_observed:true,governance_transaction_wait_observed:true,
      quickcreate_waited_on_governance:true,during_database_counts_unchanged:true,
      governance_http_status:200,quickcreate_http_status:409,
      quickcreate_error_code:"PROJECT_SKILL_UNAVAILABLE",after_database_counts_unchanged:true}' \
    >"$evidence_dir/responses/w1-public-market-governance-toctou.json"
  jq -e '
    keys == ["after_database_counts_unchanged","during_database_counts_unchanged","governance_audit_lock_observed","governance_http_status","governance_transaction_wait_observed","quickcreate_error_code","quickcreate_http_status","quickcreate_waited_on_governance","schema_version"]
    and .schema_version == "w1.public-market-governance-toctou.database-fact.v1"
    and .governance_audit_lock_observed and .governance_transaction_wait_observed
    and .quickcreate_waited_on_governance and .during_database_counts_unchanged
    and .governance_http_status == 200 and .quickcreate_http_status == 409
    and .quickcreate_error_code == "PROJECT_SKILL_UNAVAILABLE" and .after_database_counts_unchanged' \
    "$evidence_dir/responses/w1-public-market-governance-toctou.json" >/dev/null || \
    fail "治理 TOCTOU 派生 Evidence 漂移"
  suspended_etag="$(jq -er '.skill.governance_etag' "$toctou_suspend_response")"
  suspend_time="$(jq -er '.skill.transitioned_at' "$toctou_suspend_response")"
  suspend_request_id="$(jq -er '.request_id' "$toctou_suspend_response")"
  assert_governance_decision_response "$toctou_suspend_response" "$toctou_suspend_headers" "suspended" 2 '["resume","offline"]' || \
    fail "治理 suspend 响应安全契约漂移"
  assert_w1_skill_market_visibility "suspended" false || fail "Skill 暂停后 Market 未同步隐藏"
  w1_skill_market_stale_selection_fail_closed=true
  w1_public_market_governance_toctou_closed=true

  : >"$headers_file"
  suspend_replay_status="$(curl_with_body_stdin "{\"action\":\"suspend\",\"reason_code\":\"incident_containment\",\"approval_reference\":\"SMOKE-SUSPEND-${run_id}\"}" \
    --silent --show-error --max-time 10 -b "$governor_cookie_jar" --config "$conditional_config" -X POST \
    -H 'Content-Type: application/json' -D "$headers_file" -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/admin/skill-governance/${w1_skill_id}/decisions")"
  [[ "$suspend_replay_status" == "200" ]] || fail "治理 suspend 重放状态为 $suspend_replay_status"
  assert_governance_decision_response "$response_file" "$headers_file" "suspended" 2 '["resume","offline"]' || \
    fail "治理 suspend 重放响应安全契约漂移"
  jq -e --arg etag "$suspended_etag" --arg transitioned "$suspend_time" --arg request "$suspend_request_id" '
    .skill.governance_status == "suspended" and .skill.governance_epoch == 2
    and .skill.governance_etag == $etag and .skill.transitioned_at == $transitioned and .request_id != $request' \
    "$response_file" >/dev/null || fail "治理 suspend 重放未返回首次冻结业务结果"
  suspend_replay_matches="true"

  existing_session_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/agent/sessions/${w1_binding_session_id}/workspace")"
  [[ "$existing_session_status" == "200" ]] || fail "Skill 暂停后既有 Session 不可读取，状态为 $existing_session_status"

  run_w1_public_market_mixed_failure_smoke "$postgres_container"

  before_counts="$(read_governance_quickcreate_counts "$postgres_container")"
  write_conditional_curl_config "$quick_config" "$user_curl_config" "" "governance-suspended-project-${run_id}" || fail "暂停 QuickCreate curl 配置写入失败"
  suspended_quick_status="$(curl_with_body_stdin "$quick_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$quick_config" -H 'Content-Type: application/json' -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/projects:quick-create')"
  [[ "$suspended_quick_status" == "409" ]] || fail "Skill 暂停后 QuickCreate 状态为 $suspended_quick_status"
  jq -e '.error.code == "PROJECT_SKILL_UNAVAILABLE"' "$response_file" >/dev/null || fail "暂停 QuickCreate 错误码漂移"
  after_counts="$(read_governance_quickcreate_counts "$postgres_container")"
  jq -ne --argjson before "$before_counts" --argjson after "$after_counts" '$before == $after' >/dev/null || \
    fail "暂停 QuickCreate 留下部分 Project 事实"

  write_conditional_curl_config "$conditional_config" "$governor_curl_config" "$suspended_etag" "governance-resume-${run_id}" || fail "治理 resume curl 配置写入失败"
  : >"$headers_file"
  resume_status="$(curl_with_body_stdin "{\"action\":\"resume\",\"reason_code\":\"incident_resolved\",\"approval_reference\":\"SMOKE-RESUME-${run_id}\"}" \
    --silent --show-error --max-time 10 -b "$governor_cookie_jar" --config "$conditional_config" -X POST \
    -H 'Content-Type: application/json' -D "$headers_file" -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/admin/skill-governance/${w1_skill_id}/decisions")"
  [[ "$resume_status" == "200" ]] || fail "治理 resume 状态为 $resume_status"
  resumed_etag="$(jq -er '.skill.governance_etag' "$response_file")"
  assert_governance_decision_response "$response_file" "$headers_file" "active" 3 '["suspend","offline"]' || \
    fail "治理 resume 响应安全契约漂移"
  assert_w1_skill_market_visibility "resumed" true || fail "Skill 恢复后 Market 未同步重现"

  write_conditional_curl_config "$quick_config" "$user_curl_config" "" "governance-resumed-project-${run_id}" || fail "恢复 QuickCreate curl 配置写入失败"
  resumed_quick_status="$(curl_with_body_stdin "$quick_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$quick_config" -H 'Content-Type: application/json' -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/projects:quick-create')"
  [[ "$resumed_quick_status" == "201" ]] || fail "Skill 恢复后 QuickCreate 状态为 $resumed_quick_status"
  resumed_project_id="$(jq -er '.project_id | strings | select(test("^[0-9a-f-]{36}$"))' "$response_file")"
  poll_bootstrap_ready "$resumed_project_id" "$resumed_bootstrap" || fail "Skill 恢复后的新 Project 未进入 ready"

  write_conditional_curl_config "$conditional_config" "$governor_curl_config" "$resumed_etag" "governance-offline-${run_id}" || fail "治理 offline curl 配置写入失败"
  : >"$headers_file"
  offline_status="$(curl_with_body_stdin "{\"action\":\"offline\",\"reason_code\":\"repeated_violation\",\"approval_reference\":\"SMOKE-OFFLINE-${run_id}\"}" \
    --silent --show-error --max-time 10 -b "$governor_cookie_jar" --config "$conditional_config" -X POST \
    -H 'Content-Type: application/json' -D "$headers_file" -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/admin/skill-governance/${w1_skill_id}/decisions")"
  [[ "$offline_status" == "200" ]] || fail "治理 offline 状态为 $offline_status"
  offline_etag="$(jq -er '.skill.governance_etag' "$response_file")"
  assert_governance_decision_response "$response_file" "$headers_file" "offline" 4 '[]' || \
    fail "治理 offline 响应安全契约漂移"
  assert_w1_skill_market_visibility "offline" false || fail "Skill 下架后 Market 未同步隐藏"
  w1_skill_market_governance_visibility=true

  before_counts="$(read_governance_quickcreate_counts "$postgres_container")"
  write_conditional_curl_config "$quick_config" "$user_curl_config" "" "governance-offline-project-${run_id}" || fail "下架 QuickCreate curl 配置写入失败"
  offline_quick_status="$(curl_with_body_stdin "$quick_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$quick_config" -H 'Content-Type: application/json' -o "$response_file" -w '%{http_code}' \
    'http://127.0.0.1:18081/api/v1/projects:quick-create')"
  [[ "$offline_quick_status" == "409" ]] || fail "Skill 下架后 QuickCreate 状态为 $offline_quick_status"
  jq -e '.error.code == "PROJECT_SKILL_UNAVAILABLE"' "$response_file" >/dev/null || fail "下架 QuickCreate 错误码漂移"
  after_counts="$(read_governance_quickcreate_counts "$postgres_container")"
  jq -ne --argjson before "$before_counts" --argjson after "$after_counts" '$before == $after' >/dev/null || \
    fail "下架 QuickCreate 留下部分 Project 事实"

  offline_resume_before_fact="$(read_governance_skill_state_fact "$postgres_container")" || fail "offline resume 前事实读取失败"
  write_conditional_curl_config "$conditional_config" "$governor_curl_config" "$offline_etag" "governance-offline-resume-${run_id}" || fail "终态 resume curl 配置写入失败"
  : >"$headers_file"
  offline_resume_status="$(curl_with_body_stdin "{\"action\":\"resume\",\"reason_code\":\"risk_cleared\",\"approval_reference\":\"SMOKE-OFFLINE-RESUME-${run_id}\"}" \
    --silent --show-error --max-time 10 -b "$governor_cookie_jar" --config "$conditional_config" -X POST \
    -H 'Content-Type: application/json' -D "$headers_file" -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/admin/skill-governance/${w1_skill_id}/decisions")"
  [[ "$offline_resume_status" == "409" ]] || fail "offline 终态 resume 状态为 $offline_resume_status"
  assert_governance_error_response "$response_file" "$headers_file" "SKILL_GOVERNANCE_CONFLICT" || fail "offline 终态错误响应漂移"
  offline_resume_after_fact="$(read_governance_skill_state_fact "$postgres_container")" || fail "offline resume 后事实读取失败"
  jq -ne --argjson before "$offline_resume_before_fact" --argjson after "$offline_resume_after_fact" '
    $before == $after and $before.governance_status == "offline" and $before.governance_epoch == 4
    and $before.governance_receipts == 3 and $before.governance_audits == 3' >/dev/null || \
    fail "offline 终态 resume 改变了聚合、发布指针、回执或审计"
  offline_resume_state_unchanged="true"

  offline_existing_session_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/agent/sessions/${w1_binding_session_id}/workspace")"
  [[ "$offline_existing_session_status" == "200" ]] || fail "Skill 下架后既有 Session 不可读取，状态为 $offline_existing_session_status"
  jq -e --arg project "$w1_binding_project_id" --arg session "$w1_binding_session_id" '
    .session.id == $session and .session.project_id == $project and .session.status == "active"' "$response_file" >/dev/null || \
    fail "Skill 下架后既有 Session 权威投影漂移"
  existing_session_snapshot_after="$(read_existing_w1_session_snapshot_fact "$postgres_container")" || \
    fail "Skill 下架后既有 Session Snapshot 事实读取失败"
  jq -ne --argjson before "$existing_session_snapshot_before" --argjson after "$existing_session_snapshot_after" '
    $before == $after and $after.business.governance_epoch == 1 and $after.agent.governance_epoch == 1' >/dev/null || \
    fail "Skill 下架改写了既有 Session 的 Business resolution 或 Agent snapshot"
  existing_session_snapshot_unchanged="true"

  status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}")"
  [[ "$status" == "200" ]] || fail "offline 后 Creator 详情状态为 $status"
  offline_draft_etag="$(jq -er '.skill.draft_etag' "$response_file")"
  local offline_payload=""
  w1_offline_draft_name="W1 Skill offline draft ${run_id}"
  offline_payload="$(build_w1_skill_payload "$w1_offline_draft_name" "offline-review")"
  write_conditional_curl_config "$conditional_config" "$user_curl_config" "$offline_draft_etag" "" || fail "offline 草稿更新 curl 配置写入失败"
  offline_update_status="$(curl_with_body_stdin "$offline_payload" --silent --show-error --max-time 10 -b "$cookie_jar" \
    --config "$conditional_config" -X PUT -H 'Content-Type: application/json' -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}/draft")"
  [[ "$offline_update_status" == "200" ]] || fail "offline 后草稿更新状态为 $offline_update_status"
  offline_updated_etag="$(jq -er '.skill.draft_etag' "$response_file")"
  write_conditional_curl_config "$conditional_config" "$user_curl_config" "$offline_updated_etag" "governance-offline-review-${run_id}" || fail "offline 提审 curl 配置写入失败"
  offline_submit_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" --config "$conditional_config" -X POST \
    -o "$response_file" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/skills/${w1_skill_id}/reviews")"
  [[ "$offline_submit_status" == "201" ]] || fail "offline 后新 Review 提交状态为 $offline_submit_status"
  offline_review_id="$(jq -er '.review_id | strings | select(test("^[0-9a-f-]{36}$"))' "$response_file")"

  : >"$headers_file"
  offline_review_detail_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" -D "$headers_file" \
    -o "$response_file" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/admin/skill-reviews/${offline_review_id}")"
  [[ "$offline_review_detail_status" == "200" ]] || fail "offline 后新 Review 详情状态为 $offline_review_detail_status"
  offline_review_etag="$(jq -er '.review.review_etag' "$response_file")"
  [[ "$(response_header_value "$headers_file" 'ETag')" == "$offline_review_etag" ]] || fail "offline Review Header/Body ETag 不一致"
  write_conditional_curl_config "$conditional_config" "$owner_b_curl_config" "$offline_review_etag" "governance-offline-approve-${run_id}" || fail "offline 审批 curl 配置写入失败"
  offline_approve_status="$(curl_with_body_stdin '{"decision":"approved"}' --silent --show-error --max-time 10 \
    -b "$owner_b_cookie_jar" --config "$conditional_config" -X POST -H 'Content-Type: application/json' \
    -o "$response_file" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/admin/skill-reviews/${offline_review_id}/decisions")"
  [[ "$offline_approve_status" == "409" ]] || fail "offline 后批准 Review 状态为 $offline_approve_status"
  jq -e '.error.code == "SKILL_REVIEW_CONFLICT"' "$response_file" >/dev/null || fail "offline 后批准错误码漂移"

  local governor_revoke_output="$w1_temp_dir/governor-revoke.json"
  (
    cd "$repo_root/business"
    DORA_ROLE_ADMIN_POSTGRES_DSN="$BUSINESS_DATABASE_URL" GOWORK=off "$go_bin" run ./cmd/business-role-admin \
      -action revoke -assignment-id "$governor_assignment_id" -expected-version 1 \
      -target-user-id "$governor_user_id" -actor-user-id "$provisioner_user_id" \
      -role skill_governor -reason local_smoke_governance_cleanup \
      -approval-reference "local-smoke-governor-revoke-${run_id}"
  ) >"$governor_revoke_output"
  jq -e --arg assignment "$governor_assignment_id" --arg governor "$governor_user_id" '
    .action == "revoke" and .assignment_id == $assignment and .target_user_id == $governor
    and .role == "skill_governor" and .status == "revoked" and .version == 2' "$governor_revoke_output" >/dev/null || \
    fail "Governor 正式撤权结果漂移"
  governor_session_status="$(curl --silent --show-error --max-time 10 -b "$governor_cookie_jar" \
    -o "$response_file" -w '%{http_code}' 'http://127.0.0.1:18081/api/v1/auth/session')"
  [[ "$governor_session_status" == "200" ]] || fail "Governor 撤权后 Session 重新解析状态为 $governor_session_status"
  jq -e --arg governor "$governor_user_id" '.principal.id == $governor and .principal.roles == [] and .principal.capabilities == []' \
    "$response_file" >/dev/null || fail "Governor 撤权后同一 Cookie 仍保留 capability"
  revoked_role_count="$(jq -er '.principal.roles | length' "$response_file")"
  revoked_capability_count="$(jq -er '.principal.capabilities | length' "$response_file")"
  governor_denied_status="$(curl --silent --show-error --max-time 10 -b "$governor_cookie_jar" \
    -o "$response_file" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/admin/skill-governance/${w1_skill_id}")"
  [[ "$governor_denied_status" == "403" ]] || fail "Governor 撤权后治理 API 状态为 $governor_denied_status"
  governor_denied_code="$(jq -er '.error.code' "$response_file")"
  [[ "$governor_denied_code" == "SKILL_GOVERNANCE_CAPABILITY_REQUIRED" ]] || fail "Governor 撤权错误码漂移"

  final_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'governance_status', skill_record.governance_status,
      'governance_epoch', skill_record.governance_epoch,
      'version', skill_record.version,
      'current_published_snapshot_id', skill_record.current_published_snapshot_id,
      'publication_revision', skill_record.publication_revision,
      'published_count', (SELECT COUNT(*) FROM business.skill_published_snapshot WHERE skill_id = skill_record.id),
      'review_count', (SELECT COUNT(*) FROM business.skill_review_submission WHERE skill_id = skill_record.id),
      'new_review_reviewing', EXISTS (SELECT 1 FROM business.skill_review_submission WHERE id = '$offline_review_id'::uuid AND status = 'reviewing'),
      'failed_approve_receipts', (SELECT COUNT(*) FROM business.skill_command_receipt WHERE command_type = 'approve_and_publish' AND scope_id = '$offline_review_id'::uuid),
      'failed_approve_audits', (SELECT COUNT(*) FROM business.skill_governance_audit WHERE review_submission_id = '$offline_review_id'::uuid),
      'governance_receipts', (SELECT COUNT(*) FROM business.skill_command_receipt WHERE result_skill_id = skill_record.id AND command_type = 'governance_transition'),
      'governance_audits', (SELECT COUNT(*) FROM business.skill_governance_audit WHERE skill_id = skill_record.id AND action IN ('governance_suspended','governance_resumed','governance_offlined')),
      'linked_governance_facts', (SELECT COUNT(*) FROM business.skill_command_receipt AS receipt JOIN business.skill_governance_audit AS audit ON audit.command_receipt_id = receipt.id WHERE receipt.result_skill_id = skill_record.id AND receipt.command_type = 'governance_transition' AND receipt.actor_user_id = audit.actor_user_id AND receipt.response_governance_status = audit.to_status AND receipt.response_governance_epoch = audit.governance_epoch),
      'strict_governance_linkage', NOT EXISTS (
        SELECT 1
        FROM business.skill_command_receipt AS receipt
        LEFT JOIN business.skill_governance_audit AS audit ON audit.command_receipt_id = receipt.id
        WHERE receipt.result_skill_id = skill_record.id
          AND receipt.command_type = 'governance_transition'
          AND (
            audit.id IS NULL
            OR receipt.scope_id IS DISTINCT FROM skill_record.id
            OR audit.skill_id IS DISTINCT FROM skill_record.id
            OR receipt.actor_user_id IS DISTINCT FROM audit.actor_user_id
            OR receipt.request_id IS DISTINCT FROM audit.request_id
            OR receipt.response_governance_status IS DISTINCT FROM audit.to_status
            OR receipt.response_governance_epoch IS DISTINCT FROM audit.governance_epoch
            OR receipt.created_at IS DISTINCT FROM audit.occurred_at
            OR receipt.result_content_revision_id IS NOT NULL
            OR receipt.result_review_submission_id IS NOT NULL
            OR receipt.result_published_snapshot_id IS DISTINCT FROM skill_record.current_published_snapshot_id
            OR receipt.response_published_snapshot_id IS DISTINCT FROM skill_record.current_published_snapshot_id
            OR receipt.response_review_submission_id IS NOT NULL
            OR receipt.response_review_status IS NOT NULL
            OR receipt.response_review_reason_code IS NOT NULL
            OR receipt.response_review_updated_at IS NOT NULL
            OR audit.review_submission_id IS NOT NULL
            OR audit.actor_role_key IS DISTINCT FROM 'skill_governor'
            OR audit.safe_reason_code IS NULL
            OR audit.approval_reference IS NULL
            OR audit.source_address IS NULL
          )
      ),
      'transition_matrix_matches', (SELECT COUNT(*) FROM business.skill_governance_audit WHERE skill_id = skill_record.id AND ((action = 'governance_suspended' AND from_status = 'active' AND to_status = 'suspended' AND governance_epoch = 2) OR (action = 'governance_resumed' AND from_status = 'suspended' AND to_status = 'active' AND governance_epoch = 3) OR (action = 'governance_offlined' AND from_status = 'active' AND to_status = 'offline' AND governance_epoch = 4))),
      'original_resolution_epoch_one', EXISTS (SELECT 1 FROM business.project_session_skill_resolution AS resolution JOIN business.project_session_skill_resolution_item AS item ON item.resolution_id = resolution.id WHERE resolution.project_id = '$w1_binding_project_id'::uuid AND item.skill_id = skill_record.id AND item.governance_epoch = 1),
      'resumed_resolution_epoch_three', EXISTS (SELECT 1 FROM business.project_session_skill_resolution AS resolution JOIN business.project_session_skill_resolution_item AS item ON item.resolution_id = resolution.id WHERE resolution.project_id = '$resumed_project_id'::uuid AND item.skill_id = skill_record.id AND item.governance_epoch = 3)
    ) FROM business.skill AS skill_record WHERE skill_record.id = '$w1_skill_id'::uuid;")"
  jq -e --arg pointer "$(jq -er '.published_snapshot_id' <<<"$baseline_fact")" \
    --argjson base_version "$(jq -er '.version' <<<"$baseline_fact")" \
    --argjson offline_version "$(jq -er '.version' <<<"$offline_resume_before_fact")" '
    .governance_status == "offline" and .governance_epoch == 4
    and $offline_version == ($base_version + 3)
    and .current_published_snapshot_id == $pointer and .publication_revision == 1 and .published_count == 1
    and .review_count == 2 and .new_review_reviewing and .failed_approve_receipts == 0 and .failed_approve_audits == 0
    and .governance_receipts == 3 and .governance_audits == 3 and .linked_governance_facts == 3
    and .strict_governance_linkage and .transition_matrix_matches == 3
    and .original_resolution_epoch_one and .resumed_resolution_epoch_three' \
    <<<"$final_fact" >/dev/null || fail "治理终态、回执审计、发布指针或 Snapshot epoch 事实漂移"
  final_ready_status="$(curl --silent --show-error --max-time 10 -o /dev/null -w '%{http_code}' 'http://127.0.0.1:18081/readyz')"
  [[ "$final_ready_status" == "200" ]] || fail "治理事实落库后 Business Readiness 状态为 $final_ready_status"

  status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" -o "$response_file" -w '%{http_code}' \
    "http://127.0.0.1:18081/api/v1/agent/sessions/${w1_public_market_session_id}/workspace")"
  [[ "$status" == "200" ]] || fail "Public Market Skill 下架/新 Draft 后既有消费者 Session 不可读取，状态为 $status"
  jq -e --arg project "$w1_public_market_project_id" --arg session "$w1_public_market_session_id" '
    .session.id == $session and .session.project_id == $project and .session.status == "active"' "$response_file" >/dev/null || \
    fail "Public Market 既有消费者 Session 权威投影漂移"
  public_market_snapshot_after="$(read_public_market_session_snapshot_fact "$postgres_container")" || \
    fail "Public Market 治理/新 Draft 后 Snapshot 事实读取失败"
  jq -ne --argjson before "$w1_public_market_snapshot_before" --argjson after "$public_market_snapshot_after" '
    $before == $after and $after.business.governance_epoch == 1 and $after.agent.governance_epoch == 1' >/dev/null || \
    fail "治理或新 Draft 改写了 Public Market 既有消费者 Session Snapshot"
  printf '%s\n' "$public_market_snapshot_after" >"$evidence_dir/responses/w1-public-market-binding-snapshot-after.json"
  w1_public_market_publisher_snapshot_frozen=true

  governance_produced_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  jq -n --arg run_id "$run_id" --arg produced_at "$governance_produced_at" --arg skill_id "$w1_skill_id" \
    --arg resumed_project_id "$resumed_project_id" --arg offline_review_id "$offline_review_id" \
    --arg governor_role "$governor_role" --arg governor_capability "$governor_capability" \
    --arg creator_forbidden_code "$creator_forbidden_code" --arg reviewer_forbidden_code "$reviewer_forbidden_code" \
    --arg governor_review_forbidden_code "$governor_review_forbidden_code" --arg governor_denied_code "$governor_denied_code" \
    --arg source_digest_sha256 "$source_digest_sha256" --arg business_binary_sha256 "$business_binary_sha256" \
    --arg agent_binary_sha256 "$agent_binary_sha256" --argjson creator_forbidden "$creator_forbidden_status" \
    --argjson reviewer_forbidden "$reviewer_forbidden_status" --argjson governor_review_forbidden "$governor_review_forbidden_status" \
    --argjson queue_status "$queue_status" --argjson detail_status "$detail_status" \
    --argjson suspend_status "$suspend_status" --argjson suspend_replay_status "$suspend_replay_status" \
    --argjson existing_session_status "$existing_session_status" --argjson suspended_quick_status "$suspended_quick_status" \
    --argjson resume_status "$resume_status" --argjson resumed_quick_status "$resumed_quick_status" \
    --argjson offline_status "$offline_status" --argjson offline_quick_status "$offline_quick_status" \
    --argjson offline_existing_session_status "$offline_existing_session_status" \
    --argjson offline_resume_status "$offline_resume_status" --argjson offline_update_status "$offline_update_status" \
    --argjson offline_submit_status "$offline_submit_status" --argjson offline_review_detail_status "$offline_review_detail_status" \
    --argjson offline_approve_status "$offline_approve_status" --argjson governor_session_status "$governor_session_status" \
    --argjson governor_denied_status "$governor_denied_status" --argjson revoked_role_count "$revoked_role_count" \
    --argjson revoked_capability_count "$revoked_capability_count" --argjson suspend_replay_matches "$suspend_replay_matches" \
    --argjson offline_resume_state_unchanged "$offline_resume_state_unchanged" \
    --argjson existing_session_snapshot_unchanged "$existing_session_snapshot_unchanged" \
    --argjson final_ready_status "$final_ready_status" \
    --argjson database "$final_fact" '
    {schema_version:"w1.skill-governance.smoke.evidence.v1",status:"pending",run_id:$run_id,produced_at:$produced_at,
      source_digest_sha256:$source_digest_sha256,business_binary_sha256:$business_binary_sha256,agent_binary_sha256:$agent_binary_sha256,
      skill_id:$skill_id,resumed_project_id:$resumed_project_id,offline_review_id:$offline_review_id,
      facts:{governance_status:$database.governance_status,governance_epoch:$database.governance_epoch,
        governance_receipts:$database.governance_receipts,governance_audits:$database.governance_audits,
        linked_governance_facts:$database.linked_governance_facts,published_count:$database.published_count,
        review_count:$database.review_count,strict_governance_linkage:$database.strict_governance_linkage,
        offline_resume_state_unchanged:$offline_resume_state_unchanged,
        existing_session_snapshot_unchanged:$existing_session_snapshot_unchanged},
      assertions:{
        skill_governor_rbac:($governor_role == "skill_governor" and $governor_capability == "skill.govern"
          and $creator_forbidden == 403 and $creator_forbidden_code == "SKILL_GOVERNANCE_CAPABILITY_REQUIRED"
          and $reviewer_forbidden == 403 and $reviewer_forbidden_code == "SKILL_GOVERNANCE_CAPABILITY_REQUIRED"
          and $governor_review_forbidden == 403 and $governor_review_forbidden_code == "SKILL_REVIEW_CAPABILITY_REQUIRED"
          and $queue_status == 200 and $detail_status == 200),
        skill_governor_revocation:($governor_session_status == 200 and $revoked_role_count == 0 and $revoked_capability_count == 0
          and $governor_denied_status == 403 and $governor_denied_code == "SKILL_GOVERNANCE_CAPABILITY_REQUIRED"),
        skill_governance_idempotency:($suspend_status == 200 and $suspend_replay_status == 200
          and $suspend_replay_matches
          and $database.governance_receipts == 3 and $database.governance_audits == 3
          and $database.linked_governance_facts == 3 and $database.strict_governance_linkage
          and $final_ready_status == 200),
        skill_governance_quickcreate_gate:($existing_session_status == 200 and $suspended_quick_status == 409
          and $resume_status == 200 and $resumed_quick_status == 201 and $offline_quick_status == 409
          and $offline_existing_session_status == 200 and $existing_session_snapshot_unchanged
          and $database.original_resolution_epoch_one and $database.resumed_resolution_epoch_three),
        skill_governance_offline_terminal:($offline_status == 200 and $offline_resume_status == 409
          and $offline_resume_state_unchanged
          and $offline_update_status == 200 and $offline_submit_status == 201 and $offline_review_detail_status == 200
          and $offline_approve_status == 409 and $database.governance_status == "offline" and $database.governance_epoch == 4
          and $database.published_count == 1 and $database.new_review_reviewing
          and $database.failed_approve_receipts == 0 and $database.failed_approve_audits == 0)
      }}' >"$governance_pending_evidence_file"
  jq -e 'keys == ["agent_binary_sha256","assertions","business_binary_sha256","facts","offline_review_id","produced_at","resumed_project_id","run_id","schema_version","skill_id","source_digest_sha256","status"]
    and .schema_version == "w1.skill-governance.smoke.evidence.v1" and .status == "pending"
    and (.facts | keys) == ["existing_session_snapshot_unchanged","governance_audits","governance_epoch","governance_receipts","governance_status","linked_governance_facts","offline_resume_state_unchanged","published_count","review_count","strict_governance_linkage"]
    and (.assertions | keys) == ["skill_governance_idempotency","skill_governance_offline_terminal","skill_governance_quickcreate_gate","skill_governor_rbac","skill_governor_revocation"]
    and all(.assertions[]; ((. | type) == "boolean" and . == true))' "$governance_pending_evidence_file" >/dev/null || \
    fail "Skill Governance Evidence 含未通过或非闭集断言"

  jq -n --arg run_id "$run_id" --arg produced_at "$governance_produced_at" \
    --arg source_digest_sha256 "$source_digest_sha256" --arg business_binary_sha256 "$business_binary_sha256" \
    --argjson public_read "$w1_skill_market_public_read" \
    --argjson safe_projection "$w1_skill_market_safe_projection" \
    --argjson keyset_pagination "$w1_skill_market_keyset_pagination" \
    --argjson governance_visibility "$w1_skill_market_governance_visibility" \
    --argjson cursor_fail_closed "$w1_skill_market_cursor_fail_closed" \
    --argjson stale_selection_fail_closed "$w1_skill_market_stale_selection_fail_closed" '
    {schema_version:"w1.skill-market.smoke.evidence.v2",status:"pending",run_id:$run_id,produced_at:$produced_at,
      source_digest_sha256:$source_digest_sha256,business_binary_sha256:$business_binary_sha256,
      assertions:{
        skill_market_public_read:$public_read,
        skill_market_safe_projection:$safe_projection,
        skill_market_keyset_pagination:$keyset_pagination,
        skill_market_governance_visibility:$governance_visibility,
        skill_market_cursor_fail_closed:$cursor_fail_closed,
        skill_market_stale_selection_fail_closed:$stale_selection_fail_closed
      }}' >"$skill_market_pending_evidence_file"
  jq -e '
    keys == ["assertions","business_binary_sha256","produced_at","run_id","schema_version","source_digest_sha256","status"]
    and .schema_version == "w1.skill-market.smoke.evidence.v2" and .status == "pending"
    and (.assertions | keys) == ["skill_market_cursor_fail_closed","skill_market_governance_visibility","skill_market_keyset_pagination","skill_market_public_read","skill_market_safe_projection","skill_market_stale_selection_fail_closed"]
    and all(.assertions[]; ((. | type) == "boolean" and . == true))' "$skill_market_pending_evidence_file" >/dev/null || \
    fail "Skill Market Evidence 含未通过或非闭集断言"
  w1_skill_governance_smoke_ran=true
  w1_skill_market_smoke_ran=true
}

write_w1_skill_market_binding_evidence() {
  local produced_at=""
  produced_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  jq -n --arg run_id "$run_id" --arg produced_at "$produced_at" \
    --arg source_digest_sha256 "$source_digest_sha256" \
    --arg business_binary_sha256 "$business_binary_sha256" --arg agent_binary_sha256 "$agent_binary_sha256" \
    --argjson quickcreate "$w1_public_market_quickcreate" \
    --argjson identity_separation "$w1_public_market_permission_identity_separation" \
    --argjson snapshot_frozen "$w1_public_market_publisher_snapshot_frozen" \
    --argjson governance_toctou "$w1_public_market_governance_toctou_closed" \
    --argjson mixed_atomicity "$w1_public_market_mixed_binding_atomicity" \
    --argjson login_recovered "$w1_public_market_login_preselection_recovered" \
    --argjson idempotency_replay "$w1_public_market_idempotency_frozen_replay" '
    {schema_version:"w1.skill-market-binding.smoke.evidence.v1",status:"pending",run_id:$run_id,produced_at:$produced_at,
      source_digest_sha256:$source_digest_sha256,business_binary_sha256:$business_binary_sha256,
      agent_binary_sha256:$agent_binary_sha256,
      assertions:{
        public_market_quickcreate:$quickcreate,
        public_market_permission_identity_separation:$identity_separation,
        public_market_publisher_snapshot_frozen:$snapshot_frozen,
        public_market_governance_toctou_closed:$governance_toctou,
        public_market_mixed_binding_atomicity:$mixed_atomicity,
        public_market_login_preselection_recovered:$login_recovered,
        public_market_idempotency_frozen_replay:$idempotency_replay
      }}' >"$skill_market_binding_pending_evidence_file"
  jq -e '
    keys == ["agent_binary_sha256","assertions","business_binary_sha256","produced_at","run_id","schema_version","source_digest_sha256","status"]
    and .schema_version == "w1.skill-market-binding.smoke.evidence.v1" and .status == "pending"
    and (.assertions | keys) == ["public_market_governance_toctou_closed","public_market_idempotency_frozen_replay","public_market_login_preselection_recovered","public_market_mixed_binding_atomicity","public_market_permission_identity_separation","public_market_publisher_snapshot_frozen","public_market_quickcreate"]
    and all(.assertions[]; ((. | type) == "boolean" and . == true))' \
    "$skill_market_binding_pending_evidence_file" >/dev/null || \
    fail "Public Market Binding Evidence 含未通过或非闭集断言"
  w1_skill_market_binding_smoke_ran=true
}

run_w1_browser_frozen_smoke() {
  local postgres_container="$1"
  local browser_result_file="$2"
  local owner_api_raw="$w1_temp_dir/browser-owner-detail.json"
  local review_api_raw="$w1_temp_dir/browser-review-detail.json"
  local second_review_api_raw="$w1_temp_dir/browser-second-review-detail.json"
  local bootstrap_file="$evidence_dir/responses/w1-browser-bootstrap.json"
  local old_verifier_file="$evidence_dir/responses/w1-browser-republish-old-agent-verified.json"
  local new_verifier_file="$evidence_dir/responses/w1-browser-republish-new-agent-verified.json"
  local preselection_database_file="$evidence_dir/responses/w1-browser-public-market-preselection-database.json"
  local browser_creator_id=""
  local browser_reviewer_id=""
  local browser_skill_id=""
  local browser_review_id=""
  local browser_snapshot_id=""
  local browser_project_id=""
  local second_review_id=""
  local second_snapshot_id=""
  local new_project_id=""
  local new_session_id=""
  local public_market_consumer_id=""
  local public_market_project_id=""
  local public_market_session_id=""
  local public_market_selected_skill_id=""
  local browser_catalog_session_id=""
  local browser_catalog_request_id=""
  local browser_catalog_exact_unavailable=""
  local creator_admin_denial_request_id=""
  local creator_admin_denial_audit_fact=""
  local creator_admin_denial_audit_count=""
  local creator_admin_denial_audited="false"
  local submitted_summary=""
  local current_draft_summary=""
  local submitted_summary_b64=""
  local current_draft_summary_b64=""
  local owner_api_status=""
  local review_api_status=""
  local second_review_api_status=""
  local browser_session_id=""
  local browser_input_id=""
  local business_fact=""
  local agent_fact=""
  local republish_business_fact=""
  local republish_agent_fact=""
  local public_market_browser_fact=""

  [[ -s "$browser_result_file" ]] || fail "W1 浏览器未产出结构化真实结果"
  jq -e --arg creator "$user_id" --arg reviewer "$owner_b_seed_user_id" '
    keys == ["creator_admin_api_forbidden","creator_admin_denial_request_id","creator_admin_implicit_api_blocked","creator_admin_route_blocked","creator_id","current_draft_summary","market_public_detail","market_public_list","market_published_projection_safe","new_project_id","new_quickcreate_replay_matches","new_session_id","old_quickcreate_replay_matches","old_workspace_revisited","owner_published_projection_no_version_ui","project_id","public_market_consumer_id","public_market_login_preselection_recovered","public_market_pre_submit_quickcreate_count","public_market_project_id","public_market_selected_skill_id","public_market_session_id","public_market_submit_quickcreate_count","published_snapshot_id","review_id","reviewer_id","reviewer_owner_read_not_found","reviewer_owner_resource_facts_not_disclosed","reviewer_owner_route_not_found","reviewer_owner_write_not_found","schema_version","second_decision_replay_matches","second_published_snapshot_id","second_review_id","second_review_replay_matches","skill_id","submitted_summary","tool_catalog_exact_unavailable","tool_catalog_request_id","tool_catalog_session_id"]
    and .schema_version == "w1.real-review-result.v6"
    and .creator_admin_route_blocked == true
    and .creator_admin_implicit_api_blocked == true
    and .creator_admin_api_forbidden == true
    and .reviewer_owner_route_not_found == true
    and .reviewer_owner_read_not_found == true
    and .reviewer_owner_write_not_found == true
    and .reviewer_owner_resource_facts_not_disclosed == true
    and .market_public_list == true
    and .market_public_detail == true
    and .market_published_projection_safe == true
    and .creator_id == $creator and .reviewer_id == $reviewer and .creator_id != .reviewer_id
    and .public_market_consumer_id == $reviewer
    and .public_market_selected_skill_id == .skill_id
    and .public_market_login_preselection_recovered == true
    and .public_market_pre_submit_quickcreate_count == 0
    and .public_market_submit_quickcreate_count == 1
    and .second_review_replay_matches == true
    and .second_decision_replay_matches == true
    and .old_quickcreate_replay_matches == true
    and .new_quickcreate_replay_matches == true
    and .owner_published_projection_no_version_ui == true
    and .old_workspace_revisited == true
    and .review_id != .second_review_id
    and .published_snapshot_id != .second_published_snapshot_id
    and .project_id != .new_project_id
    and .tool_catalog_session_id != .new_session_id
    and ([.creator_id,.reviewer_id,.skill_id,.review_id,.published_snapshot_id,.project_id,.second_review_id,.second_published_snapshot_id,.new_project_id,.new_session_id,.public_market_project_id,.public_market_session_id,.tool_catalog_session_id,.tool_catalog_request_id,.creator_admin_denial_request_id]
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
  second_review_id="$(jq -er '.second_review_id' "$browser_result_file")"
  second_snapshot_id="$(jq -er '.second_published_snapshot_id' "$browser_result_file")"
  new_project_id="$(jq -er '.new_project_id' "$browser_result_file")"
  new_session_id="$(jq -er '.new_session_id' "$browser_result_file")"
  public_market_consumer_id="$(jq -er '.public_market_consumer_id' "$browser_result_file")"
  public_market_project_id="$(jq -er '.public_market_project_id' "$browser_result_file")"
  public_market_session_id="$(jq -er '.public_market_session_id' "$browser_result_file")"
  public_market_selected_skill_id="$(jq -er '.public_market_selected_skill_id' "$browser_result_file")"
  browser_catalog_session_id="$(jq -er '.tool_catalog_session_id' "$browser_result_file")"
  browser_catalog_request_id="$(jq -er '.tool_catalog_request_id' "$browser_result_file")"
  browser_catalog_exact_unavailable="$(jq -er '.tool_catalog_exact_unavailable' "$browser_result_file")"
  creator_admin_denial_request_id="$(jq -er '.creator_admin_denial_request_id' "$browser_result_file")"
  submitted_summary="$(jq -er '.submitted_summary' "$browser_result_file")"
  current_draft_summary="$(jq -er '.current_draft_summary' "$browser_result_file")"
  submitted_summary_b64="$(printf '%s' "$submitted_summary" | base64 | tr -d '\n')"
  current_draft_summary_b64="$(printf '%s' "$current_draft_summary" | base64 | tr -d '\n')"

  jq -e --arg consumer "$public_market_consumer_id" --arg skill "$public_market_selected_skill_id" '
    keys == ["before_login","before_submit","consumer_id","database_counts_unchanged","schema_version","skill_id"]
    and .schema_version == "w1.public-market-preselection.database-fact.v1"
    and .consumer_id == $consumer and .skill_id == $skill and .database_counts_unchanged
    and .before_login == .before_submit
    and (.before_login | keys) == ["binding_audits","binding_sets","bindings","outboxes","projects","receipts","resolution_items","resolutions","session_bindings"]' \
    "$preselection_database_file" >/dev/null || \
    fail "W1 Browser 登录预选双阶段数据库零增量事实与浏览器身份不一致"

  public_market_browser_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT json_build_object(
      'project_count', (SELECT COUNT(*) FROM business.project WHERE id = '$public_market_project_id'::uuid AND owner_user_id = '$public_market_consumer_id'::uuid),
      'resolution_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution WHERE project_id = '$public_market_project_id'::uuid AND owner_user_id = '$public_market_consumer_id'::uuid),
      'item_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution_item WHERE project_id = '$public_market_project_id'::uuid AND skill_id = '$public_market_selected_skill_id'::uuid AND publisher_user_id = '$browser_creator_id'::uuid),
      'business_permission_digest', (SELECT encode(permission_snapshot_digest, 'hex') FROM business.project_session_skill_resolution_item WHERE project_id = '$public_market_project_id'::uuid AND skill_id = '$public_market_selected_skill_id'::uuid),
      'session_binding_ready', EXISTS (SELECT 1 FROM business.project_session_binding WHERE project_id = '$public_market_project_id'::uuid AND agent_session_id = '$public_market_session_id'::uuid AND provisioning_status = 'ready')
    );")" || fail "W1 Browser Public Market Business 事实读取失败"
  local public_market_browser_agent_fact=""
  public_market_browser_agent_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent -Atc "
    SELECT json_build_object(
      'session_count', (SELECT COUNT(*) FROM agent.session WHERE id = '$public_market_session_id'::uuid AND project_id = '$public_market_project_id'::uuid AND user_id = '$public_market_consumer_id'::uuid),
      'item_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot_item WHERE session_id = '$public_market_session_id'::uuid AND skill_id = '$public_market_selected_skill_id'::uuid AND publisher_user_id = '$browser_creator_id'::uuid),
      'agent_permission_digest', (SELECT permission_snapshot_digest FROM agent.session_skill_snapshot_item WHERE session_id = '$public_market_session_id'::uuid AND skill_id = '$public_market_selected_skill_id'::uuid)
    );")" || fail "W1 Browser Public Market Agent 事实读取失败"
  jq -cn --argjson business "$public_market_browser_fact" --argjson agent "$public_market_browser_agent_fact" \
    '{business:$business,agent:$agent}' >"$evidence_dir/responses/w1-browser-public-market-binding.json"
  jq -e '
    .business.project_count == 1 and .business.resolution_count == 1 and .business.item_count == 1
    and .business.session_binding_ready and (.business.business_permission_digest | test("^[0-9a-f]{64}$"))
    and .agent.session_count == 1 and .agent.item_count == 1
    and .agent.agent_permission_digest == .business.business_permission_digest' \
    "$evidence_dir/responses/w1-browser-public-market-binding.json" >/dev/null || \
    fail "W1 Browser Public Market 消费者/Publisher/Agent Snapshot 事实不一致"
  w1_public_market_login_preselection_recovered=true

  creator_admin_denial_audit_fact="$(sed -n '/^{/p' "$evidence_dir/business.log" \
    | jq -cs --arg actor "$browser_creator_id" --arg request "$creator_admin_denial_request_id" '
      map(select(.actor_id == $actor and .request_id == $request)) as $events
      | {count:($events | length),audited:(($events | length) == 1
          and $events[0].event_type == "security.authorization.v1"
          and $events[0].route == "/api/v1/admin/skill-reviews"
          and $events[0].action == "list"
          and $events[0].decision == "denied"
          and $events[0].actor_id == $actor
          and $events[0].request_id == $request
          and $events[0].error_code == "SKILL_REVIEW_CAPABILITY_REQUIRED")}'
  )"
  creator_admin_denial_audit_count="$(jq -er '.count' <<<"$creator_admin_denial_audit_fact")"
  creator_admin_denial_audited="$(jq -er '.audited' <<<"$creator_admin_denial_audit_fact")"
  [[ "$creator_admin_denial_audit_count" == "1" && "$creator_admin_denial_audited" == "true" ]] || \
    fail "W1 Creator Reviewer API 拒绝未关联唯一结构化授权审计事件"

  owner_api_status="$(curl --silent --show-error --max-time 10 -b "$cookie_jar" \
    -o "$owner_api_raw" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/skills/${browser_skill_id}")"
  [[ "$owner_api_status" == "200" ]] || fail "W1 Browser Skill 正式 Owner API 状态为 $owner_api_status"
  review_api_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    -o "$review_api_raw" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/admin/skill-reviews/${browser_review_id}")"
  [[ "$review_api_status" == "200" ]] || fail "W1 Browser Review 正式 Reviewer API 状态为 $review_api_status"
  second_review_api_status="$(curl --silent --show-error --max-time 10 -b "$owner_b_cookie_jar" \
    -o "$second_review_api_raw" -w '%{http_code}' "http://127.0.0.1:18081/api/v1/admin/skill-reviews/${second_review_id}")"
  [[ "$second_review_api_status" == "200" ]] || fail "W1 Browser 第二次 Review 正式 Reviewer API 状态为 $second_review_api_status"

  jq -n --argjson owner_status "$owner_api_status" --argjson review_status "$review_api_status" \
    --argjson second_review_status "$second_review_api_status" \
    --argjson tool_catalog_exact_unavailable "$browser_catalog_exact_unavailable" \
    --arg creator "$browser_creator_id" --arg skill "$browser_skill_id" --arg review "$browser_review_id" \
    --arg snapshot "$browser_snapshot_id" --arg second_review "$second_review_id" \
    --arg second_snapshot "$second_snapshot_id" --arg submitted "$submitted_summary" --arg current "$current_draft_summary" \
    --arg tool_catalog_session_id "$browser_catalog_session_id" --arg tool_catalog_request_id "$browser_catalog_request_id" \
    --slurpfile owner "$owner_api_raw" --slurpfile detail "$review_api_raw" \
    --slurpfile second_detail "$second_review_api_raw" '
    {owner_status:$owner_status,review_status:$review_status,second_review_status:$second_review_status,
      skill_id:$skill,review_id:$review,published_snapshot_id:$snapshot,
      second_review_id:$second_review,second_published_snapshot_id:$second_snapshot,
      tool_catalog_session_id:$tool_catalog_session_id,tool_catalog_request_id:$tool_catalog_request_id,
      tool_catalog_exact_unavailable:$tool_catalog_exact_unavailable,
      owner_current_published_is_b:($owner[0].skill.skill_id == $skill
        and $owner[0].skill.definition.summary == $current
        and $owner[0].skill.content_status == "published"
        and $owner[0].skill.has_unpublished_changes == false
        and $owner[0].skill.review_status == "approved"),
      review_frozen_submission_is_a:($detail[0].review.review_id == $review
        and $detail[0].review.skill_id == $skill
        and $detail[0].review.owner_user_id == $creator
        and $detail[0].review.status == "approved"
        and $detail[0].review.definition.summary == $submitted
        and $detail[0].review.definition.summary != $current),
      first_review_observes_current_b:($detail[0].review.current_published.published_snapshot_id == $second_snapshot
        and $detail[0].review.current_published.definition.summary == $current
        and $detail[0].review.current_published.definition.summary != $submitted
        and $detail[0].review.comparison == {has_current_published:true,same_content:false}
        and $detail[0].review.allowed_actions == []),
      second_review_frozen_and_current_is_b:($second_detail[0].review.review_id == $second_review
        and $second_detail[0].review.skill_id == $skill
        and $second_detail[0].review.owner_user_id == $creator
        and $second_detail[0].review.status == "approved"
        and $second_detail[0].review.definition.summary == $current
        and $second_detail[0].review.current_published.published_snapshot_id == $second_snapshot
        and $second_detail[0].review.current_published.definition.summary == $current
        and $second_detail[0].review.comparison == {has_current_published:true,same_content:true}
        and $second_detail[0].review.allowed_actions == [])}' \
    >"$evidence_dir/responses/w1-browser-frozen-api.json"
  jq -e '.owner_status == 200 and .review_status == 200 and .second_review_status == 200
    and .owner_current_published_is_b and .review_frozen_submission_is_a
    and .first_review_observes_current_b and .second_review_frozen_and_current_is_b' \
    "$evidence_dir/responses/w1-browser-frozen-api.json" >/dev/null || \
    fail "W1 Browser 正式 API 未证明 A/B 两次审核冻结与当前发布 B"

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
        JOIN business.skill_published_snapshot AS published ON published.id = '$browser_snapshot_id'::uuid
          AND published.skill_id = skill_record.id
        WHERE skill_record.id = '$browser_skill_id'::uuid
          AND published.source_content_revision_id = review.content_revision_id
          AND published.content_digest = review.content_digest
          AND skill_record.current_published_snapshot_id = '$second_snapshot_id'::uuid
          AND skill_record.current_draft_revision_id = (SELECT second_review.content_revision_id
            FROM business.skill_review_submission AS second_review
            WHERE second_review.id = '$second_review_id'::uuid)),
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

  republish_business_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -Atc "
    SELECT jsonb_build_object(
      'skill_count', (SELECT COUNT(*) FROM business.skill
        WHERE id = '$browser_skill_id'::uuid AND owner_user_id = '$browser_creator_id'::uuid),
      'publication_revision', (SELECT publication_revision FROM business.skill
        WHERE id = '$browser_skill_id'::uuid),
      'current_pointer_is_b', EXISTS (SELECT 1 FROM business.skill
        WHERE id = '$browser_skill_id'::uuid AND current_published_snapshot_id = '$second_snapshot_id'::uuid),
      'review_count', (SELECT COUNT(*) FROM business.skill_review_submission
        WHERE skill_id = '$browser_skill_id'::uuid),
      'content_revision_count', (SELECT COUNT(*) FROM business.skill_content_revision
        WHERE skill_id = '$browser_skill_id'::uuid),
      'first_review_count', (SELECT COUNT(*) FROM business.skill_review_submission
        WHERE id = '$browser_review_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND status = 'approved' AND submitted_by_user_id = '$browser_creator_id'::uuid
          AND decided_by_user_id = '$browser_reviewer_id'::uuid),
      'second_review_count', (SELECT COUNT(*) FROM business.skill_review_submission
        WHERE id = '$second_review_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND status = 'approved' AND submitted_by_user_id = '$browser_creator_id'::uuid
          AND decided_by_user_id = '$browser_reviewer_id'::uuid),
      'snapshot_count', (SELECT COUNT(*) FROM business.skill_published_snapshot
        WHERE skill_id = '$browser_skill_id'::uuid),
      'first_snapshot_count', (SELECT COUNT(*) FROM business.skill_published_snapshot
        WHERE id = '$browser_snapshot_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND review_submission_id = '$browser_review_id'::uuid AND publication_revision = 1),
      'second_snapshot_count', (SELECT COUNT(*) FROM business.skill_published_snapshot
        WHERE id = '$second_snapshot_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND review_submission_id = '$second_review_id'::uuid AND publication_revision = 2),
      'revision_lineage_consistent', EXISTS (
        SELECT 1
        FROM business.skill_review_submission AS first_review
        JOIN business.skill_content_revision AS first_revision
          ON first_revision.id = first_review.content_revision_id
          AND first_revision.skill_id = first_review.skill_id
        JOIN business.skill_published_snapshot AS first_snapshot
          ON first_snapshot.id = '$browser_snapshot_id'::uuid
          AND first_snapshot.skill_id = first_review.skill_id
          AND first_snapshot.review_submission_id = first_review.id
          AND first_snapshot.source_content_revision_id = first_revision.id
        JOIN business.skill_review_submission AS second_review
          ON second_review.id = '$second_review_id'::uuid
          AND second_review.skill_id = first_review.skill_id
        JOIN business.skill_content_revision AS second_revision
          ON second_revision.id = second_review.content_revision_id
          AND second_revision.skill_id = second_review.skill_id
        JOIN business.skill_published_snapshot AS second_snapshot
          ON second_snapshot.id = '$second_snapshot_id'::uuid
          AND second_snapshot.skill_id = second_review.skill_id
          AND second_snapshot.review_submission_id = second_review.id
          AND second_snapshot.source_content_revision_id = second_revision.id
        WHERE first_review.id = '$browser_review_id'::uuid
          AND first_review.skill_id = '$browser_skill_id'::uuid
          AND first_revision.id <> second_revision.id
          AND first_revision.content_digest = first_review.content_digest
          AND first_review.content_digest = first_snapshot.content_digest
          AND second_revision.content_digest = second_review.content_digest
          AND second_review.content_digest = second_snapshot.content_digest
          AND first_revision.content_digest <> second_revision.content_digest),
      'skill_create_receipt_count', (SELECT COUNT(*) FROM business.skill_command_receipt
        WHERE result_skill_id = '$browser_skill_id'::uuid AND command_type = 'create'),
      'skill_submit_receipt_count', (SELECT COUNT(*) FROM business.skill_command_receipt
        WHERE result_skill_id = '$browser_skill_id'::uuid AND command_type = 'submit_review'),
      'skill_submit_result_count', (SELECT COUNT(DISTINCT result_review_submission_id)
        FROM business.skill_command_receipt
        WHERE result_skill_id = '$browser_skill_id'::uuid AND command_type = 'submit_review'
          AND result_review_submission_id IN ('$browser_review_id'::uuid, '$second_review_id'::uuid)),
      'skill_approve_receipt_count', (SELECT COUNT(*) FROM business.skill_command_receipt
        WHERE result_skill_id = '$browser_skill_id'::uuid AND command_type = 'approve_and_publish'),
      'skill_approve_result_count', (SELECT COUNT(DISTINCT result_published_snapshot_id)
        FROM business.skill_command_receipt
        WHERE result_skill_id = '$browser_skill_id'::uuid AND command_type = 'approve_and_publish'
          AND result_published_snapshot_id IN ('$browser_snapshot_id'::uuid, '$second_snapshot_id'::uuid)),
      'skill_publish_audit_count', (SELECT COUNT(*) FROM business.skill_governance_audit
        WHERE skill_id = '$browser_skill_id'::uuid AND action = 'review_approved_and_published'
          AND actor_user_id = '$browser_reviewer_id'::uuid
          AND review_submission_id IN ('$browser_review_id'::uuid, '$second_review_id'::uuid)),
      'skill_publish_audit_review_count', (SELECT COUNT(DISTINCT review_submission_id)
        FROM business.skill_governance_audit
        WHERE skill_id = '$browser_skill_id'::uuid AND action = 'review_approved_and_published'
          AND actor_user_id = '$browser_reviewer_id'::uuid
          AND review_submission_id IN ('$browser_review_id'::uuid, '$second_review_id'::uuid)),
      'old_project_creation_receipt_count', (SELECT COUNT(*) FROM business.project_creation_receipt
        WHERE project_id = '$browser_project_id'::uuid AND owner_user_id = '$browser_creator_id'::uuid
          AND command_type = 'quick_create' AND request_schema_version = 'project_quick_create.v2'
          AND skill_count = 1),
      'new_project_creation_receipt_count', (SELECT COUNT(*) FROM business.project_creation_receipt
        WHERE project_id = '$new_project_id'::uuid AND owner_user_id = '$browser_creator_id'::uuid
          AND command_type = 'quick_create' AND request_schema_version = 'project_quick_create.v2'
          AND skill_count = 1),
      'old_project_count', (SELECT COUNT(*) FROM business.project
        WHERE id = '$browser_project_id'::uuid AND owner_user_id = '$browser_creator_id'::uuid),
      'new_project_count', (SELECT COUNT(*) FROM business.project
        WHERE id = '$new_project_id'::uuid AND owner_user_id = '$browser_creator_id'::uuid),
      'old_project_binding_set_count', (SELECT COUNT(*) FROM business.project_skill_binding_set
        WHERE project_id = '$browser_project_id'::uuid AND owner_user_id = '$browser_creator_id'::uuid
          AND enabled_count = 1),
      'new_project_binding_set_count', (SELECT COUNT(*) FROM business.project_skill_binding_set
        WHERE project_id = '$new_project_id'::uuid AND owner_user_id = '$browser_creator_id'::uuid
          AND enabled_count = 1),
      'old_project_skill_binding_count', (SELECT COUNT(*) FROM business.project_skill_binding
        WHERE project_id = '$browser_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND status = 'enabled' AND source = 'quick_create'),
      'new_project_skill_binding_count', (SELECT COUNT(*) FROM business.project_skill_binding
        WHERE project_id = '$new_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND status = 'enabled' AND source = 'quick_create'),
      'old_project_binding_audit_count', (SELECT COUNT(*) FROM business.project_skill_binding_audit
        WHERE project_id = '$browser_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND source = 'quick_create' AND actor_user_id = '$browser_creator_id'::uuid
          AND action = 'enabled' AND to_status = 'enabled'),
      'new_project_binding_audit_count', (SELECT COUNT(*) FROM business.project_skill_binding_audit
        WHERE project_id = '$new_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND source = 'quick_create' AND actor_user_id = '$browser_creator_id'::uuid
          AND action = 'enabled' AND to_status = 'enabled'),
      'old_project_session_binding_count', (SELECT COUNT(*) FROM business.project_session_binding
        WHERE project_id = '$browser_project_id'::uuid AND agent_session_id = '$browser_session_id'::uuid
          AND provisioning_status = 'ready' AND request_schema_version = 'ensure_project_session.v2'
          AND skill_count = 1),
      'new_project_session_binding_count', (SELECT COUNT(*) FROM business.project_session_binding
        WHERE project_id = '$new_project_id'::uuid AND agent_session_id = '$new_session_id'::uuid
          AND provisioning_status = 'ready' AND request_schema_version = 'ensure_project_session.v2'
          AND skill_count = 1),
      'old_project_outbox_count', (SELECT COUNT(*) FROM business.project_session_outbox
        WHERE aggregate_id = '$browser_project_id'::uuid AND owner_user_id = '$browser_creator_id'::uuid
          AND event_type = 'agent.session.ensure' AND schema_version = 'session_bootstrap_outbox_payload.v2'
          AND status = 'delivered' AND skill_count = 1 AND payload_encryption_algorithm IS NULL
          AND payload_key_version IS NULL AND payload_nonce IS NULL AND payload_ciphertext IS NULL
          AND payload_cleared_at IS NOT NULL),
      'new_project_outbox_count', (SELECT COUNT(*) FROM business.project_session_outbox
        WHERE aggregate_id = '$new_project_id'::uuid AND owner_user_id = '$browser_creator_id'::uuid
          AND event_type = 'agent.session.ensure' AND schema_version = 'session_bootstrap_outbox_payload.v2'
          AND status = 'delivered' AND skill_count = 1 AND payload_encryption_algorithm IS NULL
          AND payload_key_version IS NULL AND payload_nonce IS NULL AND payload_ciphertext IS NULL
          AND payload_cleared_at IS NOT NULL),
      'old_resolution_header_count', (SELECT COUNT(*)
        FROM business.project_session_skill_resolution AS resolution
        JOIN business.project_session_binding AS binding
          ON binding.project_id = resolution.project_id AND binding.command_id = resolution.command_id
        WHERE resolution.project_id = '$browser_project_id'::uuid
          AND resolution.owner_user_id = '$browser_creator_id'::uuid
          AND resolution.snapshot_kind = 'published_refs' AND resolution.skill_count = 1
          AND binding.agent_session_id = '$browser_session_id'::uuid AND binding.provisioning_status = 'ready'),
      'old_resolution_item_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution_item
        WHERE project_id = '$browser_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND published_snapshot_id = '$browser_snapshot_id'::uuid AND publication_revision = 1),
      'old_resolution_other_snapshot_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution_item
        WHERE project_id = '$browser_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND published_snapshot_id <> '$browser_snapshot_id'::uuid),
      'new_resolution_header_count', (SELECT COUNT(*)
        FROM business.project_session_skill_resolution AS resolution
        JOIN business.project_session_binding AS binding
          ON binding.project_id = resolution.project_id AND binding.command_id = resolution.command_id
        WHERE resolution.project_id = '$new_project_id'::uuid
          AND resolution.owner_user_id = '$browser_creator_id'::uuid
          AND resolution.snapshot_kind = 'published_refs' AND resolution.skill_count = 1
          AND binding.agent_session_id = '$new_session_id'::uuid AND binding.provisioning_status = 'ready'),
      'new_resolution_item_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution_item
        WHERE project_id = '$new_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND published_snapshot_id = '$second_snapshot_id'::uuid AND publication_revision = 2),
      'new_resolution_other_snapshot_count', (SELECT COUNT(*) FROM business.project_session_skill_resolution_item
        WHERE project_id = '$new_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND published_snapshot_id <> '$second_snapshot_id'::uuid)
    ) || jsonb_build_object(
      'old_creation_snapshot_digest', (SELECT encode(skill_snapshot_digest, 'hex')
        FROM business.project_creation_receipt WHERE project_id = '$browser_project_id'::uuid),
      'old_binding_snapshot_digest', (SELECT encode(skill_snapshot_digest, 'hex')
        FROM business.project_session_binding WHERE project_id = '$browser_project_id'::uuid),
      'old_outbox_snapshot_digest', (SELECT encode(skill_snapshot_digest, 'hex')
        FROM business.project_session_outbox WHERE aggregate_id = '$browser_project_id'::uuid),
      'old_resolution_snapshot_digest', (SELECT encode(snapshot_set_digest, 'hex')
        FROM business.project_session_skill_resolution WHERE project_id = '$browser_project_id'::uuid),
      'new_creation_snapshot_digest', (SELECT encode(skill_snapshot_digest, 'hex')
        FROM business.project_creation_receipt WHERE project_id = '$new_project_id'::uuid),
      'new_binding_snapshot_digest', (SELECT encode(skill_snapshot_digest, 'hex')
        FROM business.project_session_binding WHERE project_id = '$new_project_id'::uuid),
      'new_outbox_snapshot_digest', (SELECT encode(skill_snapshot_digest, 'hex')
        FROM business.project_session_outbox WHERE aggregate_id = '$new_project_id'::uuid),
      'new_resolution_snapshot_digest', (SELECT encode(snapshot_set_digest, 'hex')
        FROM business.project_session_skill_resolution WHERE project_id = '$new_project_id'::uuid),
      'first_snapshot_content_digest', (SELECT encode(content_digest, 'hex') FROM business.skill_published_snapshot
        WHERE id = '$browser_snapshot_id'::uuid),
      'second_snapshot_content_digest', (SELECT encode(content_digest, 'hex') FROM business.skill_published_snapshot
        WHERE id = '$second_snapshot_id'::uuid),
      'old_resolution_content_digest', (SELECT encode(content_digest, 'hex')
        FROM business.project_session_skill_resolution_item
        WHERE project_id = '$browser_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid),
      'new_resolution_content_digest', (SELECT encode(content_digest, 'hex')
        FROM business.project_session_skill_resolution_item
        WHERE project_id = '$new_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid),
      'old_resolution_runtime_digest', (SELECT encode(runtime_content_digest, 'hex')
        FROM business.project_session_skill_resolution_item
        WHERE project_id = '$browser_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid),
      'new_resolution_runtime_digest', (SELECT encode(runtime_content_digest, 'hex')
        FROM business.project_session_skill_resolution_item
        WHERE project_id = '$new_project_id'::uuid AND skill_id = '$browser_skill_id'::uuid)
    );")" || fail "W1 Browser Business A/B 重发布事实读取失败"
  printf '%s\n' "$republish_business_fact" >"$evidence_dir/responses/w1-browser-republish-business.json"
  jq -e '
    .skill_count == 1 and .publication_revision == 2 and .current_pointer_is_b
    and .content_revision_count == 2
    and .review_count == 2 and .first_review_count == 1 and .second_review_count == 1
    and .snapshot_count == 2 and .first_snapshot_count == 1 and .second_snapshot_count == 1
    and .revision_lineage_consistent
    and .skill_create_receipt_count == 1 and .skill_submit_receipt_count == 2
    and .skill_submit_result_count == 2 and .skill_approve_receipt_count == 2
    and .skill_approve_result_count == 2 and .skill_publish_audit_count == 2
    and .skill_publish_audit_review_count == 2
    and .old_project_creation_receipt_count == 1 and .new_project_creation_receipt_count == 1
    and .old_project_binding_audit_count == 1 and .new_project_binding_audit_count == 1
    and .old_project_count == 1 and .new_project_count == 1
    and .old_project_binding_set_count == 1 and .new_project_binding_set_count == 1
    and .old_project_skill_binding_count == 1 and .new_project_skill_binding_count == 1
    and .old_project_session_binding_count == 1 and .new_project_session_binding_count == 1
    and .old_project_outbox_count == 1 and .new_project_outbox_count == 1
    and .old_resolution_header_count == 1 and .old_resolution_item_count == 1
    and .old_resolution_other_snapshot_count == 0
    and .new_resolution_header_count == 1 and .new_resolution_item_count == 1
    and .new_resolution_other_snapshot_count == 0
    and all([.first_snapshot_content_digest,.second_snapshot_content_digest,
      .old_resolution_content_digest,.new_resolution_content_digest,
      .old_resolution_runtime_digest,.new_resolution_runtime_digest,
      .old_creation_snapshot_digest,.old_binding_snapshot_digest,
      .old_outbox_snapshot_digest,.old_resolution_snapshot_digest,
      .new_creation_snapshot_digest,.new_binding_snapshot_digest,
      .new_outbox_snapshot_digest,.new_resolution_snapshot_digest][];
      test("^[0-9a-f]{64}$"))
    and .first_snapshot_content_digest == .old_resolution_content_digest
    and .second_snapshot_content_digest == .new_resolution_content_digest
    and .first_snapshot_content_digest != .second_snapshot_content_digest
    and .old_resolution_runtime_digest != .new_resolution_runtime_digest
    and .old_creation_snapshot_digest == .old_binding_snapshot_digest
    and .old_binding_snapshot_digest == .old_outbox_snapshot_digest
    and .old_outbox_snapshot_digest == .old_resolution_snapshot_digest
    and .new_creation_snapshot_digest == .new_binding_snapshot_digest
    and .new_binding_snapshot_digest == .new_outbox_snapshot_digest
    and .new_outbox_snapshot_digest == .new_resolution_snapshot_digest
    and .old_resolution_snapshot_digest != .new_resolution_snapshot_digest' \
    "$evidence_dir/responses/w1-browser-republish-business.json" >/dev/null || \
    fail "W1 Browser Business 未证明 publication_revision=2、当前 B 或 Project A/B 隔离"

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

  republish_agent_fact="$(docker exec "$postgres_container" psql -U dora_admin -d dora_agent -Atc "
    SELECT json_build_object(
      'old_session_count', (SELECT COUNT(*) FROM agent.session
        WHERE id = '$browser_session_id'::uuid AND project_id = '$browser_project_id'::uuid
          AND user_id = '$browser_creator_id'::uuid),
      'old_header_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot
        WHERE session_id = '$browser_session_id'::uuid AND snapshot_kind = 'published_refs' AND skill_count = 1),
      'old_item_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot_item
        WHERE session_id = '$browser_session_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND published_snapshot_id = '$browser_snapshot_id'::uuid AND publication_revision = 1),
      'old_other_snapshot_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot_item
        WHERE session_id = '$browser_session_id'::uuid
          AND published_snapshot_id <> '$browser_snapshot_id'::uuid),
      'old_receipt_count', (SELECT COUNT(*) FROM agent.session_command_receipt AS receipt
        JOIN agent.session_skill_snapshot AS header ON header.session_id = receipt.session_id
        WHERE receipt.session_id = '$browser_session_id'::uuid
          AND receipt.command_type = 'ensure_project_session_v2' AND receipt.skill_count = 1
          AND receipt.skill_snapshot_digest = header.snapshot_digest),
      'old_input_count', (SELECT COUNT(*) FROM agent.session_input AS input_record
        JOIN agent.session_command_receipt AS receipt
          ON receipt.session_id = input_record.session_id AND receipt.input_id = input_record.id
        WHERE input_record.session_id = '$browser_session_id'::uuid),
      'old_message_count', (SELECT COUNT(*) FROM agent.session_message
        WHERE session_id = '$browser_session_id'::uuid),
      'old_sequence_counter_count', (SELECT COUNT(*) FROM agent.session_sequence_counter
        WHERE session_id = '$browser_session_id'::uuid
          AND last_message_seq = 1 AND last_input_enqueue_seq = 1),
      'old_runtime_lease_count', (SELECT COUNT(*) FROM agent.session_runtime_lease
        WHERE session_id = '$browser_session_id'::uuid),
      'old_event_counter_count', (SELECT COUNT(*) FROM agent.session_event_counter
        WHERE session_id = '$browser_session_id'::uuid AND last_seq = 2 AND min_available_seq = 1),
      'old_event_log_count', (SELECT COUNT(*) FROM agent.session_event_log
        WHERE session_id = '$browser_session_id'::uuid),
      'old_event_log_shape_count', (SELECT COUNT(*) FROM agent.session_event_log
        WHERE session_id = '$browser_session_id'::uuid
          AND ((seq = 1 AND event_type = 'session.created')
            OR (seq = 2 AND event_type = 'session.input.accepted'))),
      'old_snapshot_digest', (SELECT snapshot_digest FROM agent.session_skill_snapshot
        WHERE session_id = '$browser_session_id'::uuid),
      'old_receipt_snapshot_digest', (SELECT skill_snapshot_digest FROM agent.session_command_receipt
        WHERE session_id = '$browser_session_id'::uuid AND command_type = 'ensure_project_session_v2'),
      'old_content_digest', (SELECT content_digest FROM agent.session_skill_snapshot_item
        WHERE session_id = '$browser_session_id'::uuid AND skill_id = '$browser_skill_id'::uuid),
      'old_runtime_digest', (SELECT runtime_content_digest FROM agent.session_skill_snapshot_item
        WHERE session_id = '$browser_session_id'::uuid AND skill_id = '$browser_skill_id'::uuid),
      'new_session_count', (SELECT COUNT(*) FROM agent.session
        WHERE id = '$new_session_id'::uuid AND project_id = '$new_project_id'::uuid
          AND user_id = '$browser_creator_id'::uuid),
      'new_header_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot
        WHERE session_id = '$new_session_id'::uuid AND snapshot_kind = 'published_refs' AND skill_count = 1),
      'new_item_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot_item
        WHERE session_id = '$new_session_id'::uuid AND skill_id = '$browser_skill_id'::uuid
          AND published_snapshot_id = '$second_snapshot_id'::uuid AND publication_revision = 2),
      'new_other_snapshot_count', (SELECT COUNT(*) FROM agent.session_skill_snapshot_item
        WHERE session_id = '$new_session_id'::uuid
          AND published_snapshot_id <> '$second_snapshot_id'::uuid),
      'new_receipt_count', (SELECT COUNT(*) FROM agent.session_command_receipt AS receipt
        JOIN agent.session_skill_snapshot AS header ON header.session_id = receipt.session_id
        WHERE receipt.session_id = '$new_session_id'::uuid
          AND receipt.command_type = 'ensure_project_session_v2' AND receipt.skill_count = 1
          AND receipt.skill_snapshot_digest = header.snapshot_digest),
      'new_input_count', (SELECT COUNT(*) FROM agent.session_input AS input_record
        JOIN agent.session_command_receipt AS receipt
          ON receipt.session_id = input_record.session_id AND receipt.input_id = input_record.id
        WHERE input_record.session_id = '$new_session_id'::uuid),
      'new_message_count', (SELECT COUNT(*) FROM agent.session_message
        WHERE session_id = '$new_session_id'::uuid),
      'new_sequence_counter_count', (SELECT COUNT(*) FROM agent.session_sequence_counter
        WHERE session_id = '$new_session_id'::uuid
          AND last_message_seq = 1 AND last_input_enqueue_seq = 1),
      'new_runtime_lease_count', (SELECT COUNT(*) FROM agent.session_runtime_lease
        WHERE session_id = '$new_session_id'::uuid),
      'new_event_counter_count', (SELECT COUNT(*) FROM agent.session_event_counter
        WHERE session_id = '$new_session_id'::uuid AND last_seq = 2 AND min_available_seq = 1),
      'new_event_log_count', (SELECT COUNT(*) FROM agent.session_event_log
        WHERE session_id = '$new_session_id'::uuid),
      'new_event_log_shape_count', (SELECT COUNT(*) FROM agent.session_event_log
        WHERE session_id = '$new_session_id'::uuid
          AND ((seq = 1 AND event_type = 'session.created')
            OR (seq = 2 AND event_type = 'session.input.accepted'))),
      'new_snapshot_digest', (SELECT snapshot_digest FROM agent.session_skill_snapshot
        WHERE session_id = '$new_session_id'::uuid),
      'new_receipt_snapshot_digest', (SELECT skill_snapshot_digest FROM agent.session_command_receipt
        WHERE session_id = '$new_session_id'::uuid AND command_type = 'ensure_project_session_v2'),
      'new_content_digest', (SELECT content_digest FROM agent.session_skill_snapshot_item
        WHERE session_id = '$new_session_id'::uuid AND skill_id = '$browser_skill_id'::uuid),
      'new_runtime_digest', (SELECT runtime_content_digest FROM agent.session_skill_snapshot_item
        WHERE session_id = '$new_session_id'::uuid AND skill_id = '$browser_skill_id'::uuid)
    );")" || fail "W1 Browser Agent A/B Session 事实读取失败"
  printf '%s\n' "$republish_agent_fact" >"$evidence_dir/responses/w1-browser-republish-agent.json"
  jq -e '
    .old_session_count == 1 and .old_header_count == 1 and .old_item_count == 1
    and .old_other_snapshot_count == 0 and .old_receipt_count == 1
    and .old_message_count == 1 and .old_input_count == 1
    and .old_sequence_counter_count == 1 and .old_runtime_lease_count == 1
    and .old_event_counter_count == 1 and .old_event_log_count == 2 and .old_event_log_shape_count == 2
    and .new_session_count == 1 and .new_header_count == 1 and .new_item_count == 1
    and .new_other_snapshot_count == 0 and .new_receipt_count == 1
    and .new_message_count == 1 and .new_input_count == 1
    and .new_sequence_counter_count == 1 and .new_runtime_lease_count == 1
    and .new_event_counter_count == 1 and .new_event_log_count == 2 and .new_event_log_shape_count == 2
    and all([.old_snapshot_digest,.old_content_digest,.old_runtime_digest,
      .old_receipt_snapshot_digest,.new_snapshot_digest,.new_content_digest,.new_runtime_digest,
      .new_receipt_snapshot_digest][];
      test("^[0-9a-f]{64}$"))
    and .old_snapshot_digest == .old_receipt_snapshot_digest
    and .new_snapshot_digest == .new_receipt_snapshot_digest
    and .old_snapshot_digest != .new_snapshot_digest
    and .old_content_digest != .new_content_digest
    and .old_runtime_digest != .new_runtime_digest' \
    "$evidence_dir/responses/w1-browser-republish-agent.json" >/dev/null || \
    fail "W1 Browser Agent 未证明旧 Session=A、新 Session=B 或 receipt/header/item/input 唯一"

  if ! (
    cd "$repo_root/agent"
    DORA_SMOKE_AGENT_SESSION_ID="$browser_session_id" GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-snapshot-verifier
  ) >"$old_verifier_file"; then
    fail "W1 Browser Agent 旧 Session Snapshot 正式 Load 路径校验失败"
  fi
  jq -e --arg session "$browser_session_id" --arg skill "$browser_skill_id" --arg snapshot "$browser_snapshot_id" '
    .status == "verified" and .session_id == $session and .skill_count == 1 and (.skills | length) == 1
    and .skills[0].skill_id == $skill and .skills[0].published_snapshot_id == $snapshot' \
    "$old_verifier_file" >/dev/null || fail "W1 Browser Agent 旧 Session Snapshot 解密验证结果漂移"
  if ! (
    cd "$repo_root/agent"
    DORA_SMOKE_AGENT_SESSION_ID="$new_session_id" GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-snapshot-verifier
  ) >"$new_verifier_file"; then
    fail "W1 Browser Agent 新 Session Snapshot 正式 Load 路径校验失败"
  fi
  jq -e --arg session "$new_session_id" --arg skill "$browser_skill_id" --arg snapshot "$second_snapshot_id" '
    .status == "verified" and .session_id == $session and .skill_count == 1 and (.skills | length) == 1
    and .skills[0].skill_id == $skill and .skills[0].published_snapshot_id == $snapshot' \
    "$new_verifier_file" >/dev/null || fail "W1 Browser Agent 新 Session Snapshot 解密验证结果漂移"

  jq -n \
    --arg schema_version "w1.skill-republish-session-isolation.smoke.evidence.v1" \
    --arg run_id "$run_id" --arg produced_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg source_digest_sha256 "$source_digest_sha256" \
    --arg business_binary_sha256 "$business_binary_sha256" --arg agent_binary_sha256 "$agent_binary_sha256" \
    --arg skill_id "$browser_skill_id" --arg first_review_id "$browser_review_id" \
    --arg second_review_id "$second_review_id" \
    --arg first_published_snapshot_id "$browser_snapshot_id" \
    --arg second_published_snapshot_id "$second_snapshot_id" \
    --arg old_project_id "$browser_project_id" --arg old_session_id "$browser_session_id" \
    --arg new_project_id "$new_project_id" --arg new_session_id "$new_session_id" \
    --slurpfile browser "$browser_result_file" \
    --slurpfile business "$evidence_dir/responses/w1-browser-republish-business.json" \
    --slurpfile agent "$evidence_dir/responses/w1-browser-republish-agent.json" \
    --slurpfile old_verifier "$old_verifier_file" --slurpfile new_verifier "$new_verifier_file" '
    {schema_version:$schema_version,status:"pending",run_id:$run_id,produced_at:$produced_at,
      source_digest_sha256:$source_digest_sha256,business_binary_sha256:$business_binary_sha256,
      agent_binary_sha256:$agent_binary_sha256,skill_id:$skill_id,
      first_review_id:$first_review_id,second_review_id:$second_review_id,
      first_published_snapshot_id:$first_published_snapshot_id,
      second_published_snapshot_id:$second_published_snapshot_id,
      old_project_id:$old_project_id,old_session_id:$old_session_id,
      new_project_id:$new_project_id,new_session_id:$new_session_id,
      facts:{
        business:{publication_revision:$business[0].publication_revision,
          content_revision_count:$business[0].content_revision_count,
          review_count:$business[0].review_count,snapshot_count:$business[0].snapshot_count,
          revision_lineage_consistent:$business[0].revision_lineage_consistent,
          skill_create_receipt_count:$business[0].skill_create_receipt_count,
          skill_submit_receipt_count:$business[0].skill_submit_receipt_count,
          skill_submit_result_count:$business[0].skill_submit_result_count,
          skill_approve_receipt_count:$business[0].skill_approve_receipt_count,
          skill_approve_result_count:$business[0].skill_approve_result_count,
          skill_publish_audit_count:$business[0].skill_publish_audit_count,
          skill_publish_audit_review_count:$business[0].skill_publish_audit_review_count,
          old_project_creation_receipt_count:$business[0].old_project_creation_receipt_count,
          new_project_creation_receipt_count:$business[0].new_project_creation_receipt_count,
          old_project_count:$business[0].old_project_count,new_project_count:$business[0].new_project_count,
          old_project_binding_set_count:$business[0].old_project_binding_set_count,
          new_project_binding_set_count:$business[0].new_project_binding_set_count,
          old_project_skill_binding_count:$business[0].old_project_skill_binding_count,
          new_project_skill_binding_count:$business[0].new_project_skill_binding_count,
          old_project_binding_audit_count:$business[0].old_project_binding_audit_count,
          new_project_binding_audit_count:$business[0].new_project_binding_audit_count,
          old_project_session_binding_count:$business[0].old_project_session_binding_count,
          new_project_session_binding_count:$business[0].new_project_session_binding_count,
          old_project_outbox_count:$business[0].old_project_outbox_count,
          new_project_outbox_count:$business[0].new_project_outbox_count,
          old_resolution_header_count:$business[0].old_resolution_header_count,
          old_resolution_item_count:$business[0].old_resolution_item_count,
          new_resolution_header_count:$business[0].new_resolution_header_count,
          new_resolution_item_count:$business[0].new_resolution_item_count},
        agent:{old_session_count:$agent[0].old_session_count,old_header_count:$agent[0].old_header_count,
          old_item_count:$agent[0].old_item_count,old_receipt_count:$agent[0].old_receipt_count,
          old_message_count:$agent[0].old_message_count,old_input_count:$agent[0].old_input_count,
          old_sequence_counter_count:$agent[0].old_sequence_counter_count,
          old_runtime_lease_count:$agent[0].old_runtime_lease_count,
          old_event_counter_count:$agent[0].old_event_counter_count,
          old_event_log_count:$agent[0].old_event_log_count,
          old_event_log_shape_count:$agent[0].old_event_log_shape_count,
          new_session_count:$agent[0].new_session_count,
          new_header_count:$agent[0].new_header_count,new_item_count:$agent[0].new_item_count,
          new_receipt_count:$agent[0].new_receipt_count,new_message_count:$agent[0].new_message_count,
          new_input_count:$agent[0].new_input_count,
          new_sequence_counter_count:$agent[0].new_sequence_counter_count,
          new_runtime_lease_count:$agent[0].new_runtime_lease_count,
          new_event_counter_count:$agent[0].new_event_counter_count,
          new_event_log_count:$agent[0].new_event_log_count,
          new_event_log_shape_count:$agent[0].new_event_log_shape_count},
        snapshot_digests:{old:$agent[0].old_snapshot_digest,new:$agent[0].new_snapshot_digest}},
      assertions:{
        browser_second_review_replay:$browser[0].second_review_replay_matches,
        browser_second_decision_replay:$browser[0].second_decision_replay_matches,
        browser_new_quickcreate_replay:$browser[0].new_quickcreate_replay_matches,
        browser_old_quickcreate_replay:$browser[0].old_quickcreate_replay_matches,
        old_command_replay_preserves_a:($browser[0].old_quickcreate_replay_matches
          and $business[0].old_project_count == 1 and $business[0].old_project_creation_receipt_count == 1
          and $business[0].old_project_binding_set_count == 1
          and $business[0].old_project_skill_binding_count == 1
          and $business[0].old_project_binding_audit_count == 1
          and $business[0].old_resolution_header_count == 1 and $business[0].old_resolution_item_count == 1
          and $business[0].old_resolution_other_snapshot_count == 0
          and $business[0].old_project_session_binding_count == 1
          and $business[0].old_project_outbox_count == 1
          and $agent[0].old_session_count == 1 and $agent[0].old_header_count == 1
          and $agent[0].old_item_count == 1 and $agent[0].old_other_snapshot_count == 0
          and $agent[0].old_receipt_count == 1
          and $agent[0].old_message_count == 1 and $agent[0].old_input_count == 1
          and $agent[0].old_sequence_counter_count == 1 and $agent[0].old_runtime_lease_count == 1
          and $agent[0].old_event_counter_count == 1 and $agent[0].old_event_log_count == 2
          and $agent[0].old_event_log_shape_count == 2
          and $old_verifier[0].skills[0].published_snapshot_id == $first_published_snapshot_id),
        browser_owner_published_projection_no_version_ui:$browser[0].owner_published_projection_no_version_ui,
        browser_old_workspace_revisited:$browser[0].old_workspace_revisited,
        business_publication_revision_two:($business[0].skill_count == 1
          and $business[0].publication_revision == 2),
        business_current_pointer_is_second:$business[0].current_pointer_is_b,
        business_content_revisions_unique:($business[0].content_revision_count == 2),
        business_revision_lineage_consistent:$business[0].revision_lineage_consistent,
        business_two_reviews_unique:($first_review_id != $second_review_id
          and $business[0].review_count == 2 and $business[0].first_review_count == 1
          and $business[0].second_review_count == 1),
        business_two_snapshots_unique:($first_published_snapshot_id != $second_published_snapshot_id
          and $business[0].snapshot_count == 2 and $business[0].first_snapshot_count == 1
          and $business[0].second_snapshot_count == 1),
        business_skill_replay_receipts_unique:($business[0].skill_create_receipt_count == 1
          and $business[0].skill_submit_receipt_count == 2 and $business[0].skill_submit_result_count == 2
          and $business[0].skill_approve_receipt_count == 2 and $business[0].skill_approve_result_count == 2),
        business_publish_audits_unique:($business[0].skill_publish_audit_count == 2
          and $business[0].skill_publish_audit_review_count == 2),
        business_creator_quickcreates_unique:($business[0].old_project_creation_receipt_count == 1
          and $business[0].new_project_creation_receipt_count == 1
          and $business[0].old_project_count == 1 and $business[0].new_project_count == 1
          and $business[0].old_project_binding_set_count == 1 and $business[0].new_project_binding_set_count == 1
          and $business[0].old_project_skill_binding_count == 1 and $business[0].new_project_skill_binding_count == 1
          and $business[0].old_project_binding_audit_count == 1
          and $business[0].new_project_binding_audit_count == 1
          and $business[0].old_resolution_header_count == 1 and $business[0].new_resolution_header_count == 1
          and $business[0].old_resolution_item_count == 1 and $business[0].new_resolution_item_count == 1
          and $business[0].old_project_session_binding_count == 1
          and $business[0].new_project_session_binding_count == 1
          and $business[0].old_project_outbox_count == 1 and $business[0].new_project_outbox_count == 1),
        business_outboxes_v2_cleared:($business[0].old_project_outbox_count == 1
          and $business[0].new_project_outbox_count == 1),
        business_old_project_resolves_first:($business[0].old_resolution_header_count == 1
          and $business[0].old_resolution_item_count == 1
          and $business[0].old_resolution_other_snapshot_count == 0),
        business_new_project_resolves_second:($business[0].new_resolution_header_count == 1
          and $business[0].new_resolution_item_count == 1
          and $business[0].new_resolution_other_snapshot_count == 0),
        agent_old_session_snapshot_first:($old_session_id != $new_session_id
          and $agent[0].old_session_count == 1 and $agent[0].old_item_count == 1
          and $agent[0].old_other_snapshot_count == 0
          and $old_verifier[0].skills[0].published_snapshot_id == $first_published_snapshot_id),
        agent_new_session_snapshot_second:($agent[0].new_session_count == 1
          and $agent[0].new_item_count == 1 and $agent[0].new_other_snapshot_count == 0
          and $new_verifier[0].skills[0].published_snapshot_id == $second_published_snapshot_id),
        agent_old_facts_unique:($agent[0].old_header_count == 1 and $agent[0].old_item_count == 1
          and $agent[0].old_receipt_count == 1 and $agent[0].old_message_count == 1
          and $agent[0].old_input_count == 1 and $agent[0].old_sequence_counter_count == 1
          and $agent[0].old_runtime_lease_count == 1 and $agent[0].old_event_counter_count == 1
          and $agent[0].old_event_log_count == 2 and $agent[0].old_event_log_shape_count == 2),
        agent_new_facts_unique:($agent[0].new_header_count == 1 and $agent[0].new_item_count == 1
          and $agent[0].new_receipt_count == 1 and $agent[0].new_message_count == 1
          and $agent[0].new_input_count == 1 and $agent[0].new_sequence_counter_count == 1
          and $agent[0].new_runtime_lease_count == 1 and $agent[0].new_event_counter_count == 1
          and $agent[0].new_event_log_count == 2 and $agent[0].new_event_log_shape_count == 2),
        agent_old_verifier_passed:($old_verifier[0].status == "verified"
          and $old_verifier[0].session_id == $old_session_id),
        agent_new_verifier_passed:($new_verifier[0].status == "verified"
          and $new_verifier[0].session_id == $new_session_id),
        old_session_cross_module_consistent:($business[0].first_snapshot_content_digest
          == $business[0].old_resolution_content_digest
          and $business[0].old_resolution_content_digest == $agent[0].old_content_digest
          and $agent[0].old_content_digest == $old_verifier[0].skills[0].content_digest
          and $business[0].old_resolution_runtime_digest == $agent[0].old_runtime_digest
          and $agent[0].old_runtime_digest == $old_verifier[0].skills[0].runtime_content_digest
          and $agent[0].old_snapshot_digest == $old_verifier[0].snapshot_digest
          and $agent[0].old_receipt_snapshot_digest == $agent[0].old_snapshot_digest
          and $old_verifier[0].skill_count == $agent[0].old_item_count),
        new_session_cross_module_consistent:($business[0].second_snapshot_content_digest
          == $business[0].new_resolution_content_digest
          and $business[0].new_resolution_content_digest == $agent[0].new_content_digest
          and $agent[0].new_content_digest == $new_verifier[0].skills[0].content_digest
          and $business[0].new_resolution_runtime_digest == $agent[0].new_runtime_digest
          and $agent[0].new_runtime_digest == $new_verifier[0].skills[0].runtime_content_digest
          and $agent[0].new_snapshot_digest == $new_verifier[0].snapshot_digest
          and $agent[0].new_receipt_snapshot_digest == $agent[0].new_snapshot_digest
          and $new_verifier[0].skill_count == $agent[0].new_item_count),
        old_snapshot_digest_chain_consistent:($business[0].old_creation_snapshot_digest
          == $business[0].old_binding_snapshot_digest
          and $business[0].old_binding_snapshot_digest == $business[0].old_outbox_snapshot_digest
          and $business[0].old_outbox_snapshot_digest == $business[0].old_resolution_snapshot_digest
          and $business[0].old_resolution_snapshot_digest == $agent[0].old_snapshot_digest
          and $agent[0].old_snapshot_digest == $agent[0].old_receipt_snapshot_digest
          and $agent[0].old_receipt_snapshot_digest == $old_verifier[0].snapshot_digest),
        new_snapshot_digest_chain_consistent:($business[0].new_creation_snapshot_digest
          == $business[0].new_binding_snapshot_digest
          and $business[0].new_binding_snapshot_digest == $business[0].new_outbox_snapshot_digest
          and $business[0].new_outbox_snapshot_digest == $business[0].new_resolution_snapshot_digest
          and $business[0].new_resolution_snapshot_digest == $agent[0].new_snapshot_digest
          and $agent[0].new_snapshot_digest == $agent[0].new_receipt_snapshot_digest
          and $agent[0].new_receipt_snapshot_digest == $new_verifier[0].snapshot_digest),
        snapshot_set_digests_distinct:($business[0].old_resolution_snapshot_digest
          != $business[0].new_resolution_snapshot_digest),
        publication_content_digests_distinct:($business[0].first_snapshot_content_digest
          != $business[0].second_snapshot_content_digest),
        resolution_runtime_digests_distinct:($business[0].old_resolution_runtime_digest
          != $business[0].new_resolution_runtime_digest),
        agent_header_snapshot_digests_distinct:($agent[0].old_snapshot_digest
          != $agent[0].new_snapshot_digest)}}' \
    >"$skill_republish_pending_evidence_file"
  jq -e '
    .schema_version == "w1.skill-republish-session-isolation.smoke.evidence.v1"
    and .status == "pending" and (.assertions | length) == 33
    and all(.assertions[]; ((. | type) == "boolean" and . == true))' \
    "$skill_republish_pending_evidence_file" >/dev/null || \
    fail "W1 Skill A/B 重发布 Session 隔离 Evidence 含未通过断言"
  w1_skill_republish_smoke_ran=true

  jq -n --slurpfile api "$evidence_dir/responses/w1-browser-frozen-api.json" \
    --slurpfile browser "$browser_result_file" \
    --slurpfile preselection "$preselection_database_file" \
    --slurpfile business "$evidence_dir/responses/w1-browser-frozen-business.json" \
    --slurpfile agent "$evidence_dir/responses/w1-browser-frozen-agent.json" \
    --slurpfile verifier "$old_verifier_file" \
    --argjson creator_admin_denial_audited "$creator_admin_denial_audited" '
    {skill_id:$api[0].skill_id,review_id:$api[0].review_id,published_snapshot_id:$api[0].published_snapshot_id,
      creator_admin_denial_audited:$creator_admin_denial_audited,
      reviewer_owner_route_not_found:$browser[0].reviewer_owner_route_not_found,
      reviewer_owner_read_not_found:$browser[0].reviewer_owner_read_not_found,
      reviewer_owner_write_not_found:$browser[0].reviewer_owner_write_not_found,
      reviewer_owner_resource_facts_not_disclosed:$browser[0].reviewer_owner_resource_facts_not_disclosed,
      browser_result_contract:($browser[0].schema_version == "w1.real-review-result.v6"
        and $browser[0].creator_admin_route_blocked == true
        and $browser[0].creator_admin_implicit_api_blocked == true
        and $browser[0].creator_admin_api_forbidden == true
        and ($browser[0].creator_admin_denial_request_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
        and $creator_admin_denial_audited
        and $browser[0].reviewer_owner_route_not_found == true
        and $browser[0].reviewer_owner_read_not_found == true
        and $browser[0].reviewer_owner_write_not_found == true
        and $browser[0].reviewer_owner_resource_facts_not_disclosed == true
        and $browser[0].market_public_list == true
        and $browser[0].market_public_detail == true
        and $browser[0].market_published_projection_safe == true
        and $browser[0].public_market_consumer_id == $browser[0].reviewer_id
        and $browser[0].public_market_selected_skill_id == $browser[0].skill_id
        and $browser[0].public_market_login_preselection_recovered == true
        and $browser[0].public_market_pre_submit_quickcreate_count == 0
        and $browser[0].public_market_submit_quickcreate_count == 1
        and $browser[0].second_review_replay_matches == true
        and $browser[0].second_decision_replay_matches == true
        and $browser[0].old_quickcreate_replay_matches == true
        and $browser[0].new_quickcreate_replay_matches == true
        and $browser[0].owner_published_projection_no_version_ui == true
        and $browser[0].old_workspace_revisited == true
        and $preselection[0].consumer_id == $browser[0].public_market_consumer_id
        and $preselection[0].skill_id == $browser[0].public_market_selected_skill_id
        and $preselection[0].database_counts_unchanged == true
        and $preselection[0].before_login == $preselection[0].before_submit
        and $api[0].owner_status == 200 and $api[0].review_status == 200
        and $api[0].second_review_status == 200),
      browser_public_market_preselection_database_zero_delta:
        ($preselection[0].schema_version == "w1.public-market-preselection.database-fact.v1"
          and $preselection[0].consumer_id == $browser[0].reviewer_id
          and $preselection[0].skill_id == $browser[0].skill_id
          and $preselection[0].database_counts_unchanged == true
          and $preselection[0].before_login == $preselection[0].before_submit),
      formal_api_frozen_revision:($api[0].owner_current_published_is_b
        and $api[0].review_frozen_submission_is_a and $api[0].first_review_observes_current_b
        and $api[0].second_review_frozen_and_current_is_b),
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
    | .browser_review_publish_quickcreate_v2 = (.browser_result_contract
      and $browser[0].creator_admin_route_blocked == true
      and $browser[0].creator_admin_implicit_api_blocked == true
      and $browser[0].creator_admin_api_forbidden == true
      and $creator_admin_denial_audited
      and $browser[0].reviewer_owner_route_not_found == true
      and $browser[0].reviewer_owner_read_not_found == true
      and $browser[0].reviewer_owner_write_not_found == true
      and $browser[0].reviewer_owner_resource_facts_not_disclosed == true
      and .browser_public_market_preselection_database_zero_delta
      and .formal_api_frozen_revision
      and .business_frozen_revision and .agent_snapshot_matches_published
      and .digest_business_agent_verifier_consistent and .browser_tool_catalog_static_unavailable)' \
    >"$evidence_dir/responses/w1-browser-frozen-consistency.json"
  jq -e '.browser_result_contract and .formal_api_frozen_revision and .business_frozen_revision
    and .agent_snapshot_matches_published and .digest_business_agent_verifier_consistent
    and .browser_public_market_preselection_database_zero_delta
    and .browser_tool_catalog_static_unavailable and .creator_admin_denial_audited
    and .reviewer_owner_route_not_found and .reviewer_owner_read_not_found
    and .reviewer_owner_write_not_found and .reviewer_owner_resource_facts_not_disclosed
    and .browser_review_publish_quickcreate_v2' \
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
if [[ -n "$w1_evidence_current_manifest" ]]; then
  mkdir -p "$w1_evidence_release_root"
  chmod 700 "$w1_evidence_release_root"
  rm -f "$w1_evidence_current_manifest" "$w1_evidence_release_root/.current-${run_id}.tmp"
  rm -rf "$w1_evidence_release_dir" "$w1_evidence_release_root/.${run_id}.staging"
  # 固定顶层文件是旧的非组原子发布接口；新消费者只能从 current manifest 发现同批五份 Evidence。
  rm -f "$repo_root/.local/smoke/w1-skill-foundation-evidence.json" \
    "$repo_root/.local/smoke/w1-skill-governance-evidence.json" \
    "$repo_root/.local/smoke/w1-skill-market-evidence.json" \
    "$repo_root/.local/smoke/w1-skill-market-binding-evidence.json" \
    "$repo_root/.local/smoke/w1-skill-republish-session-isolation-evidence.json"
fi
if [[ -n "$governance_evidence_file" ]]; then
  rm -f "$governance_evidence_file" "${governance_evidence_file}.tmp"
fi
if [[ -n "$skill_market_evidence_file" ]]; then
  rm -f "$skill_market_evidence_file" "${skill_market_evidence_file}.tmp"
fi
if [[ -n "$skill_market_binding_evidence_file" ]]; then
  rm -f "$skill_market_binding_evidence_file" "${skill_market_binding_evidence_file}.tmp"
fi
if [[ -n "$skill_republish_evidence_file" ]]; then
  rm -f "$skill_republish_evidence_file" "${skill_republish_evidence_file}.tmp"
fi
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
  w1_public_market_control_dir="$w1_temp_dir/public-market-preselection-control"
  mkdir -m 700 "$w1_public_market_control_dir"
  governance_pending_evidence_file="$evidence_dir/governance-evidence.pending.json"
  skill_market_pending_evidence_file="$evidence_dir/skill-market-evidence.pending.json"
  skill_market_binding_pending_evidence_file="$evidence_dir/skill-market-binding-evidence.pending.json"
  skill_republish_pending_evidence_file="$evidence_dir/skill-republish-session-isolation-evidence.pending.json"
  governor_cookie_jar="$w1_temp_dir/governor-cookie.jar"
  governor_curl_config="$w1_temp_dir/governor-csrf.curl"
  governor_login_response_temp="$w1_temp_dir/governor-login.json"
  : >"$governor_cookie_jar"
  : >"$governor_curl_config"
  : >"$governor_login_response_temp"
  chmod 600 "$governor_cookie_jar" "$governor_curl_config" "$governor_login_response_temp"
else
  w05_browser_result_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-browser-result.XXXXXX")"
  w05_retention_control_dir="$(mktemp -d "${TMPDIR:-/tmp}/dora-w05-retention-control.XXXXXX")"
  agent_restart_workspace_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-agent-restart-workspace.XXXXXX")"
  agent_restart_sse_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-agent-restart-sse.XXXXXX")"
  agent_restart_sse_status_temp="$(mktemp "${TMPDIR:-/tmp}/dora-w05-agent-restart-sse-status.XXXXXX")"
fi
chmod 600 "$cookie_jar" "$user_curl_config" "$owner_b_cookie_jar" "$owner_b_curl_config" \
  "$login_response_temp" "$workspace_response_temp" \
  "$owner_b_seed_response_temp" "$owner_b_login_response_temp" "$owner_b_denied_response_temp" \
  "$owner_b_denied_headers_temp" "$source_manifest_temp"
if [[ "$w1_skill_smoke_enabled" != "1" ]]; then
  chmod 700 "$w05_retention_control_dir"
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
governor_email="governor.${DORA_SMOKE_USER_EMAIL}"
governor_password="governor-${DORA_SMOKE_USER_PASSWORD}"
governor_display_name="本地冒烟 Skill 治理员"

"${compose[@]}" up -d
ENV_FILE="$env_file" "$repo_root/scripts/wait-for-local-infra.sh"
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" business up
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" agent up
(
  cd "$repo_root/business"
  GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-seeder
)
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  (
    cd "$repo_root/business"
    DORA_SMOKE_REVIEWER_EMAIL="$owner_b_email" DORA_SMOKE_REVIEWER_PASSWORD="$owner_b_password" \
      DORA_SMOKE_REVIEWER_DISPLAY_NAME="$owner_b_display_name" \
      DORA_SMOKE_GOVERNOR_EMAIL="$governor_email" DORA_SMOKE_GOVERNOR_PASSWORD="$governor_password" \
      DORA_SMOKE_GOVERNOR_DISPLAY_NAME="$governor_display_name" \
      DORA_SMOKE_PROVISIONER_EMAIL="$provisioner_email" DORA_SMOKE_PROVISIONER_PASSWORD="$provisioner_password" \
      DORA_SMOKE_PROVISIONER_DISPLAY_NAME="$provisioner_display_name" \
      GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-reviewer-seeder
  ) >"$owner_b_seed_response_temp"
  reviewer_assignment_id="$(jq -er '.assignment_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
  owner_b_seed_user_id="$(jq -er '.reviewer_user_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
  reviewer_seed_creator_user_id="$(jq -er '.creator_user_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
  governor_assignment_id="$(jq -er '.governor_assignment_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
  governor_user_id="$(jq -er '.governor_user_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
  provisioner_user_id="$(jq -er '.provisioner_user_id | strings | select(test("^[0-9a-f-]{36}$"))' "$owner_b_seed_response_temp")"
  jq -e '
    .role == "skill_reviewer" and .reason == "local_smoke_fixture"
    and .governor_role == "skill_governor" and .governor_reason == "local_smoke_governance_fixture"
    and .assignment_id != .governor_assignment_id
    and .creator_user_id != .reviewer_user_id
    and .creator_user_id != .governor_user_id
    and .creator_user_id != .provisioner_user_id
    and .reviewer_user_id != .governor_user_id
    and .reviewer_user_id != .provisioner_user_id
    and .governor_user_id != .provisioner_user_id' "$owner_b_seed_response_temp" >/dev/null || \
    fail "Reviewer/Governor Seeder 未创建四身份隔离的正式角色分配"
else
  (
    cd "$repo_root/business"
    DORA_SMOKE_USER_EMAIL="$owner_b_email" DORA_SMOKE_USER_PASSWORD="$owner_b_password" \
      DORA_SMOKE_USER_DISPLAY_NAME="$owner_b_display_name" GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-seeder
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
  run_w1_skill_governance_smoke "$postgres_container"
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
    "$w1_skill_id" "$w1_review_id" "$w1_skill_name" "$w1_updated_skill_name" \
    "$(jq -cn --arg market "$w1_market_draft_name" --arg offline "$w1_offline_draft_name" '[$market,$offline]')" || \
    fail "跨 Owner Skill 错误响应泄漏了权威资源事实"
  jq -n --argjson status "$owner_b_skill_status" --arg creator "$user_id" --arg reviewer "$owner_b_user_id" \
    --arg skill "$w1_skill_id" --arg review "$w1_review_id" --arg original_name "$w1_skill_name" \
    --arg updated_name "$w1_updated_skill_name" --arg market_name "$w1_market_draft_name" \
    --arg offline_name "$w1_offline_draft_name" --slurpfile response "$w1_temp_dir/response.json" '
    ($response[0] | [.. | strings]
      | any(.[]; contains($skill) or contains($review) or contains($original_name) or contains($updated_name)
        or contains($market_name) or contains($offline_name))) as $disclosed
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
    rm -f "$w05_retention_control_dir/request.json" "$w05_retention_control_dir/ack.json"
    run_w05_retention_window_injector >"$evidence_dir/retention-window-injector.log" 2>&1 &
    w05_retention_injector_pid="$!"
    (
      cd "$repo_root/frontend"
      DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
      DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
      DORA_E2E_OWNER_B_EMAIL="$owner_b_email" \
      DORA_E2E_OWNER_B_PASSWORD="$owner_b_password" \
      DORA_E2E_PROMPT="$w05_browser_prompt" \
      DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:18081" \
      DORA_E2E_W05_RESULT_PATH="$w05_browser_result_temp" \
      DORA_E2E_W05_RETENTION_CONTROL_DIR="$w05_retention_control_dir" \
      npm run test:e2e:w0
    ) >"$evidence_dir/frontend-playwright.log" 2>&1 || {
      sed -n '1,240p' "$evidence_dir/frontend-playwright.log" >&2
      fail "W0 浏览器页面链路失败"
    }
    if ! wait "$w05_retention_injector_pid"; then
      w05_retention_injector_pid=""
      fail "W0.5 Retention Window 故障注入器未完成原子推进"
    fi
    w05_retention_injector_pid=""
    jq -e --arg creator "$user_id" --arg owner_b "$owner_b_user_id" '
      keys == ["controlled_disconnect","creator_user_id","cross_owner_agent_blocked","cross_owner_not_found","cross_owner_user_id","project_id","resource_facts_not_disclosed","retention_no_stale_event_replayed","retention_reset_received","retention_reset_without_id","retention_same_session_recovery","retention_snapshot_reloaded","retention_snapshot_retained","same_session_recovery","schema_version","session_id"]
      and .schema_version == "w05.workspace-browser.smoke.result.v2"
      and .creator_user_id == $creator and .cross_owner_user_id == $owner_b
      and .creator_user_id != .cross_owner_user_id
      and (.project_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and (.session_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and .controlled_disconnect == true
      and .same_session_recovery == true
      and .retention_reset_received == true
      and .retention_reset_without_id == true
      and .retention_snapshot_retained == true
      and .retention_snapshot_reloaded == true
      and .retention_same_session_recovery == true
      and .retention_no_stale_event_replayed == true
      and .cross_owner_not_found == true
      and .cross_owner_agent_blocked == true
      and .resource_facts_not_disclosed == true' "$w05_browser_result_temp" >/dev/null || \
      fail "W0.5 Playwright 结构化 Retention/恢复/跨 Owner 结果不满足严格契约"

    jq -e --slurpfile browser "$w05_browser_result_temp" '
      keys == ["event_id","input_id","inserted_events","last_seq","min_available_seq","project_id","pruned_events","retained_event_seq","schema_version","session_id"]
      and .schema_version == "w05.retention-window.fixture.ack.v1"
      and .project_id == $browser[0].project_id and .session_id == $browser[0].session_id
      and (.input_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and (.event_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
      and .inserted_events == 1 and .pruned_events == 2
      and .last_seq == 3 and .min_available_seq == 3 and .retained_event_seq == 3' \
      "$w05_retention_control_dir/ack.json" >/dev/null || \
      fail "W0.5 Retention Window ACK 与浏览器结果不一致"

    retention_database_fact="$(docker exec -i -e PGOPTIONS='-c statement_timeout=10000 -c lock_timeout=5000' \
      "$postgres_container" psql -v ON_ERROR_STOP=1 -qAtF '|' \
      -U dora_admin -d dora_agent \
      -v session_id="$(jq -er '.session_id' "$w05_browser_result_temp")" <<'SQL'
        SELECT count(event_record.seq), min(event_record.seq), max(event_record.seq),
          counter.last_seq, counter.min_available_seq
        FROM agent.session_event_counter AS counter
        JOIN agent.session_event_log AS event_record ON event_record.session_id = counter.session_id
        WHERE counter.session_id = :'session_id'::uuid
        GROUP BY counter.last_seq, counter.min_available_seq;
SQL
    )"
    [[ "$retention_database_fact" == "1|3|3|3|3" ]] || \
      fail "W0.5 Retention Window 最终数据库事实漂移: $retention_database_fact"
    retention_server_reset_count="$(sed -n '/^{/p' "$evidence_dir/agent-restart.log" \
      | jq -r --arg session "$(jq -er '.session_id' "$w05_browser_result_temp")" '
      select(.msg == "Workspace EventLog 投影需要客户端 Reset"
        and .session_id == $session and .reason == "cursor_expired") | 1' \
      | wc -l | tr -d '[:space:]')"
    [[ "$retention_server_reset_count" =~ ^[0-9]+$ && "$retention_server_reset_count" -ge 1 ]] || \
      fail "Agent 未记录浏览器 Session 的 cursor_expired Reset"
    jq -n --slurpfile browser "$w05_browser_result_temp" --slurpfile ack "$w05_retention_control_dir/ack.json" \
      --argjson server_reset_count "$retention_server_reset_count" '
      {schema_version:"w05.retention-window.smoke.fact.v1",
       project_id:$browser[0].project_id,session_id:$browser[0].session_id,
       last_seq:$ack[0].last_seq,min_available_seq:$ack[0].min_available_seq,
       retained_event_seq:$ack[0].retained_event_seq,retained_event_count:1,
       inserted_events:$ack[0].inserted_events,pruned_events:$ack[0].pruned_events,
       server_cursor_expired_reset_count:$server_reset_count,
       retention_window_advanced:($ack[0].last_seq == 3 and $ack[0].min_available_seq == 3),
       retention_old_events_pruned:($ack[0].pruned_events == 2 and $ack[0].retained_event_seq == 3),
       retention_server_cursor_expired_reset:($server_reset_count >= 1)}' \
      >"$evidence_dir/responses/browser-retention-window.json"
    jq -e '.retention_window_advanced and .retention_old_events_pruned
      and .retention_server_cursor_expired_reset' \
      "$evidence_dir/responses/browser-retention-window.json" >/dev/null || \
      fail "W0.5 Retention Window 派生证据不成立"
  fi
  browser_smoke_ran=true
fi

if [[ "$w1_browser_smoke_enabled" == "1" ]]; then
  [[ -x "$repo_root/frontend/node_modules/.bin/playwright" ]] || fail "未安装前端 Playwright 依赖，请先在 frontend 执行 npm install"
  [[ -n "$DORA_SMOKE_USER_EMAIL" && -n "$DORA_SMOKE_USER_PASSWORD" && -n "$owner_b_email" && -n "$owner_b_password" ]] || \
    fail "W1 Reviewer 浏览器门禁缺少 Creator/Reviewer 凭据"
  w1_browser_result="$w1_temp_dir/browser-real-review-result.json"
  rm -f "$w1_browser_result"
  rm -f "$w1_public_market_control_dir"/*.json "$w1_public_market_control_dir"/*.tmp
  (
    cd "$repo_root/frontend"
    DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
    DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
    DORA_E2E_REVIEWER_EMAIL="$owner_b_email" \
    DORA_E2E_REVIEWER_PASSWORD="$owner_b_password" \
    DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:18081" \
    DORA_E2E_OUTPUT_DIR="../.local/playwright/w1-skill-foundation" \
    DORA_E2E_W1_RESULT_PATH="$w1_browser_result" \
    DORA_E2E_W1_PUBLIC_MARKET_CONTROL_DIR="$w1_public_market_control_dir" \
    exec npm run test:e2e:w1-real-review
  ) >"$evidence_dir/frontend-w1-playwright.log" 2>&1 &
  w1_browser_playwright_pid="$!"
  run_w1_public_market_preselection_controller "$w1_browser_playwright_pid"
  if ! wait "$w1_browser_playwright_pid"; then
    w1_browser_playwright_pid=""
    sed -n '1,240p' "$evidence_dir/frontend-w1-playwright.log" >&2
    fail "W1 Creator→Reviewer→QuickCreate v2 浏览器真实链路失败"
  fi
  w1_browser_playwright_pid=""
  run_w1_browser_frozen_smoke "$postgres_container" "$w1_browser_result"
  write_w1_skill_market_binding_evidence
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
  [[ "$w1_skill_market_binding_smoke_ran" == "true" ]] || fail "W1 Public Market Binding 门禁未完整执行"
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
      --arg schema_version "w05.workspace-transport.smoke.evidence.v3" \
      --arg run_id "$run_id" --arg produced_at "$produced_at" \
      --arg source_digest_sha256 "$source_digest_sha256" \
      --arg business_binary_sha256 "$business_binary_sha256" --arg agent_binary_sha256 "$agent_binary_sha256" \
      --arg project_id "$project_id" --arg session_id "$session_id" --arg input_id "$input_id" \
      --arg blank_project_id "$blank_project_id" --arg blank_session_id "$blank_session_id" \
      --argjson browser_ui "$browser_smoke_ran" \
      --slurpfile restart "$evidence_dir/responses/agent-restart-recovery.json" \
      --slurpfile browser "$w05_browser_result_temp" \
      --slurpfile retention "$evidence_dir/responses/browser-retention-window.json" \
      '{schema_version:$schema_version,status:"pending",run_id:$run_id,produced_at:$produced_at,source_digest_sha256:$source_digest_sha256,business_binary_sha256:$business_binary_sha256,agent_binary_sha256:$agent_binary_sha256,prompt_project:{project_id:$project_id,session_id:$session_id,input_id:$input_id},blank_project:{project_id:$blank_project_id,session_id:$blank_session_id,input_id:null},browser_workspace:{project_id:$browser[0].project_id,session_id:$browser[0].session_id},assertions:{concurrent_requests:100,idempotent_replay:true,idempotency_conflict:true,business_prompt_cleared:true,agent_unique_facts:true,blank_negative_side_effects:true,workspace_snapshot:true,workspace_empty_arrays:true,workspace_owner_safe_not_found:true,workspace_cross_owner_not_found:true,events_cross_owner_not_found:true,agent_direct_access_denied:true,sse_replay_and_ready:true,sse_cursor_reset:true,browser_ui:$browser_ui,logout_revoked:true,logout_workspace_denied:true,agent_restart_hit:$restart[0].agent_restart_hit,snapshot_after_restart:$restart[0].snapshot_after_restart,sse_after_restart:$restart[0].sse_after_restart,browser_controlled_disconnect:$browser[0].controlled_disconnect,browser_same_session_recovery:$browser[0].same_session_recovery,browser_cross_owner_not_found:$browser[0].cross_owner_not_found,browser_cross_owner_agent_blocked:$browser[0].cross_owner_agent_blocked,browser_resource_facts_not_disclosed:$browser[0].resource_facts_not_disclosed,retention_window_advanced:$retention[0].retention_window_advanced,retention_old_events_pruned:$retention[0].retention_old_events_pruned,retention_server_cursor_expired_reset:$retention[0].retention_server_cursor_expired_reset,browser_retention_reset_received:$browser[0].retention_reset_received,browser_retention_reset_without_id:$browser[0].retention_reset_without_id,browser_retention_snapshot_retained:$browser[0].retention_snapshot_retained,browser_retention_snapshot_reloaded:$browser[0].retention_snapshot_reloaded,browser_retention_same_session_recovery:$browser[0].retention_same_session_recovery,browser_retention_no_stale_event_replayed:$browser[0].retention_no_stale_event_replayed}}' \
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
    keys == ["agent_binary_sha256","assertions","business_binary_sha256","produced_at","project_skill_binding","run_id","schema_version","skill_foundation","source_digest_sha256","status","transport_prerequisite"]
    and .schema_version == "w1.skill-foundation.smoke.evidence.v3"
    and .status == "pending"
    and (.run_id | type) == "string" and (.run_id | length) > 0
    and (.produced_at | type) == "string"
    and (.source_digest_sha256 | test("^[0-9a-f]{64}$"))
    and (.business_binary_sha256 | test("^[0-9a-f]{64}$"))
    and (.agent_binary_sha256 | test("^[0-9a-f]{64}$"))
    and (.transport_prerequisite | type) == "object"
    and (.skill_foundation | type) == "object"
    and (.project_skill_binding | type) == "object"
    and (.assertions | keys) == ["agent_v2_snapshot_encrypted","browser_agent_snapshot_matches_published","browser_business_frozen_revision","browser_digest_business_agent_verifier_consistent","browser_formal_api_frozen_revision","browser_review_publish_quickcreate_v2","browser_tool_catalog_static_unavailable","browser_ui","business_v2_envelope_cleared","content_digest_business_agent_verifier_consistent","cover_asset_null_only","cross_owner_not_found","governance_audit_count","if_match_update","logout_revoked","logout_workspace_denied","missing_array_failed_closed","no_physical_foreign_keys","null_array_failed_closed","owner_list_and_detail","public_tool_refs_failed_closed","published_snapshot_count","quick_create_v2_concurrent_requests","quick_create_v2_conflict","quick_create_v2_replay","receipt_audit_request_id_consistent","review_approve_and_publish","review_approve_replay","review_count","review_frozen_definition","review_if_match","review_replay_200","review_strong_etag","review_submit_201","reviewer_rbac","reviewer_revocation","revision_count","runtime_content_digest_business_agent_verifier_consistent","skill_count_business_agent_verifier_consistent","skill_create_201","skill_create_conflict_409","skill_create_replay_200","snapshot_digest_business_agent_verifier_consistent","stale_etag_conflict","tool_catalog_cross_owner_not_found","transport_prerequisite","v1_v2_session_isolation"]
    and .assertions.revision_count == 2
    and .assertions.review_count == 1
    and .assertions.published_snapshot_count == 1
    and .assertions.governance_audit_count == 1
    and .assertions.quick_create_v2_concurrent_requests == 100
    and all([.assertions.revision_count,.assertions.review_count,.assertions.published_snapshot_count,
      .assertions.governance_audit_count,.assertions.quick_create_v2_concurrent_requests][];
      ((. | type) == "number" and . >= 0 and . == floor))
    and ([.assertions | to_entries[]
      | select(.key != "revision_count"
        and .key != "review_count"
        and .key != "published_snapshot_count"
        and .key != "governance_audit_count"
        and .key != "quick_create_v2_concurrent_requests")] as $boolean_assertions
      | ($boolean_assertions | length) == 42
      and all($boolean_assertions[]; ((.value | type) == "boolean" and .value == true)))' \
    "$pending_evidence_file" >/dev/null || fail "W1 canonical Evidence 含未通过断言，禁止发布 passed summary"
  [[ "$w1_skill_governance_smoke_ran" == "true" && -s "$governance_pending_evidence_file" ]] || \
    fail "W1 Governance Smoke 未产生独立 pending Evidence"
  jq -e '
    keys == ["agent_binary_sha256","assertions","business_binary_sha256","facts","offline_review_id","produced_at","resumed_project_id","run_id","schema_version","skill_id","source_digest_sha256","status"]
    and .schema_version == "w1.skill-governance.smoke.evidence.v1"
    and .status == "pending"
    and (.run_id | type) == "string" and (.run_id | length) > 0
    and (.produced_at | type) == "string"
    and (.source_digest_sha256 | test("^[0-9a-f]{64}$"))
    and (.business_binary_sha256 | test("^[0-9a-f]{64}$"))
    and (.agent_binary_sha256 | test("^[0-9a-f]{64}$"))
    and all([.skill_id,.resumed_project_id,.offline_review_id][];
      ((. | type) == "string" and test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")))
    and (.facts | keys) == ["existing_session_snapshot_unchanged","governance_audits","governance_epoch","governance_receipts","governance_status","linked_governance_facts","offline_resume_state_unchanged","published_count","review_count","strict_governance_linkage"]
    and (.facts.governance_status | type) == "string"
    and all([.facts.governance_epoch,.facts.governance_receipts,.facts.governance_audits,
      .facts.linked_governance_facts,.facts.published_count,.facts.review_count][];
      ((. | type) == "number" and . >= 0 and . == floor))
    and all([.facts.strict_governance_linkage,.facts.offline_resume_state_unchanged,
      .facts.existing_session_snapshot_unchanged][]; ((. | type) == "boolean" and . == true))
    and (.assertions | keys) == ["skill_governance_idempotency","skill_governance_offline_terminal","skill_governance_quickcreate_gate","skill_governor_rbac","skill_governor_revocation"]
    and all(.assertions[]; ((. | type) == "boolean" and . == true))' "$governance_pending_evidence_file" >/dev/null || \
    fail "W1 Governance Evidence 含未通过断言，禁止发布 passed summary"
  [[ "$w1_skill_market_smoke_ran" == "true" && -s "$skill_market_pending_evidence_file" ]] || \
    fail "W1 Skill Market Smoke 未产生独立 pending Evidence"
  jq -e '
    keys == ["assertions","business_binary_sha256","produced_at","run_id","schema_version","source_digest_sha256","status"]
    and .schema_version == "w1.skill-market.smoke.evidence.v2"
    and .status == "pending"
    and (.assertions | keys) == ["skill_market_cursor_fail_closed","skill_market_governance_visibility","skill_market_keyset_pagination","skill_market_public_read","skill_market_safe_projection","skill_market_stale_selection_fail_closed"]
    and all(.assertions[]; ((. | type) == "boolean" and . == true))' "$skill_market_pending_evidence_file" >/dev/null || \
    fail "W1 Skill Market Evidence 含未通过断言，禁止发布 passed summary"
  [[ "$w1_skill_market_binding_smoke_ran" == "true" && -s "$skill_market_binding_pending_evidence_file" ]] || \
    fail "W1 Public Market Binding Smoke 未产生独立 pending Evidence"
  jq -e '
    keys == ["agent_binary_sha256","assertions","business_binary_sha256","produced_at","run_id","schema_version","source_digest_sha256","status"]
    and .schema_version == "w1.skill-market-binding.smoke.evidence.v1"
    and .status == "pending"
    and (.assertions | keys) == ["public_market_governance_toctou_closed","public_market_idempotency_frozen_replay","public_market_login_preselection_recovered","public_market_mixed_binding_atomicity","public_market_permission_identity_separation","public_market_publisher_snapshot_frozen","public_market_quickcreate"]
    and all(.assertions[]; ((. | type) == "boolean" and . == true))' \
    "$skill_market_binding_pending_evidence_file" >/dev/null || \
    fail "W1 Public Market Binding Evidence 含未通过断言，禁止发布 passed summary"
  [[ "$w1_skill_republish_smoke_ran" == "true" && -s "$skill_republish_pending_evidence_file" ]] || \
    fail "W1 Skill A/B 重发布 Session 隔离 Smoke 未产生独立 pending Evidence"
  jq -e '
    keys == ["agent_binary_sha256","assertions","business_binary_sha256","facts","first_published_snapshot_id","first_review_id","new_project_id","new_session_id","old_project_id","old_session_id","produced_at","run_id","schema_version","second_published_snapshot_id","second_review_id","skill_id","source_digest_sha256","status"]
    and .schema_version == "w1.skill-republish-session-isolation.smoke.evidence.v1"
    and .status == "pending"
    and (.run_id | type) == "string" and (.run_id | length) > 0
    and (.produced_at | type) == "string"
    and (.source_digest_sha256 | test("^[0-9a-f]{64}$"))
    and (.business_binary_sha256 | test("^[0-9a-f]{64}$"))
    and (.agent_binary_sha256 | test("^[0-9a-f]{64}$"))
    and all([.skill_id,.first_review_id,.second_review_id,
      .first_published_snapshot_id,.second_published_snapshot_id,
      .old_project_id,.old_session_id,.new_project_id,.new_session_id][];
      ((. | type) == "string" and test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")))
    and .first_review_id != .second_review_id
    and .first_published_snapshot_id != .second_published_snapshot_id
    and .old_project_id != .new_project_id and .old_session_id != .new_session_id
    and (.facts | keys) == ["agent","business","snapshot_digests"]
    and (.facts.business | keys) == ["content_revision_count","new_project_binding_audit_count","new_project_binding_set_count","new_project_count","new_project_creation_receipt_count","new_project_outbox_count","new_project_session_binding_count","new_project_skill_binding_count","new_resolution_header_count","new_resolution_item_count","old_project_binding_audit_count","old_project_binding_set_count","old_project_count","old_project_creation_receipt_count","old_project_outbox_count","old_project_session_binding_count","old_project_skill_binding_count","old_resolution_header_count","old_resolution_item_count","publication_revision","review_count","revision_lineage_consistent","skill_approve_receipt_count","skill_approve_result_count","skill_create_receipt_count","skill_publish_audit_count","skill_publish_audit_review_count","skill_submit_receipt_count","skill_submit_result_count","snapshot_count"]
    and .facts.business == {publication_revision:2,content_revision_count:2,review_count:2,snapshot_count:2,
      revision_lineage_consistent:true,
      skill_create_receipt_count:1,skill_submit_receipt_count:2,skill_submit_result_count:2,
      skill_approve_receipt_count:2,skill_approve_result_count:2,
      skill_publish_audit_count:2,skill_publish_audit_review_count:2,
      old_project_creation_receipt_count:1,
      new_project_creation_receipt_count:1,old_project_count:1,new_project_count:1,
      old_project_binding_set_count:1,new_project_binding_set_count:1,
      old_project_skill_binding_count:1,new_project_skill_binding_count:1,
      old_project_binding_audit_count:1,new_project_binding_audit_count:1,
      old_project_session_binding_count:1,new_project_session_binding_count:1,
      old_project_outbox_count:1,new_project_outbox_count:1,
      old_resolution_header_count:1,old_resolution_item_count:1,
      new_resolution_header_count:1,new_resolution_item_count:1}
    and (.facts.agent | keys) == ["new_event_counter_count","new_event_log_count","new_event_log_shape_count","new_header_count","new_input_count","new_item_count","new_message_count","new_receipt_count","new_runtime_lease_count","new_sequence_counter_count","new_session_count","old_event_counter_count","old_event_log_count","old_event_log_shape_count","old_header_count","old_input_count","old_item_count","old_message_count","old_receipt_count","old_runtime_lease_count","old_sequence_counter_count","old_session_count"]
    and .facts.agent == {old_session_count:1,old_header_count:1,old_item_count:1,
      old_receipt_count:1,old_message_count:1,old_input_count:1,old_sequence_counter_count:1,
      old_runtime_lease_count:1,old_event_counter_count:1,old_event_log_count:2,old_event_log_shape_count:2,
      new_session_count:1,new_header_count:1,new_item_count:1,new_receipt_count:1,
      new_message_count:1,new_input_count:1,new_sequence_counter_count:1,new_runtime_lease_count:1,
      new_event_counter_count:1,new_event_log_count:2,new_event_log_shape_count:2}
    and (.facts.snapshot_digests | keys) == ["new","old"]
    and all(.facts.snapshot_digests[]; ((. | type) == "string" and test("^[0-9a-f]{64}$")))
    and .facts.snapshot_digests.old != .facts.snapshot_digests.new
    and (.assertions | keys) == ["agent_header_snapshot_digests_distinct","agent_new_facts_unique","agent_new_session_snapshot_second","agent_new_verifier_passed","agent_old_facts_unique","agent_old_session_snapshot_first","agent_old_verifier_passed","browser_new_quickcreate_replay","browser_old_quickcreate_replay","browser_old_workspace_revisited","browser_owner_published_projection_no_version_ui","browser_second_decision_replay","browser_second_review_replay","business_content_revisions_unique","business_creator_quickcreates_unique","business_current_pointer_is_second","business_new_project_resolves_second","business_old_project_resolves_first","business_outboxes_v2_cleared","business_publication_revision_two","business_publish_audits_unique","business_revision_lineage_consistent","business_skill_replay_receipts_unique","business_two_reviews_unique","business_two_snapshots_unique","new_session_cross_module_consistent","new_snapshot_digest_chain_consistent","old_command_replay_preserves_a","old_session_cross_module_consistent","old_snapshot_digest_chain_consistent","publication_content_digests_distinct","resolution_runtime_digests_distinct","snapshot_set_digests_distinct"]
    and all(.assertions[]; ((. | type) == "boolean" and . == true))' \
    "$skill_republish_pending_evidence_file" >/dev/null || \
    fail "W1 Skill A/B 重发布 Session 隔离 Evidence 含未通过或非闭集断言"
  jq -ne --slurpfile foundation "$pending_evidence_file" --slurpfile governance "$governance_pending_evidence_file" \
    --slurpfile market "$skill_market_pending_evidence_file" --slurpfile binding "$skill_market_binding_pending_evidence_file" \
    --slurpfile republish "$skill_republish_pending_evidence_file" '
    [$foundation[0],$governance[0],$market[0],$binding[0],$republish[0]] as $evidence
    | all($evidence[]; .run_id == $evidence[0].run_id
        and .source_digest_sha256 == $evidence[0].source_digest_sha256)' >/dev/null || \
    fail "W1 五份 Evidence 的 run_id/source digest 不一致"
elif [[ "$browser_smoke_ran" == "true" ]]; then
  jq -e '
    .schema_version == "w05.workspace-transport.smoke.evidence.v3"
    and .status == "pending"
    and (keys == ["agent_binary_sha256","assertions","blank_project","browser_workspace","business_binary_sha256","produced_at","prompt_project","run_id","schema_version","source_digest_sha256","status"])
    and (.browser_workspace.project_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
    and (.browser_workspace.session_id | test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
    and .assertions.concurrent_requests == 100
    and (.assertions | length) == 34
    and (.assertions | keys) == ["agent_direct_access_denied","agent_restart_hit","agent_unique_facts","blank_negative_side_effects","browser_controlled_disconnect","browser_cross_owner_agent_blocked","browser_cross_owner_not_found","browser_resource_facts_not_disclosed","browser_retention_no_stale_event_replayed","browser_retention_reset_received","browser_retention_reset_without_id","browser_retention_same_session_recovery","browser_retention_snapshot_reloaded","browser_retention_snapshot_retained","browser_same_session_recovery","browser_ui","business_prompt_cleared","concurrent_requests","events_cross_owner_not_found","idempotency_conflict","idempotent_replay","logout_revoked","logout_workspace_denied","retention_old_events_pruned","retention_server_cursor_expired_reset","retention_window_advanced","snapshot_after_restart","sse_after_restart","sse_cursor_reset","sse_replay_and_ready","workspace_cross_owner_not_found","workspace_empty_arrays","workspace_owner_safe_not_found","workspace_snapshot"]
    and ([.assertions | to_entries[] | select(.key != "concurrent_requests")] | length) == 33
    and all(.assertions | to_entries[] | select(.key != "concurrent_requests");
      ((.value | type) == "boolean" and .value == true))' \
    "$pending_evidence_file" >/dev/null || fail "W0.5 Recovery Evidence v3 含未通过或非布尔断言，禁止发布 passed summary"
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
  assert_evidence_excludes_literal "$governor_password" "Skill Governor 密码"
  assert_evidence_excludes_literal "$governor_csrf_token" "Skill Governor CSRF"
  assert_evidence_excludes_literal "$governor_cookie_token" "Skill Governor Cookie"
  assert_evidence_excludes_literal "$w1_skill_name" "W1 Skill 原始名称"
  assert_evidence_excludes_literal "$w1_updated_skill_name" "W1 Skill 更新后原始名称"
  assert_evidence_excludes_literal "$w1_market_draft_name" "W1 Skill Market 隔离草稿名称"
  assert_evidence_excludes_literal "$w1_offline_draft_name" "W1 Skill Offline 草稿名称"
  assert_evidence_excludes_literal "$w1_owner_private_skill_name" "W1 Mixed Owner-private Skill 原始名称"
  assert_evidence_excludes_literal "$w1_binding_prompt" "W1 Binding 完整 Prompt"
  assert_evidence_excludes_regex '"definition"[[:space:]]*:' "W1 Skill 完整定义正文"
  assert_evidence_excludes_regex "$W1_EVIDENCE_ENCRYPTED_FIELD_REGEX" "W1 密文、Nonce 或 Key Version 字段"
  assert_evidence_excludes_regex '"governance_etag"[[:space:]]*:|"sg1-' "Skill Governance ETag"
  assert_evidence_excludes_regex 'incident_containment|incident_resolved|repeated_violation|risk_cleared|SMOKE-(SUSPEND|RESUME|OFFLINE)' "Skill Governance 原因或审批引用"
  assert_evidence_excludes_regex 'governance-(suspend|resume|offline|suspended-project|resumed-project|offline-project)' "Skill Governance 原始幂等键"
  assert_evidence_excludes_regex '(public-market-(binding|stale)|public-market-mixed-(success|suspended)|mixed-owner-private-(create|review|approve))-[0-9]' "Public Market Binding 原始幂等键"
  assert_evidence_excludes_regex "$W1_EVIDENCE_IDEMPOTENCY_KEY_REGEX" "W1 浏览器原始幂等键"
  assert_evidence_excludes_regex 'W1-REVIEW-SENTINEL-[AB]-[0-9]+|W1 Reviewer QuickCreate( Published B)? [0-9]+' "W1 Skill 重发布正文或完整 Prompt"
  assert_evidence_excludes_regex 'W1 Public Market QuickCreate [0-9]+' "W1 Public Market 浏览器完整 Prompt"
  assert_evidence_excludes_regex '"schema_version":"project_skill_permission_snapshot\.v2"' "Public Market Permission Canonical"
  if [[ -n "${BUSINESS_PROJECT_PROMPT_KEY_BASE64:-}" ]]; then
    assert_evidence_excludes_literal "$BUSINESS_PROJECT_PROMPT_KEY_BASE64" "Business Prompt 密钥材料"
  fi
  if [[ -n "${AGENT_CONTENT_KEY_BASE64:-}" ]]; then
    assert_evidence_excludes_literal "$AGENT_CONTENT_KEY_BASE64" "Agent Content 密钥材料"
  fi
fi

# 只有脱敏扫描、Runtime 退出和 etcd 租约摘除全部成功后，才发布 passed summary。
if [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  # 五份文件先进入不可见的同 run staging 目录，current.json 的最后一次 rename 是唯一提交点。
  publish_w1_evidence_release "$w1_evidence_release_root" "$run_id" "$source_digest_sha256" \
    "$pending_evidence_file" "$governance_pending_evidence_file" "$skill_market_pending_evidence_file" \
    "$skill_market_binding_pending_evidence_file" "$skill_republish_pending_evidence_file" || \
    fail "W1 五份 Evidence release 组原子发布失败"
else
  jq '.status = "passed"' "$pending_evidence_file" >"${evidence_file}.tmp"
  rm -f "$pending_evidence_file"
  # canonical W0.5 summary 是最后一次可失败写操作；旧 W0 summary 已在运行开始撤销，避免双真源假绿。
  mv "${evidence_file}.tmp" "$evidence_file"
fi
trap - EXIT

if [[ "$w1_skill_smoke_enabled" == "1" && "$w1_browser_smoke_ran" == "true" ]]; then
  echo "W1 Skill 发布、治理、Project Binding、Session Snapshot v2、跨 Owner 与浏览器冒烟通过"
elif [[ "$w1_skill_smoke_enabled" == "1" ]]; then
  echo "W1 Skill 发布、治理、Project Binding、Session Snapshot v2、数据库一致性与跨 Owner 冒烟通过"
elif [[ "$browser_smoke_ran" == "true" ]]; then
  echo "W0.5 Transport API/Snapshot/SSE 与真实浏览器登录、Quick Create、正式工作台、退出冒烟通过"
else
  echo "W0.5 Transport 真实登录、100 并发 Quick Create、Business generated Kitex→Agent、Workspace Snapshot/SSE、空 Prompt 与退出冒烟通过"
fi
