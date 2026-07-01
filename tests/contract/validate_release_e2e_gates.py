#!/usr/bin/env python3
"""Validate release E2E fixture and release gate artifacts without external packages."""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
FAKE_PROVIDER_DIR = REPO_ROOT / "tests" / "e2e" / "fake-provider"
E2E_ROOT = REPO_ROOT / "tests" / "e2e"
BROWSER_SMOKE_DIR = E2E_ROOT / "browser"
FIXTURE_ROOT = REPO_ROOT / "tests" / "fixtures" / "e2e"
CONTRACT_FIXTURE_ROOT = REPO_ROOT / "tests" / "fixtures" / "contracts"
RELEASE_GOVERNANCE_DOC = REPO_ROOT / "docs" / "active" / "technical" / "release-governance.md"
BROWSER_SMOKE_SCRIPT = REPO_ROOT / "scripts" / "validate-release-browser-smoke.sh"
FULL_HTTP_SMOKE_SCRIPT = REPO_ROOT / "scripts" / "validate-release-full-http-smoke.sh"
FULL_HTTP_SMOKE_TEST = REPO_ROOT / "services" / "agent" / "internal" / "e2e" / "release" / "full_http_service_smoke_test.go"
HTTP_SERVICE_E2E_SCRIPT = REPO_ROOT / "scripts" / "validate-release-http-service-e2e.sh"
HTTP_SERVICE_E2E_TEST = E2E_ROOT / "http" / "validate_release_http_service_e2e.py"
HTTP_SERVICE_E2E_SCRIPT_TEST = REPO_ROOT / "services" / "agent" / "internal" / "e2e" / "release" / "http_service_e2e_script_test.go"
HTTP_SERVICE_E2E_REPORT = REPO_ROOT / "tests" / "reports" / "release-http-service-e2e-report.md"
MAKEFILE = REPO_ROOT / "Makefile"
ACTIVE_CONTRACT_WORKFLOW = REPO_ROOT / ".github" / "workflows" / "active-contract-gates.yml"

REQUIRED_BEHAVIORS = {
    "deterministic_success",
    "async_pending",
    "partial_success",
    "transient_failure",
    "terminal_failure",
    "slow_callback",
}

REQUIRED_E2E_FIXTURES = {
    "agent-workspace/city_tourism_default_skill.json": {
        "case_id": "city_tourism_default_skill_e2e_v1",
        "depends_on_contracts": {"foundation", "board graph", "tool asset"},
    },
    "agent-workspace/generic_creation_graph_fallback.json": {
        "case_id": "generic_creation_graph_fallback_e2e_v1",
        "depends_on_contracts": {"foundation", "board graph"},
    },
    "skill-marketplace/paid_marketplace_skill_usage.json": {
        "case_id": "paid_marketplace_skill_usage_e2e_v1",
        "depends_on_contracts": {"foundation", "board graph", "tool asset", "skill market"},
    },
    "skill-marketplace/enterprise_pinned_install_upgrade.json": {
        "case_id": "enterprise_pinned_install_upgrade_e2e_v1",
        "depends_on_contracts": {"skill market"},
    },
    "agent-workspace/tool_partial_failure_release.json": {
        "case_id": "tool_partial_failure_release_e2e_v1",
        "depends_on_contracts": {"board graph", "tool asset"},
    },
    "skill-marketplace/listing_suspended_guard.json": {
        "case_id": "listing_suspended_guard_e2e_v1",
        "depends_on_contracts": {"skill market"},
    },
    "admin-governance/refund_settlement_reverse.json": {
        "case_id": "refund_settlement_reverse_e2e_v1",
        "depends_on_contracts": {"skill market"},
    },
    "agent-workspace/replay_after_restart.json": {
        "case_id": "replay_after_restart_e2e_v1",
        "depends_on_contracts": {"foundation", "board graph", "tool asset"},
    },
}

REQUIRED_RELEASE_GATES = {
    "Contract Gate",
    "Migration Gate",
    "Fixture Gate",
    "Fake Provider Gate",
    "Local Full HTTP Service Gate",
    "Browser Smoke Gate",
    "Test Environment HTTP Service Gate",
    "Feature Flag Gate",
    "Observability Gate",
    "Rollback Gate",
}

REQUIRED_FLAGS = {
    "agent_runtime_v2",
    "tool_generation_v2",
    "marketplace_v2",
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

REQUIRED_BROWSER_SMOKE_TOKENS = {
    "chromium.launch",
    "frontend",
    "admin_frontend",
    "/api/marketplace/installations",
    "/api/creator/skills",
    "/api/admin/settlements/",
    "settlement_release",
    "settlement_payout",
}

REQUIRED_FULL_HTTP_SMOKE_TOKENS = {
    "TestReleaseFullHTTPServiceE2ESmoke",
    "./services/business/cmd/business",
    "./services/agent/cmd/agent",
    "/api/auth/login",
    "/api/agent/sessions",
    "/api/agent/runs",
    "BUSINESS_HOSTPORTS",
    "AGENT_RUNTIME_REDIS_MODE",
    "testdb.StartPostgres",
    "testredis.Start",
}

REQUIRED_HTTP_SERVICE_E2E_TOKENS = {
    "RELEASE_BUSINESS_BASE_URL",
    "RELEASE_AGENT_BASE_URL",
    "RELEASE_TEST_PROJECT_ID",
    "RELEASE_TEST_SPACE_ID",
    "RELEASE_ACCESS_TOKEN",
    "/healthz",
    "/readyz",
    "/api/auth/login",
    "/api/agent/sessions",
    "/api/agent/runs",
    "/api/agent/runs/{encoded_run_id}/events",
    "creative.guide.presented",
    "agent.run.completed",
    "creative.router.decided",
    "agent.message.completed",
    "waiting_input",
    "RELEASE_HTTP_E2E_REPORT_PATH",
    "write_markdown_report",
}

REQUIRED_HTTP_SERVICE_E2E_REPORT_TOKENS = {
    "Release HTTP Service E2E Report",
    "make release-http-service-e2e",
    "RELEASE_BUSINESS_BASE_URL",
    "RELEASE_AGENT_BASE_URL",
    "business /healthz",
    "business /readyz",
    "agent /healthz",
    "agent /readyz",
    "/api/auth/login",
    "/api/agent/sessions",
    "/api/agent/runs",
    "creative.guide.presented",
    "agent.run.completed",
    "creative.router.decided",
    "agent.message.completed",
}

REQUIRED_HTTP_SERVICE_E2E_SCRIPT_TEST_TOKENS = {
    "TestReleaseHTTPServiceE2EScript",
    "scripts/validate-release-http-service-e2e.sh",
    "RELEASE_BUSINESS_BASE_URL",
    "RELEASE_AGENT_BASE_URL",
    "RELEASE_HTTP_E2E_REPORT_PATH",
    "testdb.StartPostgres",
    "testredis.Start",
    "status: passed",
}


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


def validate_fake_provider() -> None:
    manifest = load_json(FAKE_PROVIDER_DIR / "fake_provider_manifest.json")
    scenarios = load_json(FAKE_PROVIDER_DIR / "provider_scenarios.json")

    manifest_behaviors = {item["behavior_id"] for item in manifest.get("behavior_contracts", [])}
    missing_manifest = REQUIRED_BEHAVIORS - manifest_behaviors
    if missing_manifest:
        fail(f"fake provider manifest missing behaviors {sorted(missing_manifest)}")

    provider_supported = set()
    for provider in manifest.get("providers", []):
        provider_supported.update(provider.get("supported_behaviors", []))
    missing_supported = REQUIRED_BEHAVIORS - provider_supported
    if missing_supported:
        fail(f"fake provider providers missing supported behaviors {sorted(missing_supported)}")

    scenario_behaviors = {item["behavior_id"] for item in scenarios.get("scenarios", [])}
    missing_scenarios = REQUIRED_BEHAVIORS - scenario_behaviors
    if missing_scenarios:
        fail(f"fake provider scenarios missing behaviors {sorted(missing_scenarios)}")

    for scenario in scenarios.get("scenarios", []):
        if "case_id" not in scenario or "provider_id" not in scenario or "expected" not in scenario:
            fail(f"fake provider scenario missing required keys: {scenario}")

    print("release fake provider artifacts ok")


def validate_suite_indexes() -> None:
    required_suites = [
        E2E_ROOT / "agent-workspace" / "scenarios.json",
        E2E_ROOT / "skill-marketplace" / "scenarios.json",
        E2E_ROOT / "admin-governance" / "scenarios.json",
    ]
    indexed_paths: set[str] = set()
    for path in required_suites:
        suite = load_json(path)
        if suite.get("status") != "active":
            fail(f"{path}: suite must be active")
        gates = set(suite.get("required_gates", []))
        if "Fixture Gate" not in gates:
            fail(f"{path}: suite must include Fixture Gate")
        for item in suite.get("fixtures", []):
            fixture_path = item.get("fixture_path")
            if not fixture_path:
                fail(f"{path}: fixture item missing fixture_path")
            indexed_paths.add(fixture_path.removeprefix("tests/fixtures/e2e/"))
            if item.get("required") is not True:
                fail(f"{path}: all release fixtures must be required")
            if item.get("fake_provider_behavior") not in REQUIRED_BEHAVIORS:
                fail(f"{path}: unknown fake provider behavior {item.get('fake_provider_behavior')}")

    missing_index = set(REQUIRED_E2E_FIXTURES) - indexed_paths
    if missing_index:
        fail(f"e2e suite indexes missing fixtures {sorted(missing_index)}")
    print("release e2e suite indexes ok")


def validate_e2e_fixtures() -> None:
    for relative_path, expected in REQUIRED_E2E_FIXTURES.items():
        path = FIXTURE_ROOT / relative_path
        data = load_json(path)
        if data.get("status") != "active":
            fail(f"{path}: fixture must be active")
        if data.get("case_id") != expected["case_id"]:
            fail(f"{path}: bad case_id {data.get('case_id')}")
        depends_on = set(data.get("depends_on_contracts", []))
        missing_contracts = expected["depends_on_contracts"] - depends_on
        if missing_contracts:
            fail(f"{path}: missing dependencies {sorted(missing_contracts)}")
        if not data.get("user_journey"):
            fail(f"{path}: missing user_journey")
        if not data.get("expected_business_state"):
            fail(f"{path}: missing expected_business_state")
        fake_provider = data.get("fake_provider", {})
        if fake_provider.get("behavior_id") not in REQUIRED_BEHAVIORS:
            fail(f"{path}: missing or invalid fake_provider.behavior_id")
        if "Fixture Gate" not in data.get("release_gates", []):
            fail(f"{path}: missing Fixture Gate")

        references = data.get("contract_references", [])
        if not references:
            fail(f"{path}: missing contract_references")
        for reference in references:
            reference_path = REPO_ROOT / reference["path"]
            if not reference_path.exists():
                fail(f"{path}: missing referenced contract fixture {reference_path}")
            if CONTRACT_FIXTURE_ROOT not in reference_path.parents:
                fail(f"{path}: contract reference must point to tests/fixtures/contracts")

    print("release e2e fixtures ok")


def validate_release_governance() -> None:
    if not RELEASE_GOVERNANCE_DOC.exists():
        fail(f"missing {RELEASE_GOVERNANCE_DOC}")
    text = RELEASE_GOVERNANCE_DOC.read_text(encoding="utf-8")
    for token in REQUIRED_RELEASE_GATES | REQUIRED_FLAGS | REQUIRED_METRICS:
        if token not in text:
            fail(f"{RELEASE_GOVERNANCE_DOC}: missing {token}")
    required_rollback_tokens = [
        "关闭",
        "停止消费",
        "释放所有未进入",
        "AG-UI replay",
        "dedupe_key",
    ]
    for token in required_rollback_tokens:
        if token not in text:
            fail(f"{RELEASE_GOVERNANCE_DOC}: missing rollback token {token}")
    print("release governance ok")


def validate_browser_smoke() -> None:
    package_json = BROWSER_SMOKE_DIR / "package.json"
    package_lock = BROWSER_SMOKE_DIR / "package-lock.json"
    smoke_script = BROWSER_SMOKE_DIR / "release-frontend-browser-smoke.mjs"
    for path in (package_json, package_lock, smoke_script, BROWSER_SMOKE_SCRIPT, MAKEFILE, ACTIVE_CONTRACT_WORKFLOW):
        if not path.exists():
            fail(f"missing {path}")

    package = load_json(package_json)
    if package.get("scripts", {}).get("smoke") != "node release-frontend-browser-smoke.mjs":
        fail(f"{package_json}: missing smoke script")
    if "playwright-core" not in package.get("devDependencies", {}):
        fail(f"{package_json}: missing playwright-core dependency")

    script_text = smoke_script.read_text(encoding="utf-8")
    for token in REQUIRED_BROWSER_SMOKE_TOKENS:
        if token not in script_text:
            fail(f"{smoke_script}: missing browser smoke token {token}")

    shell_text = BROWSER_SMOKE_SCRIPT.read_text(encoding="utf-8")
    for token in ("CHROME_EXECUTABLE", "npm --prefix tests/e2e/browser run smoke"):
        if token not in shell_text:
            fail(f"{BROWSER_SMOKE_SCRIPT}: missing browser smoke token {token}")

    makefile_text = MAKEFILE.read_text(encoding="utf-8")
    if "release-browser-smoke" not in makefile_text:
        fail(f"{MAKEFILE}: missing release-browser-smoke target")

    workflow_text = ACTIVE_CONTRACT_WORKFLOW.read_text(encoding="utf-8")
    for token in ("release-browser-smoke", "tests/e2e/browser/package-lock.json", "scripts/validate-release-browser-smoke.sh"):
        if token not in workflow_text:
            fail(f"{ACTIVE_CONTRACT_WORKFLOW}: missing browser smoke token {token}")
    print("release browser smoke artifacts ok")


def validate_full_http_smoke() -> None:
    for path in (FULL_HTTP_SMOKE_SCRIPT, FULL_HTTP_SMOKE_TEST, MAKEFILE, ACTIVE_CONTRACT_WORKFLOW):
        if not path.exists():
            fail(f"missing {path}")

    test_text = FULL_HTTP_SMOKE_TEST.read_text(encoding="utf-8")
    for token in REQUIRED_FULL_HTTP_SMOKE_TOKENS:
        if token not in test_text:
            fail(f"{FULL_HTTP_SMOKE_TEST}: missing full HTTP smoke token {token}")

    shell_text = FULL_HTTP_SMOKE_SCRIPT.read_text(encoding="utf-8")
    for token in ("TestReleaseFullHTTPServiceE2ESmoke", "go test ./services/agent/internal/e2e/release"):
        if token not in shell_text:
            fail(f"{FULL_HTTP_SMOKE_SCRIPT}: missing full HTTP smoke token {token}")

    makefile_text = MAKEFILE.read_text(encoding="utf-8")
    if "release-full-http-smoke" not in makefile_text:
        fail(f"{MAKEFILE}: missing release-full-http-smoke target")

    workflow_text = ACTIVE_CONTRACT_WORKFLOW.read_text(encoding="utf-8")
    for token in ("release-full-http-smoke", "scripts/validate-release-full-http-smoke.sh", "release-browser-smoke"):
        if token not in workflow_text:
            fail(f"{ACTIVE_CONTRACT_WORKFLOW}: missing full HTTP smoke token {token}")
    print("release full HTTP service smoke artifacts ok")


def validate_http_service_e2e() -> None:
    for path in (HTTP_SERVICE_E2E_SCRIPT, HTTP_SERVICE_E2E_TEST, HTTP_SERVICE_E2E_SCRIPT_TEST, HTTP_SERVICE_E2E_REPORT, MAKEFILE):
        if not path.exists():
            fail(f"missing {path}")

    test_text = HTTP_SERVICE_E2E_TEST.read_text(encoding="utf-8")
    for token in REQUIRED_HTTP_SERVICE_E2E_TOKENS:
        if token not in test_text:
            fail(f"{HTTP_SERVICE_E2E_TEST}: missing release HTTP service E2E token {token}")

    shell_text = HTTP_SERVICE_E2E_SCRIPT.read_text(encoding="utf-8")
    for token in (
        "RELEASE_BUSINESS_BASE_URL",
        "RELEASE_AGENT_BASE_URL",
        "RELEASE_HTTP_E2E_REPORT_PATH",
        "tests/reports/release-http-service-e2e-report.md",
        "python3 tests/e2e/http/validate_release_http_service_e2e.py",
    ):
        if token not in shell_text:
            fail(f"{HTTP_SERVICE_E2E_SCRIPT}: missing release HTTP service E2E token {token}")

    report_text = HTTP_SERVICE_E2E_REPORT.read_text(encoding="utf-8")
    for token in REQUIRED_HTTP_SERVICE_E2E_REPORT_TOKENS:
        if token not in report_text:
            fail(f"{HTTP_SERVICE_E2E_REPORT}: missing release HTTP service E2E report token {token}")
    if "未执行但通过" in report_text or "未执行项通过" in report_text or "status: failed" in report_text:
        fail(f"{HTTP_SERVICE_E2E_REPORT}: release HTTP service E2E report is not a valid releasable artifact")
    if "status: pending_environment" not in report_text and "status: passed" not in report_text:
        fail(f"{HTTP_SERVICE_E2E_REPORT}: report status must be pending_environment or passed")
    if "status: passed" in report_text and "未执行项：无（release HTTP service E2E 范围内）" not in report_text:
        fail(f"{HTTP_SERVICE_E2E_REPORT}: passed report must declare no unexecuted release HTTP service E2E items")

    script_test_text = HTTP_SERVICE_E2E_SCRIPT_TEST.read_text(encoding="utf-8")
    for token in REQUIRED_HTTP_SERVICE_E2E_SCRIPT_TEST_TOKENS:
        if token not in script_test_text:
            fail(f"{HTTP_SERVICE_E2E_SCRIPT_TEST}: missing release HTTP service E2E local harness token {token}")

    makefile_text = MAKEFILE.read_text(encoding="utf-8")
    if "release-http-service-e2e" not in makefile_text:
        fail(f"{MAKEFILE}: missing release-http-service-e2e target")

    print("release HTTP service E2E artifacts ok")


def main() -> None:
    validate_fake_provider()
    validate_suite_indexes()
    validate_e2e_fixtures()
    validate_release_governance()
    validate_browser_smoke()
    validate_full_http_smoke()
    validate_http_service_e2e()
    print("release gate validation ok")


if __name__ == "__main__":
    main()
