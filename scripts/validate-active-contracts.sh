#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "== foundation ~ release active contract validators =="
python3 tests/contract/validate_foundation_contracts.py
python3 tests/contract/validate_board_graph_contracts.py
python3 tests/contract/validate_tool_asset_contracts.py
python3 tests/contract/validate_skill_market_contracts.py
python3 tests/contract/validate_release_e2e_gates.py
python3 tests/contract/validate_json_schema_contracts.py

echo "== OpenAPI YAML parse =="
python3 - <<'PY'
from __future__ import annotations

from pathlib import Path

try:
    import yaml
except ModuleNotFoundError as exc:
    raise SystemExit("missing PyYAML; install with `python3 -m pip install pyyaml`") from exc

paths = [
    Path("api/openapi/agent-workbench.yaml"),
    Path("api/openapi/business-api.yaml"),
    Path("api/openapi/creator-api.yaml"),
    Path("api/openapi/admin-api.yaml"),
]
for path in paths:
    with path.open("r", encoding="utf-8") as handle:
        yaml.safe_load(handle)
    print(f"{path} yaml parse ok")
PY

echo "== board graph ~ skill market migration static gate =="
python3 - <<'PY'
from __future__ import annotations

from pathlib import Path

roots = [
    Path("db/migrations/iterations/2026-07-01-agent-runtime-contracts"),
    Path("db/migrations/iterations/2026-07-01-tool-credit-asset-contracts"),
    Path("db/migrations/iterations/2026-07-01-marketplace-contracts"),
]

missing: list[str] = []
blocked_keywords: list[str] = []

for root in roots:
    if not root.exists():
        missing.append(f"missing migration root {root}")
        continue
    up_files = sorted(root.rglob("*.up.sql"))
    down_files = sorted(root.rglob("*.down.sql"))
    up_keys = {path.with_suffix("").with_suffix("").relative_to(root) for path in up_files}
    down_keys = {path.with_suffix("").with_suffix("").relative_to(root) for path in down_files}
    for key in sorted(up_keys - down_keys):
        missing.append(f"missing down migration for {root / (str(key) + '.up.sql')}")
    for key in sorted(down_keys - up_keys):
        missing.append(f"missing up migration for {root / (str(key) + '.down.sql')}")
    for path in up_files:
        text = path.read_text(encoding="utf-8").lower()
        if " references " in f" {text} " or "foreign key" in text:
            blocked_keywords.append(f"{path}: database-level foreign key/reference is blocked")

if missing or blocked_keywords:
    raise SystemExit("\n".join(missing + blocked_keywords))

print("migration pair/static guard ok")
PY

echo "== active documentation stale status scan =="
python3 - <<'PY'
from __future__ import annotations

from pathlib import Path

roots = [
    Path("docs/current"),
    Path("docs/active"),
    Path("docs/contracts"),
    Path("docs/technical"),
]
blocked_tokens = [
    "split / not frozen",
    "PR 状态：split / not frozen",
    "下一步按 release",
    "release 未完成前",
    "测试事实源待冻结",
    "继续拆分",
    "待冻结",
    "review 方向通过",
    "P0 文档补丁完成后",
]
violations: list[str] = []
for root in roots:
    if not root.exists():
        continue
    for path in root.rglob("*.md"):
        text = path.read_text(encoding="utf-8")
        for token in blocked_tokens:
            if token in text:
                violations.append(f"{path}: stale token {token}")
if violations:
    raise SystemExit("\n".join(violations))
print("active documentation status scan ok")
PY

echo "active contract gate passed"
