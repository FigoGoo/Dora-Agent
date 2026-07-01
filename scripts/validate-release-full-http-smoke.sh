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

echo "== release full HTTP service smoke =="
go test ./services/agent/internal/e2e/release -run TestReleaseFullHTTPServiceE2ESmoke -count=1 -v
echo "release full HTTP service smoke passed"
