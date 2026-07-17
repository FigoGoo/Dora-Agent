import { requestJSON } from '../../platform/api/apiClient.js';
import {
  isMediaPreviewUUIDV7,
  parseAssembleOutputPreviewRequest,
  parseGenerateMediaPreviewRequest,
  parseMediaPreviewEnqueue
} from './mediaPreviewContract.js';

export function mediaPreviewPath(sessionID, toolKey) {
  if (!isMediaPreviewUUIDV7(sessionID)) throw new TypeError('Media Preview API 需要规范 UUIDv7 session_id');
  if (toolKey === 'generate_media') return `/api/v1/agent/sessions/${encodeURIComponent(sessionID)}/generate-media-previews`;
  if (toolKey === 'assemble_output') return `/api/v1/agent/sessions/${encodeURIComponent(sessionID)}/assemble-output-previews`;
  throw new TypeError('Media Preview API tool_key 未知');
}

export async function enqueueGenerateMediaPreview({ sessionID, request, idempotencyKey, csrfToken, signal } = {}) {
  return enqueue({ sessionID, toolKey: 'generate_media', request: parseGenerateMediaPreviewRequest(request), idempotencyKey, csrfToken, signal });
}

export async function enqueueAssembleOutputPreview({ sessionID, request, idempotencyKey, csrfToken, signal } = {}) {
  return enqueue({ sessionID, toolKey: 'assemble_output', request: parseAssembleOutputPreviewRequest(request), idempotencyKey, csrfToken, signal });
}

async function enqueue({ sessionID, toolKey, request, idempotencyKey, csrfToken, signal }) {
  if (!isMediaPreviewUUIDV7(idempotencyKey)) throw new TypeError('Media Preview Idempotency-Key 必须为规范 UUIDv7');
  if (!csrfToken) throw new TypeError('Media Preview 需要内存中的 CSRF Token');
  const payload = await requestJSON(mediaPreviewPath(sessionID, toolKey), {
    method: 'POST', expectedStatus: 202,
    headers: { 'Idempotency-Key': idempotencyKey, 'X-CSRF-Token': String(csrfToken) },
    body: JSON.stringify(request), signal
  });
  return parseMediaPreviewEnqueue(payload, sessionID, toolKey);
}
