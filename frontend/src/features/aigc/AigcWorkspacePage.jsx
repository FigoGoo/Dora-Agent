import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  AlertCircle,
  ArrowUp,
  Bot,
  CheckCircle2,
  Clapperboard,
  Clock3,
  Film,
  Image,
  LoaderCircle,
  MessageCircle,
  Music,
  Save,
  Sparkles,
  UserCircle
} from 'lucide-react';
import { BrandLogo } from '../../components/brand/BrandLogo.jsx';

const SESSION_STORAGE_KEY = 'dora:aigc:demo_session_id';

const EVENT_NAMES = [
  'chat.delta',
  'chat.message',
  'tool.progress',
  'a2ui.begin_rendering',
  'a2ui.surface_update',
  'a2ui.data_model_update',
  'a2ui.interrupt_request',
  'storyboard.snapshot',
  'storyboard.patch',
  'job.status',
  'skill.selected',
  'error'
];

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

export function AigcWorkspacePage() {
  const [sessionID, setSessionID] = useState('');
  const [storyboard, setStoryboard] = useState(null);
  const [assets, setAssets] = useState([]);
  const [jobs, setJobs] = useState([]);
  const [messages, setMessages] = useState([
    {
      id: 'welcome',
      role: 'assistant',
      content: '把剧本、风格或 Skill.md 发给我，我会先规划规格和故事板。'
    }
  ]);
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [interrupt, setInterrupt] = useState(null);
  const [surfaces, setSurfaces] = useState([]);
  const [a2uiData, setA2UIData] = useState({});
  const [selectedTarget, setSelectedTarget] = useState(null);
  const [editing, setEditing] = useState(null);
  const [autoSkill, setAutoSkill] = useState(null); // {name, reason, fallback}
  const [leftView, setLeftView] = useState('storyboard'); // 'storyboard' | 'docs'
  const [docSpec, setDocSpec] = useState(null);
  const [docSkill, setDocSkill] = useState(null);
  const [activeDoc, setActiveDoc] = useState('spec'); // 'spec' | 'skill'
  const skillInputRef = useRef(null);
  const streamingAssistantID = useRef('');

  const assetMap = useMemo(() => {
    return assets.reduce((map, asset) => {
      const id = asset.id || asset.asset_id;
      if (id) {
        map.set(id, asset);
      }
      return map;
    }, new Map());
  }, [assets]);

  const refreshAssets = useCallback(async (id) => {
    if (!id) {
      return;
    }
    const result = await requestOptionalJSON(`/api/aigc/sessions/${id}/assets`);
    if (result?.assets) {
      setAssets(result.assets);
    }
  }, []);

  const refreshSessionData = useCallback(
    async (id) => {
      if (!id) {
        return;
      }
      const [boardResult, assetsResult, jobsResult, messagesResult] = await Promise.allSettled([
        requestOptionalJSON(`/api/aigc/sessions/${id}/storyboard`),
        requestOptionalJSON(`/api/aigc/sessions/${id}/assets`),
        requestOptionalJSON(`/api/aigc/sessions/${id}/jobs`),
        requestOptionalJSON(`/api/aigc/sessions/${id}/messages`)
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
      if (messagesResult.status === 'fulfilled' && messagesResult.value?.messages?.length) {
        const visibleMessages = messagesResult.value.messages.map(messageRecordToChatMessage).filter(Boolean);
        if (visibleMessages.length) {
          setMessages(visibleMessages);
        }
      }
      const rejected = [boardResult, assetsResult, jobsResult, messagesResult].find((result) => result.status === 'rejected');
      if (rejected) {
        throw rejected.reason;
      }
    },
    []
  );

  const loadDocuments = useCallback(async () => {
    if (!sessionID) {
      return;
    }
    try {
      setDocSpec(await requestOptionalJSON(`/api/aigc/sessions/${sessionID}/spec`));
    } catch {
      setDocSpec(null);
    }
    try {
      setDocSkill(await requestOptionalJSON(`/api/aigc/sessions/${sessionID}/skill`));
    } catch {
      setDocSkill(null);
    }
  }, [sessionID]);

  const appendAssistantDelta = useCallback((text) => {
    if (!text) {
      return;
    }
    let messageID = streamingAssistantID.current;
    if (!messageID) {
      messageID = `assistant-${Date.now()}`;
      streamingAssistantID.current = messageID;
      setMessages((items) => [...items, { id: messageID, role: 'assistant', content: '', streaming: true }]);
    }
    setMessages((items) =>
      items.map((message) =>
        message.id === messageID ? { ...message, content: `${message.content}${text}`, streaming: true } : message
      )
    );
  }, []);

  const finishAssistantMessage = useCallback(() => {
    const messageID = streamingAssistantID.current;
    streamingAssistantID.current = '';
    if (!messageID) {
      return;
    }
    setMessages((items) => items.map((message) => (message.id === messageID ? { ...message, streaming: false } : message)));
  }, []);

  const addSystemMessage = useCallback((content) => {
    if (!content) {
      return;
    }
    setMessages((items) => [...items, { id: `event-${Date.now()}-${items.length}`, role: 'system', content }]);
  }, []);

  const handleA2UIEvent = useCallback(
    (event) => {
      const applyProtocolEvent = (protocolEvent) => {
        const protocolName = protocolEvent?.event;
        const protocolPayload = protocolEvent?.payload || {};
        if (protocolName === 'a2ui.begin_rendering') {
          if (protocolPayload.surfaceId || protocolPayload.surface_id || protocolPayload.root) {
            const surfaceID = protocolEvent?.surface_id || protocolPayload.surface_id || protocolPayload.surfaceId || `surface-${Date.now()}`;
            setSurfaces((items) =>
              upsertSurface(items, {
                id: surfaceID,
                surface_id: surfaceID,
                root: protocolPayload.root,
                payload: protocolPayload
              })
            );
            return true;
          }
          const notice = noticeSurfaceFromProtocol(protocolName, protocolPayload);
          if (notice) {
            setSurfaces((items) => upsertSurface(items, notice));
          }
          return true;
        }
        if (protocolName === 'a2ui.surface_update') {
          if (isRenderableSurface(protocolPayload)) {
            const surfaceID =
              protocolEvent?.surface_id || protocolEvent?.surfaceID || protocolPayload.surface_id || protocolPayload.id || `surface-${Date.now()}`;
            setSurfaces((items) =>
              upsertSurface(items, {
                id: surfaceID,
                surface_id: surfaceID,
                data_model_key: protocolEvent?.data_model_key || protocolEvent?.dataModelKey || protocolPayload.data_model_key,
                payload: protocolPayload
              })
            );
            return true;
          }
          const notice = noticeSurfaceFromProtocol(protocolName, protocolPayload);
          if (notice) {
            setSurfaces((items) => upsertSurface(items, notice));
          }
          return true;
        }
        if (protocolName === 'a2ui.interrupt_request') {
          setInterrupt(protocolPayload);
          addSystemMessage(protocolPayload.message || protocolPayload.title || '需要确认后继续。');
          return true;
        }
        if (protocolName === 'storyboard.snapshot') {
          setStoryboard(protocolPayload.storyboard || protocolPayload);
          return true;
        }
        if (protocolName === 'storyboard.patch') {
          setStoryboard((current) => applyStoryboardPatch(current, protocolPayload));
          return true;
        }
        if (protocolName === 'job.status') {
          setJobs((items) => upsertByID(items, protocolPayload, 'job_id'));
          if (protocolPayload.status === 'succeeded' && protocolPayload.result_asset_ids?.length) {
            void refreshAssets(protocolPayload.session_id || sessionID);
          }
          return true;
        }
        if (protocolName === 'skill.selected') {
          setAutoSkill({
            name: protocolPayload.skill_name || protocolPayload.skill_id || '',
            reason: protocolPayload.reason || '',
            fallback: Boolean(protocolPayload.fallback)
          });
          return true;
        }
        if (protocolName === 'a2ui.data_model_update') {
          if (protocolPayload.storyboard) {
            setStoryboard(protocolPayload.storyboard);
          }
          if (protocolPayload.assets) {
            setAssets(protocolPayload.assets);
          }
          if (Array.isArray(protocolPayload.contents)) {
            const surfaceID = protocolEvent?.surface_id || protocolPayload.surface_id || protocolPayload.surfaceId || 'default';
            setA2UIData((current) => applyA2UIDataUpdate(current, surfaceID, protocolPayload.contents));
          }
          return true;
        }
        return false;
      };

      const eventName = event?.event;
      const payload = event?.payload || {};
      if (eventName === 'chat.delta') {
        const parsed = extractA2UIEnvelopeContent(payload.text || payload.delta || '');
        if (parsed.events.length) {
          appendAssistantDelta(parsed.displayText);
          parsed.events.forEach(applyProtocolEvent);
          return;
        }
        appendAssistantDelta(parsed.displayText);
        return;
      }
      if (eventName === 'chat.message') {
        const parsed = extractA2UIEnvelopeContent(payload.text || payload.content || '');
        if (parsed.events.length) {
          addSystemMessage(parsed.displayText);
          parsed.events.forEach(applyProtocolEvent);
          return;
        }
        addSystemMessage(parsed.displayText);
        return;
      }
      if (eventName === 'tool.progress') {
        setSurfaces((items) => upsertSurface(items, toolProgressSurfaceFromPayload(payload)));
        return;
      }
      if (applyProtocolEvent(event)) {
        return;
      }
      if (eventName === 'error') {
        const message = payload.message || '请求失败';
        setError(message);
        addSystemMessage(message);
      }
    },
    [addSystemMessage, appendAssistantDelta, refreshAssets, sessionID]
  );

  useEffect(() => {
    let cancelled = false;

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
    const listeners = EVENT_NAMES.map((eventName) => {
      const listener = (event) => handleA2UIEvent(parseSSEEvent(event));
      source.addEventListener(eventName, listener);
      return [eventName, listener];
    });
    source.onerror = () => setError('事件流已断开，刷新页面可重新连接。');
    return () => {
      listeners.forEach(([eventName, listener]) => source.removeEventListener(eventName, listener));
      source.close();
    };
  }, [handleA2UIEvent, sessionID]);

  async function sendMessage(nextContent) {
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
    setMessages((items) => [...items, { id: `user-${Date.now()}`, role: 'user', content }]);
    let sent = false;
    try {
      const response = await fetch(`/api/aigc/sessions/${sessionID}/messages/stream`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content })
      });
      await readSSEStream(response, handleA2UIEvent);
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

  async function submitSurface(surface, values) {
    const content = surfaceSubmitContent(surface, values);
    if (!content) {
      return;
    }
    const sent = await sendMessage(content);
    if (sent) {
      setSurfaces((items) => items.filter((item) => item.id !== surface.id));
    }
  }

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
        if (result?.event) {
          handleA2UIEvent({ event: result.event, payload: result.payload });
        }
        await refreshSessionData(sessionID);
        return;
      }

      const response = await fetch(`/api/aigc/sessions/${sessionID}/messages/resume/stream`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          checkpoint_id: interrupt.checkpoint_id,
          interrupt_id: interrupt.interrupt_id,
          content: label,
          data: { approved, action_key: action?.key, note: label }
        })
      });
      setInterrupt(null);
      await readSSEStream(response, handleA2UIEvent);
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

  const keyElements = storyboard?.key_elements || [];
  const shots = storyboard?.shots || [];
  const audioLayers = storyboard?.audio_layers || [];
  const title = storyboard?.spec_id ? storyboard.spec_id : 'AIGC 创作工作台';

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
        <section className="aigc-storyboard-pane" aria-label="故事板">
          <div className="aigc-pane-header">
            <div>
              <Clapperboard aria-hidden="true" size={17} />
              <strong>故事板</strong>
            </div>
            <span>{statusLabel(storyboard?.status) || '未生成'}</span>
          </div>

          <div className="aigc-left-tabs" role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected={leftView === 'storyboard'}
              onClick={() => setLeftView('storyboard')}
            >
              故事板
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={leftView === 'docs'}
              onClick={() => {
                setLeftView('docs');
                void loadDocuments();
              }}
            >
              文档
            </button>
          </div>

          {leftView === 'docs' && (
            <div className="aigc-docs-pane">
              <div className="aigc-docs-tabs" role="tablist">
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeDoc === 'spec'}
                  onClick={() => setActiveDoc('spec')}
                >
                  Final_Video_Spec.md
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeDoc === 'skill'}
                  onClick={() => setActiveDoc('skill')}
                >
                  skill.md
                </button>
              </div>
              <pre className="aigc-doc-content">
                {activeDoc === 'spec'
                  ? docSpec?.markdown || '尚未生成 Final Video Spec。'
                  : docSkill?.bound
                    ? docSkill.content || ''
                    : '当前会话未绑定 Skill。'}
              </pre>
            </div>
          )}

          {leftView === 'storyboard' && (
          <>
          <div className="aigc-selected-target">
            <span>当前绑定</span>
            <strong>{selectedTarget?.label || '选择故事板项'}</strong>
          </div>

          <StoryboardSection title="关键元素" count={keyElements.length}>
            {keyElements.map((element, index) => {
              const selected = selectedTarget?.type === 'key_element' && selectedTarget.id === element.key;
              return (
                <button
                  className={selected ? 'aigc-story-item is-selected' : 'aigc-story-item'}
                  type="button"
                  key={element.key}
                  onClick={() =>
                    setSelectedTarget({ type: 'key_element', id: element.key, label: element.name || element.key })
                  }
                >
                  <div className="aigc-story-item__icon">
                    <Image aria-hidden="true" size={16} />
                  </div>
                  <div className="aigc-story-item__body">
                    <InlineEditable
                      editing={editing}
                      path={`/key_elements/${index}/name`}
                      value={element.name || element.key}
                      onStart={setEditing}
                      onChange={setEditing}
                      onSave={saveEdit}
                    />
                    <p>{element.description || element.prompt || '待补充描述'}</p>
                    <AssetStrip ids={element.asset_ids} assetMap={assetMap} />
                  </div>
                  <StatusPill status={element.status} />
                </button>
              );
            })}
          </StoryboardSection>

          <StoryboardSection title="分镜" count={shots.length}>
            {shots.map((shot, index) => {
              const selected = selectedTarget?.type === 'shot' && selectedTarget.id === shot.shot_id;
              return (
                <button
                  className={selected ? 'aigc-shot-item is-selected' : 'aigc-shot-item'}
                  type="button"
                  key={shot.shot_id}
                  onClick={() =>
                    setSelectedTarget({
                      type: 'shot',
                      id: shot.shot_id,
                      field: 'keyframe_asset_id',
                      label: `镜头 ${shot.index || index + 1}`
                    })
                  }
                >
                  <div className="aigc-shot-preview">
                    <AssetImage asset={assetMap.get(shot.keyframe_asset_id)} fallback={<Film aria-hidden="true" size={20} />} />
                  </div>
                  <div className="aigc-shot-copy">
                    <div className="aigc-shot-copy__meta">
                      <strong>镜头 {shot.index || index + 1}</strong>
                      <span>{shot.duration_sec ? `${shot.duration_sec}s` : statusLabel(shot.status) || '规划中'}</span>
                    </div>
                    <InlineEditable
                      multiline
                      editing={editing}
                      path={`/shots/${index}/scene_description`}
                      value={shot.scene_description || shot.prompt || '待补充分镜描述'}
                      onStart={setEditing}
                      onChange={setEditing}
                      onSave={saveEdit}
                    />
                    <p>{shot.camera_design || shot.narration || '镜头设计待完善'}</p>
                  </div>
                </button>
              );
            })}
          </StoryboardSection>

          <StoryboardSection title="旁白与音乐" count={audioLayers.length}>
            {audioLayers.map((layer, index) => {
              const selected = selectedTarget?.type === 'audio_layer' && selectedTarget.id === layer.layer_id;
              return (
                <button
                  className={selected ? 'aigc-story-item is-selected' : 'aigc-story-item'}
                  type="button"
                  key={layer.layer_id}
                  onClick={() =>
                    setSelectedTarget({ type: 'audio_layer', id: layer.layer_id, label: layer.type || `音频 ${index + 1}` })
                  }
                >
                  <div className="aigc-story-item__icon">
                    <Music aria-hidden="true" size={16} />
                  </div>
                  <div className="aigc-story-item__body">
                    <strong>{layer.type || `音频 ${index + 1}`}</strong>
                    <p>{layer.description || layer.prompt || '待规划音频层'}</p>
                  </div>
                  <StatusPill status={layer.status} />
                </button>
              );
            })}
          </StoryboardSection>
          </>
          )}
        </section>

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
            {autoSkill && (
              <div className="aigc-auto-skill-notice" role="status">
                🧭 已为你自动选择 Skill：<strong>{autoSkill.name}</strong>
                {autoSkill.reason ? `——${autoSkill.reason}` : ''}
                {autoSkill.fallback ? '（回落默认）' : ''}
              </div>
            )}
            {messages.map((message) => (
              <article className={`aigc-message aigc-message--${message.role}`} key={message.id}>
                <div className="aigc-message__avatar">
                  {message.role === 'user' ? <UserCircle aria-hidden="true" size={18} /> : <Bot aria-hidden="true" size={18} />}
                </div>
                <p>{message.content || (message.streaming ? '...' : '')}</p>
              </article>
            ))}
            {surfaces.filter(isVisibleSurface).map((surface) => (
              <A2UISurfaceCard surface={surface} data={a2uiData[surface.id] || {}} busy={busy} onSubmit={submitSurface} key={surface.id} />
            ))}
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

function StoryboardSection({ title, count, children }) {
  return (
    <section className="aigc-story-section">
      <header>
        <strong>{title}</strong>
        <span>{count}</span>
      </header>
      <div>{count > 0 ? children : <p className="aigc-empty">等待生成</p>}</div>
    </section>
  );
}

function InlineEditable({ editing, path, value, multiline = false, onStart, onChange, onSave }) {
  const active = editing?.path === path;
  if (!active) {
    return (
      <span className="aigc-inline-edit" onClick={() => onStart({ path, value })} onKeyDown={(event) => {
        if (event.key === 'Enter') {
          onStart({ path, value });
        }
      }} role="button" tabIndex={0}>
        {value}
      </span>
    );
  }
  const Input = multiline ? 'textarea' : 'input';
  return (
    <span className="aigc-inline-editor" onClick={(event) => event.stopPropagation()}>
      <Input
        value={editing.value}
        rows={multiline ? 2 : undefined}
        onChange={(event) => onChange({ ...editing, value: event.target.value })}
      />
      <button type="button" onClick={onSave} aria-label="保存修改">
        <Save aria-hidden="true" size={14} />
      </button>
    </span>
  );
}

function AssetStrip({ ids, assetMap }) {
  if (!ids?.length) {
    return null;
  }
  return (
    <div className="aigc-asset-strip">
      {ids.slice(0, 4).map((id) => {
        const asset = assetMap.get(id);
        return <AssetImage asset={asset} fallback={<span>{id.slice(0, 4)}</span>} key={id} />;
      })}
    </div>
  );
}

function AssetImage({ asset, fallback = null }) {
  const [failed, setFailed] = useState(false);
  if (!asset?.url || failed) {
    return fallback;
  }
  return <img src={asset.url} alt="" loading="lazy" onError={() => setFailed(true)} />;
}

function StatusPill({ status }) {
  const label = statusLabel(status);
  if (!label) {
    return null;
  }
  return <span className={`aigc-status-pill aigc-status-pill--${status}`}>{label}</span>;
}

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

function A2UISurfaceCard({ surface, data, busy, onSubmit }) {
  const payload = surface?.payload || {};
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
                onChange={(value) => setValues((current) => ({ ...current, [fieldKey(field)]: value }))}
                key={fieldKey(field)}
              />
            ))
          : null}
        {hasInteractiveFields || payload.submit_label ? (
          <button type="submit" disabled={busy || !hasInteractiveFields}>
            <CheckCircle2 aria-hidden="true" size={15} />
            <span>{payload.submit_label || '提交'}</span>
          </button>
        ) : null}
      </form>
    </article>
  );
}

function A2UIComponentTree({ surface, data, values, onValueChange }) {
  const payload = surface?.payload || {};
  const components = componentMap(payload.components);
  const root = payload.root || surface.root || payload.root_id || a2uiRootID(components);
  if (!root) {
    return null;
  }
  return (
    <div className="aigc-a2ui-tree">
      <A2UIComponent id={root} components={components} data={data} values={values} onValueChange={onValueChange} />
    </div>
  );
}

function A2UIComponent({ id, components, data, values, onValueChange }) {
  const node = components.get(id);
  if (!node) {
    return null;
  }
  const component = node.component || node;
  const text = component.Text || component.text;
  const column = component.Column || component.column;
  const row = component.Row || component.row;
  const card = component.Card || component.card;
  const textInput = component.TextInput || component.textInput || component.text_input;
  const singleChoice = component.SingleChoice || component.singleChoice || component.single_choice || component.Radio || component.radio;
  const multiChoice = component.MultiChoice || component.multiChoice || component.multi_choice || component.CheckboxGroup || component.checkbox_group;
  const imagePreview = component.ImagePreview || component.imagePreview || component.image_preview;
  const videoPreview = component.VideoPreview || component.videoPreview || component.video_preview;
  const verticalSteps = component.VerticalSteps || component.verticalSteps || component.vertical_steps;

  if (text) {
    const value = text.dataKey ? data[text.dataKey] || '' : text.value || '';
    const className = `aigc-a2ui-text aigc-a2ui-text--${text.usageHint || 'body'}`;
    return text.usageHint === 'title' ? <h2 className={className}>{value}</h2> : <p className={className}>{value}</p>;
  }
  if (column) {
    return (
      <div className="aigc-a2ui-column">
        {(column.children || []).map((childID) => (
          <A2UIComponent id={childID} components={components} data={data} values={values} onValueChange={onValueChange} key={childID} />
        ))}
      </div>
    );
  }
  if (row) {
    return (
      <div className="aigc-a2ui-row">
        {(row.children || []).map((childID) => (
          <A2UIComponent id={childID} components={components} data={data} values={values} onValueChange={onValueChange} key={childID} />
        ))}
      </div>
    );
  }
  if (card) {
    return (
      <div className="aigc-a2ui-inner-card">
        {(card.children || []).map((childID) => (
          <A2UIComponent id={childID} components={components} data={data} values={values} onValueChange={onValueChange} key={childID} />
        ))}
      </div>
    );
  }
  if (textInput) {
    return <A2UIField field={{ ...textInput, type: textInput.multiline ? 'textarea' : 'text' }} value={values[fieldKey(textInput)]} onChange={(value) => onValueChange(fieldKey(textInput), value)} />;
  }
  if (singleChoice) {
    return <A2UIField field={{ ...singleChoice, type: 'single_choice' }} value={values[fieldKey(singleChoice)]} onChange={(value) => onValueChange(fieldKey(singleChoice), value)} />;
  }
  if (multiChoice) {
    return <A2UIField field={{ ...multiChoice, type: 'multi_choice' }} value={values[fieldKey(multiChoice)]} onChange={(value) => onValueChange(fieldKey(multiChoice), value)} />;
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
  return null;
}

function A2UIField({ field, value, onChange }) {
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

function A2UIImagePreview({ item }) {
  const url = item.url || item.src || item.image_url;
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

function A2UIVideoPreview({ item }) {
  const url = item.url || item.src || item.video_url;
  if (!url) {
    return null;
  }
  return (
    <figure className="aigc-a2ui-media">
      <video src={url} poster={item.poster || item.poster_url || ''} controls preload="metadata" />
      {item.title || item.caption ? <figcaption>{item.title || item.caption}</figcaption> : null}
    </figure>
  );
}

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

async function resolveSessionID() {
  const params = new URLSearchParams(window.location.search);
  const explicit = params.get('session_id');
  const cached = readLocalSessionID();
  if (explicit || cached) {
    return explicit || cached;
  }
  const session = await requestJSON('/api/aigc/sessions', {
    method: 'POST',
    body: JSON.stringify({
      user_id: 'demo-user',
      title: 'AIGC Demo'
    })
  });
  writeLocalSessionID(session.id);
  return session.id;
}

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

function writeLocalSessionID(sessionID) {
  try {
    if (typeof window.localStorage?.setItem === 'function') {
      window.localStorage.setItem(SESSION_STORAGE_KEY, sessionID);
    }
  } catch {
    // Session creation still succeeds when browser storage is unavailable.
  }
}

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

async function readSSEStream(response, onEvent) {
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || response.statusText);
  }
  if (!response.body) {
    parseSSEText(await response.text(), onEvent);
    return;
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    const parts = buffer.split(/\n\n/);
    buffer = parts.pop() || '';
    parts.forEach((block) => emitSSEBlock(block, onEvent));
  }
  buffer += decoder.decode();
  if (buffer.trim()) {
    emitSSEBlock(buffer, onEvent);
  }
}

function parseSSEText(text, onEvent) {
  text.split(/\n\n/).forEach((block) => emitSSEBlock(block, onEvent));
}

function emitSSEBlock(block, onEvent) {
  const lines = block.split(/\r?\n/);
  const data = [];
  let eventName = 'message';
  lines.forEach((line) => {
    if (line.startsWith('event:')) {
      eventName = line.slice(6).trim();
    }
    if (line.startsWith('data:')) {
      data.push(line.slice(5).trim());
    }
  });
  if (!data.length) {
    return;
  }
  const event = JSON.parse(data.join('\n'));
  if (!event.event) {
    event.event = eventName;
  }
  onEvent(event);
}

function parseSSEEvent(event) {
  try {
    return JSON.parse(event.data);
  } catch {
    return { event: event.type, payload: { message: event.data } };
  }
}

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

function decodePointerToken(token) {
  return token.replace(/~1/g, '/').replace(/~0/g, '~');
}

function cloneJSON(value) {
  return JSON.parse(JSON.stringify(value));
}

function upsertByID(items, item, key) {
  const id = item?.[key] || item?.id;
  if (!id) {
    return items;
  }
  const next = items.filter((existing) => (existing?.[key] || existing?.id) !== id);
  return [item, ...next];
}

function upsertSurface(items, surface) {
  if (!surface?.id) {
    return items;
  }
  const existing = items.find((item) => item.id === surface.id);
  const next = items.filter((item) => item.id !== surface.id);
  const merged = existing ? mergeSurface(existing, surface) : surface;
  return [...next, merged];
}

function isRenderableSurface(payload) {
  return payload?.component === 'form' || Array.isArray(payload?.fields) || Array.isArray(payload?.components);
}

function isVisibleSurface(surface) {
  const payload = surface?.payload || {};
  return Boolean(payload.title || payload.message || payload.status || payload.component === 'form' || payload.fields?.length || payload.components?.length);
}

function noticeSurfaceFromProtocol(eventName, payload) {
  const text = a2UIProgressText(eventName, payload || {});
  if (!text) {
    return null;
  }
  const baseID = payload?.surface_id || payload?.surfaceId || payload?.surface || 'a2ui-notice';
  const surfaceID = `${baseID}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return {
    id: surfaceID,
    surface_id: surfaceID,
    payload: {
      root: 'notice-root',
      title: payload?.title || payload?.surface || '状态更新',
      status: payload?.status,
      components: [
        { id: 'notice-root', component: { Card: { children: ['notice-title', 'notice-text'] } } },
        { id: 'notice-title', component: { Text: { value: payload?.title || payload?.surface || '状态更新', usageHint: 'title' } } },
        { id: 'notice-text', component: { Text: { value: text, usageHint: 'body' } } }
      ]
    }
  };
}

function toolProgressSurfaceFromPayload(payload) {
  const text = toolProgressText(payload || {});
  return {
    id: 'agent-progress',
    surface_id: 'agent-progress',
    payload: {
      root: 'progress-root',
      title: '执行进度',
      status: payload?.status || 'running',
      components: [
        { id: 'progress-root', component: { Card: { children: ['progress-title', 'progress-text', 'progress-steps'] } } },
        { id: 'progress-title', component: { Text: { value: '执行进度', usageHint: 'title' } } },
        { id: 'progress-text', component: { Text: { value: text, usageHint: 'body' } } },
        {
          id: 'progress-steps',
          component: {
            VerticalSteps: {
              steps: [
                { title: 'Agent 分析', status: 'done' },
                { title: text, status: payload?.status === 'failed' ? 'failed' : 'running' }
              ]
            }
          }
        }
      ]
    }
  };
}

function mergeSurface(existing, incoming) {
  const payload = { ...(existing.payload || {}), ...(incoming.payload || {}) };
  if (Array.isArray(existing.payload?.components) || Array.isArray(incoming.payload?.components)) {
    payload.components = mergeComponents(existing.payload?.components || [], incoming.payload?.components || []);
  }
  return {
    ...existing,
    ...incoming,
    root: incoming.root || incoming.payload?.root || existing.root || existing.payload?.root,
    payload
  };
}

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

function applyA2UIDataUpdate(current, surfaceID, contents) {
  const surfaceData = { ...(current[surfaceID] || {}) };
  contents.forEach((item) => {
    const key = item.key || item.dataKey;
    if (!key) {
      return;
    }
    surfaceData[key] = item.valueString ?? item.value ?? item.text ?? '';
  });
  return { ...current, [surfaceID]: surfaceData };
}

function surfaceFields(surface) {
  const payload = surface?.payload || {};
  if (Array.isArray(payload.fields)) {
    return payload.fields;
  }
  if (!Array.isArray(payload.components)) {
    return [];
  }
  return payload.components.map(componentField).filter(Boolean);
}

function componentField(node) {
  const component = node?.component || node || {};
  const textInput = component.TextInput || component.textInput || component.text_input;
  const singleChoice = component.SingleChoice || component.singleChoice || component.single_choice || component.Radio || component.radio;
  const multiChoice = component.MultiChoice || component.multiChoice || component.multi_choice || component.CheckboxGroup || component.checkbox_group;
  if (textInput) {
    return { ...textInput, type: textInput.multiline ? 'textarea' : 'text' };
  }
  if (singleChoice) {
    return { ...singleChoice, type: 'single_choice' };
  }
  if (multiChoice) {
    return { ...multiChoice, type: 'multi_choice' };
  }
  return null;
}

function initialSurfaceValues(fields) {
  return fields.reduce((values, field) => {
    const key = field.key || field.name || field.label;
    if (key) {
      values[key] = field.value || (fieldType(field) === 'multi_choice' ? [] : '');
    }
    return values;
  }, {});
}

function surfaceSubmitContent(surface, values) {
  const payload = surface?.payload || {};
  const fields = surfaceFields(surface).filter(isInputField);
  const lines = fields
    .map((field) => {
      const key = fieldKey(field);
      const raw = values[key];
      const value = Array.isArray(raw) ? labelsForValues(field, raw).join('、') : labelForValue(field, raw);
      if (!key || !value) {
        return '';
      }
      return `- ${field.label || key}：${value}`;
    })
    .filter(Boolean);
  if (!lines.length) {
    return '';
  }
  return `${payload.title || '补充信息'}：\n${lines.join('\n')}`;
}

function componentMap(components) {
  const map = new Map();
  (components || []).forEach((component) => {
    if (component?.id) {
      map.set(component.id, component);
    }
  });
  return map;
}

function a2uiRootID(components) {
  if (components.has('root-col')) {
    return 'root-col';
  }
  return components.keys().next().value || '';
}

function fieldKey(field) {
  return field?.key || field?.name || field?.id || field?.label || '';
}

function fieldType(field) {
  const type = String(field?.type || field?.component || '').toLowerCase();
  if (['single_choice', 'singlechoice', 'radio', 'select'].includes(type)) {
    return 'single_choice';
  }
  if (['multi_choice', 'multichoice', 'checkbox', 'checkbox_group', 'checkboxgroup'].includes(type)) {
    return 'multi_choice';
  }
  if (['textarea', 'multiline'].includes(type) || field?.multiline) {
    return 'textarea';
  }
  if (['image_preview', 'imagepreview', 'image'].includes(type)) {
    return 'image_preview';
  }
  if (['video_preview', 'videopreview', 'video'].includes(type)) {
    return 'video_preview';
  }
  if (['vertical_steps', 'verticalsteps', 'steps'].includes(type)) {
    return 'vertical_steps';
  }
  return 'text';
}

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

function labelsForValues(field, values) {
  const labels = new Map(fieldOptions(field).map((option) => [option.value, option.label]));
  return values.map((value) => labels.get(value) || value);
}

function labelForValue(field, value) {
  const raw = String(value || '').trim();
  if (!raw) {
    return '';
  }
  return new Map(fieldOptions(field).map((option) => [option.value, option.label])).get(raw) || raw;
}

function isInputField(field) {
  return ['text', 'textarea', 'single_choice', 'multi_choice'].includes(fieldType(field));
}

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

function messageRecordToChatMessage(record) {
  const role = String(record?.role || '').toLowerCase();
  if (role !== 'user' && role !== 'assistant') {
    return null;
  }
  let content = String(record?.content || '').trim();
  if (!content) {
    return null;
  }
  if (role === 'assistant') {
    const parsed = extractA2UIEnvelopeContent(content);
    if (parsed.events.length) {
      content = parsed.displayText.trim();
      if (!content) {
        return null;
      }
    }
    if (isA2UIDrivenAssistantPrompt(content)) {
      return null;
    }
  }
  return {
    id: record.id || `${role}-${record.seq || Date.now()}`,
    role,
    content
  };
}

function isA2UIDrivenAssistantPrompt(content) {
  const raw = String(content || '');
  const lower = raw.toLowerCase();
  if (!raw.trim()) {
    return false;
  }
  const productBrief =
    (raw.includes('产品是什么') || raw.includes('产品名称') || raw.includes('核心卖点')) &&
    (raw.includes('目标平台') || raw.includes('视觉风格')) &&
    (raw.includes('告诉我') || raw.includes('提供') || raw.includes('补充') || raw.includes('填写'));
  const stageConfirmation =
    (raw.includes('请您确认') || raw.includes('请确认') || raw.includes('是否符合')) &&
    (lower.includes('final_video_spec') || raw.includes('视频规格') || raw.includes('故事板') || raw.includes('参考图') || raw.includes('关键帧'));
  return productBrief || stageConfirmation;
}

function isA2UIEnvelopeContent(content) {
  const parsed = extractA2UIEnvelopeContent(content);
  return parsed.events.length > 0 && !parsed.displayText.trim();
}

function extractA2UIEnvelopeContent(content) {
  const raw = String(content || '').trim();
  if (!raw) {
    return { events: [], displayText: '' };
  }
  const directEvents = parseA2UIEnvelopeJSON(stripJSONFence(raw));
  if (directEvents.length) {
    return { events: directEvents, displayText: '' };
  }

  const embedded = findEmbeddedA2UIEnvelope(raw);
  if (!embedded) {
    return { events: [], displayText: raw };
  }
  return {
    events: embedded.events,
    displayText: joinDisplayTextAroundEnvelope(raw.slice(0, embedded.start), raw.slice(embedded.end))
  };
}

function parseA2UIEnvelopeJSON(content) {
  try {
    return a2UIEventsFromValue(JSON.parse(content));
  } catch {
    return [];
  }
}

function a2UIEventsFromValue(value) {
  if (Array.isArray(value?.a2ui_events)) {
    return value.a2ui_events.filter(isProtocolEvent);
  }
  return isProtocolEvent(value) ? [value] : [];
}

function isProtocolEvent(value) {
  const eventName = String(value?.event || '');
  return eventName.startsWith('a2ui.') || eventName === 'storyboard.patch' || eventName === 'storyboard.snapshot' || eventName === 'job.status';
}

function findEmbeddedA2UIEnvelope(content) {
  for (let start = content.indexOf('{'); start >= 0; start = content.indexOf('{', start + 1)) {
    const parsed = parseJSONObjectAt(content, start);
    if (!parsed) {
      continue;
    }
    const events = a2UIEventsFromValue(parsed.value);
    if (events.length) {
      return { ...parsed, events };
    }
  }
  return null;
}

function parseJSONObjectAt(content, start) {
  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let index = start; index < content.length; index += 1) {
    const char = content[index];
    if (inString) {
      if (escaped) {
        escaped = false;
      } else if (char === '\\') {
        escaped = true;
      } else if (char === '"') {
        inString = false;
      }
      continue;
    }
    if (char === '"') {
      inString = true;
      continue;
    }
    if (char === '{') {
      depth += 1;
      continue;
    }
    if (char === '}') {
      depth -= 1;
      if (depth === 0) {
        const end = index + 1;
        try {
          return { value: JSON.parse(content.slice(start, end)), start, end };
        } catch {
          return null;
        }
      }
    }
  }
  return null;
}

function joinDisplayTextAroundEnvelope(prefix, suffix) {
  const before = String(prefix || '').trim().replace(/[:：,，]$/, '').trim();
  const after = String(suffix || '').trim().replace(/^[:：,，]/, '').trim();
  if (!before) {
    return after;
  }
  if (!after) {
    return before;
  }
  return `${before}\n\n${after}`;
}

function stripJSONFence(content) {
  const trimmed = String(content || '').trim();
  if (!trimmed.startsWith('```')) {
    return trimmed;
  }
  const lines = trimmed.split('\n');
  if (lines.length < 3) {
    return trimmed;
  }
  return lines.slice(1, -1).join('\n').trim();
}

function statusLabel(status) {
  return statusLabels[status] || status || '';
}

function toolProgressText(payload) {
  if (payload.message) {
    return payload.message;
  }
  if (payload.tool_name) {
    return `${payload.tool_name} ${payload.status || '执行中'}`;
  }
  if (payload.role === 'tool') {
    return '工具结果已返回';
  }
  return '工具执行中';
}

function a2UIProgressText(eventName, payload) {
  if (payload.message || payload.title || payload.label) {
    return payload.message || payload.title || payload.label;
  }
  if (payload.surface) {
    return `${payload.surface} ${payload.status || '更新中'}`;
  }
  return eventName === 'a2ui.begin_rendering' ? '开始渲染界面' : '界面已更新';
}

function errorMessage(err) {
  return err?.message || '请求失败';
}
