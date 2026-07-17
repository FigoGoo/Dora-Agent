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
compose=(docker compose --env-file "$env_file" -f "$repo_root/deploy/local/compose.yaml")
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
evidence_file="${PLAN_SPEC_PREVIEW_EVIDENCE_FILE:-$repo_root/.local/smoke/plan-spec-preview-trial-evidence.json}"
evidence_pending="${evidence_file}.pending"
work_dir=""
control_dir=""
browser_result=""
recovery_result=""
business_pid=""
agent_pid=""
playwright_pid=""
postgres_container=""
redis_container=""
etcd_container=""
runtime_shutdown_observed=false
etcd_registration_observed=false
etcd_lease_removed=false
agent_restart_observed=false

fail() {
  printf 'plan-spec-preview smoke failed: %s\n' "$1" >&2
  exit 1
}

file_mode() {
  stat -f '%Lp' "$1" 2>/dev/null || stat -c '%a' "$1"
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

cleanup_on_exit() {
  local exit_code="$?"
  trap - EXIT
  stop_pid_best_effort "$playwright_pid"
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

wait_ready() {
  local port="$1"
  local process_id="$2"
  local label="$3"
  for _ in $(seq 1 160); do
    local state=""
    state="$(ps -o stat= -p "$process_id" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$process_id" 2>/dev/null || [[ "$state" == Z* ]]; then
      fail "$label Runtime exited before readiness"
    fi
    if curl --fail --silent --max-time 1 "http://127.0.0.1:${port}/readyz" >/dev/null; then
      return 0
    fi
    sleep 0.25
  done
  fail "$label Runtime readiness timed out"
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
  if [[ -z "$candidate" || "$candidate" == 127.* || "$candidate" == "0.0.0.0" ]]; then
    return 1
  fi
  printf '%s' "$candidate"
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
      find frontend/src frontend/e2e frontend/scripts -type f -print
      find scripts -type f -print
      find frontend -maxdepth 1 -type f \( -name 'package.json' -o -name 'package-lock.json' -o -name 'npm-shrinkwrap.json' \) -print
      printf '%s\n' Makefile frontend/playwright.config.js frontend/vite.config.js
    } | LC_ALL=C sort -u
  )
  [[ -s "$output_file" ]]
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
  for _ in $(seq 1 3000); do
    if [[ -s "$path" ]] && jq -e "$jq_filter" "$path" >/dev/null 2>&1; then
      [[ "$(file_mode "$path")" == "600" ]] || fail "$label mode is not 0600"
      return 0
    fi
    local state=""
    state="$(ps -o stat= -p "$playwright_pid" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$playwright_pid" 2>/dev/null || [[ "$state" == Z* ]]; then
      sed -n '1,260p' "$work_dir/playwright.log" >&2 || true
      fail "Playwright exited before $label"
    fi
    sleep 0.05
  done
  fail "timed out waiting for $label"
}

collect_positive_authority() {
  local project_id="$1"
  local session_id="$2"
  local input_id="$3"
  local creation_spec_id="$4"
  local output_file="$5"
  local business_file="$work_dir/positive-business.$$.json"
  local agent_file="$work_dir/positive-agent.$$.json"

  docker exec -i "$postgres_container" psql -v ON_ERROR_STOP=1 -qAt \
    -U dora_admin -d dora_business \
    -v project_id="$project_id" -v creation_spec_id="$creation_spec_id" <<'SQL' >"$business_file"
WITH drafts AS MATERIALIZED (
  SELECT * FROM business.creation_spec
  WHERE project_id = :'project_id'::uuid
), receipts AS MATERIALIZED (
  SELECT * FROM business.creation_spec_command_receipt
  WHERE project_id = :'project_id'::uuid
)
SELECT json_build_object(
  'draft_count', (SELECT count(*) FROM drafts),
  'receipt_count', (SELECT count(*) FROM receipts),
  'draft_id', (SELECT id::text FROM drafts ORDER BY id LIMIT 1),
  'status', (SELECT status FROM drafts ORDER BY id LIMIT 1),
  'version', (SELECT version FROM drafts ORDER BY id LIMIT 1),
  'schema_version', (SELECT schema_version FROM drafts ORDER BY id LIMIT 1),
  'content_digest', (SELECT encode(content_digest, 'hex') FROM drafts ORDER BY id LIMIT 1),
  'source_tool_call_id', (SELECT source_tool_call_id::text FROM drafts ORDER BY id LIMIT 1),
  'source_prompt_version', (SELECT source_prompt_version FROM drafts ORDER BY id LIMIT 1),
  'source_validator_version', (SELECT source_validator_version FROM drafts ORDER BY id LIMIT 1),
  'content_exact_fields', COALESCE((
    SELECT ARRAY(SELECT jsonb_object_keys(content_json) ORDER BY 1) =
      ARRAY['acceptance_criteria','audience','constraints','deliverable_type','goal','locale','phases','title']::text[]
    FROM drafts LIMIT 1
  ), false),
  'content_shape_valid', COALESCE((
    SELECT content_json->>'deliverable_type' = 'video'
      AND content_json->>'locale' = 'zh-CN'
      AND jsonb_typeof(content_json->'phases') = 'array'
      AND jsonb_array_length(content_json->'phases') BETWEEN 1 AND 6
      AND jsonb_typeof(content_json->'constraints') = 'array'
      AND jsonb_typeof(content_json->'acceptance_criteria') = 'array'
      AND jsonb_array_length(content_json->'acceptance_criteria') BETWEEN 1 AND 8
    FROM drafts LIMIT 1
  ), false),
  'draft_receipt_consistent', EXISTS (
    SELECT 1
    FROM drafts AS draft
    JOIN receipts AS receipt
      ON receipt.creation_spec_id = draft.id
     AND receipt.source_tool_call_id = draft.source_tool_call_id
     AND receipt.result_version = draft.version
     AND receipt.result_status = draft.status
     AND receipt.result_content_digest = draft.content_digest
     AND receipt.source_prompt_version = draft.source_prompt_version
     AND receipt.source_validator_version = draft.source_validator_version
    WHERE draft.id = :'creation_spec_id'::uuid
  )
);
SQL

  docker exec -i "$postgres_container" psql -v ON_ERROR_STOP=1 -qAt \
    -U dora_admin -d dora_agent \
    -v project_id="$project_id" -v session_id="$session_id" -v input_id="$input_id" \
    -v creation_spec_id="$creation_spec_id" <<'SQL' >"$agent_file"
WITH selected_run AS MATERIALIZED (
  SELECT * FROM agent.creation_spec_preview_run
  WHERE input_id = :'input_id'::uuid
    AND session_id = :'session_id'::uuid
    AND project_id = :'project_id'::uuid
), selected_input AS MATERIALIZED (
  SELECT * FROM agent.session_input
  WHERE id = :'input_id'::uuid AND session_id = :'session_id'::uuid
), selected_projection AS MATERIALIZED (
  SELECT * FROM agent.creation_spec_preview_projection
  WHERE session_id = :'session_id'::uuid
)
SELECT json_build_object(
  'session_count', (SELECT count(*) FROM agent.session WHERE id = :'session_id'::uuid AND project_id = :'project_id'::uuid),
  'message_count', (SELECT count(*) FROM agent.session_message WHERE session_id = :'session_id'::uuid AND source_kind = 'creation_spec_preview'),
  'input_count', (SELECT count(*) FROM selected_input),
  'input_source_type', (SELECT source_type FROM selected_input LIMIT 1),
  'input_status', (SELECT status FROM selected_input LIMIT 1),
  'input_attempts', (SELECT attempts FROM selected_input LIMIT 1),
  'input_fence_token', (SELECT fence_token FROM selected_input LIMIT 1),
  'input_lease_released', COALESCE((SELECT lease_owner IS NULL AND lease_until IS NULL FROM selected_input LIMIT 1), false),
  'run_count', (SELECT count(*) FROM selected_run),
  'run_id', (SELECT run_id::text FROM selected_run LIMIT 1),
  'tool_call_id', (SELECT tool_call_id::text FROM selected_run LIMIT 1),
  'business_command_id', (SELECT business_command_id::text FROM selected_run LIMIT 1),
  'run_message_input_consistent', EXISTS (
    SELECT 1 FROM selected_run AS run
    JOIN selected_input AS input_record ON input_record.id = run.input_id AND input_record.message_id = run.message_id
    JOIN agent.session_message AS message_record ON message_record.id = run.message_id AND message_record.session_id = run.session_id
  ),
  'model_receipt_count', (
    SELECT count(*) FROM agent.creation_spec_preview_model_receipt AS model_receipt
    JOIN selected_run AS run ON run.tool_call_id = model_receipt.tool_call_id
  ),
  'model_call_indexes', COALESCE((
    SELECT json_agg(model_receipt.call_index ORDER BY model_receipt.call_index)
    FROM agent.creation_spec_preview_model_receipt AS model_receipt
    JOIN selected_run AS run ON run.tool_call_id = model_receipt.tool_call_id
  ), '[]'::json),
  'model_receipts_completed_encrypted', COALESCE((
    SELECT count(*) = 3 AND bool_and(
      model_receipt.status = 'completed'
      AND model_receipt.response_ciphertext IS NOT NULL
      AND model_receipt.response_key_version IS NOT NULL
      AND model_receipt.response_digest ~ '^[0-9a-f]{64}$'
      AND model_receipt.completed_at IS NOT NULL
    )
    FROM agent.creation_spec_preview_model_receipt AS model_receipt
    JOIN selected_run AS run ON run.tool_call_id = model_receipt.tool_call_id
  ), false),
  'tool_receipt_count', (
    SELECT count(*) FROM agent.creation_spec_preview_tool_receipt AS tool_receipt
    JOIN selected_run AS run ON run.tool_call_id = tool_receipt.tool_call_id
  ),
  'tool_receipt_completed_encrypted', EXISTS (
    SELECT 1 FROM agent.creation_spec_preview_tool_receipt AS tool_receipt
    JOIN selected_run AS run
      ON run.tool_call_id = tool_receipt.tool_call_id
     AND run.business_command_id = tool_receipt.business_command_id
    WHERE tool_receipt.stage = 'completed'
      AND tool_receipt.result_ciphertext IS NOT NULL
      AND tool_receipt.result_key_version IS NOT NULL
      AND tool_receipt.result_digest ~ '^[0-9a-f]{64}$'
      AND tool_receipt.error_code IS NULL
  ),
  'tool_receipt_durable_command_safe', EXISTS (
    SELECT 1 FROM agent.creation_spec_preview_tool_receipt AS tool_receipt
    JOIN selected_run AS run
      ON run.tool_call_id = tool_receipt.tool_call_id
     AND run.business_command_id = tool_receipt.business_command_id
    WHERE tool_receipt.stage = 'completed'
      AND tool_receipt.business_command_ciphertext IS NOT NULL
      AND tool_receipt.business_command_key_version IS NOT NULL
      AND tool_receipt.business_command_payload_digest ~ '^[0-9a-f]{64}$'
      AND tool_receipt.business_resend_attempts = 0
      AND tool_receipt.business_resend_limit = 3
      AND tool_receipt.business_last_resend_at IS NULL
      AND tool_receipt.business_resend_exhausted_at IS NULL
  ),
  'projection_count', (SELECT count(*) FROM selected_projection),
  'projection_resource_id', (SELECT resource_id::text FROM selected_projection LIMIT 1),
  'projection_content_digest', (SELECT content_digest FROM selected_projection LIMIT 1),
  'projection_safe', COALESCE((
    SELECT resource_id = :'creation_spec_id'::uuid
      AND source_input_id = :'input_id'::uuid
      AND project_id = :'project_id'::uuid
      AND schema_version = 'creation_spec.preview.card.v1'
      AND resource_version = 1
      AND status = 'draft'
      AND deliverable_type = 'video'
      AND locale = 'zh-CN'
      AND jsonb_typeof(phases) = 'array'
      AND jsonb_typeof(constraints) = 'array'
      AND jsonb_typeof(acceptance_criteria) = 'array'
    FROM selected_projection LIMIT 1
  ), false),
  'event_count', (SELECT count(*) FROM agent.session_event_log WHERE session_id = :'session_id'::uuid),
  'event_types', COALESCE((
    SELECT json_agg(event_type ORDER BY seq) FROM agent.session_event_log WHERE session_id = :'session_id'::uuid
  ), '[]'::json),
  'accepted_event_count', (
    SELECT count(*) FROM agent.session_event_log
    WHERE session_id = :'session_id'::uuid
      AND event_type = 'session.input.accepted'
      AND aggregate_id = :'input_id'::uuid
  ),
  'completed_event_count', (
    SELECT count(*) FROM agent.session_event_log
    WHERE session_id = :'session_id'::uuid
      AND event_type = 'creation_spec.preview.completed'
      AND aggregate_id = :'creation_spec_id'::uuid
  ),
  'failed_event_count', (
    SELECT count(*) FROM agent.session_event_log
    WHERE session_id = :'session_id'::uuid AND event_type = 'creation_spec.preview.failed'
  ),
  'event_last_seq', (SELECT last_seq FROM agent.session_event_counter WHERE session_id = :'session_id'::uuid),
  'event_min_available_seq', (SELECT min_available_seq FROM agent.session_event_counter WHERE session_id = :'session_id'::uuid),
  'last_message_seq', (SELECT last_message_seq FROM agent.session_sequence_counter WHERE session_id = :'session_id'::uuid),
  'last_input_enqueue_seq', (SELECT last_input_enqueue_seq FROM agent.session_sequence_counter WHERE session_id = :'session_id'::uuid),
  'session_lease_released', COALESCE((
    SELECT lease_owner IS NULL AND lease_until IS NULL FROM agent.session_runtime_lease WHERE session_id = :'session_id'::uuid
  ), false),
  'session_fence_token', (SELECT fence_token FROM agent.session_runtime_lease WHERE session_id = :'session_id'::uuid)
);
SQL

  jq -e 'type == "object"' "$business_file" >/dev/null || return 1
  jq -e 'type == "object"' "$agent_file" >/dev/null || return 1
  jq -S -n --slurpfile business "$business_file" --slurpfile agent "$agent_file" \
    '{business:$business[0],agent:$agent[0]}' >"$output_file"
  chmod 600 "$output_file"
  rm -f "$business_file" "$agent_file"
}

collect_blocked_authority() {
  local project_id="$1"
  local session_id="$2"
  local input_id="$3"
  local output_file="$4"
  local business_file="$work_dir/blocked-business.$$.json"
  local agent_file="$work_dir/blocked-agent.$$.json"

  docker exec -i "$postgres_container" psql -v ON_ERROR_STOP=1 -qAt \
    -U dora_admin -d dora_business \
    -v project_id="$project_id" <<'SQL' >"$business_file"
SELECT json_build_object(
  'project_count', (SELECT count(*) FROM business.project WHERE id = :'project_id'::uuid),
  'creation_receipt_count', (SELECT count(*) FROM business.project_creation_receipt WHERE project_id = :'project_id'::uuid),
  'binding_count', (SELECT count(*) FROM business.project_session_binding WHERE project_id = :'project_id'::uuid),
  'delivered_outbox_count', (SELECT count(*) FROM business.project_session_outbox WHERE aggregate_id = :'project_id'::uuid AND status = 'delivered'),
  'creation_spec_count', (SELECT count(*) FROM business.creation_spec WHERE project_id = :'project_id'::uuid),
  'creation_spec_receipt_count', (SELECT count(*) FROM business.creation_spec_command_receipt WHERE project_id = :'project_id'::uuid)
);
SQL

  docker exec -i "$postgres_container" psql -v ON_ERROR_STOP=1 -qAt \
    -U dora_admin -d dora_agent \
    -v project_id="$project_id" -v session_id="$session_id" -v input_id="$input_id" <<'SQL' >"$agent_file"
SELECT json_build_object(
  'session_count', (SELECT count(*) FROM agent.session WHERE id = :'session_id'::uuid AND project_id = :'project_id'::uuid),
  'message_count', (SELECT count(*) FROM agent.session_message WHERE session_id = :'session_id'::uuid),
  'input_count', (SELECT count(*) FROM agent.session_input WHERE session_id = :'session_id'::uuid),
  'legacy_input_count', (
    SELECT count(*) FROM agent.session_input
    WHERE id = :'input_id'::uuid AND session_id = :'session_id'::uuid
      AND source_type = 'user_message' AND status = 'pending'
  ),
  'session_command_receipt_count', (SELECT count(*) FROM agent.session_command_receipt WHERE session_id = :'session_id'::uuid),
  'preview_run_count', (SELECT count(*) FROM agent.creation_spec_preview_run WHERE session_id = :'session_id'::uuid),
  'preview_model_receipt_count', (
    SELECT count(*) FROM agent.creation_spec_preview_model_receipt AS model_receipt
    JOIN agent.creation_spec_preview_run AS run ON run.tool_call_id = model_receipt.tool_call_id
    WHERE run.session_id = :'session_id'::uuid
  ),
  'preview_tool_receipt_count', (
    SELECT count(*) FROM agent.creation_spec_preview_tool_receipt AS tool_receipt
    JOIN agent.creation_spec_preview_run AS run ON run.tool_call_id = tool_receipt.tool_call_id
    WHERE run.session_id = :'session_id'::uuid
  ),
  'preview_projection_count', (SELECT count(*) FROM agent.creation_spec_preview_projection WHERE session_id = :'session_id'::uuid),
  'event_count', (SELECT count(*) FROM agent.session_event_log WHERE session_id = :'session_id'::uuid),
  'event_types', COALESCE((
    SELECT json_agg(event_type ORDER BY seq) FROM agent.session_event_log WHERE session_id = :'session_id'::uuid
  ), '[]'::json),
  'accepted_event_count', (
    SELECT count(*) FROM agent.session_event_log
    WHERE session_id = :'session_id'::uuid
      AND event_type = 'session.input.accepted'
      AND aggregate_id = :'input_id'::uuid
  ),
  'event_last_seq', (SELECT last_seq FROM agent.session_event_counter WHERE session_id = :'session_id'::uuid),
  'event_min_available_seq', (SELECT min_available_seq FROM agent.session_event_counter WHERE session_id = :'session_id'::uuid),
  'last_message_seq', (SELECT last_message_seq FROM agent.session_sequence_counter WHERE session_id = :'session_id'::uuid),
  'last_input_enqueue_seq', (SELECT last_input_enqueue_seq FROM agent.session_sequence_counter WHERE session_id = :'session_id'::uuid),
  'input_attempts', (SELECT attempts FROM agent.session_input WHERE id = :'input_id'::uuid),
  'input_fence_token', (SELECT fence_token FROM agent.session_input WHERE id = :'input_id'::uuid),
  'input_lease_released', COALESCE((
    SELECT lease_owner IS NULL AND lease_until IS NULL FROM agent.session_input WHERE id = :'input_id'::uuid
  ), false),
  'session_fence_token', (SELECT fence_token FROM agent.session_runtime_lease WHERE session_id = :'session_id'::uuid),
  'session_lease_released', COALESCE((
    SELECT lease_owner IS NULL AND lease_until IS NULL FROM agent.session_runtime_lease WHERE session_id = :'session_id'::uuid
  ), false)
);
SQL

  jq -e 'type == "object"' "$business_file" >/dev/null || return 1
  jq -e 'type == "object"' "$agent_file" >/dev/null || return 1
  jq -S -n --slurpfile business "$business_file" --slurpfile agent "$agent_file" \
    '{business:$business[0],agent:$agent[0]}' >"$output_file"
  chmod 600 "$output_file"
  rm -f "$business_file" "$agent_file"
}

collect_forbidden_side_effects() {
  local output_file="$1"
  local business_file="$work_dir/side-effects-business.$$.json"
  local agent_file="$work_dir/side-effects-agent.$$.json"
  local worker_file="$work_dir/side-effects-worker.$$.json"

  docker exec -i "$postgres_container" psql -v ON_ERROR_STOP=1 -qAt -U dora_admin -d dora_business <<'SQL' >"$business_file"
SELECT json_build_object(
  'review_submission_count', (SELECT count(*) FROM business.skill_review_submission),
  'governance_audit_count', (SELECT count(*) FROM business.skill_governance_audit),
  'role_assignment_count', (SELECT count(*) FROM business.user_role_assignment),
  'billing_approval_job_table_count', (
    SELECT count(*) FROM pg_catalog.pg_tables
    WHERE schemaname = 'business'
      AND tablename ~ '(billing|charge|payment|ledger|approval|operation|batch|job)'
  )
);
SQL
  docker exec -i "$postgres_container" psql -v ON_ERROR_STOP=1 -qAt -U dora_admin -d dora_agent <<'SQL' >"$agent_file"
SELECT json_build_object(
  'billing_approval_job_table_count', (
    SELECT count(*) FROM pg_catalog.pg_tables
    WHERE schemaname = 'agent'
      AND tablename ~ '(billing|charge|payment|ledger|approval|operation|batch|job)'
  )
);
SQL
  docker exec -i "$postgres_container" psql -v ON_ERROR_STOP=1 -qAt -U dora_admin -d dora_worker <<'SQL' >"$worker_file"
SELECT json_build_object(
  'runtime_table_count', (SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname = 'worker'),
  'billing_approval_job_table_count', (
    SELECT count(*) FROM pg_catalog.pg_tables
    WHERE schemaname = 'worker'
      AND tablename ~ '(billing|charge|payment|ledger|approval|operation|batch|job)'
  )
);
SQL
  jq -S -n --slurpfile business "$business_file" --slurpfile agent "$agent_file" --slurpfile worker "$worker_file" \
    '{business:$business[0],agent:$agent[0],worker:$worker[0]}' >"$output_file"
  chmod 600 "$output_file"
  rm -f "$business_file" "$agent_file" "$worker_file"
}

stop_runtimes_strict() {
  local pid=""
  local label=""
  for pid in "$agent_pid" "$business_pid"; do
    [[ -n "$pid" ]] || fail "Runtime PID missing before strict shutdown"
    kill -0 "$pid" 2>/dev/null || fail "Runtime exited before strict shutdown"
    kill -TERM "$pid" || fail "could not signal Runtime $pid"
  done
  for label in agent business; do
    if [[ "$label" == "agent" ]]; then
      pid="$agent_pid"
    else
      pid="$business_pid"
    fi
    local stopped=false
    for _ in $(seq 1 160); do
      local state=""
      state="$(ps -o stat= -p "$pid" 2>/dev/null | tr -d '[:space:]' || true)"
      if ! kill -0 "$pid" 2>/dev/null || [[ "$state" == Z* ]]; then
        stopped=true
        break
      fi
      sleep 0.25
    done
    [[ "$stopped" == "true" ]] || fail "$label Runtime did not stop within deadline"
    if ! wait "$pid"; then
      fail "$label Runtime returned a failure during graceful shutdown"
    fi
    if [[ "$label" == "agent" ]]; then
      agent_pid=""
    else
      business_pid=""
    fi
  done
  runtime_shutdown_observed=true
}

restart_agent_strict() {
  local old_pid="$agent_pid"
  local stopped=false
  local state=""
  local agent_key="/dora/services/dora.agent.session.v1/${AGENT_INSTANCE_ID}"
  local agent_value=""
  [[ -n "$old_pid" ]] || fail "Agent PID missing before restart"
  kill -0 "$old_pid" 2>/dev/null || fail "Agent exited before restart checkpoint"
  kill -TERM "$old_pid" || fail "could not signal Agent for restart checkpoint"
  for _ in $(seq 1 160); do
    state="$(ps -o stat= -p "$old_pid" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$old_pid" 2>/dev/null || [[ "$state" == Z* ]]; then
      stopped=true
      break
    fi
    sleep 0.25
  done
  [[ "$stopped" == "true" ]] || fail "Agent did not stop within restart checkpoint deadline"
  if ! wait "$old_pid"; then
    fail "Agent returned a failure at restart checkpoint"
  fi
  agent_pid=""
  for _ in $(seq 1 120); do
    agent_value="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 \
      get "$agent_key" --print-value-only)"
    [[ -z "$agent_value" ]] && break
    sleep 0.25
  done
  [[ -z "$agent_value" ]] || fail "Agent etcd lease remained after checkpoint shutdown"

  "$repo_root/.local/bin/agent-service" >"$work_dir/agent-restart.log" 2>&1 &
  agent_pid="$!"
  wait_ready 18082 "$agent_pid" "Agent restart"
  agent_value="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 \
    get "$agent_key" --print-value-only)"
  jq -e --arg instance "$AGENT_INSTANCE_ID" '
    .service == "dora.agent.session.v1" and .instance_id == $instance and (.address | length) > 0 and (.version | length) > 0
  ' <<<"$agent_value" >/dev/null || fail "restarted Agent did not restore its etcd registration"
  agent_restart_observed=true
}

stop_agent_for_recovery_probe_strict() {
  local old_pid="$agent_pid"
  local stopped=false
  local state=""
  local agent_key="/dora/services/dora.agent.session.v1/${AGENT_INSTANCE_ID}"
  local agent_value=""
  [[ -n "$old_pid" ]] || fail "Agent PID missing before recovery probe"
  kill -0 "$old_pid" 2>/dev/null || fail "Agent exited before recovery probe"
  kill -TERM "$old_pid" || fail "could not signal Agent before recovery probe"
  for _ in $(seq 1 160); do
    state="$(ps -o stat= -p "$old_pid" 2>/dev/null | tr -d '[:space:]' || true)"
    if ! kill -0 "$old_pid" 2>/dev/null || [[ "$state" == Z* ]]; then
      stopped=true
      break
    fi
    sleep 0.25
  done
  [[ "$stopped" == "true" ]] || fail "Agent did not stop before recovery probe"
  if ! wait "$old_pid"; then
    fail "Agent returned a failure before recovery probe"
  fi
  agent_pid=""
  for _ in $(seq 1 120); do
    agent_value="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 \
      get "$agent_key" --print-value-only)"
    [[ -z "$agent_value" ]] && return 0
    sleep 0.25
  done
  fail "Agent etcd lease remained before recovery probe"
}

start_agent_after_recovery_probe_strict() {
  local agent_key="/dora/services/dora.agent.session.v1/${AGENT_INSTANCE_ID}"
  local agent_value=""
  [[ -z "$agent_pid" ]] || fail "Agent PID was unexpectedly present after recovery probe"
  "$repo_root/.local/bin/agent-service" >"$work_dir/agent-after-recovery-probe.log" 2>&1 &
  agent_pid="$!"
  wait_ready 18082 "$agent_pid" "Agent after recovery probe"
  agent_value="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 \
    get "$agent_key" --print-value-only)"
  jq -e --arg instance "$AGENT_INSTANCE_ID" '
    .service == "dora.agent.session.v1" and .instance_id == $instance and (.address | length) > 0 and (.version | length) > 0
  ' <<<"$agent_value" >/dev/null || fail "Agent did not restore etcd registration after recovery probe"
}

wait_for_etcd_lease_removal() {
  local business_key="/dora/services/dora.business.foundation.v1/${BUSINESS_INSTANCE_ID}"
  local agent_key="/dora/services/dora.agent.session.v1/${AGENT_INSTANCE_ID}"
  local business_value=""
  local agent_value=""
  for _ in $(seq 1 120); do
    business_value="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 \
      get "$business_key" --print-value-only)"
    agent_value="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 \
      get "$agent_key" --print-value-only)"
    if [[ -z "$business_value" && -z "$agent_value" ]]; then
      etcd_lease_removed=true
      return 0
    fi
    sleep 0.25
  done
  return 1
}

assert_file_excludes_literal() {
  local value="$1"
  local label="$2"
  [[ -n "$value" ]] || return 0
  local status=""
  if rg_with_pattern_stdin literal "$value" "$evidence_pending"; then
    fail "Evidence contains $label"
  else
    status="$?"
    [[ "$status" == "1" ]] || fail "Evidence redaction scan failed for $label"
  fi
}

assert_file_excludes_regex() {
  local pattern="$1"
  local label="$2"
  local status=""
  if rg_with_pattern_stdin regex "$pattern" "$evidence_pending"; then
    fail "Evidence contains $label"
  else
    status="$?"
    [[ "$status" == "1" ]] || fail "Evidence redaction scan failed for $label"
  fi
}

mkdir -p "$(dirname "$evidence_file")"
rm -f "$evidence_file" "$evidence_pending" "${evidence_file}.tmp"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/dora-plan-spec-preview.XXXXXX")"
chmod 700 "$work_dir"
control_dir="$work_dir/control"
mkdir -m 700 "$control_dir"
browser_result="$work_dir/browser-result.json"
: >"$browser_result"
chmod 600 "$browser_result"

[[ -r "$env_file" ]] || fail "ENV_FILE is not readable"
set -a
. "$env_file"
set +a
export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=true

[[ "${DORA_ENV:-}" == "local" ]] || fail "DORA_ENV must be local"
[[ "$DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED" == "true" ]] || fail "server Preview flag must be true"
command -v "$go_bin" >/dev/null 2>&1 || fail "Go SDK not found"
[[ -x "$migrate_bin" ]] || fail "golang-migrate CLI not found"
command -v docker >/dev/null 2>&1 || fail "Docker CLI not found"
command -v jq >/dev/null 2>&1 || fail "jq not found"
command -v shasum >/dev/null 2>&1 || fail "shasum not found"
command -v rg >/dev/null 2>&1 || fail "rg not found"
[[ -x "$repo_root/frontend/node_modules/.bin/playwright" ]] || fail "Playwright dependencies are not installed"

write_source_manifest "$work_dir/source-before.manifest" || fail "could not build source manifest"
source_digest_before="$(sha256_file "$work_dir/source-before.manifest")" || fail "could not hash source manifest"

mkdir -p "$repo_root/.local/bin"
GOWORK=off "$go_bin" -C "$repo_root/business" build -o "$repo_root/.local/bin/business-service" ./cmd/business-service || \
  fail "Business Runtime build failed"
GOWORK=off "$go_bin" -C "$repo_root/agent" build -o "$repo_root/.local/bin/agent-service" ./cmd/agent-service || \
  fail "Agent Runtime build failed"
business_binary_sha256="$(sha256_file "$repo_root/.local/bin/business-service")" || fail "Business binary hash failed"
agent_binary_sha256="$(sha256_file "$repo_root/.local/bin/agent-service")" || fail "Agent binary hash failed"

"${compose[@]}" up -d
ENV_FILE="$env_file" "$repo_root/scripts/wait-for-local-infra.sh"
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" business up
MIGRATE_BIN="$migrate_bin" "$repo_root/scripts/migrate.sh" agent up
migrations_applied=true
(
  cd "$repo_root/business"
  GOWORK=off "$go_bin" run -tags localsmoke ./cmd/local-smoke-seeder
) >"$work_dir/seeder.log" 2>&1 || fail "local smoke user seeding failed"

postgres_container="$("${compose[@]}" ps -q postgres)"
redis_container="$("${compose[@]}" ps -q redis)"
etcd_container="$("${compose[@]}" ps -q etcd)"
[[ -n "$postgres_container" && -n "$redis_container" && -n "$etcd_container" ]] || fail "real infra containers are missing"
postgres_version_num="$(docker exec "$postgres_container" psql -U dora_admin -d dora_business -qAtc 'SHOW server_version_num')"
[[ "$postgres_version_num" =~ ^[0-9]+$ && "$postgres_version_num" -ge 160000 ]] || fail "PostgreSQL 16+ was not observed"
redis_pong="$(docker exec "$redis_container" redis-cli ping | tr -d '\r')"
redis_version="$(docker exec "$redis_container" redis-cli info server | tr -d '\r' | sed -n 's/^redis_version://p' | head -n 1)"
[[ "$redis_pong" == "PONG" && "$redis_version" =~ ^7\. ]] || fail "Redis 7 was not observed"
docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 endpoint health \
  >"$work_dir/etcd-health.log" 2>&1 || fail "etcd health check failed"

collect_forbidden_side_effects "$work_dir/side-effects-before.json" || fail "could not capture side-effect baseline"

if [[ "${BUSINESS_RPC_ADVERTISED_ADDRESS%:*}" == "host.docker.internal" ]]; then
  BUSINESS_RPC_ADVERTISED_ADDRESS="$(discover_local_ipv4):${BUSINESS_RPC_LISTEN_ADDR##*:}"
  export BUSINESS_RPC_ADVERTISED_ADDRESS
fi
if [[ "${AGENT_RPC_ADVERTISED_ADDRESS%:*}" == "host.docker.internal" ]]; then
  AGENT_RPC_ADVERTISED_ADDRESS="$(discover_local_ipv4):${AGENT_RPC_LISTEN_ADDR##*:}"
  export AGENT_RPC_ADVERTISED_ADDRESS
fi

"$repo_root/.local/bin/business-service" >"$work_dir/business.log" 2>&1 &
business_pid="$!"
"$repo_root/.local/bin/agent-service" >"$work_dir/agent.log" 2>&1 &
agent_pid="$!"
wait_ready 18081 "$business_pid" Business
business_runtime_ready=true
wait_ready 18082 "$agent_pid" Agent
agent_runtime_ready=true

business_registration="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 \
  get "/dora/services/dora.business.foundation.v1/${BUSINESS_INSTANCE_ID}" --print-value-only)"
agent_registration="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 \
  get "/dora/services/dora.agent.session.v1/${AGENT_INSTANCE_ID}" --print-value-only)"
jq -e --arg instance "$BUSINESS_INSTANCE_ID" '
  .service == "dora.business.foundation.v1" and .instance_id == $instance and (.address | length) > 0 and (.version | length) > 0
' <<<"$business_registration" >/dev/null || fail "Business Foundation etcd registration is invalid"
jq -e --arg instance "$AGENT_INSTANCE_ID" '
  .service == "dora.agent.session.v1" and .instance_id == $instance and (.address | length) > 0 and (.version | length) > 0
' <<<"$agent_registration" >/dev/null || fail "Agent Session etcd registration is invalid"
etcd_registration_observed=true

preview_goal="PlanSpec Preview Trial ${run_id}"
legacy_goal="PlanSpec Legacy Lane ${run_id}"
(
  cd "$repo_root/frontend"
  CI=true \
  DORA_E2E_PLAN_SPEC_PREVIEW=1 \
  DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
  DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
  DORA_E2E_PREVIEW_GOAL="$preview_goal" \
  DORA_E2E_LEGACY_GOAL="$legacy_goal" \
  DORA_E2E_PLAN_SPEC_RESULT_PATH="$browser_result" \
  DORA_E2E_PLAN_SPEC_CONTROL_DIR="$control_dir" \
  DORA_E2E_BUSINESS_API_TARGET="http://127.0.0.1:18081" \
  DORA_E2E_OUTPUT_DIR="$work_dir/playwright-output" \
  VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED=true \
  ./node_modules/.bin/playwright test --config=playwright.config.js \
    e2e/plan-spec-preview.spec.js --grep '@plan-spec-preview'
) >"$work_dir/playwright.log" 2>&1 &
playwright_pid="$!"

positive_checkpoint="$control_dir/positive-before-replay.json"
wait_for_control_file "$positive_checkpoint" '
  keys == ["creation_spec_id","input_id","project_id","schema_version","session_id"]
  and .schema_version == "plan_spec_preview.positive_checkpoint.v1"
  and all(.project_id,.session_id,.input_id,.creation_spec_id;
    test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
' "positive authority checkpoint"
positive_project_id="$(jq -er '.project_id' "$positive_checkpoint")"
positive_session_id="$(jq -er '.session_id' "$positive_checkpoint")"
positive_input_id="$(jq -er '.input_id' "$positive_checkpoint")"
positive_creation_spec_id="$(jq -er '.creation_spec_id' "$positive_checkpoint")"
collect_positive_authority "$positive_project_id" "$positive_session_id" "$positive_input_id" \
  "$positive_creation_spec_id" "$work_dir/positive-before.json" || fail "positive authority capture failed"
restart_agent_strict
write_atomic_json "$control_dir/positive-replay-ack.json" -n \
  --arg project_id "$positive_project_id" --arg session_id "$positive_session_id" \
  --arg input_id "$positive_input_id" --arg creation_spec_id "$positive_creation_spec_id" '
  {schema_version:"plan_spec_preview.positive_checkpoint_ack.v1",project_id:$project_id,
   session_id:$session_id,input_id:$input_id,creation_spec_id:$creation_spec_id,authority_captured:true}
'

blocked_checkpoint="$control_dir/blocked-before-post.json"
wait_for_control_file "$blocked_checkpoint" '
  keys == ["input_id","project_id","schema_version","session_id"]
  and .schema_version == "plan_spec_preview.blocked_checkpoint.v1"
  and all(.project_id,.session_id,.input_id;
    test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
' "blocked-lane authority checkpoint"
blocked_project_id="$(jq -er '.project_id' "$blocked_checkpoint")"
blocked_session_id="$(jq -er '.session_id' "$blocked_checkpoint")"
blocked_input_id="$(jq -er '.input_id' "$blocked_checkpoint")"
collect_blocked_authority "$blocked_project_id" "$blocked_session_id" "$blocked_input_id" \
  "$work_dir/blocked-before.json" || fail "blocked-lane authority capture failed"
write_atomic_json "$control_dir/blocked-post-ack.json" -n \
  --arg project_id "$blocked_project_id" --arg session_id "$blocked_session_id" --arg input_id "$blocked_input_id" '
  {schema_version:"plan_spec_preview.blocked_checkpoint_ack.v1",project_id:$project_id,
   session_id:$session_id,input_id:$input_id,authority_captured:true}
'

if ! wait "$playwright_pid"; then
  playwright_pid=""
  sed -n '1,320p' "$work_dir/playwright.log" >&2 || true
  fail "real Chromium vertical slice failed"
fi
playwright_pid=""
[[ "$(file_mode "$browser_result")" == "600" ]] || fail "browser result mode is not 0600"
jq -e '
  keys == ["assertions","blocked_request_id","content_digest","creation_spec_id","creation_spec_version",
    "creator_user_id","input_id","legacy_input_id","legacy_project_id","legacy_session_id","project_id",
    "request_id","schema_version","session_id"]
  and .schema_version == "plan_spec_preview.browser_result.v1"
  and .creation_spec_version == 1
  and (.content_digest | test("^[0-9a-f]{64}$"))
  and all(.creator_user_id,.project_id,.session_id,.input_id,.request_id,.creation_spec_id,
    .legacy_project_id,.legacy_session_id,.legacy_input_id,.blocked_request_id;
    test("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))
  and (.assertions | keys) == ["chromium_browser","creation_spec_card_rendered","empty_lane_bootstrap_ready",
    "handoff_not_persisted_after_refresh","hard_refresh_snapshot_recovered","idempotent_replay_same_input",
    "legacy_quick_create_nonempty","legacy_session_lane_blocked_409","legacy_workspace_zero_delta",
    "preview_enqueue_202","preview_goal_handoff","preview_sse_completed","quick_create_initial_prompt_null",
    "same_origin_business_bff","sse_disconnect_recovered"]
  and all(.assertions[]; . == true)
' "$browser_result" >/dev/null || fail "browser result contract is invalid or contains a failed assertion"

[[ "$(jq -er '.project_id' "$browser_result")" == "$positive_project_id" \
  && "$(jq -er '.session_id' "$browser_result")" == "$positive_session_id" \
  && "$(jq -er '.input_id' "$browser_result")" == "$positive_input_id" \
  && "$(jq -er '.creation_spec_id' "$browser_result")" == "$positive_creation_spec_id" ]] || \
  fail "browser positive IDs do not match the authority checkpoint"
[[ "$(jq -er '.legacy_project_id' "$browser_result")" == "$blocked_project_id" \
  && "$(jq -er '.legacy_session_id' "$browser_result")" == "$blocked_session_id" \
  && "$(jq -er '.legacy_input_id' "$browser_result")" == "$blocked_input_id" ]] || \
  fail "browser blocked-lane IDs do not match the authority checkpoint"

collect_positive_authority "$positive_project_id" "$positive_session_id" "$positive_input_id" \
  "$positive_creation_spec_id" "$work_dir/positive-after.json" || fail "positive post-replay authority capture failed"
cmp -s "$work_dir/positive-before.json" "$work_dir/positive-after.json" || fail "idempotent replay changed authority facts"
collect_blocked_authority "$blocked_project_id" "$blocked_session_id" "$blocked_input_id" \
  "$work_dir/blocked-after.json" || fail "blocked-lane post-409 authority capture failed"
cmp -s "$work_dir/blocked-before.json" "$work_dir/blocked-after.json" || fail "SESSION_LANE_BLOCKED changed authority facts"

jq -e --arg creation_spec_id "$positive_creation_spec_id" --arg content_digest "$(jq -er '.content_digest' "$browser_result")" '
  .business.draft_count == 1
  and .business.receipt_count == 1
  and .business.draft_id == $creation_spec_id
  and .business.status == "draft"
  and .business.version == 1
  and .business.schema_version == "creation_spec.draft.v1"
  and .business.content_exact_fields == true
  and .business.content_shape_valid == true
  and .business.draft_receipt_consistent == true
  and .business.content_digest == $content_digest
  and .agent.session_count == 1
  and .agent.message_count == 1
  and .agent.input_count == 1
  and .agent.input_source_type == "creation_spec_preview"
  and .agent.input_status == "resolved"
  and .agent.input_attempts == 1
  and .agent.input_fence_token == 1
  and .agent.input_lease_released == true
  and .agent.run_count == 1
  and (.agent.run_id | test("^[0-9a-f-]{36}$"))
  and (.agent.tool_call_id | test("^[0-9a-f-]{36}$"))
  and (.agent.business_command_id | test("^[0-9a-f-]{36}$"))
  and .agent.run_message_input_consistent == true
  and .agent.model_receipt_count == 3
  and .agent.model_call_indexes == [1,2,3]
  and .agent.model_receipts_completed_encrypted == true
  and .agent.tool_receipt_count == 1
  and .agent.tool_receipt_completed_encrypted == true
  and .agent.tool_receipt_durable_command_safe == true
  and .agent.projection_count == 1
  and .agent.projection_resource_id == $creation_spec_id
  and .agent.projection_content_digest == $content_digest
  and .agent.projection_safe == true
  and .agent.event_count == 3
  and .agent.event_types == ["session.created","session.input.accepted","creation_spec.preview.completed"]
  and .agent.accepted_event_count == 1
  and .agent.completed_event_count == 1
  and .agent.failed_event_count == 0
  and .agent.event_last_seq == 3
  and .agent.event_min_available_seq == 1
  and .agent.last_message_seq == 1
  and .agent.last_input_enqueue_seq == 1
  and .agent.session_lease_released == true
  and .agent.session_fence_token == 1
' "$work_dir/positive-after.json" >/dev/null || fail "positive Business/Agent authority facts are incomplete"

jq -e '
  .business.project_count == 1
  and .business.creation_receipt_count == 1
  and .business.binding_count == 1
  and .business.delivered_outbox_count == 1
  and .business.creation_spec_count == 0
  and .business.creation_spec_receipt_count == 0
  and .agent.session_count == 1
  and .agent.message_count == 1
  and .agent.input_count == 1
  and .agent.legacy_input_count == 1
  and .agent.session_command_receipt_count == 1
  and .agent.preview_run_count == 0
  and .agent.preview_model_receipt_count == 0
  and .agent.preview_tool_receipt_count == 0
  and .agent.preview_projection_count == 0
  and .agent.event_count == 2
  and .agent.event_types == ["session.created","session.input.accepted"]
  and .agent.accepted_event_count == 1
  and .agent.event_last_seq == 2
  and .agent.event_min_available_seq == 1
  and .agent.last_message_seq == 1
  and .agent.last_input_enqueue_seq == 1
  and .agent.input_attempts == 0
  and .agent.input_fence_token == 0
  and .agent.input_lease_released == true
  and .agent.session_fence_token == 0
  and .agent.session_lease_released == true
' "$work_dir/blocked-after.json" >/dev/null || fail "blocked legacy-lane authority facts are invalid"

collect_forbidden_side_effects "$work_dir/side-effects-after.json" || fail "could not capture final side-effect facts"
cmp -s "$work_dir/side-effects-before.json" "$work_dir/side-effects-after.json" || \
  fail "billing/approval/job-adjacent authority facts changed"
jq -e '
  .business.billing_approval_job_table_count == 0
  and .agent.billing_approval_job_table_count == 0
  and .worker.billing_approval_job_table_count == 0
  and .worker.runtime_table_count == 0
' "$work_dir/side-effects-after.json" >/dev/null || fail "billing/approval/job authority unexpectedly exists"
no_billing_approval_job=true

recovery_result="$work_dir/durable-recovery-postgresql.json"
rm -f "$recovery_result"
stop_agent_for_recovery_probe_strict
(
  cd "$repo_root/agent"
  DORA_PLAN_SPEC_PREVIEW_RECOVERY_SMOKE_DSN="$AGENT_DATABASE_URL" \
  DORA_PLAN_SPEC_PREVIEW_RECOVERY_SMOKE_RESULT="$recovery_result" \
  GOWORK=off "$go_bin" test ./internal/postgres \
    -run '^TestCreationSpecPreviewDurableRecoveryPostgreSQLSmoke$' -count=1
) >"$work_dir/durable-recovery-postgresql.log" 2>&1 || {
  sed -n '1,260p' "$work_dir/durable-recovery-postgresql.log" >&2 || true
  fail "durable-command real PostgreSQL recovery probe failed"
}
[[ -s "$recovery_result" ]] || fail "durable-command recovery probe result is missing"
[[ "$(file_mode "$recovery_result")" == "600" ]] || fail "durable-command recovery probe result mode is not 0600"
jq -e '
  keys == ["assertions","counts","schema_version","status"]
  and .schema_version == "plan_spec_preview.durable_recovery_postgresql.v1"
  and .status == "passed"
  and (.assertions | keys) == ["authoritative_not_found_reserve_cas","business_adapter_command_exact",
    "durable_ciphertext_present","durable_key_reference_present","durable_payload_digest_valid",
    "exhausted_marked_explicitly","exhausted_not_claimed","formal_graph_recovery","head_of_line_not_skipped",
    "real_postgresql","resend_limit_frozen","restarted_owner_fence_rebuilt","result_payload_absent",
    "stable_business_command_id","stable_request_digest","stale_fence_rejected",
    "technical_failure_not_exhausted","technical_query_zero_budget_delta"]
  and all(.assertions[]; . == true)
  and (.counts | keys) == ["claimed_after_exhaustion","follower_pending","query_calls","resend_attempts",
    "resend_limit","save_calls"]
  and .counts.claimed_after_exhaustion == 0
  and .counts.follower_pending == 1
  and .counts.query_calls == 7
  and .counts.resend_attempts == 3
  and .counts.resend_limit == 3
  and .counts.save_calls == 3
' "$recovery_result" >/dev/null || fail "durable-command recovery probe result contract is invalid"
start_agent_after_recovery_probe_strict

write_source_manifest "$work_dir/source-after.manifest" || fail "could not rebuild source manifest"
source_digest_after="$(sha256_file "$work_dir/source-after.manifest")" || fail "could not hash final source manifest"
[[ "$source_digest_before" == "$source_digest_after" ]] || fail "source worktree changed during smoke"

stop_runtimes_strict
wait_for_etcd_lease_removal || fail "Runtime etcd leases were not removed after shutdown"

produced_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
jq -S -n \
  --arg run_id "$run_id" --arg produced_at "$produced_at" \
  --arg source_digest_sha256 "$source_digest_after" \
  --arg business_binary_sha256 "$business_binary_sha256" --arg agent_binary_sha256 "$agent_binary_sha256" \
  --arg postgres_version_num "$postgres_version_num" --arg redis_version "$redis_version" \
  --arg dora_env "$DORA_ENV" --arg server_preview_flag "$DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED" \
  --argjson business_ready "$business_runtime_ready" --argjson agent_ready "$agent_runtime_ready" \
  --argjson migrations_applied "$migrations_applied" --argjson etcd_registration "$etcd_registration_observed" \
  --argjson runtime_shutdown "$runtime_shutdown_observed" --argjson etcd_lease_removed "$etcd_lease_removed" \
  --argjson agent_restart "$agent_restart_observed" \
  --argjson no_side_effects "$no_billing_approval_job" \
  --slurpfile browser "$browser_result" --slurpfile positive "$work_dir/positive-after.json" \
  --slurpfile blocked "$work_dir/blocked-after.json" --slurpfile recovery "$recovery_result" '
  {
    schema_version:"plan_spec_preview.trial_evidence.v1",
    status:"pending",
    run_id:$run_id,
    produced_at:$produced_at,
    source_digest_sha256:$source_digest_sha256,
    business_binary_sha256:$business_binary_sha256,
    agent_binary_sha256:$agent_binary_sha256,
    resources:{
      creator_user_id:$browser[0].creator_user_id,
      project_id:$browser[0].project_id,
      session_id:$browser[0].session_id,
      input_id:$browser[0].input_id,
      request_id:$browser[0].request_id,
      run_id:$positive[0].agent.run_id,
      tool_call_id:$positive[0].agent.tool_call_id,
      business_command_id:$positive[0].agent.business_command_id,
      creation_spec_id:$browser[0].creation_spec_id,
      content_digest:$browser[0].content_digest,
      legacy_project_id:$browser[0].legacy_project_id,
      legacy_session_id:$browser[0].legacy_session_id,
      legacy_input_id:$browser[0].legacy_input_id,
      blocked_request_id:$browser[0].blocked_request_id
    },
    counts:{
      business_drafts:$positive[0].business.draft_count,
      business_command_receipts:$positive[0].business.receipt_count,
      agent_preview_messages:$positive[0].agent.message_count,
      agent_preview_inputs:$positive[0].agent.input_count,
      agent_preview_runs:$positive[0].agent.run_count,
      agent_model_receipts:$positive[0].agent.model_receipt_count,
      agent_tool_receipts:$positive[0].agent.tool_receipt_count,
      agent_projections:$positive[0].agent.projection_count,
      agent_events:$positive[0].agent.event_count,
      legacy_messages:$blocked[0].agent.message_count,
      legacy_inputs:$blocked[0].agent.input_count,
      legacy_events:$blocked[0].agent.event_count,
      recovery_query_calls:$recovery[0].counts.query_calls,
      recovery_resend_attempts:$recovery[0].counts.resend_attempts,
      recovery_resend_limit:$recovery[0].counts.resend_limit,
      recovery_save_calls:$recovery[0].counts.save_calls
    },
    assertions:{
      agent_restart_observed:$agent_restart,
      agent_runtime_ready:$agent_ready,
      agent_unique_input_run_receipts_events:($positive[0].agent.input_count == 1
        and $positive[0].agent.run_count == 1 and $positive[0].agent.model_receipt_count == 3
        and $positive[0].agent.tool_receipt_count == 1 and $positive[0].agent.event_count == 3),
      bff_enqueue_202:$browser[0].assertions.preview_enqueue_202,
      browser_authority_barriers:($positive[0].business.draft_count == 1 and $blocked[0].agent.preview_run_count == 0),
      browser_same_origin_bff:$browser[0].assertions.same_origin_business_bff,
      business_runtime_ready:$business_ready,
      business_unique_draft:($positive[0].business.draft_count == 1
        and $positive[0].business.receipt_count == 1 and $positive[0].business.draft_receipt_consistent),
      card_visible:$browser[0].assertions.creation_spec_card_rendered,
      chromium_real:$browser[0].assertions.chromium_browser,
      content_digest_consistent:($browser[0].content_digest == $positive[0].business.content_digest
        and $browser[0].content_digest == $positive[0].agent.projection_content_digest),
      current_worktree_binaries:(($source_digest_sha256 | test("^[0-9a-f]{64}$"))
        and ($business_binary_sha256 | test("^[0-9a-f]{64}$"))
        and ($agent_binary_sha256 | test("^[0-9a-f]{64}$"))),
      durable_command_persisted:$positive[0].agent.tool_receipt_durable_command_safe,
      empty_lane_bootstrap_ready:$browser[0].assertions.empty_lane_bootstrap_ready,
      etcd_lease_removed:$etcd_lease_removed,
      etcd_real:$etcd_registration,
      evidence_redacted:false,
      formal_local_eino_graph:($positive[0].agent.model_call_indexes == [1,2,3]
        and $positive[0].agent.model_receipts_completed_encrypted
        and $positive[0].agent.tool_receipt_completed_encrypted),
      hard_refresh_snapshot_recovered:($browser[0].assertions.hard_refresh_snapshot_recovered
        and $browser[0].assertions.handoff_not_persisted_after_refresh),
      idempotent_replay_zero_delta:$browser[0].assertions.idempotent_replay_same_input,
      legacy_session_lane_blocked:$browser[0].assertions.legacy_session_lane_blocked_409,
      legacy_single_accepted_event:($blocked[0].agent.accepted_event_count == 1),
      legacy_zero_delta:($browser[0].assertions.legacy_workspace_zero_delta
        and $blocked[0].agent.preview_run_count == 0 and $blocked[0].business.creation_spec_count == 0),
      local_preview_flags:($dora_env == "local" and $server_preview_flag == "true"),
      migrations_applied:$migrations_applied,
      no_billing_approval_job:$no_side_effects,
      postgres_real:(($postgres_version_num | tonumber) >= 160000),
      preview_form_handoff:$browser[0].assertions.preview_goal_handoff,
      processor_graph_completed:($positive[0].agent.input_status == "resolved"
        and $positive[0].agent.completed_event_count == 1 and $positive[0].agent.failed_event_count == 0),
      quick_create_initial_prompt_null:$browser[0].assertions.quick_create_initial_prompt_null,
      recovery_bounded_resend:($recovery[0].assertions.authoritative_not_found_reserve_cas
        and $recovery[0].counts.resend_attempts == 3 and $recovery[0].counts.resend_limit == 3),
      recovery_business_command_exact:($recovery[0].assertions.business_adapter_command_exact
        and $recovery[0].assertions.stable_business_command_id and $recovery[0].assertions.stable_request_digest),
      recovery_exhausted_hol:($recovery[0].assertions.exhausted_marked_explicitly
        and $recovery[0].assertions.exhausted_not_claimed and $recovery[0].assertions.head_of_line_not_skipped
        and $recovery[0].assertions.result_payload_absent),
      recovery_formal_graph:($recovery[0].assertions.formal_graph_recovery
        and $recovery[0].assertions.restarted_owner_fence_rebuilt
        and $recovery[0].assertions.stale_fence_rejected),
      recovery_technical_query_zero_budget:($recovery[0].assertions.technical_failure_not_exhausted
        and $recovery[0].assertions.technical_query_zero_budget_delta),
      redis_real:($redis_version | startswith("7.")),
      runtime_graceful_shutdown:$runtime_shutdown,
      sse_completion_observed:$browser[0].assertions.preview_sse_completed,
      sse_disconnect_recovered:$browser[0].assertions.sse_disconnect_recovered
    }
  }
' >"$evidence_pending"
chmod 600 "$evidence_pending"

jq -e '
  .schema_version == "plan_spec_preview.trial_evidence.v1"
  and .status == "pending"
  and (keys == ["agent_binary_sha256","assertions","business_binary_sha256","counts","produced_at","resources",
    "run_id","schema_version","source_digest_sha256","status"])
  and (.resources | keys) == ["blocked_request_id","business_command_id","content_digest","creation_spec_id",
    "creator_user_id","input_id","legacy_input_id","legacy_project_id","legacy_session_id","project_id",
    "request_id","run_id","session_id","tool_call_id"]
  and (.counts | keys) == ["agent_events","agent_model_receipts","agent_preview_inputs","agent_preview_messages",
    "agent_preview_runs","agent_projections","agent_tool_receipts","business_command_receipts","business_drafts",
    "legacy_events","legacy_inputs","legacy_messages","recovery_query_calls","recovery_resend_attempts",
    "recovery_resend_limit","recovery_save_calls"]
  and (.assertions | keys) == ["agent_restart_observed","agent_runtime_ready","agent_unique_input_run_receipts_events",
    "bff_enqueue_202","browser_authority_barriers","browser_same_origin_bff","business_runtime_ready","business_unique_draft",
    "card_visible","chromium_real","content_digest_consistent","current_worktree_binaries","durable_command_persisted",
    "empty_lane_bootstrap_ready",
    "etcd_lease_removed","etcd_real","evidence_redacted","formal_local_eino_graph","hard_refresh_snapshot_recovered",
    "idempotent_replay_zero_delta","legacy_session_lane_blocked","legacy_single_accepted_event","legacy_zero_delta",
    "local_preview_flags","migrations_applied","no_billing_approval_job","postgres_real","preview_form_handoff",
    "processor_graph_completed","quick_create_initial_prompt_null","recovery_bounded_resend",
    "recovery_business_command_exact","recovery_exhausted_hol","recovery_formal_graph",
    "recovery_technical_query_zero_budget","redis_real","runtime_graceful_shutdown",
    "sse_completion_observed","sse_disconnect_recovered"]
  and .assertions.evidence_redacted == false
  and all(.assertions | to_entries[] | select(.key != "evidence_redacted"); .value == true)
' "$evidence_pending" >/dev/null || fail "pending Trial Evidence contains a failed assertion"

assert_file_excludes_literal "$preview_goal" "Preview goal"
assert_file_excludes_literal "$legacy_goal" "legacy goal"
assert_file_excludes_literal "$DORA_SMOKE_USER_EMAIL" "login email"
assert_file_excludes_literal "$DORA_SMOKE_USER_PASSWORD" "login password"
assert_file_excludes_literal "${BUSINESS_AUTH_CSRF_SECRET_BASE64:-}" "Business CSRF secret"
assert_file_excludes_literal "${BUSINESS_PROJECT_PROMPT_KEY_BASE64:-}" "Business prompt key"
assert_file_excludes_literal "${BUSINESS_AGENT_HTTP_ASSERTION_SECRET_BASE64:-}" "Business assertion secret"
assert_file_excludes_literal "${AGENT_HTTP_ASSERTION_ACTIVE_SECRET_BASE64:-}" "Agent assertion secret"
assert_file_excludes_literal "${AGENT_SESSION_RPC_AUTH_SECRET_BASE64:-}" "Agent RPC secret"
assert_file_excludes_literal "${AGENT_CONTENT_KEY_BASE64:-}" "Agent content key"
assert_file_excludes_regex '(?i)(password|cookie|csrf_token|authorization|idempotency_key|ciphertext|nonce|key_version)[[:space:]]*"?:' \
  "secret or encrypted payload field"
assert_file_excludes_regex 'PlanSpec (Preview Trial|Legacy Lane)' "full prompt text"

jq -S '.status = "passed" | .assertions.evidence_redacted = true' "$evidence_pending" >"${evidence_file}.tmp"
chmod 600 "${evidence_file}.tmp"
mv "${evidence_file}.tmp" "$evidence_file"
chmod 600 "$evidence_file"
rm -f "$evidence_pending"
evidence_pending="$evidence_file"
jq -e '.status == "passed" and all(.assertions[]; . == true)' "$evidence_pending" >/dev/null || \
  fail "final Trial Evidence is not fully passed"
assert_file_excludes_literal "$preview_goal" "Preview goal"
assert_file_excludes_literal "$legacy_goal" "legacy goal"
assert_file_excludes_regex '(?i)(password|cookie|csrf_token|authorization|idempotency_key|ciphertext|nonce|key_version)[[:space:]]*"?:' \
  "secret or encrypted payload field"
[[ "$(file_mode "$evidence_file")" == "600" ]] || fail "Trial Evidence mode is not 0600"
evidence_pending=""

rm -rf "$work_dir"
work_dir=""
trap - EXIT
printf 'plan-spec-preview real Runtime/PostgreSQL/Redis/etcd/Chromium smoke passed: %s\n' "$evidence_file"
