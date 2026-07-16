#!/usr/bin/env bash

# 冒烟脚本会处理密码、Cookie、CSRF 与密钥材料；调用方不应恢复 xtrace。
disable_shell_xtrace() {
  case "$-" in
    *x*) set +x ;;
  esac
}

write_curl_header_config() {
  local config_file="$1"
  local header_name="$2"
  local header_value="$3"
  local escaped_value="$header_value"

  [[ "$header_name" =~ ^[A-Za-z0-9-]+$ ]] || return 2
  [[ -n "$header_value" ]] || return 2
  [[ "$header_value" != *$'\r'* && "$header_value" != *$'\n'* ]] || return 2

  escaped_value="${escaped_value//\\/\\\\}"
  escaped_value="${escaped_value//\"/\\\"}"
  (umask 077; : >"$config_file") || return 1
  chmod 600 "$config_file" || return 1
  printf 'header = "%s: %s"\n' "$header_name" "$escaped_value" >"$config_file"
}

curl_with_body_stdin() {
  local body="$1"
  shift
  printf '%s' "$body" | curl --data-binary @- "$@"
}

build_login_json() {
  local email="$1"
  local password="$2"

  printf '%s\0%s\0' "$email" "$password" | jq -Rsc '
    split("\u0000") as $parts
    | if (($parts | length) == 3 and $parts[2] == "") then
        {email: $parts[0], password: $parts[1]}
      else
        error("invalid login tuple")
      end
  '
}

rg_with_pattern_stdin() {
  local mode="$1"
  local pattern="$2"
  local search_root="$3"
  local status=0

  [[ "$mode" == "literal" || "$mode" == "regex" ]] || return 2
  [[ -n "$pattern" ]] || return 2
  [[ "$pattern" != *$'\r'* && "$pattern" != *$'\n'* ]] || return 2

  if [[ "$mode" == "literal" ]]; then
    if printf '%s\n' "$pattern" | rg -F --quiet --file - -- "$search_root"; then
      status=0
    else
      status="$?"
    fi
  elif printf '%s\n' "$pattern" | rg --quiet --file - -- "$search_root"; then
    status=0
  else
    status="$?"
  fi
  return "$status"
}
