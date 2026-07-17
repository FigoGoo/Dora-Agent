import { describe, expect, it, vi } from 'vitest';
import {
  createPlanStoryboardPreviewIntentLedger,
  createPlanStoryboardPreviewUUIDV7
} from './planStoryboardPreviewIntent.js';

const IDS = Object.freeze({
  creationSpec: '019f0000-0000-7000-8000-000000000008',
  firstKey: '019f0000-0000-7000-8000-000000000021',
  secondKey: '019f0000-0000-7000-8000-000000000022',
  thirdKey: '019f0000-0000-7000-8000-000000000023'
});

describe('Plan Storyboard Preview intent ledger', () => {
  it('reuses a key for the same normalized resource snapshot and Tool Intent', () => {
    const keyFactory = vi.fn()
      .mockReturnValueOnce(IDS.firstKey)
      .mockReturnValueOnce(IDS.secondKey);
    const ledger = createPlanStoryboardPreviewIntentLedger({ keyFactory });

    const first = ledger.claim(request('  规划三段式故事板  ', '60'));
    const replay = ledger.claim(request('规划三段式故事板', 60));
    const changed = ledger.claim(request('规划四段式故事板', 60));

    expect(replay).toBe(first);
    expect(replay.key).toBe(IDS.firstKey);
    expect(changed.key).toBe(IDS.secondKey);
    expect(keyFactory).toHaveBeenCalledTimes(2);
    expect(first.body).toEqual({
      schema_version: 'plan_storyboard.preview.enqueue-request.v1',
      creation_spec_ref: {
        id: IDS.creationSpec,
        version: 1,
        content_digest: 'a'.repeat(64)
      },
      tool_intent: {
        schema_version: 'plan_storyboard.preview.intent.v1',
        planning_instruction: '规划三段式故事板',
        target_duration_seconds: 60
      }
    });
    expect(Object.isFrozen(first.body)).toBe(true);
    expect(Object.isFrozen(first.body.creation_spec_ref)).toBe(true);
    expect(Object.isFrozen(first.body.tool_intent)).toBe(true);
  });

  it('creates a new key when trusted CreationSpec provenance changes and can be cleared', () => {
    const keyFactory = vi.fn()
      .mockReturnValueOnce(IDS.firstKey)
      .mockReturnValueOnce(IDS.secondKey)
      .mockReturnValueOnce(IDS.thirdKey);
    const ledger = createPlanStoryboardPreviewIntentLedger({ keyFactory });
    const first = ledger.claim(request('规划故事板', ''));
    const changedDigest = ledger.claim({
      ...request('规划故事板', ''),
      creationSpecRef: { id: IDS.creationSpec, version: 1, contentDigest: 'b'.repeat(64) }
    });
    ledger.clear();
    const afterClear = ledger.claim({
      ...request('规划故事板', ''),
      creationSpecRef: { id: IDS.creationSpec, version: 1, contentDigest: 'b'.repeat(64) }
    });

    expect(changedDigest.key).not.toBe(first.key);
    expect(afterClear.key).not.toBe(changedDigest.key);
    expect(ledger.current()).toBe(afterClear);
  });

  it('rejects a non-UUIDv7 key from the injected key factory', () => {
    const ledger = createPlanStoryboardPreviewIntentLedger({ keyFactory: () => 'unsafe-key' });
    expect(() => ledger.claim(request('规划故事板', 30))).toThrow('Idempotency-Key');
  });

  it('generates a canonical UUIDv7 with timestamp, version and variant bits', () => {
    const id = createPlanStoryboardPreviewUUIDV7({
      now: () => 1,
      randomValues: (bytes) => bytes.fill(0)
    });
    expect(id).toBe('00000000-0001-7000-8000-000000000000');
  });
});

function request(planningInstruction, targetDurationSeconds) {
  return {
    creationSpecRef: { id: IDS.creationSpec, version: 1, contentDigest: 'a'.repeat(64) },
    toolIntent: { planningInstruction, targetDurationSeconds }
  };
}
