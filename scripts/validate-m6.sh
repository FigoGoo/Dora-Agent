#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOROOT="${GOROOT:-/Users/figo/sdk/go1.26.3}"
export GOPATH="${GOPATH:-/Users/figo/go}"
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

echo "== M5 baseline =="
scripts/validate-m5.sh

echo "== M6 Go toolchain =="
go version

echo "== M6 gofmt dry check =="
unformatted="$(find services internal tests -name '*.go' -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  echo "gofmt required" >&2
  exit 1
fi

echo "== M6 full Go tests =="
go test -count=1 ./...

echo "== M6 SQL up/down pair check =="
python3 - <<'PY'
from pathlib import Path

for up in Path("db/migrations/iterations").rglob("*.up.sql"):
    down = up.with_name(up.name.replace(".up.sql", ".down.sql"))
    if not down.exists():
        raise SystemExit(f"missing down migration for {up}")
print("sql up/down pair check ok")
PY

echo "== M6 service acceptance semantic checks =="
python3 - <<'PY'
from pathlib import Path
import json
import re
import yaml


def fail(msg: str) -> None:
    raise SystemExit(msg)


expected = {
    "AccountSpaceService": {"ResolveCurrentSpaceContext", "ResolveAuthContextFromToken"},
    "EnterpriseService": {"PreviewTransferOwner", "ConfirmTransferOwner"},
    "AdminService": {"CreateAdmin", "DisableAdmin"},
    "UserAdminService": {"PreviewSetUserStatus", "ConfirmSetUserStatus"},
    "ProjectService": {"CheckProjectAccess", "CreateProject", "UpdateProjectTitle"},
    "ProjectAssetService": {"AttachAssetToProject"},
    "AssetService": {"CreateUploadIntent", "ConfirmUploadedAsset", "BatchCheckAssetAccess", "PrepareGeneratedAssetObjects"},
    "CreditService": {"EstimateGenerationCredits", "EstimateToolCredits", "FreezeCredits", "ChargeToolUsageCredits", "ReleaseFrozenCredits"},
    "AssetCreditCommitService": {"CommitGeneratedAssetAndCharge"},
    "SkillCatalogService": {"ListRoutableSkills", "GetPublishedSkillSpec", "GetReviewCandidateSkillSpec", "SaveSkillTestResult"},
    "ToolCapabilityService": {"CheckToolExecutionPolicy"},
    "ModelConfigService": {"ListAvailableGenerationModels", "ResolveDefaultModel", "ResolveGenerationModelSnapshot"},
    "PlatformDictionaryService": {"ListAssetElementTypes"},
    "WorkService": {"CreateWork"},
    "WorkShareService": {"PreviewShareWork", "ConfirmShareWork"},
    "FeaturedWorkAdminService": {"PreviewTakeDownWork", "ConfirmTakeDownWork"},
    "PublicContentService": {"ListPublicWorks", "GetPublicWork"},
    "NotificationService": {"CreateNotification", "ListNotifications", "GetUnreadCount", "MarkNotificationRead", "MarkAllNotificationsRead"},
}

idl = Path("api/thrift/business_agent_service.thrift").read_text()
actual: dict[str, set[str]] = {}
for service_match in re.finditer(r"service\s+(\w+)\s*\{(?P<body>.*?)\n\}", idl, re.S):
    service = service_match.group(1)
    methods = set()
    for line in service_match.group("body").splitlines():
        method_match = re.search(r"\b\w+\s+(\w+)\s*\(", line)
        if method_match:
            methods.add(method_match.group(1))
    actual[service] = methods
if actual != expected:
    missing_services = set(expected) - set(actual)
    extra_services = set(actual) - set(expected)
    detail = []
    if missing_services:
        detail.append(f"missing services {sorted(missing_services)}")
    if extra_services:
        detail.append(f"extra services {sorted(extra_services)}")
    for service in sorted(set(expected) & set(actual)):
        if actual[service] != expected[service]:
            detail.append(
                f"{service}: missing {sorted(expected[service]-actual[service])}, extra {sorted(actual[service]-expected[service])}"
            )
    fail("Thrift RPC service ledger mismatch: " + "; ".join(detail))

service_dirs = {
    "AccountSpaceService": "accountspaceservice",
    "EnterpriseService": "enterpriseservice",
    "AdminService": "adminservice",
    "UserAdminService": "useradminservice",
    "ProjectService": "projectservice",
    "ProjectAssetService": "projectassetservice",
    "AssetService": "assetservice",
    "CreditService": "creditservice",
    "AssetCreditCommitService": "assetcreditcommitservice",
    "SkillCatalogService": "skillcatalogservice",
    "ToolCapabilityService": "toolcapabilityservice",
    "ModelConfigService": "modelconfigservice",
    "PlatformDictionaryService": "platformdictionaryservice",
    "WorkService": "workservice",
    "WorkShareService": "workshareservice",
    "FeaturedWorkAdminService": "featuredworkadminservice",
    "PublicContentService": "publiccontentservice",
    "NotificationService": "notificationservice",
}
for service, directory in service_dirs.items():
    path = Path("kitex_gen/dora/api/businessagent") / directory
    if not path.is_dir():
        fail(f"Kitex generated service dir missing for {service}: {path}")
    if not (path / f"{directory}.go").exists():
        fail(f"Kitex generated service file missing for {service}: {path / (directory + '.go')}")

register = Path("services/business/internal/transport/rpc/register.go").read_text()
for service, directory in service_dirs.items():
    if f"{directory}.RegisterService" not in register:
        fail(f"RegisterAll missing {service}")

handler_text = "\n".join(path.read_text() for path in Path("services/business/internal/transport/rpc").glob("handlers*.go"))
for methods in expected.values():
    for method in methods:
        if f"func (h *Handler) {method}(" not in handler_text and f"func (h *Handler) {method}_(" not in handler_text:
            fail(f"business RPC handler missing method {method}")

bootstrap = Path("services/business/internal/bootstrap/app.go").read_text()
for needle in [
    "accountApp",
    "projectApp",
    "adminApp",
    "modelApp",
    "toolApp",
    "skillApp",
    "dictionaryApp",
    "creditApp",
    "assetApp",
    "commitApp",
    "workApp",
    "notificationApp",
]:
    if needle not in bootstrap:
        fail(f"business bootstrap missing RPC app wiring {needle}")

if "LoginIdentityType_ADMIN" not in handler_text or "adminAuthFromRPC" not in handler_text:
    fail("backend/admin RPC handlers must derive and check admin identity")

openapi_text = Path("api/openapi/business-api.yaml").read_text()
agent_openapi_text = Path("api/openapi/agent-workbench.yaml").read_text()
for token in ["JsonBody", "ApiResponse", "PageResponse"]:
    if token in openapi_text or token in agent_openapi_text:
        fail(f"OpenAPI still contains generic placeholder {token}")
if "additionalProperties: true" in openapi_text or "additionalProperties: true" in agent_openapi_text:
    fail("OpenAPI still contains additionalProperties: true generic pockets")

schema = json.loads(Path("api/agui/agent-workbench-events.schema.json").read_text())
canonical_events = {
    item.get("if", {}).get("properties", {}).get("type", {}).get("const")
    for item in schema.get("allOf", [])
}
canonical_events.discard(None)
agent_app = Path("services/agent/internal/application/workbench/app.go").read_text()
agent_gateway = Path("services/agent/internal/infra/rpc/business_gateway.go").read_text()
agent_static_gateway = Path("services/agent/internal/application/workbench/fake_gateway.go").read_text()
agent_facing_methods = {
    "ListAvailableGenerationModels",
    "GetReviewCandidateSkillSpec",
    "EstimateToolCredits",
    "ChargeToolUsageCredits",
}
for method in sorted(agent_facing_methods):
    if method not in agent_app:
        fail(f"Agent application does not consume required RPC client method {method}")
    if f"func (g *BusinessGateway) {method}(" not in agent_gateway:
        fail(f"Agent concrete BusinessGateway missing method {method}")
    if f"func (g StaticGateway) {method}(" not in agent_static_gateway:
        fail(f"Agent StaticGateway missing method {method}")

runtime_events = set(re.findall(r'appendRunEvent\([^\\n]+?\"([a-z][a-z0-9]*(?:\\.[a-z][a-z0-9]*)+)\"', agent_app))
runtime_events |= set(re.findall(r'Type:\s*\"([a-z][a-z0-9]*(?:\\.[a-z][a-z0-9]*)+)\"', agent_app))
unknown = runtime_events - canonical_events
if unknown:
    fail(f"agent runtime writes non-canonical AG-UI events: {sorted(unknown)}")
for event_type in ["agent.run.started", "agent.run.completed", "agent.run.failed", "agent.run.cancelled", "process.snapshot.saved"]:
    if event_type not in canonical_events:
        fail(f"AG-UI schema missing service-level event {event_type}")

def production_go_text(root: str) -> str:
    return "\n".join(
        path.read_text()
        for path in Path(root).rglob("*.go")
        if not path.name.endswith("_test.go")
    )


agent_text = production_go_text("services/agent")
for forbidden in [
    "business_users",
    "business_spaces",
    "credit_accounts",
    "work_public_snapshots",
    "notification_create_failures",
    "func (Work) TableName",
    "func (Notification) TableName",
]:
    if forbidden in agent_text:
        fail(f"Agent service copies or persists business fact {forbidden}")
business_text = production_go_text("services/business")
for forbidden in [
    "agent_sessions",
    "agent_runs",
    "agent_messages",
    "agent_events",
    "agent_interrupts",
    "agent_artifacts",
]:
    if forbidden in business_text:
        fail(f"Business service copies or persists Agent runtime fact {forbidden}")

seed = Path("tests/business/seed/business_core_seed.sql").read_text()
for keyword in [
    "adm_root",
    "ent_1001",
    "prj_active_1001",
    "mdl_seed_image",
    "tool_web_fetch",
    "sk_seed_storyboard",
    "ca_personal_1001",
    "ast_generated_1001",
    "wrk_seed_public",
    "ntf_skill_review_001",
]:
    if keyword not in seed:
        fail(f"business seed missing service-level fixture keyword {keyword}")

openapi = yaml.safe_load(openapi_text)
paths = openapi.get("paths", {})
for path, method in [
    ("/api/admin/users/{user_id}/status/preview", "post"),
    ("/api/admin/users/{user_id}/status/confirm", "post"),
    ("/api/projects", "post"),
    ("/api/projects/{project_id}", "patch"),
    ("/api/assets/upload-intents", "post"),
    ("/api/works/{work_id}/share/preview", "post"),
    ("/api/works/{work_id}/share/confirm", "post"),
    ("/api/admin/works/public/{public_work_id}/take-down/preview", "post"),
    ("/api/admin/works/public/{public_work_id}/take-down/confirm", "post"),
    ("/api/notifications", "get"),
    ("/api/notifications/{notification_id}/read", "post"),
]:
    if method not in paths.get(path, {}):
        fail(f"OpenAPI missing M6 route {method.upper()} {path}")

report = Path("tests/reports/m6-service-acceptance-report.md")
if not report.exists():
    fail("missing M6 service acceptance report")
report_text = report.read_text()
for ref in re.findall(r"`(code-plan/[^`]+?\\.md)`", report_text):
    if not Path(ref).exists():
        fail(f"M6 report references missing fact-source file: {ref}")
for forbidden in ["阻塞问题：无", "未执行项：无", "smoke 通过", "mock-only"]:
    if forbidden in report_text:
        fail(f"M6 report contains over-optimistic phrase: {forbidden}")
for required in ["go test -count=1 ./...", "scripts/validate-m6.sh", "RPC", "HTTP", "AG-UI", "DB"]:
    if required not in report_text:
        fail(f"M6 report missing executed evidence section {required}")

print("M6 service acceptance semantic checks ok")
PY

echo "== M6 AG-UI fixtures =="
python3 tests/agent/agui/validate_fixtures.py

echo "== M6 contract fixtures =="
python3 tests/contract/validate_fixtures.py

echo "== M6 service e2e =="
test -d tests/e2e/service
python3 tests/e2e/service/validate_m6_service_e2e.py

echo "== M6 no database-level FK =="
if rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services; then
  echo "database-level foreign key/reference detected" >&2
  exit 1
fi

echo "M6 service acceptance baseline passed"
