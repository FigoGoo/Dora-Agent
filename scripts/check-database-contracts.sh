#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go_bin="${GO_BIN:-go}"
migrate_bin="${MIGRATE_BIN:-migrate}"
target="${1:-all}"
reset_migration_dir=""
test_output_files=()
active_child_pid=""
active_child_pgid=""

cleanup() {
  if [[ -n "$reset_migration_dir" && -d "$reset_migration_dir" ]]; then
    rm -rf -- "$reset_migration_dir"
  fi
  # macOS 自带 Bash 3.2 在 nounset 下不能展开空数组；首次测试前失败时仍须保留原退出码。
  if (( ${#test_output_files[@]} > 0 )); then
    local output_file
    for output_file in "${test_output_files[@]}"; do
      rm -f -- "$output_file"
    done
  fi
}

stop_active_child() {
  local signal_name="$1"
  local child_pid="$active_child_pid"
  local child_pgid="$active_child_pgid"
  if [[ -z "$child_pid" || -z "$child_pgid" ]]; then
    active_child_pid=""
    active_child_pgid=""
    return
  fi

  if ! kill -0 -- "-$child_pgid" 2>/dev/null; then
    wait "$child_pid" 2>/dev/null || true
    active_child_pid=""
    active_child_pgid=""
    return
  fi

  kill -s "$signal_name" -- "-$child_pgid" 2>/dev/null || true
  local attempt
  for attempt in {1..20}; do
    if ! kill -0 -- "-$child_pgid" 2>/dev/null; then
      wait "$child_pid" 2>/dev/null || true
      active_child_pid=""
      active_child_pgid=""
      return
    fi
    sleep 0.1
  done
  kill -KILL -- "-$child_pgid" 2>/dev/null || true
  wait "$child_pid" 2>/dev/null || true
  active_child_pid=""
  active_child_pgid=""
}

handle_signal() {
  local signal_name="$1"
  local exit_code="$2"
  # 信号路径先停止当前 migrate/go 子进程，再自行清理并关闭全部 Trap，避免 EXIT 双清理。
  trap - EXIT HUP INT TERM
  stop_active_child "$signal_name"
  cleanup
  echo "数据库契约检查收到 ${signal_name}，已清理临时文件" >&2
  exit "$exit_code"
}

run_command() {
  local command_status=0
  # 非交互 Bash 开启 monitor 后，每个后台 Job 使用独立进程组；随后立即关闭 monitor，
  # 正常输出语义不变，取消时则能同时停止 go/migrate 及其测试二进制等全部后代。
  set -m
  "$@" &
  active_child_pid=$!
  active_child_pgid=$active_child_pid
  set +m
  wait "$active_child_pid" || command_status=$?
  active_child_pid=""
  active_child_pgid=""
  return "$command_status"
}

trap cleanup EXIT
trap 'handle_signal HUP 129' HUP
trap 'handle_signal INT 130' INT
trap 'handle_signal TERM 143' TERM

run_required_go_tests() {
  local module="$1"
  local expected_csv="$2"
  shift 2
  if [[ "${1:-}" != "--" ]]; then
    echo "run_required_go_tests 缺少命令分隔符" >&2
    return 2
  fi
  shift

  local output_file
  output_file="$(mktemp "${TMPDIR:-/tmp}/dora-${module}-required-go-test.XXXXXX")"
  test_output_files+=("$output_file")
  if ! run_command "$@" >"$output_file" 2>&1; then
    echo "$module required-mode 数据库测试命令失败" >&2
    cat "$output_file" >&2
    return 1
  fi

  # required-mode 只有目标测试真实执行且没有任何跳过才算证据；仅有 package pass、
  # "no tests to run" 或普通环境门禁 Skip 都必须失败关闭。
  local skipped
  skipped="$(grep -F '"Action":"skip"' "$output_file" | grep -F '"Test":' || true)"
  if [[ -n "$skipped" ]]; then
    echo "$module required-mode 数据库测试包含 skip" >&2
    printf '%s\n' "$skipped" >&2
    return 1
  fi

  local expected_tests=()
  local expected_test
  IFS=',' read -r -a expected_tests <<<"$expected_csv"
  for expected_test in "${expected_tests[@]}"; do
    if ! grep -F '"Action":"pass"' "$output_file" | grep -F "\"Test\":\"${expected_test}\"" >/dev/null; then
      echo "$module required-mode 数据库测试未通过目标 Test：$expected_test" >&2
      cat "$output_file" >&2
      return 1
    fi
  done

  echo "$module required-mode 数据库测试通过：${expected_csv//,/，}"
  rm -f -- "$output_file"
}

reset_module_contract_schema() {
  local module="$1"
  local database_url="$2"

  run_command "$migrate_bin" -path "$repo_root/$module/migrations" -database "$database_url" drop -f
  reset_migration_dir="$(mktemp -d "${TMPDIR:-/tmp}/dora-${module}-contract-reset.XXXXXX")"
  chmod 700 "$reset_migration_dir"
  printf 'DROP SCHEMA IF EXISTS %s CASCADE;\n' "$module" \
    >"$reset_migration_dir/000001_reset_contract_schema.up.sql"
  chmod 600 "$reset_migration_dir/000001_reset_contract_schema.up.sql"
  run_command "$migrate_bin" -path "$reset_migration_dir" -database "$database_url" up
  run_command "$migrate_bin" -path "$reset_migration_dir" -database "$database_url" drop -f
  rm -rf -- "$reset_migration_dir"
  reset_migration_dir=""
}

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
  # 时间戳文件名按 Shell glob 的稳定字典序展开，避免 pipefail 下 sort|head 的 SIGPIPE。
  migration_files=("$repo_root/$module/migrations/"*.up.sql)
  if [[ ! -e "${migration_files[0]}" ]]; then
    echo "$module 没有可执行的 up Migration" >&2
    exit 2
  fi
  first_migration="${migration_files[0]##*/}"
  first_version="${first_migration%%_*}"
  reset_module_contract_schema "$module" "$database_url"
  run_command "$migrate_bin" -path "$repo_root/$module/migrations" -database "$database_url" force "$first_version"
  run_command "$migrate_bin" -path "$repo_root/$module/migrations" -database "$database_url" down 1
  run_command "$migrate_bin" -path "$repo_root/$module/migrations" -database "$database_url" up

  case "$module" in
    business)
      run_required_go_tests business 'TestMigratedSchemaContract' -- \
        env DORA_POSTGRES_CONTRACT_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/business" test -json \
          -run '^TestMigratedSchemaContract$' -count=1 ./internal/postgres
      run_required_go_tests business 'TestProjectRepositoryPostgreSQLW0Semantics,TestRepositoryAuthPostgreSQLW0Semantics,TestAuthorizationRepositoryPostgreSQLLifecycle,TestSkillRepositoryPostgreSQLW1Semantics,TestProjectRepositoryPostgreSQLQuickCreateV2BatchSQL,TestProjectRepositoryPostgreSQLQuickCreateV2,TestSkillApprovalCannotCommitAfterConcurrentReviewerRevocation,TestSkillMarketMigrationPostgreSQLIndexContract,TestSkillMarketRepositoryPostgreSQLVisibilityAndPublisherPolicy,TestSkillMarketRepositoryPostgreSQLValidatesTwentyFirstCandidate' -- \
        env DORA_POSTGRES_CONTRACT_DSN="$database_url" \
        DORA_BUSINESS_TEST_POSTGRES_DSN="$database_url" \
        DORA_BUSINESS_TEST_ALLOW_DESTRUCTIVE=1 \
        GOWORK=off "$go_bin" -C "$repo_root/business" test -json \
          -run '^(TestProjectRepositoryPostgreSQLW0Semantics|TestRepositoryAuthPostgreSQLW0Semantics|TestAuthorizationRepositoryPostgreSQLLifecycle|TestSkillRepositoryPostgreSQLW1Semantics|TestProjectRepositoryPostgreSQLQuickCreateV2BatchSQL|TestProjectRepositoryPostgreSQLQuickCreateV2|TestSkillApprovalCannotCommitAfterConcurrentReviewerRevocation|TestSkillMarketMigrationPostgreSQLIndexContract|TestSkillMarketRepositoryPostgreSQLVisibilityAndPublisherPolicy|TestSkillMarketRepositoryPostgreSQLValidatesTwentyFirstCandidate)$' \
          -count=1 ./internal/postgres
	  run_required_go_tests business 'TestStoryboardPreviewRepositoryPostgreSQLSemantics' -- \
		env DORA_BUSINESS_TEST_POSTGRES_DSN="$database_url" \
		DORA_BUSINESS_TEST_ALLOW_DESTRUCTIVE=1 \
		GOWORK=off "$go_bin" -C "$repo_root/business" test -json \
		  -run '^TestStoryboardPreviewRepositoryPostgreSQLSemantics$' \
		  -count=1 ./internal/postgres
      ;;
    agent)
      run_required_go_tests agent 'TestMigratedSchemaContract' -- \
        env DORA_POSTGRES_CONTRACT_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/agent" test -json \
          -run '^TestMigratedSchemaContract$' -count=1 ./internal/postgres
      # Legacy Helper 必须在 fresh latest Schema 上验证；后续 Session Repository 契约会有意留下
      # 其他 Keyring 的 pristine legacy fixture，不能把它们混入本用例的专用 cohort。
      run_required_go_tests agent 'TestUserMessageLegacyUpgradeGuardsPostgreSQL,TestUserMessageLegacyUpgradePostgreSQLRecoveryAndConcurrency' -- \
        env DORA_USER_MESSAGE_RUNTIME_POSTGRES_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/agent" test -json \
          -run '^(TestUserMessageLegacyUpgradeGuardsPostgreSQL|TestUserMessageLegacyUpgradePostgreSQLRecoveryAndConcurrency)$' \
          -count=1 ./internal/postgres
      run_required_go_tests agent 'TestSessionRepositoryConcurrentEnsureContract' -- \
        env DORA_POSTGRES_CONTRACT_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/agent" test -json \
          -run '^TestSessionRepositoryConcurrentEnsureContract$' \
          -count=1 ./internal/postgres
      run_required_go_tests agent 'TestUserMessageRuntimePostgreSQLLifecycle' -- \
        env DORA_USER_MESSAGE_RUNTIME_POSTGRES_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/agent" test -json \
          -run '^TestUserMessageRuntimePostgreSQLLifecycle$' \
          -count=1 ./internal/postgres
      run_required_go_tests agent 'TestAnalyzeMaterialsRuntimePostgreSQLLifecycle' -- \
        env DORA_ANALYZE_MATERIALS_RUNTIME_POSTGRES_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/agent" test -json \
          -run '^TestAnalyzeMaterialsRuntimePostgreSQLLifecycle$' \
          -count=1 ./internal/postgres
	  run_required_go_tests agent 'TestPlanStoryboardRuntimePostgreSQLLifecycle' -- \
		env DORA_AGENT_TEST_POSTGRES_DSN="$database_url" GOWORK=off \
		"$go_bin" -C "$repo_root/agent" test -json \
		  -run '^TestPlanStoryboardRuntimePostgreSQLLifecycle$' \
		  -count=1 ./internal/postgres
      # Baseline 使用独立 reset 后的精确 Migration 005，不能受 latest forward Schema 或上一个
      # Repository Contract 留下的 V2 事实污染。它结束后保留 005 fixture 仅供人工诊断；下次运行先 reset。
      reset_module_contract_schema agent "$database_url"
      run_command "$migrate_bin" -path "$repo_root/agent/migrations" -database "$database_url" goto 20260714000500
      run_required_go_tests agent 'TestSessionLaneUpgradeBaselinePostgreSQLContract' -- \
        env DORA_POSTGRES_CONTRACT_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/agent" test -json \
          -run '^TestSessionLaneUpgradeBaselinePostgreSQLContract$' \
          -count=1 ./internal/postgres
      ;;
    worker)
      run_required_go_tests worker 'TestMigratedSchemaContract' -- \
        env DORA_POSTGRES_CONTRACT_DSN="$database_url" GOWORK=off \
        "$go_bin" -C "$repo_root/worker" test -json \
          -run '^TestMigratedSchemaContract$' -count=1 ./internal/postgres
      ;;
  esac
done
