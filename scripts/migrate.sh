#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "用法: $0 <business|agent|worker> <up|down>" >&2
  exit 2
fi

module="$1"
direction="$2"
case "$module" in
  business) database_url="${BUSINESS_DATABASE_URL:-}" ;;
  agent) database_url="${AGENT_DATABASE_URL:-}" ;;
  worker) database_url="${WORKER_DATABASE_URL:-}" ;;
  *) echo "未知 Module: $module" >&2; exit 2 ;;
esac

case "$direction" in
  up) migrate_args=(up) ;;
  down) migrate_args=(down 1) ;;
  *) echo "未知迁移方向: $direction" >&2; exit 2 ;;
esac

if [[ -z "$database_url" ]]; then
  echo "$module 数据库连接环境变量未设置" >&2
  exit 2
fi

migrate_bin="${MIGRATE_BIN:-migrate}"
if ! command -v "$migrate_bin" >/dev/null 2>&1; then
  echo "未找到 golang-migrate CLI；先安装 github.com/golang-migrate/migrate/v4/cmd/migrate" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"$migrate_bin" -path "$repo_root/$module/migrations" -database "$database_url" "${migrate_args[@]}"
