import { describe, expect, it } from 'vitest';
import {
  QUICK_CREATE_STATUS,
  createQuickCreateIntent,
  rejectQuickCreateIntent,
  replaceConflictedQuickCreateIntent,
  resolveQuickCreateIntent,
  submitQuickCreateIntent
} from './quickCreateIntent.js';

describe('Quick Create intent', () => {
  it('reuses one key across duplicate submit, provisioning and retry', () => {
    const original = createQuickCreateIntent('做一支短片', { keyFactory: () => 'idem-1' });
    const firstSubmit = submitQuickCreateIntent(original);
    const provisioning = resolveQuickCreateIntent(firstSubmit, {
      project_id: 'p1',
      creation_status: 'provisioning'
    });
    const retry = submitQuickCreateIntent(provisioning);

    expect(firstSubmit).toMatchObject({ idempotencyKey: 'idem-1', attempts: 1 });
    expect(provisioning).toMatchObject({
      idempotencyKey: 'idem-1',
      projectID: 'p1',
      status: QUICK_CREATE_STATUS.PROVISIONING
    });
    expect(retry).toMatchObject({ idempotencyKey: 'idem-1', attempts: 2 });
  });

  it('becomes created only after workspace bootstrap returns the session', () => {
    const intent = resolveQuickCreateIntent(
      resolveQuickCreateIntent(
        submitQuickCreateIntent(createQuickCreateIntent('', { keyFactory: () => 'idem-empty' })),
        { project_id: 'p-empty', creation_status: 'provisioning' }
      ),
      { project_id: 'p-empty', session_id: 's-empty', creation_status: 'ready' }
    );

    expect(intent).toMatchObject({
      projectID: 'p-empty',
      sessionID: 's-empty',
      status: QUICK_CREATE_STATUS.CREATED
    });
  });

  it('fails closed when ready is returned without the Agent-owned session id', () => {
    const intent = submitQuickCreateIntent(createQuickCreateIntent('', { keyFactory: () => 'idem-empty' }));

    expect(() => resolveQuickCreateIntent(intent, {
      project_id: 'p-empty',
      creation_status: 'ready'
    })).toThrow('ready 状态与 session_id 不一致');
  });

  it('keeps the original key on transport failure and only replaces it after a stable conflict', () => {
    const original = submitQuickCreateIntent(createQuickCreateIntent('A', { keyFactory: () => 'idem-1' }));
    const retryable = rejectQuickCreateIntent(original, { status: 503, code: 'UNAVAILABLE', retryable: true });

    expect(retryable).toMatchObject({ idempotencyKey: 'idem-1', status: QUICK_CREATE_STATUS.RETRYABLE_ERROR });
    expect(submitQuickCreateIntent(retryable).idempotencyKey).toBe('idem-1');
    expect(() => replaceConflictedQuickCreateIntent(retryable, 'B', { keyFactory: () => 'idem-2' }))
      .toThrow('IDEMPOTENCY_CONFLICT');

    const conflict = rejectQuickCreateIntent(original, { status: 409, code: 'IDEMPOTENCY_CONFLICT' });
    const replacement = replaceConflictedQuickCreateIntent(conflict, 'B', { keyFactory: () => 'idem-2' });
    expect(replacement).toMatchObject({ idempotencyKey: 'idem-2', prompt: 'B', attempts: 0 });
  });

  it('does not relabel a stable non-retryable failure as retryable', () => {
    const original = submitQuickCreateIntent(createQuickCreateIntent('A', { keyFactory: () => 'idem-1' }));
    const failed = rejectQuickCreateIntent(original, {
      status: 403,
      code: 'PROJECT_CREATE_FORBIDDEN',
      retryable: false
    });

    expect(failed).toMatchObject({ idempotencyKey: 'idem-1', status: QUICK_CREATE_STATUS.FAILED });
    expect(() => submitQuickCreateIntent(failed)).toThrow('不能重试');
  });
});
