#!/usr/bin/env python3
"""静态契约夹具门禁：不依赖 Go 工具链，校验 tests/ 夹具与门禁文档的结构不变量。

事实源以 internal/contracts/** 的 Go 校验器为准；本脚本只做可离线执行的子集，
Go 侧完整校验由 `go test ./internal/... ./services/...` 承担。
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
DIGEST = re.compile(r"^sha256:[a-f0-9]{64}$")
ERRORS: list[str] = []


def fail(message: str) -> None:
    ERRORS.append(message)


def load(path: Path):
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:  # noqa: BLE001
        fail(f"{path}: JSON 解析失败: {exc}")
        return None


def walk_digests(node, path: Path, trail: str = "") -> None:
    if isinstance(node, dict):
        for key, value in node.items():
            if isinstance(value, str) and (key.endswith("digest") or key.endswith("_digest")):
                if not DIGEST.match(value):
                    fail(f"{path}: {trail}.{key} 非法 digest: {value}")
            walk_digests(value, path, f"{trail}.{key}")
    elif isinstance(node, list):
        for index, item in enumerate(node):
            walk_digests(item, path, f"{trail}[{index}]")


def check_all_json() -> None:
    for path in sorted(REPO.glob("tests/**/*.json")):
        data = load(path)
        if data is not None:
            walk_digests(data, path)


def check_agui_sequences() -> None:
    for path in sorted(REPO.glob("tests/fixtures/contracts/agui/*.json")):
        events = load(path)
        if not isinstance(events, list) or not events:
            fail(f"{path}: AG-UI 夹具必须是非空事件数组")
            continue
        run_id = events[0].get("run_id")
        seen = set()
        for index, event in enumerate(events):
            seq = event.get("seq")
            etype = event.get("event_type", "")
            if seq != index + 1:
                fail(f"{path}: 事件 {index + 1} seq 应为 {index + 1}，实际 {seq}")
            if event.get("run_id") != run_id:
                fail(f"{path}: 事件 {index + 1} run_id 混用")
            if not str(event.get("event_id", "")).startswith("evt_"):
                fail(f"{path}: 事件 {index + 1} event_id 必须以 evt_ 开头")
            if event.get("payload_schema_version") != f"{etype}.v1":
                fail(f"{path}: 事件 {index + 1} payload_schema_version 与 event_type 不匹配")
            dedupe = f"{run_id}:{etype}:{seq}"
            if event.get("dedupe_key") != dedupe:
                fail(f"{path}: 事件 {index + 1} dedupe_key 应为 {dedupe}")
            if event.get("dedupe_key") in seen:
                fail(f"{path}: 事件 {index + 1} dedupe_key 重复")
            seen.add(event.get("dedupe_key"))
            if event.get("schema_version") != "agui.event.v1":
                fail(f"{path}: 事件 {index + 1} schema_version 必须为 agui.event.v1")


def check_tool_plan() -> None:
    path = REPO / "tests/fixtures/contracts/toolplan/city_video_toolplan.json"
    data = load(path)
    if not data:
        return
    plan = data["tool_plan"]
    total = sum(item["estimated_credits"] for item in plan["items"])
    if plan["estimated_credits"] != total:
        fail(f"{path}: estimated_credits={plan['estimated_credits']} 与 items 合计 {total} 不一致")
    if data["agui_event_payload"]["tool_plan_digest"] != plan["tool_plan_digest"]:
        fail(f"{path}: AG-UI payload 与 ToolPlan digest 不一致")
    pre = data["precondition"]
    if pre["board_status"] != "approved" or pre["board_id"] != plan["board_id"] or pre["board_version"] != plan["board_version"]:
        fail(f"{path}: ToolPlan 前置 Board 审批约束不成立")


def check_settlements() -> None:
    for name in ("skill_usage_precreate_confirm_charge", "skill_usage_refund_reversal"):
        path = REPO / f"tests/fixtures/contracts/billing/{name}.json"
        data = load(path)
        if not data:
            continue
        for key, value in data.items():
            if isinstance(value, dict) and value.get("schema_version") == "skill_settlement.v1":
                if value["platform_fee_credits"] + value["creator_credits"] != value["gross_credits"]:
                    fail(f"{path}: {key} 分账不平")


def check_e2e_indexes() -> None:
    required = {
        "agent-workspace/city_tourism_default_skill.json": "city_tourism_default_skill_e2e_v1",
        "agent-workspace/generic_creation_graph_fallback.json": "generic_creation_graph_fallback_e2e_v1",
        "agent-workspace/tool_partial_failure_release.json": "tool_partial_failure_release_e2e_v1",
        "agent-workspace/replay_after_restart.json": "replay_after_restart_e2e_v1",
        "skill-marketplace/paid_marketplace_skill_usage.json": "paid_marketplace_skill_usage_e2e_v1",
        "skill-marketplace/enterprise_pinned_install_upgrade.json": "enterprise_pinned_install_upgrade_e2e_v1",
        "skill-marketplace/listing_suspended_guard.json": "listing_suspended_guard_e2e_v1",
        "admin-governance/refund_settlement_reverse.json": "refund_settlement_reverse_e2e_v1",
    }
    indexed: dict[str, str] = {}
    for suite_path in ("agent-workspace", "skill-marketplace", "admin-governance"):
        suite = load(REPO / f"tests/e2e/{suite_path}/scenarios.json")
        if not suite:
            continue
        if suite.get("schema_version") != "e2e_suite.v1" or suite.get("status") != "active":
            fail(f"tests/e2e/{suite_path}/scenarios.json: 必须是 active 的 e2e_suite.v1")
        if "Fixture Gate" not in suite.get("required_gates", []):
            fail(f"tests/e2e/{suite_path}/scenarios.json: 缺少 Fixture Gate")
        for fixture in suite.get("fixtures", []):
            rel = fixture["fixture_path"].removeprefix("tests/fixtures/e2e/")
            indexed[rel] = fixture["case_id"]
    for rel, case_id in required.items():
        if indexed.get(rel) != case_id:
            fail(f"e2e 套件索引缺失或 case_id 不匹配: {rel} -> {indexed.get(rel)}")
        fixture = load(REPO / "tests/fixtures/e2e" / rel)
        if not fixture:
            continue
        if fixture.get("case_id") != case_id or fixture.get("status") != "active":
            fail(f"tests/fixtures/e2e/{rel}: case_id/status 不符合 PR-5 契约")
        if "Fixture Gate" not in fixture.get("release_gates", []):
            fail(f"tests/fixtures/e2e/{rel}: 缺少 Fixture Gate")
        for reference in fixture.get("contract_references", []):
            ref = reference.get("path", "")
            if not ref.startswith("tests/fixtures/contracts/"):
                fail(f"tests/fixtures/e2e/{rel}: 契约引用必须指向 tests/fixtures/contracts: {ref}")
            elif not (REPO / ref).exists():
                fail(f"tests/fixtures/e2e/{rel}: 契约引用不存在: {ref}")


def check_fake_provider() -> None:
    behaviors = {
        "deterministic_success",
        "async_pending",
        "partial_success",
        "transient_failure",
        "terminal_failure",
        "slow_callback",
    }
    manifest = load(REPO / "tests/e2e/fake-provider/fake_provider_manifest.json")
    scenarios = load(REPO / "tests/e2e/fake-provider/provider_scenarios.json")
    if manifest:
        declared = {contract["behavior_id"] for contract in manifest.get("behavior_contracts", [])}
        supported = {b for provider in manifest.get("providers", []) for b in provider.get("supported_behaviors", [])}
        if not behaviors <= declared:
            fail(f"fake provider manifest 缺少行为: {behaviors - declared}")
        if not behaviors <= supported:
            fail(f"fake provider providers 未覆盖行为: {behaviors - supported}")
    if scenarios:
        covered = {scenario["behavior_id"] for scenario in scenarios.get("scenarios", [])}
        if not behaviors <= covered:
            fail(f"fake provider scenarios 未覆盖行为: {behaviors - covered}")


def check_migrations() -> None:
    root = REPO / "db/migrations/iterations"
    ups = {p.name[: -len(".up.sql")]: p for p in root.glob("**/*.up.sql")}
    downs = {p.name[: -len(".down.sql")] for p in root.glob("**/*.down.sql")}
    for name, path in ups.items():
        if name not in downs:
            fail(f"{path}: 缺少配对的 down migration")
    pattern = re.compile(r"\b(FOREIGN\s+KEY|REFERENCES\s+\w+\s*\()", re.IGNORECASE)
    for path in root.glob("**/*.sql"):
        for line_no, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            code = line.split("--", 1)[0]
            if pattern.search(code):
                fail(f"{path}:{line_no}: 迁移禁止数据库级外键")


def check_governance_doc() -> None:
    path = REPO / "docs/11-M7-开发计划与发布治理.md"
    if not path.exists():
        fail(f"{path}: 发布治理文档缺失（releasegate Go 测试依赖）")
        return
    text = path.read_text(encoding="utf-8")
    tokens = [
        "Contract Gate", "Migration Gate", "Fixture Gate", "Fake Provider Gate",
        "Feature Flag Gate", "Observability Gate", "Rollback Gate",
        "agent_runtime_v2", "tool_generation_v2", "marketplace_v2",
        "agent_run_success_rate", "router_decision_latency_ms", "board_patch_replay_error_count",
        "graph_resume_failure_count", "tool_task_success_rate", "credit_freeze_leak_count",
        "skill_usage_charge_error_count", "marketplace_install_failure_count", "settlement_reverse_count",
        "关闭", "停止消费", "释放所有未进入", "AG-UI replay", "dedupe_key",
    ]
    for token in tokens:
        if token not in text:
            fail(f"{path}: 发布治理文档缺少必需 token: {token}")


def main() -> int:
    check_all_json()
    check_agui_sequences()
    check_tool_plan()
    check_settlements()
    check_e2e_indexes()
    check_fake_provider()
    check_migrations()
    check_governance_doc()
    if ERRORS:
        print("fixture gate FAILED:", file=sys.stderr)
        for error in ERRORS:
            print(f"  - {error}", file=sys.stderr)
        return 1
    print("fixture gate ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
