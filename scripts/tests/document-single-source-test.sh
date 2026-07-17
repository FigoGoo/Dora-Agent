#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
plan="$repo_root/docs/requirements/full-function-smoke-development-plan.md"
index="$repo_root/docs/design/agent/graphtool/README.md"
overview="$repo_root/docs/requirements/graph-tool-requirements-overview.md"
mvp_design="$repo_root/docs/design/agent/mvp-all-tools-runtime-v1-design.md"
media_design="$repo_root/docs/design/agent/media-runtime-v3-preview-design.md"
media_contract="$repo_root/docs/design/cross-module/media-runtime-v3-preview-contract.md"
media_extension="$repo_root/docs/design/agent/mvp-six-tools-media-extension-v1-design.md"
generate_design="$repo_root/docs/design/agent/graphtool/generate_media-design.md"
assemble_design="$repo_root/docs/design/agent/graphtool/assemble_output-design.md"
agent_standards="$repo_root/.agents/skills/dora-server-development/reference/agent-development-standards.md"
chatmodel_history="$repo_root/docs/aigc-chatmodelagent-demo-design.md"
storyboard_history="$repo_root/docs/aigc-tool-storyboard-design.md"
worker_history="$repo_root/docs/aigc-worker-design.md"
makefile="$repo_root/Makefile"
trial_script="$repo_root/scripts/trial-basic.sh"

fail() {
  echo "document single-source contract failed: $*" >&2
  exit 1
}

require_text() {
  local file="$1"
  local value="$2"
  local message="$3"
  grep -F -- "$value" "$file" >/dev/null || fail "$message"
}

for file in "$plan" "$index" "$overview" "$mvp_design" "$media_design" "$media_contract" "$media_extension" "$generate_design" "$assemble_design" "$agent_standards" "$chatmodel_history" "$storyboard_history" "$worker_history" "$makefile" "$trial_script"; do
  [[ -f "$file" ]] || fail "required document is missing: ${file#$repo_root/}"
done

require_text "$plan" '状态：Active / 唯一开发排期口径' 'development plan lost its unique schedule authority marker'
require_text "$plan" '../design/agent/mvp-all-tools-runtime-v1-design.md' 'development plan does not reference the unified MVP runtime design'
require_text "$plan" '../design/agent/media-runtime-v3-preview-design.md' 'development plan does not reference the media preview design'
require_text "$plan" '../design/agent/mvp-six-tools-media-extension-v1-design.md' 'development plan does not reference the six-tool media extension'
require_text "$index" '不维护独立排期' 'Graph Tool index started maintaining a second schedule'
require_text "$index" '../../../requirements/full-function-smoke-development-plan.md' 'Graph Tool index lost the unique schedule link'
require_text "$overview" '精确阶段状态只以[功能优先开发与试跑计划]' 'requirements overview lost its schedule authority statement'

require_text "$mvp_design" 'Approved for Development Preview' 'unified MVP runtime approval status drifted'
require_text "$media_design" 'Approved for Development Preview / 不授权生产实现' 'media preview approval status drifted'
require_text "$media_contract" 'Approved for Development Preview / local-only' 'media cross-module preview status drifted'
require_text "$media_contract" 'lease_expires_at 已过期的 running/reconciling' 'media claimable view lost expired-lease takeover semantics'
require_text "$media_contract" 'assemble 使用 `image_asset`' 'media source_ref enum drifted'
require_text "$media_extension" 'Approved for Development Preview / local-only' 'six-tool media extension approval status drifted'
require_text "$media_extension" 'POST /internal/v1/media-preview-assets/prepare' 'six-tool media extension lost the frozen internal transport'
require_text "$media_extension" 'make trial-basic' 'six-tool media extension lost the one-command acceptance target'
require_text "$media_extension" '快速验收与完整质量/恢复门禁严格分离' 'six-tool media extension blurred fast trial and complete quality gates'
require_text "$index" '../mvp-six-tools-media-extension-v1-design.md' 'Graph Tool index lost the six-tool media extension link'
require_text "$overview" '../design/agent/mvp-six-tools-media-extension-v1-design.md' 'requirements overview lost the six-tool media extension link'
require_text "$generate_design" '状态：Draft' 'generate_media production design must remain Draft'
require_text "$assemble_design" '状态：Draft' 'assemble_output production design must remain Draft'
require_text "$generate_design" 'Development Preview 例外' 'generate_media lost the bounded preview exception'
require_text "$assemble_design" 'Development Preview 例外' 'assemble_output lost the bounded preview exception'
require_text "$agent_standards" 'make trial-basic' 'Agent standards lost the verified local MVP trial boundary'
require_text "$agent_standards" '不等于五条 isolated canonical smoke、进程重启、故障注入、三 Module 全量门禁或生产发布已通过' 'Agent standards blurred the fast trial and complete gates'
require_text "$plan" '该命令不串行运行五条 standalone isolated smoke' 'development plan blurred trial-basic and standalone isolated smokes'
require_text "$plan" '不得把某一侧通过改写成另一侧通过' 'development plan lost the fast/full gate non-equivalence rule'

for history in "$chatmodel_history" "$storyboard_history" "$worker_history"; do
  require_text "$history" '非当前实现真源' "historical AIGC document lost its non-authoritative marker: ${history#$repo_root/}"
  require_text "$history" 'full-function-smoke-development-plan.md' "historical AIGC document lost the unique status source: ${history#$repo_root/}"
  require_text "$history" 'graphtool/README.md' "historical AIGC document lost the Graph Tool status index: ${history#$repo_root/}"
  require_text "$history" '不得在本文增量维护当前' "historical AIGC document started maintaining current status again: ${history#$repo_root/}"
done

require_text "$makefile" 'trial-basic: test-trial-basic' 'trial-basic is no longer the bounded fast MVP target'
for standalone_target in plan-spec-preview-smoke user-message-runtime-smoke analyze-materials-runtime-smoke plan-storyboard-runtime-smoke write-prompts-runtime-smoke; do
  require_text "$makefile" "$standalone_target:" "standalone canonical smoke target disappeared: $standalone_target"
done

if grep -E '(smoke-plan-spec-preview|smoke-user-message-runtime|analyze-materials-runtime-v2-smoke|plan-storyboard-runtime-v2-smoke|write-prompts-runtime-v2-smoke)' "$trial_script" >/dev/null; then
  fail 'trial-basic absorbed isolated canonical smoke commands; fast and complete gates must remain separate'
fi

if grep -F -- 'V3 开发 Profile 待冻结' "$plan" "$index" "$overview" >/dev/null; then
  fail 'stale V3 pending-freeze wording reappeared in status documents'
fi
if grep -F -- 'V3 异步媒体闭环 |' "$plan" | grep -F -- '| 待开始 |' >/dev/null; then
  fail 'development plan regressed the active media preview implementation to not-started'
fi
if grep -E 'media\.runtime\.v3preview1.*(正在实现|实现中|正在闭环)' "$plan" "$index" "$overview" "$agent_standards" >/dev/null; then
  fail 'stale media.runtime implementation-in-progress wording reappeared in current status documents'
fi
if grep -E 'Worker.*(Job|Claim).*(仍未闭环|尚未形成|正在闭环)' "$plan" "$index" "$overview" "$agent_standards" >/dev/null; then
  fail 'stale Worker Job/Claim not-closed wording reappeared in current status documents'
fi

echo 'document single-source contract passed'
