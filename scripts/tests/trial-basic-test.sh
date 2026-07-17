#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
trial_script="$repo_root/scripts/trial-basic.sh"
browser_spec="$repo_root/frontend/e2e/trial-basic.spec.js"
env_template="$repo_root/.env.example"

fail() {
  printf 'trial-basic contract failed: %s\n' "$1" >&2
  exit 1
}

[[ -x "$trial_script" ]] || fail 'trial script is not executable'
[[ -r "$browser_spec" ]] || fail 'Chromium spec is missing'
bash -n "$trial_script" || fail 'trial script has invalid shell syntax'
node --check "$browser_spec" || fail 'Chromium spec has invalid JavaScript syntax'

require_in() {
  local path="$1" literal="$2" message="$3"
  grep -F -- "$literal" "$path" >/dev/null || fail "$message"
}
require_trial() { require_in "$trial_script" "$1" "$2"; }
require_browser() { require_in "$browser_spec" "$1" "$2"; }
require_env() { require_in "$env_template" "$1" "$2"; }

require_trial '. "$repo_root/scripts/lib/smoke-secret-transport.sh"' 'secret transport guard is missing'
require_trial 'disable_shell_xtrace' 'xtrace is not disabled'
require_trial 'umask 077' 'restrictive umask is missing'
require_trial "trap 'exit 130' INT" 'SIGINT is not fail-closed'
require_trial "trap 'exit 143' TERM" 'SIGTERM is not fail-closed'
require_trial 'refusing to use a PostgreSQL database other than $database' 'dedicated test database guard is missing'
require_trial 'go_bin="$(command -v "$go_bin"' 'Go command-name resolution is missing'
require_trial 'go_bin="$default_go_bin"' 'Go command-name fallback is missing'
for module in business agent worker; do
  case "$module" in
    business) database_variable='BUSINESS_DATABASE_URL' ;;
    agent) database_variable='AGENT_DATABASE_URL' ;;
    worker) database_variable='WORKER_DATABASE_URL' ;;
  esac
  require_trial "reset_test_database $module \"\$$database_variable\"" "$module test database reset is missing"
  require_trial "\"\$repo_root/scripts/migrate.sh\" $module up" "$module migration is missing"
  require_trial "-C \"\$repo_root/$module\" build" "$module build from worktree is missing"
done
require_trial 'go_bin" run -tags localsmoke ./cmd/local-smoke-seeder' 'local user seed is missing'
require_trial 'DORA_AGENT_RUNTIME_PROFILE=mvp_all_tools.runtime.v1preview1' 'Agent base Profile is not pinned'
require_trial 'DORA_BUSINESS_RUNTIME_PROFILE=mvp_all_tools.runtime.v1preview1' 'Business base Profile is not pinned'
require_trial 'DORA_AGENT_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1' 'Agent media Profile is not pinned'
require_trial 'DORA_BUSINESS_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1' 'Business media Profile is not pinned'
require_trial 'DORA_WORKER_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1' 'Worker media Profile is not pinned'
require_trial 'DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false' 'isolated Agent Profile is not disabled'
require_trial 'DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED=false' 'isolated Business Profile is not disabled'
require_trial 'DORA_WORKER_AGENT_CONSUMER_DSN=' 'Worker Agent least-privilege DSN is missing'
require_trial 'real non-symlink ffmpeg executable is required' 'ffmpeg real-path gate is missing'
require_trial './node_modules/.bin/vite --host 127.0.0.1' 'Vite is not loopback-bound'
require_trial 'playwright test e2e/trial-basic.spec.js' 'real Chromium chain is not executed'
require_trial 'source-before.sha256' 'source before barrier is missing'
require_trial 'source-after.sha256' 'source after barrier is missing'
require_trial 'trial_basic.evidence.v1' 'published Evidence schema is missing'
require_trial 'assert_evidence_redacted' 'Evidence redaction gate is missing'
require_trial 'chmod 600 "$evidence_file"' 'Evidence mode is not restricted'
require_trial "wait_etcd_prefix_count '/dora/services/dora-agent-service/' 0 Agent" 'Agent cleanup is not asserted'
require_trial "wait_etcd_prefix_count '/dora/services/dora-business-service/' 0 Business" 'Business cleanup is not asserted'
require_trial 'stop_pid_strict "$vite_pid" Vite true' 'Vite SIGTERM shutdown is not explicitly managed'
require_trial '"$allow_sigterm_exit" == true && "$wait_status" -eq 143' 'only the managed Vite SIGTERM exit may be accepted'

for path in "$trial_script" "$browser_spec"; do
  if rg -n '(/var/run/docker\.sock|docker[[:space:]]+(info|ps|compose|exec)|psql([[:space:]]|$)|redis-cli|etcdctl)' "$path" >/dev/null; then
    fail "trial depends on container control-plane or external database CLI: $path"
  fi
done

require_browser "@trial-basic unified six-tool browser chain" 'browser tag is missing'
require_browser "name: '开始创作'" 'QuickCreate is missing'
require_browser '/text-materials' 'real text material API is missing'
for tool in creation-spec-previews analyze-materials-previews plan-storyboard-previews write-prompts-previews generate-media-previews assemble-output-previews; do
  require_browser "/$tool" "$tool endpoint is missing"
done
require_browser "name: '生成测试 PNG'" 'PNG action is missing'
require_browser "name: '装配测试 MP4'" 'MP4 action is missing'
require_browser "method: 'HEAD'" 'HEAD validation is missing'
require_browser "Range: 'bytes=0-15'" '206 Range validation is missing'
require_browser "Range: 'bytes=999999999-'" '416 Range validation is missing'
require_browser 'await page.reload()' 'hard refresh validation is missing'
require_browser "snapshot.schema_version).toBe('session.workspace.v5')" 'Workspace V5 recovery is missing'
require_browser 'png_decodable_in_browser' 'browser PNG decode is missing'
require_browser 'mp4_video_element_ready' 'browser MP4 playability is missing'
require_browser 'two_terminal_media_jobs' 'two terminal Job/digest/size validation is missing'
require_browser 'media_results: mediaResults' 'safe media Job Evidence is missing'

require_env 'DORA_BUSINESS_RUNTIME_PROFILE=' 'Business base Profile template is missing'
require_env 'DORA_AGENT_RUNTIME_PROFILE=' 'Agent base Profile template is missing'
require_env 'DORA_WORKER_MEDIA_RUNTIME_PROFILE=' 'Worker media Profile template is missing'
require_env 'WORKER_HTTP_ADDR=127.0.0.1:18083' 'Worker media-compatible loopback address is missing'

printf 'trial-basic contract passed\n'
