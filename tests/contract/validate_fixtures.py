#!/usr/bin/env python3
"""Validate M0 business RPC and HTTP contract fixtures."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parent / "fixtures"
REPO_ROOT = Path(__file__).resolve().parents[2]
RPC_DIR = ROOT / "business-rpc"
API_DIR = ROOT / "business-api"
THRIFT_IDL = REPO_ROOT / "api" / "thrift" / "business_agent_service.thrift"
SEED_DIR = REPO_ROOT / "tests" / "business" / "seed"

WRITE_RPC_METHODS = {
    "ConfirmTransferOwner",
    "CreateAdmin",
    "DisableAdmin",
    "ConfirmSetUserStatus",
    "CreateProject",
    "UpdateProjectTitle",
    "AttachAssetToProject",
    "CreateUploadIntent",
    "ConfirmUploadedAsset",
    "FreezeCredits",
    "ChargeToolUsageCredits",
    "ReleaseFrozenCredits",
    "PrepareGeneratedAssetObjects",
    "CommitGeneratedAssetAndCharge",
    "SaveSkillTestResult",
    "CreateWork",
    "ConfirmShareWork",
    "ConfirmTakeDownWork",
    "CreateNotification",
    "MarkNotificationRead",
    "MarkAllNotificationsRead",
}

REQUIRED_RPC_SCENARIOS = {
    "success": "normal success path",
    "permission_denied": "permission error path",
    "business_error": "business error path",
    "idempotency_conflict": "idempotency conflict path",
    "timeout": "timeout path",
    "version_compat": "version compatibility path",
}

DOMAIN_REQUIREMENTS = {
    "accountspace": {
        "unauthenticated_error",
        "personal_space_success",
        "enterprise_owner_success",
        "enterprise_member_success",
        "member_removed_error",
        "disabled_user_error",
    },
    "project": {
        "continue_creation_success",
        "archived_readonly_error",
        "cross_space_permission_denied",
        "project_not_found_error",
        "create_project_success",
        "update_project_title_success",
        "attach_asset_success",
    },
    "enterprise": {
        "transfer_owner_preview_success",
        "transfer_owner_confirm_success",
    },
    "admin": {
        "create_admin_success",
        "disable_admin_success",
        "preview_user_status_success",
        "confirm_user_status_success",
    },
    "skill": {
        "published_routable_success",
        "draft_not_routable_error",
        "deprecated_not_routable_error",
        "published_spec_success",
        "review_candidate_success",
        "skill_test_result_save_success",
    },
    "tool": {
        "disabled_error",
        "high_risk_success",
        "requires_confirmation_success",
        "timeout_policy_success",
        "no_charge_success",
        "tool_usage_charge_success",
    },
    "model": {
        "default_model_success",
        "available_models_page_success",
        "disabled_model_error",
        "pricing_snapshot_missing_error",
        "provider_config_missing_error",
    },
    "credit": {
        "balance_enough_success",
        "insufficient_error",
        "freeze_idempotency_success",
        "duplicate_charge_error",
        "release_idempotency_success",
        "safety_evidence_invalid_error",
    },
    "asset": {
        "batch_access_success",
        "create_upload_intent_success",
        "confirm_uploaded_asset_success",
        "prepare_slots_success",
        "upload_authorization_expired_error",
        "object_key_mismatch_error",
        "save_failed_error",
    },
    "work": {
        "create_work_success",
        "share_preview_success",
        "share_confirm_success",
        "take_down_preview_success",
        "take_down_confirm_success",
        "public_list_success",
        "public_detail_success",
    },
    "notification": {
        "create_notification_success",
        "list_notifications_success",
        "unread_count_success",
        "mark_read_success",
        "mark_all_read_success",
    },
}

REQUIRED_HTTP_STATUS = {200, 401, 403, 404, 409, 422, 500}
SERVER_HASH_HTTP_SUFFIXES = ("/share/confirm", "/take-down/confirm")
REQUIRED_SEED_KEYWORDS = {
    "adm_root": "initial admin",
    "sp_personal_1001": "personal space",
    "sp_enterprise_1001": "enterprise space",
    "prj_archived_1001": "archived project",
    "ast_other_space_1002": "cross-space asset",
    "wrk_seed_public": "public work",
    "rc_enterprise_1001": "redeem code binding",
    "ca_personal_1002": "insufficient balance account",
}


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(1)


def load_json(path: Path) -> dict[str, Any]:
    try:
        data = json.loads(path.read_text())
    except json.JSONDecodeError as exc:
        fail(f"{path}: invalid JSON: {exc}")
    if not isinstance(data, dict):
        fail(f"{path}: fixture root must be an object")
    return data


def require_path(data: dict[str, Any], path: str, source: str) -> object:
    current: object = data
    for part in path.split("."):
        if not isinstance(current, dict) or part not in current:
            fail(f"{source}: missing {path}")
        current = current[part]
    return current


def thrift_methods() -> set[str]:
    text = THRIFT_IDL.read_text()
    result: set[str] = set()
    for service_match in re.finditer(r"service\s+(\w+)\s*\{(?P<body>.*?)\n\}", text, flags=re.S):
        service = service_match.group(1)
        body = service_match.group("body")
        for line in body.splitlines():
            method_match = re.search(r"\b\w+\s+(\w+)\s*\(", line)
            if method_match:
                result.add(f"{service}.{method_match.group(1)}")
    if not result:
        fail(f"{THRIFT_IDL}: no service methods parsed")
    return result


def validate_rpc_case(source: str, data: dict[str, Any]) -> tuple[set[str], str, str | None]:
    for key in ("case_id", "service", "method", "request", "assertions"):
        if key not in data:
            fail(f"{source}: missing {key}")

    method = data["method"]
    service_method = f"{data['service']}.{method}"
    request_meta = require_path(data, "request.request_meta", source)
    if not isinstance(request_meta, dict):
        fail(f"{source}: request.request_meta must be an object")
    for key in ("request_id", "trace_id", "source"):
        if key not in request_meta:
            fail(f"{source}: request.request_meta missing {key}")

    if method in WRITE_RPC_METHODS:
        if "idempotency_key" not in request_meta:
            fail(f"{source}: write RPC fixture missing request.request_meta.idempotency_key")
        if "request_hash" not in data:
            fail(f"{source}: write RPC fixture missing top-level request_hash")
        audit = data.get("audit_expectation")
        if not isinstance(audit, dict):
            fail(f"{source}: write RPC fixture missing audit_expectation")
        for key in ("action", "trace_id", "result"):
            if key not in audit:
                fail(f"{source}: audit_expectation missing {key}")

    has_response = "response" in data
    has_error = "error" in data
    if has_response == has_error:
        fail(f"{source}: fixture must have exactly one of response or error")

    if has_error:
        error = data["error"]
        if not isinstance(error, dict):
            fail(f"{source}: error must be an object")
        for key in ("code", "message", "retryable"):
            if key not in error:
                fail(f"{source}: error missing {key}")

    labels = " ".join(str(data.get(key, "")) for key in ("case_id", "scenario"))
    matched = {name for name in REQUIRED_RPC_SCENARIOS if name in labels}
    if labels.endswith("_success") or "_success" in labels:
        matched.add("success")
    if "permission" in labels or "unauthenticated" in labels:
        matched.add("permission_denied")
    if "business_error" in labels or "insufficient" in labels or "invalid" in labels or "not_found" in labels:
        matched.add("business_error")
    if "idempotency" in labels or "duplicate" in labels:
        matched.add("idempotency_conflict")
    if "timeout" in labels:
        matched.add("timeout")
    if "version_compat" in labels:
        matched.add("version_compat")
    scenario = data.get("scenario") if isinstance(data.get("scenario"), str) else None
    return matched, service_method, scenario


def iter_rpc_cases() -> tuple[set[str], set[str], dict[str, set[str]]]:
    rpc_seen: set[str] = set()
    method_seen: set[str] = set()
    domain_seen: dict[str, set[str]] = {domain: set() for domain in DOMAIN_REQUIREMENTS}

    if not RPC_DIR.exists():
        fail(f"missing RPC fixture dir {RPC_DIR}")
    for domain in DOMAIN_REQUIREMENTS:
        if not (RPC_DIR / domain).is_dir():
            fail(f"missing RPC fixture domain dir {RPC_DIR / domain}")

    for path in sorted(RPC_DIR.rglob("*.json")):
        data = load_json(path)
        if "scenarios" in data:
            domain = data.get("domain") or path.parent.name
            if domain not in DOMAIN_REQUIREMENTS:
                fail(f"{path}: unknown domain {domain}")
            scenarios = data["scenarios"]
            if not isinstance(scenarios, list) or not scenarios:
                fail(f"{path}: scenarios must be a non-empty array")
            for idx, item in enumerate(scenarios):
                if not isinstance(item, dict):
                    fail(f"{path}:{idx}: scenario must be an object")
                matched, service_method, scenario = validate_rpc_case(f"{path}:{idx}", item)
                rpc_seen |= matched
                method_seen.add(service_method)
                if scenario:
                    domain_seen[domain].add(scenario)
                print(f"business rpc fixture ok {path}:{idx}")
        else:
            matched, service_method, _ = validate_rpc_case(str(path), data)
            rpc_seen |= matched
            method_seen.add(service_method)
            print(f"business rpc fixture ok {path}")
    return rpc_seen, method_seen, domain_seen


def validate_api_fixture(path: Path, data: dict[str, Any]) -> int:
    for key in ("case_id", "method", "path", "response_status", "response_body", "assertions"):
        if key not in data:
            fail(f"{path}: missing {key}")
    response = data["response_body"]
    if not isinstance(response, dict):
        fail(f"{path}: response_body must be an object")
    for key in ("code", "message", "trace_id"):
        if key not in response:
            fail(f"{path}: response_body missing {key}")
    status = data["response_status"]
    if not isinstance(status, int):
        fail(f"{path}: response_status must be an integer")
    if status >= 400:
        if response.get("data") is not None:
            fail(f"{path}: error response data must be null")
        error = response.get("error")
        if not isinstance(error, dict):
            fail(f"{path}: error response missing response_body.error")
        for key in ("code", "message", "retryable"):
            if key not in error:
                fail(f"{path}: response_body.error missing {key}")
    if data["method"] in {"POST", "PATCH", "PUT", "DELETE"}:
        headers = data.get("request_headers", {})
        body = data.get("request_body")
        server_hash = any(str(data["path"]).endswith(suffix) for suffix in SERVER_HASH_HTTP_SUFFIXES)
        if "Idempotency-Key" in headers and isinstance(body, dict) and "request_hash" not in body and not server_hash:
            fail(f"{path}: idempotent HTTP fixture missing request_body.request_hash")
    return status


def validate_seed() -> None:
    if not SEED_DIR.is_dir():
        fail(f"missing business seed dir {SEED_DIR}")
    text = "\n".join(path.read_text() for path in SEED_DIR.glob("*.sql"))
    missing = [label for keyword, label in REQUIRED_SEED_KEYWORDS.items() if keyword not in text]
    if missing:
        fail(f"business seed missing coverage: {missing}")


def main() -> None:
    rpc_seen, method_seen, domain_seen = iter_rpc_cases()
    missing_rpc = set(REQUIRED_RPC_SCENARIOS) - rpc_seen
    if missing_rpc:
        labels = ", ".join(f"{item} ({REQUIRED_RPC_SCENARIOS[item]})" for item in sorted(missing_rpc))
        fail(f"missing RPC fixture scenarios: {labels}")

    idl_methods = thrift_methods()
    missing_methods = idl_methods - method_seen
    if missing_methods:
        fail(f"missing RPC fixture method coverage: {sorted(missing_methods)}")

    for domain, required in DOMAIN_REQUIREMENTS.items():
        missing = required - domain_seen[domain]
        if missing:
            fail(f"{domain}: missing required scenarios {sorted(missing)}")

    http_statuses: set[int] = set()
    for path in sorted(API_DIR.glob("*.json")):
        http_statuses.add(validate_api_fixture(path, load_json(path)))
        print(f"business api fixture ok {path}")
    missing_http = REQUIRED_HTTP_STATUS - http_statuses
    if missing_http:
        fail(f"missing HTTP fixture response statuses: {sorted(missing_http)}")

    validate_seed()


if __name__ == "__main__":
    main()
