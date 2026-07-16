#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
kitex_bin="${KITEX_BIN:-$repo_root/.local/tools/kitex}"
thriftgo_bin="${THRIFTGO_BIN:-$repo_root/.local/tools/thriftgo}"
idl_path="api/thrift/foundation/v1/foundation.thrift"

if [[ "$($kitex_bin -version 2>&1)" != "v0.16.2" ]]; then
  echo "Foundation RPC 必须使用 Kitex v0.16.2 生成" >&2
  exit 2
fi
if [[ "$($thriftgo_bin --version 2>&1)" != "thriftgo 0.4.5" ]]; then
  echo "Foundation RPC 必须使用 thriftgo 0.4.5 生成" >&2
  exit 2
fi

(
  cd "$repo_root/business"
  "$kitex_bin" -module github.com/FigoGoo/Dora-Agent/business \
    -compiler-path "$thriftgo_bin" "$idl_path"
)

(
  cd "$repo_root/agent"
  "$kitex_bin" -module github.com/FigoGoo/Dora-Agent/agent \
    -compiler-path "$thriftgo_bin" "../business/$idl_path"
)
