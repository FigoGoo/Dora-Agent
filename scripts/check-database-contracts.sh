#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go_bin="${GO_BIN:-go}"
migrate_bin="${MIGRATE_BIN:-migrate}"
target="${1:-all}"

case "$target" in
  all) modules=(business agent worker) ;;
  business|agent|worker) modules=("$target") ;;
  *) echo "用法: $0 [all|business|agent|worker]" >&2; exit 2 ;;
esac

for module in "${modules[@]}"; do
  case "$module" in
    business) database_url="${BUSINESS_CONTRACT_DATABASE_URL:-${DATABASE_URL:-}}" ;;
    agent) database_url="${AGENT_CONTRACT_DATABASE_URL:-${DATABASE_URL:-}}" ;;
    worker) database_url="${WORKER_CONTRACT_DATABASE_URL:-${DATABASE_URL:-}}" ;;
  esac
  if [[ -z "$database_url" ]]; then
	 echo "$module 数据库连接环境变量未设置" >&2
	 exit 2
  fi

  database_without_query="${database_url%%\?*}"
  database_name="${database_without_query##*/}"
  if [[ "$database_name" != *_test ]]; then
    echo "$module 契约数据库名必须以 _test 结尾" >&2
    exit 2
  fi
  if ! command -v "$migrate_bin" >/dev/null 2>&1; then
    echo "未找到 golang-migrate CLI" >&2
    exit 2
  fi

  # golang-migrate 的 PostgreSQL drop 只清理 current_schema；先清 public 迁移状态，再显式清理
  # Module Schema 中的残留表，最后借首个幂等 down 删除空 Schema，保证重复运行仍从零开始。
  if [[ "$database_url" == *\?* ]]; then
    module_schema_url="${database_url}&search_path=${module}"
  else
    module_schema_url="${database_url}?search_path=${module}"
  fi
  first_version="$(find "$repo_root/$module/migrations" -maxdepth 1 -name '*.up.sql' -exec basename {} \; | sort | head -n 1 | cut -d_ -f1)"
  if [[ -z "$first_version" ]]; then
    echo "$module 没有可执行的 up Migration" >&2
    exit 2
  fi
  "$migrate_bin" -path "$repo_root/$module/migrations" -database "$database_url" drop -f
  module_drop_output=""
  if ! module_drop_output="$("$migrate_bin" -path "$repo_root/$module/migrations" -database "$module_schema_url" drop -f 2>&1)"; then
    # search_path 指向尚不存在的 Schema 时 PostgreSQL CURRENT_SCHEMA() 为空，已经等价于无残留。
    if [[ "$module_drop_output" != *"no schema"* ]]; then
      echo "$module Module Schema 清理失败" >&2
      exit 1
    fi
  fi
  "$migrate_bin" -path "$repo_root/$module/migrations" -database "$database_url" force "$first_version"
  "$migrate_bin" -path "$repo_root/$module/migrations" -database "$database_url" down 1
  "$migrate_bin" -path "$repo_root/$module/migrations" -database "$database_url" up

  case "$module" in
    business)
      DORA_POSTGRES_CONTRACT_DSN="$database_url" \
      GOWORK=off "$go_bin" -C "$repo_root/business" test \
        -run '^TestMigratedSchemaContract$' -count=1 ./internal/postgres
      DORA_POSTGRES_CONTRACT_DSN="$database_url" \
      DORA_BUSINESS_TEST_POSTGRES_DSN="$database_url" \
      DORA_BUSINESS_TEST_ALLOW_DESTRUCTIVE=1 \
      GOWORK=off "$go_bin" -C "$repo_root/business" test \
        -run '^(TestProjectRepositoryPostgreSQLW0Semantics|TestRepositoryAuthPostgreSQLW0Semantics)$' \
        -count=1 ./internal/postgres
      ;;
    agent)
      DORA_POSTGRES_CONTRACT_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/agent" test \
          -run '^TestMigratedSchemaContract$' -count=1 ./internal/postgres
      DORA_POSTGRES_CONTRACT_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/agent" test \
          -run '^TestSessionRepositoryConcurrentEnsureContract$' \
          -count=1 ./internal/postgres
      ;;
    worker)
      DORA_POSTGRES_CONTRACT_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/worker" test \
          -run '^TestMigratedSchemaContract$' -count=1 ./internal/postgres
      ;;
  esac
done
