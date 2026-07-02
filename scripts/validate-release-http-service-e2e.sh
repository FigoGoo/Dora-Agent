#!/usr/bin/env bash
set -euo pipefail

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "missing required environment variable: ${name}" >&2
    exit 2
  fi
}

check_endpoint() {
  local label="$1"
  local url="$2"
  python3 - "$label" "$url" <<'PY'
import sys
import urllib.error
import urllib.request

label, url = sys.argv[1], sys.argv[2]
try:
    with urllib.request.urlopen(url, timeout=5) as response:
        body = response.read(1024).decode("utf-8", errors="replace")
        if response.status != 200:
            raise SystemExit(f"{label} expected HTTP 200, got {response.status}: {body}")
except urllib.error.URLError as exc:
    raise SystemExit(f"{label} request failed: {exc}") from exc
PY
}

require_env "RELEASE_BUSINESS_BASE_URL"
require_env "RELEASE_AGENT_BASE_URL"

report_path="${RELEASE_HTTP_E2E_REPORT_PATH:-tests/reports/release-http-service-e2e-report.md}"
mkdir -p "$(dirname "$report_path")"

check_endpoint "business healthz" "${RELEASE_BUSINESS_BASE_URL}/healthz"
check_endpoint "business readyz" "${RELEASE_BUSINESS_BASE_URL}/readyz"
check_endpoint "agent healthz" "${RELEASE_AGENT_BASE_URL}/healthz"
check_endpoint "agent readyz" "${RELEASE_AGENT_BASE_URL}/readyz"

cat > "$report_path" <<EOF
# Release HTTP Service E2E

status: passed

- business_base_url: ${RELEASE_BUSINESS_BASE_URL}
- agent_base_url: ${RELEASE_AGENT_BASE_URL}
- checks: healthz, readyz
EOF

echo "release HTTP service E2E ok: ${report_path}"
