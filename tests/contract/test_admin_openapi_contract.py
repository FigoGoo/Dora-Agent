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


if __name__ == "__main__":
    test_admin_session_exposes_access_token()
    test_admin_list_routes_accept_page_size()
    print("admin openapi contract ok")
