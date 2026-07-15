#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/w1-smoke-mode.sh
. "$repo_root/scripts/lib/w1-smoke-mode.sh"

expect_valid() {
  local skill_enabled="$1"
  local browser_enabled="$2"
  local output=""

  if ! output="$(validate_w1_smoke_mode "$skill_enabled" "$browser_enabled")"; then
    printf 'expected valid W1 smoke mode skill=%s browser=%s, got: %s\n' \
      "$skill_enabled" "$browser_enabled" "$output" >&2
    exit 1
  fi
}

expect_invalid() {
  local skill_enabled="$1"
  local browser_enabled="$2"
  local expected_message="$3"
  local output=""

  if output="$(validate_w1_smoke_mode "$skill_enabled" "$browser_enabled")"; then
    printf 'expected invalid W1 smoke mode skill=%s browser=%s\n' \
      "$skill_enabled" "$browser_enabled" >&2
    exit 1
  fi
  [[ "$output" == "$expected_message" ]] || {
    printf 'unexpected validation message: %s\n' "$output" >&2
    exit 1
  }
}

expect_valid 0 0
expect_valid 1 1
expect_invalid 1 0 "W1-C2 canonical Evidence 必须执行 @w1-real-review 真实浏览器门禁"
expect_invalid 0 1 "W1 浏览器门禁必须同时启用 W1 API/数据库门禁"
expect_invalid invalid 1 "W1_RUN_SKILL_SMOKE 只允许 0 或 1"
expect_invalid 1 invalid "W1_RUN_BROWSER_SMOKE 只允许 0 或 1"

make_plan="$(make -n -B -f "$repo_root/Makefile" w1-smoke)"
smoke_invocation="$(printf '%s\n' "$make_plan" | rg 'scripts/smoke-w0-transport\.sh')"
[[ "$(printf '%s\n' "$smoke_invocation" | wc -l | tr -d ' ')" == "1" ]] || {
  printf 'make w1-smoke must execute exactly one canonical smoke invocation\n' >&2
  exit 1
}
[[ "$smoke_invocation" == *"W1_RUN_SKILL_SMOKE=1"* ]] || {
  printf 'make w1-smoke is missing W1_RUN_SKILL_SMOKE=1\n' >&2
  exit 1
}
[[ "$smoke_invocation" == *"W1_RUN_BROWSER_SMOKE=1"* ]] || {
  printf 'make w1-smoke is missing W1_RUN_BROWSER_SMOKE=1\n' >&2
  exit 1
}

smoke_script="$repo_root/scripts/smoke-w0-transport.sh"
browser_spec="$repo_root/frontend/e2e/w1-skill-foundation.spec.js"
[[ "$(rg -c '^run_w1_skill_governance_smoke\(\)' "$smoke_script")" == "1" ]] || {
  printf 'canonical W1 smoke must define exactly one Skill Governance flow\n' >&2
  exit 1
}
[[ "$(rg -c '^[[:space:]]*run_w1_skill_governance_smoke "\$postgres_container"$' "$smoke_script")" == "1" ]] || {
  printf 'canonical W1 smoke must invoke the Skill Governance flow exactly once\n' >&2
  exit 1
}
for fragment in \
  'w1.skill-governance.smoke.evidence.v2' \
  'w1.skill-governance.browser-fact.v1' \
  'skill_governor_rbac' \
  'skill_governor_revocation' \
  'skill_governance_idempotency' \
  'skill_governance_quickcreate_gate' \
  'skill_governance_offline_terminal' \
  'skill_governance_browser_list_detail' \
  'skill_governance_browser_decisions' \
  'skill_governance_browser_isolation' \
  'skill_governance_browser_database' \
  'DORA_SMOKE_GOVERNOR_EMAIL' \
  'DORA_SMOKE_GOVERNOR_PASSWORD' \
  'DORA_E2E_GOVERNOR_EMAIL="$governor_email"' \
  'DORA_E2E_GOVERNOR_PASSWORD="$governor_password"' \
  'DORA_E2E_W1_GOVERNANCE_CONTROL_DIR="$w1_governance_control_dir"' \
  'DORA_E2E_BASE_URL="http://127.0.0.1:${w1_browser_frontend_port}"' \
  'run_w1_governor_revocation_controller "$w1_browser_playwright_pid"' \
  'w1.governor-revocation.checkpoint.v1' \
  'w1.governor-revocation.database-ack.v1' \
  '-role skill_governor'; do
  grep -F -- "$fragment" "$smoke_script" >/dev/null || {
    printf 'canonical W1 governance smoke is missing %s\n' "$fragment" >&2
    exit 1
  }
done
grep -F 'reuseExistingServer: process.env.DORA_E2E_REUSE_EXISTING_SERVER === '\''1'\''' \
  "$repo_root/frontend/playwright.config.js" >/dev/null || {
  printf 'canonical Playwright config may silently reuse a stale frontend server\n' >&2
  exit 1
}

for fragment in \
  'w1.skill-market.smoke.evidence.v2' \
  'skill_market_stale_selection_fail_closed' \
  'w1.skill-market-binding.smoke.evidence.v1' \
  'public_market_quickcreate' \
  'public_market_permission_identity_separation' \
  'public_market_publisher_snapshot_frozen' \
  'public_market_governance_toctou_closed' \
  'public_market_mixed_binding_atomicity' \
  'public_market_login_preselection_recovered' \
  'public_market_idempotency_frozen_replay' \
  'w1.real-review-result.v6' \
  'w1.public-market-preselection.checkpoint.v1' \
  'w1.public-market-preselection.database-ack.v1' \
  'w1.public-market-preselection.database-fact.v1' \
  'run_w1_public_market_preselection_controller "$w1_browser_playwright_pid"' \
  'DORA_E2E_W1_PUBLIC_MARKET_CONTROL_DIR="$w1_public_market_control_dir"' \
  'max_attempts="3600"' \
  'kill -0 "$playwright_pid"' \
  '$before == $after' \
  'w1.public-market-mixed-binding-success.database-fact.v1' \
  'w1.public-market-mixed-binding-failure.database-fact.v1' \
  'owner_private_v1:true,public_market_v2:true' \
  'owner_private_active:true,public_market_suspended:true' \
  'LOCK TABLE business.skill_governance_audit IN ACCESS EXCLUSIVE MODE' \
  'pg_blocking_pids(blocked.pid)' \
  'w1.public-market-governance-toctou.database-fact.v1' \
  'quickcreate_waited_on_governance:true' \
  "assert_evidence_excludes_regex '(public-market-(binding|stale)|public-market-mixed-(success|suspended)|mixed-owner-private-(create|review|approve))-[0-9]'" \
  'W1 四份 Evidence 的 run_id/source digest 不一致' \
  'rm -f "$skill_market_binding_evidence_file" "${skill_market_binding_evidence_file}.tmp"'; do
  grep -F -- "$fragment" "$smoke_script" >/dev/null || {
    printf 'canonical W1 Public Market Binding smoke is missing %s\n' "$fragment" >&2
    exit 1
  }
done
[[ "$(rg -c '^[[:space:]]*w1_public_market_governance_toctou_closed=true$' "$smoke_script")" == "1" ]] || {
  printf 'W1 governance TOCTOU assertion must be derived exactly once from the lock-competition path\n' >&2
  exit 1
}
[[ "$(rg -c '^[[:space:]]*w1_public_market_mixed_binding_atomicity=true$' "$smoke_script")" == "1" ]] || {
  printf 'W1 mixed binding assertion must be derived exactly once after real success/failure facts\n' >&2
  exit 1
}
if grep -F 'missing_skill_id=' "$smoke_script" >/dev/null; then
  printf 'W1 mixed binding smoke must not substitute a missing ID for the owner-private Skill\n' >&2
  exit 1
fi
for fragment in \
  'keys == ["agent_binary_sha256","assertions","business_binary_sha256","produced_at","project_skill_binding","run_id","schema_version","skill_foundation","source_digest_sha256","status","transport_prerequisite"]' \
  'and (.assertions | keys) == ["agent_v2_snapshot_encrypted","browser_agent_snapshot_matches_published"' \
  'keys == ["agent_binary_sha256","assertions","browser","business_binary_sha256","facts","offline_review_id","produced_at","resumed_project_id","run_id","schema_version","skill_id","source_digest_sha256","status"]' \
  'and all(.assertions[]; ((. | type) == "boolean" and . == true))'; do
  grep -F -- "$fragment" "$smoke_script" >/dev/null || {
    printf 'canonical W1 Foundation/Governance Evidence exact-set gate is missing %s\n' "$fragment" >&2
    exit 1
  }
done
for fragment in \
  'DORA_E2E_W1_PUBLIC_MARKET_CONTROL_DIR' \
  'w1.public-market-preselection.checkpoint.v1' \
  'w1.public-market-preselection.database-ack.v1' \
  "phase: 'before_login'" \
  "phase: 'before_submit'" \
  'quickCreateCount: publicMarketBeforeLoginQuickCreateCount' \
  'quickCreateCount: publicMarketPreSubmitQuickCreateCount'; do
  grep -F -- "$fragment" "$browser_spec" >/dev/null || {
    printf 'W1 browser Public Market two-phase checkpoint is missing %s\n' "$fragment" >&2
    exit 1
  }
done
for fragment in \
  'DORA_E2E_W1_GOVERNANCE_CONTROL_DIR' \
  'w1.governor-revocation.checkpoint.v1' \
  'w1.governor-revocation.database-ack.v1' \
  'checkpointGovernorRevocation({ governorID, skillID })' \
  "schema_version: 'w1.real-review-result.v6'" \
  'creator_api_forbidden: creatorGovernanceAPIForbidden' \
  'reviewer_api_forbidden: reviewerGovernanceAPIForbidden' \
  'same_cookie_revocation: governorRevocationSameCookie' \
  'no_legacy_api: governorNoLegacyAPI'; do
  grep -F -- "$fragment" "$browser_spec" >/dev/null || {
    printf 'W1 browser Governance contract is missing %s\n' "$fragment" >&2
    exit 1
  }
done
[[ "$(rg -c "phase: 'before_login'" "$browser_spec")" == "1" ]] || {
  printf 'W1 browser must emit exactly one before_login checkpoint\n' >&2
  exit 1
}
[[ "$(rg -c "phase: 'before_submit'" "$browser_spec")" == "1" ]] || {
  printf 'W1 browser must emit exactly one before_submit checkpoint\n' >&2
  exit 1
}
if grep -F 'skill_market_cross_owner_use_blocked' "$smoke_script" >/dev/null; then
  printf 'canonical W1 smoke still contains superseded active cross-owner rejection evidence\n' >&2
  exit 1
fi
if grep -F 'w1.skill-market.smoke.evidence.v1' "$smoke_script" >/dev/null; then
  printf 'canonical W1 smoke still emits superseded Skill Market Evidence v1\n' >&2
  exit 1
fi
grep -F 'run -tags localsmoke ./cmd/local-smoke-seeder' "$smoke_script" >/dev/null || {
  printf 'local smoke user seeder is not build-tag isolated\n' >&2
  exit 1
}
grep -F 'test -tags localsmoke ./cmd/local-smoke-seeder ./cmd/local-smoke-reviewer-seeder' "$repo_root/Makefile" >/dev/null || {
  printf 'tagged local smoke seeders are missing from the test gate\n' >&2
  exit 1
}
for fragment in \
  'rm -f "$evidence_file" "${evidence_file}.tmp"' \
  'rm -f "$governance_evidence_file" "${governance_evidence_file}.tmp"' \
  "'binding_audits', (SELECT COUNT(*) FROM business.project_skill_binding_audit)" \
  'assert_governance_decision_response "$response_file" "$headers_file" "suspended"' \
  'assert_governance_decision_response "$response_file" "$headers_file" "active"' \
  'assert_governance_decision_response "$response_file" "$headers_file" "offline"' \
  'assert_governance_error_response "$response_file" "$headers_file" "SKILL_GOVERNANCE_CONFLICT"' \
  'offline_resume_state_unchanged="true"' \
  'existing_session_snapshot_unchanged="true"' \
  "'strict_governance_linkage', NOT EXISTS" \
  '治理事实落库后 Business Readiness'; do
  grep -F -- "$fragment" "$smoke_script" >/dev/null || {
    printf 'canonical W1 governance safety gate is missing %s\n' "$fragment" >&2
    exit 1
  }
done

printf '%s\n' "W1 smoke mode contract passed"
