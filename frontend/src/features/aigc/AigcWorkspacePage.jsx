import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  AlertCircle,
  ArrowUp,
  Bot,
  CheckCircle2,
  Clock3,
  FileText,
  LoaderCircle,
  MessageCircle,
  PlusCircle,
  Sparkles,
  UserCircle
} from 'lucide-react';
import { BrandLogo } from '../../components/brand/BrandLogo.jsx';
import { StoryboardPanel } from './StoryboardPanel.jsx';
import {
  A2UI_ACTIONS,
  A2UI_COMPONENTS,
  A2UI_EVENT_NAMES,
  A2UI_EVENTS,
  A2UI_VERSION,
  componentPayload
} from './a2uiProtocol.js';

const SESSION_STORAGE_KEY = 'dora:aigc:demo_session_id';
const WELCOME_MESSAGE = '把剧本、风格或 Skill.md 发给我，我会先规划规格和故事板。';

// statusLabels 是前端统一展示状态中文名的映射表。
const statusLabels = {
  queued: '排队中',
  running: '生成中',
  succeeded: '完成',
  failed: '失败',
  cancelled: '已取消',
  done: '完成',
  completed: '完成',
  draft: '草稿',
  reviewing: '待确认',
  confirmed: '已确认',
  generating: '生成中',
  ready: '就绪',
  generated: '已生成'
};

// AigcWorkspacePage 渲染 AIGC 工作台，负责会话、故事板、聊天和 A2UI 卡片交互。
export function AigcWorkspacePage() {
  const [sessionID, setSessionID] = useState('');
  const [storyboard, setStoryboard] = useState(null);
  const [assets, setAssets] = useState([]);
  const [jobs, setJobs] = useState([]);
  const [messages, setMessages] = useState(() => initialMessages());
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);
  const [startingSession, setStartingSession] = useState(false);
  const [error, setError] = useState('');
  const [interrupt, setInterrupt] = useState(null);
  const [surfaces, setSurfaces] = useState([]);
  const [toolRuns, setToolRuns] = useState([]);
  const [selectedTarget, setSelectedTarget] = useState(null);
  const [editing, setEditing] = useState(null);
  const skillInputRef = useRef(null);
  const streamingAssistantID = useRef('');
  const timelineOrderRef = useRef(1);

  // nextTimelineOrder 为消息、工具进度和 A2UI 卡片分配稳定时间线顺序。
  const nextTimelineOrder = useCallback(() => {
    const order = timelineOrderRef.current;
    timelineOrderRef.current += 1;
    return order;
  }, []);

  // assetMap 把素材列表索引成 Map，方便故事板和预览组件按 asset_id 读取。
  const assetMap = useMemo(() => {
    return assets.reduce((map, asset) => {
      const id = asset.id || asset.asset_id;
      if (id) {
        map.set(id, asset);
      }
      return map;
    }, new Map());
  }, [assets]);

  // refreshAssets 刷新当前会话素材，通常在生成任务完成后调用。
  const refreshAssets = useCallback(async (id) => {
    if (!id) {
      return;
    }
    const result = await requestOptionalJSON(`/api/aigc/sessions/${id}/assets`);
    if (result?.assets) {
      setAssets(result.assets);
    }
  }, []);

  // refreshSessionData 拉取故事板、素材、任务和可选历史消息，作为页面冷启动/刷新入口。
  const refreshSessionData = useCallback(
    async (id, options = {}) => {
      if (!id) {
        return;
      }
      const includeMessages = options.includeMessages !== false;
      const [boardResult, assetsResult, jobsResult, messagesResult] = await Promise.allSettled([
        requestOptionalJSON(`/api/aigc/sessions/${id}/storyboard`),
        requestOptionalJSON(`/api/aigc/sessions/${id}/assets`),
        requestOptionalJSON(`/api/aigc/sessions/${id}/jobs`),
        includeMessages ? requestOptionalJSON(`/api/aigc/sessions/${id}/messages`) : Promise.resolve(null)
      ]);
      if (boardResult.status === 'fulfilled' && boardResult.value) {
        setStoryboard(boardResult.value);
        setSelectedTarget(defaultSelectedTarget(boardResult.value));
      }
      if (assetsResult.status === 'fulfilled' && assetsResult.value?.assets) {
        setAssets(assetsResult.value.assets);
      }
      if (jobsResult.status === 'fulfilled' && jobsResult.value?.jobs) {
        setJobs(jobsResult.value.jobs);
      }
      if (includeMessages && messagesResult.status === 'fulfilled' && messagesResult.value?.messages?.length) {
        const history = restoreHistoryFromMessageRecords(messagesResult.value.messages);
        if (history.messages.length || history.surfaces.length) {
          timelineOrderRef.current = Math.max(timelineOrderRef.current, history.nextTimelineOrder);
          setMessages(history.messages);
          setSurfaces(history.surfaces);
        }
      }
      const rejected = [boardResult, assetsResult, jobsResult, includeMessages ? messagesResult : null].find(
        (result) => result?.status === 'rejected'
      );
      if (rejected) {
        throw rejected.reason;
      }
    },
    []
  );

  // finishAssistantMessage 结束正在流式显示的 assistant 消息。
  const finishAssistantMessage = useCallback(() => {
    const messageID = streamingAssistantID.current;
    streamingAssistantID.current = '';
    if (!messageID) {
      return;
    }
    setMessages((items) => items.map((message) => (message.id === messageID ? { ...message, streaming: false } : message)));
  }, []);

  // addSystemMessage 把错误、确认提示等系统信息插入聊天时间线。
  const addSystemMessage = useCallback((content) => {
    if (!content) {
      return;
    }
    setMessages((items) => [
      ...items,
      { id: `event-${Date.now()}-${items.length}`, role: 'system', content, timelineOrder: nextTimelineOrder() }
    ]);
  }, [nextTimelineOrder]);

  // resetWorkspaceState 清空当前工作台状态，用于开启新会话。
  const resetWorkspaceState = useCallback(() => {
    streamingAssistantID.current = '';
    timelineOrderRef.current = 1;
    setStoryboard(null);
    setAssets([]);
    setJobs([]);
    setMessages(initialMessages());
    setInput('');
    setError('');
    setInterrupt(null);
    setSurfaces([]);
    setToolRuns([]);
    setSelectedTarget(null);
    setEditing(null);
  }, []);

  // handleA2UIEvent 是前端唯一 A2UI 事件入口，只执行 action、interrupt 和 error。
  const handleA2UIEvent = useCallback(
    (event) => {
      const eventName = event?.event;
      const payload = event?.payload || {};
      if (eventName === A2UI_EVENTS.READY) {
        setError('');
        return;
      }
      // 前端只执行新的 Action 协议；旧事件不会在这里兜底转换成 UI。
      if (eventName === A2UI_EVENTS.ACTION) {
        applyA2UIActionEnvelope(payload, {
          setSurfaces,
          setStoryboard,
          setAssets,
          setToolRuns,
          setJobs,
          refreshAssets,
          sessionID,
          nextTimelineOrder
        });
        return;
      }
      if (eventName === A2UI_EVENTS.INTERRUPT_REQUEST) {
        setInterrupt(payload);
        addSystemMessage(payload.message || payload.title || '需要确认后继续。');
        return;
      }
      if (eventName === A2UI_EVENTS.ERROR) {
        const message = payload.message || '请求失败';
        setError(message);
        addSystemMessage(message);
      }
    },
    [addSystemMessage, nextTimelineOrder, refreshAssets, sessionID]
  );

  useEffect(() => {
    let cancelled = false;

    // boot 初始化或恢复会话，并加载首屏数据。
    async function boot() {
      setError('');
      try {
        const id = await resolveSessionID();
        if (cancelled) {
          return;
        }
        setSessionID(id);
        await refreshSessionData(id);
      } catch (err) {
        if (!cancelled) {
          setError(errorMessage(err));
        }
      }
    }

    void boot();

    return () => {
      cancelled = true;
    };
  }, [refreshSessionData]);

  useEffect(() => {
    if (!sessionID || typeof window === 'undefined' || typeof window.EventSource !== 'function') {
      return undefined;
    }
    const source = new window.EventSource(`/api/aigc/sessions/${sessionID}/events/stream`);
    const listeners = A2UI_EVENT_NAMES.map((eventName) => {
      const listener = (event) => handleA2UIEvent(parseSSEEvent(event));
      source.addEventListener(eventName, listener);
      return [eventName, listener];
    });
    source.onopen = () => setError('');
    source.onerror = () => setError('事件流已断开，刷新页面可重新连接。');
    return () => {
      listeners.forEach(([eventName, listener]) => source.removeEventListener(eventName, listener));
      source.close();
    };
  }, [handleA2UIEvent, sessionID]);

  // sendMessage 发送普通用户消息；A2UI 输出只从 /events/stream 接收。
  async function sendMessage(nextContent, options = {}) {
    const fromComposer = nextContent == null;
    const content = String(fromComposer ? input : nextContent).trim();
    if (!content || !sessionID || busy) {
      return false;
    }
    if (fromComposer) {
      setInput('');
    }
    setBusy(true);
    setError('');
    setInterrupt(null);
    setMessages((items) => [
      ...items,
      {
        id: `user-${Date.now()}`,
        role: 'user',
        content,
        displayContent: options.displayContent,
        attachments: options.attachments,
        timelineOrder: nextTimelineOrder()
      }
    ]);
    let sent = false;
    try {
      const requestBody = { content };
      if (options.uiSource) {
        requestBody.ui_source = options.uiSource;
      }
      await requestJSON(`/api/aigc/sessions/${sessionID}/messages`, {
        method: 'POST',
        body: JSON.stringify(requestBody)
      });
      finishAssistantMessage();
      await refreshSessionData(sessionID);
      sent = true;
    } catch (err) {
      finishAssistantMessage();
      const message = errorMessage(err);
      setError(message);
      addSystemMessage(message);
    } finally {
      setBusy(false);
    }
    return sent;
  }

  // submitSurface 把 A2UI 组件值归约为普通用户消息，再走标准消息发送链路。
  async function submitSurface(surface, values) {
    const submission = surfaceSubmission(surface, values);
    if (!submission.content) {
      addSystemMessage('请先完成输入、选择或文件上传。');
      return;
    }
    const sent = await sendMessage(submission.content, {
      displayContent: submission.displayContent,
      attachments: submission.attachments,
      uiSource: {
        type: 'a2ui_submit',
        card_id: surface.card_id || surface.id
      }
    });
    if (sent) {
      setSurfaces((items) => items.filter((item) => item.id !== surface.id));
    }
  }

  // startNewSession 创建新会话并重置当前工作台所有状态。
  async function startNewSession() {
    if (busy || startingSession) {
      return;
    }
    setStartingSession(true);
    setError('');
    try {
      const id = await createSession();
      resetWorkspaceState();
      setSessionID(id);
      await refreshSessionData(id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setStartingSession(false);
    }
  }

  // resumeInterrupt 处理 Agent 或媒体图的人审确认，并继续对应运行。
  async function resumeInterrupt(action) {
    if (!interrupt || !sessionID || busy) {
      return;
    }
    const label = action?.label || action?.key || '确认';
    const approved = !String(action?.key || '').includes('revise') && !String(action?.key || '').includes('reject');
    setBusy(true);
    setError('');
    try {
      if (interrupt.scope === 'media_graph') {
        const result = await requestJSON(`/api/aigc/sessions/${sessionID}/media-graph/resume`, {
          method: 'POST',
          body: JSON.stringify({
            checkpoint_id: interrupt.checkpoint_id,
            interrupt_id: interrupt.interrupt_id,
            approved,
            note: label
          })
        });
        setInterrupt(null);
        addSystemMessage(result?.status === 'interrupted' ? '仍需继续确认。' : '已确认参考图。');
        await refreshSessionData(sessionID, { includeMessages: false });
        return;
      }

      await requestJSON(`/api/aigc/sessions/${sessionID}/messages/resume`, {
        method: 'POST',
        body: JSON.stringify({
          checkpoint_id: interrupt.checkpoint_id,
          interrupt_id: interrupt.interrupt_id,
          content: label,
          data: { approved, action_key: action?.key, note: label }
        })
      });
      setInterrupt(null);
      finishAssistantMessage();
      await refreshSessionData(sessionID);
    } catch (err) {
      finishAssistantMessage();
      const message = errorMessage(err);
      setError(message);
      addSystemMessage(message);
    } finally {
      setBusy(false);
    }
  }

  // saveEdit 将左侧故事板的内联编辑保存为版本化 JSON Patch。
  async function saveEdit() {
    if (!editing || !storyboard || !sessionID) {
      return;
    }
    setError('');
    try {
      const result = await requestJSON(`/api/aigc/sessions/${sessionID}/storyboards/${storyboard.id}`, {
        method: 'PATCH',
        body: JSON.stringify({
          base_version: storyboard.version,
          source: 'user',
          ops: [{ op: 'replace', path: editing.path, value: editing.value }]
        })
      });
      setStoryboard(result.storyboard);
      setEditing(null);
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  // importSkillFile 读取本地 Skill.md，导入后绑定到当前会话。
  async function importSkillFile(event) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file || !sessionID) {
      return;
    }
    setError('');
    try {
      const content = await file.text();
      const created = await requestJSON('/api/aigc/skills', {
        method: 'POST',
        body: JSON.stringify({ content })
      });
      const skillID = created?.skill?.id || created?.plan?.skill_id;
      if (!skillID) {
        throw new Error('Skill 创建失败');
      }
      await requestJSON(`/api/aigc/sessions/${sessionID}/skill`, {
        method: 'POST',
        body: JSON.stringify({ skill_id: skillID })
      });
      const skillName = created?.skill?.name || created?.plan?.name || file.name;
      addSystemMessage(`已导入 Skill：${skillName}`);
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  const title = storyboard?.spec_id ? storyboard.spec_id : 'AIGC 创作工作台';
  const chatTimeline = useMemo(() => chatTimelineItems(messages, toolRuns, surfaces), [messages, toolRuns, surfaces]);

  return (
    <div className="aigc-workspace-shell">
      <header className="aigc-workspace-topbar">
        <div className="aigc-workspace-brand">
          <BrandLogo compact />
          <div>
            <h1>{title}</h1>
            <span>{sessionID ? `Session ${sessionID}` : '正在创建会话'}</span>
          </div>
        </div>
        <div className="aigc-workspace-actions">
          <button
            className="aigc-secondary-button"
            type="button"
            onClick={() => void startNewSession()}
            disabled={busy || startingSession || !sessionID}
          >
            <PlusCircle aria-hidden="true" size={16} />
            <span>{startingSession ? '创建中' : '新会话'}</span>
          </button>
          <button className="aigc-icon-button" type="button" onClick={() => void refreshSessionData(sessionID)} aria-label="刷新">
            <Clock3 aria-hidden="true" size={16} />
          </button>
          <button className="aigc-upload-button" type="button" onClick={() => skillInputRef.current?.click()}>
            <Sparkles aria-hidden="true" size={16} />
            <span>导入 Skill</span>
          </button>
          <input
            ref={skillInputRef}
            aria-label="导入 Skill.md"
            hidden
            type="file"
            accept=".md,text/markdown,text/plain"
            onChange={importSkillFile}
          />
        </div>
      </header>

      {error ? (
        <div className="aigc-error-banner" role="alert">
          <AlertCircle aria-hidden="true" size={16} />
          <span>{error}</span>
        </div>
      ) : null}

      <main className="aigc-workspace-main">
        <StoryboardPanel
          storyboard={storyboard}
          selectedTarget={selectedTarget}
          onSelectTarget={setSelectedTarget}
          editing={editing}
          onStartEdit={setEditing}
          onChangeEdit={setEditing}
          onSaveEdit={saveEdit}
          assetMap={assetMap}
          statusLabel={statusLabel}
        />

        <section className="aigc-chat-pane" aria-label="对话">
          <div className="aigc-pane-header">
            <div>
              <MessageCircle aria-hidden="true" size={17} />
              <strong>对话</strong>
            </div>
            <span>{busy ? '生成中' : '待输入'}</span>
          </div>

          <div className="aigc-job-strip" aria-label="生成任务">
            {jobs.length === 0 ? (
              <span>暂无后台任务</span>
            ) : (
              jobs.slice(0, 6).map((job) => <JobChip job={job} key={job.id || job.job_id} />)
            )}
          </div>

          <div className="aigc-message-list">
            {chatTimeline.map((item) => {
              if (item.type === 'message') {
                const message = item.message;
                return (
                  <article className={`aigc-message aigc-message--${message.role}`} key={item.key}>
                    <div className="aigc-message__avatar">
                      {message.role === 'user' ? <UserCircle aria-hidden="true" size={18} /> : <Bot aria-hidden="true" size={18} />}
                    </div>
                    <MessageContent message={message} assetMap={assetMap} />
                  </article>
                );
              }
              if (item.type === 'toolRun') {
                return <ToolRunCard toolRun={item.toolRun} key={item.key} />;
              }
              return (
                <A2UISurfaceCard
                  surface={item.surface}
                  busy={busy}
                  sessionID={sessionID}
                  onAssetUploaded={(asset) => setAssets((items) => upsertByID(items, asset, 'id'))}
                  onSubmit={submitSurface}
                  key={item.key}
                />
              );
            })}
            {interrupt ? (
              <article className="aigc-confirm-card">
                <strong>{interrupt.title || '确认后继续'}</strong>
                <p>{interrupt.message || '请确认当前规划。'}</p>
                <div>
                  {(interrupt.actions || []).slice(0, 3).map((action) => (
                    <button type="button" key={action.key} onClick={() => void resumeInterrupt(action)} disabled={busy}>
                      <CheckCircle2 aria-hidden="true" size={15} />
                      <span>{action.label || action.key}</span>
                    </button>
                  ))}
                </div>
              </article>
            ) : null}
          </div>

          <form
            className="aigc-composer"
            onSubmit={(event) => {
              event.preventDefault();
              void sendMessage();
            }}
          >
            <textarea
              value={input}
              onChange={(event) => setInput(event.target.value)}
              placeholder="输入创作需求或修改意见..."
              rows={3}
            />
            <div className="aigc-composer__bar">
              <span>
                <Sparkles aria-hidden="true" size={15} />
                Skill 驱动
              </span>
              <button className="aigc-send-button" type="submit" disabled={!input.trim() || busy || !sessionID}>
                {busy ? <LoaderCircle aria-hidden="true" size={16} /> : <ArrowUp aria-hidden="true" size={16} />}
                <span>发送</span>
              </button>
            </div>
          </form>
        </section>
      </main>
    </div>
  );
}

// chatTimelineItems 合并消息、工具进度和 A2UI 卡片，生成统一时间线。
function chatTimelineItems(messages, toolRuns, surfaces) {
  // 三类内容共用 timelineOrder，保证“用户消息 -> Agent 卡片 -> 后续用户消息”的顺序稳定。
  const visibleSurfaces = surfaces.filter(isVisibleSurface);
  return [
    ...messages.map((message, index) => ({
      type: 'message',
      key: `message:${message.id}`,
      timelineOrder: timelineOrder(message, index),
      fallbackOrder: index,
      message
    })),
    ...toolRuns.map((toolRun, index) => ({
      type: 'toolRun',
      key: `tool:${toolRunKey(toolRun)}`,
      timelineOrder: timelineOrder(toolRun, messages.length + index),
      fallbackOrder: messages.length + index,
      toolRun
    })),
    ...visibleSurfaces.map((surface, index) => ({
      type: 'surface',
      key: `surface:${surface.id}`,
      timelineOrder: timelineOrder(surface, messages.length + toolRuns.length + index),
      fallbackOrder: messages.length + toolRuns.length + index,
      surface
    }))
  ].sort((left, right) => left.timelineOrder - right.timelineOrder || left.fallbackOrder - right.fallbackOrder);
}

// timelineOrder 读取对象上的时间线顺序，缺失时使用传入 fallback。
function timelineOrder(item, fallback) {
  const order = Number(item?.timelineOrder);
  return Number.isFinite(order) ? order : fallback;
}

// MessageContent 渲染聊天气泡内容；文件消息用附件预览替换裸 file_id。
function MessageContent({ message, assetMap }) {
  const resolved = resolveMessageDisplay(message, assetMap);
  const fallback = message.streaming ? '...' : '';
  return (
    <div className="aigc-message__body">
      {resolved.text || fallback ? <p>{resolved.text || fallback}</p> : null}
      {resolved.attachments.length ? <MessageAttachmentList attachments={resolved.attachments} /> : null}
    </div>
  );
}

// MessageAttachmentList 以缩略图或文件名展示用户上传文件，避免把 file_id 暴露给用户。
function MessageAttachmentList({ attachments }) {
  return (
    <div className="aigc-message-attachments">
      {attachments.map((asset) => (
        <A2UIFilePreview asset={asset} key={fileAssetID(asset) || asset.url || asset.filename} />
      ))}
    </div>
  );
}

// JobChip 渲染顶部后台任务的紧凑状态块。
function JobChip({ job }) {
  const status = job.status || job.Status;
  return (
    <span className={`aigc-job-chip aigc-job-chip--${status}`}>
      {status === 'running' ? <LoaderCircle aria-hidden="true" size={13} /> : <CheckCircle2 aria-hidden="true" size={13} />}
      <span>{job.target_id || job.TargetID || job.job_id || job.id}</span>
      <strong>{statusLabel(status) || status}</strong>
    </span>
  );
}

// ToolRunCard 渲染工具运行进度卡，通常由 update_card/tool_runs 更新。
function ToolRunCard({ toolRun }) {
  const status = toolRun.status || 'running';
  const nodes = Array.isArray(toolRun.nodes) ? toolRun.nodes : [];
  return (
    <article className={`aigc-tool-run aigc-tool-run--${status}`}>
      <header>
        <div>
          {status === 'running' ? <LoaderCircle aria-hidden="true" size={15} /> : <CheckCircle2 aria-hidden="true" size={15} />}
          <strong>{toolRun.display_name || toolRun.tool_key || 'Tool'}</strong>
        </div>
        <span>{statusLabel(status) || status}</span>
      </header>
      {toolRun.summary ? <p>{toolRun.summary}</p> : null}
      {nodes.length ? (
        <A2UIVerticalSteps
          steps={nodes.map((node) => ({
            key: node.node_key || node.key,
            title: node.display_name || node.title || node.node_key,
            status: node.status,
            description: node.description || node.message
          }))}
        />
      ) : null}
      {toolRun.error_message ? <p className="aigc-tool-run__error">{toolRun.error_message}</p> : null}
    </article>
  );
}

// A2UISurfaceCard 渲染 Agent append_card 生成的聊天交互卡。
function A2UISurfaceCard({ surface, busy, sessionID, onAssetUploaded, onSubmit }) {
  const payload = surface?.payload || {};
  const data = payload.data || {};
  const fields = surfaceFields(surface);
  const [values, setValues] = useState(() => initialSurfaceValues(fields));
  const title = payload.title || payload.label || '补充信息';
  const hasComponentTree = Array.isArray(payload.components);
  const hasInteractiveFields = fields.some((field) => isInputField(field));
  const fieldSignature = fields.map(fieldKey).join('|');

  useEffect(() => {
    setValues((current) => ({ ...initialSurfaceValues(fields), ...current }));
  }, [fieldSignature]);

  return (
    <article className="aigc-a2ui-card" aria-label={title}>
      {payload.title || payload.status ? (
        <header>
          {payload.title ? <h2>{title}</h2> : <span>{surface.id}</span>}
          {payload.status ? <span>{statusLabel(payload.status)}</span> : null}
        </header>
      ) : null}
      {payload.message ? <p>{payload.message}</p> : null}
      {hasComponentTree ? (
        <A2UIComponentTree
          surface={surface}
          data={data}
          values={values}
          sessionID={sessionID}
          onAssetUploaded={onAssetUploaded}
          onValueChange={(key, value) => setValues((current) => ({ ...current, [key]: value }))}
        />
      ) : null}
      <form
        className="aigc-a2ui-form"
        onSubmit={(event) => {
          event.preventDefault();
          void onSubmit(surface, values);
        }}
      >
        {!hasComponentTree
          ? fields.map((field) => (
              <A2UIField
                field={field}
                value={values[fieldKey(field)]}
                sessionID={sessionID}
                onAssetUploaded={onAssetUploaded}
                onChange={(value) => setValues((current) => ({ ...current, [fieldKey(field)]: value }))}
                key={fieldKey(field)}
              />
            ))
          : null}
        {hasInteractiveFields ? (
          <button type="submit" disabled={busy || !hasInteractiveFields}>
            <CheckCircle2 aria-hidden="true" size={15} />
            <span>提交</span>
          </button>
        ) : null}
      </form>
    </article>
  );
}

// A2UIComponentTree 从 components/root 构建 A2UI 组件树。
function A2UIComponentTree({ surface, data, values, sessionID, onAssetUploaded, onValueChange }) {
  const payload = surface?.payload || {};
  const components = componentMap(payload.components);
  const root = payload.root || surface.root || payload.root_id || a2uiRootID(components);
  if (!root) {
    return null;
  }
  return (
    <div className="aigc-a2ui-tree">
      <A2UIComponent
        id={root}
        components={components}
        data={data}
        values={values}
        sessionID={sessionID}
        onAssetUploaded={onAssetUploaded}
        onValueChange={onValueChange}
      />
    </div>
  );
}

// A2UIComponent 根据组件 one-of 类型递归选择具体渲染器。
function A2UIComponent({ id, components, data, values, sessionID, onAssetUploaded, onValueChange }) {
  const node = components.get(id);
  if (!node) {
    return null;
  }
  const component = node.component || node;
  const text = componentPayload(component, A2UI_COMPONENTS.TEXT);
  const column = componentPayload(component, A2UI_COMPONENTS.COLUMN);
  const row = componentPayload(component, A2UI_COMPONENTS.ROW);
  const card = componentPayload(component, A2UI_COMPONENTS.CARD);
  const textInput = componentPayload(component, A2UI_COMPONENTS.TEXT_INPUT);
  const singleChoice = componentPayload(component, A2UI_COMPONENTS.SINGLE_CHOICE);
  const multiChoice = componentPayload(component, A2UI_COMPONENTS.MULTI_CHOICE);
  const fileUpload = componentPayload(component, A2UI_COMPONENTS.FILE_UPLOAD);
  const imagePreview = componentPayload(component, A2UI_COMPONENTS.IMAGE_PREVIEW);
  const videoPreview = componentPayload(component, A2UI_COMPONENTS.VIDEO_PREVIEW);
  const verticalSteps = componentPayload(component, A2UI_COMPONENTS.VERTICAL_STEPS);
  const markdown = componentPayload(component, A2UI_COMPONENTS.MARKDOWN);

  if (text) {
    const value = text.dataKey ? data[text.dataKey] || '' : text.value || '';
    const className = `aigc-a2ui-text aigc-a2ui-text--${text.usageHint || 'body'}`;
    return text.usageHint === 'title' ? <h2 className={className}>{value}</h2> : <p className={className}>{value}</p>;
  }
  if (column) {
    return (
      <div className="aigc-a2ui-column">
        {(column.children || []).map((childID) => (
          <A2UIComponent
            id={childID}
            components={components}
            data={data}
            values={values}
            sessionID={sessionID}
            onAssetUploaded={onAssetUploaded}
            onValueChange={onValueChange}
            key={childID}
          />
        ))}
      </div>
    );
  }
  if (row) {
    return (
      <div className="aigc-a2ui-row">
        {(row.children || []).map((childID) => (
          <A2UIComponent
            id={childID}
            components={components}
            data={data}
            values={values}
            sessionID={sessionID}
            onAssetUploaded={onAssetUploaded}
            onValueChange={onValueChange}
            key={childID}
          />
        ))}
      </div>
    );
  }
  if (card) {
    return (
      <div className="aigc-a2ui-inner-card">
        {(card.children || []).map((childID) => (
          <A2UIComponent
            id={childID}
            components={components}
            data={data}
            values={values}
            sessionID={sessionID}
            onAssetUploaded={onAssetUploaded}
            onValueChange={onValueChange}
            key={childID}
          />
        ))}
      </div>
    );
  }
  if (textInput) {
    return (
      <A2UIField
        field={{ ...textInput, type: textInput.multiline ? 'textarea' : 'text' }}
        value={values[fieldKey(textInput)]}
        onChange={(value) => onValueChange(fieldKey(textInput), value)}
      />
    );
  }
  if (singleChoice) {
    return <A2UIField field={{ ...singleChoice, type: 'single_choice' }} value={values[fieldKey(singleChoice)]} onChange={(value) => onValueChange(fieldKey(singleChoice), value)} />;
  }
  if (multiChoice) {
    return <A2UIField field={{ ...multiChoice, type: 'multi_choice' }} value={values[fieldKey(multiChoice)]} onChange={(value) => onValueChange(fieldKey(multiChoice), value)} />;
  }
  if (fileUpload) {
    return (
      <A2UIField
        field={{ ...fileUpload, type: 'file_upload' }}
        value={values[fieldKey(fileUpload)]}
        sessionID={sessionID}
        onAssetUploaded={onAssetUploaded}
        onChange={(value) => onValueChange(fieldKey(fileUpload), value)}
      />
    );
  }
  if (imagePreview) {
    return <A2UIImagePreview item={imagePreview} />;
  }
  if (videoPreview) {
    return <A2UIVideoPreview item={videoPreview} />;
  }
  if (verticalSteps) {
    return <A2UIVerticalSteps steps={verticalSteps.steps || []} />;
  }
  if (markdown) {
    return <A2UIMarkdown item={markdown} data={data} />;
  }
  return null;
}

// A2UIMarkdown 渲染 A2UI Markdown 组件，支持从 dataKey 读取内容。
function A2UIMarkdown({ item, data }) {
  const value = markdownValue(item, data);
  if (!value.trim()) {
    return null;
  }
  return <div className="aigc-a2ui-markdown">{renderMarkdownBlocks(value)}</div>;
}

// markdownValue 解析 Markdown 组件的文本来源。
function markdownValue(item, data) {
  if (typeof item === 'string') {
    return item;
  }
  const dataKey = item?.dataKey || item?.data_key;
  if (dataKey && data?.[dataKey] != null) {
    return String(data[dataKey]);
  }
  return String(item?.value ?? item?.content ?? item?.text ?? item?.markdown ?? '');
}

// renderMarkdownBlocks 把有限 Markdown 语法转换成 React block 节点。
function renderMarkdownBlocks(markdown) {
  const lines = markdown.replace(/\r\n?/g, '\n').split('\n');
  const blocks = [];
  let index = 0;

  while (index < lines.length) {
    const line = lines[index];
    const trimmed = line.trim();
    if (!trimmed) {
      index += 1;
      continue;
    }

    if (trimmed.startsWith('```')) {
      const code = [];
      index += 1;
      while (index < lines.length && !lines[index].trim().startsWith('```')) {
        code.push(lines[index]);
        index += 1;
      }
      index += index < lines.length ? 1 : 0;
      blocks.push(
        <pre className="aigc-a2ui-markdown__pre" key={`code-${blocks.length}`}>
          <code>{code.join('\n')}</code>
        </pre>
      );
      continue;
    }

    const heading = trimmed.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      const HeadingTag = `h${Math.min(6, heading[1].length + 1)}`;
      blocks.push(
        <HeadingTag className="aigc-a2ui-markdown__heading" key={`heading-${blocks.length}`}>
          {renderMarkdownInline(heading[2], `heading-${blocks.length}`)}
        </HeadingTag>
      );
      index += 1;
      continue;
    }

    const unordered = trimmed.match(/^[-*]\s+(.+)$/);
    if (unordered) {
      const items = [];
      while (index < lines.length) {
        const item = lines[index].trim().match(/^[-*]\s+(.+)$/);
        if (!item) {
          break;
        }
        items.push(item[1]);
        index += 1;
      }
      blocks.push(
        <ul className="aigc-a2ui-markdown__list" key={`ul-${blocks.length}`}>
          {items.map((item, itemIndex) => (
            <li key={`ul-${blocks.length}-${itemIndex}`}>{renderMarkdownInline(item, `ul-${blocks.length}-${itemIndex}`)}</li>
          ))}
        </ul>
      );
      continue;
    }

    const ordered = trimmed.match(/^\d+\.\s+(.+)$/);
    if (ordered) {
      const items = [];
      while (index < lines.length) {
        const item = lines[index].trim().match(/^\d+\.\s+(.+)$/);
        if (!item) {
          break;
        }
        items.push(item[1]);
        index += 1;
      }
      blocks.push(
        <ol className="aigc-a2ui-markdown__list" key={`ol-${blocks.length}`}>
          {items.map((item, itemIndex) => (
            <li key={`ol-${blocks.length}-${itemIndex}`}>{renderMarkdownInline(item, `ol-${blocks.length}-${itemIndex}`)}</li>
          ))}
        </ol>
      );
      continue;
    }

    const paragraph = [trimmed];
    index += 1;
    while (index < lines.length && lines[index].trim() && !isMarkdownBlockStart(lines[index].trim())) {
      paragraph.push(lines[index].trim());
      index += 1;
    }
    blocks.push(
      <p className="aigc-a2ui-markdown__paragraph" key={`p-${blocks.length}`}>
        {renderMarkdownInline(paragraph.join(' '), `p-${blocks.length}`)}
      </p>
    );
  }

  return blocks;
}

// isMarkdownBlockStart 判断当前行是否开启新的 Markdown block。
function isMarkdownBlockStart(line) {
  return /^(#{1,6})\s+/.test(line) || /^[-*]\s+/.test(line) || /^\d+\.\s+/.test(line) || line.startsWith('```');
}

// renderMarkdownInline 渲染行内 code、strong、em 和链接。
function renderMarkdownInline(text, keyPrefix) {
  const parts = [];
  const pattern = /(`[^`]+`|\*\*[^*]+\*\*|\*[^*]+\*|\[[^\]]+\]\([^)]+\))/g;
  let lastIndex = 0;
  let match;

  while ((match = pattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      parts.push(text.slice(lastIndex, match.index));
    }
    const token = match[0];
    const key = `${keyPrefix}-${parts.length}`;
    if (token.startsWith('`')) {
      parts.push(
        <code className="aigc-a2ui-markdown__code" key={key}>
          {token.slice(1, -1)}
        </code>
      );
    } else if (token.startsWith('**')) {
      parts.push(
        <strong className="aigc-a2ui-markdown__strong" key={key}>
          {token.slice(2, -2)}
        </strong>
      );
    } else if (token.startsWith('*')) {
      parts.push(
        <em className="aigc-a2ui-markdown__em" key={key}>
          {token.slice(1, -1)}
        </em>
      );
    } else {
      const link = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
      parts.push(renderMarkdownLink(link?.[1] || token, link?.[2] || '', key));
    }
    lastIndex = pattern.lastIndex;
  }

  if (lastIndex < text.length) {
    parts.push(text.slice(lastIndex));
  }
  return parts;
}

// renderMarkdownLink 渲染安全链接，非法协议会退回纯文本。
function renderMarkdownLink(label, href, key) {
  const safeHref = safeMarkdownHref(href);
  if (!safeHref) {
    return label;
  }
  return (
    <a href={safeHref} key={key} rel="noreferrer" target="_blank">
      {label}
    </a>
  );
}

// safeMarkdownHref 只允许 http、https 和 mailto 链接进入 DOM。
function safeMarkdownHref(href) {
  const value = String(href || '').trim();
  if (/^(https?:|mailto:)/i.test(value)) {
    return value;
  }
  return '';
}

// A2UIField 渲染 A2UI 表单字段和只读预览字段。
function A2UIField({ field, value, sessionID, onAssetUploaded, onChange }) {
  const key = fieldKey(field);
  const label = field.label || key;
  const type = fieldType(field);
  if (type === 'single_choice') {
    return (
      <fieldset className="aigc-a2ui-choice">
        <legend>{label}</legend>
        {fieldOptions(field).map((option) => (
          <label key={option.value}>
            <input
              type="radio"
              name={key}
              value={option.value}
              checked={value === option.value}
              required={Boolean(field.required)}
              onChange={() => onChange(option.value)}
            />
            <span>{option.label}</span>
          </label>
        ))}
      </fieldset>
    );
  }
  if (type === 'multi_choice') {
    const selected = Array.isArray(value) ? value : [];
    return (
      <fieldset className="aigc-a2ui-choice">
        <legend>{label}</legend>
        {fieldOptions(field).map((option) => (
          <label key={option.value}>
            <input
              type="checkbox"
              value={option.value}
              checked={selected.includes(option.value)}
              onChange={(event) => {
                const next = event.target.checked
                  ? [...selected, option.value]
                  : selected.filter((item) => item !== option.value);
                onChange(next);
              }}
            />
            <span>{option.label}</span>
          </label>
        ))}
      </fieldset>
    );
  }
  if (type === 'image_preview') {
    return <A2UIImagePreview item={field} />;
  }
  if (type === 'video_preview') {
    return <A2UIVideoPreview item={field} />;
  }
  if (type === 'vertical_steps') {
    return <A2UIVerticalSteps steps={field.steps || []} />;
  }
  if (type === 'file_upload') {
    return (
      <A2UIFileUploadField
        field={field}
        value={value}
        sessionID={sessionID}
        onAssetUploaded={onAssetUploaded}
        onChange={onChange}
      />
    );
  }

  const Input = type === 'textarea' || field.multiline ? 'textarea' : 'input';
  return (
    <label className="aigc-a2ui-field">
      <span>{label}</span>
      <Input
        aria-label={label}
        value={value || ''}
        placeholder={field.placeholder || ''}
        required={Boolean(field.required)}
        rows={Input === 'textarea' ? 3 : undefined}
        onChange={(event) => onChange(event.target.value)}
      />
    </label>
  );
}

// A2UIFileUploadField 上传用户文件，提交值保存 file_id，界面只显示缩略预览和文件名。
function A2UIFileUploadField({ field, value, sessionID, onAssetUploaded, onChange }) {
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');
  const key = fieldKey(field);
  const label = field.label || key || '上传文件';
  const selected = fileUploadValueList(value);

  async function uploadFiles(files) {
    if (!sessionID || !files.length) {
      return;
    }
    setUploading(true);
    setError('');
    try {
      const uploaded = [];
      for (const file of files) {
        const form = new FormData();
        form.append('session_id', sessionID);
        form.append('file', file);
        const kind = uploadKind(field, file);
        if (kind) {
          form.append('kind', kind);
        }
        const asset = await requestJSON('/api/aigc/assets', {
          method: 'POST',
          body: form
        });
        const normalized = normalizeUploadedAsset(asset, file);
        uploaded.push(normalized);
        if (onAssetUploaded) {
          onAssetUploaded(normalized);
        }
      }
      const next = field.multiple ? [...selected, ...uploaded] : uploaded[uploaded.length - 1] || null;
      onChange(next);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setUploading(false);
    }
  }

  return (
    <div className="aigc-a2ui-upload-field">
      <label>
        <span>{label}</span>
        <input
          aria-label={label}
          type="file"
          accept={field.accept || ''}
          multiple={Boolean(field.multiple)}
          required={Boolean(field.required) && selected.length === 0}
          disabled={uploading || !sessionID}
          onChange={(event) => void uploadFiles(Array.from(event.target.files || []))}
        />
      </label>
      {uploading ? <p>上传中...</p> : null}
      {error ? <p className="aigc-a2ui-upload-field__error">{error}</p> : null}
      {selected.length ? (
        <div className="aigc-a2ui-upload-list">
          {selected.map((asset) => (
            <A2UIFilePreview asset={asset} key={fileAssetID(asset) || asset.url || asset.filename} />
          ))}
        </div>
      ) : null}
    </div>
  );
}

// A2UIFilePreview 展示文件缩略图或文档占位，不显示 file_id。
function A2UIFilePreview({ asset }) {
  const label = fileAssetLabel(asset);
  if (isImageAsset(asset) && asset.url) {
    return (
      <figure className="aigc-a2ui-file-preview">
        <img src={asset.url} alt={label} loading="lazy" />
        <figcaption>{label}</figcaption>
      </figure>
    );
  }
  if (isVideoAsset(asset) && asset.url) {
    return (
      <figure className="aigc-a2ui-file-preview">
        <video src={asset.url} controls preload="metadata" />
        <figcaption>{label}</figcaption>
      </figure>
    );
  }
  return (
    <div className="aigc-a2ui-file-preview aigc-a2ui-file-preview--document">
      <FileText aria-hidden="true" size={18} />
      <span>{label}</span>
    </div>
  );
}

// A2UIImagePreview 渲染 A2UI 图片预览组件。
function A2UIImagePreview({ item }) {
  const url = item.url;
  if (!url) {
    return null;
  }
  return (
    <figure className="aigc-a2ui-media">
      <img src={url} alt={item.alt || item.title || '图片预览'} loading="lazy" />
      {item.title || item.caption ? <figcaption>{item.title || item.caption}</figcaption> : null}
    </figure>
  );
}

// A2UIVideoPreview 渲染 A2UI 视频预览组件。
function A2UIVideoPreview({ item }) {
  const url = item.url;
  if (!url) {
    return null;
  }
  return (
    <figure className="aigc-a2ui-media">
      <video src={url} poster={item.poster || ''} controls preload="metadata" />
      {item.title || item.caption ? <figcaption>{item.title || item.caption}</figcaption> : null}
    </figure>
  );
}

// A2UIVerticalSteps 渲染纵向步骤条，用于工具进度或业务流程提示。
function A2UIVerticalSteps({ steps }) {
  if (!steps.length) {
    return null;
  }
  return (
    <ol className="aigc-a2ui-steps">
      {steps.map((step, index) => (
        <li className={`aigc-a2ui-step aigc-a2ui-step--${step.status || 'pending'}`} key={step.key || step.id || index}>
          <span>{index + 1}</span>
          <div>
            <strong>{step.title || step.label || `步骤 ${index + 1}`}</strong>
            {step.description || step.message ? <p>{step.description || step.message}</p> : null}
          </div>
        </li>
      ))}
    </ol>
  );
}

// resolveSessionID 从 URL/localStorage 读取会话，缺失时创建新会话。
async function resolveSessionID() {
  const params = new URLSearchParams(window.location.search);
  const explicit = params.get('session_id');
  const cached = readLocalSessionID();
  if (explicit || cached) {
    return explicit || cached;
  }
  return createSession();
}

// createSession 调用后端创建 demo 会话并缓存 session_id。
async function createSession() {
  const session = await requestJSON('/api/aigc/sessions', {
    method: 'POST',
    body: JSON.stringify({
      user_id: 'demo-user',
      title: 'AIGC Demo'
    })
  });
  if (!session?.id) {
    throw new Error('会话创建失败');
  }
  writeLocalSessionID(session.id);
  return session.id;
}

// initialMessages 返回工作台初始欢迎消息。
function initialMessages() {
  return [
    {
      id: 'welcome',
      role: 'assistant',
      content: WELCOME_MESSAGE,
      timelineOrder: 0
    }
  ];
}

// readLocalSessionID 从 localStorage 读取上次使用的会话 ID。
function readLocalSessionID() {
  try {
    if (typeof window.localStorage?.getItem === 'function') {
      return window.localStorage.getItem(SESSION_STORAGE_KEY);
    }
  } catch {
    return '';
  }
  return '';
}

// writeLocalSessionID 缓存会话 ID；存储不可用时静默忽略。
function writeLocalSessionID(sessionID) {
  try {
    if (typeof window.localStorage?.setItem === 'function') {
      window.localStorage.setItem(SESSION_STORAGE_KEY, sessionID);
    }
  } catch {
    // Session creation still succeeds when browser storage is unavailable.
  }
}

// requestOptionalJSON 调用 JSON API，404 时返回 null 方便可选资源读取。
async function requestOptionalJSON(path, options) {
  try {
    return await requestJSON(path, options);
  } catch (err) {
    if (err.status === 404) {
      return null;
    }
    throw err;
  }
}

// requestJSON 封装 fetch JSON 请求，并把非 2xx 响应转成带 status 的 Error。
async function requestJSON(path, options = {}) {
  const body = options.body;
  const headers = body instanceof FormData ? options.headers || {} : { 'Content-Type': 'application/json', ...(options.headers || {}) };
  const response = await fetch(path, { ...options, headers });
  if (!response.ok) {
    const text = await response.text();
    const err = new Error(text || response.statusText);
    err.status = response.status;
    throw err;
  }
  if (response.status === 204) {
    return null;
  }
  return response.json();
}

// parseSSEEvent 解析 EventSource 事件，JSON 失败时转成 error-like payload。
function parseSSEEvent(event) {
  try {
    return JSON.parse(event.data);
  } catch {
    return { event: event.type, payload: { message: event.data } };
  }
}

// applyStoryboardPatch 应用故事板 JSON Patch 或生成素材更新 hint。
function applyStoryboardPatch(current, patch) {
  if (!current) {
    return current;
  }
  const next = cloneJSON(current);
  if (patch?.ops?.length) {
    patch.ops.forEach((op) => applyPatchOp(next, op));
  } else if (patch?.updates?.length) {
    patch.updates.forEach((update) => applyStoryboardUpdateHint(next, update));
  } else {
    return current;
  }
  if (patch.next_version) {
    next.version = patch.next_version;
  }
  return next;
}

// applyStoryboardUpdateHint 把生成素材绑定 hint 应用到对应故事板目标。
function applyStoryboardUpdateHint(board, update) {
  const targetType = update?.target_type || update?.targetType;
  const targetID = update?.target_id || update?.targetId;
  const assetIDs = update?.asset_ids || update?.assetIds || [];
  const field = update?.field || defaultAssetField(update?.asset_kind || update?.assetKind, targetType);
  const status = update?.status === 'generated' ? 'ready' : update?.status;
  if (!targetType || !targetID || !field) {
    return;
  }
  const target = findStoryboardTarget(board, targetType, targetID);
  if (!target) {
    return;
  }
  if (field === 'asset_ids') {
    const existing = Array.isArray(target.asset_ids) ? target.asset_ids : [];
    target.asset_ids = [...new Set([...existing, ...assetIDs])];
  } else if (assetIDs.length) {
    target[field] = assetIDs[0];
  }
  if (status) {
    target.status = status;
  }
}

// findStoryboardTarget 按目标类型和 ID 定位故事板对象。
function findStoryboardTarget(board, targetType, targetID) {
  if (targetType === 'key_element') {
    return (board.key_elements || []).find((item) => item.key === targetID);
  }
  if (targetType === 'shot') {
    return (board.shots || []).find((item) => item.shot_id === targetID);
  }
  if (targetType === 'audio_layer') {
    return (board.audio_layers || []).find((item) => item.layer_id === targetID);
  }
  return null;
}

// defaultAssetField 根据素材类型和故事板目标推断绑定字段。
function defaultAssetField(assetKind, targetType) {
  if (targetType === 'key_element') {
    return 'asset_ids';
  }
  if (targetType === 'shot') {
    return assetKind === 'video' ? 'video_asset_id' : 'keyframe_asset_id';
  }
  if (targetType === 'audio_layer') {
    return 'asset_id';
  }
  return '';
}

// applyPatchOp 在本地对象上应用单条 JSON Pointer patch。
function applyPatchOp(root, op) {
  const tokens = op.path.split('/').slice(1).map(decodePointerToken);
  if (!tokens.length) {
    return;
  }
  let target = root;
  for (let index = 0; index < tokens.length - 1; index += 1) {
    const token = tokens[index];
    const nextToken = tokens[index + 1];
    if (target[token] == null) {
      target[token] = /^\d+$/.test(nextToken) ? [] : {};
    }
    target = target[token];
  }
  const last = tokens[tokens.length - 1];
  if (Array.isArray(target)) {
    const arrayIndex = last === '-' ? target.length : Number(last);
    if (op.op === 'remove') {
      target.splice(arrayIndex, 1);
      return;
    }
    if (op.op === 'add') {
      target.splice(arrayIndex, 0, op.value);
      return;
    }
    target[arrayIndex] = op.value;
    return;
  }
  if (op.op === 'remove') {
    delete target[last];
    return;
  }
  target[last] = op.value;
}

// decodePointerToken 解码 JSON Pointer token 中的转义字符。
function decodePointerToken(token) {
  return token.replace(/~1/g, '/').replace(/~0/g, '~');
}

// cloneJSON 通过 JSON 序列化复制普通数据对象。
function cloneJSON(value) {
  return JSON.parse(JSON.stringify(value));
}

// upsertByID 用指定 key 或 id 对列表做幂等插入/替换。
function upsertByID(items, item, key) {
  const id = item?.[key] || item?.id;
  if (!id) {
    return items;
  }
  const next = items.filter((existing) => (existing?.[key] || existing?.id) !== id);
  return [item, ...next];
}

// toolRunKey 生成工具运行卡的稳定 key，避免同一任务重复渲染。
function toolRunKey(toolRun) {
  return toolRun?.data_model_key || toolRun?.run_key || toolRun?.tool_call_id || toolRun?.job_id || toolRun?.id || toolRun?.tool_key || 'tool_run';
}

// upsertToolRun 按稳定 key 合并工具运行状态。
function upsertToolRun(items, toolRun) {
  const id = toolRunKey(toolRun);
  if (!id) {
    return items;
  }
  const next = items.filter((existing) => toolRunKey(existing) !== id);
  const previous = items.find((existing) => toolRunKey(existing) === id);
  return [mergeToolRun(previous, toolRun), ...next];
}

// mergeToolRun 合并工具运行卡，保留首次出现的时间线顺序。
function mergeToolRun(previous, incoming) {
  if (!previous) {
    return incoming;
  }
  return {
    ...previous,
    ...incoming,
    timelineOrder: previous.timelineOrder ?? incoming.timelineOrder,
    nodes: mergeToolRunNodes(previous.nodes, incoming.nodes)
  };
}

// mergeToolRunNodes 按节点 key 合并工具步骤状态。
function mergeToolRunNodes(previousNodes = [], incomingNodes = []) {
  if (!Array.isArray(incomingNodes) || incomingNodes.length === 0) {
    return Array.isArray(previousNodes) ? previousNodes : [];
  }
  const merged = Array.isArray(previousNodes) ? [...previousNodes] : [];
  incomingNodes.forEach((node) => {
    const key = toolRunNodeKey(node);
    const index = merged.findIndex((existing) => toolRunNodeKey(existing) === key);
    if (index >= 0) {
      merged[index] = { ...merged[index], ...node };
      return;
    }
    merged.push(node);
  });
  return merged;
}

// toolRunNodeKey 生成工具步骤节点的稳定 key。
function toolRunNodeKey(node) {
  return node?.node_key || node?.key || node?.display_name || node?.title || '';
}

// toolRunToJob 把工具运行视图模型投影成顶部任务条所需 job 对象。
function toolRunToJob(toolRun) {
  return {
    job_id: toolRun.job_id,
    session_id: toolRun.session_id,
    target_type: toolRun.target_type,
    target_id: toolRun.target_id,
    status: toolRun.status,
    result_asset_ids: toolRun.result_asset_ids || []
  };
}

// upsertSurface 按 surface/card id 幂等插入或合并 A2UI 卡片。
function upsertSurface(items, surface) {
  if (!surface?.id) {
    return items;
  }
  const existing = items.find((item) => item.id === surface.id);
  const next = items.filter((item) => item.id !== surface.id);
  const merged = existing ? mergeSurface(existing, surface) : surface;
  return [...next, merged];
}

// applyA2UIActionEnvelope 按顺序执行 Agent 直出的 A2UI actions。
function applyA2UIActionEnvelope(envelope, context) {
  // Agent 可以一次返回多个 action，前端按数组顺序同步应用。
  const actions = Array.isArray(envelope?.actions) ? envelope.actions : [];
  actions.forEach((action) => applyA2UIAction(action, context));
}

// applyA2UIAction 根据 action type 分发到新增卡片或更新卡片逻辑。
function applyA2UIAction(action, context) {
  const type = String(action?.type || '').trim();
  if (type === 'append_card') {
    appendA2UICard(action, context);
    return;
  }
  if (type === 'update_card') {
    updateA2UICard(action, context);
  }
}

// appendA2UICard 新增 A2UI 卡片；后端下发的 card_id 已经是实例级唯一值。
function appendA2UICard(action, { setSurfaces, nextTimelineOrder }) {
  const card = normalizeActionCard(action);
  const cardID = action.card_id || card.card_id || action.ref || '';
  if (!cardID) {
    return;
  }
  setSurfaces((items) =>
    upsertSurface(items, {
      id: cardID,
      surface_id: cardID,
      card_id: cardID,
      message_id: action.message_id,
      ref: action.ref,
      surface: action.surface || 'chat',
      payload: card,
      timelineOrder: nextTimelineOrder()
    })
  );
}

// updateA2UICard 更新故事板、工具进度或指定聊天卡片。
function updateA2UICard(action, context) {
  const target = action.target || {};
  const targetSurface = target.surface || action.surface;
  const payloadPatch = action.payload?.patch;
  // storyboard/tool_runs 是工作区状态更新，其余 surface 按卡片 patch 更新。
  if (targetSurface === 'storyboard') {
    if (action.payload?.storyboard) {
      context.setStoryboard(action.payload.storyboard);
    }
    if (action.payload?.assets) {
      context.setAssets(action.payload.assets);
    }
    if (payloadPatch) {
      context.setStoryboard((current) => applyStoryboardPatch(current, payloadPatch));
    } else if (Array.isArray(action.patch)) {
      context.setStoryboard((current) => applyStoryboardPatch(current, { ops: action.patch }));
    }
    return;
  }
  if (targetSurface === 'tool_runs' && action.payload?.tool_run) {
    const toolRun = {
      ...action.payload.tool_run,
      data_model_key: target.card_id || action.card_id || target.ref || action.ref || action.payload.tool_run.data_model_key,
      timelineOrder: context.nextTimelineOrder()
    };
    context.setToolRuns((items) => upsertToolRun(items, toolRun));
    if (toolRun.job_id) {
      context.setJobs((items) => upsertByID(items, toolRunToJob(toolRun), 'job_id'));
      if (toolRun.status === 'succeeded' && toolRun.result_asset_ids?.length) {
        void context.refreshAssets(toolRun.session_id || context.sessionID);
      }
    }
    return;
  }

  const card = normalizeActionCard(action);
  const targetID = target.card_id || action.card_id || target.ref || action.ref;
  if (!targetID) {
    return;
  }
  context.setSurfaces((items) =>
    items.map((surface) => {
      if (surface.id !== targetID && surface.card_id !== targetID && surface.ref !== targetID) {
        return surface;
      }
      const payload = applyCardPatch({ ...(surface.payload || {}), ...card }, action.patch || []);
      return mergeSurface(surface, { ...surface, payload });
    })
  );
}

// normalizeActionCard 从 action 中提取 card，并补齐 card_id/ref/surface。
function normalizeActionCard(action) {
  const card = action?.card && typeof action.card === 'object' ? { ...action.card } : {};
  if (action?.card_id && !card.card_id) {
    card.card_id = action.card_id;
  }
  if (action?.ref && !card.ref) {
    card.ref = action.ref;
  }
  if (action?.surface && !card.surface) {
    card.surface = action.surface;
  }
  return card;
}

// applyCardPatch 对卡片 payload 应用 JSON Patch。
function applyCardPatch(payload, patch) {
  if (!Array.isArray(patch) || patch.length === 0) {
    return payload;
  }
  const next = cloneJSON(payload);
  patch.forEach((op) => applyPatchOp(next, op));
  return next;
}

// isVisibleSurface 判断 A2UI surface 是否具备可见内容。
function isVisibleSurface(surface) {
  const payload = surface?.payload || {};
  return Boolean(payload.title || payload.message || payload.status || payload.components?.length);
}

// mergeSurface 合并同一 A2UI 卡片，保留首次出现的时间线顺序。
function mergeSurface(existing, incoming) {
  const payload = { ...(existing.payload || {}), ...(incoming.payload || {}) };
  if (Array.isArray(existing.payload?.components) || Array.isArray(incoming.payload?.components)) {
    payload.components = mergeComponents(existing.payload?.components || [], incoming.payload?.components || []);
  }
  return {
    ...existing,
    ...incoming,
    root: incoming.root || incoming.payload?.root || existing.root || existing.payload?.root,
    timelineOrder: existing.timelineOrder ?? incoming.timelineOrder,
    payload
  };
}

// mergeComponents 按组件 id 合并组件树，供 update_card 局部更新使用。
function mergeComponents(existing, incoming) {
  const map = new Map();
  existing.forEach((component) => {
    if (component?.id) {
      map.set(component.id, component);
    }
  });
  incoming.forEach((component) => {
    if (component?.id) {
      map.set(component.id, component);
    }
  });
  return [...map.values()];
}

// surfaceFields 从标准 components 组件树中提取可提交字段。
function surfaceFields(surface) {
  const payload = surface?.payload || {};
  if (!Array.isArray(payload.components)) {
    return [];
  }
  return payload.components.map(componentField).filter(Boolean);
}

// componentField 把 A2UI 输入类组件转换成统一 field 描述。
function componentField(node) {
  const component = node?.component || node || {};
  const textInput = componentPayload(component, A2UI_COMPONENTS.TEXT_INPUT);
  const singleChoice = componentPayload(component, A2UI_COMPONENTS.SINGLE_CHOICE);
  const multiChoice = componentPayload(component, A2UI_COMPONENTS.MULTI_CHOICE);
  const fileUpload = componentPayload(component, A2UI_COMPONENTS.FILE_UPLOAD);
  if (textInput) {
    return { ...textInput, type: textInput.multiline ? 'textarea' : 'text' };
  }
  if (singleChoice) {
    return { ...singleChoice, type: 'single_choice' };
  }
  if (multiChoice) {
    return { ...multiChoice, type: 'multi_choice' };
  }
  if (fileUpload) {
    return { ...fileUpload, type: 'file_upload' };
  }
  return null;
}

// initialSurfaceValues 根据字段声明生成表单初始值。
function initialSurfaceValues(fields) {
  return fields.reduce((values, field) => {
    const key = field.key || field.name || field.label;
    if (key) {
      values[key] = field.value || (['multi_choice', 'file_upload'].includes(fieldType(field)) ? [] : '');
    }
    return values;
  }, {});
}

// surfaceSubmission 按组件类型把 A2UI 表单值归约为普通用户消息。
function surfaceSubmission(surface, values) {
  const fields = surfaceFields(surface).filter(isInputField);
  const items = fields.flatMap((field) => fieldSubmissionItems(field, values[fieldKey(field)]));
  if (!items.length) {
    return { content: '', displayContent: '', attachments: [] };
  }
  const ordered = orderSubmissionItems(items);
  const separator = ordered.every((item) => item.group === 'text') ? '\n' : '、';
  return {
    content: ordered.map((item) => item.content).filter(Boolean).join(separator),
    displayContent: ordered.map((item) => item.display || item.content).filter(Boolean).join(separator),
    attachments: ordered.map((item) => item.attachment).filter(Boolean)
  };
}

// fieldSubmissionItems 把单个字段值转换成可发送内容；选择器用选项文本，文件用 file_id。
function fieldSubmissionItems(field, value) {
  const type = fieldType(field);
  if (type === 'single_choice') {
    const label = choiceOptionLabel(field, value);
    return label ? [{ group: 'choice', content: label, display: label }] : [];
  }
  if (type === 'multi_choice') {
    const labels = (Array.isArray(value) ? value : []).map((item) => choiceOptionLabel(field, item)).filter(Boolean);
    const content = labels.join('、');
    return content ? [{ group: 'choice', content, display: content }] : [];
  }
  if (type === 'file_upload') {
    return fileUploadValueList(value)
      .map((asset) => {
        const id = fileAssetID(asset);
        if (!id) {
          return null;
        }
        return {
          group: 'file',
          content: id,
          display: fileAssetLabel(asset),
          attachment: asset
        };
      })
      .filter(Boolean);
  }
  const content = String(value ?? '').trim();
  return content ? [{ group: 'text', content, display: content }] : [];
}

// orderSubmissionItems 保证混合提交时先放选择项，再放文件，最后放用户输入文本。
function orderSubmissionItems(items) {
  const priority = { choice: 0, file: 1, text: 2 };
  return items
    .map((item, index) => ({ ...item, index }))
    .sort((left, right) => (priority[left.group] ?? 9) - (priority[right.group] ?? 9) || left.index - right.index);
}

// resolveMessageDisplay 把历史或本地消息中的 file_id 替换成用户可读文件预览。
function resolveMessageDisplay(message, assetMap) {
  const attachments = message.attachments?.length ? message.attachments : messageAssetAttachments(message.content, assetMap);
  let text = String(message.displayContent ?? message.content ?? '').trim();
  attachments.forEach((asset) => {
    const id = fileAssetID(asset);
    if (id) {
      text = text.split(id).join(fileAssetLabel(asset));
    }
  });
  return { text, attachments };
}

// choiceOptionLabel 优先返回选项 label，没有选项时返回原始值。
function choiceOptionLabel(field, value) {
  const normalized = String(value ?? '').trim();
  const option = (field?.options || []).find((item) => String(item?.value ?? '') === normalized);
  return String(option?.label ?? normalized);
}

// fileUploadValueList 归一化 FileUpload 字段值，兼容单文件和多文件。
function fileUploadValueList(value) {
  if (Array.isArray(value)) {
    return value.filter(Boolean);
  }
  return value ? [value] : [];
}

// normalizeUploadedAsset 保留后端返回的 asset 元数据，同时补足用户可见文件名。
function normalizeUploadedAsset(asset, file) {
  return {
    ...(asset || {}),
    id: asset?.id || asset?.asset_id || asset?.file_id || '',
    file_id: asset?.file_id || asset?.id || asset?.asset_id || '',
    filename: asset?.filename || file?.name || asset?.title || '上传文件',
    mime_type: asset?.mime_type || file?.type || ''
  };
}

// uploadKind 根据组件声明或文件 MIME 推断后端素材类型。
function uploadKind(field, file) {
  const explicit = String(field.kind || field.asset_kind || '').trim();
  if (explicit) {
    return explicit;
  }
  const type = String(file?.type || '').toLowerCase();
  if (type.startsWith('image/')) {
    return 'image';
  }
  if (type.startsWith('video/')) {
    return 'video';
  }
  if (type === 'application/pdf') {
    return 'pdf';
  }
  if (type.startsWith('text/')) {
    return 'text';
  }
  return '';
}

// fileAssetID 返回提交给 Agent 的 file_id。
function fileAssetID(asset) {
  if (typeof asset === 'string') {
    return asset.trim();
  }
  return String(asset?.file_id || asset?.id || asset?.asset_id || '').trim();
}

// fileAssetLabel 返回用户可见文件名，避免在聊天气泡里展示 file_id。
function fileAssetLabel(asset) {
  if (typeof asset === 'string') {
    return '已上传文件';
  }
  return String(asset?.filename || asset?.title || asset?.name || asset?.url?.split('/').pop() || '已上传文件').trim();
}

// messageAssetAttachments 从历史文本中识别已知 asset id，用于刷新后恢复缩略预览。
function messageAssetAttachments(content, assetMap) {
  if (!assetMap?.size) {
    return [];
  }
  const text = String(content || '');
  const assets = [];
  assetMap.forEach((asset, id) => {
    if (id && text.includes(id)) {
      assets.push(normalizeUploadedAsset(asset));
    }
  });
  return assets;
}

// isImageAsset 判断文件是否可用图片缩略图展示。
function isImageAsset(asset) {
  return String(asset?.kind || '').toLowerCase() === 'image' || String(asset?.mime_type || '').toLowerCase().startsWith('image/');
}

// isVideoAsset 判断文件是否可用视频预览展示。
function isVideoAsset(asset) {
  return String(asset?.kind || '').toLowerCase() === 'video' || String(asset?.mime_type || '').toLowerCase().startsWith('video/');
}

// componentMap 把组件数组索引成 id -> component 的 Map。
function componentMap(components) {
  const map = new Map();
  (components || []).forEach((component) => {
    if (component?.id) {
      map.set(component.id, component);
    }
  });
  return map;
}

// a2uiRootID 在未显式指定 root 时选择组件树默认根节点。
function a2uiRootID(components) {
  if (components.has('root-col')) {
    return 'root-col';
  }
  return components.keys().next().value || '';
}

// fieldKey 生成表单字段提交 key。
function fieldKey(field) {
  return field?.key || field?.name || field?.id || field?.label || '';
}

// fieldType 读取标准 A2UI 输入字段类型。
function fieldType(field) {
  const type = String(field?.type || field?.component || '').toLowerCase();
  if (type === 'single_choice') {
    return 'single_choice';
  }
  if (type === 'multi_choice') {
    return 'multi_choice';
  }
  if (type === 'file_upload') {
    return 'file_upload';
  }
  if (type === 'textarea' || field?.multiline) {
    return 'textarea';
  }
  if (type === 'image_preview') {
    return 'image_preview';
  }
  if (type === 'video_preview') {
    return 'video_preview';
  }
  if (type === 'vertical_steps') {
    return 'vertical_steps';
  }
  return 'text';
}

// fieldOptions 归一化单选/多选项结构。
function fieldOptions(field) {
  return (field?.options || []).map((option) => {
    if (typeof option === 'string') {
      return { label: option, value: option };
    }
    return {
      label: option.label || option.title || option.value,
      value: option.value || option.key || option.label || option.title
    };
  });
}

// isInputField 判断字段是否需要参与 A2UI 表单提交。
function isInputField(field) {
  return ['text', 'textarea', 'single_choice', 'multi_choice', 'file_upload'].includes(fieldType(field));
}

// defaultSelectedTarget 选择故事板中的首个可绑定目标。
function defaultSelectedTarget(board) {
  if (board?.key_elements?.[0]) {
    const element = board.key_elements[0];
    return { type: 'key_element', id: element.key, label: element.name || element.key };
  }
  if (board?.shots?.[0]) {
    const shot = board.shots[0];
    return { type: 'shot', id: shot.shot_id, field: 'keyframe_asset_id', label: `镜头 ${shot.index || 1}` };
  }
  return null;
}

// messageRecordToChatMessage 把后端消息记录转换成前端聊天消息。
function messageRecordToChatMessage(record) {
  const role = String(record?.role || '').toLowerCase();
  if (role !== 'user' && role !== 'assistant') {
    return null;
  }
  let content = String(record?.content || '').trim();
  if (!content) {
    return null;
  }
  return {
    id: record.id || `${role}-${record.seq || Date.now()}`,
    role,
    content
  };
}

// restoreHistoryFromMessageRecords 把历史消息拆成普通聊天气泡和 A2UI 卡片。
function restoreHistoryFromMessageRecords(records) {
  const messages = [];
  let surfaces = [];
  records.forEach((record, index) => {
    const timelineOrder = historyTimelineOrder(record, index);
    const submittedCardID = submittedA2UICardID(record);
    if (submittedCardID) {
      surfaces = removeSurfaceByID(surfaces, submittedCardID);
    }
    const envelope = parseA2UIActionEnvelopeContent(record?.content);
    if (envelope) {
      surfaces = restoreSurfacesFromEnvelope(surfaces, envelope, record, timelineOrder);
      return;
    }
    const message = messageRecordToChatMessage(record);
    if (message) {
      messages.push({ ...message, timelineOrder });
    }
  });
  const lastOrder = records.reduce((max, record, index) => Math.max(max, historyTimelineOrder(record, index)), 0);
  return {
    messages,
    surfaces,
    nextTimelineOrder: Math.max(lastOrder + 1, messages.length + surfaces.length)
  };
}

// parseA2UIActionEnvelopeContent 严格解析历史里的 Agent 直出 A2UI JSON。
function parseA2UIActionEnvelopeContent(content) {
  const value = String(content || '').trim();
  if (!value.startsWith('{') || !value.endsWith('}')) {
    return null;
  }
  try {
    const envelope = JSON.parse(value);
    if (envelope?.a2ui_version !== A2UI_VERSION || !Array.isArray(envelope.actions) || envelope.actions.length === 0) {
      return null;
    }
    return envelope;
  } catch {
    return null;
  }
}

// restoreSurfacesFromEnvelope 回放历史 ActionEnvelope 中的聊天卡新增和更新。
function restoreSurfacesFromEnvelope(surfaces, envelope, record, timelineOrder) {
  return envelope.actions.reduce((items, action, actionIndex) => {
    const type = String(action?.type || '').trim();
    if (type === A2UI_ACTIONS.APPEND_CARD) {
      const card = normalizeActionCard(action);
      const cardID = action.card_id || card.card_id || action.ref || '';
      if (!cardID) {
        return items;
      }
      return upsertSurface(items, {
        id: cardID,
        surface_id: cardID,
        card_id: cardID,
        message_id: action.message_id || record?.id,
        ref: action.ref,
        surface: action.surface || 'chat',
        payload: card,
        timelineOrder: timelineOrder + actionIndex / 100
      });
    }
    if (type === A2UI_ACTIONS.UPDATE_CARD) {
      const target = action.target || {};
      const targetID = target.card_id || action.card_id || target.ref || action.ref;
      if (!targetID) {
        return items;
      }
      return items.map((surface) => {
        if (surface.id !== targetID && surface.card_id !== targetID && surface.ref !== targetID) {
          return surface;
        }
        const payload = applyCardPatch({ ...(surface.payload || {}), ...normalizeActionCard(action) }, action.patch || []);
        return mergeSurface(surface, { ...surface, payload });
      });
    }
    return items;
  }, surfaces);
}

// submittedA2UICardID 读取用户消息中用于关闭临时 A2UI 表单的实例级 card_id。
function submittedA2UICardID(record) {
  const source = record?.metadata?.ui_source;
  if (source?.type !== 'a2ui_submit') {
    return '';
  }
  return String(source.card_id || source.cardID || '').trim();
}

// removeSurfaceByID 从历史回放结果中移除已提交的临时表单实例。
function removeSurfaceByID(surfaces, cardID) {
  return surfaces.filter((surface) => surface.id !== cardID && surface.card_id !== cardID);
}

// historyTimelineOrder 使用数据库 seq 恢复刷新前后的消息顺序。
function historyTimelineOrder(record, fallback) {
  const seq = Number(record?.seq);
  return Number.isFinite(seq) && seq > 0 ? seq - 1 : fallback;
}

// statusLabel 返回状态中文文案，未知状态原样展示。
function statusLabel(status) {
  return statusLabels[status] || status || '';
}

// errorMessage 提取用户可读错误文案。
function errorMessage(err) {
  return err?.message || '请求失败';
}
