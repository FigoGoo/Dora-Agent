import { requestJSON } from '../../platform/api/apiClient.js';
import { parseProjectListResponse } from './projectListContract.js';

export const PROJECT_LIST_PATH = '/api/v1/projects';
export const PROJECT_LIST_DEFAULT_LIMIT = 20;
export const PROJECT_LIST_MAX_LIMIT = 100;

// listProjects 使用可选不透明 after 游标读取当前 Cookie 会话所有的项目列表。
export async function listProjects({ limit = PROJECT_LIST_DEFAULT_LIMIT, after, signal } = {}) {
  if (!Number.isInteger(limit) || limit < 1 || limit > PROJECT_LIST_MAX_LIMIT) {
    throw new TypeError(`Project 列表 limit 必须为 1 到 ${PROJECT_LIST_MAX_LIMIT} 的整数`);
  }
  if (after != null && (typeof after !== 'string' || !after || after.length > 512 || !/^[A-Za-z0-9_-]+$/.test(after))) {
    throw new TypeError('Project 列表 after 必须为有界无填充 Base64URL');
  }
  const query = new URLSearchParams({ limit: String(limit) });
  if (after != null) query.set('after', after);
  const payload = await requestJSON(`${PROJECT_LIST_PATH}?${query.toString()}`, { method: 'GET', signal });
  return parseProjectListResponse(payload);
}
