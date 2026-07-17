#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
smoke_script="$repo_root/scripts/analyze-materials-runtime-v2-smoke.sh"
agent_helper="$repo_root/agent/cmd/local-smoke-analyze-materials-authority/main.go"
business_helper="$repo_root/business/cmd/local-smoke-analyze-materials-fixture/main.go"
business_repository="$repo_root/business/cmd/local-smoke-analyze-materials-fixture/repository.go"
browser_spec="$repo_root/frontend/e2e/analyze-materials-runtime.spec.js"

fail() {
  printf 'analyze-materials-runtime-v2 smoke contract failed: %s\n' "$1" >&2
  exit 1
}

[[ -x "$smoke_script" ]] || fail 'canonical smoke script is not executable'
for path in "$agent_helper" "$business_helper" "$business_repository" "$browser_spec"; do
  [[ -r "$path" ]] || fail "required canonical smoke source is missing: $path"
done
bash -n "$smoke_script" || fail 'canonical smoke script has invalid shell syntax'

require_in() {
  local path="$1"
  local literal="$2"
  local message="$3"
  grep -F "$literal" "$path" >/dev/null || fail "$message"
}

require_smoke() {
  require_in "$smoke_script" "$1" "$2"
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
require_smoke 'http://${etcd_host}:${etcd_port}/health' 'etcd direct health check is missing'
require_smoke 'http://${etcd_host}:${etcd_port}/v3/kv/range' 'etcd direct range check is missing'

for path in "$smoke_script" "$agent_helper" "$business_helper" "$business_repository" "$browser_spec"; do
  if rg -n '(/var/run/docker\.sock|docker[[:space:]]+(info|ps|compose|exec)|wait-for-local-infra\.sh|compose\.ya?ml|psql([[:space:]]|$)|redis-cli)' "$path" >/dev/null; then
    fail "canonical smoke source depends on a container control plane or external database CLI: $path"
  fi
done

require_smoke 'x-migrations-table=${reset_table}' 'isolated reset migration ledger is missing'
require_smoke 'DROP SCHEMA IF EXISTS %s CASCADE;' 'isolated schema reset is missing'
require_smoke 'DROP TABLE IF EXISTS public.schema_migrations;' 'canonical migration ledger reset is missing'
require_smoke 'reset_test_database business "$BUSINESS_DATABASE_URL"' 'Business destructive reset is not bound to the test database'
require_smoke 'reset_test_database agent "$AGENT_DATABASE_URL"' 'Agent destructive reset is not bound to the test database'
require_smoke 'refusing to use a PostgreSQL database other than $database on the canonical host port' 'database reset guard is missing'
postgres_probe_line="$(grep -n '^wait_tcp "\$postgres_host" "\$postgres_port" PostgreSQL$' "$smoke_script" | cut -d: -f1)"
business_reset_line="$(grep -n '^reset_test_database business "\$BUSINESS_DATABASE_URL"$' "$smoke_script" | cut -d: -f1)"
[[ -n "$postgres_probe_line" && -n "$business_reset_line" ]] || fail 'could not prove direct-host probe/reset order'
(( postgres_probe_line < business_reset_line )) || fail 'destructive reset runs before direct-host readiness succeeds'

require_smoke 'local-smoke-analyze-materials-authority' 'Agent PostgreSQL/Redis authority helper is missing'
require_smoke 'local-smoke-analyze-materials-fixture' 'Business fixture/authority helper is missing'
require_in "$agent_helper" 'sql.Open("pgx", dsn)' 'Agent helper does not use the Go PostgreSQL client'
require_in "$agent_helper" 'client.Ping(pingCtx)' 'Agent helper does not use the Go Redis client'
require_in "$business_repository" 'gorm.Open(postgres.Open(dsn)' 'Business helper does not use the Go PostgreSQL client'
require_in "$business_helper" 'fixtureModeSeed' 'Business fixture seed mode is missing'
require_in "$business_helper" 'fixtureModeAuthority' 'Business fixture authority mode is missing'

require_smoke 'export DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false' 'CreationSpec Preview runtime is not disabled'
require_smoke 'export DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false' 'User Message runtime is not disabled'
require_smoke 'export DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=true' 'Analyze Materials runtime is not enabled'
require_smoke 'export DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_PROFILE=analyze_materials.runtime.v2preview1' 'Analyze Materials profile is not pinned'
require_smoke 'export DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED=true' 'Business Analyze Materials proxy is not enabled'
require_smoke 'export DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED=true' 'Business Evidence preview is not enabled'
require_smoke 'AGENT_SSE_MAX_EVENT_BYTES must be 131072' 'SSE envelope headroom guard is missing'

require_smoke 'GOWORK=off "$go_bin" -C "$repo_root/business" build' 'Business runtime is not rebuilt from the worktree'
require_smoke 'GOWORK=off "$go_bin" -C "$repo_root/agent" build' 'Agent runtime is not rebuilt from the worktree'
require_smoke '"$repo_root/scripts/migrate.sh" business up' 'Business migrations are missing'
require_smoke '"$repo_root/scripts/migrate.sh" agent up' 'Agent migrations are missing'
require_smoke 'go_bin" run -tags localsmoke ./cmd/local-smoke-seeder' 'local smoke identity seeding is missing'
require_smoke '"$repo_root/.local/bin/business-service"' 'Business runtime is not started locally'
require_smoke '"$repo_root/.local/bin/agent-service"' 'Agent runtime is not started locally'
require_smoke "wait_etcd_prefix_count '/dora/services/dora-business-service/' 1 Business" 'Business exact etcd registration is not asserted'
require_smoke "wait_etcd_prefix_count '/dora/services/dora-agent-service/' 1 Agent" 'Agent exact etcd registration is not asserted'
require_smoke "wait_etcd_prefix_count '/dora/services/dora-business-service/' 0 Business" 'Business etcd cleanup is not asserted'
require_smoke "wait_etcd_prefix_count '/dora/services/dora-agent-service/' 0 Agent" 'Agent etcd cleanup is not asserted'

require_smoke '/api/v1/agent/sessions/${session_id}/analyze-materials-previews' 'public Analyze Materials route is missing'
require_smoke '[[ "$enqueue_status" == "202" ]]' 'first enqueue does not require HTTP 202'
require_smoke 'analyze_materials.preview.enqueue.v1' 'enqueue schema is not asserted'
require_smoke 'keys == ["input_id","replayed","request_id","run_id","schema_version","session_id","status","tool_call_id","turn_id"]' 'enqueue exact DTO set is not asserted'
require_smoke '[[ "$replay_status" == "202" ]]' 'same-key replay does not require HTTP 202'
require_smoke '.replayed == true' 'idempotent replay identity assertion is missing'
require_smoke '[[ "$conflict_status" == "409" ]]' 'same-key semantic conflict does not require HTTP 409'
require_smoke 'IDEMPOTENCY_CONFLICT' 'semantic conflict error code is not asserted'

for frozen_ref in \
  'analyze_materials.preview_tools@v1' \
  'analyze_materials.v2preview1' \
  'analyze_materials.preview.intent.v1' \
  'analyze_materials.preview.result.v1' \
  'local.fake.analyze_materials.router@v1' \
  'local.fake.analyze_materials.analysis@v1' \
  'analyze_materials.runtime_policy@v1' \
  'analyze_materials.local_preview_budget@v1'; do
  require_in "$agent_helper" "$frozen_ref" "Turn Context pin is missing $frozen_ref"
done
require_smoke '.counts.model_receipt_count == 2 and .counts.router_model_receipt_count == 1 and .counts.graph_model_receipt_count == 1' 'two-layer ModelReceipt exact set is missing'
require_smoke '.counts.tool_receipt_count == 1 and .counts.projection_count == 1' 'ToolReceipt/Projection uniqueness invariant is missing'
require_smoke '.counts.accepted_event_count == 1 and .counts.terminal_event_count == 1 and .counts.event_high_watermark == 3' 'exact Event set invariant is missing'
require_smoke '.counts.message_count == 0' 'Message absence invariant is missing'
require_smoke '.counts.creation_spec_preview_count == 0 and .counts.user_message_turn_count == 0' 'other runtime absence invariant is missing'
require_smoke '.counts.creation_spec_count == 0 and .counts.creation_spec_receipt_count == 0' 'Business write absence invariant is missing'
require_smoke '.counts.unsafe_event_payload_count == 0' 'Event payload redaction invariant is missing'

require_smoke 'session.workspace.v2' 'Workspace Snapshot schema is not asserted'
require_smoke '.analyze_materials_preview.schema_version == "analyze_materials.preview.card.v1"' 'Workspace safe Card is not asserted'
require_smoke 'event: analyze_materials.preview.accepted' 'accepted SSE event is not asserted'
require_smoke 'event: analyze_materials.preview.completed' 'terminal SSE event is not asserted'
require_smoke 'cmp -s "$work_dir/authority-before.json" "$work_dir/authority-after.json"' 'read/replay no-mutation barrier is missing'
require_smoke './node_modules/.bin/vite --host 127.0.0.1' 'Vite is not started on loopback'
require_smoke 'playwright test e2e/analyze-materials-runtime.spec.js' 'Chromium test is not executed'
require_in "$browser_spec" "browserName).toBe('chromium')" 'browser test does not require Chromium'
require_in "$browser_spec" 'data-tool-availability' 'browser test does not assert the static Catalog remains unavailable'
require_in "$browser_spec" 'apiMutations).toEqual(mutatingBeforeReload)' 'browser test is not read-only after login'

require_smoke 'source-before.sha256' 'source manifest before-run barrier is missing'
require_smoke 'source-after.sha256' 'source manifest after-run barrier is missing'
require_smoke 'cmp -s "$work_dir/source-before.sha256" "$work_dir/source-after.sha256"' 'source no-drift comparison is missing'
require_smoke 'analyze_materials_runtime_v2_smoke_evidence.v1' 'redacted Evidence schema is missing'
require_smoke 'source_digest:$source_digest' 'source digest is not published'
require_smoke 'chmod 600 "$evidence_file"' 'published Evidence mode is not restricted'
require_smoke 'published Evidence permissions are not 0600' 'published Evidence mode is not verified'
require_smoke 'rm -f "$evidence_file" "${evidence_file}.tmp"' 'failed Evidence cleanup is missing'
require_smoke 'local Runtime cleanup left port' 'managed runtime cleanup verification is missing'

printf 'analyze-materials-runtime-v2 smoke contract passed\n'
