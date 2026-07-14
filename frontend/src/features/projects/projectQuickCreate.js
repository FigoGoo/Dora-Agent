import { requestJSON } from '../../platform/api/apiClient.js';

export const PROJECT_QUICK_CREATE_PATH = '/api/v1/projects:quick-create';

// quickCreateProject 提交一次稳定创建意图；调用方拥有并复用 Idempotency-Key。
export function quickCreateProject({ prompt, idempotencyKey, csrfToken, signal } = {}) {
  if (!idempotencyKey) {
    throw new TypeError('quickCreateProject 需要稳定的 Idempotency-Key');
  }
  if (!csrfToken) {
    throw new TypeError('quickCreateProject 需要内存中的 CSRF Token');
  }
  const headers = { 'Idempotency-Key': idempotencyKey };
  headers['X-CSRF-Token'] = csrfToken;
  return requestJSON(PROJECT_QUICK_CREATE_PATH, {
    method: 'POST',
    headers,
    body: JSON.stringify({ initial_prompt: prompt == null ? null : String(prompt) }),
    signal
  });
}

// bootstrapProjectWorkspace 在 Project 已受理后读取 Agent Session 初始化结果。
export function bootstrapProjectWorkspace(projectID, { signal } = {}) {
  if (!projectID) {
    throw new TypeError('bootstrapProjectWorkspace 需要 project_id');
  }
  return requestJSON(`/api/v1/projects/${encodeURIComponent(projectID)}/bootstrap`, {
    method: 'GET',
    signal
  });
}
