#!/usr/bin/env python3
"""Validate PR-3 Tool/Credit/Asset contract artifacts without external packages."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
SCHEMA_DIRS = [
    REPO_ROOT / "api" / "schemas" / "tool",
    REPO_ROOT / "api" / "schemas" / "credit",
    REPO_ROOT / "api" / "schemas" / "asset",
]
AGUI_EVENTS_DIR = REPO_ROOT / "api" / "agui" / "events"
FIXTURE_ROOT = REPO_ROOT / "tests" / "fixtures" / "contracts"
THRIFT_DIR = REPO_ROOT / "api" / "thrift"
MIGRATION_DIR = REPO_ROOT / "db" / "migrations" / "iterations" / "2026-07-01-tool-credit-asset-contracts"


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(1)


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        fail(f"{path}: invalid JSON: {exc}")


def required_fields(schema_name: str) -> set[str]:
    for directory in SCHEMA_DIRS:
        path = directory / schema_name
        if path.exists():
            return set(load_json(path).get("required", []))
    fail(f"missing schema {schema_name}")


def require_keys(path: Path, data: dict[str, Any], required: set[str], label: str) -> None:
    missing = required - set(data)
    if missing:
        fail(f"{path}: {label} missing {sorted(missing)}")


def validate_json_artifacts() -> None:
    paths: list[Path] = []
    for directory in SCHEMA_DIRS:
        paths.extend(directory.glob("*.json"))
    paths.extend(
        [
            AGUI_EVENTS_DIR / "cost_disclosure.generation.presented.schema.json",
            AGUI_EVENTS_DIR / "tool.task.updated.schema.json",
            AGUI_EVENTS_DIR / "asset.commit.updated.schema.json",
        ]
    )
    for name in ("toolplan", "credit", "asset", "tool"):
        paths.extend((FIXTURE_ROOT / name).glob("*.json"))
    for path in paths:
        if not path.exists():
            fail(f"missing {path}")
        load_json(path)
    print(f"pr3 json ok {len(paths)} files")


def validate_thrift() -> None:
    required = {
        "business_credit_service.thrift": [
            'include "business_agent_service.thrift"',
            "EstimateToolCredits",
            "FreezeCredits",
            "CommitCredits",
            "ReleaseCredits",
            "RequestMeta",
        ],
        "business_asset_service.thrift": [
            'include "business_agent_service.thrift"',
            "CommitGeneratedAssets",
            "RequestMeta",
        ],
        "business_tool_service.thrift": [
            "GetToolPricing",
            "pricing_digest",
        ],
    }
    for filename, tokens in required.items():
        text = (THRIFT_DIR / filename).read_text(encoding="utf-8")
        for token in tokens:
            if token not in text:
                fail(f"{filename}: missing {token}")
    print("pr3 thrift surface ok")


def validate_toolplan_fixture() -> None:
    path = FIXTURE_ROOT / "toolplan" / "city_video_toolplan.json"
    data = load_json(path)
    tool_plan = data["tool_plan"]
    require_keys(path, tool_plan, required_fields("tool-plan.v1.schema.json"), "tool_plan")
    if data["precondition"]["board_status"] != "approved":
        fail(f"{path}: ToolPlan must require approved board")
    if tool_plan["board_version"] != data["precondition"]["board_version"]:
        fail(f"{path}: ToolPlan board_version must match approved board")
    if tool_plan["graph_plan_id"] != data["precondition"]["graph_plan_id"]:
        fail(f"{path}: ToolPlan graph_plan_id must match GraphPlan")
    if tool_plan["estimated_credits"] <= 0:
        fail(f"{path}: estimated_credits must be positive")
    if data["agui_event_payload"]["tool_plan_digest"] != tool_plan["tool_plan_digest"]:
        fail(f"{path}: AG-UI tool_plan_digest must match ToolPlan")
    print("pr3 toolplan fixture ok")


def validate_credit_fixtures() -> None:
    required = required_fields("credit-freeze.v1.schema.json")

    success_path = FIXTURE_ROOT / "credit" / "freeze_commit_success.json"
    success = load_json(success_path)
    require_keys(success_path, success["hold_after_freeze"], required, "hold_after_freeze")
    require_keys(success_path, success["hold_after_commit"], required, "hold_after_commit")
    if success["hold_after_freeze"]["status"] != "frozen":
        fail(f"{success_path}: freeze status must be frozen")
    if success["hold_after_commit"]["status"] != "committed":
        fail(f"{success_path}: commit status must be committed")
    if success["hold_after_commit"]["committed_credits"] != success["hold_after_freeze"]["frozen_credits"]:
        fail(f"{success_path}: committed credits must equal frozen credits")

    release_path = FIXTURE_ROOT / "credit" / "freeze_release_on_tool_failure.json"
    release = load_json(release_path)
    require_keys(release_path, release["hold_after_freeze"], required, "hold_after_freeze")
    require_keys(release_path, release["hold_after_release"], required, "hold_after_release")
    require_keys(release_path, release["tool_task_failure"], required_fields("tool-task.v1.schema.json"), "tool_task_failure")
    if release["tool_task_failure"]["status"] != "failed":
        fail(f"{release_path}: tool task must fail")
    if release["hold_after_release"]["status"] != "released":
        fail(f"{release_path}: hold must be released")
    if release["hold_after_release"]["released_credits"] != release["hold_after_freeze"]["frozen_credits"]:
        fail(f"{release_path}: released credits must equal frozen credits")
    print("pr3 credit fixtures ok")


def validate_asset_and_tool_fixtures() -> None:
    result_required = required_fields("tool-result.v1.schema.json")
    asset_required = required_fields("generated-asset.v1.schema.json")
    task_required = required_fields("tool-task.v1.schema.json")

    asset_path = FIXTURE_ROOT / "asset" / "partial_commit_success.json"
    asset = load_json(asset_path)
    require_keys(asset_path, asset["tool_result"], result_required, "tool_result")
    for item in asset["tool_result"]["assets"]:
        require_keys(asset_path, item, asset_required, "generated_asset")
    if asset["tool_result"]["status"] != "partially_succeeded":
        fail(f"{asset_path}: tool result must be partially_succeeded")
    if asset["commit_response"]["status"] != "PARTIALLY_COMMITTED":
        fail(f"{asset_path}: commit response must be PARTIALLY_COMMITTED")
    if not asset["billing_rule"]["failed_assets_must_not_be_charged"]:
        fail(f"{asset_path}: failed assets must not be charged")

    tool_path = FIXTURE_ROOT / "tool" / "provider_async_resume.json"
    tool = load_json(tool_path)
    require_keys(tool_path, tool["tool_task_before_restart"], task_required, "tool_task_before_restart")
    require_keys(tool_path, tool["tool_task_after_resume"], task_required, "tool_task_after_resume")
    if tool["redis_stream_event"]["stream"] != "tool:task:completed":
        fail(f"{tool_path}: provider completion must use tool:task:completed stream")
    if tool["tool_task_after_resume"]["status"] != "succeeded":
        fail(f"{tool_path}: resumed task must succeed")
    print("pr3 asset/tool fixtures ok")


def validate_migrations() -> None:
    required_tables = {
        MIGRATION_DIR / "agent" / "0001_agent_tool_plan_task.up.sql": [
            "tool_plans",
            "tool_tasks",
        ],
        MIGRATION_DIR / "business" / "0001_business_credit_asset_tool.up.sql": [
            "credit_holds",
            "credit_ledger_entries",
            "tool_pricing_snapshots",
            "generated_assets",
            "asset_commit_records",
        ],
    }
    for path, tables in required_tables.items():
        if not path.exists():
            fail(f"missing migration {path}")
        text = path.read_text(encoding="utf-8").lower()
        for table in tables:
            if f"create table if not exists {table}" not in text:
                fail(f"{path}: missing create table {table}")
        if re.search(r"\breferences\b", text):
            fail(f"{path}: migration must not declare database-level foreign keys")
    for path in (MIGRATION_DIR / "agent").glob("*.down.sql"):
        if "drop table if exists" not in path.read_text(encoding="utf-8").lower():
            fail(f"{path}: missing drop table")
    for path in (MIGRATION_DIR / "business").glob("*.down.sql"):
        if "drop table if exists" not in path.read_text(encoding="utf-8").lower():
            fail(f"{path}: missing drop table")
    print("pr3 migration static guard ok")


def main() -> None:
    validate_json_artifacts()
    validate_thrift()
    validate_toolplan_fixture()
    validate_credit_fixtures()
    validate_asset_and_tool_fixtures()
    validate_migrations()
    print("pr3 contract validation ok")


if __name__ == "__main__":
    main()
