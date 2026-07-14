#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go_bin="${GO_BIN:-go}"
migrate_bin="${MIGRATE_BIN:-migrate}"
target="${1:-all}"
reset_migration_dir=""

cleanup() {
  if [[ -n "$reset_migration_dir" && -d "$reset_migration_dir" ]]; then
    rm -rf -- "$reset_migration_dir"
  fi
}

trap cleanup EXIT

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

  # golang-migrate 的 PostgreSQL drop 只清 relation，不清触发器函数等 routine。契约库可能因上次真实
  # Migration 测试保留 routine，不能通过修改已发布首版 Migration 来适配测试工具的清理缺口。
  # 数据库名已经强制为 *_test；这里用独立临时 reset migration 删除整个 Module Schema，再删除临时
  # migration state，随后仍执行“force 首版 + down 1 + fresh up”验证正式 Migration 生命周期。
  first_version="$(find "$repo_root/$module/migrations" -maxdepth 1 -name '*.up.sql' -exec basename {} \; | sort | head -n 1 | cut -d_ -f1)"
  if [[ -z "$first_version" ]]; then
    echo "$module 没有可执行的 up Migration" >&2
    exit 2
  fi
  "$migrate_bin" -path "$repo_root/$module/migrations" -database "$database_url" drop -f
  reset_migration_dir="$(mktemp -d "${TMPDIR:-/tmp}/dora-${module}-contract-reset.XXXXXX")"
  chmod 700 "$reset_migration_dir"
  printf 'DROP SCHEMA IF EXISTS %s CASCADE;\n' "$module" \
    >"$reset_migration_dir/000001_reset_contract_schema.up.sql"
  chmod 600 "$reset_migration_dir/000001_reset_contract_schema.up.sql"
  "$migrate_bin" -path "$reset_migration_dir" -database "$database_url" up
  "$migrate_bin" -path "$reset_migration_dir" -database "$database_url" drop -f
  rm -rf -- "$reset_migration_dir"
  reset_migration_dir=""
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
        -run '^(TestProjectRepositoryPostgreSQLW0Semantics|TestRepositoryAuthPostgreSQLW0Semantics|TestAuthorizationRepositoryPostgreSQLLifecycle|TestSkillRepositoryPostgreSQLW1Semantics|TestProjectRepositoryPostgreSQLQuickCreateV2BatchSQL|TestProjectRepositoryPostgreSQLQuickCreateV2|TestSkillApprovalCannotCommitAfterConcurrentReviewerRevocation)$' \
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
