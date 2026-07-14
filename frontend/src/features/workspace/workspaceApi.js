import { requestJSON } from '../../platform/api/apiClient.js';
import { canonicalCursor } from './workspaceContract.js';

// loadAgentWorkspaceSnapshot 始终访问 Business 同源 BFF，不接收或拼接 Agent Base URL。
export function loadAgentWorkspaceSnapshot(sessionID, { signal } = {}) {
  if (!sessionID) throw new TypeError('loadAgentWorkspaceSnapshot 需要 session_id');
  return requestJSON(`/api/v1/agent/sessions/${encodeURIComponent(sessionID)}/workspace`, {
    method: 'GET',
    signal
  });
}

// workspaceEventsPath 生成只包含冻结 after_seq 的同源 SSE 路径。
export function workspaceEventsPath(sessionID, cursor) {
  if (!sessionID) throw new TypeError('workspaceEventsPath 需要 session_id');
  const normalizedCursor = canonicalCursor(cursor, 'after_seq');
  return `/api/v1/agent/sessions/${encodeURIComponent(sessionID)}/events?after_seq=${normalizedCursor}`;
}
