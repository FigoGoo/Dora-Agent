import { requestJSON } from '../../platform/api/apiClient.js';
import {
  isCanonicalPreviewUUIDV7,
  normalizeCreationSpecPreviewIntent,
  parseCreationSpecPreviewEnqueueResponse
} from './creationSpecPreviewContract.js';

export function creationSpecPreviewPath(sessionID) {
  if (!isCanonicalPreviewUUIDV7(sessionID)) {
    throw new TypeError('Creation Spec Preview API 需要规范小写 UUIDv7 session_id');
  }
  return `/api/v1/agent/sessions/${encodeURIComponent(sessionID)}/creation-spec-previews`;
}

// enqueueCreationSpecPreview 只提交持久化意图；202 pending 不是 Creation Spec 成功结果。
export async function enqueueCreationSpecPreview({
  sessionID,
  intent,
  idempotencyKey,
  csrfToken,
  signal
} = {}) {
  if (!isCanonicalPreviewUUIDV7(idempotencyKey)) {
    throw new TypeError('Creation Spec Preview Idempotency-Key 必须为规范小写 UUIDv7');
  }
  if (!csrfToken) throw new TypeError('Creation Spec Preview 需要内存中的 CSRF Token');
  const body = normalizeCreationSpecPreviewIntent(intent);
  const payload = await requestJSON(creationSpecPreviewPath(sessionID), {
    method: 'POST',
    expectedStatus: 202,
    headers: {
      'Idempotency-Key': idempotencyKey,
      'X-CSRF-Token': String(csrfToken)
    },
    body: JSON.stringify(body),
    signal
  });
  return parseCreationSpecPreviewEnqueueResponse(payload, sessionID);
}
