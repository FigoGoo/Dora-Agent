#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOROOT="${GOROOT:-/Users/figo/sdk/go1.26.3}"
export GOPATH="${GOPATH:-/Users/figo/go}"
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

echo "== M1 baseline =="
scripts/validate-m1.sh

echo "== M2 gofmt dry check =="
unformatted="$(find services internal tests -name '*.go' -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  echo "gofmt required" >&2
  exit 1
fi

echo "== M2 semantic source checks =="
python3 - <<'PY'
from pathlib import Path
import json

def fail(msg: str) -> None:
    raise SystemExit(msg)

business_router = Path("services/business/internal/transport/http/handlers_m2.go").read_text()
agent_router = Path("services/agent/internal/api/http/workbench_handlers.go").read_text()
business_rpc = Path("services/business/internal/transport/rpc/handlers.go").read_text()
agent_app = Path("services/agent/internal/application/workbench/app.go").read_text()
business_migration = Path("db/migrations/iterations/2026-06-27-business-core/business/0013_m2_identity_project_alignment.up.sql").read_text()

for route in [
    "/api/auth/register",
    "/api/auth/login",
    "/api/account/current-space",
    "/api/account/switch-identity",
    "/api/enterprise/register",
    "/api/enterprise/members",
    "/api/admin/auth/login",
    "/api/admin/auth/rotate-password",
    "/api/admin/users/:user_id/status/confirm",
    "/api/projects",
    "/api/projects/:project_id/archive",
]:
    if route not in business_router:
        fail(f"business M2 route missing: {route}")

for route in [
    "/api/agent/sessions",
    "/api/agent/sessions/:session_id/messages",
    "/api/agent/runs",
    "/api/agent/runs/:run_id/events",
    "/api/agent/runs/:run_id/cancel",
    "/api/agent/runs/:run_id/snapshot",
]:
    if route not in agent_router:
        fail(f"agent M2 route missing: {route}")

for needle in [
    "h.Account.ResolveCurrentSpaceContext",
    "h.Project.CheckProjectAccess",
    "CodeProjectArchived",
    "CodeCrossSpaceDenied",
]:
    if needle not in business_rpc and needle not in Path("services/business/internal/pkg/errors/error.go").read_text():
        fail(f"business RPC/error semantic missing: {needle}")

for needle in [
    "ResolveCurrentSpaceContext",
    "CheckProjectAccess",
    "ProjectAccessPurpose_CONTINUE_CREATION",
    "ProjectAccessPurpose_VIEW",
    "readonly = \"project_archived\"",
    "CountActiveRuns",
]:
    if needle not in agent_app:
        fail(f"agent session/run project gate missing: {needle}")

for needle in [
    "email_hash",
    "phone_hash",
    "session_token_hash",
    "csrf_token_hash",
    "creative_allowed",
    "last_activity_at",
    "source_session_id",
]:
    if needle not in business_migration:
        fail(f"M2 alignment migration missing {needle}")

if "FOREIGN KEY" in business_migration or "REFERENCES" in business_migration:
    fail("M2 migration contains database-level external constraint keyword")

account = json.loads(Path("tests/contract/fixtures/business-rpc/accountspace/scenarios.json").read_text())
project = json.loads(Path("tests/contract/fixtures/business-rpc/project/scenarios.json").read_text())
account_codes = {case.get("scenario"): case.get("error", {}).get("code") for case in account["scenarios"] if "error" in case}
project_codes = {case.get("scenario"): case.get("error", {}).get("code") for case in project["scenarios"] if "error" in case}
expected = {
    ("account", "unauthenticated_error"): "UNAUTHENTICATED",
    ("account", "member_removed_error"): "PERMISSION_DENIED",
    ("account", "disabled_user_error"): "PERMISSION_DENIED",
    ("project", "archived_readonly_error"): "PROJECT_ARCHIVED",
    ("project", "cross_space_permission_denied"): "CROSS_SPACE_DENIED",
    ("project", "project_not_found_error"): "PROJECT_NOT_FOUND",
}
for (domain, scenario), code in expected.items():
    actual = account_codes.get(scenario) if domain == "account" else project_codes.get(scenario)
    if actual != code:
        fail(f"{domain} fixture {scenario} expected {code}, got {actual}")

print("m2 semantic source checks ok")
PY

echo "== M2 targeted tests =="
go test -count=1 ./services/business/internal/transport/http ./services/business/internal/transport/rpc ./services/agent/internal/api/http ./services/agent/internal/application/workbench

echo "== Full Go tests =="
go test -count=1 ./...

echo "== M2 external constraint scan =="
if rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services internal; then
  echo "blocked database-level external constraint keyword found" >&2
  exit 1
fi

echo "== M2 report truthfulness check =="
python3 - <<'PY'
from pathlib import Path
report = Path("tests/reports/m2-technical-baseline-report.md")
if not report.exists():
    raise SystemExit("missing M2 report")
text = report.read_text()
required = [
    "scripts/validate-m0.sh",
    "scripts/validate-m1.sh",
    "go test -count=1 ./...",
    "scripts/validate-m2.sh",
    "未执行项：无",
]
for needle in required:
    if needle not in text:
        raise SystemExit(f"M2 report missing executed command/result: {needle}")
if "未执行项通过" in text or "未执行但通过" in text:
    raise SystemExit("M2 report claims unexecuted items passed")
print("m2 report check ok")
PY

echo "M2 validation passed"
