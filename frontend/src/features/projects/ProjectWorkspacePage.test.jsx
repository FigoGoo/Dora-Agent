import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import {
  projectBootstrapFixture,
  WORKSPACE_IDS,
  workspaceSnapshotFixture
} from '../../test/workspaceFixtures.js';
import { parseProjectBootstrap, ProjectWorkspacePage } from './ProjectWorkspacePage.jsx';

describe('ProjectWorkspacePage', () => {
  it('loads Project Bootstrap then Agent Snapshot and exposes the formal ready/live state', async () => {
    const bootstrap = vi.fn()
      .mockResolvedValueOnce(projectBootstrapFixture({ creation_status: 'provisioning', session_id: null }))
      .mockResolvedValueOnce(projectBootstrapFixture());
    const loadSnapshot = vi.fn().mockResolvedValue(workspaceSnapshotFixture());
    const openStream = vi.fn((options) => {
      options.onReady({ cursor: 1, latestSeq: 1, minAvailableSeq: 1 });
      return { close: vi.fn() };
    });

    const { container } = render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={bootstrap}
        loadSnapshot={loadSnapshot}
        openStream={openStream}
        retryDelays={[0, 0]}
      />
    );

    expect(await screen.findByText('工作台已就绪')).toBeInTheDocument();
    const workspace = container.querySelector('[data-workspace-state="ready"]');
    expect(workspace).toHaveAttribute('data-stream-state', 'live');
    expect(workspace).toHaveAttribute('data-project-id', WORKSPACE_IDS.project);
    expect(workspace).toHaveAttribute('data-session-id', WORKSPACE_IDS.session);
    expect(screen.getByText('还没有消息')).toBeInTheDocument();
    expect(openStream).toHaveBeenCalledWith(expect.objectContaining({ cursor: 1 }));
  });

  it('maps an exhausted provisioning window to offline and supports an explicit full retry', async () => {
    const user = userEvent.setup();
    const bootstrap = vi.fn()
      .mockResolvedValueOnce(projectBootstrapFixture({ creation_status: 'provisioning', session_id: null }))
      .mockResolvedValueOnce(projectBootstrapFixture());
    const openStream = vi.fn(() => ({ close: vi.fn() }));

    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={bootstrap}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotFixture())}
        openStream={openStream}
        retryDelays={[0]}
      />
    );

    expect(await screen.findByRole('alert')).toHaveTextContent('工作台仍在准备');
    expect(screen.getByRole('main')).toHaveAttribute('data-workspace-state', 'offline');
    await user.click(screen.getByRole('button', { name: '重新连接工作台' }));
    expect(await screen.findByText('工作台已就绪')).toBeInTheDocument();
    expect(bootstrap).toHaveBeenCalledTimes(2);
  });

  it('shows reconnecting and returns to live after a controlled transport disconnect', async () => {
    const streams = [];
    const scheduled = [];
    const close = vi.fn();
    const bootstrap = vi.fn().mockResolvedValue(projectBootstrapFixture());
    const loadSnapshot = vi.fn().mockResolvedValue(workspaceSnapshotFixture({ event_high_watermark: 2 }));
    const openStream = vi.fn((options) => {
      streams.push(options);
      return { close };
    });

    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={bootstrap}
        loadSnapshot={loadSnapshot}
        openStream={openStream}
        streamRetryDelays={[0]}
        schedule={(callback) => {
          scheduled.push(callback);
          return scheduled.length;
        }}
        cancelSchedule={vi.fn()}
      />
    );

    await waitFor(() => expect(streams).toHaveLength(1));
    act(() => streams[0].onReady({ cursor: 2, latestSeq: 2, minAvailableSeq: 1 }));
    expect(screen.getByRole('main')).toHaveAttribute('data-stream-state', 'live');

    act(() => streams[0].onTransportError());
    await waitFor(() => expect(loadSnapshot).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(scheduled).toHaveLength(1));
    expect(screen.getByRole('main')).toHaveAttribute('data-workspace-state', 'ready');
    expect(screen.getByRole('main')).toHaveAttribute('data-stream-state', 'reconnecting');
    expect(screen.getByRole('status')).toHaveTextContent('实时连接正在恢复…');
    expect(close).toHaveBeenCalledTimes(1);

    act(() => scheduled.shift()());
    await waitFor(() => expect(streams).toHaveLength(2));
    expect(streams[1].cursor).toBe(2);
    expect(screen.getByRole('main')).toHaveAttribute('data-stream-state', 'connecting');
    act(() => streams[1].onReady({ cursor: 2, latestSeq: 2, minAvailableSeq: 1 }));
    expect(screen.getByRole('main')).toHaveAttribute('data-stream-state', 'live');

    // 同一 Session 的旧连接即使延迟回调，也不能启动新的 reset 代次。
    act(() => streams[0].onReset({ reason: 'cursor_expired' }));
    await act(async () => Promise.resolve());
    expect(screen.getByRole('main')).toHaveAttribute('data-workspace-state', 'ready');
    expect(screen.getByRole('main')).toHaveAttribute('data-stream-state', 'live');
    expect(bootstrap).toHaveBeenCalledTimes(1);
    expect(loadSnapshot).toHaveBeenCalledTimes(2);
  });

  it('replaces the rendered projection from a full Snapshot after stream.reset', async () => {
    let resolveResetSnapshot;
    const resetSnapshot = new Promise((resolve) => {
      resolveResetSnapshot = resolve;
    });
    const streams = [];
    const bootstrap = vi.fn().mockResolvedValue(projectBootstrapFixture());
    const loadSnapshot = vi.fn()
      .mockResolvedValueOnce(snapshotWithMessage('旧 Snapshot 内容', 2))
      .mockReturnValueOnce(resetSnapshot);
    const openStream = vi.fn((options) => {
      streams.push(options);
      return { close: vi.fn() };
    });

    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={bootstrap}
        loadSnapshot={loadSnapshot}
        openStream={openStream}
      />
    );

    await waitFor(() => expect(streams).toHaveLength(1));
    act(() => streams[0].onReady({ cursor: 2, latestSeq: 2, minAvailableSeq: 1 }));
    expect(await screen.findByText('旧 Snapshot 内容')).toBeInTheDocument();

    act(() => streams[0].onReset({ reason: 'cursor_expired' }));
    expect(screen.getByRole('main')).toHaveAttribute('data-workspace-state', 'reset');
    expect(screen.getByText('正在同步最新工作台状态…')).toBeInTheDocument();
    // 完整回源提交前保留最后一次已验证投影，但保持 reset/只读状态。
    expect(screen.getByText('旧 Snapshot 内容')).toBeInTheDocument();
    await waitFor(() => expect(loadSnapshot).toHaveBeenCalledTimes(2));
    expect(bootstrap).toHaveBeenCalledTimes(2);

    await act(async () => resolveResetSnapshot(snapshotWithMessage('新 Snapshot 内容', 7)));
    await waitFor(() => expect(streams).toHaveLength(2));
    expect(streams[1].cursor).toBe(7);
    expect(screen.queryByText('旧 Snapshot 内容')).not.toBeInTheDocument();
    expect(screen.getByText('新 Snapshot 内容')).toBeInTheDocument();
    expect(screen.getByRole('main')).toHaveAttribute('data-stream-state', 'connecting');
    act(() => streams[1].onReady({ cursor: 7, latestSeq: 7, minAvailableSeq: 1 }));
    expect(screen.getByRole('main')).toHaveAttribute('data-stream-state', 'live');
  });

  it('ignores every old-generation stream callback after switching Project and Session', async () => {
    const secondProjectID = '019f0000-0000-7000-8000-000000000091';
    const secondSessionID = '019f0000-0000-7000-8000-000000000092';
    const streams = [];
    const closes = [];
    const bootstrap = vi.fn(async (projectID) => projectBootstrapFixture(projectID === secondProjectID ? {
      project_id: secondProjectID,
      session_id: secondSessionID
    } : {}));
    const loadSnapshot = vi.fn(async (sessionID) => sessionID === secondSessionID
      ? snapshotWithMessage('新 Session 内容', 3, { projectID: secondProjectID, sessionID: secondSessionID })
      : snapshotWithMessage('旧 Session 内容', 2));
    const openStream = vi.fn((options) => {
      const close = vi.fn();
      closes.push(close);
      streams.push(options);
      return { close };
    });

    const pageProps = { bootstrap, loadSnapshot, openStream };
    const { rerender } = render(
      <ProjectWorkspacePage projectID={WORKSPACE_IDS.project} {...pageProps} />
    );
    await waitFor(() => expect(streams).toHaveLength(1));
    act(() => streams[0].onReady({ cursor: 2, latestSeq: 2, minAvailableSeq: 1 }));
    expect(await screen.findByText('旧 Session 内容')).toBeInTheDocument();

    rerender(<ProjectWorkspacePage projectID={secondProjectID} {...pageProps} />);
    await waitFor(() => expect(streams).toHaveLength(2));
    act(() => streams[1].onReady({ cursor: 3, latestSeq: 3, minAvailableSeq: 1 }));
    expect(await screen.findByText('新 Session 内容')).toBeInTheDocument();
    expect(screen.queryByText('旧 Session 内容')).not.toBeInTheDocument();
    expect(screen.getByRole('main')).toHaveAttribute('data-project-id', secondProjectID);
    expect(screen.getByRole('main')).toHaveAttribute('data-session-id', secondSessionID);
    expect(screen.getByRole('main')).toHaveAttribute('data-stream-state', 'live');
    expect(closes[0]).toHaveBeenCalled();

    act(() => {
      streams[0].onReady({ cursor: 2, latestSeq: 99, minAvailableSeq: 99 });
      streams[0].onReset({ reason: 'cursor_expired' });
      streams[0].onProtocolError(new Error('late protocol failure'));
      streams[0].onTransportError();
    });
    await act(async () => Promise.resolve());
    expect(screen.getByText('新 Session 内容')).toBeInTheDocument();
    expect(screen.getByRole('main')).toHaveAttribute('data-workspace-state', 'ready');
    expect(screen.getByRole('main')).toHaveAttribute('data-stream-state', 'live');
    expect(bootstrap).toHaveBeenCalledTimes(2);
    expect(loadSnapshot).toHaveBeenCalledTimes(2);
    expect(streams).toHaveLength(2);
  });

  it.each([
    ['pending', '已受理'],
    ['claimed', '已领取'],
    ['running', '处理中'],
    ['retry_wait', '等待重试'],
    ['resolved', '已完成'],
    ['dead', '处理失败']
  ])('renders Snapshot Input status %s as %s', async (status, label) => {
    const openStream = vi.fn((options) => {
      options.onReady({ cursor: 2, latestSeq: 2, minAvailableSeq: 1 });
      return { close: vi.fn() };
    });

    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotFixture({
          messages: [{
            id: WORKSPACE_IDS.message,
            message_seq: 1,
            role: 'user',
            content: '创建一支短片',
            created_at: '2026-07-14T00:00:00.000000Z'
          }],
          inputs: [{
            id: WORKSPACE_IDS.input,
            message_id: WORKSPACE_IDS.message,
            source_type: 'user_message',
            status,
            enqueue_seq: 1,
            available_at: '2026-07-14T00:00:00.000000Z',
            created_at: '2026-07-14T00:00:00.000000Z',
            updated_at: '2026-07-14T00:00:00.000000Z'
          }],
          event_high_watermark: 2
        }))}
        openStream={openStream}
      />
    );

    expect(await screen.findByText(`输入 1：${label}`)).toBeInTheDocument();
  });

  it('aborts an in-flight formal Snapshot and closes the stream when unmounted', async () => {
    let capturedSignal;
    const loadSnapshot = vi.fn((_sessionID, { signal }) => {
      capturedSignal = signal;
      return new Promise(() => {});
    });
    const { unmount } = render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={loadSnapshot}
        openStream={vi.fn()}
      />
    );

    await waitFor(() => expect(capturedSignal).toBeDefined());
    unmount();
    expect(capturedSignal.aborted).toBe(true);
  });

  it.each([
    [401, 'unauthorized', '请先登录'],
    [404, 'not_found', '工作台不存在或不可访问']
  ])('maps Snapshot HTTP %s to %s without rendering stale data', async (status, state, heading) => {
    const error = Object.assign(new Error('resource failure'), { status });
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockRejectedValue(error)}
        openStream={vi.fn()}
      />
    );

    expect(await screen.findByRole('heading', { name: heading })).toBeInTheDocument();
    expect(screen.getByRole('main')).toHaveAttribute('data-workspace-state', state);
    expect(screen.queryByText(WORKSPACE_IDS.session)).not.toBeInTheDocument();
  });

  it('rejects drifted Project Binding and non-RFC3339 timestamps', () => {
    expect(() => parseProjectBootstrap(projectBootstrapFixture({
      project_id: '019f0000-0000-7000-8000-000000000099'
    }), WORKSPACE_IDS.project)).toThrow('错误的 project_id');
    expect(() => parseProjectBootstrap(projectBootstrapFixture({ updated_at: 'July 14, 2026' }), WORKSPACE_IDS.project))
      .toThrow('RFC3339');
  });
});

function snapshotWithMessage(content, eventHighWatermark, binding = {}) {
  const base = workspaceSnapshotFixture();
  return workspaceSnapshotFixture({
    session: {
      ...base.session,
      id: binding.sessionID || WORKSPACE_IDS.session,
      project_id: binding.projectID || WORKSPACE_IDS.project
    },
    messages: [{
      id: WORKSPACE_IDS.message,
      message_seq: 1,
      role: 'user',
      content,
      created_at: '2026-07-14T00:00:00.000000Z'
    }],
    event_high_watermark: eventHighWatermark
  });
}
