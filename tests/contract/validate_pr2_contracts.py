#!/usr/bin/env python3
"""Validate PR-2 Agent Runtime contract artifacts without external packages."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
BOARD_SCHEMA_DIR = REPO_ROOT / "api" / "schemas" / "board"
GRAPH_SCHEMA_DIR = REPO_ROOT / "api" / "schemas" / "graph"
AGUI_EVENTS_DIR = REPO_ROOT / "api" / "agui" / "events"
BOARD_FIXTURE_DIR = REPO_ROOT / "tests" / "fixtures" / "contracts" / "board"
GRAPH_FIXTURE_DIR = REPO_ROOT / "tests" / "fixtures" / "contracts" / "graph"
OPENAPI_PATH = REPO_ROOT / "api" / "openapi" / "agent-workbench.yaml"
MIGRATION_DIR = REPO_ROOT / "db" / "migrations" / "iterations" / "2026-07-01-agent-runtime-contracts" / "agent"


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(1)


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        fail(f"{path}: invalid JSON: {exc}")


def required_fields(schema_name: str) -> set[str]:
    for directory in (BOARD_SCHEMA_DIR, GRAPH_SCHEMA_DIR):
        path = directory / schema_name
        if path.exists():
            schema = load_json(path)
            return set(schema.get("required", []))
    fail(f"missing schema {schema_name}")


def require_keys(path: Path, data: dict[str, Any], required: set[str], label: str) -> None:
    missing = required - set(data)
    if missing:
        fail(f"{path}: {label} missing {sorted(missing)}")


def validate_json_artifacts() -> None:
    paths = (
        list(BOARD_SCHEMA_DIR.glob("*.json"))
        + list(GRAPH_SCHEMA_DIR.glob("*.json"))
        + list(AGUI_EVENTS_DIR.glob("board.*.schema.json"))
        + list(AGUI_EVENTS_DIR.glob("graph.*.schema.json"))
        + list(BOARD_FIXTURE_DIR.glob("*.json"))
        + list(GRAPH_FIXTURE_DIR.glob("*.json"))
    )
    if not paths:
        fail("no PR-2 JSON artifacts found")
    for path in paths:
        load_json(path)
    print(f"pr2 json ok {len(paths)} files")


def validate_openapi() -> None:
    text = OPENAPI_PATH.read_text(encoding="utf-8")
    required_tokens = [
        "/boards/{board_id}:",
        "/boards/{board_id}/patches:",
        "/boards/{board_id}/approve:",
        "/graphs/{graph_plan_id}:",
        "event_type:",
        "payload_schema_version:",
        "seq:",
        "dedupe_key:",
    ]
    for token in required_tokens:
        if token not in text:
            fail(f"{OPENAPI_PATH}: missing {token}")
    print("pr2 openapi surface ok")


def validate_board_fixtures() -> None:
    board_required = required_fields("creative-board.v1.schema.json")
    element_required = required_fields("creative-element.v1.schema.json")
    patch_required = required_fields("board-patch.v1.schema.json")
    snapshot_required = required_fields("board-snapshot.v1.schema.json")

    create_path = BOARD_FIXTURE_DIR / "create_city_tourism_board.json"
    create_data = load_json(create_path)
    board = create_data["expected"]["creative_board"]
    require_keys(create_path, board, board_required, "creative_board")
    if board["status"] != "ready" or board["tool_plan_allowed"]:
        fail(f"{create_path}: initial board must be ready and tool_plan_allowed=false")
    for element in create_data["expected"]["elements"]:
        require_keys(create_path, element, element_required, "element")

    replay_path = BOARD_FIXTURE_DIR / "patch_replay_storyboard.json"
    replay_data = load_json(replay_path)
    require_keys(replay_path, replay_data["initial_snapshot"], snapshot_required, "initial_snapshot")
    require_keys(replay_path, replay_data["expected_snapshot"], snapshot_required, "expected_snapshot")
    for patch in replay_data["patches"]:
        require_keys(replay_path, patch, patch_required, "patch")
    if replay_data["expected_snapshot"]["version"] <= replay_data["initial_snapshot"]["version"]:
        fail(f"{replay_path}: replay snapshot version must increase")

    approve_path = BOARD_FIXTURE_DIR / "approve_board_for_toolplan.json"
    approve_data = load_json(approve_path)
    require_keys(approve_path, approve_data["approval_patch"], patch_required, "approval_patch")
    require_keys(approve_path, approve_data["board_after"], board_required, "board_after")
    if approve_data["board_after"]["status"] != "approved":
        fail(f"{approve_path}: board_after.status must be approved")
    if approve_data["board_after"]["tool_plan_allowed"] is not True:
        fail(f"{approve_path}: board_after.tool_plan_allowed must be true")

    print("pr2 board fixtures ok")


def validate_graph_fixtures() -> None:
    generic_required = required_fields("generic-creation-graph.v1.schema.json")
    template_required = required_fields("graph-template.v1.schema.json")
    plan_required = required_fields("graph-plan.v1.schema.json")
    checkpoint_required = required_fields("graph-checkpoint.v1.schema.json")

    generic_path = GRAPH_FIXTURE_DIR / "generic_creation_graph_plan.json"
    generic_data = load_json(generic_path)
    generic = generic_data["generic_creation_graph"]
    require_keys(generic_path, generic, generic_required, "generic_creation_graph")
    require_keys(generic_path, generic_data["graph_template"], template_required, "graph_template")
    require_keys(generic_path, generic_data["graph_plan"], plan_required, "graph_plan")
    if generic["marketplace_listing_id"] is not None:
        fail(f"{generic_path}: generic graph must not have marketplace_listing_id")
    if generic["pricing_policy"] != "free" or generic["usage_fee"] != 0:
        fail(f"{generic_path}: generic graph must be free")
    if generic_data["graph_plan"]["value_delivered_stage"] != "storyboard_ready":
        fail(f"{generic_path}: graph_plan value_delivered_stage must be storyboard_ready")

    checkpoint_path = GRAPH_FIXTURE_DIR / "interrupt_resume_checkpoint.json"
    checkpoint_data = load_json(checkpoint_path)
    require_keys(checkpoint_path, checkpoint_data["checkpoint"], checkpoint_required, "checkpoint")
    if checkpoint_data["checkpoint"]["resumable"] is not True:
        fail(f"{checkpoint_path}: checkpoint must be resumable")
    if checkpoint_data["expected"]["checkpoint_status_after_resume"] != "resumed":
        fail(f"{checkpoint_path}: expected resume status must be resumed")

    print("pr2 graph fixtures ok")


def validate_migrations() -> None:
    up_path = MIGRATION_DIR / "0001_agent_runtime_board_graph.up.sql"
    down_path = MIGRATION_DIR / "0001_agent_runtime_board_graph.down.sql"
    if not up_path.exists() or not down_path.exists():
        fail(f"{MIGRATION_DIR}: missing up/down migration")
    up_text = up_path.read_text(encoding="utf-8").lower()
    down_text = down_path.read_text(encoding="utf-8").lower()
    required_tables = [
        "agent_runs",
        "agent_run_events",
        "creative_boards",
        "creative_elements",
        "board_patches",
        "graph_templates",
        "graph_plans",
        "graph_checkpoints",
    ]
    for table in required_tables:
        if f"create table if not exists {table}" not in up_text:
            fail(f"{up_path}: missing create table {table}")
        if f"drop table if exists {table}" not in down_text:
            fail(f"{down_path}: missing drop table {table}")
    if re.search(r"\breferences\b", up_text):
        fail(f"{up_path}: Agent DB migration must not declare database-level foreign keys")
    print("pr2 migration static guard ok")


def main() -> None:
    validate_json_artifacts()
    validate_openapi()
    validate_board_fixtures()
    validate_graph_fixtures()
    validate_migrations()
    print("pr2 contract validation ok")


if __name__ == "__main__":
    main()
