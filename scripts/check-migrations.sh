#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
failed=0

for module in business agent worker; do
  migration_dir="$repo_root/$module/migrations"
  while IFS= read -r up_file; do
    base="${up_file%.up.sql}"
    if [[ ! -f "${base}.down.sql" ]]; then
      echo "缺少 Down Migration: ${base}.down.sql" >&2
      failed=1
    fi
  done < <(find "$migration_dir" -maxdepth 1 -type f -name '*.up.sql' | sort)

  while IFS= read -r down_file; do
    base="${down_file%.down.sql}"
    if [[ ! -f "${base}.up.sql" ]]; then
      echo "缺少 Up Migration: ${base}.up.sql" >&2
      failed=1
    fi
  done < <(find "$migration_dir" -maxdepth 1 -type f -name '*.down.sql' | sort)

  if rg --ignore-case --glob '*.sql' \
    '(foreign[[:space:]]+key|references[[:space:]]+|on[[:space:]]+delete[[:space:]]+cascade|on[[:space:]]+update[[:space:]]+cascade)' \
    "$migration_dir" >/dev/null; then
		echo "$module Migration 禁止创建物理外键" >&2
		failed=1
	fi
done

exit "$failed"
