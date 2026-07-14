#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/smoke-secret-transport.sh
. "$repo_root/scripts/lib/smoke-secret-transport.sh"

fail() {
  printf 'smoke secret transport contract failed: %s\n' "$1" >&2
  exit 1
}

file_mode() {
  stat -f '%Lp' "$1" 2>/dev/null || stat -c '%a' "$1"
}

test_root="$(mktemp -d "${TMPDIR:-/tmp}/dora-smoke-secret-test.XXXXXX")"
trap 'rm -rf "$test_root"' EXIT
mkdir -p "$test_root/bin" "$test_root/evidence"

curl_args_file="$test_root/curl.args"
curl_stdin_file="$test_root/curl.stdin"
curl_config_file="$test_root/curl.conf"
cookie_jar="$test_root/cookie.jar"
csrf_secret='csrf-contract-secret-!@#'
cookie_secret='cookie-contract-secret-!@#'
login_email='smoke+contract@example.com'
login_password='password-contract-secret-!@# "slash\\"'

printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'printf '\''%s\n'\'' "$@" >"$CURL_ARGS_FILE"' \
  'payload="$(</dev/stdin)"' \
  'printf '\''%s'\'' "$payload" >"$CURL_STDIN_FILE"' \
  'printf '\''204'\''' \
  >"$test_root/bin/curl"
chmod 700 "$test_root/bin/curl"
printf '# Netscape HTTP Cookie File\nlocalhost\tFALSE\t/\tFALSE\t0\tsession\t%s\n' "$cookie_secret" >"$cookie_jar"
chmod 600 "$cookie_jar"

write_curl_header_config "$curl_config_file" 'X-CSRF-Token' "$csrf_secret" || fail 'curl config write failed'
[[ "$(file_mode "$curl_config_file")" == "600" ]] || fail 'curl config mode is not 0600'
grep -F -- "X-CSRF-Token: $csrf_secret" "$curl_config_file" >/dev/null || fail 'curl config does not contain CSRF header'
if write_curl_header_config "$curl_config_file" 'X-CSRF-Token' $'unsafe\r\nheader'; then
  fail 'curl config accepted a CRLF header value'
fi
write_curl_header_config "$curl_config_file" 'X-CSRF-Token' "$csrf_secret" || fail 'curl config rewrite failed'

login_payload="$(build_login_json "$login_email" "$login_password")" || fail 'login JSON build failed'
jq -e --arg email "$login_email" --arg password "$login_password" \
  '.email == $email and .password == $password and (keys == ["email", "password"])' \
  <<<"$login_payload" >/dev/null || fail 'login JSON value drifted'

export CURL_ARGS_FILE="$curl_args_file" CURL_STDIN_FILE="$curl_stdin_file"
curl_status="$(PATH="$test_root/bin:$PATH" curl_with_body_stdin "$login_payload" \
  --config "$curl_config_file" -b "$cookie_jar" 'http://127.0.0.1/session')"
[[ "$curl_status" == "204" ]] || fail 'curl wrapper did not preserve status output'
cmp -s <(printf '%s' "$login_payload") "$curl_stdin_file" || fail 'curl wrapper did not preserve request body on stdin'
grep -Fx -- '--data-binary' "$curl_args_file" >/dev/null || fail 'curl wrapper omitted --data-binary'
grep -Fx -- '@-' "$curl_args_file" >/dev/null || fail 'curl wrapper did not select stdin'
grep -Fx -- "$curl_config_file" "$curl_args_file" >/dev/null || fail 'curl wrapper omitted config path'
for secret in "$csrf_secret" "$cookie_secret" "$login_password" "$login_payload"; do
  if grep -F -- "$secret" "$curl_args_file" >/dev/null; then
    fail 'curl argv contains secret material'
  fi
done

rg_args_file="$test_root/rg.args"
rg_pattern_copy="$test_root/rg.pattern"
rg_secret='regex-contract-secret-[0-9]+'
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'printf '\''%s\n'\'' "$@" >"$RG_ARGS_FILE"' \
  'pattern_source=""' \
  'while (($#)); do' \
  '  if [[ "$1" == "--file" ]]; then pattern_source="$2"; break; fi' \
  '  shift' \
  'done' \
  '[[ "$pattern_source" == "-" ]]' \
  'cp /dev/stdin "$RG_PATTERN_COPY"' \
  'exit "${RG_EXIT_CODE:-1}"' \
  >"$test_root/bin/rg"
chmod 700 "$test_root/bin/rg"
export RG_ARGS_FILE="$rg_args_file" RG_PATTERN_COPY="$rg_pattern_copy"
export RG_EXIT_CODE=1
if PATH="$test_root/bin:$PATH" rg_with_pattern_stdin regex "$rg_secret" "$test_root/evidence"; then
  fail 'rg wrapper changed the no-match exit status'
else
  rg_status="$?"
fi
[[ "$rg_status" == "1" ]] || fail 'rg wrapper returned an unexpected status'
if grep -F -- "$rg_secret" "$rg_args_file" >/dev/null; then
  fail 'rg argv contains secret pattern material'
fi
[[ "$(<"$rg_pattern_copy")" == "$rg_secret" ]] || fail 'rg stdin pattern content drifted'
grep -Fx -- '--file' "$rg_args_file" >/dev/null || fail 'rg wrapper omitted --file'
grep -Fx -- '-' "$rg_args_file" >/dev/null || fail 'rg wrapper did not read patterns from stdin'

trace_secret='xtrace-contract-secret-!@#'
trace_file="$test_root/xtrace.log"
TRACE_SECRET="$trace_secret" bash -x -c '
  . "$1"
  disable_shell_xtrace
  local_copy="$TRACE_SECRET"
  : "$local_copy"
' _ "$repo_root/scripts/lib/smoke-secret-transport.sh" 2>"$trace_file"
if grep -F -- "$trace_secret" "$trace_file" >/dev/null; then
  fail 'xtrace contains secret material after the guard'
fi

smoke_script="$repo_root/scripts/smoke-w0-transport.sh"
grep -F '. "$repo_root/scripts/lib/smoke-secret-transport.sh"' "$smoke_script" >/dev/null || fail 'smoke script does not load secret transport guard'
grep -F 'disable_shell_xtrace' "$smoke_script" >/dev/null || fail 'smoke script does not disable xtrace'
if rg 'X-CSRF-Token: \$' "$smoke_script" >/dev/null; then
  fail 'smoke script still passes a dynamic CSRF header in argv'
fi
if rg -- '--data-binary[[:space:]]+"\$|--data-binary[[:space:]]+"\$\(' "$smoke_script" >/dev/null; then
  fail 'smoke script still passes a dynamic body in argv'
fi
if rg 'rg .*"\$(value|pattern)"' "$smoke_script" >/dev/null; then
  fail 'smoke script still passes a secret rg pattern in argv'
fi
if rg -- '--arg[[:space:]]+password' "$repo_root/scripts/lib/smoke-secret-transport.sh" >/dev/null; then
  fail 'login JSON builder still passes password in jq argv'
fi

printf 'smoke secret transport contract passed\n'
