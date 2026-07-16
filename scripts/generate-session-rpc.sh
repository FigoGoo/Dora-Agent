#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
kitex_bin="${KITEX_BIN:-$repo_root/.local/tools/kitex}"
thriftgo_bin="${THRIFTGO_BIN:-$repo_root/.local/tools/thriftgo}"
idl_path="api/thrift/session/v1/session.thrift"

if [[ "$($kitex_bin -version 2>&1)" != "v0.16.2" ]]; then
  echo "Session RPC 必须使用 Kitex v0.16.2 生成" >&2
  exit 2
fi
if [[ "$($thriftgo_bin --version 2>&1)" != "thriftgo 0.4.5" ]]; then
  echo "Session RPC 必须使用 thriftgo 0.4.5 生成" >&2
  exit 2
fi

# Agent 拥有 IDL Source 与 Server 生成类型；Business 只生成同一 Source 的消费端类型，禁止复制 IDL。
(
  cd "$repo_root/agent"
  "$kitex_bin" -module github.com/FigoGoo/Dora-Agent/agent \
    -compiler-path "$thriftgo_bin" "$idl_path"
)

(
  cd "$repo_root/business"
  "$kitex_bin" -module github.com/FigoGoo/Dora-Agent/business \
    -compiler-path "$thriftgo_bin" "../agent/$idl_path"
)
