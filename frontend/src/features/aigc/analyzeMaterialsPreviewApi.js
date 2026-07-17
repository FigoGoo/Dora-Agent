import { requestJSON } from '../../platform/api/apiClient.js';
import {
  isAnalyzeMaterialsUUIDV7,
  parseAnalyzeMaterialsEnqueueResponse,
  parseAnalyzeMaterialsIntent
} from './analyzeMaterialsPreviewContract.js';

export function analyzeMaterialsPreviewPath(sessionID) {
  if (!isAnalyzeMaterialsUUIDV7(sessionID)) throw new TypeError('Analyze Materials Preview API 需要规范 UUIDv7 session_id');
  return `/api/v1/agent/sessions/${encodeURIComponent(sessionID)}/analyze-materials-previews`;
}

// enqueueAnalyzeMaterialsPreview 是未接 UI 的显式 API helper；202 pending 不代表分析完成。
export async function enqueueAnalyzeMaterialsPreview({ sessionID, intent, idempotencyKey, csrfToken, signal } = {}) {
  if (!isAnalyzeMaterialsUUIDV7(idempotencyKey)) throw new TypeError('Analyze Materials Preview Idempotency-Key 必须为规范 UUIDv7');
  if (!csrfToken) throw new TypeError('Analyze Materials Preview 需要内存中的 CSRF Token');
  const body = parseAnalyzeMaterialsIntent(intent);
  const payload = await requestJSON(analyzeMaterialsPreviewPath(sessionID), {
    method: 'POST', expectedStatus: 202,
    headers: { 'Idempotency-Key': idempotencyKey, 'X-CSRF-Token': String(csrfToken) },
    body: JSON.stringify(body), signal
  });
  return parseAnalyzeMaterialsEnqueueResponse(payload, sessionID);
}
