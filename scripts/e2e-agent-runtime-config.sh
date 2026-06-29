#!/usr/bin/env bash
set -euo pipefail

BUSINESS_BASE_URL="${BUSINESS_BASE_URL:-http://127.0.0.1:19080}"
AGENT_BASE_URL="${AGENT_BASE_URL:-http://127.0.0.1:18080}"
BUSINESS_DB_NAME="${BUSINESS_DB_NAME:-doraigc}"
AGENT_DB_NAME="${AGENT_DB_NAME:-dora_agent}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-doraigc-postgres}"
POSTGRES_USER="${POSTGRES_USER:-doraigc}"
ADMIN_ACCOUNT="${ADMIN_ACCOUNT:-admin@dora.local}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-local-admin-change-me}"
USER_ACCOUNT="${USER_ACCOUNT:-user1001@dora.local}"
USER_PASSWORD="${USER_PASSWORD:-local-user-change-me}"
PROJECT_ID="${PROJECT_ID:-prj_active_1001}"

RUN_SUFFIX="${RUN_SUFFIX:-$(date -u +%Y%m%d%H%M%S)}"
TRACE_ID="e2e-agent-runtime-${RUN_SUFFIX}"
UNIQUE_HINT="agent-e2e-skill-${RUN_SUFFIX}"
PROVIDER_CODE="agent_e2e_provider_${RUN_SUFFIX}"
MODEL_CODE="agent-e2e-image-${RUN_SUFFIX}"
SKILL_KEY="agent_e2e_skill_${RUN_SUFFIX}"
MODEL_RESOURCE_TYPE="image"

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
    echo "HTTP $method $url failed with status $status" >&2
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

restore_default() {
  if [[ -n "${ORIGINAL_DEFAULT_MODEL:-}" && -n "${ORIGINAL_DEFAULT_PRICE:-}" ]]; then
    psql_business -q <<SQL >/dev/null
UPDATE default_models
SET model_id = '${ORIGINAL_DEFAULT_MODEL}', pricing_snapshot_id = '${ORIGINAL_DEFAULT_PRICE}', updated_at = now(), updated_by = 'e2e-restore'
WHERE resource_type = '${MODEL_RESOURCE_TYPE}' AND scope = 'global' AND status = 'active';
SQL
  fi
  if [[ -n "${SKILL_ID:-}" && -n "${ADMIN_TOKEN:-}" && "${KEEP_E2E_SKILL:-false}" != "true" ]]; then
    api POST "$BUSINESS_BASE_URL/api/admin/skills/system/$SKILL_ID/deprecate" "$ADMIN_TOKEN" "e2e-deprecate-${RUN_SUFFIX}" '{}' >/dev/null || true
  fi
}
trap restore_default EXIT

echo "[1/8] health checks"
curl -fsS "$BUSINESS_BASE_URL/readyz" >/dev/null
curl -fsS "$AGENT_BASE_URL/readyz" >/dev/null

echo "[2/8] login admin and user"
ADMIN_TOKEN=$(api POST "$BUSINESS_BASE_URL/api/admin/auth/login" "" "" "{\"account\":\"$ADMIN_ACCOUNT\",\"password\":\"$ADMIN_PASSWORD\"}" | json_field '.data.access_token')
USER_LOGIN=$(api POST "$BUSINESS_BASE_URL/api/auth/login" "" "" "{\"login_type\":\"personal\",\"account\":\"$USER_ACCOUNT\",\"password\":\"$USER_PASSWORD\"}")
USER_TOKEN=$(echo "$USER_LOGIN" | json_field '.data.access_token')
SPACE_ID=$(echo "$USER_LOGIN" | json_field '.data.current_space_id')

echo "[3/8] create provider, model, and set image default"
read -r ORIGINAL_DEFAULT_MODEL ORIGINAL_DEFAULT_PRICE < <(psql_business -Atc "SELECT model_id, pricing_snapshot_id FROM default_models WHERE resource_type='${MODEL_RESOURCE_TYPE}' AND scope='global' AND status='active' LIMIT 1;" | awk -F'|' '{print $1, $2}')
PROVIDER=$(api POST "$BUSINESS_BASE_URL/api/admin/models/providers" "$ADMIN_TOKEN" "e2e-provider-${RUN_SUFFIX}" "$(jq -nc \
  --arg provider_code "$PROVIDER_CODE" \
  --arg provider_name "Agent E2E Provider $RUN_SUFFIX" \
  '{provider_code:$provider_code,provider_name:$provider_name,provider_type:"openai_compatible",status:"active",base_url:"http://127.0.0.1:19999/v1",config:{secret_key_ref:"secret/e2e/local",timeout_ms:12345}}')")
PROVIDER_ID=$(echo "$PROVIDER" | json_field '.data.provider_id')
MODEL=$(api POST "$BUSINESS_BASE_URL/api/admin/models" "$ADMIN_TOKEN" "e2e-model-${RUN_SUFFIX}" "$(jq -nc \
  --arg provider_id "$PROVIDER_ID" \
  --arg model_code "$MODEL_CODE" \
  --arg display_name "Agent E2E Image Model $RUN_SUFFIX" \
  '{provider_id:$provider_id,model_code:$model_code,display_name:$display_name,resource_type:"image",billing_unit:"asset",unit_points:1,min_charge_points:1,status:"active",capability_tags:["e2e","image"],route_config:{e2e_marker:"runtime_config",quality:"smoke"}}')")
MODEL_ID=$(echo "$MODEL" | json_field '.data.model_id')
api POST "$BUSINESS_BASE_URL/api/admin/models/default" "$ADMIN_TOKEN" "e2e-default-${RUN_SUFFIX}" "$(jq -nc --arg model_id "$MODEL_ID" '{resource_type:"image",model_id:$model_id}')" >/dev/null

echo "[4/8] create system skill with output_elements"
SKILL=$(api POST "$BUSINESS_BASE_URL/api/admin/skills/system" "$ADMIN_TOKEN" "e2e-skill-${RUN_SUFFIX}" "$(jq -nc \
  --arg skill_key "$SKILL_KEY" \
  --arg skill_name "Agent E2E Skill $RUN_SUFFIX" \
  --arg hint "$UNIQUE_HINT" \
  '{skill_key:$skill_key,skill_name:$skill_name,version:"1.0.0",route_hints:{keyword:$hint},skill_markdown:"# Name\nAgent E2E Skill\n\n## Invocation Rule\nUse when prompt contains the unique e2e marker.\n\n## Result Outputs\nReturn a short image generation plan.",skill_spec_json:"{}",output_schema_json:"{}",confirmation_policy_json:"{\"requires_confirmation\":false}",output_elements:[{element_type:"image_ref",element_name:"E2E image",required:true,use_draft:true,use_final:true,editable:true,referable:true,display_order:10,display_slot:"asset_detail",schema_json:"{\"type\":\"object\"}"}]}')")
SKILL_ID=$(echo "$SKILL" | json_field '.data.skill_id')
VERSION_ID=$(echo "$SKILL" | json_field '.data.latest_version_id')

echo "[5/8] seed required active skill test cases and publish"
psql_business -q <<SQL
INSERT INTO skill_test_cases (id, skill_id, version_id, case_name, test_input_json, expected_elements_json, status, created_by_user_id, created_by, updated_by, created_at, updated_at)
VALUES
  ('skcase_${RUN_SUFFIX}_1', '${SKILL_ID}', '${VERSION_ID}', 'e2e case 1', '{"prompt":"${UNIQUE_HINT} one"}'::jsonb, '[{"element_type":"image_ref"}]'::jsonb, 'active', 'adm_root', 'adm_root', 'adm_root', now(), now()),
  ('skcase_${RUN_SUFFIX}_2', '${SKILL_ID}', '${VERSION_ID}', 'e2e case 2', '{"prompt":"${UNIQUE_HINT} two"}'::jsonb, '[{"element_type":"image_ref"}]'::jsonb, 'active', 'adm_root', 'adm_root', 'adm_root', now(), now()),
  ('skcase_${RUN_SUFFIX}_3', '${SKILL_ID}', '${VERSION_ID}', 'e2e case 3', '{"prompt":"${UNIQUE_HINT} three"}'::jsonb, '[{"element_type":"image_ref"}]'::jsonb, 'active', 'adm_root', 'adm_root', 'adm_root', now(), now())
ON CONFLICT DO NOTHING;
SQL
api POST "$BUSINESS_BASE_URL/api/admin/skills/system/$SKILL_ID/publish" "$ADMIN_TOKEN" "e2e-publish-${RUN_SUFFIX}" "$(jq -nc --arg version_id "$VERSION_ID" '{version_id:$version_id}')" >/dev/null

echo "[6/8] assert business DB persisted admin runtime config"
BUSINESS_RUNTIME_ROW=$(psql_business -Atc "
SELECT jsonb_build_object(
  'provider_id', p.id,
  'provider_code', p.provider_code,
  'provider_status', p.status,
  'provider_timeout_ms', (p.config_json->>'timeout_ms')::int,
  'model_id', m.id,
  'model_code', m.model_code,
  'model_status', m.status,
  'model_resource_type', m.resource_type,
  'model_route_marker', m.route_config_json->>'e2e_marker',
  'default_model_id', d.model_id,
  'skill_id', s.id,
  'skill_status', s.status,
  'published_version_id', s.published_version_id,
  'skill_keyword', s.route_hints_json->>'keyword',
  'version_status', sv.status,
  'output_element_count', (
    SELECT count(*) FROM skill_output_element_schemas oe
    WHERE oe.skill_id = s.id AND oe.version_id = sv.id AND oe.element_type = 'image_ref'
      AND oe.use_draft = true AND oe.use_final = true AND oe.display_slot = 'asset_detail'
  ),
  'active_test_case_count', (
    SELECT count(*) FROM skill_test_cases tc
    WHERE tc.skill_id = s.id AND tc.version_id = sv.id AND tc.status = 'active'
  )
)::text
FROM model_providers p
JOIN models m ON m.provider_id = p.id
JOIN default_models d ON d.model_id = m.id AND d.resource_type = '${MODEL_RESOURCE_TYPE}' AND d.scope = 'global' AND d.status = 'active'
JOIN skills s ON s.id = '${SKILL_ID}'
JOIN skill_versions sv ON sv.id = '${VERSION_ID}' AND sv.skill_id = s.id
WHERE p.id = '${PROVIDER_ID}' AND m.id = '${MODEL_ID}';
")
echo "$BUSINESS_RUNTIME_ROW" | jq -e \
  --arg provider_id "$PROVIDER_ID" \
  --arg provider_code "$PROVIDER_CODE" \
  --arg model_id "$MODEL_ID" \
  --arg model_code "$MODEL_CODE" \
  --arg skill_id "$SKILL_ID" \
  --arg version_id "$VERSION_ID" \
  --arg unique_hint "$UNIQUE_HINT" \
  'select(
    .provider_id == $provider_id and
    .provider_code == $provider_code and
    .provider_status == "active" and
    .provider_timeout_ms == 12345 and
    .model_id == $model_id and
    .model_code == $model_code and
    .model_status == "active" and
    .model_resource_type == "image" and
    .model_route_marker == "runtime_config" and
    .default_model_id == $model_id and
    .skill_id == $skill_id and
    .skill_status == "published" and
    .published_version_id == $version_id and
    .skill_keyword == $unique_hint and
    .version_status == "published" and
    .output_element_count == 1 and
    .active_test_case_count == 3
  )' >/dev/null

echo "[7/8] create user agent session and run prompt"
SESSION=$(api POST "$AGENT_BASE_URL/api/agent/sessions" "$USER_TOKEN" "e2e-agent-session-${RUN_SUFFIX}" "$(jq -nc --arg project_id "$PROJECT_ID" '{project_id:$project_id,initial_title:"Agent E2E Runtime Config"}')")
SESSION_ID=$(echo "$SESSION" | json_field '.session_id')
PROMPT="请使用 ${UNIQUE_HINT} 帮我生成一张可爱的产品图方案"
RUN=$(api POST "$AGENT_BASE_URL/api/agent/runs" "$USER_TOKEN" "e2e-agent-run-${RUN_SUFFIX}" "$(jq -nc \
  --arg session_id "$SESSION_ID" \
  --arg project_id "$PROJECT_ID" \
  --arg prompt "$PROMPT" \
  '{session_id:$session_id,project_id:$project_id,user_input:{client_message_id:"cm_e2e",content_type:"text",text:$prompt}}')")
RUN_ID=$(echo "$RUN" | json_field '.run_id')

echo "[8/8] assert runtime events and persisted snapshots"
EVENTS=$(api GET "$AGENT_BASE_URL/api/agent/runs/$RUN_ID/events?after_sequence=0&limit=100" "$USER_TOKEN")
echo "$EVENTS" | jq -e --arg skill_id "$SKILL_ID" --arg unique_hint "$UNIQUE_HINT" '.events[] | select(.type=="agent.skill.selected" and .payload.skill_id==$skill_id and .payload.matched_reason=="route_hint:keyword" and .payload.route_hints.keyword==$unique_hint)' >/dev/null
echo "$EVENTS" | jq -e --arg model_id "$MODEL_ID" '.events[] | select(.type=="generation.progress" and .payload.status=="model_snapshot_resolved" and .payload.model_id==$model_id)' >/dev/null
echo "$EVENTS" | jq -e '([.events[].payload | tostring | contains("provider_runtime_ref")] | any) | not' >/dev/null

RUN_ROW=$(psql_agent -Atc "SELECT skill_selection::text || E'\n' || model_selection_snapshot::text FROM agent_runs WHERE id='${RUN_ID}';")
echo "$RUN_ROW" | jq -e --arg skill_id "$SKILL_ID" 'select(.skill_id==$skill_id and .output_elements_count==1 and .output_elements[0].element_type=="image_ref")' >/dev/null
echo "$RUN_ROW" | tail -n 1 | jq -e --arg model_id "$MODEL_ID" --arg provider_ref "${PROVIDER_CODE}:${MODEL_CODE}" 'select(.model_id==$model_id and .provider_runtime_ref==$provider_ref and .timeout_ms==12345 and .runtime_parameters.e2e_marker=="runtime_config")' >/dev/null

INTERRUPT_PAYLOAD=$(psql_agent -Atc "SELECT confirmation_payload::text FROM agent_interrupts WHERE run_id='${RUN_ID}' AND interrupt_type='credit_generation_confirmation' ORDER BY created_at DESC LIMIT 1;")
echo "$INTERRUPT_PAYLOAD" | jq -e --arg model_id "$MODEL_ID" --arg provider_ref "${PROVIDER_CODE}:${MODEL_CODE}" 'select(.model_snapshot.model_id==$model_id and .model_snapshot.provider_runtime_ref==$provider_ref and .output_elements[0].element_type=="image_ref")' >/dev/null

echo "[done] success"
jq -nc \
  --arg run_id "$RUN_ID" \
  --arg session_id "$SESSION_ID" \
  --arg skill_id "$SKILL_ID" \
  --arg version_id "$VERSION_ID" \
  --arg model_id "$MODEL_ID" \
  --arg provider_id "$PROVIDER_ID" \
  --arg unique_hint "$UNIQUE_HINT" \
  --arg trace_id "$TRACE_ID" \
  '{run_id:$run_id,session_id:$session_id,skill_id:$skill_id,version_id:$version_id,model_id:$model_id,provider_id:$provider_id,unique_hint:$unique_hint,trace_id:$trace_id}'
