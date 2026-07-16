#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${ENV_FILE:-$repo_root/.env.local}"
business_pid=""
agent_pid=""
worker_pid=""

stop_processes() {
  for pid in "$business_pid" "$agent_pid" "$worker_pid"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid"
    fi
  done
  for pid in "$business_pid" "$agent_pid" "$worker_pid"; do
    if [[ -n "$pid" ]]; then
      wait "$pid" 2>/dev/null || true
    fi
  done
}

wait_ready() {
  port="$1"
  for _ in $(seq 1 60); do
    if curl --fail --silent --max-time 1 "http://127.0.0.1:${port}/readyz" >/dev/null; then
      return 0
    fi
    sleep 0.5
  done
  echo "端口 $port 的 Readiness 未在 30 秒内成功" >&2
  return 1
}

discover_local_ipv4() {
  local candidate=""
  local interface=""

  if command -v ip >/dev/null 2>&1; then
    candidate="$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == "src") {print $(i + 1); exit}}')"
  fi
  if [[ -z "$candidate" ]] && command -v route >/dev/null 2>&1 && command -v ipconfig >/dev/null 2>&1; then
    interface="$(route -n get default 2>/dev/null | awk '$1 == "interface:" {print $2; exit}')"
    if [[ -n "$interface" ]]; then
      candidate="$(ipconfig getifaddr "$interface" 2>/dev/null || true)"
    fi
  fi
  if [[ -z "$candidate" ]] && command -v ifconfig >/dev/null 2>&1; then
    candidate="$(ifconfig | awk '$1 == "inet" && $2 !~ /^127\./ && $2 !~ /^169\.254\./ && $2 !~ /^198\.1[89]\./ && $2 != "0.0.0.0" {print $2; exit}')"
  fi
  if [[ -z "$candidate" ]] && command -v hostname >/dev/null 2>&1; then
    candidate="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
  fi
  if [[ -z "$candidate" || "$candidate" == 127.* || "$candidate" == "0.0.0.0" ]]; then
    echo "无法为本机 Runtime 冒烟确定非回环 IPv4，请显式设置 Business/Agent RPC Advertised Address" >&2
    return 1
  fi
  printf '%s' "$candidate"
}

trap stop_processes EXIT
set -a
. "$env_file"
set +a

# host.docker.internal 是容器访问宿主机的别名，宿主机进程自访问时并不保证路由回监听端口。
# Foundation/Session RPC 冒烟的三个 Runtime 都运行在宿主机，因此只为模板主机名选择默认路由的非回环地址。
if [[ "${BUSINESS_RPC_ADVERTISED_ADDRESS%:*}" == "host.docker.internal" ]]; then
  rpc_port="${BUSINESS_RPC_LISTEN_ADDR##*:}"
  BUSINESS_RPC_ADVERTISED_ADDRESS="$(discover_local_ipv4):${rpc_port}"
  export BUSINESS_RPC_ADVERTISED_ADDRESS
fi
if [[ "${AGENT_RPC_ADVERTISED_ADDRESS%:*}" == "host.docker.internal" ]]; then
  rpc_port="${AGENT_RPC_LISTEN_ADDR##*:}"
  AGENT_RPC_ADVERTISED_ADDRESS="$(discover_local_ipv4):${rpc_port}"
  export AGENT_RPC_ADVERTISED_ADDRESS
fi

"$repo_root/.local/bin/business-service" &
business_pid="$!"
"$repo_root/.local/bin/agent-service" &
agent_pid="$!"
"$repo_root/.local/bin/business-worker" &
worker_pid="$!"

wait_ready 18081
wait_ready 18082
wait_ready 18083

for port in 18081 18082 18083; do
  curl --fail --silent --show-error "http://127.0.0.1:${port}/livez"
  echo
  curl --fail --silent --show-error "http://127.0.0.1:${port}/readyz"
  echo
done

etcd_container="$(docker ps -q --filter label=com.docker.compose.project=dora-local --filter label=com.docker.compose.service=etcd)"
if [[ -z "$etcd_container" ]]; then
  echo "未找到 dora-local etcd 容器" >&2
  exit 1
fi

registered_keys="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 get /dora/services/ --prefix --keys-only)"
if [[ "$registered_keys" != *"/dora/services/dora-business-service/"* ]] ||
  [[ "$registered_keys" != *"/dora/services/dora.business.foundation.v1/"* ]] ||
  [[ "$registered_keys" != *"/dora/services/dora-agent-service/"* ]] ||
  [[ "$registered_keys" != *"/dora/services/dora.agent.session.v1/"* ]]; then
  echo "Business HTTP/Foundation RPC 或 Agent HTTP/Session RPC etcd 注册缺失" >&2
  exit 1
fi

stop_processes
trap - EXIT

remaining_keys="$(docker exec "$etcd_container" /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 get /dora/services/ --prefix --keys-only)"
if [[ -n "$remaining_keys" ]]; then
  echo "服务退出后仍残留 etcd 注册键" >&2
  exit 1
fi

echo "M1.1 Runtime + M1.2 Foundation RPC + W0 Session RPC 注册/生命周期冒烟通过"
