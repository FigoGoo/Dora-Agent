#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
smoke_script="$repo_root/scripts/smoke-user-message-runtime.sh"
browser_spec="$repo_root/frontend/e2e/user-message-runtime.spec.js"
makefile="$repo_root/Makefile"

fail() {
  printf 'user-message-runtime smoke contract failed: %s\n' "$1" >&2
  exit 1
}

signal_probe_pid=""
signal_probe_child_pid=""
cleanup_signal_probe() {
  [[ -z "$signal_probe_child_pid" ]] || kill -KILL "$signal_probe_child_pid" 2>/dev/null || true
  [[ -z "$signal_probe_pid" ]] || kill -KILL "$signal_probe_pid" 2>/dev/null || true
}
trap cleanup_signal_probe EXIT

[[ -x "$smoke_script" ]] || fail 'canonical smoke script is not executable'
[[ -r "$browser_spec" ]] || fail 'real browser spec is missing'
bash -n "$smoke_script" || fail 'canonical smoke script has invalid shell syntax'
node --check "$browser_spec" >/dev/null || fail 'browser spec has invalid JavaScript syntax'

# 最小动态回归：TERM 必须返回 143，且 EXIT cleanup 必须停止受管前台子进程。
bash -c '
  child_pid=""
  cleanup() {
    if [[ -n "$child_pid" ]] && kill -0 "$child_pid" 2>/dev/null; then
      kill -TERM "$child_pid" 2>/dev/null || true
    fi
    wait "$child_pid" 2>/dev/null || true
  }
  trap cleanup EXIT
  trap "exit 130" INT
  trap "exit 143" TERM
  sleep 30 &
  child_pid="$!"
  wait "$child_pid"
' &
signal_probe_pid="$!"
for _ in $(seq 1 40); do
  signal_probe_child_pid="$(pgrep -P "$signal_probe_pid" | head -n 1 || true)"
  [[ -n "$signal_probe_child_pid" ]] && break
  sleep 0.05
done
[[ -n "$signal_probe_child_pid" ]] || fail 'signal cleanup probe did not start its managed child'
kill -TERM "$signal_probe_pid"
set +e
wait "$signal_probe_pid"
signal_probe_status="$?"
set -e
signal_probe_pid=""
[[ "$signal_probe_status" == "143" ]] || fail "TERM returned $signal_probe_status instead of 143"
for _ in $(seq 1 40); do
  if ! kill -0 "$signal_probe_child_pid" 2>/dev/null; then
    break
  fi
  sleep 0.05
done
if kill -0 "$signal_probe_child_pid" 2>/dev/null; then
  fail 'TERM cleanup left its managed child alive'
fi
signal_probe_child_pid=""

target_recipe="$(awk '
  /^user-message-runtime-smoke:/ {capture=1; print; next}
  capture && /^[^[:space:]#][^:]*:/ {exit}
  capture {print}
' "$makefile")"
[[ "$target_recipe" == *'user-message-runtime-smoke: migration-tools check-frontend'* ]] || \
  fail 'Make target does not require migration tooling and frontend verification'
[[ "$target_recipe" == *'./scripts/smoke-user-message-runtime.sh'* ]] || \
  fail 'Make target does not execute the canonical real smoke script'
[[ "$target_recipe" != *'go test'* && "$target_recipe" != *'vitest'* ]] || \
  fail 'Make target uses unit tests as the smoke result'

test_gate_recipe="$(awk '
  /^test-user-message-runtime-smoke:/ {capture=1; print; next}
  capture && /^[^[:space:]#][^:]*:/ {exit}
  capture {print}
' "$makefile")"
[[ "$test_gate_recipe" == *'./scripts/tests/user-message-runtime-smoke-test.sh'* ]] || \
  fail 'Make test gate does not execute the smoke contract test'
smoke_contracts_recipe="$(awk '
  /^test-smoke-contracts:/ {capture=1; print; next}
  capture && /^[^[:space:]#][^:]*:/ {exit}
  capture {print}
' "$makefile")"
[[ "$smoke_contracts_recipe" == *'./scripts/tests/user-message-runtime-smoke-test.sh'* ]] || \
  fail 'root smoke contract gate does not include the user-message-runtime contract'

grep -F '. "$repo_root/scripts/lib/smoke-secret-transport.sh"' "$smoke_script" >/dev/null || \
  fail 'smoke script does not load the secret transport guard'
grep -F 'disable_shell_xtrace' "$smoke_script" >/dev/null || fail 'smoke script does not disable xtrace'
grep -F 'umask 077' "$smoke_script" >/dev/null || fail 'smoke script does not set a restrictive umask'
grep -F "trap 'exit 130' INT" "$smoke_script" >/dev/null || fail 'SIGINT is not fail-closed'
grep -F "trap 'exit 143' TERM" "$smoke_script" >/dev/null || fail 'SIGTERM is not fail-closed'
grep -F 'DORA_SMOKE_POSTGRES_PORT:-15432' "$smoke_script" >/dev/null || fail 'PostgreSQL exposed port is not canonical'
grep -F 'DORA_SMOKE_REDIS_PORT:-16379' "$smoke_script" >/dev/null || fail 'Redis exposed port is not canonical'
grep -F 'DORA_SMOKE_ETCD_PORT:-12379' "$smoke_script" >/dev/null || fail 'etcd exposed port is not canonical'
grep -F 'wait_tcp "$postgres_host" "$postgres_port" PostgreSQL' "$smoke_script" >/dev/null || \
  fail 'PostgreSQL is not checked through its host port'
grep -F 'wait_tcp "$redis_host" "$redis_port" Redis' "$smoke_script" >/dev/null || \
  fail 'Redis is not checked through its host port'
grep -F 'http://${etcd_host}:${etcd_port}/health' "$smoke_script" >/dev/null || \
  fail 'etcd is not checked through its host port'
grep -F 'http://${etcd_host}:${etcd_port}/v3/kv/range' "$smoke_script" >/dev/null || \
  fail 'etcd registration provenance is not queried through the exposed host port'
if rg -n '/var/run/docker\.sock|docker[[:space:]]+info|docker[[:space:]]+compose|wait-for-local-infra\.sh' "$smoke_script" >/dev/null; then
  fail 'canonical smoke contains a Docker socket/Compose readiness dependency'
fi
grep -F 'wait_etcd_prefix_empty "/dora/services/dora.business.foundation.v1/"' "$smoke_script" >/dev/null || \
  fail 'Business etcd preflight/shutdown isolation is missing'
grep -F 'wait_etcd_prefix_empty "/dora/services/dora.agent.session.v1/"' "$smoke_script" >/dev/null || \
  fail 'Agent etcd preflight/shutdown isolation is missing'
grep -F 'assert_etcd_registration "/dora/services/dora.business.foundation.v1/"' "$smoke_script" >/dev/null || \
  fail 'Business runtime provenance is not bound to its exact etcd registration'
grep -F 'assert_etcd_registration "/dora/services/dora.agent.session.v1/"' "$smoke_script" >/dev/null || \
  fail 'Agent runtime provenance is not bound to its exact etcd registration'
grep -F 'monitor_etcd_provenance &' "$smoke_script" >/dev/null || \
  fail 'continuous etcd provenance monitor is not started'
grep -F 'sleep 0.25' "$smoke_script" >/dev/null || \
  fail 'continuous etcd provenance monitor interval drifted'
grep -F 'service_prefix_drift' "$smoke_script" >/dev/null || \
  fail 'continuous etcd provenance drift is not fail-closed'

grep -F 'export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false' "$smoke_script" >/dev/null || \
  fail 'CreationSpec Preview is not forcibly disabled'
grep -F 'export DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=true' "$smoke_script" >/dev/null || \
  fail 'User Message Runtime is not forcibly enabled'
grep -F 'export DORA_AGENT_USER_MESSAGE_RUNTIME_PROFILE=user_message.runtime.v2preview1' "$smoke_script" >/dev/null || \
  fail 'User Message Runtime profile is not forcibly pinned'
grep -F 'export AGENT_DATABASE_URL="$AGENT_CONTRACT_DATABASE_URL"' "$smoke_script" >/dev/null || \
  fail 'Agent Runtime does not use the explicit contract database DSN'
grep -F '/dora_agent_test?sslmode=disable' "$smoke_script" >/dev/null || \
  fail 'dedicated dora_agent_test DSN gate is missing'
grep -F 'refusing to reset a PostgreSQL database other than dora_agent_test' "$smoke_script" >/dev/null || \
  fail 'destructive reset is not fail-closed on the exact test database identity'
grep -F 'DROP SCHEMA IF EXISTS agent CASCADE;' "$smoke_script" >/dev/null || \
  fail 'dedicated Agent test schema reset is missing'
grep -F 'DROP TABLE IF EXISTS public.schema_migrations;' "$smoke_script" >/dev/null || \
  fail 'dedicated Agent test migration ledger reset is missing'
if rg -n -- '-U dora_admin -d dora_agent([[:space:]\\]|$)' "$smoke_script" >/dev/null; then
  fail 'canonical smoke still queries or mutates the non-test Agent database'
fi
grep -F 'VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED=false' "$smoke_script" >/dev/null || \
  fail 'Vite CreationSpec Preview flag is not explicitly disabled'

grep -F 'GOWORK=off "$go_bin" -C "$repo_root/business" build' "$smoke_script" >/dev/null || \
  fail 'Business Runtime is not rebuilt from the current worktree'
grep -F 'GOWORK=off "$go_bin" -C "$repo_root/agent" build' "$smoke_script" >/dev/null || \
  fail 'Agent Runtime is not rebuilt from the current worktree'
grep -F '"$repo_root/scripts/migrate.sh" business up' "$smoke_script" >/dev/null || fail 'Business migration is missing'
grep -F '"$repo_root/scripts/migrate.sh" agent up' "$smoke_script" >/dev/null || fail 'Agent migration is missing'
grep -F 'go_bin" run -tags localsmoke ./cmd/local-smoke-seeder' "$smoke_script" >/dev/null || \
  fail 'local smoke identity seeding is missing'
grep -F 'go_bin" run -tags localsmoke ./cmd/local-smoke-user-message-legacy-seeder' "$smoke_script" >/dev/null || \
  fail 'local smoke legacy user_message seeding is missing'
grep -F 'wait_legacy_runtime_completed "$legacy_session_id"' "$smoke_script" >/dev/null || \
  fail 'managed Agent does not wait for the seeded legacy input to reach terminal Runtime authority'
reset_line="$(grep -n '^reset_agent_test_schema "\$work_dir/agent-test-reset.log"$' "$smoke_script" | cut -d: -f1)"
agent_migration_line="$(grep -n 'scripts/migrate.sh" agent up' "$smoke_script" | cut -d: -f1)"
legacy_seed_line="$(grep -n 'go_bin" run -tags localsmoke ./cmd/local-smoke-user-message-legacy-seeder' "$smoke_script" | cut -d: -f1)"
agent_start_line="$(grep -n '^"\$repo_root/.local/bin/agent-service"' "$smoke_script" | cut -d: -f1)"
[[ -n "$reset_line" && -n "$agent_migration_line" && -n "$legacy_seed_line" && -n "$agent_start_line" ]] || \
  fail 'could not prove reset/migration/legacy-seed/Agent startup order'
(( reset_line < agent_migration_line && agent_migration_line < legacy_seed_line && legacy_seed_line < agent_start_line )) || \
  fail 'legacy Trial order must be dedicated reset, migration, legacy seeding, then Agent bootstrap'
grep -F '"$repo_root/.local/bin/business-service"' "$smoke_script" >/dev/null || fail 'Business Runtime is not started'
grep -F '"$repo_root/.local/bin/agent-service"' "$smoke_script" >/dev/null || fail 'Agent Runtime is not started'
grep -F './node_modules/.bin/vite --host 127.0.0.1' "$smoke_script" >/dev/null || fail 'Vite is not started locally'
grep -F 'DORA_E2E_EXTERNAL_SERVER=1' "$smoke_script" >/dev/null || fail 'Playwright does not use the managed Vite process'
grep -F 'playwright_pid="$!"' "$smoke_script" >/dev/null || fail 'Playwright PID is not tracked'
grep -F 'stop_pid_best_effort "$playwright_pid"' "$smoke_script" >/dev/null || fail 'Playwright cleanup is missing'
grep -F './node_modules/.bin/playwright test' "$smoke_script" >/dev/null || fail 'real Playwright is not executed'
grep -F 'e2e/user-message-runtime.spec.js --grep' "$smoke_script" >/dev/null || fail 'canonical browser spec is not selected'
grep -F "'@user-message-runtime'" "$smoke_script" >/dev/null || fail 'canonical browser tag is not selected'

for required_table in \
  'agent.session_user_message_turn' \
  'agent.session_user_message_turn_context' \
  'agent.session_user_message_run' \
  'agent.session_user_message_model_receipt' \
  'agent.session_user_message_output_receipt' \
  'agent.session_user_message_output_projection' \
  'agent.session_user_message_upgrade_ledger' \
  'agent.session_command_receipt' \
  'agent.session_event_log' \
  'agent.creation_spec_preview_run'; do
  grep -F "$required_table" "$smoke_script" >/dev/null || fail "PostgreSQL authority query is missing $required_table"
done
grep -F 'user_message.empty_tools@v1' "$smoke_script" >/dev/null || fail 'empty Tool Registry authority is not checked'
grep -F 'local.fake.user_message@v1' "$smoke_script" >/dev/null || fail 'local Fake route authority is not checked'
grep -F 'model_receipt.execution_fence = run_record.owner_fence' "$smoke_script" >/dev/null || \
  fail 'model execution fence is not bound to the terminal Run fence'
grep -F 'creation_spec_preview_run_count == 0' "$smoke_script" >/dev/null || \
  fail 'CreationSpec zero-side-effect assertion is missing'
grep -F '.ledger_stage == "verified"' "$smoke_script" >/dev/null || \
  fail 'legacy Ledger verified authority is not asserted'
grep -F '.upgrade_generation == 1 and .ledger_version == 3' "$smoke_script" >/dev/null || \
  fail 'legacy Ledger generation/version authority is not asserted'
grep -F 'legacy_ids_preserved:true' "$smoke_script" >/dev/null || \
  fail 'legacy stable ID preservation is not recorded in Trial Evidence'
grep -F 'legacy_ledger_verified:true' "$smoke_script" >/dev/null || \
  fail 'legacy Ledger verification is not recorded in Trial Evidence'
grep -F 'legacy_runtime_authority_unique:true' "$smoke_script" >/dev/null || \
  fail 'legacy Runtime uniqueness is not recorded in Trial Evidence'
grep -F 'identity:{session_id:$legacy_session_id,input_id:$legacy_input_id,message_id:$legacy_message_id' "$smoke_script" >/dev/null || \
  fail 'redacted legacy stable IDs are not included in Trial Evidence'
grep -F 'upgrade_ledger:{stage:$legacy_authority[0].ledger_stage' "$smoke_script" >/dev/null || \
  fail 'verified legacy Ledger facts are not included in Trial Evidence'

grep -F 'user_message_runtime.browser_result.v1' "$smoke_script" >/dev/null || fail 'browser result schema is not validated'
grep -F 'user_message_runtime.trial_evidence.v1' "$smoke_script" >/dev/null || fail 'Trial Evidence schema is missing'
grep -F '.status = "passed" | .assertions.evidence_redacted = true' "$smoke_script" >/dev/null || \
  fail 'passed publication is not gated by the redaction transition'
grep -F 'file_mode "$evidence_file"' "$smoke_script" >/dev/null || fail '0600 Evidence mode is not verified'
grep -F 'source tree changed during canonical Trial' "$smoke_script" >/dev/null || fail 'source zero-delta barrier is missing'
grep -F 'stop_pid_best_effort "$vite_pid"' "$smoke_script" >/dev/null || fail 'Vite cleanup is missing'
grep -F 'stop_pid_best_effort "$agent_pid"' "$smoke_script" >/dev/null || fail 'Agent cleanup is missing'
grep -F 'stop_pid_best_effort "$business_pid"' "$smoke_script" >/dev/null || fail 'Business cleanup is missing'
grep -F 'local Runtime cleanup left port' "$smoke_script" >/dev/null || fail 'cleanup verification is missing'
grep -F 'etcd_lease_removed:true' "$smoke_script" >/dev/null || fail 'etcd lease removal is not recorded'
grep -F 'etcd_continuous_provenance:true' "$smoke_script" >/dev/null || \
  fail 'continuous etcd provenance is not recorded'

if rg -n 'page\.route|context\.route|route\.fulfill|route\.abort|intercept' "$browser_spec" >/dev/null; then
  fail 'browser scenario contains request interception'
fi
grep -F "const RESULT_SCHEMA = 'user_message_runtime.browser_result.v1'" "$browser_spec" >/dev/null || \
  fail 'browser result schema drifted'
grep -F "process.env.DORA_E2E_USER_MESSAGE_RUNTIME === '1'" "$browser_spec" >/dev/null || \
  fail 'browser scenario is not fail-closed behind its explicit gate'
grep -F "@user-message-runtime real browser vertical slice" "$browser_spec" >/dev/null || fail 'browser tag drifted'
grep -F "expect(snapshot.event_high_watermark).toBe(3)" "$browser_spec" >/dev/null || fail 'event high watermark is not asserted'
grep -F "document.activeElement === element" "$browser_spec" >/dev/null || fail 'toolbox focus is not observed from the DOM'
grep -F 'quick_create_input_preserved:' "$browser_spec" >/dev/null || fail 'safe QuickCreate preservation assertion is missing'
if grep -F 'quick_create_original_prompt:' "$browser_spec" >/dev/null; then
  fail 'browser result uses a sensitive Prompt-derived assertion key'
fi
grep -F "role: 'user', content: expectedPrompt" "$browser_spec" >/dev/null || fail 'original user message is not asserted'
grep -F "role === 'assistant'" "$browser_spec" >/dev/null || fail 'Assistant Message absence is not asserted'
grep -F "mode: 0o600" "$browser_spec" >/dev/null || fail 'browser result is not created with 0600 mode'
grep -F 'await rename(temporaryPath, path)' "$browser_spec" >/dev/null || fail 'browser result is not atomically published'

result_payload="$(awk '
  /await writeAtomicJSON\(resultPath, \{/ {capture=1}
  capture {print}
  capture && /^      \}\);/ {exit}
' "$browser_spec")"
[[ "$result_payload" != *'prompt:'* && "$result_payload" != *'email:'* && "$result_payload" != *'password:'* ]] || \
  fail 'browser result payload contains a protected value'

if rg -n '(^|[[:space:]])@true([[:space:]]|$)|echo.*passed' "$smoke_script" >/dev/null; then
  fail 'smoke script contains an unconditional passed placeholder'
fi

printf 'user-message-runtime smoke contract passed\n'
