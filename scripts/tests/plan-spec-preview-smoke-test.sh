#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
smoke_script="$repo_root/scripts/smoke-plan-spec-preview.sh"
browser_spec="$repo_root/frontend/e2e/plan-spec-preview.spec.js"
recovery_test="$repo_root/agent/internal/postgres/creation_spec_preview_recovery_smoke_test.go"
makefile="$repo_root/Makefile"

fail() {
  printf 'plan-spec-preview smoke contract failed: %s\n' "$1" >&2
  exit 1
}

[[ -x "$smoke_script" ]] || fail 'canonical smoke script is not executable'
[[ -r "$browser_spec" ]] || fail 'real browser spec is missing'
[[ -r "$recovery_test" ]] || fail 'PostgreSQL durable-command recovery probe is missing'
bash -n "$smoke_script" || fail 'canonical smoke script has invalid shell syntax'
node --check "$browser_spec" >/dev/null || fail 'browser spec has invalid JavaScript syntax'

target_recipe="$(awk '
  /^plan-spec-preview-smoke:/ {capture=1; print; next}
  capture && /^[^[:space:]#][^:]*:/ {exit}
  capture {print}
' "$makefile")"
[[ "$target_recipe" == *'plan-spec-preview-smoke: migration-tools check-frontend'* ]] || \
  fail 'Make target does not require migration tooling and frontend verification'
[[ "$target_recipe" == *'./scripts/smoke-plan-spec-preview.sh'* ]] || \
  fail 'Make target does not execute the canonical real smoke script'
[[ "$target_recipe" != *'go test'* && "$target_recipe" != *'vitest'* ]] || \
  fail 'Make target uses unit tests as the smoke result'

grep -F '. "$repo_root/scripts/lib/smoke-secret-transport.sh"' "$smoke_script" >/dev/null || \
  fail 'smoke script does not load the secret transport guard'
grep -F 'disable_shell_xtrace' "$smoke_script" >/dev/null || fail 'smoke script does not disable xtrace'
grep -F 'umask 077' "$smoke_script" >/dev/null || fail 'smoke script does not set a restrictive umask'
grep -F 'GOWORK=off "$go_bin" -C "$repo_root/business" build' "$smoke_script" >/dev/null || \
  fail 'Business Runtime is not rebuilt from the current worktree'
grep -F 'GOWORK=off "$go_bin" -C "$repo_root/agent" build' "$smoke_script" >/dev/null || \
  fail 'Agent Runtime is not rebuilt from the current worktree'
grep -F '"${compose[@]}" up -d' "$smoke_script" >/dev/null || fail 'real Docker infrastructure is not started'
grep -F '"$repo_root/scripts/migrate.sh" business up' "$smoke_script" >/dev/null || fail 'Business migration is missing'
grep -F '"$repo_root/scripts/migrate.sh" agent up' "$smoke_script" >/dev/null || fail 'Agent migration is missing'
grep -F '"$repo_root/.local/bin/business-service"' "$smoke_script" >/dev/null || fail 'Business Runtime is not started'
grep -F '"$repo_root/.local/bin/agent-service"' "$smoke_script" >/dev/null || fail 'Agent Runtime is not started'
grep -F 'restart_agent_strict' "$smoke_script" >/dev/null || fail 'real Agent restart checkpoint is missing'
grep -F 'agent_restart_observed' "$smoke_script" >/dev/null || fail 'Agent restart is missing from Evidence'
grep -F 'DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=true' "$smoke_script" >/dev/null || fail 'server Preview flag is not explicit'
grep -F 'VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED=true' "$smoke_script" >/dev/null || fail 'browser Preview flag is not explicit'
grep -F './node_modules/.bin/playwright test' "$smoke_script" >/dev/null || fail 'real Playwright is not executed'
grep -F -- "--grep '@plan-spec-preview'" "$smoke_script" >/dev/null || fail 'canonical browser scenario is not selected'

for required_table in \
  'business.creation_spec' \
  'business.creation_spec_command_receipt' \
  'agent.creation_spec_preview_run' \
  'agent.creation_spec_preview_model_receipt' \
  'agent.creation_spec_preview_tool_receipt' \
  'agent.creation_spec_preview_projection' \
  'agent.session_event_log'; do
  grep -F "$required_table" "$smoke_script" >/dev/null || fail "authority query is missing $required_table"
done
grep -F 'positive-before-replay.json' "$smoke_script" >/dev/null || fail 'positive replay authority barrier is missing'
grep -F 'blocked-before-post.json' "$smoke_script" >/dev/null || fail 'blocked-lane authority barrier is missing'
grep -F 'cmp -s "$work_dir/positive-before.json" "$work_dir/positive-after.json"' "$smoke_script" >/dev/null || \
  fail 'idempotent replay zero-delta comparison is missing'
grep -F 'cmp -s "$work_dir/blocked-before.json" "$work_dir/blocked-after.json"' "$smoke_script" >/dev/null || \
  fail 'blocked-lane zero-delta comparison is missing'
blocked_collector="$(awk '
  /^collect_blocked_authority\(\)/ {capture=1}
  capture && /^collect_forbidden_side_effects\(\)/ {exit}
  capture {print}
' "$smoke_script")"
[[ "$(grep -Fc "SELECT lease_owner IS NULL AND lease_until IS NULL FROM agent.session_runtime_lease" <<<"$blocked_collector")" == "1" ]] || \
  fail 'blocked-lane authority SQL contains a missing or duplicate Session lease projection'
grep -F 'plan_spec_preview.trial_evidence.v1' "$smoke_script" >/dev/null || fail 'Trial Evidence schema is missing'
trial_resource_keys="$(awk '
  /^    resources:\{/ {capture=1; next}
  capture && /^    \},$/ {exit}
  capture && /^[[:space:]]+[a-z_]+:/ {
    key=$1
    sub(/:.*/, "", key)
    print key
  }
' "$smoke_script")"
[[ -n "$trial_resource_keys" ]] || fail 'Trial Evidence resources object is missing'
[[ -z "$(sort <<<"$trial_resource_keys" | uniq -d)" ]] || fail 'Trial Evidence resources contain duplicate keys'
grep -F '.status = "passed" | .assertions.evidence_redacted = true' "$smoke_script" >/dev/null || \
  fail 'passed publication is not gated by the redaction transition'
grep -F 'file_mode "$evidence_file"' "$smoke_script" >/dev/null || fail '0600 Evidence mode is not verified'

grep -F -x 'stop_agent_for_recovery_probe_strict' "$smoke_script" >/dev/null || \
  fail 'durable-command recovery probe is not isolated behind an Agent stop barrier'
grep -F -- "-run '^TestCreationSpecPreviewDurableRecoveryPostgreSQLSmoke$' -count=1" "$smoke_script" >/dev/null || \
  fail 'canonical smoke does not execute the PostgreSQL durable-command recovery probe'
grep -F -x 'start_agent_after_recovery_probe_strict' "$smoke_script" >/dev/null || \
  fail 'Agent is not restarted after the durable-command recovery probe'
browser_run_line="$(grep -n -F './node_modules/.bin/playwright test' "$smoke_script" | tail -1 | cut -d: -f1)"
recovery_stop_line="$(grep -n -F -x 'stop_agent_for_recovery_probe_strict' "$smoke_script" | cut -d: -f1)"
recovery_test_line="$(grep -n -F -- "-run '^TestCreationSpecPreviewDurableRecoveryPostgreSQLSmoke$' -count=1" "$smoke_script" | cut -d: -f1)"
recovery_start_line="$(grep -n -F -x 'start_agent_after_recovery_probe_strict' "$smoke_script" | cut -d: -f1)"
((browser_run_line < recovery_stop_line && recovery_stop_line < recovery_test_line && recovery_test_line < recovery_start_line)) || \
  fail 'durable-command recovery probe is not ordered after the browser chain inside an Agent stop window'
grep -F 'plan_spec_preview.durable_recovery_postgresql.v1' "$smoke_script" >/dev/null || \
  fail 'durable-command recovery result schema is not validated'
grep -F 'file_mode "$recovery_result"' "$smoke_script" >/dev/null || \
  fail '0600 durable-command recovery result mode is not verified'
grep -F -- '--slurpfile recovery "$recovery_result"' "$smoke_script" >/dev/null || \
  fail 'durable-command recovery result is not slurped into Trial Evidence'
for required_recovery_count in \
  'claimed_after_exhaustion == 0' \
  'follower_pending == 1' \
  'query_calls == 7' \
  'resend_attempts == 3' \
  'resend_limit == 3' \
  'save_calls == 3'; do
  grep -F "$required_recovery_count" "$smoke_script" >/dev/null || \
    fail "durable-command recovery result does not pin $required_recovery_count"
done

grep -F 'plancreationspec.Compile(' "$recovery_test" >/dev/null || \
  fail 'recovery probe does not compile the formal plan_creation_spec Graph'
grep -F 'graph.Recover(' "$recovery_test" >/dev/null || \
  fail 'recovery probe does not execute formal Graph recovery'
grep -F 'previewRecoverySmokeBusinessAdapter' "$recovery_test" >/dev/null || \
  fail 'recovery probe scripted Business adapter is missing'
grep -F 'reflect.DeepEqual(command, adapter.expected)' "$recovery_test" >/dev/null || \
  fail 'recovery probe does not enforce an exact stable Business command'
grep -F 'queryOutcomes: []string{' "$recovery_test" >/dev/null || \
  fail 'recovery probe does not freeze scripted Query outcomes'
[[ "$(grep -Fc 'contentcrypto.NewAES256GCMProtector(key,' "$recovery_test")" == "2" ]] || \
  fail 'recovery probe does not rebuild an AES protector after restart'
grep -F 'repository.ReplayRecovery(ctx, trustedV1)' "$recovery_test" >/dev/null || \
  fail 'recovery probe does not reject the stale Owner/Fence replay'
grep -F 'repository.ReplayRecovery(ctx, trustedV2)' "$recovery_test" >/dev/null || \
  fail 'recovery probe does not rebuild recovery state with the restarted Owner/Fence'
if grep -F 'repository.ReserveCommandResend' "$recovery_test" >/dev/null; then
  fail 'recovery probe bypasses formal Graph recovery with a direct resend reservation'
fi
grep -F 'os.Chmod(target, 0o600)' "$recovery_test" >/dev/null || \
  fail 'recovery probe does not publish strict 0600 result evidence'

if rg -n 'page\.route|context\.route|route\.fulfill|route\.abort|mock|intercept' "$browser_spec" >/dev/null; then
  fail 'browser scenario contains request interception or mocks'
fi
grep -F 'postDataJSON()).toEqual({ initial_prompt: null })' "$browser_spec" >/dev/null || \
  fail 'QuickCreate initial_prompt=null is not asserted'
grep -F 'expect(previewResponse.status()).toBe(202)' "$browser_spec" >/dev/null || fail 'BFF 202 is not asserted'
grep -F "creation_spec.preview.completed" "$browser_spec" >/dev/null || fail 'SSE completion is not observed'
grep -F 'setOffline(true)' "$browser_spec" >/dev/null || fail 'controlled SSE disconnect is missing'
grep -F 'await page.reload()' "$browser_spec" >/dev/null || fail 'hard-refresh recovery is missing'
grep -F "code: 'SESSION_LANE_BLOCKED'" "$browser_spec" >/dev/null || fail 'legacy lane 409 code is not asserted'
grep -F 'positive-replay-ack.json' "$browser_spec" >/dev/null || fail 'browser does not wait for replay authority capture'
grep -F 'blocked-post-ack.json' "$browser_spec" >/dev/null || fail 'browser does not wait for blocked-lane authority capture'

if rg -n '(^|[[:space:]])@true([[:space:]]|$)|echo.*passed' "$smoke_script" >/dev/null; then
  fail 'smoke script contains an unconditional passed placeholder'
fi

printf 'plan-spec-preview smoke contract passed\n'
