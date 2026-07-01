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

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-doraigc-postgres}"
POSTGRES_USER="${POSTGRES_USER:-doraigc}"
BUSINESS_DB_NAME="${BUSINESS_DB_NAME:-doraigc}"

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

docker exec -i "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$BUSINESS_DB_NAME" -Atc "
SELECT jsonb_build_object(
  'skill_id', id,
  'skill_name', skill_name,
  'skill_scope', skill_scope,
  'version', 'published',
  'status', status,
  'route_hints', route_hints_json
)::text
FROM skills
WHERE deleted_at IS NULL AND status = 'published' AND published_version_id IS NOT NULL
ORDER BY updated_at DESC, id ASC;
" > "$tmp_dir/skills.jsonl"

go run ./services/agent/cmd/skill-routing-eval "$tmp_dir/skills.jsonl"
