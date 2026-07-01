#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOROOT="${GOROOT:-/Users/figo/sdk/go1.26.3}"
export GOPATH="${GOPATH:-/Users/figo/go}"
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

echo "== account agent HTTP baseline =="
scripts/validate-account-agent-http.sh

echo "== catalog skill runtime gofmt dry check =="
unformatted="$(find services internal tests -name '*.go' -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  echo "gofmt required" >&2
  exit 1
fi

echo "== catalog skill runtime full Go tests =="
go test -count=1 ./...

echo "== catalog skill runtime SQL up/down pair check =="
python3 - <<'PY'
from pathlib import Path

for up in Path("db/migrations/iterations").rglob("*.up.sql"):
    down = up.with_name(up.name.replace(".up.sql", ".down.sql"))
    if not down.exists():
        raise SystemExit(f"missing down migration for {up}")
print("sql up/down pair check ok")
PY

echo "== catalog skill runtime semantic source checks =="
python3 - <<'PY'
from pathlib import Path
import json
import re
import yaml

def fail(msg: str) -> None:
    raise SystemExit(msg)

catalog_runtime_rpc = {
    "AccountSpaceService.ResolveAuthContextFromToken",
    "SkillCatalogService.ListRoutableSkills",
    "SkillCatalogService.GetPublishedSkillSpec",
    "SkillCatalogService.GetReviewCandidateSkillSpec",
    "SkillCatalogService.SaveSkillTestResult",
    "ToolCapabilityService.CheckToolExecutionPolicy",
    "ModelConfigService.ListAvailableGenerationModels",
    "ModelConfigService.ResolveDefaultModel",
    "ModelConfigService.ResolveGenerationModelSnapshot",
    "PlatformDictionaryService.ListAssetElementTypes",
}

idl = Path("api/thrift/business_agent_service.thrift").read_text()
for service_method in sorted(catalog_runtime_rpc):
    service, method = service_method.split(".")
    pattern = rf"service\s+{service}\s*\{{(?P<body>.*?)\n\}}"
    match = re.search(pattern, idl, re.S)
    if not match or method not in match.group("body"):
        fail(f"IDL missing catalog skill runtime RPC {service_method}")

business_rpc = Path("services/business/internal/transport/rpc/handlers.go").read_text()
for method in [item.split(".")[1] for item in catalog_runtime_rpc]:
    needle = f'NotImplemented("{method}"'
    if needle in business_rpc:
        fail(f"catalog skill runtime RPC still returns NOT_IMPLEMENTED: {method}")
for needle in [
    "ResolveAuthContextFromToken",
    "ListRoutableSkills",
    "CheckToolExecutionPolicy",
    "ResolveDefaultModel",
    "ResolveGenerationModelSnapshot",
    "ListAssetElementTypes",
]:
    if needle not in business_rpc:
        fail(f"business RPC handler missing {needle}")

agent_gateway = Path("services/agent/internal/infra/rpc/business_gateway.go").read_text()
for needle in [
    "ResolveAuthContextFromToken",
    "skillcatalogservice.Client",
    "toolcapabilityservice.Client",
    "modelconfigservice.Client",
    "platformdictionaryservice.Client",
    "SaveSkillTestResult_",
]:
    if needle not in agent_gateway:
        fail(f"agent gateway missing {needle}")

agent_http = Path("services/agent/internal/api/http/workbench_handlers.go").read_text()
for forbidden in ["X-Actor-User-Id is required", "X-Space-Id is required"]:
    if forbidden in agent_http:
        fail(f"agent auth still trusts client identity header: {forbidden}")
for required in ["Authorization is required", "ResolveAuthContextFromToken", "text/event-stream", "heartbeat"]:
    if required not in agent_http:
        fail(f"agent HTTP missing catalog skill runtime auth/SSE semantic: {required}")

agent_app = Path("services/agent/internal/application/workbench/app.go").read_text()
for needle in [
    "recordSkillRuntimeStartEvents",
    "ListRoutableSkills",
    "CheckToolExecutionPolicy",
    "ResolveDefaultModel",
    "CreateSafetyEvaluation",
    "generation.progress",
    "safety.prompt.evaluating",
    "safety.prompt.evaluated",
]:
    if needle not in agent_app:
        fail(f"agent workbench missing catalog skill runtime start-turn semantic: {needle}")

schema = json.loads(Path("api/agui/agent-workbench-events.schema.json").read_text())
canonical_events = {
    item.get("if", {}).get("properties", {}).get("type", {}).get("const")
    for item in schema.get("allOf", [])
}
canonical_events.discard(None)
runtime_events = set(re.findall(r'appendRunEvent\([^\\n]+?\"([a-z][a-z0-9]*(?:\\.[a-z][a-z0-9]*)+)\"', agent_app))
runtime_events |= set(re.findall(r'Type:\\s*\"([a-z][a-z0-9]*(?:\\.[a-z][a-z0-9]*)+)\"', agent_app))
unknown_runtime_events = runtime_events - canonical_events
if unknown_runtime_events:
    fail(f"agent runtime writes non-canonical AG-UI events: {sorted(unknown_runtime_events)}")
for forbidden in [
    "agent.run.created",
    "tool.confirmation.required",
    "safety.evaluation.passed",
    "skill.route.",
    "model.snapshot.",
    "tool_generation.asset_credit.deferred",
]:
    if forbidden in agent_app:
        fail(f"agent runtime still writes non-canonical event semantic {forbidden}")
for field in ["SessionID", "ProjectID", "SpaceID", "ActorUserID", "Timestamp", "Component"]:
    if field not in agent_app:
        fail(f"agent EventDTO missing AG-UI top-level field {field}")
for field in ["SchemaHintJSON", "RenderHintJSON", "SchemaHintJson", "RenderHintJson"]:
    if field not in agent_app + agent_gateway:
        fail(f"agent asset element mapping missing {field}")
for field in [
    "ResourceType", "Status", "UsageStage", "DraftEnabled", "FinalEnabled", "Editable", "Referable", "RenderHint",
]:
    if field not in idl + agent_app + agent_gateway:
        fail(f"asset element DTO mapping missing catalog skill runtime field {field}")
for needle in [
    "requireViewProjectAccess(ctx, auth, session.ProjectID",
    "requireViewProjectAccess(ctx, auth, run.ProjectID",
    "ensureViewProjectAccess(access)",
]:
    if needle not in agent_app:
        fail(f"agent read/view permission semantic missing: {needle}")
for needle in [
    "validateRunInputs(req)",
    "runInputSummary(req)",
    "\"referenced_assets\"",
    "\"control_inputs\"",
    "\"safety_targets\"",
    "\"generation_plan\"",
]:
    if needle not in agent_app:
        fail(f"agent run input summary missing {needle}")
if '"provider_runtime_ref": snapshot.ProviderRuntimeRef' in agent_app:
    fail("generation.progress leaks provider_runtime_ref")
for needle in [
    "\"skill_scope\"",
    "\"matched_reason\"",
    "\"fallback_reason\"",
    "\"tool_refs_digest\"",
    "\"execution_space_id\"",
    "\"billing_credit_account_scope\"",
]:
    if needle not in agent_app:
        fail(f"agent skill_selection snapshot missing {needle}")

for directory in [
    "services/agent/internal/runtime/eino",
    "services/agent/internal/runtime/turnloop",
    "services/agent/internal/runtime/skill",
    "services/agent/internal/runtime/tool",
    "services/agent/internal/runtime/safety",
    "services/agent/internal/runtime/modeltool",
    "services/agent/internal/runtime/memory",
    "services/agent/internal/runtime/skilltest",
    "services/agent/internal/domain/event",
    "services/agent/internal/events/agui",
    "services/agent/internal/events/stream",
]:
    if not Path(directory).is_dir():
        fail(f"missing catalog skill runtime agent runtime directory {directory}")

if "github.com/cloudwego/eino v0.9.10" not in Path("go.mod").read_text():
    fail("go.mod missing pinned github.com/cloudwego/eino v0.9.10")

skill_model = Path("services/business/internal/infra/repository/businesscore/models_catalog.go").read_text()
skill_app = Path("services/business/internal/application/skillcatalog/app.go").read_text()
for needle in ["confirmation_policy_json", "ConfirmationPolicyJSON"]:
    if needle not in skill_model + skill_app:
        fail(f"skill confirmation policy not persisted/read from DB: {needle}")
if 'ConfirmationPolicyJSON: `{"requires_confirmation":false}`' in skill_app:
    fail("skill confirmation policy is still hard-coded in business application response")
if not Path("db/migrations/iterations/2026-06-27-business-core/business/0014_skill_confirmation_policy.up.sql").exists():
    fail("missing catalog skill runtime skill confirmation policy migration")
if not Path("db/migrations/iterations/2026-06-27-business-core/business/0015_skill_test_result_idempotency.up.sql").exists():
    fail("missing catalog skill runtime skill test result idempotency migration")
for needle in [
    "idempotency_key",
    "request_hash",
    "skill_test:<test_run_id>",
    "CodeSafetyEvidenceInvalid",
    "validateSkillTestSafetyEvidence",
]:
    if needle not in skill_model + skill_app:
        fail(f"skill test contract semantic missing {needle}")
if 'safetyEvidenceJSON = "{}"' in skill_app:
    fail("skill test safety evidence still defaults to loose empty JSON")

skilltest_runner = Path("services/agent/internal/runtime/skilltest/runner.go").read_text()
for needle in ["ElementTypeSpec", "StageViolations", "UnrenderableHints", "DraftEnabled", "FinalEnabled", "RenderHint"]:
    if needle not in skilltest_runner:
        fail(f"skill output validation missing {needle}")

business_openapi = yaml.safe_load(Path("api/openapi/business-api.yaml").read_text())
asset_schema = business_openapi["components"]["schemas"]["AssetElementTypeDTO"]
for field in ["resource_type", "status", "usage_stage", "draft_enabled", "final_enabled", "editable", "referable", "render_hint"]:
    if field not in asset_schema.get("properties", {}):
        fail(f"business OpenAPI AssetElementTypeDTO missing {field}")
for field in ["resource_type", "status", "usage_stage", "draft_enabled", "final_enabled", "editable", "referable"]:
    if field not in asset_schema.get("required", []):
        fail(f"business OpenAPI AssetElementTypeDTO required list missing {field}")
business_router = Path("services/business/internal/transport/http/handlers_catalog_admin.go").read_text()
catalog_runtime_routes = [
    ("get", "/api/models/generation", "/api/models/generation"),
    ("get", "/api/tools/bindable", "/api/tools/bindable"),
    ("get", "/api/skills", "/api/skills"),
    ("post", "/api/skills", "/api/skills"),
    ("get", "/api/skills/{skill_id}", "/api/skills/:skill_id"),
    ("patch", "/api/skills/{skill_id}", "/api/skills/:skill_id"),
    ("post", "/api/skills/{skill_id}/test", "/api/skills/:skill_id/test"),
    ("post", "/api/skills/{skill_id}/submit-review", "/api/skills/:skill_id/submit-review"),
    ("post", "/api/skills/{skill_id}/rollback", "/api/skills/:skill_id/rollback"),
    ("get", "/api/asset-element-types", "/api/asset-element-types"),
    ("get", "/api/admin/models/providers", "/api/admin/models/providers"),
    ("post", "/api/admin/models/providers", "/api/admin/models/providers"),
    ("patch", "/api/admin/models/providers/{provider_id}", "/api/admin/models/providers/:provider_id"),
    ("post", "/api/admin/models/providers/{provider_id}/connectivity-test", "/api/admin/models/providers/:provider_id/connectivity-test"),
    ("get", "/api/admin/models", "/api/admin/models"),
    ("post", "/api/admin/models", "/api/admin/models"),
    ("patch", "/api/admin/models/{model_id}", "/api/admin/models/:model_id"),
    ("post", "/api/admin/models/default", "/api/admin/models/default"),
    ("post", "/api/admin/models/{model_id}/status", "/api/admin/models/:model_id/status"),
    ("get", "/api/admin/tools", "/api/admin/tools"),
    ("post", "/api/admin/tools/{tool_key}/impact-preview", "/api/admin/tools/:tool_key/impact-preview"),
    ("patch", "/api/admin/tools/{tool_key}/policy", "/api/admin/tools/:tool_key/policy"),
    ("patch", "/api/admin/tools/{tool_key}/pricing-policy", "/api/admin/tools/:tool_key/pricing-policy"),
    ("post", "/api/admin/tools/{tool_key}/status", "/api/admin/tools/:tool_key/status"),
    ("put", "/api/admin/tools/{tool_key}/whitelist", "/api/admin/tools/:tool_key/whitelist"),
    ("get", "/api/admin/skills/system", "/api/admin/skills/system"),
    ("post", "/api/admin/skills/system", "/api/admin/skills/system"),
    ("post", "/api/admin/skills/system/{skill_id}/test", "/api/admin/skills/system/:skill_id/test"),
    ("post", "/api/admin/skills/system/{skill_id}/publish", "/api/admin/skills/system/:skill_id/publish"),
    ("post", "/api/admin/skills/system/{skill_id}/deprecate", "/api/admin/skills/system/:skill_id/deprecate"),
    ("get", "/api/admin/skills/reviews", "/api/admin/skills/reviews"),
    ("post", "/api/admin/skills/reviews/{review_id}/confirm", "/api/admin/skills/reviews/:review_id/confirm"),
    ("get", "/api/admin/asset-element-types", "/api/admin/asset-element-types"),
]
for method, openapi_path, gin_path in catalog_runtime_routes:
    if method not in business_openapi["paths"].get(openapi_path, {}):
        fail(f"business OpenAPI missing catalog skill runtime route {method.upper()} {openapi_path}")
    if gin_path not in business_router:
        fail(f"business Gin missing catalog skill runtime route {gin_path}")

fixtures_root = Path("tests/contract/fixtures/business-rpc")
method_seen = set()
scenario_seen = set()
for path in fixtures_root.rglob("*.json"):
    data = json.loads(path.read_text())
    items = data.get("scenarios", [data])
    for item in items:
        if not isinstance(item, dict):
            continue
        service = item.get("service")
        method = item.get("method")
        if service and method:
            method_seen.add(f"{service}.{method}")
        if item.get("scenario"):
            scenario_seen.add(item["scenario"])
missing = catalog_runtime_rpc - method_seen
if missing:
    fail(f"catalog skill runtime RPC fixture missing methods: {sorted(missing)}")
for scenario in [
    "resolve_auth_context_from_token_success",
    "resolve_auth_context_from_token_cross_space_denied",
    "published_routable_success",
    "requires_confirmation_success",
    "default_model_success",
    "pricing_snapshot_missing_error",
]:
    if scenario not in scenario_seen:
        fail(f"catalog skill runtime fixture missing scenario {scenario}")

seed = Path("tests/business/seed/business_core_seed.sql").read_text()
if seed.count("'aet_") < 14:
    fail("seed must contain at least 14 asset element types")
if seed.count("'skcase_storyboard_") < 3:
    fail("seed must contain at least 3 storyboard skill test cases")
for needle in ["model_providers", "tool_policies", "skills", "skill_test_cases", "asset_element_types"]:
    if needle not in seed:
        fail(f"seed missing {needle}")
if "confirmation_policy_json" not in seed:
    fail("seed missing skill confirmation policy baseline")

print("catalog_skill_runtime semantic source checks ok")
PY

echo "== catalog skill runtime contract fixture validation =="
python3 tests/contract/validate_fixtures.py

echo "== catalog skill runtime external constraint scan =="
if rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services internal; then
  echo "blocked database-level external constraint keyword found" >&2
  exit 1
fi

echo "== catalog skill runtime report truthfulness check =="
python3 - <<'PY'
from pathlib import Path

report = Path("tests/reports/catalog-skill-runtime-report.md")
if not report.exists():
    raise SystemExit("missing catalog skill runtime report")
text = report.read_text()
required = [
    "scripts/validate-toolchain-contract-baseline.sh",
    "scripts/validate-engineering-baseline.sh",
    "scripts/validate-account-agent-http.sh",
    "go test -count=1 ./...",
    "scripts/validate-catalog-skill-runtime.sh",
    "ResolveAuthContextFromToken",
    "SkillCatalogService.ListRoutableSkills",
    "ToolCapabilityService.CheckToolExecutionPolicy",
    "ModelConfigService.ResolveGenerationModelSnapshot",
    "PlatformDictionaryService.ListAssetElementTypes",
    "未执行项：无（catalog skill runtime 范围内）",
]
for needle in required:
    if needle not in text:
        raise SystemExit(f"catalog skill runtime report missing executed command/result: {needle}")
if "未执行项通过" in text or "未执行但通过" in text:
    raise SystemExit("catalog skill runtime report claims unexecuted items passed")
print("catalog_skill_runtime report check ok")
PY

echo "catalog skill runtime validation passed"
