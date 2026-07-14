#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${ENV_FILE:-$repo_root/.env.local}"
compose=(docker compose --env-file "$env_file" -f "$repo_root/deploy/local/compose.yaml")

for _ in $(seq 1 60); do
  unhealthy=0
  for service in postgres redis etcd; do
    container_id="$("${compose[@]}" ps -q "$service")"
    if [[ -z "$container_id" ]] || [[ "$(docker inspect --format '{{.State.Health.Status}}' "$container_id")" != "healthy" ]]; then
      unhealthy=1
    fi
  done
  if [[ "$unhealthy" -eq 0 ]]; then
    exit 0
  fi
  sleep 1
done

"${compose[@]}" ps >&2
echo "本地基础设施未在 60 秒内就绪" >&2
exit 1
