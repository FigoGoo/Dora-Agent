#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
smoke_script="$repo_root/scripts/write-prompts-runtime-v2-smoke.sh"
browser_spec="$repo_root/frontend/e2e/write-prompts-runtime.spec.js"

fail() {
  printf 'write-prompts-runtime-v2 smoke contract failed: %s\n' "$1" >&2
  exit 1
}

[[ -x "$smoke_script" ]] || fail 'canonical smoke script is not executable'
[[ -r "$browser_spec" ]] || fail 'canonical Chromium spec is missing'
bash -n "$smoke_script" || fail 'canonical smoke script has invalid shell syntax'
node --check "$browser_spec" || fail 'canonical Chromium spec has invalid JavaScript syntax'

require_in() {
  local path="$1" literal="$2" message="$3"
  grep -F "$literal" "$path" >/dev/null || fail "$message"
}

require_smoke() { require_in "$smoke_script" "$1" "$2"; }
require_browser() { require_in "$browser_spec" "$1" "$2"; }

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
(( postgres_probe_line < business_reset_line )) || fail 'destructive reset runs before mapped PostgreSQL is reachable'

require_smoke 'GOWORK=off "$go_bin" -C "$repo_root/business" build' 'Business is not rebuilt from worktree'
require_smoke 'GOWORK=off "$go_bin" -C "$repo_root/agent" build' 'Agent is not rebuilt from worktree'
require_smoke '"$repo_root/scripts/migrate.sh" business up' 'Business migrations are missing'
require_smoke '"$repo_root/scripts/migrate.sh" agent up' 'Agent migrations are missing'
require_smoke 'go_bin" run -tags localsmoke ./cmd/local-smoke-seeder' 'local identity seed is missing'
require_smoke '"$repo_root/.local/bin/business-service"' 'Business is not started on host'
require_smoke '"$repo_root/.local/bin/agent-service"' 'Agent is not started on host'
require_smoke './node_modules/.bin/vite --host 127.0.0.1' 'Vite is not loopback-bound'
require_smoke 'playwright test e2e/write-prompts-runtime.spec.js' 'Chromium Trial is not executed'

first_write_line="$(grep -n '^enable_write_prompts_profile$' "$smoke_script" | sed -n '1s/:.*//p')"
creation_line="$(grep -n '^enable_creation_spec_profile$' "$smoke_script" | sed -n '1s/:.*//p')"
second_write_line="$(grep -n '^enable_write_prompts_profile$' "$smoke_script" | sed -n '2s/:.*//p')"
[[ -n "$first_write_line" && -n "$creation_line" && -n "$second_write_line" ]] || \
  fail 'three canonical Profile phases are not explicit'
(( first_write_line < creation_line && creation_line < second_write_line )) || \
  fail 'Profile phase order must be empty WritePrompts -> CreationSpec -> safe Storyboard fixture -> WritePrompts browser'
[[ "$(grep -c '^enable_write_prompts_profile$' "$smoke_script")" == 2 ]] || fail 'WritePrompts Profile must be enabled exactly twice'
[[ "$(grep -c '^enable_creation_spec_profile$' "$smoke_script")" == 1 ]] || fail 'CreationSpec Profile must be enabled exactly once'

require_smoke 'disable_all_preview_profiles' 'exclusive Profile reset is missing'
require_smoke 'export DORA_AGENT_WRITE_PROMPTS_RUNTIME_ENABLED=true' 'Agent write_prompts flag is missing'
require_smoke 'export DORA_AGENT_WRITE_PROMPTS_RUNTIME_PROFILE=write_prompts.runtime.v2preview1' 'Agent profile is not pinned'
require_smoke 'export DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_ENABLED=true' 'Business write_prompts flag is missing'
require_smoke 'export DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_PROFILE=write_prompts.runtime.v2preview1' 'Business profile is not pinned'
require_smoke 'export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false' 'CreationSpec Runtime is not disabled in target reset'
require_smoke 'export DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false' 'User Message Runtime is not disabled'
require_smoke 'export DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=false' 'Analyze Materials Runtime is not disabled'
require_smoke 'export DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED=false' 'Storyboard Runtime is not disabled in target reset'
require_smoke 'export DORA_BUSINESS_PLAN_STORYBOARD_RUNTIME_ENABLED=false' 'Business Storyboard Runtime is not disabled in target reset'
require_smoke 'VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED=false' 'frontend CreationSpec flag is not disabled'
require_smoke 'VITE_DORA_PLAN_STORYBOARD_RUNTIME_ENABLED=false' 'frontend Storyboard flag is not disabled'
require_smoke 'VITE_DORA_WRITE_PROMPTS_RUNTIME_ENABLED=true' 'frontend write_prompts flag is not enabled'
require_smoke 'AGENT_SSE_MAX_EVENT_BYTES must be at least 131072' 'Prompt SSE envelope guard is missing'

require_smoke '/creation-spec-previews' 'authoritative CreationSpec public source path is missing'
require_smoke 'seed_storyboard_preview_fixture' 'safe Storyboard Preview fixture is missing'
require_smoke 'INSERT INTO business.storyboard_preview_draft' 'Business-owned Storyboard Preview fixture is missing'
require_smoke 'INSERT INTO agent.plan_storyboard_preview_turn_context' 'Agent Workspace Storyboard context fixture is missing'
require_smoke 'UPDATE agent.session_sequence_counter' 'Storyboard fixture must advance the Session input sequence counter'
require_smoke "'plan_storyboard.preview.completed'" 'Agent Workspace Storyboard Card fixture is missing'
require_smoke 'x-migrations-table=dora_business_storyboard_fixture' 'Business fixture migration ledger is not isolated'
require_smoke 'x-migrations-table=dora_agent_storyboard_fixture' 'Agent fixture migration ledger is not isolated'
require_smoke '.plan_storyboard_preview.slots | length' 'Storyboard Source must contain real slots'
require_smoke 'Profile switch did not preserve the authoritative Storyboard Source' 'Source preservation guard is missing'
require_browser '/write-prompts-previews' 'write_prompts BFF endpoint is missing'
require_browser 'write_prompts.preview.enqueue-request.v1' 'write_prompts request schema is not pinned'
require_browser 'write_prompts.preview.enqueue.v1' 'write_prompts response schema is not pinned'
require_smoke 'event: write_prompts.preview.accepted' 'accepted SSE is not asserted'
require_smoke 'event: write_prompts.preview.completed' 'terminal SSE is not asserted'

require_browser "browserName).toBe('chromium')" 'browser test does not require Chromium'
require_browser 'write_prompts.preview.enqueue-request.v1' 'browser form request schema is not asserted'
require_browser 'write_prompts.preview.enqueue.v1' 'browser enqueue schema is not asserted'
require_browser 'prompt.preview.card.v1' 'browser Card schema is not asserted'
require_browser 'PROMPT_PREVIEW_DRAFT_CREATED' 'browser result code assertion drifted'
require_browser "'write_prompts.preview.accepted'" 'browser accepted SSE observation is missing'
require_browser "'write_prompts.preview.completed'" 'browser terminal SSE observation is missing'
require_browser 'await page.reload()' 'browser hard refresh is missing'
require_browser "data-tool-availability', 'unavailable'" 'static Catalog unavailable assertion is missing'
require_browser 'agent-restart-request.json' 'browser restart control checkpoint is missing'
require_browser "data-stream-state', 'reconnecting'" 'browser disconnect observation is missing'
require_browser "name: '重新连接工作台'" 'manual reconnect fallback is missing'
require_browser 'agent-restart-ack.json' 'browser restart acknowledgement is missing'

require_smoke 'call_kind = '\''router'\''' 'Router Model receipt count is not checked'
require_smoke 'call_kind = '\''graph_prompt'\''' 'Graph Model receipt count is not checked'
require_smoke 'business.prompt_preview_draft' 'Business Prompt Draft count is not checked'
require_smoke 'business.prompt_preview_command_receipt' 'Business Command Receipt count is not checked'
require_smoke "to_regclass('business.prompt_artifact')" 'production PromptArtifact absence is not checked'
require_smoke "to_regclass('agent.operation')" 'production async aggregate absence is not checked'
require_smoke 'source-before.sha256' 'source manifest before-run barrier is missing'
require_smoke 'source-after.sha256' 'source manifest after-run barrier is missing'
require_smoke 'cmp -s "$work_dir/source-before.sha256" "$work_dir/source-after.sha256"' 'source no-drift comparison is missing'
require_smoke 'write_prompts_runtime_v2_smoke_evidence.v1' 'redacted Evidence schema is missing'
require_smoke 'assert_evidence_redacted' 'Evidence redaction scan is missing'
require_smoke 'chmod 600 "$evidence_file"' 'published Evidence mode is not restricted'
require_smoke 'published Evidence permissions are not 0600' 'published Evidence mode is not verified'
require_smoke 'rm -f "$evidence_file"' 'failed Evidence cleanup is missing'
require_smoke "wait_etcd_prefix_count '/dora/services/dora-agent-service/' 0 Agent" 'Agent etcd cleanup is not asserted'
require_smoke "wait_etcd_prefix_count '/dora/services/dora-business-service/' 0 Business" 'Business etcd cleanup is not asserted'
require_smoke 'wait_port_closed "$vite_port" Vite' 'Vite cleanup is not asserted'

printf 'write-prompts-runtime-v2 smoke contract passed\n'
