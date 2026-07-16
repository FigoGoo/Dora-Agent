import { requestJSON } from '../../platform/api/apiClient.js';

export const PROJECT_QUICK_CREATE_PATH = '/api/v1/projects:quick-create';
export const PROJECT_QUICK_CREATE_SCHEMA_V2 = 'project_quick_create.v2';
export const PROJECT_QUICK_CREATE_MAX_SKILL_COUNT = 16;
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

// quickCreateProject 提交一次稳定创建意图；调用方拥有并复用 Idempotency-Key。
export function quickCreateProject({ prompt, enabledSkillIDs = [], idempotencyKey, csrfToken, signal } = {}) {
  if (!idempotencyKey) {
    throw new TypeError('quickCreateProject 需要稳定的 Idempotency-Key');
  }
  if (!csrfToken) {
    throw new TypeError('quickCreateProject 需要内存中的 CSRF Token');
  }
  const normalizedSkillIDs = normalizeSkillIDs(enabledSkillIDs);
  const body = normalizedSkillIDs.length > 0
    ? {
        schema_version: PROJECT_QUICK_CREATE_SCHEMA_V2,
        initial_prompt: prompt == null ? null : String(prompt),
        enabled_skill_ids: normalizedSkillIDs
      }
    : { initial_prompt: prompt == null ? null : String(prompt) };
  const headers = { 'Idempotency-Key': idempotencyKey };
  headers['X-CSRF-Token'] = csrfToken;
  return requestJSON(PROJECT_QUICK_CREATE_PATH, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
    signal
  });
}

function normalizeSkillIDs(enabledSkillIDs) {
  if (!Array.isArray(enabledSkillIDs)) {
    throw new TypeError('enabledSkillIDs 必须为数组');
  }
  if (enabledSkillIDs.some((skillID) => typeof skillID !== 'string' || !UUID_V7_PATTERN.test(skillID))) {
    throw new TypeError('enabledSkillIDs 只能包含规范小写 UUIDv7');
  }
  const normalized = [...enabledSkillIDs];
  if (new Set(normalized).size !== normalized.length) {
    throw new TypeError('enabledSkillIDs 不得包含重复 ID');
  }
  if (normalized.length > PROJECT_QUICK_CREATE_MAX_SKILL_COUNT) {
    throw new TypeError(`enabledSkillIDs 最多包含 ${PROJECT_QUICK_CREATE_MAX_SKILL_COUNT} 个 Skill`);
  }
  return [...normalized].sort();
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
