import { requestJSON } from '../../platform/api/apiClient.js';
import {
  isCanonicalTextMaterialUUIDv7,
  normalizeTextMaterialContent,
  parseTextMaterialCreateResponse,
  parseTextMaterialListResponse
} from './textMaterialContract.js';

// textMaterialPath 返回当前 Project 的同源文本素材资源路径。
export function textMaterialPath(projectID) {
  if (!isCanonicalTextMaterialUUIDv7(projectID)) throw new TypeError('Text Material API 需要规范小写 UUIDv7 project_id');
  return `/api/v1/projects/${encodeURIComponent(projectID)}/text-materials`;
}

// loadTextMaterials 读取当前 Project 最近一百条完整文本素材。
export async function loadTextMaterials({ projectID, signal } = {}) {
  const payload = await requestJSON(textMaterialPath(projectID), { method: 'GET', expectedStatus: 200, signal });
  return parseTextMaterialListResponse(payload);
}

// createTextMaterial 以 UUIDv7 Idempotency-Key 创建或同义重放一条 NFC 文本素材。
export async function createTextMaterial({ projectID, content, idempotencyKey, csrfToken, signal } = {}) {
  if (!isCanonicalTextMaterialUUIDv7(idempotencyKey)) throw new TypeError('Text Material Idempotency-Key 必须为规范小写 UUIDv7');
  if (!csrfToken) throw new TypeError('Text Material 创建需要内存中的 CSRF Token');
  const payload = await requestJSON(textMaterialPath(projectID), {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey, 'X-CSRF-Token': String(csrfToken) },
    body: JSON.stringify({ content: normalizeTextMaterialContent(content) }),
    signal
  });
  return parseTextMaterialCreateResponse(payload);
}
