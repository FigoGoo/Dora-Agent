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

printf '%s\n' "W1 smoke mode contract passed"
