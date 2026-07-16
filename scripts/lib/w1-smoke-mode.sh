#!/usr/bin/env bash

# validate_w1_smoke_mode 仅验证执行模式，不读取环境、不产生 Evidence。
validate_w1_smoke_mode() {
  local skill_enabled="${1:-}"
  local browser_enabled="${2:-}"

  if [[ "$skill_enabled" != "0" && "$skill_enabled" != "1" ]]; then
    printf '%s\n' "W1_RUN_SKILL_SMOKE 只允许 0 或 1"
    return 1
  fi
  if [[ "$browser_enabled" != "0" && "$browser_enabled" != "1" ]]; then
    printf '%s\n' "W1_RUN_BROWSER_SMOKE 只允许 0 或 1"
    return 1
  fi
  if [[ "$browser_enabled" == "1" && "$skill_enabled" != "1" ]]; then
    printf '%s\n' "W1 浏览器门禁必须同时启用 W1 API/数据库门禁"
    return 1
  fi
  if [[ "$skill_enabled" == "1" && "$browser_enabled" != "1" ]]; then
    printf '%s\n' "W1-C2 canonical Evidence 必须执行 @w1-real-review 真实浏览器门禁"
    return 1
  fi
}
