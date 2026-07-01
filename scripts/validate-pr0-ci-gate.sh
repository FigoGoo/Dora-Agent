#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -z "${GOROOT:-}" && -x "/Users/figo/sdk/go1.26.3/bin/go" ]]; then
  export GOROOT="/Users/figo/sdk/go1.26.3"
fi
if [[ -n "${GOROOT:-}" ]]; then
  export PATH="$GOROOT/bin:${GOPATH:-$HOME/go}/bin:$PATH"
fi

echo "== active contract gate =="
scripts/validate-active-contracts.sh

echo "== Go toolchain =="
go version

echo "== gofmt dry check =="
unformatted="$(find services internal -name '*.go' -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  echo "gofmt required" >&2
  exit 1
fi

echo "== Go tests =="
go test ./services/... ./internal/...

echo "== frontend tests/build =="
if [[ ! -d frontend/node_modules ]]; then
  echo "frontend/node_modules missing; run npm ci --prefix frontend" >&2
  exit 1
fi
npm --prefix frontend test
npm --prefix frontend run build

echo "== admin frontend tests/build =="
if [[ ! -d admin_frontend/node_modules ]]; then
  echo "admin_frontend/node_modules missing; run pnpm --dir admin_frontend install --frozen-lockfile" >&2
  exit 1
fi
if ! command -v pnpm >/dev/null 2>&1; then
  echo "pnpm is required for admin_frontend" >&2
  exit 1
fi
pnpm --dir admin_frontend test
pnpm --dir admin_frontend build

echo "PR-0 CI gate passed"
