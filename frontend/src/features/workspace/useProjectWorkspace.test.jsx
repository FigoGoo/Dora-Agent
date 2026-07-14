import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import {
  messageFixture,
  projectBootstrapFixture,
  WORKSPACE_IDS,
  workspaceSnapshotFixture
} from '../../test/workspaceFixtures.js';
import { useProjectWorkspace } from './useProjectWorkspace.js';

const ZERO_DELAYS = Object.freeze([0]);
const immediateSchedule = (callback) => {
  queueMicrotask(callback);
  return 1;
};
const noopCancel = () => {};

describe('useProjectWorkspace', () => {
  it('commits one validated Snapshot before opening SSE from its high watermark', async () => {
    const calls = [];
    const openStream = vi.fn((options) => {
      calls.push(`stream:${options.cursor}`);
      options.onReady({ cursor: options.cursor, latestSeq: options.cursor, minAvailableSeq: 1 });
      return { close: vi.fn() };
    });
    const { result } = renderWorkspaceHook({
      bootstrap: vi.fn(async () => {
        calls.push('bootstrap');
        return projectBootstrapFixture();
      }),
      loadSnapshot: vi.fn(async () => {
        calls.push('snapshot');
        return workspaceSnapshotFixture({ event_high_watermark: 2 });
      }),
      openStream
    });

    await waitFor(() => expect(result.current.state.streamState).toBe('live'));
    expect(calls).toEqual(['bootstrap', 'snapshot', 'stream:2']);
    expect(result.current.state).toMatchObject({ kind: 'ready', cursor: 2 });
  });

  it('probes with the same Snapshot API before reconnecting from the last confirmed cursor', async () => {
    const streams = [];
    const loadSnapshot = vi.fn().mockResolvedValue(workspaceSnapshotFixture({ event_high_watermark: 2 }));
    const { result } = renderWorkspaceHook({
      loadSnapshot,
      openStream: vi.fn((options) => {
        streams.push(options);
        return { close: vi.fn() };
      })
    });
    await waitFor(() => expect(streams).toHaveLength(1));

    await act(async () => streams[0].onTransportError());
    await waitFor(() => expect(loadSnapshot).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(streams).toHaveLength(2));
    expect(streams[1].cursor).toBe(2);
    expect(result.current.state.streamState).toBe('connecting');
  });

  it.each([
    ['expired cursor', workspaceSnapshotFixture({ event_high_watermark: 5, min_available_seq: 3 })],
    ['regressed high watermark', workspaceSnapshotFixture({ event_high_watermark: 1, min_available_seq: 1 })]
  ])('enters reset when the Snapshot probe reports %s', async (_label, probeSnapshot) => {
    const streams = [];
    const never = new Promise(() => {});
    const loadSnapshot = vi.fn()
      .mockResolvedValueOnce(workspaceSnapshotFixture({ event_high_watermark: 2 }))
      .mockResolvedValueOnce(probeSnapshot)
      .mockReturnValueOnce(never);
    const bootstrap = vi.fn().mockResolvedValue(projectBootstrapFixture());
    const { result } = renderWorkspaceHook({
      bootstrap,
      loadSnapshot,
      openStream: vi.fn((options) => {
        streams.push(options);
        return { close: vi.fn() };
      })
    });
    await waitFor(() => expect(streams).toHaveLength(1));

    await act(async () => streams[0].onTransportError());
    await waitFor(() => expect(loadSnapshot).toHaveBeenCalledTimes(3));
    await waitFor(() => expect(result.current.state.kind).toBe('reset'));
    expect(result.current.state.snapshot).not.toBeNull();
    expect(bootstrap).toHaveBeenCalledTimes(2);
    expect(streams).toHaveLength(1);
  });

  it('keeps one reconnect budget across repeated pre-ready connection failures', async () => {
    const streams = [];
    const loadSnapshot = vi.fn().mockResolvedValue(workspaceSnapshotFixture({ event_high_watermark: 2 }));
    const { result } = renderWorkspaceHook({
      loadSnapshot,
      streamRetryDelays: Object.freeze([0, 0]),
      openStream: vi.fn((options) => {
        streams.push(options);
        return { close: vi.fn() };
      })
    });
    await waitFor(() => expect(streams).toHaveLength(1));

    await act(async () => streams[0].onTransportError());
    await waitFor(() => expect(streams).toHaveLength(2));
    await act(async () => streams[1].onTransportError());
    await waitFor(() => expect(streams).toHaveLength(3));
    await act(async () => streams[2].onTransportError());

    await waitFor(() => expect(result.current.state.kind).toBe('offline'));
    expect(streams).toHaveLength(3);
    expect(loadSnapshot).toHaveBeenCalledTimes(4);
  });

  it('resets when stream.ready has not caught up exactly to the local cursor', async () => {
    const streams = [];
    const never = new Promise(() => {});
    const loadSnapshot = vi.fn()
      .mockResolvedValueOnce(workspaceSnapshotFixture({ event_high_watermark: 2 }))
      .mockReturnValueOnce(never);
    const { result } = renderWorkspaceHook({
      loadSnapshot,
      openStream: vi.fn((options) => {
        streams.push(options);
        return { close: vi.fn() };
      })
    });
    await waitFor(() => expect(streams).toHaveLength(1));

    act(() => streams[0].onReady({ cursor: 2, latestSeq: 3, minAvailableSeq: 1 }));
    await waitFor(() => expect(result.current.state.kind).toBe('reset'));
    expect(streams).toHaveLength(1);
  });

  it('aborts the old Snapshot and ignores its late completion after the new Project is live', async () => {
    const pending = [];
    const loadSnapshot = vi.fn((_sessionID, { signal }) => new Promise((resolve) => pending.push({ resolve, signal })));
    const streams = [];
    const options = {
      projectID: WORKSPACE_IDS.project,
      bootstrap: vi.fn().mockResolvedValue(projectBootstrapFixture()),
      loadSnapshot,
      openStream: vi.fn((streamOptions) => {
        streams.push(streamOptions);
        return { close: vi.fn() };
      }),
      projectRetryDelays: ZERO_DELAYS,
      streamRetryDelays: ZERO_DELAYS,
      schedule: immediateSchedule,
      cancelSchedule: noopCancel
    };
    const { result, rerender } = renderHook((props) => useProjectWorkspace(props), { initialProps: options });
    await waitFor(() => expect(pending).toHaveLength(1));
    const secondProject = '019f0000-0000-7000-8000-000000000099';
    const secondSession = '019f0000-0000-7000-8000-000000000098';
    rerender({
      ...options,
      projectID: secondProject,
      bootstrap: vi.fn().mockResolvedValue(projectBootstrapFixture({
        project_id: secondProject,
        session_id: secondSession
      }))
    });
    await waitFor(() => expect(pending).toHaveLength(2));
    expect(pending[0].signal.aborted).toBe(true);

    const baseSnapshot = workspaceSnapshotFixture();
    await act(async () => pending[1].resolve(workspaceSnapshotFixture({
      session: {
        ...baseSnapshot.session,
        id: secondSession,
        project_id: secondProject
      },
      messages: [messageFixture({ content: '新 Project Snapshot' })]
    })));
    await waitFor(() => expect(streams).toHaveLength(1));
    act(() => streams[0].onReady({ cursor: 1, latestSeq: 1, minAvailableSeq: 1 }));
    await waitFor(() => expect(result.current.state.streamState).toBe('live'));
    expect(result.current.state.project.projectID).toBe(secondProject);
    expect(result.current.state.snapshot.session.id).toBe(secondSession);
    expect(result.current.state.snapshot.messages[0].content).toBe('新 Project Snapshot');
    expect(streams[0]).toMatchObject({ projectID: secondProject, sessionID: secondSession, cursor: 1 });

    // 旧请求故意忽略 Abort 并迟到成功，也不能提交旧投影或建立旧流。
    await act(async () => {
      pending[0].resolve(workspaceSnapshotFixture({
        messages: [messageFixture({ content: '迟到的旧 Project Snapshot' })]
      }));
      await Promise.resolve();
    });
    expect(result.current.state.project.projectID).toBe(secondProject);
    expect(result.current.state.snapshot.session.id).toBe(secondSession);
    expect(result.current.state.snapshot.messages[0].content).toBe('新 Project Snapshot');
    expect(result.current.state.streamState).toBe('live');
    expect(streams).toHaveLength(1);
  });
});

function renderWorkspaceHook(overrides = {}) {
  const options = {
    projectID: WORKSPACE_IDS.project,
    bootstrap: vi.fn().mockResolvedValue(projectBootstrapFixture()),
    loadSnapshot: vi.fn().mockResolvedValue(workspaceSnapshotFixture()),
    openStream: vi.fn(() => ({ close: vi.fn() })),
    projectRetryDelays: ZERO_DELAYS,
    streamRetryDelays: ZERO_DELAYS,
    schedule: immediateSchedule,
    cancelSchedule: noopCancel,
    ...overrides
  };
  return renderHook(() => useProjectWorkspace(options));
}
