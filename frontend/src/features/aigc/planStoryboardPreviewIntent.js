import {
  isPlanStoryboardPreviewUUIDV7,
  normalizePlanStoryboardPreviewEnqueueRequest
} from './planStoryboardPreviewContract.js';

// createPlanStoryboardPreviewIntentLedger 对同一资源快照与同一规范 Tool Intent 复用 UUIDv7 幂等键。
export function createPlanStoryboardPreviewIntentLedger({ keyFactory = createPlanStoryboardPreviewUUIDV7 } = {}) {
  let current = null;
  return Object.freeze({
    claim(request) {
      const body = normalizePlanStoryboardPreviewEnqueueRequest(request);
      const semantic = JSON.stringify(body);
      if (current?.semantic === semantic) return current;
      const key = keyFactory();
      if (!isPlanStoryboardPreviewUUIDV7(key)) {
        throw new Error('无法生成规范的 Plan Storyboard Preview Idempotency-Key');
      }
      const frozenBody = freezeWireRequest(body);
      current = Object.freeze({
        semantic,
        key,
        body: frozenBody,
        creationSpecRef: Object.freeze({
          id: frozenBody.creation_spec_ref.id,
          version: frozenBody.creation_spec_ref.version,
          contentDigest: frozenBody.creation_spec_ref.content_digest
        }),
        toolIntent: Object.freeze({
          planningInstruction: frozenBody.tool_intent.planning_instruction,
          targetDurationSeconds: frozenBody.tool_intent.target_duration_seconds
        })
      });
      return current;
    },
    current() {
      return current;
    },
    clear() {
      current = null;
    }
  });
}

// createPlanStoryboardPreviewUUIDV7 使用 Unix 毫秒时间与安全随机数生成规范 UUIDv7。
export function createPlanStoryboardPreviewUUIDV7({ now = Date.now, randomValues = defaultRandomValues } = {}) {
  const timestamp = Number(now());
  if (!Number.isSafeInteger(timestamp) || timestamp < 0 || timestamp > 0xffffffffffff) {
    throw new Error('无法生成 Plan Storyboard Preview UUIDv7 时间戳');
  }
  const bytes = randomValues(new Uint8Array(16));
  if (!(bytes instanceof Uint8Array) || bytes.length !== 16) {
    throw new Error('无法生成 Plan Storyboard Preview UUIDv7 随机数');
  }
  let remaining = timestamp;
  for (let index = 5; index >= 0; index -= 1) {
    bytes[index] = remaining & 0xff;
    remaining = Math.floor(remaining / 256);
  }
  bytes[6] = 0x70 | (bytes[6] & 0x0f);
  bytes[8] = 0x80 | (bytes[8] & 0x3f);
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function freezeWireRequest(body) {
  const toolIntent = { ...body.tool_intent };
  return Object.freeze({
    schema_version: body.schema_version,
    creation_spec_ref: Object.freeze({ ...body.creation_spec_ref }),
    tool_intent: Object.freeze(toolIntent)
  });
}

function defaultRandomValues(bytes) {
  if (typeof globalThis.crypto?.getRandomValues !== 'function') {
    throw new Error('当前环境不支持安全生成 Plan Storyboard Preview Idempotency-Key');
  }
  return globalThis.crypto.getRandomValues(bytes);
}
