#!/usr/bin/env bash
set -euo pipefail

BUSINESS_BASE_URL="${BUSINESS_BASE_URL:-http://127.0.0.1:19080}"
AGENT_BASE_URL="${AGENT_BASE_URL:-http://127.0.0.1:18080}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-doraigc-postgres}"
POSTGRES_USER="${POSTGRES_USER:-doraigc}"
BUSINESS_DB_NAME="${BUSINESS_DB_NAME:-doraigc}"
AGENT_DB_NAME="${AGENT_DB_NAME:-dora_agent}"
ADMIN_ACCOUNT="${ADMIN_ACCOUNT:-admin@dora.local}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-local-admin-change-me}"
USER_ACCOUNT="${USER_ACCOUNT:-user1001@dora.local}"
USER_PASSWORD="${USER_PASSWORD:-local-user-change-me}"
PROJECT_ID="${PROJECT_ID:-prj_active_1001}"
RUN_SUFFIX="${RUN_SUFFIX:-$(date -u +%Y%m%d%H%M%S)}"
TRACE_ID="deepseek-v4-flash-real-${RUN_SUFFIX}"
UNIQUE_HINT="deepseek-v4-flash-real-${RUN_SUFFIX}"
MODEL_RESOURCE_TYPE="image"
PROVIDER_CODE="deepseek"
MODEL_CODE="${DEEPSEEK_MODEL:-deepseek-v4-flash}"
DEEPSEEK_BASE_URL_FOR_PROVIDER="${DEEPSEEK_BASE_URL:-https://api.deepseek.com}"

json_field() {
  jq -r "$1"
}

api() {
  local method="$1"
  local url="$2"
  local token="$3"
  local idem="${4:-}"
  local body="${5:-}"
  local response
  local status
  response=$(mktemp)
  if [[ -n "$body" ]]; then
    status=$(curl -sS -o "$response" -w '%{http_code}' -X "$method" "$url" \
      -H 'Content-Type: application/json' \
      -H "X-Trace-Id: $TRACE_ID" \
      ${token:+-H "Authorization: Bearer $token"} \
      ${idem:+-H "Idempotency-Key: $idem"} \
      -d "$body")
  else
    status=$(curl -sS -o "$response" -w '%{http_code}' -X "$method" "$url" \
      -H 'Content-Type: application/json' \
      -H "X-Trace-Id: $TRACE_ID" \
      ${token:+-H "Authorization: Bearer $token"} \
      ${idem:+-H "Idempotency-Key: $idem"})
  fi
  if [[ "$status" != "200" ]]; then
    echo "HTTP $method $url failed status=$status" >&2
    cat "$response" >&2
    rm -f "$response"
    return 1
  fi
  cat "$response"
  rm -f "$response"
}

psql_business() {
  docker exec -i "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$BUSINESS_DB_NAME" -v ON_ERROR_STOP=1 "$@"
}

psql_agent() {
  docker exec -i "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$AGENT_DB_NAME" -v ON_ERROR_STOP=1 "$@"
}

cleanup_skill() {
  if [[ -n "${SKILL_ID:-}" && -n "${ADMIN_TOKEN:-}" && "${KEEP_E2E_SKILL:-false}" != "true" ]]; then
    api POST "$BUSINESS_BASE_URL/api/admin/skills/system/$SKILL_ID/deprecate" "$ADMIN_TOKEN" "deepseek-deprecate-${RUN_SUFFIX}" '{}' >/dev/null || true
  fi
}
trap cleanup_skill EXIT

env_value() {
  local key="$1"
  awk -F= -v key="$key" '$1 == key {print substr($0, length(key) + 2)}' .env.local 2>/dev/null | tail -n 1
}

require_local_deepseek_config() {
  local key
  key="$(env_value DEEPSEEK_API_KEY)"
  if [[ -z "$key" ]]; then
    echo "DEEPSEEK_API_KEY missing in .env.local" >&2
    exit 1
  fi
  if [[ -z "${DEEPSEEK_MODEL:-}" ]]; then
    local local_model
    local_model="$(env_value DEEPSEEK_MODEL)"
    if [[ -n "$local_model" ]]; then
      MODEL_CODE="$local_model"
    fi
  fi
  if [[ -z "${DEEPSEEK_BASE_URL:-}" ]]; then
    local local_base_url
    local_base_url="$(env_value DEEPSEEK_BASE_URL)"
    if [[ -n "$local_base_url" ]]; then
      DEEPSEEK_BASE_URL_FOR_PROVIDER="$local_base_url"
    fi
  fi
}

ensure_deepseek_provider() {
  local existing_id
  existing_id=$(psql_business -Atc "SELECT id FROM model_providers WHERE provider_code='${PROVIDER_CODE}' AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1;")
  if [[ -n "$existing_id" ]]; then
    PROVIDER_ID="$existing_id"
    api PATCH "$BUSINESS_BASE_URL/api/admin/models/providers/$PROVIDER_ID" "$ADMIN_TOKEN" "deepseek-provider-patch-${RUN_SUFFIX}" "$(jq -nc \
      --arg provider_name "DeepSeek" \
      --arg base_url "$DEEPSEEK_BASE_URL_FOR_PROVIDER" \
      '{provider_name:$provider_name,provider_type:"openai_compatible",status:"active",base_url:$base_url,secret_key_ref:"env:DEEPSEEK_API_KEY",config:{timeout_ms:120000,model_adapter:"deepseek",runtime_env:"DEEPSEEK_API_KEY"}}')" >/dev/null
    return
  fi
  local provider
  provider=$(api POST "$BUSINESS_BASE_URL/api/admin/models/providers" "$ADMIN_TOKEN" "deepseek-provider-${RUN_SUFFIX}" "$(jq -nc \
    --arg provider_code "$PROVIDER_CODE" \
    --arg base_url "$DEEPSEEK_BASE_URL_FOR_PROVIDER" \
    '{provider_code:$provider_code,provider_name:"DeepSeek",provider_type:"openai_compatible",status:"active",base_url:$base_url,secret_key_ref:"env:DEEPSEEK_API_KEY",config:{timeout_ms:120000,model_adapter:"deepseek",runtime_env:"DEEPSEEK_API_KEY"}}')")
  PROVIDER_ID=$(echo "$provider" | json_field '.data.provider_id')
}

ensure_deepseek_model() {
  local existing_id
  existing_id=$(psql_business -Atc "SELECT id FROM models WHERE provider_id='${PROVIDER_ID}' AND model_code='${MODEL_CODE}' AND resource_type='${MODEL_RESOURCE_TYPE}' AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1;")
  if [[ -n "$existing_id" ]]; then
    MODEL_ID="$existing_id"
    local patched
    patched=$(api PATCH "$BUSINESS_BASE_URL/api/admin/models/$MODEL_ID" "$ADMIN_TOKEN" "deepseek-model-patch-${RUN_SUFFIX}" "$(jq -nc \
      --arg provider_id "$PROVIDER_ID" \
      --arg model_code "$MODEL_CODE" \
      '{provider_id:$provider_id,model_code:$model_code,display_name:"DeepSeek V4 Flash",resource_type:"image",billing_unit:"asset",unit_points:1,min_charge_points:1,status:"active",capability_tags:["deepseek","v4","flash","chat"],route_config:{provider:"deepseek",model:$model_code,runtime:"deepseek_chat_completions",real_output:true}}')")
    PRICING_SNAPSHOT_ID=$(echo "$patched" | json_field '.data.pricing_snapshot_id')
    return
  fi
  local model
  model=$(api POST "$BUSINESS_BASE_URL/api/admin/models" "$ADMIN_TOKEN" "deepseek-model-${RUN_SUFFIX}" "$(jq -nc \
    --arg provider_id "$PROVIDER_ID" \
    --arg model_code "$MODEL_CODE" \
    '{provider_id:$provider_id,model_code:$model_code,display_name:"DeepSeek V4 Flash",resource_type:"image",billing_unit:"asset",unit_points:1,min_charge_points:1,status:"active",capability_tags:["deepseek","v4","flash","chat"],route_config:{provider:"deepseek",model:$model_code,runtime:"deepseek_chat_completions",real_output:true}}')")
  MODEL_ID=$(echo "$model" | json_field '.data.model_id')
  PRICING_SNAPSHOT_ID=$(echo "$model" | json_field '.data.pricing_snapshot_id')
}

echo "[1/8] check local DeepSeek config and service health"
require_local_deepseek_config
curl -fsS "$BUSINESS_BASE_URL/readyz" >/dev/null
curl -fsS "$AGENT_BASE_URL/readyz" >/dev/null

echo "[2/8] login admin and user"
ADMIN_TOKEN=$(api POST "$BUSINESS_BASE_URL/api/admin/auth/login" "" "" "{\"account\":\"$ADMIN_ACCOUNT\",\"password\":\"$ADMIN_PASSWORD\"}" | json_field '.data.access_token')
USER_LOGIN=$(api POST "$BUSINESS_BASE_URL/api/auth/login" "" "" "{\"login_type\":\"personal\",\"account\":\"$USER_ACCOUNT\",\"password\":\"$USER_PASSWORD\"}")
USER_TOKEN=$(echo "$USER_LOGIN" | json_field '.data.access_token')

echo "[3/8] ensure DeepSeek provider/model and make image default"
ensure_deepseek_provider
ensure_deepseek_model
api POST "$BUSINESS_BASE_URL/api/admin/models/default" "$ADMIN_TOKEN" "deepseek-default-${RUN_SUFFIX}" "$(jq -nc --arg model_id "$MODEL_ID" --arg pricing_snapshot_id "$PRICING_SNAPSHOT_ID" '{resource_type:"image",model_id:$model_id,pricing_snapshot_id:$pricing_snapshot_id}')" >/dev/null

echo "[4/8] create and publish route-hint Skill"
SKILL_MARKDOWN=$(jq -Rs . <<EOF
# DeepSeek V4 Flash 真实对话 Skill

当用户提示包含 ${UNIQUE_HINT} 时，必须使用本 Skill，先整理需求，再让 DeepSeek 输出一段可观察的中文创作方案。

<tool id="model_generation:image">模型生成</tool>

输出需要包含：一句总结、三条分镜建议、一个风险提醒。
EOF
)
OUTPUT_ELEMENT_SCHEMA='{"type":"object","properties":{"asset_id":{"type":"string"},"text_preview":{"type":"string"}}}'
SKILL=$(api POST "$BUSINESS_BASE_URL/api/admin/skills/system" "$ADMIN_TOKEN" "deepseek-skill-${RUN_SUFFIX}" "$(jq -nc \
  --arg skill_key "deepseek_v4_flash_real_${RUN_SUFFIX}" \
  --arg skill_name "DeepSeek V4 Flash 真实对话 Skill ${RUN_SUFFIX}" \
  --arg hint "$UNIQUE_HINT" \
  --arg output_element_schema "$OUTPUT_ELEMENT_SCHEMA" \
  --argjson skill_markdown "$SKILL_MARKDOWN" \
  '{skill_key:$skill_key,skill_name:$skill_name,skill_tags:["deepseek","real_output","e2e"],version:"1.0.0",route_hints:{keyword:$hint},skill_markdown:$skill_markdown,confirmation_policy_json:"{\"requires_confirmation\":false}",output_elements:[{element_type:"image_ref",element_name:"DeepSeek 真实输出资产",required:true,use_draft:true,use_final:true,editable:true,referable:true,display_order:10,display_slot:"asset_detail",schema_json:$output_element_schema}]}')")
SKILL_ID=$(echo "$SKILL" | json_field '.data.skill_id')
VERSION_ID=$(echo "$SKILL" | json_field '.data.latest_version_id')
psql_business -q <<SQL
INSERT INTO skill_test_cases (id, skill_id, version_id, case_name, test_input_json, expected_elements_json, status, created_by_user_id, created_by, updated_by, created_at, updated_at)
VALUES
  ('skcase_${RUN_SUFFIX}_1', '${SKILL_ID}', '${VERSION_ID}', 'deepseek real output case 1', '{"prompt":"${UNIQUE_HINT} case 1"}'::jsonb, '[{"element_type":"image_ref"}]'::jsonb, 'active', 'adm_root', 'adm_root', 'adm_root', now(), now()),
  ('skcase_${RUN_SUFFIX}_2', '${SKILL_ID}', '${VERSION_ID}', 'deepseek real output case 2', '{"prompt":"${UNIQUE_HINT} case 2"}'::jsonb, '[{"element_type":"image_ref"}]'::jsonb, 'active', 'adm_root', 'adm_root', 'adm_root', now(), now()),
  ('skcase_${RUN_SUFFIX}_3', '${SKILL_ID}', '${VERSION_ID}', 'deepseek real output case 3', '{"prompt":"${UNIQUE_HINT} case 3"}'::jsonb, '[{"element_type":"image_ref"}]'::jsonb, 'active', 'adm_root', 'adm_root', 'adm_root', now(), now())
ON CONFLICT DO NOTHING;
SQL
api POST "$BUSINESS_BASE_URL/api/admin/skills/system/$SKILL_ID/publish" "$ADMIN_TOKEN" "deepseek-publish-${RUN_SUFFIX}" "$(jq -nc --arg version_id "$VERSION_ID" '{version_id:$version_id}')" >/dev/null

echo "[5/8] create user run and assert Skill/model routing"
SESSION=$(api POST "$AGENT_BASE_URL/api/agent/sessions" "$USER_TOKEN" "deepseek-session-${RUN_SUFFIX}" "$(jq -nc --arg project_id "$PROJECT_ID" '{project_id:$project_id,initial_title:"DeepSeek V4 Flash 真实输出验证"}')")
SESSION_ID=$(echo "$SESSION" | json_field '.session_id')
PROMPT="${UNIQUE_HINT} 请输出一个 30 秒城市香水广告短片方案：一句总结、三条分镜建议、一个风险提醒。"
RUN=$(api POST "$AGENT_BASE_URL/api/agent/runs" "$USER_TOKEN" "deepseek-run-${RUN_SUFFIX}" "$(jq -nc --arg session_id "$SESSION_ID" --arg project_id "$PROJECT_ID" --arg prompt "$PROMPT" '{session_id:$session_id,project_id:$project_id,user_input:{client_message_id:"cm_deepseek_v4_flash_real",content_type:"text",text:$prompt}}')")
RUN_ID=$(echo "$RUN" | json_field '.run_id')
EVENTS_BEFORE=$(api GET "$AGENT_BASE_URL/api/agent/runs/$RUN_ID/events?after_sequence=0&limit=100" "$USER_TOKEN")
echo "$EVENTS_BEFORE" | jq -e --arg skill_id "$SKILL_ID" '.events[] | select(.type=="agent.skill.selected" and .payload.skill_id==$skill_id and .payload.matched_reason=="route_hint:keyword")' >/dev/null
echo "$EVENTS_BEFORE" | jq -e --arg model_id "$MODEL_ID" '.events[] | select(.type=="generation.progress" and .payload.status=="model_snapshot_resolved" and .payload.model_id==$model_id)' >/dev/null

SNAPSHOT=$(api GET "$AGENT_BASE_URL/api/agent/runs/$RUN_ID/snapshot" "$USER_TOKEN")
INTERRUPT_ID=$(echo "$SNAPSHOT" | json_field '.interrupt.interrupt_id')
PAYLOAD_DIGEST=$(echo "$SNAPSHOT" | json_field '.interrupt.payload_digest')

echo "[6/8] accept confirmation and wait for real DeepSeek generation"
api POST "$AGENT_BASE_URL/api/agent/runs/$RUN_ID/interrupts/$INTERRUPT_ID/accept" "$USER_TOKEN" "deepseek-accept-${RUN_SUFFIX}" "$(jq -nc --arg run_id "$RUN_ID" --arg interrupt_id "$INTERRUPT_ID" --arg digest "$PAYLOAD_DIGEST" '{run_id:$run_id,interrupt_id:$interrupt_id,action:"confirm",confirmed_payload_digest:$digest}')" >/dev/null
for _ in {1..90}; do
  STATUS=$(api GET "$AGENT_BASE_URL/api/agent/runs/$RUN_ID" "$USER_TOKEN" | json_field '.status')
  if [[ "$STATUS" == "completed" || "$STATUS" == "failed" ]]; then
    break
  fi
  sleep 2
done
if [[ "${STATUS:-}" != "completed" ]]; then
  echo "run did not complete, status=${STATUS:-unknown}" >&2
  api GET "$AGENT_BASE_URL/api/agent/runs/$RUN_ID/events?after_sequence=0&limit=100" "$USER_TOKEN" | jq '.events[] | {seq:.sequence,type:.type,payload:.payload}' >&2
  exit 1
fi

echo "[7/8] assert persisted DeepSeek output, draft artifact, final asset ref"
EVENTS_AFTER=$(api GET "$AGENT_BASE_URL/api/agent/runs/$RUN_ID/events?after_sequence=0&limit=100" "$USER_TOKEN")
echo "$EVENTS_AFTER" | jq -e '.events[] | select(.type=="generation.artifact.completed" and .payload.metadata_summary.adapter=="deepseek_chat_completions" and .payload.metadata_summary.provider=="deepseek" and (.payload.metadata_summary.output_preview | length > 20))' >/dev/null
echo "$EVENTS_AFTER" | jq -e '.events[] | select(.type=="asset.save.completed" and .payload.elements[0].element_type=="image_ref")' >/dev/null
SKILL_SELECTION=$(psql_agent -Atc "SELECT skill_selection::text FROM agent_runs WHERE id='${RUN_ID}';")
MODEL_SELECTION=$(psql_agent -Atc "SELECT model_selection_snapshot::text FROM agent_runs WHERE id='${RUN_ID}';")
DRAFT_ARTIFACT=$(psql_agent -Atc "SELECT content::text FROM agent_artifacts WHERE run_id='${RUN_ID}' AND artifact_type='draft_element' AND element_type='image_ref' ORDER BY created_at DESC LIMIT 1;")
FINAL_ARTIFACT=$(psql_agent -Atc "SELECT id || '|' || artifact_type || '|' || status || '|' || element_type || '|' || business_ref_id FROM agent_artifacts WHERE run_id='${RUN_ID}' AND artifact_type='asset_ref' ORDER BY created_at DESC LIMIT 1;")
echo "$SKILL_SELECTION" | jq -e --arg skill_id "$SKILL_ID" 'select(.skill_id==$skill_id and .output_elements_count==1 and .output_elements[0].element_type=="image_ref")' >/dev/null
echo "$MODEL_SELECTION" | jq -e --arg model_id "$MODEL_ID" --arg provider_ref "${PROVIDER_CODE}:${MODEL_CODE}" 'select(.model_snapshot.model_id==$model_id and .model_snapshot.provider_runtime_ref==$provider_ref and .model_snapshot.runtime_parameters.runtime=="deepseek_chat_completions")' >/dev/null
echo "$DRAFT_ARTIFACT" | jq -e 'select(.metadata_summary.adapter=="deepseek_chat_completions" and (.metadata_summary.output_preview | length > 20) and .elements_summary.primary_element_type=="image_ref")' >/dev/null
if [[ -z "$FINAL_ARTIFACT" ]]; then
  echo "final asset_ref artifact missing" >&2
  exit 1
fi

echo "[8/8] summary"
OUTPUT_PREVIEW=$(echo "$DRAFT_ARTIFACT" | jq -r '.metadata_summary.output_preview')
ARTIFACT_EVENT=$(echo "$EVENTS_AFTER" | jq -c '.events[] | select(.type=="generation.artifact.completed") | .payload' | tail -n 1)
jq -nc \
  --arg trace_id "$TRACE_ID" \
  --arg run_id "$RUN_ID" \
  --arg session_id "$SESSION_ID" \
  --arg skill_id "$SKILL_ID" \
  --arg version_id "$VERSION_ID" \
  --arg model_id "$MODEL_ID" \
  --arg provider_id "$PROVIDER_ID" \
  --arg provider_ref "${PROVIDER_CODE}:${MODEL_CODE}" \
  --arg unique_hint "$UNIQUE_HINT" \
  --arg final_artifact "$FINAL_ARTIFACT" \
  --arg output_preview "$OUTPUT_PREVIEW" \
  --argjson events "$EVENTS_AFTER" \
  --argjson skill_selection "$SKILL_SELECTION" \
  --argjson model_selection "$MODEL_SELECTION" \
  --argjson draft_artifact "$DRAFT_ARTIFACT" \
  --argjson artifact_event "$ARTIFACT_EVENT" \
  '{trace_id:$trace_id,run_id:$run_id,session_id:$session_id,skill_id:$skill_id,version_id:$version_id,model_id:$model_id,provider_id:$provider_id,provider_runtime_ref:$provider_ref,unique_hint:$unique_hint,status:"completed",output_preview:$output_preview,artifact_event:$artifact_event,draft_artifact:$draft_artifact,final_artifact:$final_artifact,skill_selection:$skill_selection,model_selection:$model_selection,event_flow:[ $events.events[] | {seq:.sequence,type:.type,payload:.payload}]}'
