#!/usr/bin/env python3
"""Validate active JSON Schema contracts and mapped contract fixtures."""

from __future__ import annotations

import copy
import json
import sys
import warnings
from pathlib import Path
from typing import Any, Iterable

warnings.filterwarnings("ignore", category=DeprecationWarning)

try:
    from jsonschema import Draft202012Validator, FormatChecker, RefResolver
except ModuleNotFoundError as exc:
    raise SystemExit(
        "missing jsonschema; install contract gate dependencies with "
        "`python3 -m pip install -r requirements/contract-gates.txt`"
    ) from exc

REPO_ROOT = Path(__file__).resolve().parents[2]
SCHEMA_ROOTS = [REPO_ROOT / "api" / "schemas", REPO_ROOT / "api" / "agui"]
FIXTURE_ROOT = REPO_ROOT / "tests" / "fixtures" / "contracts"
AGUI_FIXTURE_DIR = FIXTURE_ROOT / "agui"


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(1)


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        fail(f"missing {path}")
    except json.JSONDecodeError as exc:
        fail(f"{path}: invalid JSON: {exc}")


def strip_ids(value: Any) -> Any:
    """Keep filesystem-relative refs stable by ignoring published $id bases."""
    if isinstance(value, dict):
        return {key: strip_ids(item) for key, item in value.items() if key != "$id"}
    if isinstance(value, list):
        return [strip_ids(item) for item in value]
    return value


def all_schema_paths() -> list[Path]:
    paths: list[Path] = []
    for root in SCHEMA_ROOTS:
        paths.extend(root.rglob("*.json"))
    return sorted(paths)


SCHEMAS: dict[Path, dict[str, Any]] = {path: strip_ids(load_json(path)) for path in all_schema_paths()}
SCHEMA_STORE = {path.as_uri(): schema for path, schema in SCHEMAS.items()}


def schema_path(relative: str) -> Path:
    path = REPO_ROOT / relative
    if path not in SCHEMAS:
        fail(f"schema not loaded: {path}")
    return path


def validator_for(path: Path) -> Draft202012Validator:
    schema = SCHEMAS[path]
    return Draft202012Validator(
        schema,
        resolver=RefResolver(base_uri=path.as_uri(), referrer=schema, store=SCHEMA_STORE),
        format_checker=FormatChecker(),
    )


def path_label(parts: Iterable[Any]) -> str:
    label = "$"
    for part in parts:
        if isinstance(part, int):
            label += f"[{part}]"
        else:
            label += f".{part}"
    return label


def validate_instance(schema: Path, instance: Any, label: str) -> None:
    validator = validator_for(schema)
    errors = sorted(validator.iter_errors(instance), key=lambda error: list(error.path))
    if errors:
        first = errors[0]
        fail(f"{label}: schema {schema.relative_to(REPO_ROOT)} failed at {path_label(first.path)}: {first.message}")


def validate_schema_documents() -> None:
    for path, schema in SCHEMAS.items():
        try:
            Draft202012Validator.check_schema(schema)
        except Exception as exc:  # pragma: no cover - jsonschema exception classes vary by version.
            fail(f"{path}: invalid JSON Schema: {exc}")
    print(f"json schema documents ok {len(SCHEMAS)} files")


def validate_schema_examples() -> None:
    count = 0
    for path, schema in SCHEMAS.items():
        for index, example in enumerate(schema.get("examples", []), 1):
            validate_instance(path, example, f"{path.relative_to(REPO_ROOT)} examples[{index}]")
            count += 1
    print(f"json schema examples ok {count} examples")


def select(data: Any, *segments: Any) -> Any:
    current = data
    for segment in segments:
        current = current[segment]
    return current


def validate_value(path: Path, data: Any, schema: Path, *segments: Any) -> None:
    value = select(data, *segments) if segments else data
    label = f"{path.relative_to(REPO_ROOT)}:{'.'.join(map(str, segments)) if segments else '$'}"
    validate_instance(schema, value, label)


def validate_each(path: Path, values: Iterable[Any], schema: Path, label: str) -> None:
    for index, value in enumerate(values):
        validate_instance(schema, value, f"{path.relative_to(REPO_ROOT)}:{label}[{index}]")


def validate_router_fixtures() -> int:
    schema = schema_path("api/schemas/router/router-decision.v1.schema.json")
    count = 0
    for path in sorted((FIXTURE_ROOT / "router").glob("*.json")):
        validate_value(path, load_json(path), schema, "expected", "router_decision")
        count += 1
    return count


def validate_agui_fixtures() -> int:
    envelope_schema = schema_path("api/agui/agent-workbench-events.schema.json")
    payload_schemas = {
        schema.get("title"): path
        for path, schema in SCHEMAS.items()
        if path.parent == REPO_ROOT / "api" / "agui" / "events" and schema.get("title")
    }
    count = 0
    for path in sorted(AGUI_FIXTURE_DIR.glob("*.json")):
        events = load_json(path)
        if not isinstance(events, list):
            fail(f"{path}: AG-UI fixture must be an array")
        for index, event in enumerate(events):
            validate_instance(envelope_schema, event, f"{path.relative_to(REPO_ROOT)}[{index}]")
            payload_schema = payload_schemas.get(event.get("payload_schema_version"))
            if payload_schema is not None:
                validate_instance(payload_schema, event.get("payload"), f"{path.relative_to(REPO_ROOT)}[{index}].payload")
            count += 1
    return count


def validate_board_fixtures() -> int:
    board = schema_path("api/schemas/board/creative-board.v1.schema.json")
    element = schema_path("api/schemas/board/creative-element.v1.schema.json")
    patch = schema_path("api/schemas/board/board-patch.v1.schema.json")
    snapshot = schema_path("api/schemas/board/board-snapshot.v1.schema.json")

    count = 0
    create_path = FIXTURE_ROOT / "board" / "create_city_tourism_board.json"
    create = load_json(create_path)
    validate_value(create_path, create, board, "expected", "creative_board")
    validate_each(create_path, create["expected"]["elements"], element, "expected.elements")
    count += 1 + len(create["expected"]["elements"])

    replay_path = FIXTURE_ROOT / "board" / "patch_replay_storyboard.json"
    replay = load_json(replay_path)
    validate_value(replay_path, replay, snapshot, "initial_snapshot")
    validate_each(replay_path, replay["patches"], patch, "patches")
    validate_value(replay_path, replay, snapshot, "expected_snapshot")
    count += 2 + len(replay["patches"])

    approve_path = FIXTURE_ROOT / "board" / "approve_board_for_toolplan.json"
    approve = load_json(approve_path)
    validate_value(approve_path, approve, board, "board_before")
    validate_value(approve_path, approve, patch, "approval_patch")
    validate_value(approve_path, approve, board, "board_after")
    count += 3
    return count


def validate_graph_fixtures() -> int:
    generic_graph = schema_path("api/schemas/graph/generic-creation-graph.v1.schema.json")
    template = schema_path("api/schemas/graph/graph-template.v1.schema.json")
    plan = schema_path("api/schemas/graph/graph-plan.v1.schema.json")
    checkpoint_schema = schema_path("api/schemas/graph/graph-checkpoint.v1.schema.json")

    path = FIXTURE_ROOT / "graph" / "generic_creation_graph_plan.json"
    data = load_json(path)
    validate_value(path, data, generic_graph, "generic_creation_graph")
    validate_value(path, data, template, "graph_template")
    validate_value(path, data, plan, "graph_plan")

    checkpoint_path = FIXTURE_ROOT / "graph" / "interrupt_resume_checkpoint.json"
    checkpoint = load_json(checkpoint_path)
    validate_value(checkpoint_path, checkpoint, checkpoint_schema, "checkpoint")
    return 4


def validate_tool_credit_asset_fixtures() -> int:
    tool_plan = schema_path("api/schemas/tool/tool-plan.v1.schema.json")
    tool_task = schema_path("api/schemas/tool/tool-task.v1.schema.json")
    tool_result = schema_path("api/schemas/tool/tool-result.v1.schema.json")
    credit = schema_path("api/schemas/credit/credit-freeze.v1.schema.json")
    credit_lot = schema_path("api/schemas/credit/credit-lot.v1.schema.json")
    recharge_order = schema_path("api/schemas/credit/recharge-order.v1.schema.json")
    mock_payment = schema_path("api/schemas/credit/mock-payment.v1.schema.json")
    generation_payload = schema_path("api/agui/events/cost_disclosure.generation.presented.schema.json")

    count = 0
    toolplan_path = FIXTURE_ROOT / "toolplan" / "city_video_toolplan.json"
    toolplan = load_json(toolplan_path)
    validate_value(toolplan_path, toolplan, tool_plan, "tool_plan")
    validate_value(toolplan_path, toolplan, generation_payload, "agui_event_payload")
    count += 2

    for path in sorted((FIXTURE_ROOT / "credit").glob("*.json")):
        data = load_json(path)
        for key, value in data.items():
            if key.startswith("hold_after_"):
                validate_instance(credit, value, f"{path.relative_to(REPO_ROOT)}:{key}")
                count += 1
        if "credit_lot" in data:
            validate_value(path, data, credit_lot, "credit_lot")
            count += 1
        if "recharge_order" in data:
            validate_value(path, data, recharge_order, "recharge_order")
            count += 1
        if "mock_payment_transaction" in data:
            validate_value(path, data, mock_payment, "mock_payment_transaction")
            count += 1
        if "tool_task_failure" in data:
            validate_value(path, data, tool_task, "tool_task_failure")
            count += 1

    asset_path = FIXTURE_ROOT / "asset" / "partial_commit_success.json"
    asset = load_json(asset_path)
    validate_value(asset_path, asset, tool_result, "tool_result")
    count += 1

    tool_path = FIXTURE_ROOT / "tool" / "provider_async_resume.json"
    tool = load_json(tool_path)
    validate_value(tool_path, tool, tool_task, "tool_task_before_restart")
    validate_value(tool_path, tool, tool_task, "tool_task_after_resume")
    count += 2
    return count


def validate_marketplace_billing_fixtures() -> int:
    schemas = {
        "skill_package": schema_path("api/schemas/skill/skill-package.v1.schema.json"),
        "skill_version": schema_path("api/schemas/skill/skill-version.v1.schema.json"),
        "pricing_policy": schema_path("api/schemas/skill/skill-pricing-policy.v1.schema.json"),
        "listing": schema_path("api/schemas/skill/marketplace-listing.v1.schema.json"),
        "installation": schema_path("api/schemas/skill/skill-installation.v1.schema.json"),
        "usage": schema_path("api/schemas/skill/skill-usage-record.v1.schema.json"),
        "settlement": schema_path("api/schemas/settlement/skill-settlement.v1.schema.json"),
    }
    count = 0

    publish_path = FIXTURE_ROOT / "marketplace" / "creator_submit_approve_publish.json"
    publish = load_json(publish_path)
    for key in ("skill_package", "skill_version", "pricing_policy", "listing"):
        validate_value(publish_path, publish, schemas[key], key)
        count += 1

    personal_path = FIXTURE_ROOT / "marketplace" / "user_install_latest_personal.json"
    validate_value(personal_path, load_json(personal_path), schemas["installation"], "installation")
    count += 1

    enterprise_path = FIXTURE_ROOT / "marketplace" / "enterprise_install_pinned_upgrade.json"
    enterprise = load_json(enterprise_path)
    validate_value(enterprise_path, enterprise, schemas["installation"], "initial_installation")
    validate_value(enterprise_path, enterprise, schemas["installation"], "installation_after_upgrade")
    count += 2

    charge_path = FIXTURE_ROOT / "billing" / "skill_usage_precreate_confirm_charge.json"
    charge = load_json(charge_path)
    validate_value(charge_path, charge, schemas["usage"], "usage_after_create")
    validate_value(charge_path, charge, schemas["usage"], "usage_after_charge")
    validate_value(charge_path, charge, schemas["settlement"], "settlement")
    count += 3

    refund_path = FIXTURE_ROOT / "billing" / "skill_usage_refund_reversal.json"
    refund = load_json(refund_path)
    validate_value(refund_path, refund, schemas["usage"], "usage_before_refund")
    validate_value(refund_path, refund, schemas["usage"], "usage_after_refund")
    validate_value(refund_path, refund, schemas["settlement"], "settlement_after_reverse")
    count += 3
    return count


def validate_contract_fixtures() -> None:
    counts = {
        "router": validate_router_fixtures(),
        "agui": validate_agui_fixtures(),
        "board": validate_board_fixtures(),
        "graph": validate_graph_fixtures(),
        "tool_credit_asset": validate_tool_credit_asset_fixtures(),
        "marketplace_billing": validate_marketplace_billing_fixtures(),
    }
    total = sum(counts.values())
    detail = ", ".join(f"{key}={value}" for key, value in counts.items())
    print(f"json schema contract fixtures ok {total} instances ({detail})")


def main() -> None:
    validate_schema_documents()
    validate_schema_examples()
    validate_contract_fixtures()
    print("json schema contract validation ok")


if __name__ == "__main__":
    main()
