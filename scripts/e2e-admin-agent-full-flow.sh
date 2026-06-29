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
TRACE_ID="e2e-admin-agent-full-${RUN_SUFFIX}"
UNIQUE_HINT="agent-full-flow-${RUN_SUFFIX}"
TOOL_NAME="storyboard_extract_${RUN_SUFFIX}"
TOOL_TYPE="builtin"
TOOL_KEY="${TOOL_NAME}:${TOOL_TYPE}"
PROVIDER_CODE="full_flow_provider_${RUN_SUFFIX}"
MODEL_CODE="full-flow-image-${RUN_SUFFIX}"
SKILL_KEY="full_flow_skill_${RUN_SUFFIX}"
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

echo "[1/9] health checks"
curl -fsS "$BUSINESS_BASE_URL/readyz" >/dev/null
curl -fsS "$AGENT_BASE_URL/readyz" >/dev/null

echo "[2/9] login admin and user"
ADMIN_TOKEN=$(api POST "$BUSINESS_BASE_URL/api/admin/auth/login" "" "" "{\"account\":\"$ADMIN_ACCOUNT\",\"password\":\"$ADMIN_PASSWORD\"}" | json_field '.data.access_token')
USER_LOGIN=$(api POST "$BUSINESS_BASE_URL/api/auth/login" "" "" "{\"login_type\":\"personal\",\"account\":\"$USER_ACCOUNT\",\"password\":\"$USER_PASSWORD\"}")
USER_TOKEN=$(echo "$USER_LOGIN" | json_field '.data.access_token')

echo "[3/9] register tool in admin"
TOOL_INPUT_SCHEMA='{"type":"object","properties":{"prompt":{"type":"string"}}}'
TOOL_OUTPUT_SCHEMA='{"type":"object","properties":{"shots":{"type":"array"}}}'
TOOL=$(api POST "$BUSINESS_BASE_URL/api/admin/tools" "$ADMIN_TOKEN" "e2e-tool-${RUN_SUFFIX}" "$(jq -nc \
  --arg tool_name "$TOOL_NAME" \
  --arg display_name "分镜提取 ${RUN_SUFFIX}" \
  --arg reason "E2E 后台新增 Tool 并验证 Agent prompt 消费" \
  --arg input_schema "$TOOL_INPUT_SCHEMA" \
  --arg output_schema "$TOOL_OUTPUT_SCHEMA" \
  '{tool_name:$tool_name,tool_type:"builtin",display_name:$display_name,description:"从复杂创作提示词中提取镜头、场景、主角和素材清单，供系统 Skill 生成图片资产前使用。",status:"active",version:"1.0.0",input_schema_json:$input_schema,output_schema_json:$output_schema,allowed:true,risk_level:"medium",requires_confirmation:false,timeout_ms:47000,retry_policy:{max_retries:"1"},cancel_policy:{cancelable:"true"},charge_mode:"per_call",billing_unit:"call",unit_points:0,free_quota:0,min_charge_points:0,reason:$reason}')" )
echo "$TOOL" | jq -e --arg tool_key "$TOOL_KEY" '.data.tool_key == $tool_key and .data.allowed == true and .data.requires_confirmation == false and .data.timeout_ms == 47000' >/dev/null

echo "[4/9] create provider, model, and set image default"
read -r ORIGINAL_DEFAULT_MODEL ORIGINAL_DEFAULT_PRICE < <(psql_business -Atc "SELECT model_id, pricing_snapshot_id FROM default_models WHERE resource_type='${MODEL_RESOURCE_TYPE}' AND scope='global' AND status='active' LIMIT 1;" | awk -F'|' '{print $1, $2}')
PROVIDER=$(api POST "$BUSINESS_BASE_URL/api/admin/models/providers" "$ADMIN_TOKEN" "e2e-provider-${RUN_SUFFIX}" "$(jq -nc \
  --arg provider_code "$PROVIDER_CODE" \
  --arg provider_name "全链路 E2E 模型供应商 $RUN_SUFFIX" \
  '{provider_code:$provider_code,provider_name:$provider_name,provider_type:"openai_compatible",status:"active",base_url:"http://127.0.0.1:19999/v1",secret_key_ref:"secret/e2e/full-flow",config:{timeout_ms:23456,scenario:"admin_agent_full_flow"}}')")
PROVIDER_ID=$(echo "$PROVIDER" | json_field '.data.provider_id')
MODEL=$(api POST "$BUSINESS_BASE_URL/api/admin/models" "$ADMIN_TOKEN" "e2e-model-${RUN_SUFFIX}" "$(jq -nc \
  --arg provider_id "$PROVIDER_ID" \
  --arg model_code "$MODEL_CODE" \
  --arg display_name "全链路 E2E 图片模型 $RUN_SUFFIX" \
  '{provider_id:$provider_id,model_code:$model_code,display_name:$display_name,resource_type:"image",billing_unit:"asset",unit_points:1,min_charge_points:1,status:"active",capability_tags:["e2e","full_flow","image"],route_config:{e2e_marker:"admin_agent_full_flow",quality:"storyboard",scenario:"complex_prompt"}}')")
MODEL_ID=$(echo "$MODEL" | json_field '.data.model_id')
PRICING_SNAPSHOT_ID=$(echo "$MODEL" | json_field '.data.pricing_snapshot_id')
api POST "$BUSINESS_BASE_URL/api/admin/models/default" "$ADMIN_TOKEN" "e2e-default-${RUN_SUFFIX}" "$(jq -nc --arg model_id "$MODEL_ID" --arg pricing_snapshot_id "$PRICING_SNAPSHOT_ID" '{resource_type:"image",model_id:$model_id,pricing_snapshot_id:$pricing_snapshot_id}')" >/dev/null

echo "[5/9] create system skill with markdown tool ref and output_elements"
OUTPUT_ELEMENT_SCHEMA='{"type":"object","properties":{"asset_id":{"type":"string"}}}'
SKILL_MARKDOWN=$(jq -Rs . <<EOF
# 全链路故事板图片 Skill <名称>

## 说明 <说明>

当用户需要把复杂产品故事、镜头脚本或多素材创意转换为可生成图片资产的故事板方案时触发。

## 调用规则 <调用规则>

用户提示词包含 ${UNIQUE_HINT} 时必须触发。

## 输入 <输入>

读取用户的目标、受众、产品卖点、视觉风格和输出比例；缺少关键信息时先给出可确认草稿。

## 计划 <计划>

1. 用分镜 Tool 提取镜头、角色、场景和素材清单。
2. 生成 3 段故事板草稿。
3. 调用默认图片模型准备最终图片资产。

## 工具引用 <工具引用>

<tool id="${TOOL_KEY}">分镜提取</tool>

## AG-UI 元素引用 <AG-UI元素引用>

对话框内：
<agui id="confirm_card">确认卡片</agui>

对话框外：
<agui id="storyboard_panel">故事板面板</agui>
<agui id="asset_panel">资产面板</agui>

## 结果输出 <结果输出>

输出一张图片资产，并保留草稿和最终产物结构，供用户确认后进入资产提交。
EOF
)
SKILL=$(api POST "$BUSINESS_BASE_URL/api/admin/skills/system" "$ADMIN_TOKEN" "e2e-skill-${RUN_SUFFIX}" "$(jq -nc \
  --arg skill_key "$SKILL_KEY" \
  --arg skill_name "全链路故事板图片 Skill $RUN_SUFFIX" \
  --arg hint "$UNIQUE_HINT" \
  --arg output_element_schema "$OUTPUT_ELEMENT_SCHEMA" \
  --argjson skill_markdown "$SKILL_MARKDOWN" \
  '{skill_key:$skill_key,skill_name:$skill_name,skill_tags:["e2e","full_flow"],version:"1.0.0",route_hints:{keyword:$hint},skill_markdown:$skill_markdown,confirmation_policy_json:"{\"requires_confirmation\":false}",output_elements:[{element_type:"image_ref",element_name:"最终图片资产",required:true,use_draft:true,use_final:true,editable:true,referable:true,display_order:10,display_slot:"asset_detail",schema_json:$output_element_schema}]}')")
SKILL_ID=$(echo "$SKILL" | json_field '.data.skill_id')
VERSION_ID=$(echo "$SKILL" | json_field '.data.latest_version_id')

echo "[6/9] seed required active skill test cases and publish"
psql_business -q <<SQL
INSERT INTO skill_test_cases (id, skill_id, version_id, case_name, test_input_json, expected_elements_json, status, created_by_user_id, created_by, updated_by, created_at, updated_at)
VALUES
  ('skcase_${RUN_SUFFIX}_1', '${SKILL_ID}', '${VERSION_ID}', '复杂故事板链路 case 1', '{"prompt":"${UNIQUE_HINT} premium skincare launch"}'::jsonb, '[{"element_type":"image_ref"}]'::jsonb, 'active', 'adm_root', 'adm_root', 'adm_root', now(), now()),
  ('skcase_${RUN_SUFFIX}_2', '${SKILL_ID}', '${VERSION_ID}', '复杂故事板链路 case 2', '{"prompt":"${UNIQUE_HINT} multi scene product poster"}'::jsonb, '[{"element_type":"image_ref"}]'::jsonb, 'active', 'adm_root', 'adm_root', 'adm_root', now(), now()),
  ('skcase_${RUN_SUFFIX}_3', '${SKILL_ID}', '${VERSION_ID}', '复杂故事板链路 case 3', '{"prompt":"${UNIQUE_HINT} cinematic packaging key visual"}'::jsonb, '[{"element_type":"image_ref"}]'::jsonb, 'active', 'adm_root', 'adm_root', 'adm_root', now(), now())
ON CONFLICT DO NOTHING;
SQL
api POST "$BUSINESS_BASE_URL/api/admin/skills/system/$SKILL_ID/publish" "$ADMIN_TOKEN" "e2e-publish-${RUN_SUFFIX}" "$(jq -nc --arg version_id "$VERSION_ID" '{version_id:$version_id}')" >/dev/null

echo "[7/9] assert business DB persisted all admin runtime config"
BUSINESS_RUNTIME_ROW=$(psql_business -Atc "
SELECT jsonb_build_object(
  'tool_name', td.tool_name,
  'tool_type', td.tool_type,
  'tool_status', td.status,
  'tool_allowed', tp.allowed,
  'tool_risk_level', tp.risk_level,
  'tool_requires_confirmation', tp.requires_confirmation,
  'tool_timeout_ms', tp.timeout_ms,
  'tool_unit_points', tpp.unit_points,
  'tool_register_reason', tpcr.after_json->>'reason',
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
  'default_pricing_snapshot_id', d.pricing_snapshot_id,
  'skill_id', s.id,
  'skill_status', s.status,
  'published_version_id', s.published_version_id,
  'skill_keyword', s.route_hints_json->>'keyword',
  'version_status', sv.status,
  'binding_count', (
    SELECT count(*) FROM skill_tool_bindings stb
    WHERE stb.skill_id = s.id AND stb.version_id = sv.id AND stb.tool_name = '${TOOL_NAME}' AND stb.tool_type = '${TOOL_TYPE}' AND stb.deleted_at IS NULL
  ),
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
FROM tool_definitions td
JOIN tool_policies tp ON tp.tool_name = td.tool_name AND tp.tool_type = td.tool_type AND tp.status = 'active'
JOIN tool_pricing_policies tpp ON tpp.tool_name = td.tool_name AND tpp.tool_type = td.tool_type AND tpp.status = 'active'
JOIN tool_policy_change_records tpcr ON tpcr.tool_name = td.tool_name AND tpcr.tool_type = td.tool_type AND tpcr.change_type = 'tool.register'
JOIN model_providers p ON p.id = '${PROVIDER_ID}'
JOIN models m ON m.provider_id = p.id AND m.id = '${MODEL_ID}'
JOIN default_models d ON d.model_id = m.id AND d.resource_type = '${MODEL_RESOURCE_TYPE}' AND d.scope = 'global' AND d.status = 'active'
JOIN skills s ON s.id = '${SKILL_ID}'
JOIN skill_versions sv ON sv.id = '${VERSION_ID}' AND sv.skill_id = s.id
WHERE td.tool_name = '${TOOL_NAME}' AND td.tool_type = '${TOOL_TYPE}';
")
echo "$BUSINESS_RUNTIME_ROW" | jq -e \
  --arg tool_name "$TOOL_NAME" \
  --arg provider_id "$PROVIDER_ID" \
  --arg provider_code "$PROVIDER_CODE" \
  --arg model_id "$MODEL_ID" \
  --arg model_code "$MODEL_CODE" \
  --arg pricing_snapshot_id "$PRICING_SNAPSHOT_ID" \
  --arg skill_id "$SKILL_ID" \
  --arg version_id "$VERSION_ID" \
  --arg unique_hint "$UNIQUE_HINT" \
  'select(
    .tool_name == $tool_name and
    .tool_type == "builtin" and
    .tool_status == "active" and
    .tool_allowed == true and
    .tool_risk_level == "medium" and
    .tool_requires_confirmation == false and
    .tool_timeout_ms == 47000 and
    .tool_unit_points == 0 and
    .tool_register_reason == "E2E 后台新增 Tool 并验证 Agent prompt 消费" and
    .provider_id == $provider_id and
    .provider_code == $provider_code and
    .provider_status == "active" and
    .provider_timeout_ms == 23456 and
    .model_id == $model_id and
    .model_code == $model_code and
    .model_status == "active" and
    .model_resource_type == "image" and
    .model_route_marker == "admin_agent_full_flow" and
    .default_model_id == $model_id and
    .default_pricing_snapshot_id == $pricing_snapshot_id and
    .skill_id == $skill_id and
    .skill_status == "published" and
    .published_version_id == $version_id and
    .skill_keyword == $unique_hint and
    .version_status == "published" and
    .binding_count == 1 and
    .output_element_count == 1 and
    .active_test_case_count == 3
  )' >/dev/null

echo "[8/9] create user agent session and run complex prompt"
SESSION=$(api POST "$AGENT_BASE_URL/api/agent/sessions" "$USER_TOKEN" "e2e-agent-session-${RUN_SUFFIX}" "$(jq -nc --arg project_id "$PROJECT_ID" '{project_id:$project_id,initial_title:"后台配置全链路验证"}')")
SESSION_ID=$(echo "$SESSION" | json_field '.session_id')
PROMPT="请用 ${UNIQUE_HINT} 帮我做一个高端护肤新品上市的复杂视觉方案：包含主视觉、三段故事板、镜头氛围、产品卖点和最终图片资产。"
RUN=$(api POST "$AGENT_BASE_URL/api/agent/runs" "$USER_TOKEN" "e2e-agent-run-${RUN_SUFFIX}" "$(jq -nc \
  --arg session_id "$SESSION_ID" \
  --arg project_id "$PROJECT_ID" \
  --arg prompt "$PROMPT" \
  '{session_id:$session_id,project_id:$project_id,user_input:{client_message_id:"cm_full_flow",content_type:"text",text:$prompt}}')")
RUN_ID=$(echo "$RUN" | json_field '.run_id')

echo "[9/9] assert runtime events and persisted consumption snapshots"
EVENTS=$(api GET "$AGENT_BASE_URL/api/agent/runs/$RUN_ID/events?after_sequence=0&limit=100" "$USER_TOKEN")
echo "$EVENTS" | jq -e --arg skill_id "$SKILL_ID" --arg unique_hint "$UNIQUE_HINT" '.events[] | select(.type=="agent.skill.selected" and .payload.skill_id==$skill_id and .payload.matched_reason=="route_hint:keyword" and .payload.route_hints.keyword==$unique_hint)' >/dev/null
echo "$EVENTS" | jq -e --arg tool_name "$TOOL_NAME" '.events[] | select(.type=="tool.call.started" and .payload.tool_name==$tool_name and .payload.tool_type=="builtin" and .payload.policy_allowed==true and .payload.requires_confirmation==false and .payload.timeout_ms==47000)' >/dev/null
echo "$EVENTS" | jq -e '.events[] | select(.type=="credits.estimated" and .payload.usage=="independent_tool" and .payload.estimate_points==0)' >/dev/null
echo "$EVENTS" | jq -e '.events[] | select(.type=="tool.call.completed" and .payload.status=="completed")' >/dev/null
echo "$EVENTS" | jq -e --arg model_id "$MODEL_ID" '.events[] | select(.type=="generation.progress" and .payload.status=="model_snapshot_resolved" and .payload.model_id==$model_id)' >/dev/null
echo "$EVENTS" | jq -e '([.events[].payload | tostring | contains("provider_runtime_ref")] | any) | not' >/dev/null

SKILL_SELECTION=$(psql_agent -Atc "SELECT skill_selection::text FROM agent_runs WHERE id='${RUN_ID}';")
MODEL_SELECTION=$(psql_agent -Atc "SELECT model_selection_snapshot::text FROM agent_runs WHERE id='${RUN_ID}';")
echo "$SKILL_SELECTION" | jq -e --arg skill_id "$SKILL_ID" 'select(.skill_id==$skill_id and .tool_refs_count==1 and .output_elements_count==1 and .output_elements[0].element_type=="image_ref")' >/dev/null
echo "$MODEL_SELECTION" | jq -e --arg model_id "$MODEL_ID" --arg provider_ref "${PROVIDER_CODE}:${MODEL_CODE}" 'select(.model_id==$model_id and .provider_runtime_ref==$provider_ref and .timeout_ms==23456 and .runtime_parameters.e2e_marker=="admin_agent_full_flow")' >/dev/null

INTERRUPT_PAYLOAD=$(psql_agent -Atc "SELECT confirmation_payload::text FROM agent_interrupts WHERE run_id='${RUN_ID}' AND interrupt_type='credit_generation_confirmation' ORDER BY created_at DESC LIMIT 1;")
echo "$INTERRUPT_PAYLOAD" | jq -e --arg model_id "$MODEL_ID" --arg provider_ref "${PROVIDER_CODE}:${MODEL_CODE}" 'select(.model_snapshot.model_id==$model_id and .model_snapshot.provider_runtime_ref==$provider_ref and .output_elements[0].element_type=="image_ref")' >/dev/null

echo "[done] success"
jq -nc \
  --arg run_id "$RUN_ID" \
  --arg session_id "$SESSION_ID" \
  --arg skill_id "$SKILL_ID" \
  --arg version_id "$VERSION_ID" \
  --arg tool_key "$TOOL_KEY" \
  --arg model_id "$MODEL_ID" \
  --arg provider_id "$PROVIDER_ID" \
  --arg unique_hint "$UNIQUE_HINT" \
  --arg trace_id "$TRACE_ID" \
  '{run_id:$run_id,session_id:$session_id,skill_id:$skill_id,version_id:$version_id,tool_key:$tool_key,model_id:$model_id,provider_id:$provider_id,unique_hint:$unique_hint,trace_id:$trace_id}'
