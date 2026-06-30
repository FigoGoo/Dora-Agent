import { useEffect, useMemo, useRef, useState } from 'react';
import { Check, ChevronDown, ChevronRight, Play, Plus, RefreshCw, RotateCcw, Send, Settings2, Square, X } from 'lucide-react';
import { agentApi, openRunStream } from '../../services/agentApi.js';
import { applySnapshot, createAguiState, reduceAguiEvent, reduceAguiEvents } from './aguiState.js';
import { Alert } from '../../components/admin/Alert.jsx';
import { Badge } from '../../components/admin/Badge.jsx';
import { Button } from '../../components/admin/Button.jsx';
import { EmptyState } from '../../components/admin/EmptyState.jsx';
import { PageHeader } from '../../components/admin/PageHeader.jsx';
import { SelectField } from '../../components/admin/SelectField.jsx';
import { TextField } from '../../components/admin/TextField.jsx';
import { useToast } from '../../components/admin/Toast.jsx';

const initialForm = {
  token: '',
  spaceId: '',
  projectId: '',
  sessionId: '',
  selectedSkillId: '',
  modelResourceType: 'image',
  modelId: '',
  modelDisplayName: '',
  pricingSnapshotId: '',
  referencedAssetsJson: '[]',
  controlInputsJson: '[]'
};

const resourceTypeOptions = [
  { label: 'image', value: 'image' },
  { label: 'music', value: 'music' },
  { label: 'video', value: 'video' },
  { label: 'text', value: 'text' }
];

const localDebugAccount = {
  loginType: 'personal',
  account: 'user1001@dora.local',
  password: 'local-user-change-me',
  projectId: 'prj_active_1001'
};

function clientMessageId(prefix) {
  return `${prefix}_${Date.now().toString(36)}`;
}

function traceId() {
  return `admin-skill-agent-${Date.now().toString(36)}`;
}

function parseJsonArray(value, label) {
  const parsed = JSON.parse(value || '[]');
  if (!Array.isArray(parsed)) {
    throw new Error(`${label} 必须是 JSON 数组。`);
  }
  return parsed;
}

function buildModelSelection(form) {
  if (!form.modelId.trim() && !form.pricingSnapshotId.trim()) {
    return undefined;
  }
  return {
    resource_type: form.modelResourceType,
    model_id: form.modelId.trim(),
    model_display_name: form.modelDisplayName.trim(),
    pricing_snapshot_id: form.pricingSnapshotId.trim(),
    is_default: false
  };
}

function readReplayEvents(payload) {
  return payload?.events || [];
}

function buildQuery(params = {}) {
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      query.set(key, String(value));
    }
  });
  const serialized = query.toString();
  return serialized ? `?${serialized}` : '';
}

async function businessRequest(path, options = {}) {
  const headers = {
    Accept: 'application/json',
    ...options.headers
  };
  if (options.token) {
    headers.Authorization = `Bearer ${options.token}`;
  }
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  const response = await fetch(`${path}${buildQuery(options.query)}`, {
    method: options.method || 'GET',
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal
  });
  const payload = await response.json().catch(() => null);
  if (!response.ok || payload?.code === 'ERROR') {
    const detail = payload?.error || payload || {};
    throw new Error(detail.message || '本地调试默认值生成失败。');
  }
  return payload?.data || payload;
}

function isLocalDebugHost() {
  const host = window.location.hostname;
  return host === '127.0.0.1' || host === 'localhost' || host === '::1';
}

function pickProjectId(projects) {
  const items = projects?.items || [];
  const selected = items.find((project) => project.status === 'active' && project.creative_allowed !== false) || items[0];
  return selected?.project_id || localDebugAccount.projectId;
}

function pickSkillId(skills) {
  const items = skills?.items || [];
  const selected = items.find((skill) => skill.status === 'published') || items[0];
  return selected?.skill_id || '';
}

async function loadLocalDebugDefaults(signal) {
  const login = await businessRequest('/api/auth/login', {
    method: 'POST',
    body: {
      login_type: localDebugAccount.loginType,
      account: localDebugAccount.account,
      password: localDebugAccount.password
    },
    signal
  });
  const token = login.access_token || '';
  const [projects, skills] = await Promise.all([
    businessRequest('/api/projects', { token, signal }),
    businessRequest('/api/skills', { token, query: { status: 'published', limit: 20 }, signal })
  ]);
  return {
    token,
    spaceId: login.current_space?.space_id || login.current_space_id || '',
    projectId: pickProjectId(projects),
    selectedSkillId: pickSkillId(skills)
  };
}

function latestEventLabel(event) {
  return `${event.sequence || '-'} ${event.type || event.event_type || 'unknown'}`;
}

function roleLabel(role) {
  if (role === 'user') {
    return '用户';
  }
  if (role === 'assistant') {
    return 'Agent';
  }
  return role || '消息';
}

function interleaveMessages(userMessages, agentMessages) {
  const messages = [];
  const max = Math.max(userMessages.length, agentMessages.length);
  for (let index = 0; index < max; index += 1) {
    if (userMessages[index]) {
      messages.push(userMessages[index]);
    }
    if (agentMessages[index]) {
      messages.push({ ...agentMessages[index], role: agentMessages[index].role || 'assistant' });
    }
  }
  return messages;
}

function JsonPreview({ value }) {
  return <pre className="admin-agent-test__json">{JSON.stringify(value || {}, null, 2)}</pre>;
}

export function SkillAgentTestPage() {
  const [form, setForm] = useState(initialForm);
  const [agui, setAgui] = useState(createAguiState);
  const [session, setSession] = useState(null);
  const [run, setRun] = useState(null);
  const [draftMessage, setDraftMessage] = useState('');
  const [userMessages, setUserMessages] = useState([]);
  const [rejectReason, setRejectReason] = useState('admin_debug_rejected');
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [debugOpen, setDebugOpen] = useState(false);
  const [bootstrapMessage, setBootstrapMessage] = useState('');
  const [bootstrapLoading, setBootstrapLoading] = useState(false);
  const [loading, setLoading] = useState('');
  const [streaming, setStreaming] = useState(false);
  const streamAbortRef = useRef(null);
  const toast = useToast();

  const auth = useMemo(
    () => ({ token: form.token, spaceId: form.spaceId.trim(), traceId: traceId() }),
    [form.token, form.spaceId]
  );

  const conversationMessages = useMemo(
    () => interleaveMessages(userMessages, agui.messages),
    [userMessages, agui.messages]
  );

  const canStartConversation = Boolean(form.token.trim() && form.projectId.trim() && form.selectedSkillId.trim());
  const confirmationReady = agui.confirmation?.interrupt_id && agui.confirmation?.payload_digest;
  const runId = run?.run_id || run?.runId;

  useEffect(() => {
    return () => streamAbortRef.current?.abort();
  }, []);

  useEffect(() => {
    if (!isLocalDebugHost()) {
      return undefined;
    }
    const controller = new AbortController();
    setBootstrapLoading(true);
    loadLocalDebugDefaults(controller.signal).then((defaults) => {
      setForm((current) => ({
        ...current,
        token: current.token || defaults.token,
        spaceId: current.spaceId || defaults.spaceId,
        projectId: current.projectId || defaults.projectId,
        selectedSkillId: current.selectedSkillId || defaults.selectedSkillId
      }));
      setBootstrapMessage(`已自动填入本地用户 ${localDebugAccount.account} 的 token、项目和已发布 Skill。`);
    }).catch((error) => {
      if (error.name !== 'AbortError') {
        setBootstrapMessage(error.message || '本地默认值生成失败，请手动填写。');
      }
    }).finally(() => {
      if (!controller.signal.aborted) {
        setBootstrapLoading(false);
      }
    });
    return () => controller.abort();
  }, []);

  function updateForm(name, value) {
    setForm((current) => ({ ...current, [name]: value }));
  }

  function requireRunId() {
    if (!runId) {
      throw new Error('当前没有可操作的 run。');
    }
    return runId;
  }

  function mergeEvents(events) {
    setAgui((current) => reduceAguiEvents(current, events));
  }

  async function createOrUseSession() {
    if (form.sessionId.trim()) {
      return { session_id: form.sessionId.trim(), project_id: form.projectId.trim(), status: 'active' };
    }
    return agentApi.createSession({
      ...auth,
      body: { project_id: form.projectId.trim(), initial_title: `Skill 调试 ${form.selectedSkillId.trim()}` }
    });
  }

  async function createRun(messageText, messageId) {
    if (!canStartConversation) {
      throw new Error('请先填写 Token、项目 ID 和 Skill ID。');
    }
    const referencedAssets = parseJsonArray(form.referencedAssetsJson, '引用资产');
    const controlInputs = parseJsonArray(form.controlInputsJson, '控件输入');
    streamAbortRef.current?.abort();
    setStreaming(false);
    const nextSession = await createOrUseSession();
    const body = {
      session_id: nextSession.session_id,
      project_id: form.projectId.trim(),
      selected_skill_id: form.selectedSkillId.trim(),
      user_input: {
        client_message_id: messageId,
        content_type: 'text',
        text: messageText,
        language: 'zh-CN'
      },
      referenced_assets: referencedAssets,
      control_inputs: controlInputs
    };
    const modelSelection = buildModelSelection(form);
    if (modelSelection) {
      body.model_selection = modelSelection;
    }
    const nextRun = await agentApi.createRun({ ...auth, body });
    setSession(nextSession);
    setRun(nextRun);
    setAgui(createAguiState());
    setUserMessages([{ message_id: messageId, role: 'user', content: messageText, final: true }]);
    toast?.notify('Agent 对话已开始');
    startStream(nextRun.run_id, 0);
  }

  async function appendInput(messageText, messageId) {
    await agentApi.appendUserInput({
      ...auth,
      runId: requireRunId(),
      body: {
        user_input: {
          client_message_id: messageId,
          content_type: 'text',
          text: messageText,
          language: 'zh-CN'
        }
      }
    });
    setUserMessages((current) => [...current, { message_id: messageId, role: 'user', content: messageText, final: true }]);
    await replayEvents();
  }

  async function sendMessage(event) {
    event.preventDefault();
    const messageText = draftMessage.trim();
    if (!messageText) {
      toast?.notify('请输入用户消息。', 'warning');
      return;
    }
    const messageId = clientMessageId(runId ? 'cm_admin_append' : 'cm_admin_skill');
    setLoading(runId ? 'append' : 'run');
    try {
      if (runId) {
        await appendInput(messageText, messageId);
      } else {
        await createRun(messageText, messageId);
      }
      setDraftMessage('');
    } catch (error) {
      toast?.notify(error.message || '发送消息失败。', 'danger', { title: '发送失败', traceId: error.traceId });
    } finally {
      setLoading('');
    }
  }

  function startStream(nextRunId = requireRunId(), afterSequence = agui.lastSequence) {
    streamAbortRef.current?.abort();
    const controller = new AbortController();
    streamAbortRef.current = controller;
    setStreaming(true);
    openRunStream({
      ...auth,
      runId: nextRunId,
      lastEventId: afterSequence ? String(afterSequence) : '',
      signal: controller.signal,
      onEvent: (event) => setAgui((current) => reduceAguiEvent(current, event))
    }).catch((error) => {
      if (error.name !== 'AbortError') {
        toast?.notify(error.message || 'SSE 已断开。', 'warning', { title: '事件流断开', traceId: error.traceId });
      }
    }).finally(() => {
      if (streamAbortRef.current === controller) {
        setStreaming(false);
      }
    });
  }

  function stopStream() {
    streamAbortRef.current?.abort();
    streamAbortRef.current = null;
    setStreaming(false);
  }

  function resetConversation() {
    stopStream();
    setRun(null);
    setSession(null);
    setAgui(createAguiState());
    setUserMessages([]);
    setDraftMessage('');
  }

  async function replayEvents() {
    setLoading('replay');
    try {
      const payload = await agentApi.replayEvents({ ...auth, runId: requireRunId(), afterSequence: agui.lastSequence, pageSize: 100 });
      mergeEvents(readReplayEvents(payload));
      if (payload?.snapshot_required) {
        await loadSnapshot();
      }
      toast?.notify('事件补偿完成');
    } catch (error) {
      toast?.notify(error.message || '事件补偿失败。', 'danger', { title: '补偿失败', traceId: error.traceId });
    } finally {
      setLoading('');
    }
  }

  async function loadSnapshot() {
    setLoading('snapshot');
    try {
      const snapshot = await agentApi.getSnapshot({ ...auth, runId: requireRunId() });
      setAgui((current) => applySnapshot(current, snapshot));
      toast?.notify('快照已恢复');
    } catch (error) {
      toast?.notify(error.message || '快照恢复失败。', 'danger', { title: '恢复失败', traceId: error.traceId });
    } finally {
      setLoading('');
    }
  }

  async function acceptInterrupt() {
    setLoading('accept');
    try {
      const confirmation = agui.confirmation;
      await agentApi.acceptInterrupt({
        ...auth,
        runId: requireRunId(),
        interruptId: confirmation.interrupt_id,
        body: {
          run_id: requireRunId(),
          interrupt_id: confirmation.interrupt_id,
          action: 'confirm',
          confirmed_payload_digest: confirmation.payload_digest
        }
      });
      await replayEvents();
    } catch (error) {
      toast?.notify(error.message || '确认失败。', 'danger', { title: '确认失败', traceId: error.traceId });
    } finally {
      setLoading('');
    }
  }

  async function rejectInterrupt() {
    setLoading('reject');
    try {
      const confirmation = agui.confirmation;
      await agentApi.rejectInterrupt({
        ...auth,
        runId: requireRunId(),
        interruptId: confirmation.interrupt_id,
        body: {
          run_id: requireRunId(),
          interrupt_id: confirmation.interrupt_id,
          reason_code: rejectReason.trim() || 'admin_debug_rejected'
        }
      });
      await replayEvents();
    } catch (error) {
      toast?.notify(error.message || '拒绝失败。', 'danger', { title: '拒绝失败', traceId: error.traceId });
    } finally {
      setLoading('');
    }
  }

  async function cancelRun() {
    setLoading('cancel');
    try {
      await agentApi.cancelRun({
        ...auth,
        runId: requireRunId(),
        body: { run_id: requireRunId(), cancel_reason: 'admin_skill_agent_test_cancel' }
      });
      stopStream();
      await replayEvents();
    } catch (error) {
      toast?.notify(error.message || '打断失败。', 'danger', { title: '打断失败', traceId: error.traceId });
    } finally {
      setLoading('');
    }
  }

  return (
    <>
      <PageHeader
        title="Skill Agent 调试台"
        description="像普通用户一样和指定 Skill 对话，旁路查看 Tool、AG-UI、确认和恢复链路。"
        actions={
          <>
            <Button type="button" size="sm" icon={Plus} onClick={resetConversation}>
              新对话
            </Button>
            <Button type="button" size="sm" icon={streaming ? Square : Play} onClick={streaming ? stopStream : () => startStream()} disabled={!runId}>
              {streaming ? '断开事件流' : '打开事件流'}
            </Button>
            <Button type="button" size="sm" icon={RefreshCw} onClick={replayEvents} loading={loading === 'replay'} disabled={!runId}>
              事件补偿
            </Button>
            <Button type="button" size="sm" icon={RotateCcw} onClick={loadSnapshot} loading={loading === 'snapshot'} disabled={!runId}>
              快照恢复
            </Button>
          </>
        }
      />

      <section className="admin-agent-test">
        <section className="admin-agent-test__setup">
          <div className="admin-agent-test__setup-main">
            <Alert tone="info" title={bootstrapLoading ? '正在生成本地调试 Token' : '模拟普通用户对话'}>
              {bootstrapMessage || '只使用普通用户 Agent API Token；Skill ID 会在创建 run 时通过 selected_skill_id 发送。'}
            </Alert>
            <div className="admin-agent-test__grid admin-agent-test__grid--setup">
              <TextField label="Agent API Bearer Token" value={form.token} onChange={(event) => updateForm('token', event.target.value)} required />
              <TextField label="Project ID" value={form.projectId} onChange={(event) => updateForm('projectId', event.target.value)} required />
              <TextField label="Selected Skill ID（随 run 发送）" value={form.selectedSkillId} onChange={(event) => updateForm('selectedSkillId', event.target.value)} required />
              <TextField label="Session ID（可选）" value={form.sessionId} onChange={(event) => updateForm('sessionId', event.target.value)} />
            </div>
          </div>
          <Button
            type="button"
            size="sm"
            icon={advancedOpen ? ChevronDown : ChevronRight}
            onClick={() => setAdvancedOpen((current) => !current)}
          >
            高级参数
          </Button>
          {advancedOpen ? (
            <div className="admin-agent-test__advanced">
              <div className="admin-agent-test__grid">
                <TextField label="Space ID" value={form.spaceId} onChange={(event) => updateForm('spaceId', event.target.value)} />
                <SelectField label="模型资源类型" value={form.modelResourceType} onChange={(event) => updateForm('modelResourceType', event.target.value)} options={resourceTypeOptions} />
                <TextField label="Model ID" value={form.modelId} onChange={(event) => updateForm('modelId', event.target.value)} />
                <TextField label="Pricing Snapshot ID" value={form.pricingSnapshotId} onChange={(event) => updateForm('pricingSnapshotId', event.target.value)} />
                <TextField label="Model Display Name" value={form.modelDisplayName} onChange={(event) => updateForm('modelDisplayName', event.target.value)} />
              </div>
              <TextField label="Referenced Assets JSON" textarea rows={3} value={form.referencedAssetsJson} onChange={(event) => updateForm('referencedAssetsJson', event.target.value)} />
              <TextField label="Control Inputs JSON" textarea rows={3} value={form.controlInputsJson} onChange={(event) => updateForm('controlInputsJson', event.target.value)} />
            </div>
          ) : null}
        </section>

        <section className="admin-agent-test__conversation">
          <header className="admin-agent-test__conversation-header">
            <div>
              <h2>模拟用户对话</h2>
              <p>第一次发送会创建 Agent run；同一轮里继续发送会追加用户输入。</p>
            </div>
            <div className="admin-agent-test__conversation-meta">
              <Badge>{agui.runStatus}</Badge>
              <Badge>{streaming ? 'streaming' : 'idle'}</Badge>
              <Badge>{agui.selectedSkill?.matched_reason || (form.selectedSkillId ? 'skill ready' : 'skill pending')}</Badge>
            </div>
          </header>

          <div className="admin-agent-test__chat-scroll" aria-live="polite">
            {agui.thinking ? <p className="admin-agent-test__thinking">{agui.thinking}</p> : null}
            {conversationMessages.length ? (
              <div className="admin-agent-test__messages admin-agent-test__messages--chat">
                {conversationMessages.map((message, index) => (
                  <article key={message.message_id || `${message.role}-${index}`} className={`admin-agent-test__message admin-agent-test__message--${message.role || 'assistant'}`}>
                    <div className="admin-agent-test__message-bubble">
                      <Badge>{roleLabel(message.role)}</Badge>
                      <p>{message.content || '-'}</p>
                    </div>
                  </article>
                ))}
              </div>
            ) : (
              <EmptyState title="输入一句用户消息开始调试" />
            )}
          </div>

          {agui.confirmation ? (
            <div className="admin-agent-test__confirm-banner">
              <div>
                <strong>{agui.confirmation.title || 'Agent 请求确认'}</strong>
                <p>{agui.confirmation.summary || agui.confirmation.reason || '-'}</p>
              </div>
              <div className="admin-row-actions">
                <Button type="button" size="sm" icon={Check} onClick={acceptInterrupt} loading={loading === 'accept'} disabled={!confirmationReady}>
                  确认
                </Button>
                <Button type="button" size="sm" variant="danger-ghost" icon={X} onClick={rejectInterrupt} loading={loading === 'reject'}>
                  拒绝
                </Button>
              </div>
            </div>
          ) : null}

          <form className="admin-agent-test__composer" onSubmit={sendMessage}>
            <TextField
              label={runId ? '继续模拟用户输入' : '用户消息'}
              textarea
              rows={3}
              value={draftMessage}
              onChange={(event) => setDraftMessage(event.target.value)}
              required
            />
            <div className="admin-agent-test__composer-actions">
              <Button type="submit" variant="primary" icon={Send} loading={loading === 'run' || loading === 'append'} disabled={!runId && !canStartConversation}>
                {runId ? '发送' : '开始对话'}
              </Button>
              <Button type="button" variant="danger-ghost" icon={Square} onClick={cancelRun} loading={loading === 'cancel'} disabled={!runId}>
                打断
              </Button>
            </div>
          </form>
        </section>

        <section className="admin-agent-test__debug">
          <header className="admin-agent-test__debug-header">
            <div>
              <h2>调试详情</h2>
              <p>需要看 Tool、AG-UI 或 snapshot 时再展开。</p>
            </div>
            <Button type="button" size="sm" icon={debugOpen ? ChevronDown : Settings2} onClick={() => setDebugOpen((current) => !current)}>
              {debugOpen ? '收起详情' : '展开详情'}
            </Button>
          </header>

          {debugOpen ? (
            <div className="admin-agent-test__debug-body">
              <div className="admin-agent-test__summary" aria-label="运行摘要">
                <article>
                  <span>Run</span>
                  <strong>{runId || '-'}</strong>
                  <Badge>{agui.runStatus}</Badge>
                </article>
                <article>
                  <span>Session</span>
                  <strong>{session?.session_id || form.sessionId || '-'}</strong>
                  <Badge>{streaming ? 'streaming' : 'idle'}</Badge>
                </article>
                <article>
                  <span>Skill</span>
                  <strong>{agui.selectedSkill?.skill_name || agui.selectedSkill?.skill_id || form.selectedSkillId || '-'}</strong>
                  <Badge>{agui.selectedSkill?.matched_reason || 'pending'}</Badge>
                </article>
              </div>

              <section className="admin-agent-test__split">
                <div className="admin-agent-test__panel">
                  <header>
                    <h2>Tool 调用</h2>
                    <Badge>{agui.tools.length}</Badge>
                  </header>
                  {agui.tools.length ? (
                    <div className="admin-agent-test__list">
                      {agui.tools.map((tool) => (
                        <article key={tool.tool_call_id}>
                          <strong>{tool.tool_name || tool.tool_call_id}</strong>
                          <span>{tool.tool_type || '-'} · {tool.status || '-'} · {tool.progress ?? 0}%</span>
                        </article>
                      ))}
                    </div>
                  ) : (
                    <EmptyState title="暂无 Tool 调用" />
                  )}
                </div>

                <div className="admin-agent-test__panel">
                  <header>
                    <h2>确认与恢复</h2>
                    <Badge>{agui.confirmation?.status || 'idle'}</Badge>
                  </header>
                  {agui.confirmation ? (
                    <div className="admin-agent-test__confirm">
                      <strong>{agui.confirmation.title || '确认操作'}</strong>
                      <p>{agui.confirmation.summary || agui.confirmation.reason || '-'}</p>
                      <small>{agui.confirmation.interrupt_id}</small>
                      <TextField label="拒绝原因" value={rejectReason} onChange={(event) => setRejectReason(event.target.value)} />
                    </div>
                  ) : (
                    <EmptyState title="暂无确认" />
                  )}
                </div>
              </section>

              <section className="admin-agent-test__split">
                <div className="admin-agent-test__panel">
                  <header>
                    <h2>生成与资产</h2>
                    <Badge>{agui.generationTasks.length + agui.assets.length}</Badge>
                  </header>
                  <JsonPreview value={{ credit: agui.credit, tasks: agui.generationTasks, assets: agui.assets, blackboard: agui.blackboard }} />
                </div>

                <div className="admin-agent-test__panel">
                  <header>
                    <h2>AG-UI 事件</h2>
                    <Badge>{agui.events.length}</Badge>
                  </header>
                  {agui.events.length ? (
                    <ol className="admin-agent-test__events">
                      {agui.events.map((event) => (
                        <li key={event.event_id}>
                          <strong>{latestEventLabel(event)}</strong>
                          <span>{event.event_id}</span>
                        </li>
                      ))}
                    </ol>
                  ) : (
                    <EmptyState title="暂无事件" />
                  )}
                </div>
              </section>
            </div>
          ) : null}
        </section>
      </section>
    </>
  );
}
