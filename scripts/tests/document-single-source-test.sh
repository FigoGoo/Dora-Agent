#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "document single-source contract failed: $*" >&2
  exit 1
}

require_file() {
  [[ -f "$repo_root/$1" ]] || fail "required document is missing: $1"
}

require_text() {
  local file="$repo_root/$1"
  local value="$2"
  local message="$3"
  grep -F -- "$value" "$file" >/dev/null || fail "$message"
}

require_unique_marker() {
  local marker="$1"
  local count
  count="$(grep -R -F --include='*.md' -- "$marker" \
    "$repo_root/docs" "$repo_root/.agents" "$repo_root/README.md" "$repo_root/AGENTS.md" | wc -l | tr -d ' ')"
  [[ "$count" -eq 1 ]] || fail "marker must have exactly one owner: $marker (found $count)"
}

check_relative_links() {
  local file="$1"
  local directory="${file%/*}"
  local target
  [[ "$directory" != "$file" ]] || directory="."

  while IFS= read -r target; do
    target="${target#](}"
    target="${target%)}"
    target="${target%%#*}"
    target="${target%% *}"
    case "$target" in
      ""|http://*|https://*|mailto:*|data:*) continue ;;
    esac
    [[ -e "$repo_root/$directory/$target" ]] || fail "broken Markdown link in $file: $target"
  done < <(grep -Eo '\]\([^)]+\)' "$repo_root/$file" || true)
}

active_documents=(
  "docs/README.md"
  "docs/requirements/product-scope.md"
  "docs/requirements/delivery-status.md"
  "docs/design/system-architecture.md"
  "docs/design/functions/identity-and-projects.md"
  "docs/design/functions/skills-and-governance.md"
  "docs/design/functions/materials-and-analysis.md"
  "docs/design/functions/creation-workflow.md"
  "docs/design/functions/media-and-assets.md"
  "docs/design/functions/workspace-and-events.md"
  "docs/design/functions/runtime-and-quality.md"
  "docs/design/agent/graphtool/README.md"
  "README.md"
  "AGENTS.md"
  ".agents/skills/dora-server-development/SKILL.md"
  ".agents/skills/dora-server-development/reference/business-server-development-standards.md"
  ".agents/skills/dora-server-development/reference/business-worker-development-standards.md"
  ".agents/skills/dora-server-development/reference/agent-development-standards.md"
)

graph_tool_documents=(
  "docs/design/agent/graphtool/plan_creation_spec-design.md"
  "docs/design/agent/graphtool/analyze_materials-design.md"
  "docs/design/agent/graphtool/plan_storyboard-design.md"
  "docs/design/agent/graphtool/write_prompts-design.md"
  "docs/design/agent/graphtool/generate_media-design.md"
  "docs/design/agent/graphtool/assemble_output-design.md"
)

frozen_approval_artifacts=(
  "docs/design/agent/approvals/analyze_materials_runtime_v2preview1/approval_manifest.json"
  "docs/design/agent/approvals/user_message_runtime_v2preview1/approval_manifest.json"
  "docs/design/agent/approvals/immutable_turn_context_v1/approval_manifest.json"
  "docs/design/agent/analyze-materials-runtime-v2-design.md"
  "docs/design/agent/user-message-runtime-v2-design.md"
  "docs/design/agent/immutable-turn-context-decision-review-v1.md"
  "docs/design/agent/immutable-turn-context-design-v1.md"
  "docs/design/agent/approval-consumption-receipt-contract-v1.md"
  "docs/design/agent/approval-continuation-cross-object-evidence-v1.md"
  "docs/design/agent/continuation-child-tool-receipt-contract-v1.md"
  "docs/design/agent/session-lane-postgresql-design-v1.md"
  "docs/design/cross-module/creation-spec-candidate-decision-contract-v1.md"
  "docs/design/cross-module/testdata/creation_spec_preview_save_digest_v1.json"
  "docs/design/cross-module/testdata/storyboard_preview_save_digest_v1.json"
  "docs/design/cross-module/testdata/prompt_preview_save_digest_v1.json"
)

for file in "${active_documents[@]}" "${graph_tool_documents[@]}" "${frozen_approval_artifacts[@]}"; do
  require_file "$file"
done

require_text "docs/README.md" "状态：Current / 唯一文档入口" "docs index lost its unique-entry marker"
require_text "docs/requirements/delivery-status.md" "状态：Active / 唯一阶段状态源" "delivery status lost its unique-state marker"
require_text "docs/design/system-architecture.md" "状态：Current / 当前实现真源" "system architecture is no longer the current architecture source"
require_unique_marker "状态：Current / 唯一文档入口"
require_unique_marker "状态：Active / 唯一阶段状态源"
require_unique_marker "状态：Current / 当前实现真源"
require_text "README.md" "docs/README.md" "root README lost the unique documentation entry"
require_text "AGENTS.md" "docs/requirements/delivery-status.md" "AGENTS lost the current delivery-status route"
require_text ".agents/skills/dora-server-development/SKILL.md" "docs/requirements/delivery-status.md" "server skill lost the current delivery-status route"
require_text ".agents/skills/dora-server-development/reference/agent-development-standards.md" "文档按功能维护" "Agent standards lost the functional-document rule"

for file in "${graph_tool_documents[@]}"; do
  require_text "$file" "Development Preview" "Graph Tool design lost its local Preview boundary: $file"
  require_text "$file" "生产" "Graph Tool design lost its production boundary: $file"
  [[ "$(grep -c '^```mermaid' "$repo_root/$file")" -ge 2 ]] || fail "Graph Tool design needs Graph and state diagrams: $file"
  require_text "$file" "## 4. 稳定 Node / Branch exact-set" "Graph Tool design lost its exact Node/Branch section: $file"
  require_text "$file" "## 5. 强类型 Graph State 摘要" "Graph Tool design lost its Graph State section: $file"
  require_text "$file" "## 6. 业务状态机与迁移表" "Graph Tool design lost its separate business state machine: $file"
done

obsolete_documents=(
  "docs/aigc-a2ui-design.md"
  "docs/aigc-chatmodelagent-demo-design.md"
  "docs/aigc-tool-storyboard-design.md"
  "docs/aigc-worker-design.md"
  "docs/design/agent/a2ui-event-action-contract-v1.md"
  "docs/design/agent/eino-dependency-lock-review-v1.md"
  "docs/design/agent/graph-tool-result-receipt-contract-v1.md"
  "docs/design/agent/graphtool/plan_creation_spec-w2-r04-gap-review.md"
  "docs/design/agent/media-runtime-v3-preview-design.md"
  "docs/design/agent/mvp-all-tools-runtime-v1-design.md"
  "docs/design/agent/mvp-six-tools-media-extension-v1-design.md"
  "docs/design/agent/plan-storyboard-runtime-v2-design.md"
  "docs/design/agent/runner-session-lane-review-v1.md"
  "docs/design/agent/session-event-foundation-marker-v1.md"
  "docs/design/agent/session-event-foundation-review.md"
  "docs/design/agent/session-lane-ingress-command-contract-v1.md"
  "docs/design/agent/session-lane-legacy-upgrade-contract-v1.md"
  "docs/design/agent/session-lane-runtime-contract-v1.md"
  "docs/design/agent/session-skill-snapshot-v2-review.md"
  "docs/design/agent/tool-definition-catalog-v1.md"
  "docs/design/agent/write-prompts-runtime-v2-design.md"
  "docs/design/business/auth-project-foundation-review.md"
  "docs/design/business/w1-public-market-binding-v1.md"
  "docs/design/business/w1-reviewer-rbac-admin-review-v1.md"
  "docs/design/business/w1-skill-governance-v1.md"
  "docs/design/business/w1-skill-market-read-v1.md"
  "docs/design/cross-module/aigc-contract-catalog.md"
  "docs/design/cross-module/foundation-rpc-v1.md"
  "docs/design/cross-module/material-analysis-evidence-preview-v1.md"
  "docs/design/cross-module/media-runtime-v3-preview-contract.md"
  "docs/design/cross-module/persistence-foundation-v1.md"
  "docs/design/cross-module/project-skill-binding-contract-v1.md"
  "docs/design/cross-module/w0-identity-workspace-contract-v1.md"
  "docs/design/cross-module/w05-workspace-transport-contract-v1.md"
  "docs/design/cross-module/w1-skill-tool-entry-contract-v1.md"
  "docs/design/frontend/integration-foundation-v1.md"
  "docs/design/testing/smk-001-004-vertical-slice-review.md"
  "docs/design/testing/full-function-smoke-engineering-design.md"
  "docs/design/migration/main-branch-aigc-asset-inventory.md"
  "docs/requirements/admin-requirements-overview.md"
  "docs/requirements/common-requirements-baseline.md"
  "docs/requirements/full-function-smoke-development-plan.md"
  "docs/requirements/graph-tool-requirements-overview.md"
  "docs/requirements/payment-requirements-overview.md"
  "docs/requirements/server-requirements-overview.md"
  "docs/requirements/user-requirements-overview.md"
)

current_documents=("${active_documents[@]}" "${graph_tool_documents[@]}")
allowed_markdown=("${active_documents[@]}" "${graph_tool_documents[@]}" "${frozen_approval_artifacts[@]}")

while IFS= read -r absolute_file; do
  relative_file="${absolute_file#$repo_root/}"
  allowed=false
  for file in "${allowed_markdown[@]}"; do
    if [[ "$relative_file" == "$file" ]]; then
      allowed=true
      break
    fi
  done
  [[ "$allowed" == true ]] || fail "unrouted Markdown document: $relative_file"
done < <(find "$repo_root/docs" -type f -name '*.md' -print)

for file in "${obsolete_documents[@]}"; do
  [[ ! -e "$repo_root/$file" ]] || fail "obsolete document reappeared: $file"
  obsolete_name="${file##*/}"
  if grep -F -- "$obsolete_name" "${current_documents[@]/#/$repo_root/}" >/dev/null; then
    fail "current documentation still references an obsolete source: $obsolete_name"
  fi
done

for file in "${current_documents[@]}"; do
  check_relative_links "$file"
done

makefile="$repo_root/Makefile"
trial_script="$repo_root/scripts/trial-basic.sh"
require_file "Makefile"
require_file "scripts/trial-basic.sh"
require_text "Makefile" "trial-basic: test-trial-basic" "trial-basic is no longer the bounded fast MVP target"
for target in plan-spec-preview-smoke user-message-runtime-smoke analyze-materials-runtime-smoke plan-storyboard-runtime-smoke write-prompts-runtime-smoke; do
  grep -E "^${target}:" "$makefile" >/dev/null || fail "standalone smoke target disappeared: $target"
done

if grep -E '(smoke-plan-spec-preview|smoke-user-message-runtime|analyze-materials-runtime-v2-smoke|plan-storyboard-runtime-v2-smoke|write-prompts-runtime-v2-smoke|smoke-w0-transport|W1_RUN_|test-smoke-contracts|check-frontend|go[[:space:]]+(test|vet)|npm[[:space:]]+(test|run[[:space:]]+build))' "$trial_script" >/dev/null; then
  fail "trial-basic absorbed an isolated, module, Skill/Workspace, or frontend quality gate"
fi

echo "document single-source contract passed"
