import { useProjectWorkspace } from '../workspace/useProjectWorkspace.js';
import { parseProjectBootstrap } from '../workspace/workspaceContract.js';

export { parseProjectBootstrap };

const INPUT_STATUS_LABELS = Object.freeze({
  pending: '已受理',
  claimed: '已领取',
  running: '处理中',
  retry_wait: '等待重试',
  resolved: '已完成',
  dead: '处理失败'
});

// ProjectWorkspacePage 渲染冻结 W0.5 正式状态机，不读取 Demo Session 或浏览器持久化状态。
export function ProjectWorkspacePage({ projectID, ...workspaceOptions }) {
  const { state, retry } = useProjectWorkspace({ projectID, ...normalizeLegacyOptions(workspaceOptions) });
  const hasSnapshot = Boolean(state.project && state.snapshot);
  const sessionID = state.snapshot?.session?.id || '';
  const busy = state.kind === 'loading' || state.kind === 'reset';

  if (state.kind === 'unauthorized') {
    return (
      <main className="project-workspace-gate" data-workspace-state="unauthorized" data-stream-state="closed" aria-busy="false">
        <h1>请先登录</h1>
        <p role="alert">登录状态已失效，请重新登录后访问工作台。</p>
        <button type="button" className="start-button" onClick={() => navigate('/')}>返回首页登录</button>
      </main>
    );
  }
  if (state.kind === 'not_found') {
    return (
      <main className="project-workspace-gate" data-workspace-state="not_found" data-stream-state="closed" aria-busy="false">
        <h1>工作台不存在或不可访问</h1>
        <p role="alert">请确认项目仍然存在，或返回项目列表重新选择。</p>
        <button type="button" className="start-button" onClick={() => navigate('/projects')}>返回项目列表</button>
      </main>
    );
  }

  return (
    <main
      className={`project-workspace-gate${hasSnapshot ? ' project-workspace-gate--ready' : ''}`}
      aria-labelledby="project-workspace-title"
      aria-busy={busy ? 'true' : 'false'}
      data-workspace-state={state.kind}
      data-stream-state={hasSnapshot ? state.streamState : 'closed'}
      data-project-id={hasSnapshot ? state.project.projectID : undefined}
      data-session-id={hasSnapshot ? sessionID : undefined}
    >
      <h1 id="project-workspace-title">创作工作台</h1>
      <WorkspaceStatus state={state} retry={retry} />
      {hasSnapshot ? <WorkspaceSnapshotView project={state.project} snapshot={state.snapshot} /> : null}
    </main>
  );
}

function WorkspaceStatus({ state, retry }) {
  if (state.kind === 'loading') {
    const message = state.phase === 'provisioning'
      ? '项目已受理，工作台正在准备…'
      : state.phase === 'snapshot' ? '正在安全加载工作台快照…' : '正在读取项目状态…';
    return <p role="status">{message}</p>;
  }
  if (state.kind === 'reset') {
    return <p role="status">正在同步最新工作台状态…</p>;
  }
  if (state.kind === 'offline') {
    return (
      <section>
        <p role="alert">{state.error?.message || '工作台连接已中断，请重试。'}</p>
        <button type="button" className="start-button" onClick={retry}>重新连接工作台</button>
      </section>
    );
  }
  const streamMessage = state.streamState === 'live'
    ? '实时更新已连接'
    : state.streamState === 'reconnecting' ? '实时连接正在恢复…' : '正在连接实时更新…';
  return <p role="status"><span>工作台已就绪</span><span>，{streamMessage}</span></p>;
}

function WorkspaceSnapshotView({ project, snapshot }) {
  return (
    <section aria-label="工作台快照">
      <dl>
        <div><dt>Project</dt><dd>{project.projectID}</dd></div>
        <div><dt>Session</dt><dd>{snapshot.session.id}</dd></div>
        <div><dt>项目标题</dt><dd>{project.title}</dd></div>
        <div><dt>会话状态</dt><dd>{snapshot.session.status}</dd></div>
      </dl>
      <section aria-labelledby="workspace-messages-title">
        <h2 id="workspace-messages-title">消息</h2>
        {snapshot.messages.length === 0 ? <p>还没有消息</p> : (
          <ol>
            {snapshot.messages.map((message) => <li key={message.id}>{message.content}</li>)}
          </ol>
        )}
      </section>
      <section aria-labelledby="workspace-inputs-title">
        <h2 id="workspace-inputs-title">输入状态</h2>
        {snapshot.inputs.length === 0 ? <p>还没有待处理输入</p> : (
          <ol>
            {snapshot.inputs.map((input) => (
              <li key={input.id}>输入 {input.enqueueSeq}：{INPUT_STATUS_LABELS[input.status] || input.status}</li>
            ))}
          </ol>
        )}
      </section>
    </section>
  );
}

// normalizeLegacyOptions 仅兼容既有组件测试的 retryDelays 注入名，不改变生产契约。
function normalizeLegacyOptions(options) {
  const { retryDelays, ...rest } = options;
  return retryDelays ? { ...rest, projectRetryDelays: retryDelays } : rest;
}

function navigate(path) {
  window.history.pushState({}, '', path);
  window.dispatchEvent(new Event('dora:navigate'));
}
