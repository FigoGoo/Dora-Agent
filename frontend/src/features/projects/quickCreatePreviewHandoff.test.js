import { afterEach, describe, expect, it, vi } from 'vitest';

const PROJECT_A = '019f0000-0000-7000-8000-000000000041';
const PROJECT_B = '019f0000-0000-7000-8000-000000000042';

afterEach(() => {
  vi.restoreAllMocks();
  vi.resetModules();
});

describe('QuickCreate Preview volatile handoff', () => {
  it('is one-shot and never exposes one Project goal to another Project', async () => {
    const handoff = await freshHandoff();

    handoff.stageQuickCreatePreviewGoal(PROJECT_A, '只属于 Project A 的目标');

    expect(handoff.consumeQuickCreatePreviewGoal(PROJECT_B)).toBeNull();
    expect(handoff.consumeQuickCreatePreviewGoal(PROJECT_A)).toBe('只属于 Project A 的目标');
    expect(handoff.consumeQuickCreatePreviewGoal(PROJECT_A)).toBeNull();
  });

  it('disappears with a fresh module instance and never touches browser persistence or the URL', async () => {
    const localSet = vi.spyOn(Storage.prototype, 'setItem');
    const beforeURL = window.location.href;
    const handoff = await freshHandoff();

    handoff.stageQuickCreatePreviewGoal(PROJECT_A, '刷新后必须消失');

    expect(localSet).not.toHaveBeenCalled();
    expect(window.location.href).toBe(beforeURL);
    vi.resetModules();
    const afterHardRefresh = await import('./quickCreatePreviewHandoff.js');
    expect(afterHardRefresh.consumeQuickCreatePreviewGoal(PROJECT_A)).toBeNull();
  });
});

async function freshHandoff() {
  vi.resetModules();
  return import('./quickCreatePreviewHandoff.js');
}
