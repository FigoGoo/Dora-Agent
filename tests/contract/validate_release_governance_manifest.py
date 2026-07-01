#!/usr/bin/env python3
"""Validate release governance manifest and its active documentation links."""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
MANIFEST_PATH = REPO_ROOT / "configs" / "release" / "governance.json"
RELEASE_GOVERNANCE_DOC = REPO_ROOT / "docs" / "active" / "technical" / "release-governance.md"
ACTIVE_README = REPO_ROOT / "docs" / "active" / "README.md"
CURRENT_README = REPO_ROOT / "docs" / "current" / "README.md"
RELEASE_GATE_DOC = REPO_ROOT / "docs" / "active" / "contracts" / "pr-5-e2e-fixtures-release-gates.md"
MAKEFILE = REPO_ROOT / "Makefile"
ACTIVE_CONTRACT_SCRIPT = REPO_ROOT / "scripts" / "validate-active-contracts.sh"
GOVERNANCE_SCRIPT = REPO_ROOT / "scripts" / "validate-release-governance.sh"

REQUIRED_FLAGS = {
    "agent_runtime_v2",
    "tool_generation_v2",
    "marketplace_v2",
    "creator_portal_v2",
    "admin_governance_v2",
}

REQUIRED_GATES = {
    "Contract Gate",
    "Migration Gate",
    "Fixture Gate",
    "Service E2E Gate",
    "Agent / Business HTTP Gate",
    "Local Full HTTP Service Gate",
    "Browser Smoke Gate",
    "Fake Provider Gate",
    "Test Environment HTTP Service Gate",
    "Release Governance Manifest Gate",
    "Build Gate",
    "Release Gate",
}

REQUIRED_METRICS = {
    "agent_run_success_rate",
    "router_decision_latency_ms",
    "board_patch_replay_error_count",
    "graph_resume_failure_count",
    "tool_task_success_rate",
    "credit_freeze_leak_count",
    "skill_usage_charge_error_count",
    "marketplace_install_failure_count",
    "settlement_reverse_count",
}

REQUIRED_REPAIR_CASES = {
    "tool_task_terminal_failure_hold_not_released",
    "duplicate_provider_callback",
    "skill_usage_charged_but_settlement_missing",
    "settlement_reversed_but_creator_hold_releasable",
    "listing_suspended_but_new_installation_created",
}

REQUIRED_POLICY_GATES = {
    "active-contract-gate",
    "development-ci-gate",
    "release-full-http-smoke",
    "release-browser-smoke",
    "release-http-service-e2e",
    "release-governance-gate",
    "manual_release_approval",
}


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(1)


def load_json(path: Path) -> dict[str, Any]:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        fail(f"missing {path}")
    except json.JSONDecodeError as exc:
        fail(f"{path}: invalid JSON: {exc}")
    if not isinstance(data, dict):
        fail(f"{path}: root must be an object")
    return data


def require_list(data: dict[str, Any], key: str) -> list[Any]:
    value = data.get(key)
    if not isinstance(value, list) or not value:
        fail(f"{MANIFEST_PATH}: {key} must be a non-empty list")
    return value


def require_text(value: Any, path: str) -> str:
    if not isinstance(value, str) or not value.strip():
        fail(f"{MANIFEST_PATH}: {path} must be a non-empty string")
    return value


def validate_policy(manifest: dict[str, Any]) -> None:
    if manifest.get("schema_version") != "release_governance_manifest.v1":
        fail(f"{MANIFEST_PATH}: schema_version must be release_governance_manifest.v1")
    if manifest.get("status") != "active":
        fail(f"{MANIFEST_PATH}: status must be active")

    policy = manifest.get("release_policy")
    if not isinstance(policy, dict):
        fail(f"{MANIFEST_PATH}: release_policy must be an object")
    if policy.get("default_provider_mode") != "fake":
        fail(f"{MANIFEST_PATH}: default_provider_mode must be fake")
    if policy.get("all_new_feature_flags_default_off") is not True:
        fail(f"{MANIFEST_PATH}: all_new_feature_flags_default_off must be true")
    true_provider_requires = set(policy.get("true_provider_requires", []))
    missing = REQUIRED_POLICY_GATES - true_provider_requires
    if missing:
        fail(f"{MANIFEST_PATH}: true_provider_requires missing {sorted(missing)}")


def validate_feature_flags(manifest: dict[str, Any]) -> None:
    flags = require_list(manifest, "feature_flags")
    seen: set[str] = set()
    for item in flags:
        if not isinstance(item, dict):
            fail(f"{MANIFEST_PATH}: feature_flags item must be an object")
        flag = require_text(item.get("flag"), "feature_flags[].flag")
        seen.add(flag)
        if item.get("default") != "off":
            fail(f"{MANIFEST_PATH}: {flag} default must be off")
        controls = item.get("controls")
        if not isinstance(controls, list) or len(controls) < 2:
            fail(f"{MANIFEST_PATH}: {flag} controls must list at least two boundaries")
        rollback_actions = item.get("rollback_actions")
        if not isinstance(rollback_actions, list) or not rollback_actions:
            fail(f"{MANIFEST_PATH}: {flag} rollback_actions must be non-empty")
        if not any(flag in str(action) for action in rollback_actions):
            fail(f"{MANIFEST_PATH}: {flag} rollback_actions must mention the flag")

    missing = REQUIRED_FLAGS - seen
    if missing:
        fail(f"{MANIFEST_PATH}: feature_flags missing {sorted(missing)}")


def validate_release_gates(manifest: dict[str, Any]) -> None:
    gates = require_list(manifest, "release_gates")
    seen: set[str] = set()
    for item in gates:
        if not isinstance(item, dict):
            fail(f"{MANIFEST_PATH}: release_gates item must be an object")
        gate = require_text(item.get("gate"), "release_gates[].gate")
        seen.add(gate)
        if item.get("required") is not True:
            fail(f"{MANIFEST_PATH}: {gate} must be required")
        require_text(item.get("phase"), f"release_gates[{gate}].phase")
        require_text(item.get("command"), f"release_gates[{gate}].command")
        artifacts = item.get("artifacts")
        if not isinstance(artifacts, list) or not artifacts:
            fail(f"{MANIFEST_PATH}: {gate} artifacts must be non-empty")
        blocking_conditions = item.get("blocking_conditions")
        if not isinstance(blocking_conditions, list) or not blocking_conditions:
            fail(f"{MANIFEST_PATH}: {gate} blocking_conditions must be non-empty")

    missing = REQUIRED_GATES - seen
    if missing:
        fail(f"{MANIFEST_PATH}: release_gates missing {sorted(missing)}")


def validate_observability(manifest: dict[str, Any]) -> None:
    metrics = require_list(manifest, "observability_metrics")
    seen: set[str] = set()
    for item in metrics:
        if not isinstance(item, dict):
            fail(f"{MANIFEST_PATH}: observability_metrics item must be an object")
        metric = require_text(item.get("metric"), "observability_metrics[].metric")
        seen.add(metric)
        require_text(item.get("kind"), f"observability_metrics[{metric}].kind")
        require_text(item.get("owner"), f"observability_metrics[{metric}].owner")
        require_text(item.get("blocking_threshold"), f"observability_metrics[{metric}].blocking_threshold")
        labels = item.get("labels")
        if not isinstance(labels, list) or not labels:
            fail(f"{MANIFEST_PATH}: {metric} labels must be non-empty")
        trace_fields = item.get("required_trace_fields")
        if not isinstance(trace_fields, list) or "trace_id" not in trace_fields:
            fail(f"{MANIFEST_PATH}: {metric} required_trace_fields must include trace_id")

    missing = REQUIRED_METRICS - seen
    if missing:
        fail(f"{MANIFEST_PATH}: observability_metrics missing {sorted(missing)}")


def validate_rollback(manifest: dict[str, Any]) -> None:
    steps = require_list(manifest, "rollback_steps")
    orders: list[int] = []
    combined: list[str] = []
    for item in steps:
        if not isinstance(item, dict):
            fail(f"{MANIFEST_PATH}: rollback_steps item must be an object")
        order = item.get("order")
        if not isinstance(order, int):
            fail(f"{MANIFEST_PATH}: rollback_steps[].order must be integer")
        orders.append(order)
        combined.append(require_text(item.get("action"), f"rollback_steps[{order}].action"))
        details = item.get("details")
        if not isinstance(details, list) or not details:
            fail(f"{MANIFEST_PATH}: rollback_steps[{order}].details must be non-empty")
        combined.extend(str(detail) for detail in details)

    if orders != list(range(1, len(orders) + 1)):
        fail(f"{MANIFEST_PATH}: rollback_steps order must be contiguous from 1")

    text = " ".join(combined)
    required_tokens = [
        "feature flag",
        "stop",
        "credit hold",
        "preserve",
        "AG-UI replay",
        "dedupe_key",
        "audit",
    ]
    for token in required_tokens:
        if token not in text:
            fail(f"{MANIFEST_PATH}: rollback_steps missing token {token}")


def validate_data_repair(manifest: dict[str, Any]) -> None:
    cases = require_list(manifest, "data_repair_cases")
    seen: set[str] = set()
    for item in cases:
        if not isinstance(item, dict):
            fail(f"{MANIFEST_PATH}: data_repair_cases item must be an object")
        case_id = require_text(item.get("case"), "data_repair_cases[].case")
        seen.add(case_id)
        require_text(item.get("trigger"), f"data_repair_cases[{case_id}].trigger")
        require_text(item.get("repair"), f"data_repair_cases[{case_id}].repair")
        require_text(item.get("idempotency_key"), f"data_repair_cases[{case_id}].idempotency_key")
        if item.get("audit_required") is not True:
            fail(f"{MANIFEST_PATH}: {case_id} audit_required must be true")

    missing = REQUIRED_REPAIR_CASES - seen
    if missing:
        fail(f"{MANIFEST_PATH}: data_repair_cases missing {sorted(missing)}")


def validate_docs_and_scripts() -> None:
    for path in (RELEASE_GOVERNANCE_DOC, ACTIVE_README, CURRENT_README, RELEASE_GATE_DOC, MAKEFILE, ACTIVE_CONTRACT_SCRIPT, GOVERNANCE_SCRIPT):
        if not path.exists():
            fail(f"missing {path}")

    release_doc = RELEASE_GOVERNANCE_DOC.read_text(encoding="utf-8")
    for token in (
        "configs/release/governance.json",
        "tests/contract/validate_release_governance_manifest.py",
        "make release-governance-gate",
        "Release Governance Manifest Gate",
    ):
        if token not in release_doc:
            fail(f"{RELEASE_GOVERNANCE_DOC}: missing {token}")

    active_readme = ACTIVE_README.read_text(encoding="utf-8")
    if "Release Governance Manifest" not in active_readme or "make release-governance-gate" not in active_readme:
        fail(f"{ACTIVE_README}: missing release governance gate references")

    current_readme = CURRENT_README.read_text(encoding="utf-8")
    if "Release Governance 开发已启动" not in current_readme:
        fail(f"{CURRENT_README}: missing Release Governance status")

    release_gate_text = RELEASE_GATE_DOC.read_text(encoding="utf-8")
    for token in ("Release Governance Manifest", "configs/release/governance.json", "make release-governance-gate"):
        if token not in release_gate_text:
            fail(f"{RELEASE_GATE_DOC}: missing {token}")

    makefile_text = MAKEFILE.read_text(encoding="utf-8")
    if "release-governance-gate" not in makefile_text:
        fail(f"{MAKEFILE}: missing release-governance-gate target")

    script_text = ACTIVE_CONTRACT_SCRIPT.read_text(encoding="utf-8")
    if "validate_release_governance_manifest.py" not in script_text:
        fail(f"{ACTIVE_CONTRACT_SCRIPT}: release governance validator not wired")


def main() -> None:
    manifest = load_json(MANIFEST_PATH)
    validate_policy(manifest)
    validate_feature_flags(manifest)
    validate_release_gates(manifest)
    validate_observability(manifest)
    validate_rollback(manifest)
    validate_data_repair(manifest)
    validate_docs_and_scripts()
    print("release governance manifest ok")


if __name__ == "__main__":
    main()
