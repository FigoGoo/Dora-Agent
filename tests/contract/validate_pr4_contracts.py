#!/usr/bin/env python3
"""Validate PR-4 Marketplace contract artifacts without external packages."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
SKILL_SCHEMA_DIR = REPO_ROOT / "api" / "schemas" / "skill"
SETTLEMENT_SCHEMA_DIR = REPO_ROOT / "api" / "schemas" / "settlement"
FIXTURE_ROOT = REPO_ROOT / "tests" / "fixtures" / "contracts"
OPENAPI_DIR = REPO_ROOT / "api" / "openapi"
THRIFT_DIR = REPO_ROOT / "api" / "thrift"
MIGRATION_DIR = REPO_ROOT / "db" / "migrations" / "iterations" / "2026-07-01-marketplace-contracts" / "business"


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(1)


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        fail(f"{path}: invalid JSON: {exc}")


def required_fields(schema_name: str) -> set[str]:
    for directory in (SKILL_SCHEMA_DIR, SETTLEMENT_SCHEMA_DIR):
        path = directory / schema_name
        if path.exists():
            return set(load_json(path).get("required", []))
    fail(f"missing schema {schema_name}")


def require_keys(path: Path, data: dict[str, Any], required: set[str], label: str) -> None:
    missing = required - set(data)
    if missing:
        fail(f"{path}: {label} missing {sorted(missing)}")


def validate_json_artifacts() -> None:
    paths = (
        list(SKILL_SCHEMA_DIR.glob("*.json"))
        + list(SETTLEMENT_SCHEMA_DIR.glob("*.json"))
        + list((FIXTURE_ROOT / "marketplace").glob("*.json"))
        + list((FIXTURE_ROOT / "billing").glob("*.json"))
    )
    if not paths:
        fail("no PR-4 JSON artifacts found")
    for path in paths:
        load_json(path)
    print(f"pr4 json ok {len(paths)} files")


def validate_openapi_surfaces() -> None:
    required_tokens = {
        "business-api.yaml": [
            "listMarketplaceSkills",
            "getMarketplaceSkill",
            "installMarketplaceSkill",
            "upgradeSkillInstallation",
            "listInstalledSkills",
        ],
        "creator-api.yaml": [
            "createCreatorSkillDraft",
            "submitCreatorSkillVersion",
            "listCreatorListings",
            "getCreatorSkillUsageAnalytics",
        ],
        "admin-api.yaml": [
            "approveSkillReview",
            "suspendMarketplaceListing",
            "approveSkillUsageRefund",
        ],
    }
    for filename, tokens in required_tokens.items():
        path = OPENAPI_DIR / filename
        if not path.exists():
            fail(f"missing OpenAPI {path}")
        text = path.read_text(encoding="utf-8")
        for token in tokens:
            if token not in text:
                fail(f"{filename}: missing {token}")
    print("pr4 openapi surface ok")


def validate_thrift_surfaces() -> None:
    required = {
        "business_skill_marketplace_service.thrift": [
            "ListMarketplaceSkills",
            "InstallSkill",
            "UpgradeSkillInstallation",
            "EstimateSkillUsageCredits",
            "CreateSkillUsageRecord",
            "FreezeSkillUsageCredits",
            "CommitSkillUsageAndSettle",
            "ReleaseSkillUsageFreeze",
            "RequestMeta",
        ],
        "business_settlement_service.thrift": [
            "CreateSkillSettlementHold",
            "ReverseSkillSettlement",
            "RequestMeta",
        ],
    }
    for filename, tokens in required.items():
        path = THRIFT_DIR / filename
        if not path.exists():
            fail(f"missing thrift {path}")
        text = path.read_text(encoding="utf-8")
        for token in tokens:
            if token not in text:
                fail(f"{filename}: missing {token}")
    print("pr4 thrift surface ok")


def validate_creator_publish_fixture() -> None:
    path = FIXTURE_ROOT / "marketplace" / "creator_submit_approve_publish.json"
    data = load_json(path)
    require_keys(path, data["skill_package"], required_fields("skill-package.v1.schema.json"), "skill_package")
    require_keys(path, data["skill_version"], required_fields("skill-version.v1.schema.json"), "skill_version")
    require_keys(path, data["pricing_policy"], required_fields("skill-pricing-policy.v1.schema.json"), "pricing_policy")
    require_keys(path, data["listing"], required_fields("marketplace-listing.v1.schema.json"), "listing")
    if data["skill_version"]["status"] != "published":
        fail(f"{path}: skill version must be published")
    if data["listing"]["status"] != "listed":
        fail(f"{path}: listing must be listed")
    if data["skill_version"]["pricing_policy_digest"] != data["listing"]["pricing_policy_digest"]:
        fail(f"{path}: pricing digest must match version and listing")
    print("pr4 creator publish fixture ok")


def validate_installation_fixtures() -> None:
    required = required_fields("skill-installation.v1.schema.json")
    personal_path = FIXTURE_ROOT / "marketplace" / "user_install_latest_personal.json"
    personal = load_json(personal_path)
    require_keys(personal_path, personal["installation"], required, "installation")
    if personal["installation"]["account_scope"] != "personal":
        fail(f"{personal_path}: personal install must use personal scope")
    if personal["installation"]["version_strategy"] != "latest_published":
        fail(f"{personal_path}: personal install must default latest_published")

    enterprise_path = FIXTURE_ROOT / "marketplace" / "enterprise_install_pinned_upgrade.json"
    enterprise = load_json(enterprise_path)
    require_keys(enterprise_path, enterprise["initial_installation"], required, "initial_installation")
    require_keys(enterprise_path, enterprise["installation_after_upgrade"], required, "installation_after_upgrade")
    if enterprise["initial_installation"]["version_strategy"] != "pinned":
        fail(f"{enterprise_path}: enterprise install must default pinned")
    if enterprise["upgrade_request"]["confirmed"] is not True:
        fail(f"{enterprise_path}: enterprise upgrade must require confirmation")
    if enterprise["historical_run_rule"]["must_resume_with_skill_version"] != "v1":
        fail(f"{enterprise_path}: historical run must resume with old snapshot version")
    print("pr4 installation fixtures ok")


def validate_billing_fixtures() -> None:
    usage_required = required_fields("skill-usage-record.v1.schema.json")
    settlement_required = required_fields("skill-settlement.v1.schema.json")

    charge_path = FIXTURE_ROOT / "billing" / "skill_usage_precreate_confirm_charge.json"
    charge = load_json(charge_path)
    expected_sequence = [
        "EstimateSkillUsageCredits",
        "CreateSkillUsageRecord",
        "cost_disclosure.skill_usage.presented",
        "FreezeSkillUsageCredits",
        "GraphValueDelivered",
        "CommitSkillUsageAndSettle",
    ]
    if charge["sequence"] != expected_sequence:
        fail(f"{charge_path}: bad skill usage sequence")
    require_keys(charge_path, charge["usage_after_create"], usage_required, "usage_after_create")
    require_keys(charge_path, charge["usage_after_charge"], usage_required, "usage_after_charge")
    require_keys(charge_path, charge["settlement"], settlement_required, "settlement")
    if charge["usage_after_create"]["usage_status"] != "confirmation_required":
        fail(f"{charge_path}: usage must be precreated as confirmation_required")
    if charge["usage_after_create"]["charge_status"] != "not_frozen":
        fail(f"{charge_path}: usage after create must not be frozen")
    if charge["usage_after_charge"]["usage_status"] != "value_delivered":
        fail(f"{charge_path}: usage after charge must be value_delivered")
    if charge["usage_after_charge"]["charge_status"] != "charged":
        fail(f"{charge_path}: usage after charge must be charged")

    refund_path = FIXTURE_ROOT / "billing" / "skill_usage_refund_reversal.json"
    refund = load_json(refund_path)
    require_keys(refund_path, refund["usage_before_refund"], usage_required, "usage_before_refund")
    require_keys(refund_path, refund["usage_after_refund"], usage_required, "usage_after_refund")
    require_keys(refund_path, refund["settlement_after_reverse"], settlement_required, "settlement_after_reverse")
    if refund["usage_after_refund"]["refund_status"] != "refund_reversed":
        fail(f"{refund_path}: refund status must be refund_reversed")
    if refund["settlement_after_reverse"]["status"] != "reversed":
        fail(f"{refund_path}: settlement must be reversed")
    print("pr4 billing fixtures ok")


def validate_data_visibility_fixture() -> None:
    path = FIXTURE_ROOT / "marketplace" / "data_visibility_creator_safe.json"
    data = load_json(path)
    response = data["creator_api_response"]
    forbidden = set(data["forbidden_fields"])
    leaked = forbidden & set(response)
    if leaked:
        fail(f"{path}: creator API leaked forbidden fields {sorted(leaked)}")
    required_safe = {"usage_count", "revenue_hold_amount", "refund_count", "failure_code_summary"}
    missing = required_safe - set(response)
    if missing:
        fail(f"{path}: creator API missing safe summary fields {sorted(missing)}")
    print("pr4 data visibility fixture ok")


def validate_migration() -> None:
    up_path = MIGRATION_DIR / "0001_skill_marketplace_settlement.up.sql"
    down_path = MIGRATION_DIR / "0001_skill_marketplace_settlement.down.sql"
    if not up_path.exists() or not down_path.exists():
        fail(f"{MIGRATION_DIR}: missing up/down migration")
    up_text = up_path.read_text(encoding="utf-8").lower()
    down_text = down_path.read_text(encoding="utf-8").lower()
    required_tables = [
        "skill_packages",
        "skill_versions",
        "skill_pricing_policies",
        "marketplace_listings",
        "skill_installations",
        "skill_usage_records",
        "skill_settlement_records",
        "skill_refund_cases",
        "skill_review_records",
    ]
    for table in required_tables:
        if f"create table if not exists {table}" not in up_text:
            fail(f"{up_path}: missing create table {table}")
        if f"drop table if exists {table}" not in down_text:
            fail(f"{down_path}: missing drop table {table}")
    if re.search(r"\breferences\b", up_text):
        fail(f"{up_path}: migration must not declare database-level foreign keys")
    print("pr4 migration static guard ok")


def main() -> None:
    validate_json_artifacts()
    validate_openapi_surfaces()
    validate_thrift_surfaces()
    validate_creator_publish_fixture()
    validate_installation_fixtures()
    validate_billing_fixtures()
    validate_data_visibility_fixture()
    validate_migration()
    print("pr4 contract validation ok")


if __name__ == "__main__":
    main()
