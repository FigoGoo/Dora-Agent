import { PROJECT_QUICK_CREATE_MAX_SKILL_COUNT } from './projectQuickCreate.js';

export const QUICK_CREATE_STATUS = Object.freeze({
  EDITING: 'editing',
  AWAITING_AUTH: 'awaiting_auth',
  SUBMITTING: 'submitting',
  PROVISIONING: 'provisioning',
  CREATED: 'created',
  CONFLICT: 'conflict',
  RETRYABLE_ERROR: 'retryable_error',
  FAILED: 'failed'
});
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

// createQuickCreateIntent 在用户首次提交时冻结原始 Prompt、Skill 集合与幂等键。
export function createQuickCreateIntent(prompt, { keyFactory = createIdempotencyKey, enabledSkillIDs = [] } = {}) {
  const frozenSkillIDs = normalizeSkillIDs(enabledSkillIDs);
  return {
    prompt: prompt == null ? '' : String(prompt),
    enabledSkillIDs: frozenSkillIDs,
    idempotencyKey: keyFactory(),
    status: QUICK_CREATE_STATUS.EDITING,
    attempts: 0,
    projectID: '',
    sessionID: '',
    error: null
  };
}

function normalizeSkillIDs(skillIDs) {
  if (!Array.isArray(skillIDs)) throw new TypeError('Quick Create Skill 集合必须为数组');
  if (skillIDs.some((skillID) => typeof skillID !== 'string' || !UUID_V7_PATTERN.test(skillID))) {
    throw new TypeError('Quick Create Skill 集合只能包含规范小写 UUIDv7');
  }
  const normalized = [...skillIDs];
  if (new Set(normalized).size !== normalized.length) {
    throw new TypeError('Quick Create Skill 集合包含重复项');
  }
  if (normalized.length > PROJECT_QUICK_CREATE_MAX_SKILL_COUNT) {
    throw new TypeError(`Quick Create 最多选择 ${PROJECT_QUICK_CREATE_MAX_SKILL_COUNT} 个 Skill`);
  }
  return Object.freeze([...normalized].sort());
}

// submitQuickCreateIntent 对重复点击和网络重试始终复用原意图的 key。
export function submitQuickCreateIntent(intent) {
  assertIntent(intent);
  if (
    intent.status === QUICK_CREATE_STATUS.CONFLICT
    || intent.status === QUICK_CREATE_STATUS.CREATED
    || intent.status === QUICK_CREATE_STATUS.FAILED
  ) {
    throw new Error(`状态 ${intent.status} 不能重试原 Quick Create 意图`);
  }
  return {
    ...intent,
    status: QUICK_CREATE_STATUS.SUBMITTING,
    attempts: intent.attempts + 1,
    error: null
  };
}

// resolveQuickCreateIntent 接受首次只有 project_id 的 provisioning 响应，也接受后续 Session Bootstrap。
export function resolveQuickCreateIntent(intent, payload) {
  assertIntent(intent);
  const projectID = String(payload?.project_id || intent.projectID || '');
  if (!projectID) {
    throw new Error('Quick Create 响应缺少 project_id');
  }
  const sessionID = String(payload?.session_id || intent.sessionID || '');
  const provisioning = String(payload?.creation_status || '');
  if (provisioning !== 'provisioning' && provisioning !== 'ready') {
    throw new Error('Quick Create 响应包含未知 creation_status');
  }
  if ((provisioning === 'ready') !== Boolean(sessionID)) {
    throw new Error('Quick Create 响应的 ready 状态与 session_id 不一致');
  }
  return {
    ...intent,
    projectID,
    sessionID,
    status: provisioning === 'ready' ? QUICK_CREATE_STATUS.CREATED : QUICK_CREATE_STATUS.PROVISIONING,
    error: null
  };
}

// rejectQuickCreateIntent 保留原 key；只有稳定冲突会开放 replaceConflictedQuickCreateIntent。
export function rejectQuickCreateIntent(intent, error) {
  assertIntent(intent);
  const conflict = Number(error?.status) === 409 && error?.code === 'IDEMPOTENCY_CONFLICT';
  const status = Number(error?.status) || 0;
  const retryable = Boolean(error?.retryable) || status === 0 || status >= 500;
  return {
    ...intent,
    status: conflict
      ? QUICK_CREATE_STATUS.CONFLICT
      : retryable ? QUICK_CREATE_STATUS.RETRYABLE_ERROR : QUICK_CREATE_STATUS.FAILED,
    error: publicError(error)
  };
}

// replaceConflictedQuickCreateIntent 只允许在服务端确认同键异义后生成新意图和新 key。
export function replaceConflictedQuickCreateIntent(intent, prompt = intent?.prompt, options) {
  assertIntent(intent);
  if (intent.status !== QUICK_CREATE_STATUS.CONFLICT) {
    throw new Error('只有 IDEMPOTENCY_CONFLICT 才能替换 Quick Create 意图');
  }
  return createQuickCreateIntent(prompt, {
    ...options,
    enabledSkillIDs: options?.enabledSkillIDs ?? intent.enabledSkillIDs
  });
}

export function createIdempotencyKey() {
  if (typeof globalThis.crypto?.randomUUID !== 'function') {
    throw new Error('当前环境不支持安全生成 Idempotency-Key');
  }
  return `quick-create-${globalThis.crypto.randomUUID()}`;
}

function assertIntent(intent) {
  if (!intent?.idempotencyKey) {
    throw new TypeError('缺少有效 Quick Create 意图');
  }
}

function publicError(error) {
  return {
    status: Number(error?.status) || 0,
    code: String(error?.code || 'PROJECT_QUICK_CREATE_FAILED'),
    message: String(error?.message || '快速创建失败'),
    requestID: String(error?.requestID || ''),
    retryable: Boolean(error?.retryable)
  };
}
