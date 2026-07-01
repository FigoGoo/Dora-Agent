#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOROOT="${GOROOT:-/Users/figo/sdk/go1.26.3}"
export GOPATH="${GOPATH:-/Users/figo/go}"
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

echo "== tool generation baseline =="
scripts/validate-tool-generation-flow.sh

echo "== work marketplace gofmt dry check =="
unformatted="$(find services tests -name '*.go' -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  echo "gofmt required" >&2
  exit 1
fi

echo "== work marketplace full Go tests =="
go test -count=1 ./...

echo "== work marketplace SQL up/down pair check =="
python3 - <<'PY'
from pathlib import Path

for up in Path("db/migrations/iterations").rglob("*.up.sql"):
    down = up.with_name(up.name.replace(".up.sql", ".down.sql"))
    if not down.exists():
        raise SystemExit(f"missing down migration for {up}")
print("sql up/down pair check ok")
PY

echo "== work marketplace semantic source checks =="
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
        fail(f"OpenAPI still exposes old single-step work marketplace path {old_path}")

work_marketplace_routes = {
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
for method, openapi_path, gin_path in sorted(work_marketplace_routes):
    if method not in paths.get(openapi_path, {}):
        fail(f"OpenAPI missing work marketplace route {method.upper()} {openapi_path}")
    if f'"{gin_path}"' not in http_sources:
        fail(f"business Gin router missing work marketplace route {gin_path}")
for old_gin in [
    '"/api/works/:work_id/share"',
    '"/api/admin/works/public/:public_work_id/take-down"',
]:
    if old_gin in http_sources:
        fail(f"business Gin router still exposes old single-step work marketplace route {old_gin}")

schema_requirements = {
    "PreviewShareWorkRequest": {"public_title", "public_description", "tags", "safety_evidence"},
    "ShareWorkPreviewDTO": {"preview_token", "work_id", "public_title", "public_description_digest", "tags", "privacy_redaction_summary", "public_media_summary", "expires_at"},
    "ConfirmShareWorkRequest": {"preview_token"},
    "PreviewTakeDownPublicWorkRequest": {"reason", "notify_author"},
    "TakeDownPublicWorkPreviewDTO": {"preview_token", "expires_at", "notify_author"},
    "ConfirmTakeDownPublicWorkRequest": {"preview_token", "reason", "notify_author"},
    "NotificationDTO": {"type", "title", "summary", "navigation_hint", "read_at", "created_at", "related_resource_type", "related_resource_id"},
    "NotificationListDTO": {"items", "limit", "offset", "total"},
    "NotificationNavigationHintDTO": {"target_route", "target_resource_id"},
    "NotificationNavigationDTO": {"notification_id", "allowed", "target_route", "target_resource_id", "target_type", "denied_reason"},
}
for schema_name, fields in schema_requirements.items():
    schema = schemas.get(schema_name)
    if not schema:
        fail(f"OpenAPI missing work marketplace schema {schema_name}")
    props = set(schema.get("properties", {}))
    missing = fields - props
    if missing:
        fail(f"OpenAPI schema {schema_name} missing fields {sorted(missing)}")

for schema_name in ["PreviewShareWorkRequest", "ConfirmShareWorkRequest", "PreviewTakeDownPublicWorkRequest", "ConfirmTakeDownPublicWorkRequest"]:
    props = set(schemas[schema_name].get("properties", {}))
    forbidden = {"title", "description", "safety_evidence_id", "request_hash"} & props
    if forbidden:
        fail(f"OpenAPI schema {schema_name} still exposes stale fields {sorted(forbidden)}")
notification_props = set(schemas["NotificationDTO"].get("properties", {}))
if "status" in notification_props:
    fail("OpenAPI NotificationDTO still exposes stale status field")
navigation_props = set(schemas["NotificationNavigationDTO"].get("properties", {}))
if "target_id" in navigation_props:
    fail("OpenAPI NotificationNavigationDTO still exposes stale target_id field")
hint_props = set(schemas["NotificationNavigationHintDTO"].get("properties", {}))
if {"target_id", "path"} & hint_props:
    fail("OpenAPI NotificationNavigationHintDTO still exposes stale target_id/path fields")
notification_list_props = set(schemas["NotificationListDTO"].get("properties", {}))
if {"page_size", "has_more", "next_page_token"} & notification_list_props:
    fail("OpenAPI NotificationListDTO still exposes stale page_size/has_more/next_page_token fields")

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
handlers = Path("services/business/internal/transport/http/handlers_work_notification_marketplace.go").read_text()

for needle in [
    "ShareTextDigest",
    'Scene != "work_share"',
    'TargetType != "work_share_text"',
    'Result_ != "passed"',
    "ConfirmShareWork",
    "ConfirmTakeDownWork",
    "DecisionReplay",
    "requireActiveEnterpriseMember",
    "recordNotificationFailure",
    "notification_status",
    "public/works",
    "private_reset_at",
]:
    if needle not in work_app:
        fail(f"work application missing work marketplace semantic {needle}")
for needle in [
    "CreateNotification",
    "MarkNotificationRead",
    "MarkAllNotificationsRead",
    "GetNotificationNavigation",
    "DecisionReplay",
    "enterprise member is not active",
    "RecordCreateFailure",
    "RelatedResourceType",
    "NavigationHint",
]:
    if needle not in notification_app:
        fail(f"notification application missing work marketplace semantic {needle}")
for needle in ["SetNotificationService", "skill_review_approved", "skill_review_rejected", "RecordCreateFailure"]:
    if needle not in skill_app:
        fail(f"skill review notification wiring missing {needle}")
for needle in ["work.New", "notification.New", "SetNotificationService", "PublicWebBaseURL", "TOSBaseURL"]:
    if needle not in bootstrap:
        fail(f"business bootstrap missing work marketplace wiring {needle}")
for needle in ["Work", "*work.App", "Notification", "*notification.App", "registerWorkNotificationMarketplaceRoutes"]:
    if needle not in router:
        fail(f"business router missing work marketplace wiring {needle}")
for needle in [
    "previewShareWork",
    "confirmShareWork",
    "adminPreviewTakeDownWork",
    "adminConfirmTakeDownWork",
    "notificationNavigation",
    "requireIdempotency()",
]:
    if needle not in handlers:
        fail(f"business work marketplace HTTP handler missing {needle}")
for stale_preview in [
    'router.POST("/api/works/:work_id/share/preview", auth.userAuth(), requireIdempotency()',
    'router.POST("/api/admin/works/public/:public_work_id/take-down/preview", auth.adminAuth(false), requireIdempotency()',
]:
    if stale_preview in handlers:
        fail("work marketplace preview route must not require Idempotency-Key or request_hash")

migration = Path("db/migrations/iterations/2026-06-27-business-core/business/0018_work_notification_alignment.up.sql")
if not migration.exists():
    fail("missing work marketplace work/notification alignment migration")
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
        fail(f"work marketplace migration missing field/table {needle}")

seed = Path("tests/business/seed/business_core_seed.sql").read_text()
for needle in ["wrk_seed_public", "pubw_seed_storyboard", "wps_seed_public", "ntf_skill_review_001"]:
    if needle not in seed:
        fail(f"business seed missing work marketplace seed {needle}")

fixture_cases = set()
fixture_by_id = {}
for path in Path("tests/contract/fixtures/business-api").glob("*.json"):
    data = json.loads(path.read_text())
    case_id = data.get("case_id", "")
    fixture_cases.add(case_id)
    fixture_by_id[case_id] = data
required_fixture_cases = {
    "business_api_work_share_preview_success",
    "business_api_work_share_confirm_success",
    "business_api_work_share_business_error_safety_digest_mismatch",
    "business_api_work_share_idempotency_conflict",
    "business_api_admin_public_work_takedown_preview_success",
    "business_api_admin_public_work_takedown_confirm_success",
    "business_api_notification_read_success",
    "business_api_notification_navigation_success",
    "business_api_public_work_timeout_error",
    "business_api_public_work_version_compat_success",
}
missing = required_fixture_cases - fixture_cases
if missing:
    fail(f"missing work marketplace business-api fixtures {sorted(missing)}")

preview_fixture = fixture_by_id["business_api_work_share_preview_success"]
if "Idempotency-Key" in preview_fixture.get("request_headers", {}):
    fail("work share preview fixture must not require Idempotency-Key")
preview_body = preview_fixture.get("request_body", {})
for field in ["public_title", "public_description", "tags", "safety_evidence"]:
    if field not in preview_body:
        fail(f"work share preview fixture missing {field}")
if {"title", "description", "safety_evidence_id", "request_hash"} & set(preview_body):
    fail("work share preview fixture still uses stale request fields")

confirm_fixture = fixture_by_id["business_api_work_share_confirm_success"]
if set(confirm_fixture.get("request_body", {})) != {"preview_token"}:
    fail("work share confirm fixture body must contain only preview_token")
if "Idempotency-Key" not in confirm_fixture.get("request_headers", {}):
    fail("work share confirm fixture missing Idempotency-Key")

takedown_preview = fixture_by_id["business_api_admin_public_work_takedown_preview_success"]
if "Idempotency-Key" in takedown_preview.get("request_headers", {}):
    fail("take-down preview fixture must not require Idempotency-Key")
if "request_hash" in takedown_preview.get("request_body", {}):
    fail("take-down preview fixture must not carry request_hash")

takedown_confirm = fixture_by_id["business_api_admin_public_work_takedown_confirm_success"]
if "request_hash" in takedown_confirm.get("request_body", {}):
    fail("take-down confirm fixture must not carry request_hash")
if "Idempotency-Key" not in takedown_confirm.get("request_headers", {}):
    fail("take-down confirm fixture missing Idempotency-Key")

notification_fixture = fixture_by_id["business_api_notification_read_success"]["response_body"]["data"]
if "status" in notification_fixture:
    fail("notification fixture still exposes stale status")
for field in ["title", "related_resource_type", "related_resource_id", "navigation_hint", "read_at", "created_at"]:
    if field not in notification_fixture:
        fail(f"notification fixture missing {field}")
hint = notification_fixture["navigation_hint"]
if {"target_id", "path"} & set(hint):
    fail("notification fixture still exposes stale navigation hint target_id/path fields")
for field in ["target_route", "target_resource_id"]:
    if field not in hint:
        fail(f"notification fixture navigation_hint missing {field}")
notification_page = fixture_by_id["business_api_notifications_unread_page_success"]["response_body"]["data"]
if {"page_size", "has_more", "next_page_token"} & set(notification_page):
    fail("notification page fixture still exposes stale pagination fields")
for field in ["items", "limit", "offset", "total"]:
    if field not in notification_page:
        fail(f"notification page fixture missing {field}")
notification_nav = fixture_by_id["business_api_notification_navigation_success"]["response_body"]["data"]
if "target_id" in notification_nav:
    fail("notification navigation fixture still exposes stale target_id")
for field in ["notification_id", "allowed", "target_route", "target_resource_id"]:
    if field not in notification_nav:
        fail(f"notification navigation fixture missing {field}")

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
        fail(f"Agent service must not persist/copy work marketplace business fact {forbidden}")
agent_app = Path("services/agent/internal/application/workbench/app.go").read_text()
for forbidden_event in ["work.share", "public.work", "notification."]:
    if forbidden_event in agent_app:
        fail(f"Agent runtime must not emit work marketplace non-canonical public/notification event {forbidden_event}")
for forbidden_payload in ["public_slug", "take_down", "share_status"]:
    if forbidden_payload in agent_app:
        fail(f"Agent snapshot/event payload leaks work marketplace public state {forbidden_payload}")
for required in ["workspace.assets.updated", "process.snapshot.saved", "assetRefsForEvent"]:
    if required not in agent_app:
        fail(f"Agent snapshot/assets event baseline missing {required}")

print("work marketplace semantic source checks ok")
PY

echo "== work marketplace AG-UI fixtures =="
python3 tests/agent/agui/validate_fixtures.py

echo "== work marketplace contract fixtures =="
python3 tests/contract/validate_fixtures.py

echo "== work marketplace no database-level FK =="
if rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services; then
  echo "database-level foreign key/reference detected" >&2
  exit 1
fi

echo "work marketplace technical baseline passed"
