#!/usr/bin/env python3
"""Admin HTTP API contract checks used by the admin frontend."""

from __future__ import annotations

from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[2]
OPENAPI = ROOT / "api" / "openapi" / "business-api.yaml"


def test_admin_session_exposes_access_token() -> None:
    doc = yaml.safe_load(OPENAPI.read_text())
    schema = doc["components"]["schemas"]["AdminSessionDTO"]

    assert "access_token" in schema["required"]
    assert schema["properties"]["access_token"]["type"] == "string"


def test_admin_list_routes_accept_page_size() -> None:
    doc = yaml.safe_load(OPENAPI.read_text())
    paths = doc["paths"]
    admin_list_paths = [
        "/api/admin/admins",
        "/api/admin/users",
        "/api/admin/audit-logs",
        "/api/admin/models/providers",
        "/api/admin/models",
        "/api/admin/tools",
        "/api/admin/skills/system",
        "/api/admin/skills/reviews",
        "/api/admin/credits/grants/targets",
        "/api/admin/credits/codes",
        "/api/admin/works/public",
    ]

    for path in admin_list_paths:
        params = paths[path]["get"].get("parameters", [])
        names = {
            param.get("name") or param.get("$ref", "").rsplit("/", 1)[-1]
            for param in params
        }
        assert "PageSize" in names or "page_size" in names, path


def test_admin_skill_create_uses_markdown_contract() -> None:
    doc = yaml.safe_load(OPENAPI.read_text())
    schema = doc["components"]["schemas"]["SkillDraftMutationRequest"]

    assert "skill_markdown" in schema["required"]
    assert "skill_spec_json" not in schema["required"]
    assert schema["properties"]["skill_markdown"]["type"] == "string"
    assert schema["properties"]["skill_tags"]["type"] == "array"


def test_admin_tool_dto_exposes_policy_and_pricing_fields() -> None:
    doc = yaml.safe_load(OPENAPI.read_text())
    paths = doc["paths"]
    schema = doc["components"]["schemas"]["AdminToolDTO"]
    properties = schema["properties"]

    assert paths["/api/admin/tools"]["post"]["operationId"] == "registerAdminTool"
    register_schema = doc["components"]["schemas"]["RegisterAdminToolRequest"]
    for required_field in [
        "tool_name",
        "tool_type",
        "display_name",
        "description",
        "risk_level",
        "charge_mode",
        "billing_unit",
        "reason",
    ]:
        assert required_field in register_schema["required"]

    for field in [
        "display_name",
        "description",
        "allowed",
        "requires_confirmation",
        "timeout_ms",
        "billing_unit",
        "unit_points",
        "free_quota",
        "min_charge_points",
        "pricing_policy_id",
    ]:
        assert field in properties


def test_admin_models_can_filter_by_provider() -> None:
    doc = yaml.safe_load(OPENAPI.read_text())
    paths = doc["paths"]
    model_schema = doc["components"]["schemas"]["AdminModelDTO"]
    params = paths["/api/admin/models"]["get"].get("parameters", [])
    param_names = {
        param.get("name") or param.get("$ref", "").rsplit("/", 1)[-1]
        for param in params
    }

    assert "provider_id" in param_names
    assert "resource_type" in param_names
    assert "status" in param_names
    assert "provider_name" in model_schema["properties"]


if __name__ == "__main__":
    test_admin_session_exposes_access_token()
    test_admin_list_routes_accept_page_size()
    test_admin_skill_create_uses_markdown_contract()
    test_admin_tool_dto_exposes_policy_and_pricing_fields()
    test_admin_models_can_filter_by_provider()
    print("admin openapi contract ok")
