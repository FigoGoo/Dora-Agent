import { normalizeCreationSpecPreviewIntent } from './creationSpecPreviewContract.js';

// createCreationSpecPreviewIntentLedger 对同一规范语义复用 UUIDv7，语义改变时立即换键。
export function createCreationSpecPreviewIntentLedger({ keyFactory = createPreviewUUIDV7 } = {}) {
  let current = null;
  return Object.freeze({
    claim(intent) {
      const body = normalizeCreationSpecPreviewIntent(intent);
      const semantic = JSON.stringify(body);
      if (current?.semantic === semantic) return current;
      const canonicalIntent = Object.freeze({
        goal: body.goal,
        deliverableType: body.deliverable_type,
        audience: Object.hasOwn(body, 'audience') ? body.audience : undefined,
        locale: body.locale,
        constraints: Object.freeze([...body.constraints])
      });
      current = Object.freeze({ semantic, key: keyFactory(), body: Object.freeze(body), intent: canonicalIntent });
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

// createPreviewUUIDV7 使用 48-bit Unix 毫秒时间与安全随机数生成规范 UUIDv7。
export function createPreviewUUIDV7({ now = Date.now, randomValues = defaultRandomValues } = {}) {
  const timestamp = Number(now());
  if (!Number.isSafeInteger(timestamp) || timestamp < 0 || timestamp > 0xffffffffffff) {
    throw new Error('无法生成 Creation Spec Preview UUIDv7 时间戳');
  }
  const bytes = randomValues(new Uint8Array(16));
  if (!(bytes instanceof Uint8Array) || bytes.length !== 16) {
    throw new Error('无法生成 Creation Spec Preview UUIDv7 随机数');
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

function defaultRandomValues(bytes) {
  if (typeof globalThis.crypto?.getRandomValues !== 'function') {
    throw new Error('当前环境不支持安全生成 Creation Spec Preview Idempotency-Key');
  }
  return globalThis.crypto.getRandomValues(bytes);
}
