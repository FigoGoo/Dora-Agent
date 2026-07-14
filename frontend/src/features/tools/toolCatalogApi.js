import { requestJSON } from '../../platform/api/apiClient.js';
import { canonicalToolCatalogUUIDV7, parseToolCatalogResponse } from './toolCatalogContract.js';

export function toolCatalogPath(sessionID) {
  const canonicalSessionID = canonicalToolCatalogUUIDV7(sessionID, 'session_id');
  return `/api/v1/agent/sessions/${canonicalSessionID}/tools`;
}

// loadToolCatalog 只读取 Business 同源 BFF；不接受 Query、Agent Base URL 或客户端目录补丁。
export async function loadToolCatalog(sessionID, { signal } = {}) {
  const payload = await requestJSON(toolCatalogPath(sessionID), {
    method: 'GET',
    cache: 'no-store',
    signal
  });
  return parseToolCatalogResponse(payload);
}
