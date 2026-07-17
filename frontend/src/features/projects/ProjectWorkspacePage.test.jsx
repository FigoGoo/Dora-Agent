import { StrictMode } from 'react';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import {
  analyzeMaterialsPreviewCardFixture,
  creationSpecPreviewCardFixture,
  directResponseCardFixture,
  projectBootstrapFixture,
  promptPreviewCardFixture,
  storyboardAcceptedEventFixture,
  storyboardEventFixture,
  storyboardPreviewCardFixture,
  WORKSPACE_IDS,
  workspaceSnapshotFixture,
  workspaceSnapshotV3Fixture,
  workspaceSnapshotV4Fixture,
  messageFixture,
  inputFixture,
  turnFailureCardFixture
} from '../../test/workspaceFixtures.js';
import { toolCatalogFixture } from '../../test/toolCatalogFixtures.js';
import { parseToolCatalogResponse } from '../tools/toolCatalogContract.js';
import { parseProjectBootstrap, ProjectWorkspacePage } from './ProjectWorkspacePage.jsx';
import { stageQuickCreatePreviewGoal } from './quickCreatePreviewHandoff.js';
import { parsePersistentWorkspaceEvent } from '../workspace/workspaceContract.js';

describe('ProjectWorkspacePage', () => {
  it('consumes only the matching QuickCreate handoff into the Preview goal field', async () => {
    stageQuickCreatePreviewGoal(WORKSPACE_IDS.project, '从 QuickCreate 进入的目标');
    const enqueue = vi.fn();

    render(
      <StrictMode>
        <ProjectWorkspacePage
          projectID={WORKSPACE_IDS.project}
          creationSpecPreviewEnabled
          bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
          loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotFixture())}
          loadToolCatalog={vi.fn().mockResolvedValue(parseToolCatalogResponse(toolCatalogFixture()))}
          openStream={vi.fn(() => ({ close: vi.fn() }))}
          enqueueCreationSpecPreview={enqueue}
        />
      </StrictMode>
    );

    expect(await screen.findByLabelText('创作目标')).toHaveValue('从 QuickCreate 进入的目标');
    expect(enqueue).not.toHaveBeenCalled();
  });

  it('loads Project Bootstrap then Agent Snapshot and exposes the formal ready/live state', async () => {
    const bootstrap = vi.fn()
      .mockResolvedValueOnce(projectBootstrapFixture({ creation_status: 'provisioning', session_id: null }))
      .mockResolvedValueOnce(projectBootstrapFixture());
    const loadSnapshot = vi.fn().mockResolvedValue(workspaceSnapshotFixture());
    const loadToolCatalog = vi.fn().mockResolvedValue(parseToolCatalogResponse(toolCatalogFixture()));
    const openStream = vi.fn((options) => {
      options.onReady({ cursor: 1, latestSeq: 1, minAvailableSeq: 1 });
      return { close: vi.fn() };
    });

    const { container } = render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        creationSpecPreviewEnabled
        bootstrap={bootstrap}
        loadSnapshot={loadSnapshot}
        loadToolCatalog={loadToolCatalog}
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
    const catalog = await screen.findByRole('region', { name: '工具目录' });
    expect(await within(catalog).findAllByRole('listitem')).toHaveLength(6);
    expect(within(catalog).getAllByText('设计评审中')).toHaveLength(6);
    expect(loadToolCatalog).toHaveBeenCalledWith(WORKSPACE_IDS.session, { signal: expect.any(AbortSignal) });
    expect(openStream).toHaveBeenCalledWith(expect.objectContaining({ cursor: 1 }));
    expect(screen.getByRole('form', { name: '创建目标预览' })).toBeInTheDocument();
  });

  it('renders Direct Response as a Card and open_toolbox only focuses the catalog', async () => {
    const user = userEvent.setup();
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotFixture({
          messages: [messageFixture()],
          inputs: [inputFixture({ status: 'resolved' })],
          latest_turn_output: directResponseCardFixture(),
          event_high_watermark: 3
        }))}
        loadToolCatalog={vi.fn().mockResolvedValue(parseToolCatalogResponse(toolCatalogFixture()))}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );
    expect(await screen.findByRole('heading', { name: '需求已接收' })).toBeInTheDocument();
    const toolbox = await screen.findByRole('region', { name: '工具目录' });
    await user.click(screen.getByRole('button', { name: '打开工具箱' }));
    expect(toolbox).toHaveFocus();
    expect(screen.getAllByRole('listitem')).toHaveLength(8);
  });

  it('restores a read-only Analyze Materials Card without exposing an entry form', async () => {
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotFixture({
          inputs: [inputFixture({ source_type: 'analyze_materials_preview', message_id: null, status: 'resolved' })],
          analyze_materials_preview: analyzeMaterialsPreviewCardFixture(), event_high_watermark: 3
        }))}
        loadToolCatalog={vi.fn().mockResolvedValue(parseToolCatalogResponse(toolCatalogFixture()))}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );
    expect(await screen.findByRole('heading', { name: '素材分析已完成' })).toBeInTheDocument();
    expect(screen.getByText(/开发预览 · 非权威结果/)).toBeInTheDocument();
    expect(screen.queryByRole('form', { name: /素材分析/ })).not.toBeInTheDocument();
    const catalog = await screen.findByRole('region', { name: '工具目录' });
    expect(within(catalog).getAllByText('设计评审中')).toHaveLength(6);
  });

  it('enables the real text material picker only behind the analyzeMaterials runtime capability', async () => {
    const loadTextMaterials = vi.fn().mockResolvedValue({
      items: [{
        assetID: WORKSPACE_IDS.asset,
        assetVersion: 1,
        mediaType: 'text',
        status: 'ready',
        content: '正式 Project 内的文本素材',
        createdAt: '2026-07-17T10:00:00Z',
        createdAtMs: Date.parse('2026-07-17T10:00:00Z')
      }],
      requestID: WORKSPACE_IDS.request
    });
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        analyzeMaterialsPreviewEnabled
        loadTextMaterials={loadTextMaterials}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotFixture())}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );
    expect(await screen.findByRole('form', { name: '创建文本素材' })).toBeInTheDocument();
    expect(screen.getByRole('form', { name: '分析文本素材' })).toBeInTheDocument();
    expect(await screen.findByText('正式 Project 内的文本素材')).toBeInTheDocument();
    expect(loadTextMaterials).toHaveBeenCalledWith(expect.objectContaining({
      projectID: WORKSPACE_IDS.project,
      signal: expect.any(AbortSignal)
    }));
  });

  it('binds the Storyboard form to the current parsed Creation Spec Card and same-origin BFF command', async () => {
    const user = userEvent.setup();
    const enqueue = vi.fn().mockResolvedValue({
      requestID: WORKSPACE_IDS.request,
      inputID: WORKSPACE_IDS.storyboardInput,
      turnID: WORKSPACE_IDS.storyboardTurn,
      runID: WORKSPACE_IDS.storyboardRun,
      toolCallID: WORKSPACE_IDS.storyboardToolCall,
      status: 'pending',
      replayed: false
    });
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        planStoryboardPreviewEnabled
        planStoryboardPreviewCsrfToken="csrf-storyboard"
        planStoryboardPreviewKeyFactory={() => WORKSPACE_IDS.request}
        enqueuePlanStoryboardPreview={enqueue}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotV3Fixture({
          creation_spec_preview: creationSpecPreviewCardFixture()
        }))}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );

    await user.type(await screen.findByLabelText('故事板规划要求'), '按开场和演示规划故事板');
    await user.click(screen.getByRole('button', { name: '生成故事板开发预览' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledWith(expect.objectContaining({
      sessionID: WORKSPACE_IDS.session,
      csrfToken: 'csrf-storyboard',
      idempotencyKey: WORKSPACE_IDS.request,
      creationSpecRef: {
        id: WORKSPACE_IDS.creationSpec,
        version: 1,
        contentDigest: 'a'.repeat(64)
      },
      toolIntent: {
        planningInstruction: '按开场和演示规划故事板',
        targetDurationSeconds: undefined
      }
    })));
    expect(screen.queryByDisplayValue(WORKSPACE_IDS.creationSpec)).not.toBeInTheDocument();
  });

  it('keeps the Storyboard entry flag-off while restoring a strict read-only hard-refresh Card', async () => {
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotV3Fixture({
          inputs: [inputFixture({
            id: WORKSPACE_IDS.storyboardInput,
            source_type: 'plan_storyboard_preview',
            message_id: null,
            status: 'resolved'
          })],
          creation_spec_preview: creationSpecPreviewCardFixture(),
          plan_storyboard_preview: storyboardPreviewCardFixture(),
          event_high_watermark: 3
        }))}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );

    expect(await screen.findByRole('heading', { name: '新品短片故事板' })).toBeInTheDocument();
    expect(screen.getByText('开发预览 · 隔离 JSON Draft · 未激活/未扣费')).toBeInTheDocument();
    expect(screen.queryByRole('form', { name: /故事板规划要求/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '生成故事板开发预览' })).not.toBeInTheDocument();
  });

  it('renders the same strict Storyboard Card after accepted and terminal SSE events', async () => {
    let stream;
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotV3Fixture({
          inputs: [inputFixture({
            id: WORKSPACE_IDS.storyboardInput,
            source_type: 'plan_storyboard_preview',
            message_id: null,
            status: 'pending'
          })]
        }))}
        openStream={vi.fn((options) => {
          stream = options;
          return { close: vi.fn() };
        })}
      />
    );
    await waitFor(() => expect(stream).toBeDefined());
    act(() => {
      stream.onEvent(parsePersistentWorkspaceEvent({
        type: 'plan_storyboard.preview.accepted',
        lastEventId: '2',
        data: JSON.stringify(storyboardAcceptedEventFixture())
      }, { expectedProjectID: WORKSPACE_IDS.project, expectedSessionID: WORKSPACE_IDS.session }));
      stream.onEvent(parsePersistentWorkspaceEvent({
        type: 'plan_storyboard.preview.completed',
        lastEventId: '3',
        data: JSON.stringify(storyboardEventFixture())
      }, { expectedProjectID: WORKSPACE_IDS.project, expectedSessionID: WORKSPACE_IDS.session }));
    });

    expect(await screen.findByRole('heading', { name: '新品短片故事板' })).toBeInTheDocument();
    expect(screen.getByText('输入 1：已完成')).toBeInTheDocument();
  });

  it('shows the Write Prompts form only for a current Storyboard Card with at least one Slot and auto-binds its ref', async () => {
    const user = userEvent.setup();
    const enqueue = vi.fn().mockResolvedValue({
      requestID: WORKSPACE_IDS.request,
      inputID: WORKSPACE_IDS.promptInput,
      turnID: WORKSPACE_IDS.promptTurn,
      runID: WORKSPACE_IDS.promptRun,
      toolCallID: WORKSPACE_IDS.promptToolCall,
      status: 'pending',
      replayed: false
    });
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        writePromptsPreviewEnabled
        writePromptsPreviewCsrfToken="csrf-write-prompts"
        writePromptsPreviewKeyFactory={() => WORKSPACE_IDS.request}
        enqueueWritePromptsPreview={enqueue}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotV4Fixture({
          inputs: [inputFixture({
            id: WORKSPACE_IDS.storyboardInput,
            source_type: 'plan_storyboard_preview',
            message_id: null,
            status: 'resolved'
          })],
          plan_storyboard_preview: storyboardPreviewCardFixture()
        }))}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );
    await user.type(await screen.findByLabelText('提示词写作要求'), '为全部槽位编写提示词');
    await user.selectOptions(screen.getByLabelText('输出语言（可选）'), 'zh-CN');
    await user.click(screen.getByRole('button', { name: '生成提示词开发预览' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledWith(expect.objectContaining({
      sessionID: WORKSPACE_IDS.session,
      csrfToken: 'csrf-write-prompts',
      storyboardPreviewRef: {
        id: WORKSPACE_IDS.storyboardPreview,
        version: 1,
        contentDigest: 'b'.repeat(64)
      },
      toolIntent: { writingInstruction: '为全部槽位编写提示词', outputLanguage: 'zh-CN' }
    })));
    expect(screen.queryByDisplayValue(WORKSPACE_IDS.storyboardPreview)).not.toBeInTheDocument();
  });

  it('keeps the Write Prompts entry flag-off while restoring the same strict hard-refresh Card', async () => {
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotV4Fixture({
          inputs: [
            inputFixture({
              id: WORKSPACE_IDS.storyboardInput,
              source_type: 'plan_storyboard_preview',
              message_id: null,
              status: 'resolved'
            }),
            inputFixture({
              id: WORKSPACE_IDS.promptInput,
              source_type: 'write_prompts_preview',
              message_id: null,
              enqueue_seq: 2,
              status: 'resolved'
            })
          ],
          plan_storyboard_preview: storyboardPreviewCardFixture(),
          write_prompts_preview: promptPreviewCardFixture(),
          event_high_watermark: 5
        }))}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );
    expect(await screen.findByRole('heading', { name: '媒体提示词预览' })).toBeInTheDocument();
    expect(screen.getByText('开发预览 · 隔离 Prompt Draft · 未审核/未扣费/不可生成媒体')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '生成提示词开发预览' })).not.toBeInTheDocument();
  });

  it.each([
    ['failed', '处理未完成'],
    ['recovery_pending', '正在恢复处理']
  ])('renders safe %s Failure output without an action', async (status, heading) => {
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotFixture({
          messages: [messageFixture()],
          inputs: [inputFixture({ status: status === 'failed' ? 'dead' : 'recovery_pending' })],
          latest_turn_output: turnFailureCardFixture({ status }),
          event_high_watermark: 3
        }))}
        loadToolCatalog={vi.fn().mockResolvedValue(parseToolCatalogResponse(toolCatalogFixture()))}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );
    expect(await screen.findByRole('heading', { name: heading })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '打开工具箱' })).not.toBeInTheDocument();
  });

  it('does not expose the Preview form unless the local frontend gate is explicitly enabled', async () => {
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotFixture())}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );
    expect(await screen.findByText('工作台已就绪')).toBeInTheDocument();
    expect(screen.queryByRole('form', { name: '创建目标预览' })).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Creation Spec 开发预览' })).not.toBeInTheDocument();
  });

  it('restores a strict Creation Spec Card from a hard-refresh Snapshot', async () => {
    const openStream = vi.fn((options) => {
      options.onReady({ cursor: 4, latestSeq: 4, minAvailableSeq: 1 });
      return { close: vi.fn() };
    });
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotFixture({
          event_high_watermark: 4,
          creation_spec_preview: creationSpecPreviewCardFixture()
        }))}
        openStream={openStream}
      />
    );
    expect(await screen.findByRole('heading', { name: '新品短片创作规范' })).toBeInTheDocument();
    expect(screen.getByText('开发预览 · Draft · 未扣费/未激活')).toBeInTheDocument();
    expect(screen.getByRole('article')).toHaveAttribute('data-creation-spec-version', '1');
  });

  it('safely disables Preview interaction for an unknown Snapshot card version', async () => {
    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(workspaceSnapshotFixture({
          creation_spec_preview: creationSpecPreviewCardFixture({ schema_version: 'creation_spec.preview.card.v2' })
        }))}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('预览入口已禁用');
    expect(screen.queryByRole('button', { name: '生成开发预览' })).not.toBeInTheDocument();
    expect(screen.queryByText('新品短片创作规范')).not.toBeInTheDocument();
  });

  it('does not read the Tool Catalog until a ready Project has a validated Snapshot', async () => {
    let resolveSnapshot;
    const pendingSnapshot = new Promise((resolve) => {
      resolveSnapshot = resolve;
    });
    const loadToolCatalog = vi.fn().mockResolvedValue(parseToolCatalogResponse(toolCatalogFixture()));

    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockReturnValue(pendingSnapshot)}
        loadToolCatalog={loadToolCatalog}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
      />
    );

    expect(await screen.findByText('正在安全加载工作台快照…')).toBeInTheDocument();
    expect(loadToolCatalog).not.toHaveBeenCalled();
    await act(async () => resolveSnapshot(workspaceSnapshotFixture()));
    await waitFor(() => expect(loadToolCatalog).toHaveBeenCalledWith(WORKSPACE_IDS.session, {
      signal: expect.any(AbortSignal)
    }));
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
      .mockResolvedValueOnce(snapshotWithMessage('旧 Snapshot 内容', 2, {
        creationSpecPreview: creationSpecPreviewCardFixture()
      }))
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
    expect(screen.getByRole('article')).toHaveAttribute('data-creation-spec-version', '1');

    act(() => streams[0].onReset({ reason: 'cursor_expired' }));
    expect(screen.getByRole('main')).toHaveAttribute('data-workspace-state', 'reset');
    expect(screen.getByText('正在同步最新工作台状态…')).toBeInTheDocument();
    // 完整回源提交前保留最后一次已验证投影，但保持 reset/只读状态。
    expect(screen.getByText('旧 Snapshot 内容')).toBeInTheDocument();
    await waitFor(() => expect(loadSnapshot).toHaveBeenCalledTimes(2));
    expect(bootstrap).toHaveBeenCalledTimes(2);

    await act(async () => resolveResetSnapshot(snapshotWithMessage('新 Snapshot 内容', 7, {
      creationSpecPreview: creationSpecPreviewCardFixture({ version: 2, content_digest: 'b'.repeat(64) })
    })));
    await waitFor(() => expect(streams).toHaveLength(2));
    expect(streams[1].cursor).toBe(7);
    expect(screen.queryByText('旧 Snapshot 内容')).not.toBeInTheDocument();
    expect(screen.getByText('新 Snapshot 内容')).toBeInTheDocument();
    expect(screen.getByRole('article')).toHaveAttribute('data-creation-spec-version', '2');
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
    ['recovery_pending', '等待恢复'],
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
    creation_spec_preview: binding.creationSpecPreview || null,
    event_high_watermark: eventHighWatermark
  });
}
