#!/usr/bin/env python3
"""Validate PR-1 Contract-first active artifacts without external packages."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
ROUTER_SCHEMA = REPO_ROOT / "api" / "schemas" / "router" / "router-decision.v1.schema.json"
AGUI_ENVELOPE_SCHEMA = REPO_ROOT / "api" / "agui" / "agent-workbench-events.schema.json"
AGUI_EVENTS_DIR = REPO_ROOT / "api" / "agui" / "events"
ROUTER_FIXTURE_DIR = REPO_ROOT / "tests" / "fixtures" / "contracts" / "router"
AGUI_FIXTURE_DIR = REPO_ROOT / "tests" / "fixtures" / "contracts" / "agui"


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(1)


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        fail(f"{path}: invalid JSON: {exc}")


def validate_all_json() -> None:
    paths = (
        list((REPO_ROOT / "api" / "schemas").rglob("*.json"))
        + list((REPO_ROOT / "api" / "agui").rglob("*.json"))
        + list((REPO_ROOT / "tests" / "fixtures" / "contracts").rglob("*.json"))
    )
    for path in paths:
        load_json(path)
    print(f"json ok {len(paths)} files")


def validate_router_fixtures() -> None:
    schema = load_json(ROUTER_SCHEMA)
    required = set(schema["required"])
    decisions = set(schema["properties"]["decision"]["enum"])
    skill_sources = {value for value in schema["properties"]["skill_source"]["enum"] if value is not None}
    entitlements = {value for value in schema["properties"]["entitlement_status"]["enum"] if value is not None}

    fixture_paths = sorted(ROUTER_FIXTURE_DIR.glob("*.json"))
    if not fixture_paths:
        fail(f"{ROUTER_FIXTURE_DIR}: no router fixtures")

    for path in fixture_paths:
        data = load_json(path)
        decision = data.get("expected", {}).get("router_decision")
        if not isinstance(decision, dict):
            fail(f"{path}: missing expected.router_decision")

        missing = required - set(decision)
        if missing:
            fail(f"{path}: router_decision missing {sorted(missing)}")

        if decision["schema_version"] != "router_decision.v1":
            fail(f"{path}: bad router schema_version")
        if decision["decision"] not in decisions:
            fail(f"{path}: invalid decision {decision['decision']!r}")
        if decision["safe_to_execute"] is not False:
            fail(f"{path}: safe_to_execute must be false")
        if decision.get("skill_source") is not None and decision["skill_source"] not in skill_sources:
            fail(f"{path}: invalid skill_source {decision['skill_source']!r}")
        if decision.get("entitlement_status") is not None and decision["entitlement_status"] not in entitlements:
            fail(f"{path}: invalid entitlement_status {decision['entitlement_status']!r}")
        if not isinstance(decision["candidate_skills"], list):
            fail(f"{path}: candidate_skills must be array")
        if not isinstance(decision["marketplace_candidates"], list):
            fail(f"{path}: marketplace_candidates must be array")

        print(f"router fixture ok {path.relative_to(REPO_ROOT)}")


def validate_agui_fixtures() -> None:
    envelope = load_json(AGUI_ENVELOPE_SCHEMA)
    envelope_required = set(envelope["required"])
    payload_required: dict[str, set[str]] = {}

    for path in AGUI_EVENTS_DIR.glob("*.schema.json"):
        schema = load_json(path)
        payload_required[schema["title"].removesuffix(".v1") + ".v1"] = set(schema.get("required", []))

    event_type_re = re.compile(r"^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$")
    payload_version_re = re.compile(r"^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+\.v[0-9]+$")

    fixture_paths = sorted(AGUI_FIXTURE_DIR.glob("*.json"))
    if not fixture_paths:
        fail(f"{AGUI_FIXTURE_DIR}: no AG-UI fixtures")

    for path in fixture_paths:
        events = load_json(path)
        if not isinstance(events, list) or not events:
            fail(f"{path}: fixture must be a non-empty array")

        seen_dedupe: set[str] = set()
        for index, event in enumerate(events, 1):
            missing = envelope_required - set(event)
            if missing:
                fail(f"{path}:{index}: missing envelope fields {sorted(missing)}")

            if event["schema_version"] != "agui.event.v1":
                fail(f"{path}:{index}: bad schema_version")
            if event["seq"] != index:
                fail(f"{path}: seq must be continuous from 1, got {event['seq']} at position {index}")
            if event["dedupe_key"] in seen_dedupe:
                fail(f"{path}:{index}: duplicate dedupe_key {event['dedupe_key']!r}")
            seen_dedupe.add(event["dedupe_key"])
            if not event_type_re.match(event["event_type"]):
                fail(f"{path}:{index}: invalid event_type {event['event_type']!r}")
            if not payload_version_re.match(event["payload_schema_version"]):
                fail(f"{path}:{index}: invalid payload_schema_version {event['payload_schema_version']!r}")

            payload = event["payload"]
            if not isinstance(payload, dict):
                fail(f"{path}:{index}: payload must be object")
            required = payload_required.get(event["payload_schema_version"])
            if required:
                missing_payload = required - set(payload)
                if missing_payload:
                    fail(f"{path}:{index}: payload missing {sorted(missing_payload)}")

        print(f"agui fixture ok {path.relative_to(REPO_ROOT)}")


def main() -> None:
    validate_all_json()
    validate_router_fixtures()
    validate_agui_fixtures()
    print("pr1 contract validation ok")


if __name__ == "__main__":
    main()
