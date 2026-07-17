#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
smoke_script="$repo_root/scripts/plan-storyboard-runtime-v2-smoke.sh"
browser_spec="$repo_root/frontend/e2e/plan-storyboard-runtime.spec.js"
makefile="$repo_root/Makefile"

fail() {
  printf 'plan-storyboard-runtime-v2 smoke contract failed: %s\n' "$1" >&2
  exit 1
}

[[ -x "$smoke_script" ]] || fail 'canonical smoke script is not executable'
[[ -r "$browser_spec" ]] || fail 'canonical Chromium spec is missing'
bash -n "$smoke_script" || fail 'canonical smoke script has invalid shell syntax'
node --check "$browser_spec" || fail 'canonical Chromium spec has invalid JavaScript syntax'

require_in() {
  local path="$1"
  local literal="$2"
  local message="$3"
  grep -F "$literal" "$path" >/dev/null || fail "$message"
}

require_smoke() {
  require_in "$smoke_script" "$1" "$2"
}

require_browser() {
  require_in "$browser_spec" "$1" "$2"
}

require_smoke '. "$repo_root/scripts/lib/smoke-secret-transport.sh"' 'secret transport guard is missing'
require_smoke 'disable_shell_xtrace' 'xtrace is not disabled'
require_smoke 'umask 077' 'restrictive umask is missing'
require_smoke "trap 'exit 130' INT" 'SIGINT is not fail-closed'
require_smoke "trap 'exit 143' TERM" 'SIGTERM is not fail-closed'
require_smoke 'DORA_SMOKE_POSTGRES_HOST:-127.0.0.1' 'PostgreSQL host is not pinned to loopback'
require_smoke 'DORA_SMOKE_POSTGRES_PORT:-15432' 'PostgreSQL port is not canonical'
require_smoke 'DORA_SMOKE_REDIS_HOST:-127.0.0.1' 'Redis host is not pinned to loopback'
require_smoke 'DORA_SMOKE_REDIS_PORT:-16379' 'Redis port is not canonical'
require_smoke 'DORA_SMOKE_ETCD_HOST:-127.0.0.1' 'etcd host is not pinned to loopback'
require_smoke 'DORA_SMOKE_ETCD_PORT:-12379' 'etcd port is not canonical'
require_smoke 'http://${etcd_host}:${etcd_port}/v3/kv/range' 'direct etcd registration query is missing'

for path in "$smoke_script" "$browser_spec"; do
  if rg -n '(/var/run/docker\.sock|docker[[:space:]]+(info|ps|compose|exec)|wait-for-local-infra\.sh|compose\.ya?ml|psql([[:space:]]|$)|redis-cli|etcdctl)' "$path" >/dev/null; then
    fail "canonical Trial depends on a container control plane or external database CLI: $path"
  fi
done

require_smoke 'x-migrations-table=${reset_table}' 'isolated reset migration ledger is missing'
require_smoke 'DROP SCHEMA IF EXISTS %s CASCADE;' 'isolated schema reset is missing'
require_smoke 'DROP TABLE IF EXISTS public.schema_migrations;' 'canonical migration ledger reset is missing'
require_smoke 'reset_test_database business "$BUSINESS_DATABASE_URL"' 'Business reset is not bound to its test DSN'
require_smoke 'reset_test_database agent "$AGENT_DATABASE_URL"' 'Agent reset is not bound to its test DSN'
require_smoke 'refusing to use a PostgreSQL database other than $database on the canonical host port' 'test database guard is missing'
postgres_probe_line="$(grep -n '^wait_tcp "\$postgres_host" "\$postgres_port" PostgreSQL$' "$smoke_script" | cut -d: -f1)"
business_reset_line="$(grep -n '^reset_test_database business "\$BUSINESS_DATABASE_URL"$' "$smoke_script" | cut -d: -f1)"
[[ -n "$postgres_probe_line" && -n "$business_reset_line" ]] || fail 'could not prove direct-host probe/reset order'
(( postgres_probe_line < business_reset_line )) || fail 'destructive reset runs before the mapped PostgreSQL port is reachable'

require_smoke 'GOWORK=off "$go_bin" -C "$repo_root/business" build' 'Business is not rebuilt from the worktree'
require_smoke 'GOWORK=off "$go_bin" -C "$repo_root/agent" build' 'Agent is not rebuilt from the worktree'
require_smoke '"$repo_root/scripts/migrate.sh" business up' 'Business migrations are missing'
require_smoke '"$repo_root/scripts/migrate.sh" agent up' 'Agent migrations are missing'
require_smoke 'go_bin" run -tags localsmoke ./cmd/local-smoke-seeder' 'local identity seed is missing'
require_smoke '"$repo_root/.local/bin/business-service"' 'Business is not started on the host'
require_smoke '"$repo_root/.local/bin/agent-service"' 'Agent is not started on the host'
require_smoke './node_modules/.bin/vite --host 127.0.0.1' 'Vite is not started on loopback'
require_smoke 'playwright test e2e/plan-storyboard-runtime.spec.js' 'Chromium Trial is not executed'

first_storyboard_line="$(grep -n '^enable_storyboard_profile$' "$smoke_script" | sed -n '1s/:.*//p')"
creation_spec_line="$(grep -n '^enable_creation_spec_profile$' "$smoke_script" | sed -n '1s/:.*//p')"
second_storyboard_line="$(grep -n '^enable_storyboard_profile$' "$smoke_script" | sed -n '2s/:.*//p')"
[[ -n "$first_storyboard_line" && -n "$creation_spec_line" && -n "$second_storyboard_line" ]] || \
  fail 'three canonical Profile phases are not explicit'
(( first_storyboard_line < creation_spec_line && creation_spec_line < second_storyboard_line )) || \
  fail 'Profile phase order must be Storyboard empty Lane -> CreationSpec Draft -> Storyboard browser'
[[ "$(grep -c '^enable_storyboard_profile$' "$smoke_script")" == "2" ]] || \
  fail 'Storyboard Profile must be enabled exactly twice'
[[ "$(grep -c '^enable_creation_spec_profile$' "$smoke_script")" == "1" ]] || \
  fail 'CreationSpec Profile must be enabled exactly once'

require_smoke 'configure_creation_spec_addresses "$local_ipv4"' 'CreationSpec phase does not use the discovered host IPv4'
require_smoke 'BUSINESS_HTTP_ADDR="0.0.0.0:${business_http_port}"' 'CreationSpec Business HTTP wildcard bind is missing'
require_smoke 'BUSINESS_RPC_LISTEN_ADDR="${local_ipv4}:${business_rpc_port}"' 'CreationSpec Business RPC host bind is missing'
require_smoke 'AGENT_RPC_LISTEN_ADDR="${local_ipv4}:${agent_rpc_port}"' 'CreationSpec Agent RPC host bind is missing'
require_smoke 'BUSINESS_RPC_ADVERTISED_ADDRESS="${local_ipv4}:${business_rpc_port}"' 'CreationSpec Business RPC advertised host is missing'
require_smoke 'AGENT_RPC_ADVERTISED_ADDRESS="${local_ipv4}:${agent_rpc_port}"' 'CreationSpec Agent RPC advertised host is missing'
require_smoke 'configure_storyboard_addresses' 'Storyboard exact-loopback address phase is missing'
require_smoke 'BUSINESS_HTTP_ADDR="127.0.0.1:${business_http_port}"' 'Storyboard Business HTTP is not exact loopback'
require_smoke 'BUSINESS_RPC_LISTEN_ADDR="127.0.0.1:${business_rpc_port}"' 'Storyboard Business RPC is not exact loopback'
require_smoke 'AGENT_HTTP_ADDR="127.0.0.1:${agent_http_port}"' 'Storyboard Agent HTTP is not exact loopback'
require_smoke 'AGENT_RPC_LISTEN_ADDR="127.0.0.1:${agent_rpc_port}"' 'Storyboard Agent RPC is not exact loopback'

require_smoke 'export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=true' 'CreationSpec Processor enable is missing'
require_smoke 'export DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED=false' 'CreationSpec phase does not disable Storyboard'
require_smoke 'export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false' 'Storyboard phase does not disable CreationSpec'
require_smoke 'export DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED=true' 'Storyboard Runtime is not enabled'
require_smoke 'export DORA_AGENT_PLAN_STORYBOARD_RUNTIME_PROFILE=plan_storyboard.runtime.v2preview1' 'Storyboard Profile is not pinned'
require_smoke 'export AGENT_SSE_MAX_CONNECTION_DURATION=20s' 'Storyboard SSE connection duration is not bounded below shutdown timeout'
require_smoke 'export AGENT_SHUTDOWN_TIMEOUT=35s' 'Storyboard shutdown timeout does not leave SSE drain headroom'
require_smoke 'export DORA_BUSINESS_PLAN_STORYBOARD_RUNTIME_ENABLED=true' 'Business Storyboard BFF/RPC gate is not enabled'
require_smoke 'export DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false' 'User Message Runtime is not disabled'
require_smoke 'export DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=false' 'Analyze Materials Runtime is not disabled'
require_smoke 'AGENT_SSE_MAX_EVENT_BYTES must be 131072' 'Storyboard SSE envelope headroom guard is missing'

require_smoke 'quick_payload='"'"'{"initial_prompt":null}'"'"'' 'empty QuickCreate Lane is missing'
require_smoke '.messages == [] and .inputs == []' 'empty Workspace Lane assertion is missing'
require_smoke '.creation_spec_preview == null and .plan_storyboard_preview == null' 'empty projection assertion is missing'
require_smoke '/api/v1/agent/sessions/${session_id}/creation-spec-previews' 'CreationSpec Draft preparation endpoint is missing'
require_smoke 'plan_creation_spec.preview.enqueue.v1' 'CreationSpec enqueue schema is not asserted'
require_smoke '.creation_spec_preview.status == "draft"' 'CreationSpec Draft terminal state is not asserted'
require_smoke 'stop_profile_runtimes' 'exclusive Profile shutdown barrier is missing'
require_smoke '/api/v1/agent/sessions/${session_id}/events?after_seq=${creation_high_watermark}' 'Storyboard SSE replay cursor is missing'
require_smoke 'event: plan_storyboard.preview.accepted' 'Storyboard accepted SSE is not asserted'
require_smoke 'event: plan_storyboard.preview.completed' 'Storyboard terminal SSE is not asserted'

require_browser "browserName).toBe('chromium')" 'browser test does not require Chromium'
require_browser 'plan_storyboard.preview.enqueue-request.v1' 'browser form request schema is not asserted'
require_browser 'plan_storyboard.preview.enqueue.v1' 'browser enqueue response schema is not asserted'
require_browser 'storyboard.preview.card.v1' 'browser Snapshot/Card schema is not asserted'
require_browser 'STORYBOARD_PREVIEW_DRAFT_CREATED' 'browser result code assertion drifted'
require_browser "'plan_storyboard.preview.accepted'" 'browser accepted SSE observation is missing'
require_browser "'plan_storyboard.preview.completed'" 'browser terminal SSE observation is missing'
require_browser 'await page.reload()' 'browser hard refresh is missing'
require_browser "data-tool-availability', 'unavailable'" 'static Catalog unavailable assertion is missing'
require_browser 'agent-restart-request.json' 'browser restart control checkpoint is missing'
require_browser "data-stream-state', 'reconnecting'" 'browser disconnect observation is missing'
require_browser "name: '重新连接工作台'" 'manual reconnect fallback is missing'
require_browser 'agent-restart-ack.json' 'browser restart acknowledgement is missing'
require_smoke 'stop_pid_strict "$agent_pid" Agent-restart-checkpoint' 'managed Agent disconnect is missing'
require_smoke 'wait "$pid" || fail "$label returned a failure during graceful shutdown"' 'managed Agent shutdown no longer checks the strict process exit status'
require_smoke 'wait_for_control_file "$disconnect_observed"' 'shell does not wait for browser disconnect evidence'
require_smoke 'start_agent storyboard-reconnect' 'managed Agent reconnect is missing'

require_smoke 'source-before.sha256' 'source manifest before-run barrier is missing'
require_smoke 'source-after.sha256' 'source manifest after-run barrier is missing'
require_smoke 'cmp -s "$work_dir/source-before.sha256" "$work_dir/source-after.sha256"' 'source no-drift comparison is missing'
require_smoke 'plan_storyboard_runtime_v2_smoke_evidence.v1' 'redacted Evidence schema is missing'
require_smoke 'assert_evidence_redacted' 'Evidence redaction scan is missing'
require_smoke 'chmod 600 "$evidence_file"' 'published Evidence mode is not restricted'
require_smoke 'published Evidence permissions are not 0600' 'published Evidence mode is not verified'
require_smoke 'rm -f "$evidence_file"' 'failed Evidence cleanup is missing'
require_smoke 'wait_etcd_prefix_count '"'"'/dora/services/dora-agent-service/'"'"' 0 Agent' 'Agent etcd cleanup is not asserted'
require_smoke 'wait_etcd_prefix_count '"'"'/dora/services/dora-business-service/'"'"' 0 Business' 'Business etcd cleanup is not asserted'
require_smoke 'wait_port_closed "$vite_port" Vite' 'Vite cleanup is not asserted'

require_in "$makefile" 'test-plan-storyboard-runtime-smoke:' 'static smoke target is missing from Makefile'
require_in "$makefile" './scripts/tests/plan-storyboard-runtime-v2-smoke-test.sh' 'static smoke test is not in the aggregate gate'
require_in "$makefile" 'plan-storyboard-runtime-smoke: migration-tools check-frontend' 'canonical smoke target is missing from Makefile'
require_in "$makefile" './scripts/plan-storyboard-runtime-v2-smoke.sh' 'canonical smoke target does not invoke the script'

printf 'plan-storyboard-runtime-v2 smoke contract passed\n'
