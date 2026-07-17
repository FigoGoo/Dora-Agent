import { describe, expect, it, vi } from 'vitest';
import { createCreationSpecPreviewIntentLedger, createPreviewUUIDV7 } from './creationSpecPreviewIntent.js';

describe('Creation Spec Preview intent ledger', () => {
  it('reuses one key for the same normalized semantic and replaces it after a semantic edit', () => {
    const keyFactory = vi.fn()
      .mockReturnValueOnce('019f0000-0000-7000-8000-000000000021')
      .mockReturnValueOnce('019f0000-0000-7000-8000-000000000022');
    const ledger = createCreationSpecPreviewIntentLedger({ keyFactory });
    const first = ledger.claim(intent('  同一目标  '));
    const replay = ledger.claim(intent('同一目标'));
    const changed = ledger.claim(intent('新的目标'));
    expect(replay.key).toBe(first.key);
    expect(changed.key).not.toBe(first.key);
    expect(keyFactory).toHaveBeenCalledTimes(2);
  });

  it('generates a canonical UUIDv7 with timestamp, version and variant bits', () => {
    const id = createPreviewUUIDV7({
      now: () => 1,
      randomValues: (bytes) => bytes.fill(0)
    });
    expect(id).toBe('00000000-0001-7000-8000-000000000000');
  });
});

function intent(goal) {
  return { goal, deliverableType: 'video', audience: '', locale: 'zh-CN', constraints: [] };
}
