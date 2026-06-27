#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOROOT="${GOROOT:-/Users/figo/sdk/go1.26.3}"
export GOPATH="${GOPATH:-/Users/figo/go}"
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

echo "== M2 baseline =="
scripts/validate-m2.sh

echo "== M3 gofmt dry check =="
unformatted="$(find services internal tests -name '*.go' -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  echo "gofmt required" >&2
  exit 1
fi

echo "== M3 full Go tests =="
go test -count=1 ./...

echo "== M3 SQL up/down pair check =="
python3 - <<'PY'
from pathlib import Path

for up in Path("db/migrations/iterations").rglob("*.up.sql"):
    down = up.with_name(up.name.replace(".up.sql", ".down.sql"))
    if not down.exists():
        raise SystemExit(f"missing down migration for {up}")
print("sql up/down pair check ok")
PY

echo "== M3 semantic source checks =="
python3 - <<'PY'
from pathlib import Path
import json
import re
import yaml

def fail(msg: str) -> None:
    raise SystemExit(msg)

m3_rpc = {
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
for service_method in sorted(m3_rpc):
    service, method = service_method.split(".")
    pattern = rf"service\s+{service}\s*\{{(?P<body>.*?)\n\}}"
    match = re.search(pattern, idl, re.S)
    if not match or method not in match.group("body"):
        fail(f"IDL missing M3 RPC {service_method}")

business_rpc = Path("services/business/internal/transport/rpc/handlers.go").read_text()
for method in [item.split(".")[1] for item in m3_rpc]:
    needle = f'NotImplemented("{method}"'
    if needle in business_rpc:
        fail(f"M3 RPC still returns NOT_IMPLEMENTED: {method}")
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
        fail(f"agent HTTP missing M3 auth/SSE semantic: {required}")

agent_app = Path("services/agent/internal/application/workbench/app.go").read_text()
for needle in [
    "recordM3StartEvents",
    "ListRoutableSkills",
    "CheckToolExecutionPolicy",
    "ResolveDefaultModel",
    "CreateSafetyEvaluation",
    "m4.asset_credit.deferred",
]:
    if needle not in agent_app:
        fail(f"agent workbench missing M3 start-turn semantic: {needle}")

for directory in [
    "services/agent/internal/runtime/eino",
    "services/agent/internal/runtime/turnloop",
    "services/agent/internal/runtime/skill",
    "services/agent/internal/runtime/tool",
    "services/agent/internal/runtime/safety",
    "services/agent/internal/runtime/modeltool",
    "services/agent/internal/runtime/skilltest",
    "services/agent/internal/events/stream",
]:
    if not Path(directory).is_dir():
        fail(f"missing M3 agent runtime directory {directory}")

if "github.com/cloudwego/eino v0.9.10" not in Path("go.mod").read_text():
    fail("go.mod missing pinned github.com/cloudwego/eino v0.9.10")

business_openapi = yaml.safe_load(Path("api/openapi/business-api.yaml").read_text())
business_router = Path("services/business/internal/transport/http/handlers_m3.go").read_text()
m3_routes = [
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
for method, openapi_path, gin_path in m3_routes:
    if method not in business_openapi["paths"].get(openapi_path, {}):
        fail(f"business OpenAPI missing M3 route {method.upper()} {openapi_path}")
    if gin_path not in business_router:
        fail(f"business Gin missing M3 route {gin_path}")

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
missing = m3_rpc - method_seen
if missing:
    fail(f"M3 RPC fixture missing methods: {sorted(missing)}")
for scenario in [
    "resolve_auth_context_from_token_success",
    "resolve_auth_context_from_token_cross_space_denied",
    "published_routable_success",
    "requires_confirmation_success",
    "default_model_success",
    "pricing_snapshot_missing_error",
]:
    if scenario not in scenario_seen:
        fail(f"M3 fixture missing scenario {scenario}")

seed = Path("tests/business/seed/business_core_seed.sql").read_text()
if seed.count("'aet_") < 14:
    fail("seed must contain at least 14 asset element types")
if seed.count("'skcase_storyboard_") < 3:
    fail("seed must contain at least 3 storyboard skill test cases")
for needle in ["model_providers", "tool_policies", "skills", "skill_test_cases", "asset_element_types"]:
    if needle not in seed:
        fail(f"seed missing {needle}")

print("m3 semantic source checks ok")
PY

echo "== M3 contract fixture validation =="
python3 tests/contract/validate_fixtures.py

echo "== M3 external constraint scan =="
if rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services internal; then
  echo "blocked database-level external constraint keyword found" >&2
  exit 1
fi

echo "== M3 report truthfulness check =="
python3 - <<'PY'
from pathlib import Path

report = Path("tests/reports/m3-technical-baseline-report.md")
if not report.exists():
    raise SystemExit("missing M3 report")
text = report.read_text()
required = [
    "scripts/validate-m0.sh",
    "scripts/validate-m1.sh",
    "scripts/validate-m2.sh",
    "go test -count=1 ./...",
    "scripts/validate-m3.sh",
    "ResolveAuthContextFromToken",
    "SkillCatalogService.ListRoutableSkills",
    "ToolCapabilityService.CheckToolExecutionPolicy",
    "ModelConfigService.ResolveGenerationModelSnapshot",
    "PlatformDictionaryService.ListAssetElementTypes",
    "未执行项：无（M3 范围内）",
]
for needle in required:
    if needle not in text:
        raise SystemExit(f"M3 report missing executed command/result: {needle}")
if "未执行项通过" in text or "未执行但通过" in text:
    raise SystemExit("M3 report claims unexecuted items passed")
print("m3 report check ok")
PY

echo "M3 validation passed"
