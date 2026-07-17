import { useEffect, useState } from 'react';
import { useProjectWorkspace } from '../workspace/useProjectWorkspace.js';
import { parseProjectBootstrap } from '../workspace/workspaceContract.js';
import { ToolCatalogPanel } from '../tools/ToolCatalogPanel.jsx';
import { CreationSpecCard } from '../aigc/CreationSpecCard.jsx';
import { CreationSpecPreviewForm } from '../aigc/CreationSpecPreviewForm.jsx';
import { PlanStoryboardPreviewForm } from '../aigc/PlanStoryboardPreviewForm.jsx';
import { StoryboardPreviewCard } from '../aigc/StoryboardPreviewCard.jsx';
import { WritePromptsPreviewForm } from '../aigc/WritePromptsPreviewForm.jsx';
import { PromptPreviewCard } from '../aigc/PromptPreviewCard.jsx';
import { useOptionalAuthSession } from '../../platform/auth/authSession.js';
import { consumeQuickCreatePreviewGoal } from './quickCreatePreviewHandoff.js';
import { TurnOutputCard } from '../workspace/TurnOutputCard.jsx';
import { runtimeCapabilityEnabled } from '../../platform/runtimeProfile.js';
import { TextMaterialForm } from '../materials/TextMaterialForm.jsx';
import { MediaPreviewControls } from '../media/MediaPreviewControls.jsx';
import { MediaPreviewCard } from '../media/MediaPreviewCard.jsx';

export { parseProjectBootstrap };

const INPUT_STATUS_LABELS = Object.freeze({
  pending: '已受理',
  claimed: '已领取',
  running: '处理中',
  retry_wait: '等待重试',
  recovery_pending: '等待恢复',
  resolved: '已完成',
  dead: '处理失败'
});

// ProjectWorkspacePage 渲染冻结 W0.5 正式状态机，不读取 Demo Session 或浏览器持久化状态。
export function ProjectWorkspacePage({
  projectID,
  loadToolCatalog,
  enqueueCreationSpecPreview,
  previewCsrfToken,
  previewKeyFactory,
  creationSpecPreviewEnabled = runtimeCapabilityEnabled('planCreationSpec'),
  enqueuePlanStoryboardPreview,
  planStoryboardPreviewCsrfToken,
  planStoryboardPreviewKeyFactory,
  planStoryboardPreviewEnabled = runtimeCapabilityEnabled('planStoryboard'),
  enqueueWritePromptsPreview,
  writePromptsPreviewCsrfToken,
  writePromptsPreviewKeyFactory,
  writePromptsPreviewEnabled = runtimeCapabilityEnabled('writePrompts'),
  analyzeMaterialsPreviewEnabled = runtimeCapabilityEnabled('analyzeMaterials'),
  loadTextMaterials,
  createTextMaterial,
  enqueueAnalyzeMaterialsPreview,
  textMaterialCsrfToken,
  textMaterialKeyFactory,
  analyzeMaterialsPreviewKeyFactory,
  enqueueGenerateMediaPreview,
  enqueueAssembleOutputPreview,
  mediaPreviewCsrfToken,
  mediaPreviewKeyFactory,
  generateMediaPreviewEnabled = runtimeCapabilityEnabled('generateMedia'),
  assembleOutputPreviewEnabled = runtimeCapabilityEnabled('assembleOutput'),
  ...workspaceOptions
}) {
  const auth = useOptionalAuthSession();
  const { state, retry } = useProjectWorkspace({ projectID, ...normalizeLegacyOptions(workspaceOptions) });
  const [previewHandoff, setPreviewHandoff] = useState({ projectID: '', goal: '' });
  const hasSnapshot = Boolean(state.project && state.snapshot);
  const canReadToolCatalog = state.kind === 'ready' && hasSnapshot;
  const sessionID = state.snapshot?.session?.id || '';
  const busy = state.kind === 'loading' || state.kind === 'reset';
  const preview = state.snapshot?.creationSpecPreview || null;
  const previewFailure = state.snapshot?.creationSpecPreviewFailure || null;
  const latestTurnOutput = state.snapshot?.latestTurnOutput || null;
  const analyzeMaterialsPreview = state.snapshot?.analyzeMaterialsPreview || null;
  const planStoryboardPreview = state.snapshot?.planStoryboardPreview || null;
  const writePromptsPreview = state.snapshot?.writePromptsPreview || null;
  const mediaPreviews = state.snapshot?.mediaPreviews || [];
  const mediaPreviewEnabled = generateMediaPreviewEnabled && assembleOutputPreviewEnabled;
  const hasCurrentCreationSpecDraft = preview?.kind === 'card' && preview.status === 'draft';
  const hasStoryboardTargets = planStoryboardPreview?.kind === 'plan_storyboard_preview'
    && planStoryboardPreview.status === 'completed' && planStoryboardPreview.slots.length > 0;

  useEffect(() => {
    if (!creationSpecPreviewEnabled) return;
    const goal = consumeQuickCreatePreviewGoal(projectID);
    setPreviewHandoff((current) => {
      if (goal !== null) return { projectID, goal };
      return current.projectID === projectID ? current : { projectID, goal: '' };
    });
  }, [creationSpecPreviewEnabled, projectID]);

  const previewInitialGoal = previewHandoff.projectID === projectID ? previewHandoff.goal : '';

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
      {hasSnapshot ? <TurnOutputCard output={latestTurnOutput} /> : null}
      {hasSnapshot ? <TurnOutputCard output={analyzeMaterialsPreview} /> : null}
      {canReadToolCatalog && analyzeMaterialsPreviewEnabled ? (
        <TextMaterialForm
          projectID={projectID}
          sessionID={sessionID}
          csrfToken={textMaterialCsrfToken ?? auth?.csrfToken ?? ''}
          load={loadTextMaterials}
          create={createTextMaterial}
          enqueueAnalyze={enqueueAnalyzeMaterialsPreview}
          materialKeyFactory={textMaterialKeyFactory}
          analysisKeyFactory={analyzeMaterialsPreviewKeyFactory}
        />
      ) : null}
      {canReadToolCatalog && (creationSpecPreviewEnabled || preview) ? (
        <CreationSpecPreviewPanel
          enabled={creationSpecPreviewEnabled}
          sessionID={sessionID}
          csrfToken={previewCsrfToken ?? auth?.csrfToken ?? ''}
          preview={preview}
          failure={previewFailure}
          enqueue={enqueueCreationSpecPreview}
          keyFactory={previewKeyFactory}
          initialGoal={previewInitialGoal}
        />
      ) : null}
      {canReadToolCatalog && (planStoryboardPreview || (planStoryboardPreviewEnabled && hasCurrentCreationSpecDraft)) ? (
        <PlanStoryboardPreviewPanel
          enabled={planStoryboardPreviewEnabled && hasCurrentCreationSpecDraft}
          sessionID={sessionID}
          csrfToken={planStoryboardPreviewCsrfToken ?? auth?.csrfToken ?? ''}
          creationSpec={preview}
          preview={planStoryboardPreview}
          failure={planStoryboardPreview?.status === 'failed' ? planStoryboardPreview : null}
          enqueue={enqueuePlanStoryboardPreview}
          keyFactory={planStoryboardPreviewKeyFactory}
        />
      ) : null}
      {canReadToolCatalog && (writePromptsPreview || (writePromptsPreviewEnabled && hasStoryboardTargets)) ? (
        <WritePromptsPreviewPanel
          enabled={writePromptsPreviewEnabled && hasStoryboardTargets}
          sessionID={sessionID}
          csrfToken={writePromptsPreviewCsrfToken ?? auth?.csrfToken ?? ''}
          storyboardPreview={planStoryboardPreview}
          preview={writePromptsPreview}
          failure={writePromptsPreview?.status === 'failed' ? writePromptsPreview : null}
          enqueue={enqueueWritePromptsPreview}
          keyFactory={writePromptsPreviewKeyFactory}
        />
      ) : null}
      {canReadToolCatalog && (mediaPreviewEnabled || mediaPreviews.length > 0) ? (
        <MediaPreviewPanel
          enabled={mediaPreviewEnabled}
          sessionID={sessionID}
          csrfToken={mediaPreviewCsrfToken ?? auth?.csrfToken ?? ''}
          promptPreview={writePromptsPreview}
          mediaCards={mediaPreviews}
          enqueueGenerate={enqueueGenerateMediaPreview}
          enqueueAssemble={enqueueAssembleOutputPreview}
          keyFactory={mediaPreviewKeyFactory}
        />
      ) : null}
      {canReadToolCatalog ? (
        <ToolCatalogPanel sessionID={sessionID} loadCatalog={loadToolCatalog} />
      ) : null}
    </main>
  );
}

function MediaPreviewPanel({ enabled, mediaCards, ...controlProps }) {
  return (
    <section className="media-preview-panel" aria-label="本地媒体开发预览结果">
      {enabled ? <MediaPreviewControls mediaCards={mediaCards} {...controlProps} /> : (
        <header>
          <h2>本地媒体开发预览</h2>
          <p>媒体运行 Profile 已关闭，以下为只读历史结果。</p>
        </header>
      )}
      <div className="media-preview-list">
        {mediaCards.map((card) => (
          <MediaPreviewCard
            key={`${card.inputID}:${card.status}:${card.jobID || card.operationID || card.errorCode}`}
            card={card}
          />
        ))}
      </div>
    </section>
  );
}

function WritePromptsPreviewPanel(props) {
  return (
    <section className="write-prompts-preview-panel" aria-labelledby="write-prompts-preview-title">
      <h2 id="write-prompts-preview-title">Prompt JSON Draft 开发预览</h2>
      {props.enabled ? <WritePromptsPreviewForm {...props} /> : null}
      <PromptPreviewCard preview={props.preview} />
    </section>
  );
}

function PlanStoryboardPreviewPanel(props) {
  if (props.preview?.kind === 'unsupported') {
    return (
      <section className="plan-storyboard-preview-panel" aria-labelledby="plan-storyboard-preview-title">
        <h2 id="plan-storyboard-preview-title">Storyboard JSON Draft 开发预览</h2>
        <p role="alert">当前 Storyboard Preview 版本无法安全展示，入口已禁用。请刷新或升级客户端。</p>
      </section>
    );
  }
  return (
    <section className="plan-storyboard-preview-panel" aria-labelledby="plan-storyboard-preview-title">
      <h2 id="plan-storyboard-preview-title">Storyboard JSON Draft 开发预览</h2>
      {props.enabled ? <PlanStoryboardPreviewForm {...props} /> : null}
      <StoryboardPreviewCard preview={props.preview} />
    </section>
  );
}

function CreationSpecPreviewPanel(props) {
  if (props.preview?.kind === 'unsupported') {
    return (
      <section className="creation-spec-preview-panel" aria-labelledby="creation-spec-preview-title">
        <h2 id="creation-spec-preview-title">Creation Spec 开发预览</h2>
        <p role="alert">当前 Creation Spec Preview 版本无法安全展示，预览入口已禁用。请刷新或升级客户端。</p>
      </section>
    );
  }
  return (
    <section className="creation-spec-preview-panel" aria-labelledby="creation-spec-preview-title">
      <h2 id="creation-spec-preview-title">Creation Spec 开发预览</h2>
      {props.enabled ? <CreationSpecPreviewForm {...props} /> : null}
      <CreationSpecCard preview={props.preview} />
    </section>
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
