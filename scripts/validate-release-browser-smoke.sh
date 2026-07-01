#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ ! -d frontend/node_modules ]]; then
  echo "frontend/node_modules missing; run npm ci --prefix frontend" >&2
  exit 1
fi

if [[ ! -d admin_frontend/node_modules ]]; then
  echo "admin_frontend/node_modules missing; run pnpm --dir admin_frontend install --frozen-lockfile" >&2
  exit 1
fi

if [[ ! -d tests/e2e/browser/node_modules ]]; then
  echo "tests/e2e/browser/node_modules missing; run npm ci --prefix tests/e2e/browser" >&2
  exit 1
fi

if [[ -n "${CHROME_EXECUTABLE:-}" ]]; then
  if [[ ! -x "$CHROME_EXECUTABLE" ]]; then
    echo "CHROME_EXECUTABLE is not executable: $CHROME_EXECUTABLE" >&2
    exit 1
  fi
else
  for candidate in \
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
    "/usr/bin/google-chrome" \
    "/usr/bin/google-chrome-stable" \
    "/usr/bin/chromium" \
    "/usr/bin/chromium-browser"
  do
    if [[ -x "$candidate" ]]; then
      export CHROME_EXECUTABLE="$candidate"
      break
    fi
  done
fi

if [[ -z "${CHROME_EXECUTABLE:-}" ]]; then
  for binary in google-chrome google-chrome-stable chromium chromium-browser; do
    if command -v "$binary" >/dev/null 2>&1; then
      CHROME_EXECUTABLE="$(command -v "$binary")"
      export CHROME_EXECUTABLE
      break
    fi
  done
fi

if [[ -z "${CHROME_EXECUTABLE:-}" ]]; then
  echo "Chrome executable not found; set CHROME_EXECUTABLE before running release browser smoke" >&2
  exit 1
fi

echo "== release browser smoke =="
echo "Chrome executable: $CHROME_EXECUTABLE"
npm --prefix tests/e2e/browser run smoke
