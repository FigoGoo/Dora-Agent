import {
  isWritePromptsPreviewUUIDV7,
  normalizeWritePromptsPreviewEnqueueRequest
} from './writePromptsPreviewContract.js';

// createWritePromptsPreviewIntentLedger 对同一 Source 快照与规范 Intent 复用 UUIDv7 幂等键。
export function createWritePromptsPreviewIntentLedger({ keyFactory = createWritePromptsPreviewUUIDV7 } = {}) {
  let current = null;
  return Object.freeze({
    claim(request) {
      const body = normalizeWritePromptsPreviewEnqueueRequest(request);
      const semantic = JSON.stringify(body);
      if (current?.semantic === semantic) return current;
      const key = keyFactory();
      if (!isWritePromptsPreviewUUIDV7(key)) throw new Error('无法生成规范的 Write Prompts Preview Idempotency-Key');
      const frozenBody = freezeWireRequest(body);
      current = Object.freeze({
        semantic,
        key,
        body: frozenBody,
        storyboardPreviewRef: Object.freeze({
          id: frozenBody.storyboard_preview_ref.id,
          version: frozenBody.storyboard_preview_ref.version,
          contentDigest: frozenBody.storyboard_preview_ref.content_digest
        }),
        toolIntent: Object.freeze({
          writingInstruction: frozenBody.tool_intent.writing_instruction,
          outputLanguage: frozenBody.tool_intent.output_language
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

// createWritePromptsPreviewUUIDV7 使用 Unix 毫秒时间与安全随机数生成规范 UUIDv7。
export function createWritePromptsPreviewUUIDV7({ now = Date.now, randomValues = defaultRandomValues } = {}) {
  const timestamp = Number(now());
  if (!Number.isSafeInteger(timestamp) || timestamp < 0 || timestamp > 0xffffffffffff) {
    throw new Error('无法生成 Write Prompts Preview UUIDv7 时间戳');
  }
  const bytes = randomValues(new Uint8Array(16));
  if (!(bytes instanceof Uint8Array) || bytes.length !== 16) {
    throw new Error('无法生成 Write Prompts Preview UUIDv7 随机数');
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
  return Object.freeze({
    schema_version: body.schema_version,
    storyboard_preview_ref: Object.freeze({ ...body.storyboard_preview_ref }),
    tool_intent: Object.freeze({ ...body.tool_intent })
  });
}

function defaultRandomValues(bytes) {
  if (typeof globalThis.crypto?.getRandomValues !== 'function') {
    throw new Error('当前环境不支持安全生成 Write Prompts Preview Idempotency-Key');
  }
  return globalThis.crypto.getRandomValues(bytes);
}
