#!/usr/bin/env python3
"""Validate toolchain baseline AG-UI fixture shape and schema coverage."""

from __future__ import annotations

import json
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parent / "fixtures"
SCHEMA_PATH = Path(__file__).resolve().parents[3] / "api" / "agui" / "agent-workbench-events.schema.json"

TOP_LEVEL_REQUIRED = {
    "event_id",
    "type",
    "session_id",
    "run_id",
    "project_id",
    "space_id",
    "actor_user_id",
    "sequence",
    "timestamp",
    "component",
    "trace_id",
    "payload",
}

PAYLOAD_REQUIRED = {'agent.run.started': {'project_id', 'started_at', 'session_id', 'run_status'}, 'agent.run.completed': {'snapshot_version', 'completed_at', 'run_status', 'final_message_id', 'last_event_sequence'}, 'agent.run.cancelled': {'cancelled_at', 'run_status', 'cancel_reason', 'released_points', 'last_event_sequence'}, 'agent.thinking.started': {'thinking_id', 'display_mode', 'started_at'}, 'agent.thinking.delta': {'thinking_id', 'text_delta', 'delta_index'}, 'agent.thinking.completed': {'summary_digest', 'thinking_id', 'completed_at'}, 'agent.message.delta': {'text_delta', 'content_type', 'role', 'message_id'}, 'agent.message.completed': {'final_text_digest', 'token_usage_summary', 'role', 'content_type', 'message_id'}, 'agent.skill.selected': {'skill_id', 'skill_name', 'skill_scope', 'skill_version', 'matched_reason'}, 'agent.skill.missing': {'fallback_mode', 'matched_tags', 'user_message'}, 'platform.tags.updated': {'tags'}, 'chat.controls.requested': {'controls'}, 'chat.controls.locked': {'locked_fields', 'locked_reason', 'confirmation_id'}, 'safety.prompt.evaluating': {'checked_target', 'scene', 'target_ref_id', 'target_type'}, 'safety.prompt.evaluated': {'expires_at', 'safety_evidence_id', 'safety_status', 'policy_version'}, 'safety.prompt.blocked': {'retryable', 'support_trace_id', 'safety_status', 'user_message'}, 'safety.prompt.failed': {'retryable', 'safety_status', 'error_code', 'support_trace_id', 'user_message'}, 'credits.estimated': {'estimate_points', 'credit_account_scope', 'available_points', 'credit_account_id', 'pricing_snapshot_id', 'expires_soon_points'}, 'confirmation.required': {'summary', 'expires_at', 'interrupt_id', 'title', 'confirmation_id', 'risks', 'points', 'actions'}, 'confirmation.accepted': {'interrupt_id', 'action', 'confirmation_id', 'next_step', 'payload_digest', 'accepted_at'}, 'confirmation.rejected': {'interrupt_id', 'action', 'confirmation_id', 'rejected_at', 'reason_code', 'run_status'}, 'resume.accepted': {'interrupt_id', 'requires_safety_evaluation', 'next_step', 'resume_action', 'accepted_at'}, 'credits.frozen': {'freeze_id', 'expires_at', 'frozen_points'}, 'credits.charged': {'released_points', 'charged_points'}, 'credits.released': {'freeze_id', 'released_points', 'reason'}, 'credits.insufficient': {'available_points', 'estimate_points', 'retryable', 'user_message'}, 'tool.call.started': {'tool_type', 'tool_name', 'tool_call_id', 'risk_level', 'timeout_ms'}, 'tool.call.progress': {'progress', 'tool_call_id', 'status', 'partial_summary', 'current_step'}, 'tool.call.completed': {'charged_estimate_item_ids', 'result_summary', 'tool_call_id', 'status', 'artifact_refs'}, 'tool.call.failed': {'retryable', 'tool_call_id', 'error_code', 'support_trace_id', 'user_message'}, 'generation.progress': {'partial_completed', 'progress', 'status', 'resource_type', 'task_id'}, 'generation.artifact.completed': {'name', 'artifact_id', 'metadata_summary', 'elements_summary', 'resource_type'}, 'asset.save.started': {'freeze_id', 'artifact_id', 'estimate_id', 'resource_type', 'project_id'}, 'asset.save.completed': {'artifact_id', 'asset_id', 'save_status', 'resource_type', 'elements', 'downloadable'}, 'asset.save.failed': {'retryable', 'artifact_id', 'error_code', 'released_points', 'user_message'}, 'workspace.assets.updated': {'last_asset_id', 'asset_count', 'assets', 'version', 'mode'}, 'workspace.blackboard.updated': {'storyline', 'elements', 'version', 'active_node_id', 'mode'}, 'process.snapshot.saved': {'snapshot_version', 'blackboard_version', 'messages_count', 'snapshot_id', 'assets_count', 'last_event_sequence'}, 'project.archived.blocked': {'allowed_actions', 'project_status', 'creative_allowed', 'read_only_reason', 'user_message'}, 'agent.run.failed': {'error_type', 'retryable', 'error_code', 'support_trace_id', 'user_message'}}

EXPECTED_VALUES = {('agent.run.completed', 'run_status'): 'completed', ('agent.run.cancelled', 'run_status'): 'cancelled', ('agent.thinking.started', 'display_mode'): 'typewriter', ('agent.skill.missing', 'fallback_mode'): 'text_model', ('safety.prompt.evaluated', 'safety_status'): 'passed', ('safety.prompt.failed', 'safety_status'): 'failed', ('confirmation.accepted', 'action'): 'confirm', ('confirmation.rejected', 'action'): 'reject', ('tool.call.completed', 'status'): 'completed', ('project.archived.blocked', 'project_status'): 'archived', ('project.archived.blocked', 'creative_allowed'): False}

EXPECTED_FIXTURES = {
    "normal_generation_success.json",
    "safety_blocked.json",
    "credit_insufficient.json",
    "confirmation_rejected.json",
    "additional_input_resume_safety.json",
    "asset_save_failed_release.json",
    "project_archived_running.json",
    "sse_replay_gap.json",
    "unknown_event_ignored.json",
}

NORMAL_GENERATION_REQUIRED_EVENTS = {
    "agent.thinking.started",
    "agent.thinking.delta",
    "agent.thinking.completed",
    "platform.tags.updated",
    "chat.controls.requested",
    "chat.controls.locked",
    "agent.message.delta",
    "agent.message.completed",
    "agent.skill.selected",
    "safety.prompt.evaluating",
    "safety.prompt.evaluated",
    "credits.estimated",
    "confirmation.required",
    "confirmation.accepted",
    "credits.frozen",
    "tool.call.started",
    "tool.call.progress",
    "tool.call.completed",
    "generation.progress",
    "generation.artifact.completed",
    "asset.save.started",
    "asset.save.completed",
    "credits.charged",
    "workspace.assets.updated",
    "workspace.blackboard.updated",
    "process.snapshot.saved",
    "agent.run.completed",
}


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(1)


def load_events(path: Path) -> list[dict]:
    try:
        events = json.loads(path.read_text())
    except json.JSONDecodeError as exc:
        fail(f"{path}: invalid JSON: {exc}")
    if not isinstance(events, list) or not events:
        fail(f"{path}: fixture must be a non-empty event array")
    return events


def schema_event_coverage() -> None:
    schema = json.loads(SCHEMA_PATH.read_text())
    seen: dict[str, str] = {}
    for item in schema.get("allOf", []):
        event_type = item.get("if", {}).get("properties", {}).get("type", {}).get("const")
        payload_ref = item.get("then", {}).get("properties", {}).get("payload", {}).get("$ref")
        if event_type and payload_ref:
            seen[event_type] = payload_ref.rsplit("/", 1)[-1]
    missing_events = set(PAYLOAD_REQUIRED) - set(seen)
    if missing_events:
        fail(f"AG-UI schema missing event payload branches: {sorted(missing_events)}")
    defs = schema.get("$defs", {})
    for event_type, required in PAYLOAD_REQUIRED.items():
        payload_name = seen[event_type]
        actual_required = set(defs.get(payload_name, {}).get("required", []))
        missing_fields = required - actual_required
        if missing_fields:
            fail(f"AG-UI schema {event_type} missing required payload fields {sorted(missing_fields)}")


def validate_file(path: Path) -> set[str]:
    events = load_events(path)
    sequences: list[int] = []
    has_unknown = False
    event_types: set[str] = set()
    for idx, event in enumerate(events):
        missing = TOP_LEVEL_REQUIRED - set(event)
        if missing:
            fail(f"{path}:{idx}: missing top-level fields {sorted(missing)}")

        payload = event["payload"]
        if not isinstance(payload, dict):
            fail(f"{path}:{idx}: payload must be an object")
        if "account_type" in payload:
            fail(f"{path}:{idx}: use credit_account_scope, not account_type")

        event_type = event["type"]
        event_types.add(event_type)
        required = PAYLOAD_REQUIRED.get(event_type)
        if required is None:
            has_unknown = True
        else:
            missing_payload = required - set(payload)
            if missing_payload:
                fail(f"{path}:{idx}: {event_type} missing payload fields {sorted(missing_payload)}")
            for (const_event, field), expected in EXPECTED_VALUES.items():
                if const_event == event_type and field in payload and payload[field] != expected:
                    fail(f"{path}:{idx}: {event_type}.{field} expected {expected!r}, got {payload[field]!r}")

        sequence = event["sequence"]
        if not isinstance(sequence, int) or sequence < 1:
            fail(f"{path}:{idx}: sequence must be a positive integer")
        sequences.append(sequence)

    expected = list(range(1, len(events) + 1))
    if path.name == "sse_replay_gap.json":
        if sequences == expected:
            fail(f"{path}: replay gap fixture must contain a sequence gap")
    elif sequences != expected:
        fail(f"{path}: expected continuous sequence {expected}, got {sequences}")

    if path.name == "unknown_event_ignored.json" and not has_unknown:
        fail(f"{path}: expected at least one unknown event")

    if path.name == "normal_generation_success.json":
        missing = NORMAL_GENERATION_REQUIRED_EVENTS - event_types
        if missing:
            fail(f"{path}: missing normal generation canonical events {sorted(missing)}")

    if path.name == "additional_input_resume_safety.json":
        required_resume = {"resume.accepted", "safety.prompt.evaluating", "safety.prompt.evaluated"}
        missing = required_resume - event_types
        if missing:
            fail(f"{path}: missing resume safety events {sorted(missing)}")

    return event_types


def main() -> None:
    schema_event_coverage()
    fixture_names = {path.name for path in ROOT.glob("*.json")}
    missing_fixtures = EXPECTED_FIXTURES - fixture_names
    if missing_fixtures:
        fail(f"missing AG-UI fixtures: {sorted(missing_fixtures)}")
    for path in sorted(ROOT.glob("*.json")):
        validate_file(path)
        print(f"agui fixture ok {path}")


if __name__ == "__main__":
    main()
