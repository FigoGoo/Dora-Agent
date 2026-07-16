#!/usr/bin/env bash

W1_EVIDENCE_ENCRYPTED_FIELD_REGEX='(?i)"(payload_key_version|payload_nonce|payload_ciphertext|runtime_content_key_version|runtime_content_nonce|runtime_content_ciphertext)"[[:space:]]*:'
W1_EVIDENCE_IDEMPOTENCY_KEY_REGEX='(skill-create|skill-review|skill-review-decision|quick-create)-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}'

# publish_w1_evidence_release builds one immutable five-file W1 release and makes it
# visible with a single atomic current.json rename. Files outside the manifest are
# historical artifacts only; consumers must start from current.json.
publish_w1_evidence_release() {
  local release_root="$1"
  local run_id="$2"
  local source_digest_sha256="$3"
  local foundation_pending="$4"
  local governance_pending="$5"
  local market_pending="$6"
  local binding_pending="$7"
  local republish_pending="$8"
  local release_staging="$release_root/.${run_id}.staging"
  local release_dir="$release_root/$run_id"
  local current_staging="$release_root/.current-${run_id}.tmp"
  local current_manifest="$release_root/current.json"
  local produced_at=""
  local injection="${W1_EVIDENCE_PUBLISH_FAIL_AFTER:-}"
  local foundation_file="w1-skill-foundation-evidence.json"
  local governance_file="w1-skill-governance-evidence.json"
  local market_file="w1-skill-market-evidence.json"
  local binding_file="w1-skill-market-binding-evidence.json"
  local republish_file="w1-skill-republish-session-isolation-evidence.json"
  local foundation_sha256=""
  local governance_sha256=""
  local market_sha256=""
  local binding_sha256=""
  local republish_sha256=""

  if [[ ! "$run_id" =~ ^[0-9]{8}T[0-9]{6}Z-[0-9]+$ || ! "$source_digest_sha256" =~ ^[0-9a-f]{64}$ ]]; then
    printf 'W1 Evidence release 参数不合法\n' >&2
    return 1
  fi
  if [[ -e "$release_dir" || -e "$release_staging" || -e "$current_staging" ]]; then
    printf 'W1 Evidence release 路径已存在: %s\n' "$run_id" >&2
    return 1
  fi
  mkdir -p "$release_root" || return 1
  chmod 700 "$release_root" || return 1
  mkdir -m 700 "$release_staging" || return 1

  if ! jq -ne --arg run_id "$run_id" --arg source "$source_digest_sha256" \
    --slurpfile foundation "$foundation_pending" --slurpfile governance "$governance_pending" \
    --slurpfile market "$market_pending" --slurpfile binding "$binding_pending" \
    --slurpfile republish "$republish_pending" '
    [$foundation[0],$governance[0],$market[0],$binding[0],$republish[0]] as $all
    | all($all[]; .status == "pending" and .run_id == $run_id and .source_digest_sha256 == $source)
      and $foundation[0].schema_version == "w1.skill-foundation.smoke.evidence.v3"
      and $governance[0].schema_version == "w1.skill-governance.smoke.evidence.v1"
      and $market[0].schema_version == "w1.skill-market.smoke.evidence.v2"
      and $binding[0].schema_version == "w1.skill-market-binding.smoke.evidence.v1"
      and $republish[0].schema_version == "w1.skill-republish-session-isolation.smoke.evidence.v1"' \
    >/dev/null; then
    rm -rf "$release_staging" "$current_staging"
    printf 'W1 Evidence release pending 契约不一致\n' >&2
    return 1
  fi
  produced_at="$(jq -er '.produced_at' "$foundation_pending")" || {
    rm -rf "$release_staging" "$current_staging"
    return 1
  }

  if ! jq '.status = "passed"' "$foundation_pending" >"$release_staging/$foundation_file"; then
    rm -rf "$release_staging" "$current_staging"
    return 1
  fi
  if [[ "$injection" == "foundation" ]]; then
    rm -rf "$release_staging" "$current_staging"
    return 97
  fi
  if ! jq '.status = "passed"' "$governance_pending" >"$release_staging/$governance_file"; then
    rm -rf "$release_staging" "$current_staging"
    return 1
  fi
  if [[ "$injection" == "governance" ]]; then
    rm -rf "$release_staging" "$current_staging"
    return 97
  fi
  if ! jq '.status = "passed"' "$market_pending" >"$release_staging/$market_file"; then
    rm -rf "$release_staging" "$current_staging"
    return 1
  fi
  if [[ "$injection" == "market" ]]; then
    rm -rf "$release_staging" "$current_staging"
    return 97
  fi
  if ! jq '.status = "passed"' "$binding_pending" >"$release_staging/$binding_file"; then
    rm -rf "$release_staging" "$current_staging"
    return 1
  fi
  if [[ "$injection" == "binding" ]]; then
    rm -rf "$release_staging" "$current_staging"
    return 97
  fi
  if ! jq '.status = "passed"' "$republish_pending" >"$release_staging/$republish_file"; then
    rm -rf "$release_staging" "$current_staging"
    return 1
  fi
  if [[ "$injection" == "republish" ]]; then
    rm -rf "$release_staging" "$current_staging"
    return 97
  fi
  chmod 600 "$release_staging"/*.json || {
    rm -rf "$release_staging" "$current_staging"
    return 1
  }

  foundation_sha256="$(shasum -a 256 "$release_staging/$foundation_file")" || {
    rm -rf "$release_staging" "$current_staging"
    return 1
  }
  foundation_sha256="${foundation_sha256%% *}"
  governance_sha256="$(shasum -a 256 "$release_staging/$governance_file")" || {
    rm -rf "$release_staging" "$current_staging"
    return 1
  }
  governance_sha256="${governance_sha256%% *}"
  market_sha256="$(shasum -a 256 "$release_staging/$market_file")" || {
    rm -rf "$release_staging" "$current_staging"
    return 1
  }
  market_sha256="${market_sha256%% *}"
  binding_sha256="$(shasum -a 256 "$release_staging/$binding_file")" || {
    rm -rf "$release_staging" "$current_staging"
    return 1
  }
  binding_sha256="${binding_sha256%% *}"
  republish_sha256="$(shasum -a 256 "$release_staging/$republish_file")" || {
    rm -rf "$release_staging" "$current_staging"
    return 1
  }
  republish_sha256="${republish_sha256%% *}"

  if ! jq -n \
    --arg run_id "$run_id" --arg produced_at "$produced_at" --arg source "$source_digest_sha256" \
    --arg foundation_file "$run_id/$foundation_file" --arg foundation_sha256 "$foundation_sha256" \
    --arg governance_file "$run_id/$governance_file" --arg governance_sha256 "$governance_sha256" \
    --arg market_file "$run_id/$market_file" --arg market_sha256 "$market_sha256" \
    --arg binding_file "$run_id/$binding_file" --arg binding_sha256 "$binding_sha256" \
    --arg republish_file "$run_id/$republish_file" --arg republish_sha256 "$republish_sha256" \
    '{schema_version:"w1.evidence-release-manifest.v1",status:"passed",run_id:$run_id,
      produced_at:$produced_at,source_digest_sha256:$source,evidence:{
        foundation:{file:$foundation_file,schema_version:"w1.skill-foundation.smoke.evidence.v3",sha256:$foundation_sha256},
        governance:{file:$governance_file,schema_version:"w1.skill-governance.smoke.evidence.v1",sha256:$governance_sha256},
        market:{file:$market_file,schema_version:"w1.skill-market.smoke.evidence.v2",sha256:$market_sha256},
        market_binding:{file:$binding_file,schema_version:"w1.skill-market-binding.smoke.evidence.v1",sha256:$binding_sha256},
        republish_session_isolation:{file:$republish_file,schema_version:"w1.skill-republish-session-isolation.smoke.evidence.v1",sha256:$republish_sha256}}}' \
    >"$current_staging"; then
    rm -rf "$release_staging" "$current_staging"
    return 1
  fi
  chmod 600 "$current_staging" || {
    rm -rf "$release_staging" "$current_staging"
    return 1
  }
  if ! jq -e --arg run_id "$run_id" --arg source "$source_digest_sha256" '
    keys == ["evidence","produced_at","run_id","schema_version","source_digest_sha256","status"]
    and .schema_version == "w1.evidence-release-manifest.v1" and .status == "passed"
    and .run_id == $run_id and .source_digest_sha256 == $source
    and (.evidence | keys) == ["foundation","governance","market","market_binding","republish_session_isolation"]
    and all(.evidence[];
      (keys == ["file","schema_version","sha256"])
      and (.file | startswith($run_id + "/"))
      and (.sha256 | test("^[0-9a-f]{64}$")))' "$current_staging" >/dev/null; then
    rm -rf "$release_staging" "$current_staging"
    return 1
  fi

  if ! mv "$release_staging" "$release_dir"; then
    rm -rf "$release_staging" "$current_staging"
    return 1
  fi
  if [[ "$injection" == "release_dir" ]]; then
    rm -rf "$release_dir" "$current_staging"
    return 97
  fi
  # This rename is the only commit point. No individual Evidence path is authoritative.
  if ! mv "$current_staging" "$current_manifest"; then
    rm -rf "$release_dir" "$current_staging"
    return 1
  fi
}
