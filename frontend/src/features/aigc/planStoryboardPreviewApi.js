import { requestJSON } from '../../platform/api/apiClient.js';
import {
  isPlanStoryboardPreviewUUIDV7,
  normalizePlanStoryboardPreviewEnqueueRequest,
  parsePlanStoryboardPreviewEnqueueResponse
} from './planStoryboardPreviewContract.js';

// planStoryboardPreviewPath 返回 Business 同源 BFF 的唯一 Storyboard Development Preview 写路径。
export function planStoryboardPreviewPath(sessionID) {
  if (!isPlanStoryboardPreviewUUIDV7(sessionID)) {
    throw new TypeError('Plan Storyboard Preview API 需要规范小写 UUIDv7 session_id');
  }
  return `/api/v1/agent/sessions/${encodeURIComponent(sessionID)}/plan-storyboard-previews`;
}

// enqueuePlanStoryboardPreview 只提交持久化 typed Input；202 pending/replayed 不是 Storyboard Draft 完成结果。
export async function enqueuePlanStoryboardPreview({
  sessionID,
  creationSpecRef,
  toolIntent,
  idempotencyKey,
  csrfToken,
  signal
} = {}) {
  if (!isPlanStoryboardPreviewUUIDV7(idempotencyKey)) {
    throw new TypeError('Plan Storyboard Preview Idempotency-Key 必须为规范小写 UUIDv7');
  }
  if (!csrfToken) throw new TypeError('Plan Storyboard Preview 需要内存中的 CSRF Token');
  const body = normalizePlanStoryboardPreviewEnqueueRequest({ creationSpecRef, toolIntent });
  const payload = await requestJSON(planStoryboardPreviewPath(sessionID), {
    method: 'POST',
    expectedStatus: 202,
    headers: {
      'Idempotency-Key': idempotencyKey,
      'X-CSRF-Token': String(csrfToken)
    },
    body: JSON.stringify(body),
    signal
  });
  return parsePlanStoryboardPreviewEnqueueResponse(payload, sessionID);
}
