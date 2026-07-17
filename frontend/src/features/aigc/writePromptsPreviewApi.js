import { requestJSON } from '../../platform/api/apiClient.js';
import {
  isWritePromptsPreviewUUIDV7,
  normalizeWritePromptsPreviewEnqueueRequest,
  parseWritePromptsPreviewEnqueueResponse
} from './writePromptsPreviewContract.js';

// writePromptsPreviewPath 返回 Business 同源 BFF 的唯一 Prompt Development Preview 写路径。
export function writePromptsPreviewPath(sessionID) {
  if (!isWritePromptsPreviewUUIDV7(sessionID)) {
    throw new TypeError('Write Prompts Preview API 需要规范小写 UUIDv7 session_id');
  }
  return `/api/v1/agent/sessions/${encodeURIComponent(sessionID)}/write-prompts-previews`;
}

// enqueueWritePromptsPreview 只提交持久化 typed Input；202 不是 Prompt Draft 完成结果。
export async function enqueueWritePromptsPreview({
  sessionID,
  storyboardPreviewRef,
  toolIntent,
  idempotencyKey,
  csrfToken,
  signal
} = {}) {
  if (!isWritePromptsPreviewUUIDV7(idempotencyKey)) {
    throw new TypeError('Write Prompts Preview Idempotency-Key 必须为规范小写 UUIDv7');
  }
  if (!csrfToken) throw new TypeError('Write Prompts Preview 需要内存中的 CSRF Token');
  const body = normalizeWritePromptsPreviewEnqueueRequest({ storyboardPreviewRef, toolIntent });
  const payload = await requestJSON(writePromptsPreviewPath(sessionID), {
    method: 'POST',
    expectedStatus: 202,
    headers: {
      'Idempotency-Key': idempotencyKey,
      'X-CSRF-Token': String(csrfToken)
    },
    body: JSON.stringify(body),
    signal
  });
  return parseWritePromptsPreviewEnqueueResponse(payload, sessionID);
}
