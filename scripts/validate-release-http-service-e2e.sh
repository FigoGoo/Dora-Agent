#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -z "${RELEASE_BUSINESS_BASE_URL:-}" ]]; then
  echo "RELEASE_BUSINESS_BASE_URL is required, e.g. https://business.test.example.com" >&2
  exit 1
fi

if [[ -z "${RELEASE_AGENT_BASE_URL:-}" ]]; then
  echo "RELEASE_AGENT_BASE_URL is required, e.g. https://agent.test.example.com" >&2
  exit 1
fi

export RELEASE_HTTP_E2E_REPORT_PATH="${RELEASE_HTTP_E2E_REPORT_PATH:-tests/reports/release-http-service-e2e-report.md}"

echo "== release HTTP service E2E =="
python3 tests/e2e/http/validate_release_http_service_e2e.py
echo "release HTTP service E2E passed"
echo "release HTTP service E2E report: $RELEASE_HTTP_E2E_REPORT_PATH"
