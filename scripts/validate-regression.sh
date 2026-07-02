#!/usr/bin/env bash
# 全量回归门禁：静态夹具门禁 + gofmt + go vet + go test + 前端测试（可选跳过）。
# 用法：
#   scripts/validate-regression.sh              # Go + 夹具
#   RUN_FRONTEND=1 scripts/validate-regression.sh  # 追加前端与管理端
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "== fixture gate (python) =="
python3 scripts/validate-fixtures.py

echo "== gofmt dry check =="
unformatted="$(find services internal -name '*.go' -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  echo "gofmt required" >&2
  exit 1
fi

echo "== go vet =="
go vet ./services/... ./internal/...

echo "== go test =="
go test -count=1 ./services/... ./internal/...

if [[ "${RUN_FRONTEND:-0}" == "1" ]]; then
  echo "== frontend test =="
  npm --prefix frontend test
  npm --prefix frontend run build
  echo "== admin frontend test =="
  pnpm --dir admin_frontend test
  pnpm --dir admin_frontend build
fi

echo "regression ok"
