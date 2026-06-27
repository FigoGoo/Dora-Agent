#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOROOT="${GOROOT:-/Users/figo/sdk/go1.26.3}"
export GOPATH="${GOPATH:-/Users/figo/go}"
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

echo "== M3 baseline =="
scripts/validate-m3.sh

echo "== M4 gofmt dry check =="
unformatted="$(find services internal tests -name '*.go' -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  echo "gofmt required" >&2
  exit 1
fi

echo "== M4 full Go tests =="
go test -count=1 ./...

echo "== M4 SQL up/down pair check =="
python3 - <<'PY'
from pathlib import Path

for up in Path("db/migrations/iterations").rglob("*.up.sql"):
    down = up.with_name(up.name.replace(".up.sql", ".down.sql"))
    if not down.exists():
        raise SystemExit(f"missing down migration for {up}")
print("sql up/down pair check ok")
PY

echo "== M4 semantic source checks =="
python3 - <<'PY'
from pathlib import Path
import json
import re
import yaml

def fail(msg: str) -> None:
    raise SystemExit(msg)

m4_rpc = {
    "AssetService.BatchCheckAssetAccess",
    "AssetService.PrepareGeneratedAssetObjects",
    "CreditService.EstimateGenerationCredits",
    "CreditService.EstimateToolCredits",
    "CreditService.FreezeCredits",
    "CreditService.ChargeToolUsageCredits",
    "CreditService.ReleaseFrozenCredits",
    "AssetCreditCommitService.CommitGeneratedAssetAndCharge",
}

idl = Path("api/thrift/business_agent_service.thrift").read_text()
for service_method in sorted(m4_rpc):
    service, method = service_method.split(".")
    match = re.search(rf"service\s+{service}\s*\{{(?P<body>.*?)\n\}}", idl, re.S)
    if not match or method not in match.group("body"):
        fail(f"IDL missing M4 RPC {service_method}")

business_rpc = Path("services/business/internal/transport/rpc/handlers.go").read_text()
for method in [item.split(".")[1] for item in m4_rpc]:
    if f'NotImplemented("{method}"' in business_rpc:
        fail(f"M4 RPC still directly returns NOT_IMPLEMENTED: {method}")
for needle in [
    "credit.App",
    "asset.App",
    "assetcommit.App",
    "EstimateGenerationCredits(ctx",
    "EstimateToolCredits(ctx",
    "FreezeCredits(ctx",
    "ChargeToolUsageCredits(ctx",
    "ReleaseFrozenCredits(ctx",
    "BatchCheckAssetAccess(ctx",
    "PrepareGeneratedAssetObjects(ctx",
    "CommitGeneratedAssetAndCharge(ctx",
]:
    if needle not in business_rpc:
        fail(f"business RPC handler missing M4 semantic {needle}")

bootstrap = Path("services/business/internal/bootstrap/app.go").read_text()
for needle in ["credit.New", "asset.New", "assetcommit.New", "creditApp", "assetApp", "commitApp"]:
    if needle not in bootstrap:
        fail(f"business bootstrap missing M4 app wiring {needle}")

openapi = yaml.safe_load(Path("api/openapi/business-api.yaml").read_text())
openapi_paths = set(openapi.get("paths", {}))
http_sources = "\n".join(path.read_text() for path in Path("services/business/internal/transport/http").glob("*.go"))
m4_http_paths = {
    "/api/credits/summary",
    "/api/credits/ledger",
    "/api/credits/redeem",
    "/api/enterprise/credits",
    "/api/enterprise/usage",
    "/api/assets",
    "/api/assets/:asset_id",
    "/api/assets/upload-intents",
    "/api/assets/upload-intents/:upload_intent_id/confirm",
    "/api/assets/upload-intents/:upload_intent_id/abort",
    "/api/assets/:asset_id/access",
    "/api/admin/credits/grants/targets",
    "/api/admin/credits/grants",
    "/api/admin/credits/codes",
    "/api/admin/credits/codes/:batch_id/disable",
    "/api/admin/credits/codes/:batch_id/export",
}
for gin_path in sorted(m4_http_paths):
    openapi_path = gin_path.replace(":asset_id", "{asset_id}").replace(":upload_intent_id", "{upload_intent_id}").replace(":batch_id", "{batch_id}")
    if openapi_path not in openapi_paths:
        fail(f"OpenAPI missing M4 HTTP path {openapi_path}")
    if f'"{gin_path}"' not in http_sources:
        fail(f"business HTTP router missing M4 path {gin_path}")

agent_gateway = Path("services/agent/internal/infra/rpc/business_gateway.go").read_text()
for needle in [
    "creditservice.Client",
    "assetservice.Client",
    "assetcreditcommitservice.Client",
    "EstimateGenerationCredits(",
    "FreezeCredits(",
    "ReleaseFrozenCredits(",
    "PrepareGeneratedAssetObjects(",
    "CommitGeneratedAssetAndCharge(",
    "BatchCheckAssetAccess(",
]:
    if needle not in agent_gateway:
        fail(f"agent RPC gateway missing M4 client semantic {needle}")

agent_app = Path("services/agent/internal/application/workbench/app.go").read_text()
for needle in [
    "ensureReferencedAssetAccess",
    "EstimateGenerationCredits",
    "createCreditConfirmationInterrupt",
    "runM4ConfirmedGeneration",
    "FreezeCredits",
    "PrepareGeneratedAssetObjects",
    "CommitGeneratedAssetAndCharge",
    "ReleaseFrozenCredits",
    "agent.run.completed",
    "credits.estimated",
    "credits.frozen",
    "credits.charged",
    "asset.save.started",
    "asset.save.completed",
    "workspace.assets.updated",
    "process.snapshot.saved",
]:
    if needle not in agent_app:
        fail(f"agent workbench missing M4 close-loop semantic {needle}")

schema = json.loads(Path("api/agui/agent-workbench-events.schema.json").read_text())
canonical_events = {
    item.get("if", {}).get("properties", {}).get("type", {}).get("const")
    for item in schema.get("allOf", [])
}
canonical_events.discard(None)
for event_type in [
    "credits.estimated",
    "credits.frozen",
    "credits.charged",
    "credits.released",
    "credits.insufficient",
    "generation.artifact.completed",
    "asset.save.started",
    "asset.save.completed",
    "asset.save.failed",
    "workspace.assets.updated",
    "process.snapshot.saved",
    "agent.run.completed",
    "agent.run.failed",
    "agent.run.cancelled",
]:
    if event_type not in canonical_events:
        fail(f"AG-UI schema missing M4 canonical event {event_type}")
runtime_events = set(re.findall(r'appendRunEvent\([^\\n]+?\"([a-z][a-z0-9]*(?:\\.[a-z][a-z0-9]*)+)\"', agent_app))
runtime_events |= set(re.findall(r'Type:\s*\"([a-z][a-z0-9]*(?:\\.[a-z][a-z0-9]*)+)\"', agent_app))
unknown = runtime_events - canonical_events
if unknown:
    fail(f"agent runtime writes non-canonical AG-UI events: {sorted(unknown)}")

required_types = {
    "short_text", "long_text", "rich_text", "structured_object", "list", "image_ref", "audio_ref",
    "video_ref", "file_ref", "prompt", "storyboard", "timeline", "tag_group", "parameter_group",
}
seed = Path("tests/business/seed/business_core_seed.sql").read_text()
fixtures = "\n".join(path.read_text() for path in Path("tests/contract/fixtures").rglob("*.json"))
for element_type in sorted(required_types):
    if f"'{element_type}'" not in seed and f'"{element_type}"' not in seed:
        fail(f"business seed missing M4 asset element type {element_type}")
    if f'"element_type": "{element_type}"' not in fixtures:
        fail(f"contract fixtures missing M4 asset element type {element_type}")
for forbidden in ["image.primary", "text.caption", "image.variation", "metadata.generation"]:
    if forbidden in seed or forbidden in fixtures or forbidden in Path("tests/agent/agui/fixtures/normal_generation_success.json").read_text():
        fail(f"old asset element taxonomy remains in fixtures/seed: {forbidden}")

for path in [
    "services/business/internal/application/credit/app.go",
    "services/business/internal/application/asset/app.go",
    "services/business/internal/application/assetcommit/app.go",
]:
    text = Path(path).read_text()
    for needle in ["IdempotencyKey", "requestHash", "Transaction"]:
        if needle not in text:
            fail(f"{path} missing M4 write integrity semantic {needle}")

print("M4 semantic source checks ok")
PY

echo "== M4 AG-UI fixtures =="
python3 tests/agent/agui/validate_fixtures.py

echo "== M4 contract fixtures =="
python3 tests/contract/validate_fixtures.py

echo "== M4 no database-level FK =="
if rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services internal; then
  echo "database-level foreign key/reference detected" >&2
  exit 1
fi

echo "M4 technical baseline passed"
