#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOROOT="${GOROOT:-/Users/figo/sdk/go1.26.3}"
export GOPATH="${GOPATH:-/Users/figo/go}"
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

echo "== M4 baseline =="
scripts/validate-m4.sh

echo "== M5 gofmt dry check =="
unformatted="$(find services tests -name '*.go' -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  echo "gofmt required" >&2
  exit 1
fi

echo "== M5 full Go tests =="
go test -count=1 ./...

echo "== M5 SQL up/down pair check =="
python3 - <<'PY'
from pathlib import Path

for up in Path("db/migrations/iterations").rglob("*.up.sql"):
    down = up.with_name(up.name.replace(".up.sql", ".down.sql"))
    if not down.exists():
        raise SystemExit(f"missing down migration for {up}")
print("sql up/down pair check ok")
PY

echo "== M5 semantic source checks =="
python3 - <<'PY'
from pathlib import Path
import json
import re
import yaml


def fail(msg: str) -> None:
    raise SystemExit(msg)


openapi = yaml.safe_load(Path("api/openapi/business-api.yaml").read_text())
paths = openapi.get("paths", {})
components = openapi.get("components", {})
schemas = components.get("schemas", {})

for old_path in [
    "/api/works/{work_id}/share",
    "/api/admin/works/public/{public_work_id}/take-down",
]:
    if old_path in paths:
        fail(f"OpenAPI still exposes old single-step M5 path {old_path}")

m5_routes = {
    ("get", "/api/works", "/api/works"),
    ("post", "/api/works", "/api/works"),
    ("get", "/api/works/{work_id}", "/api/works/:work_id"),
    ("patch", "/api/works/{work_id}", "/api/works/:work_id"),
    ("post", "/api/works/{work_id}/share/preview", "/api/works/:work_id/share/preview"),
    ("post", "/api/works/{work_id}/share/confirm", "/api/works/:work_id/share/confirm"),
    ("post", "/api/works/{work_id}/unshare", "/api/works/:work_id/unshare"),
    ("get", "/api/public/home", "/api/public/home"),
    ("get", "/api/public/works", "/api/public/works"),
    ("get", "/api/public/works/{public_work_id}", "/api/public/works/:public_work_id"),
    ("post", "/api/public/works/{public_work_id}/like", "/api/public/works/:public_work_id/like"),
    ("post", "/api/public/works/{public_work_id}/unlike", "/api/public/works/:public_work_id/unlike"),
    ("get", "/api/admin/works/public", "/api/admin/works/public"),
    ("post", "/api/admin/works/public/{public_work_id}/take-down/preview", "/api/admin/works/public/:public_work_id/take-down/preview"),
    ("post", "/api/admin/works/public/{public_work_id}/take-down/confirm", "/api/admin/works/public/:public_work_id/take-down/confirm"),
    ("get", "/api/notifications", "/api/notifications"),
    ("get", "/api/notifications/unread-count", "/api/notifications/unread-count"),
    ("post", "/api/notifications/{notification_id}/read", "/api/notifications/:notification_id/read"),
    ("post", "/api/notifications/read-all", "/api/notifications/read-all"),
    ("get", "/api/notifications/{notification_id}/navigation", "/api/notifications/:notification_id/navigation"),
}
http_sources = "\n".join(path.read_text() for path in Path("services/business/internal/transport/http").glob("*.go"))
for method, openapi_path, gin_path in sorted(m5_routes):
    if method not in paths.get(openapi_path, {}):
        fail(f"OpenAPI missing M5 route {method.upper()} {openapi_path}")
    if f'"{gin_path}"' not in http_sources:
        fail(f"business Gin router missing M5 route {gin_path}")
for old_gin in [
    '"/api/works/:work_id/share"',
    '"/api/admin/works/public/:public_work_id/take-down"',
]:
    if old_gin in http_sources:
        fail(f"business Gin router still exposes old single-step M5 route {old_gin}")

schema_requirements = {
    "ShareWorkPreviewDTO": {"preview_token", "expires_at", "safety_evidence_id", "safety_evidence_digest"},
    "ConfirmShareWorkRequest": {"preview_token", "safety_evidence_id", "request_hash"},
    "TakeDownPublicWorkPreviewDTO": {"preview_token", "expires_at", "notify_author"},
    "ConfirmTakeDownPublicWorkRequest": {"preview_token", "reason", "request_hash"},
    "NotificationDTO": {"type", "summary", "body", "navigation_hint", "read_at", "created_at"},
    "NotificationNavigationHintDTO": {"target_type", "target_id", "path"},
}
for schema_name, fields in schema_requirements.items():
    schema = schemas.get(schema_name)
    if not schema:
        fail(f"OpenAPI missing M5 schema {schema_name}")
    props = set(schema.get("properties", {}))
    missing = fields - props
    if missing:
        fail(f"OpenAPI schema {schema_name} missing fields {sorted(missing)}")

public_detail = schemas.get("PublicWorkDetailDTO", {})
if "work_id" in public_detail.get("required", []) or "work_id" in public_detail.get("properties", {}):
    fail("PublicWorkDetailDTO must not expose private work_id")
for field in ["public_work_id", "snapshot_id", "public_media_refs", "share_url"]:
    if field not in public_detail.get("properties", {}):
        fail(f"PublicWorkDetailDTO missing public-safe field {field}")

work_app = Path("services/business/internal/application/work/app.go").read_text()
notification_app = Path("services/business/internal/application/notification/app.go").read_text()
skill_app = Path("services/business/internal/application/skillcatalog/app.go").read_text()
bootstrap = Path("services/business/internal/bootstrap/app.go").read_text()
router = Path("services/business/internal/transport/http/router.go").read_text()
handlers = Path("services/business/internal/transport/http/handlers_m5.go").read_text()

for needle in [
    "ShareTextDigest",
    'Scene != "work_share"',
    'TargetType != "work_share_text"',
    'Result_ != "passed"',
    "ConfirmShareWork",
    "ConfirmTakeDownWork",
    "recordNotificationFailure",
    "notification_status",
    "public/works",
    "private_reset_at",
]:
    if needle not in work_app:
        fail(f"work application missing M5 semantic {needle}")
for needle in [
    "CreateNotification",
    "MarkNotificationRead",
    "MarkAllNotificationsRead",
    "GetNotificationNavigation",
    "RecordCreateFailure",
    "RelatedResourceType",
    "NavigationHint",
]:
    if needle not in notification_app:
        fail(f"notification application missing M5 semantic {needle}")
for needle in ["SetNotificationService", "skill_review_approved", "skill_review_rejected", "RecordCreateFailure"]:
    if needle not in skill_app:
        fail(f"skill review notification wiring missing {needle}")
for needle in ["work.New", "notification.New", "SetNotificationService", "PublicWebBaseURL", "TOSBaseURL"]:
    if needle not in bootstrap:
        fail(f"business bootstrap missing M5 wiring {needle}")
for needle in ["Work", "*work.App", "Notification", "*notification.App", "registerM5Routes"]:
    if needle not in router:
        fail(f"business router missing M5 wiring {needle}")
for needle in [
    "previewShareWork",
    "confirmShareWork",
    "adminPreviewTakeDownWork",
    "adminConfirmTakeDownWork",
    "notificationNavigation",
    "requireIdempotency()",
]:
    if needle not in handlers:
        fail(f"business M5 HTTP handler missing {needle}")

migration = Path("db/migrations/iterations/2026-06-27-business-core/business/0018_m5_work_notification_alignment.up.sql")
if not migration.exists():
    fail("missing M5 work/notification alignment migration")
migration_text = migration.read_text()
for needle in [
    "share_status",
    "current_snapshot_id",
    "private_reset_at",
    "public_work_id",
    "public_media_refs",
    "notification_id",
    "navigation_hint",
    "notification_create_failures",
]:
    if needle not in migration_text:
        fail(f"M5 migration missing field/table {needle}")

seed = Path("tests/business/seed/business_core_seed.sql").read_text()
for needle in ["wrk_seed_public", "pubw_seed_storyboard", "wps_seed_public", "ntf_skill_review_001"]:
    if needle not in seed:
        fail(f"business seed missing M5 seed {needle}")

fixture_cases = set()
for path in Path("tests/contract/fixtures/business-api").glob("*.json"):
    data = json.loads(path.read_text())
    fixture_cases.add(data.get("case_id", ""))
required_fixture_cases = {
    "business_api_m5_work_share_preview_success",
    "business_api_m5_work_share_confirm_success",
    "business_api_m5_work_share_business_error_safety_digest_mismatch",
    "business_api_m5_work_share_idempotency_conflict",
    "business_api_m5_admin_public_work_takedown_preview_success",
    "business_api_m5_admin_public_work_takedown_confirm_success",
    "business_api_m5_notification_read_success",
    "business_api_m5_public_work_timeout_error",
    "business_api_m5_public_work_version_compat_success",
}
missing = required_fixture_cases - fixture_cases
if missing:
    fail(f"missing M5 business-api fixtures {sorted(missing)}")

agent_text = "\n".join(path.read_text() for path in Path("services/agent").rglob("*.go"))
for forbidden in [
    "public_work_id",
    "work_public_snapshots",
    "share_status",
    "notification_id",
    "NotificationDTO",
    "CreateNotification",
]:
    if forbidden in agent_text:
        fail(f"Agent service must not persist/copy M5 business fact {forbidden}")
agent_app = Path("services/agent/internal/application/workbench/app.go").read_text()
for forbidden_event in ["work.share", "public.work", "notification."]:
    if forbidden_event in agent_app:
        fail(f"Agent runtime must not emit M5 non-canonical public/notification event {forbidden_event}")
for forbidden_payload in ["public_slug", "take_down", "share_status"]:
    if forbidden_payload in agent_app:
        fail(f"Agent snapshot/event payload leaks M5 public state {forbidden_payload}")
for required in ["workspace.assets.updated", "process.snapshot.saved", "assetRefsForEvent"]:
    if required not in agent_app:
        fail(f"Agent snapshot/assets event baseline missing {required}")

print("M5 semantic source checks ok")
PY

echo "== M5 AG-UI fixtures =="
python3 tests/agent/agui/validate_fixtures.py

echo "== M5 contract fixtures =="
python3 tests/contract/validate_fixtures.py

echo "== M5 no database-level FK =="
if rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services; then
  echo "database-level foreign key/reference detected" >&2
  exit 1
fi

echo "M5 technical baseline passed"
