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
import { requestJSON, requestOptionalJSON } from '../../platform/api/apiClient.js';
import { connectReconnectingSSE } from '../../platform/events/reconnectingSSE.js';
import { StoryboardPanel } from './StoryboardPanel.jsx';
import {
  A2UI_ACTIONS,
  A2UI_COMPONENTS,
  A2UI_EVENT_NAMES,
  A2UI_EVENTS,
  componentPayload,
  isSupportedA2UIActionEnvelope
} from './a2uiProtocol.js';

const SESSION_STORAGE_KEY = 'dora:aigc:demo_session_id';
const WELCOME_MESSAGE = '把剧本、风格或 Skill.md 发给我，我会先规划规格和故事板。';

// statusLabels 是前端统一展示状态中文名的映射表。
const statusLabels = {
  queued: '排队中',
  accepted: '已受理',
  waiting_jobs: '等待任务',
  waiting_provider: '等待生成',
  waiting_user: '等待用户确认',
  finalizing: '处理中',
  retry_wait: '等待重试',
  running: '生成中',
  succeeded: '完成',
  partial_failed: '部分失败',
  partial: '部分完成',
  cancelling: '取消中',
  failed: '失败',
  cancelled: '已取消',
  done: '完成',
  completed: '完成',
  draft: '草稿',
  reviewing: '待确认',
  candidate: '候选待审',
  active: '已采用',
  stale: '需更新',
  missing: '缺失',
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
  const approvalDecisionRequestsRef = useRef(new Map());
  const candidateApprovalDecisionRequestsRef = useRef(new Map());
  const terminalApprovalSurfaceIDsRef = useRef(new Set());
  const lastEventSeqRef = useRef(0);
  const activeSessionRef = useRef({ id: '', generation: 0 });
  const sessionAbortControllerRef = useRef(null);
  const resourceMutationVersionsRef = useRef({ storyboard: 0, assets: 0, jobs: 0, messages: 0 });
  const latestResourceRequestsRef = useRef({ storyboard: 0, assets: 0, jobs: 0, messages: 0 });
  const resourceRequestSequenceRef = useRef(0);

  // activateSession 为会话切换建立新的请求代次，并取消上一会话仍在进行的读取。
  const activateSession = useCallback((id) => {
    sessionAbortControllerRef.current?.abort();
    const generation = activeSessionRef.current.generation + 1;
    activeSessionRef.current = { id, generation };
    sessionAbortControllerRef.current = new AbortController();
    return generation;
  }, []);

  // currentSessionRequest 返回只对当前会话有效的请求上下文。
  const currentSessionRequest = useCallback((id) => {
    const active = activeSessionRef.current;
    if (!id || active.id !== id || !sessionAbortControllerRef.current) {
      return null;
    }
    return { generation: active.generation, signal: sessionAbortControllerRef.current.signal };
  }, []);

  // isCurrentSessionRequest 防止已经切走的会话在异步响应后写回工作台。
  const isCurrentSessionRequest = useCallback((id, generation) => {
    const active = activeSessionRef.current;
    return active.id === id && active.generation === generation && !sessionAbortControllerRef.current?.signal.aborted;
  }, []);

  // markResourceMutation 记录 SSE 或本地交互产生的新状态，阻止更早的 REST 快照覆盖它。
  const markResourceMutation = useCallback((resource) => {
    resourceMutationVersionsRef.current[resource] = (resourceMutationVersionsRef.current[resource] || 0) + 1;
  }, []);

  // beginResourceSnapshot 为一次 REST 快照读取冻结请求顺序和读取开始时的实时状态版本。
  const beginResourceSnapshot = useCallback((resources) => {
    const requestID = resourceRequestSequenceRef.current + 1;
    resourceRequestSequenceRef.current = requestID;
    const mutationVersions = {};
    resources.forEach((resource) => {
      latestResourceRequestsRef.current[resource] = requestID;
      mutationVersions[resource] = resourceMutationVersionsRef.current[resource] || 0;
    });
    return { requestID, mutationVersions };
  }, []);

  // canApplyResourceSnapshot 仅允许最新请求且读取期间未发生实时/本地更新的快照写回。
  const canApplyResourceSnapshot = useCallback((resource, snapshot) => {
    return (
      latestResourceRequestsRef.current[resource] === snapshot.requestID &&
      resourceMutationVersionsRef.current[resource] === snapshot.mutationVersions[resource]
    );
  }, []);

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
    const request = currentSessionRequest(id);
    if (!request) {
      return;
    }
    const snapshot = beginResourceSnapshot(['assets']);
    try {
      const result = await requestOptionalJSON(`/api/aigc/sessions/${id}/assets`, { signal: request.signal });
      if (isCurrentSessionRequest(id, request.generation) && result?.assets) {
        setAssets((current) => (canApplyResourceSnapshot('assets', snapshot) ? result.assets : current));
      }
    } catch (err) {
      if (!isCurrentSessionRequest(id, request.generation) || err?.name === 'AbortError') {
        return;
      }
      throw err;
    }
  }, [beginResourceSnapshot, canApplyResourceSnapshot, currentSessionRequest, isCurrentSessionRequest]);

  // refreshSessionData 拉取故事板、素材、任务和可选历史消息，作为页面冷启动/刷新入口。
  const refreshSessionData = useCallback(
    async (id, options = {}) => {
      const request = currentSessionRequest(id);
      if (!request) {
        return;
      }
      const includeMessages = options.includeMessages !== false;
      const resources = includeMessages
        ? ['storyboard', 'assets', 'jobs', 'messages']
        : ['storyboard', 'assets', 'jobs'];
      const snapshot = beginResourceSnapshot(resources);
      const [boardResult, assetsResult, jobsResult, messagesResult] = await Promise.allSettled([
        requestOptionalJSON(`/api/aigc/sessions/${id}/storyboard`, { signal: request.signal }),
        requestOptionalJSON(`/api/aigc/sessions/${id}/assets`, { signal: request.signal }),
        requestOptionalJSON(`/api/aigc/sessions/${id}/jobs`, { signal: request.signal }),
        includeMessages ? requestOptionalJSON(`/api/aigc/sessions/${id}/messages`, { signal: request.signal }) : Promise.resolve(null)
      ]);
      if (!isCurrentSessionRequest(id, request.generation)) {
        return;
      }
      if (
        boardResult.status === 'fulfilled' &&
        boardResult.value &&
        canApplyResourceSnapshot('storyboard', snapshot)
      ) {
        setStoryboard((current) => mergeStoryboardSnapshot(current, boardResult.value));
        setSelectedTarget((current) => current || defaultSelectedTarget(boardResult.value));
      }
      if (
        assetsResult.status === 'fulfilled' &&
        assetsResult.value?.assets &&
        canApplyResourceSnapshot('assets', snapshot)
      ) {
        setAssets(assetsResult.value.assets);
      }
      if (
        jobsResult.status === 'fulfilled' &&
        jobsResult.value?.jobs &&
        canApplyResourceSnapshot('jobs', snapshot)
      ) {
        setJobs(jobsResult.value.jobs);
      }
      if (
        includeMessages &&
        messagesResult.status === 'fulfilled' &&
        messagesResult.value?.messages?.length &&
        canApplyResourceSnapshot('messages', snapshot)
      ) {
        const history = restoreHistoryFromMessageRecords(messagesResult.value.messages);
        history.terminalApprovalSurfaceIDs.forEach((surfaceID) => terminalApprovalSurfaceIDsRef.current.add(surfaceID));
        if (history.messages.length || history.surfaces.length || history.terminalApprovalSurfaceIDs.length) {
          timelineOrderRef.current = Math.max(timelineOrderRef.current, history.nextTimelineOrder);
          setMessages(history.messages);
          setSurfaces((current) =>
            mergeHydratedSurfaces(
              current,
              history.surfaces,
              history.submittedSurfaceIDs,
              terminalApprovalSurfaceIDsRef.current
            )
          );
        }
      }
      const rejected = [boardResult, assetsResult, jobsResult, includeMessages ? messagesResult : null].find(
        (result) => result?.status === 'rejected'
      );
      if (rejected) {
        throw rejected.reason;
      }
    },
    [beginResourceSnapshot, canApplyResourceSnapshot, currentSessionRequest, isCurrentSessionRequest]
  );

  useEffect(() => {
    if (!storyboard) {
      return;
    }
    setSelectedTarget((current) =>
      current && storyboardContainsTarget(storyboard, current) ? current : defaultSelectedTarget(storyboard)
    );
  }, [storyboard]);

  // finishAssistantMessage 结束正在流式显示的 assistant 消息。
  const finishAssistantMessage = useCallback(() => {
    const messageID = streamingAssistantID.current;
    streamingAssistantID.current = '';
    if (!messageID) {
      return;
    }
    markResourceMutation('messages');
    setMessages((items) => items.map((message) => (message.id === messageID ? { ...message, streaming: false } : message)));
  }, [markResourceMutation]);

  // addSystemMessage 把错误、确认提示等系统信息插入聊天时间线。
  const addSystemMessage = useCallback((content) => {
    if (!content) {
      return;
    }
    markResourceMutation('messages');
    setMessages((items) => [
      ...items,
      { id: `event-${Date.now()}-${items.length}`, role: 'system', content, timelineOrder: nextTimelineOrder() }
    ]);
  }, [markResourceMutation, nextTimelineOrder]);

  // resetWorkspaceState 清空当前工作台状态，用于开启新会话。
  const resetWorkspaceState = useCallback(() => {
    sessionAbortControllerRef.current?.abort();
    sessionAbortControllerRef.current = null;
    activeSessionRef.current = { id: '', generation: activeSessionRef.current.generation + 1 };
    streamingAssistantID.current = '';
    timelineOrderRef.current = 1;
    approvalDecisionRequestsRef.current.clear();
    candidateApprovalDecisionRequestsRef.current.clear();
    terminalApprovalSurfaceIDsRef.current.clear();
    lastEventSeqRef.current = 0;
    resourceMutationVersionsRef.current = { storyboard: 0, assets: 0, jobs: 0, messages: 0 };
    latestResourceRequestsRef.current = { storyboard: 0, assets: 0, jobs: 0, messages: 0 };
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
    (event, generation) => {
      const eventName = event?.event;
      const payload = event?.payload || {};
      if (!sessionID || !isCurrentSessionRequest(sessionID, generation)) {
        return;
      }
      if (eventName === A2UI_EVENTS.READY) {
        setError('');
        return;
      }
      const eventSeq = Number(event?.seq || 0);
      if (Number.isFinite(eventSeq) && eventSeq > 0) {
        if (eventSeq <= lastEventSeqRef.current) {
          return;
        }
        lastEventSeqRef.current = eventSeq;
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
          refreshSessionData,
          sessionID,
          nextTimelineOrder,
          markResourceMutation,
          terminalApprovalSurfaceIDs: terminalApprovalSurfaceIDsRef.current
        });
        return;
      }
      if (eventName === A2UI_EVENTS.INTERRUPT_REQUEST) {
        setInterrupt(payload);
        addSystemMessage(payload.message || payload.title || '需要确认后继续。');
        return;
      }
      if (eventName === A2UI_EVENTS.INTERRUPT_RESOLVED) {
        setInterrupt((current) => {
          if (!current) return null;
          if (payload.interrupt_id) {
            return payload.interrupt_id === current.interrupt_id ? null : current;
          }
          return payload.checkpoint_id && payload.checkpoint_id === current.checkpoint_id ? null : current;
        });
        return;
      }
      if (eventName === A2UI_EVENTS.ERROR) {
        const message = payload.message || '请求失败';
        setError(message);
        addSystemMessage(message);
      }
    },
    [addSystemMessage, isCurrentSessionRequest, markResourceMutation, nextTimelineOrder, refreshAssets, refreshSessionData, sessionID]
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
        activateSession(id);
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
      sessionAbortControllerRef.current?.abort();
    };
  }, [activateSession, refreshSessionData]);

  useEffect(() => {
    if (!sessionID || typeof window === 'undefined' || typeof window.EventSource !== 'function') {
      return undefined;
    }
    const request = currentSessionRequest(sessionID);
    if (!request) {
      return undefined;
    }
    const isActiveStream = () => isCurrentSessionRequest(sessionID, request.generation);
    const stream = connectReconnectingSSE({
      url: `/api/aigc/sessions/${sessionID}/events/stream`,
      eventNames: A2UI_EVENT_NAMES,
      initialCursor: lastEventSeqRef.current,
      onEvent: (event) => {
        if (isActiveStream()) {
          handleA2UIEvent(event, request.generation);
        }
      },
      onOpen: () => {
        if (isActiveStream()) {
          setError('');
        }
      },
      onTransportError: () => {
        if (isActiveStream()) {
          setError('事件流已断开，正在自动重连。');
        }
      }
    });
    return () => stream.close();
  }, [currentSessionRequest, handleA2UIEvent, isCurrentSessionRequest, sessionID]);

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
    const clientMessageID = `ui-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
    markResourceMutation('messages');
    setMessages((items) => [
      ...items,
      {
        id: clientMessageID,
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
        headers: { 'Idempotency-Key': clientMessageID },
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

  // submitSurface 仅让携带 approval_id 的权威审核卡走 Decision API；其余合法表单才归约为普通用户消息。
  async function submitSurface(surface, values) {
    const approvalID = approvalIDFromSurface(surface);
    if (approvalID) {
      if (isTerminalApprovalStatus(surface?.payload?.status)) {
        return;
      }
      const decision = String(values?.decision || '').trim().toLowerCase();
      if (!isApprovalDecision(decision)) {
        addSystemMessage('请选择确认或拒绝。');
        return;
      }
      setBusy(true);
      setError('');
      try {
        const request = freezeApprovalDecisionRequest(
          approvalDecisionRequestsRef.current,
          approvalID,
          decision,
          approvalDecisionVersion(surface)
        );
        await requestJSON(`/api/aigc/sessions/${sessionID}/approvals/${approvalID}/decision`, {
          method: 'POST',
          body: JSON.stringify(request)
        });
        markApprovalTerminal(terminalApprovalSurfaceIDsRef.current, surface, approvalID);
        setSurfaces((items) => items.filter((item) => !isSameApprovalSurface(item, surface, approvalID)));
        addSystemMessage(
          request.decision === 'approved' ? '已确认审核结果。' : '已拒绝审核结果，可继续提出修改要求。'
        );
        await refreshSessionData(sessionID, { includeMessages: false });
      } catch (err) {
        setError(errorMessage(err));
      } finally {
        setBusy(false);
      }
      return;
    }
    if (isApprovalLikeSurface(surface)) {
      addSystemMessage('审批卡缺少 approval_id，无法提交。请刷新页面或重新发起审核。');
      return;
    }
    const missingRequiredFields = requiredSurfaceFieldsMissing(surface, values);
    if (missingRequiredFields.length) {
      addSystemMessage(
        `请完成必填项：${missingRequiredFields.map((field) => field.label || fieldKey(field)).join('、')}。`
      );
      return;
    }
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
      activateSession(id);
      setSessionID(id);
      await refreshSessionData(id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setStartingSession(false);
    }
  }

  // resumeInterrupt 通过统一消息恢复协议处理 Agent 人审确认。
  async function resumeInterrupt(action) {
    if (!interrupt || !sessionID || busy) {
      return;
    }
    const label = action?.label || action?.key || '确认';
    const approved = !String(action?.key || '').includes('revise') && !String(action?.key || '').includes('reject');
    setBusy(true);
    setError('');
    try {
      await requestJSON(`/api/aigc/sessions/${sessionID}/messages/resume`, {
        method: 'POST',
        body: JSON.stringify({
          checkpoint_id: interrupt.checkpoint_id,
          interrupt_id: interrupt.interrupt_id,
          content: label,
          data: { approved, action_key: action?.key, note: label }
        })
      });
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
    const id = sessionID;
    const request = currentSessionRequest(id);
    if (!request) {
      return;
    }
    setError('');
    try {
      if (editing.kind === 'prompt') {
        const result = await requestJSON(
          `/api/aigc/sessions/${id}/storyboards/${storyboard.id}/targets/${editing.targetID}/prompt`,
          {
            method: 'PATCH',
            signal: request.signal,
            body: JSON.stringify({
              expected_version: storyboard.version,
              target_revision: editing.targetRevision,
              prompt_revision: editing.promptRevision,
              purpose: editing.purpose,
              prompt: editing.value
            })
          }
        );
        if (!isCurrentSessionRequest(id, request.generation)) {
          return;
        }
        markResourceMutation('storyboard');
        setStoryboard(result.storyboard || result.aggregate || result);
        setEditing(null);
        return;
      }
      const result = await requestJSON(`/api/aigc/sessions/${id}/storyboards/${storyboard.id}`, {
        method: 'PATCH',
        signal: request.signal,
        body: JSON.stringify({
          base_version: storyboard.version,
          source: 'user',
          ops: [{ op: 'replace', path: editing.path, value: editing.value }]
        })
      });
      if (!isCurrentSessionRequest(id, request.generation)) {
        return;
      }
      markResourceMutation('storyboard');
      setStoryboard(result.storyboard);
      setEditing(null);
    } catch (err) {
      if (!isCurrentSessionRequest(id, request.generation) || err?.name === 'AbortError') {
        return;
      }
      setError(errorMessage(err));
    }
  }

  // regenerateTarget 是左侧故事板的确定性 UI Command，不经过 Agent Tool 选择。
  async function regenerateTarget(target) {
    if (!storyboard || storyboard.pending_revision_id || !sessionID || busy || !target?.targetID || !target?.slot?.key) {
      return;
    }
    setBusy(true);
    setError('');
    try {
      const result = await requestJSON(
        `/api/aigc/sessions/${sessionID}/storyboards/${storyboard.id}/targets/${target.targetID}/regenerate`,
        {
          method: 'POST',
          body: JSON.stringify({
            expected_version: storyboard.version,
            target_revision: target.targetRevision,
            asset_slot: target.slot.key,
            media_kind: target.slot.media_kind,
            idempotency_key: `regenerate:${storyboard.id}:${target.targetID}:${target.slot.key}:v${storyboard.version}`
          })
        }
      );
      if (result.storyboard || result.aggregate) {
        markResourceMutation('storyboard');
        setStoryboard(result.storyboard || result.aggregate);
      }
      await refreshSessionData(sessionID, { includeMessages: false });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  // activateCandidate 采用某一候选资产；存在 Approval 时走统一 Decision API。
  async function activateCandidate({ targetID, targetRevision, slot, binding }) {
    if (!storyboard || storyboard.pending_revision_id || !sessionID || busy || !targetID || !binding) {
      return;
    }
    setBusy(true);
    setError('');
    try {
      let result;
      if (binding.approval_id) {
        const request = freezeApprovalDecisionRequest(
          approvalDecisionRequestsRef.current,
          binding.approval_id,
          'approved',
          0
        );
        result = await requestJSON(`/api/aigc/sessions/${sessionID}/approvals/${binding.approval_id}/decision`, {
          method: 'POST',
          body: JSON.stringify(request)
        });
        markApprovalTerminal(terminalApprovalSurfaceIDsRef.current, null, binding.approval_id);
        setSurfaces((items) => items.filter((surface) => !isSameApprovalSurface(surface, null, binding.approval_id)));
        setInterrupt((current) => (current?.approval_id === binding.approval_id ? null : current));
      } else {
        result = await requestJSON(
          `/api/aigc/sessions/${sessionID}/storyboards/${storyboard.id}/targets/${targetID}/assets/${binding.asset_id}/bind`,
          {
            method: 'POST',
            body: JSON.stringify({
              expected_version: storyboard.version,
              target_revision: targetRevision,
              asset_slot: slot.key,
              binding_id: binding.id
            })
          }
        );
      }
      if (result?.storyboard || result?.aggregate) {
        markResourceMutation('storyboard');
        setStoryboard(result.storyboard || result.aggregate);
      }
      await refreshSessionData(sessionID, { includeMessages: false });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  // confirmCandidateAssets 以 storyboard 版本为围栏，一次确认当前故事板的全部候选素材。
  async function confirmCandidateAssets() {
    if (!sessionID || !storyboard?.id || busy || !candidateApprovalReview.ready) {
      return;
    }
    const request = freezeCandidateApprovalDecisionRequest(
      candidateApprovalDecisionRequestsRef.current,
      storyboard.id,
      storyboard.version
    );
    setBusy(true);
    setError('');
    try {
      const result = await requestJSON(
        `/api/aigc/sessions/${sessionID}/storyboards/${storyboard.id}/candidate-approvals/decision`,
        {
          method: 'POST',
          body: JSON.stringify(request)
        }
      );
      if (result?.storyboard) {
        markResourceMutation('storyboard');
        setStoryboard(result.storyboard);
      }
      const terminalApprovalIDs = candidateApprovalResultIDs(result, candidateApprovalReview.bindings);
      if (terminalApprovalIDs.size > 0) {
        setSurfaces((items) =>
          items.filter((surface) => {
            const approvalID = approvalIDFromSurface(surface);
            if (!terminalApprovalIDs.has(approvalID)) {
              return true;
            }
            markApprovalTerminal(terminalApprovalSurfaceIDsRef.current, surface, approvalID);
            return false;
          })
        );
      }
      await refreshSessionData(sessionID, { includeMessages: false });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function bindExistingAsset({ targetID, targetRevision, slot, assetID }) {
    if (!storyboard || storyboard.pending_revision_id || !sessionID || busy || !targetID || !slot?.key || !assetID) {
      return;
    }
    setBusy(true);
    setError('');
    try {
      const result = await requestJSON(
        `/api/aigc/sessions/${sessionID}/storyboards/${storyboard.id}/targets/${targetID}/assets/${assetID}/bind`,
        {
          method: 'POST',
          body: JSON.stringify({
            expected_version: storyboard.version,
            target_revision: targetRevision,
            asset_slot: slot.key,
            generation_epoch: slot.generation_epoch || 0,
            idempotency_key: `manual-bind:${storyboard.id}:${targetID}:${slot.key}:${assetID}:v${storyboard.version}:e${slot.generation_epoch || 0}`
          })
        }
      );
      markResourceMutation('storyboard');
      setStoryboard(result.storyboard || result.aggregate || storyboard);
      await refreshSessionData(sessionID, { includeMessages: false });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  // uploadAndBindAsset 支持用户在动态故事板槽位内直接上传并填充素材。
  async function uploadAndBindAsset({ targetID, targetRevision, slot, file }) {
    if (!file || !sessionID || busy || storyboard?.pending_revision_id) {
      return;
    }
    setBusy(true);
    setError('');
    let normalized;
    try {
      const form = new FormData();
      form.append('session_id', sessionID);
      form.append('file', file);
      const kind = slotUploadKind(slot?.media_kind, file);
      if (kind) {
        form.append('kind', kind);
      }
      const created = await requestJSON('/api/aigc/assets', { method: 'POST', body: form });
      normalized = normalizeUploadedAsset(created, file);
      markResourceMutation('assets');
      setAssets((items) => upsertByID(items, normalized, 'id'));
    } catch (err) {
      setError(errorMessage(err));
      return;
    } finally {
      setBusy(false);
    }
    await bindExistingAsset({ targetID, targetRevision, slot, assetID: normalized.id });
  }

  async function controlGenerationOperation(operationID, action) {
    if (!sessionID || !operationID || busy) {
      return;
    }
    setBusy(true);
    setError('');
    try {
      await requestJSON(`/api/aigc/sessions/${sessionID}/generation-operations/${operationID}/control`, {
        method: 'POST',
        body: JSON.stringify({ action, idempotency_key: `${action}:${operationID}` })
      });
      await refreshSessionData(sessionID, { includeMessages: false });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  // importSkillFile 读取本地 Skill.md，导入后绑定到当前会话。
  async function importSkillFile(event) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file || !sessionID) {
      return;
    }
    const id = sessionID;
    const request = currentSessionRequest(id);
    if (!request) {
      return;
    }
    setError('');
    try {
      const content = await file.text();
      if (!isCurrentSessionRequest(id, request.generation)) {
        return;
      }
      const created = await requestJSON('/api/aigc/skills', {
        method: 'POST',
        signal: request.signal,
        body: JSON.stringify({ content })
      });
      if (!isCurrentSessionRequest(id, request.generation)) {
        return;
      }
      const skillID = created?.skill?.id || created?.plan?.skill_id;
      if (!skillID) {
        throw new Error('Skill 创建失败');
      }
      await requestJSON(`/api/aigc/sessions/${id}/skill`, {
        method: 'POST',
        signal: request.signal,
        body: JSON.stringify({ skill_id: skillID })
      });
      if (!isCurrentSessionRequest(id, request.generation)) {
        return;
      }
      const skillName = created?.skill?.name || created?.plan?.name || file.name;
      addSystemMessage(`已导入 Skill：${skillName}`);
    } catch (err) {
      if (!isCurrentSessionRequest(id, request.generation) || err?.name === 'AbortError') {
        return;
      }
      setError(errorMessage(err));
    }
  }

  const candidateApprovalReview = useMemo(
    () => candidateApprovalReviewState(storyboard, jobs),
    [jobs, storyboard]
  );
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
          onRegenerateTarget={regenerateTarget}
          onActivateCandidate={activateCandidate}
          candidateApprovalReview={candidateApprovalReview}
          onConfirmCandidateAssets={confirmCandidateAssets}
          candidateApprovalBusy={busy}
          onBindAsset={bindExistingAsset}
          onUploadAsset={uploadAndBindAsset}
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

          <JobStatusSummary jobs={jobs} candidateReviewReady={candidateApprovalReview.ready} />

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
                return <ToolRunCard toolRun={item.toolRun} busy={busy} onControl={controlGenerationOperation} key={item.key} />;
              }
              return (
                <A2UISurfaceCard
                  surface={item.surface}
                  busy={busy}
                  sessionID={sessionID}
                  onAssetUploaded={(asset) => {
                    markResourceMutation('assets');
                    setAssets((items) => upsertByID(items, asset, 'id'));
                  }}
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

// chatTimelineItems 合并消息、当前高层能力进度和 A2UI 卡片，生成统一时间线。
function chatTimelineItems(messages, toolRuns, surfaces) {
  // 同一时刻只展示一个待用户处理的 surface；候选素材始终在左侧故事板统一审核。
  const visibleSurfaces = visibleChatSurfaces(surfaces);
  const activeApproval = visibleSurfaces.find((surface) => Boolean(approvalIDFromSurface(surface)));
  // Job/素材级投影属于工作区内部状态，聊天区只显示当前高层 Capability。
  const currentCapabilityRun = selectCurrentCapabilityRun(toolRuns, activeApproval);
  const visibleToolRuns = currentCapabilityRun ? [currentCapabilityRun] : [];
  return [
    ...messages.map((message, index) => ({
      type: 'message',
      key: `message:${message.id}`,
      timelineOrder: timelineOrder(message, index),
      fallbackOrder: index,
      message
    })),
    ...visibleToolRuns.map((toolRun, index) => ({
      type: 'toolRun',
      key: `tool:${toolRunKey(toolRun)}`,
      timelineOrder: timelineOrder(toolRun, messages.length + index),
      fallbackOrder: messages.length + index,
      toolRun
    })),
    ...visibleSurfaces.map((surface, index) => ({
      type: 'surface',
      key: `surface:${surface.id}`,
      timelineOrder: timelineOrder(surface, messages.length + visibleToolRuns.length + index),
      fallbackOrder: messages.length + visibleToolRuns.length + index,
      surface
    }))
  ].sort((left, right) => left.timelineOrder - right.timelineOrder || left.fallbackOrder - right.fallbackOrder);
}

const publicCapabilityOrder = [
  'analyze_materials',
  'plan_creation_spec',
  'plan_storyboard',
  'generate_media',
  'assemble_output'
];

const publicCapabilityLabels = {
  analyze_materials: '素材分析',
  plan_creation_spec: '创作规范',
  plan_storyboard: '故事板规划',
  generate_media: '素材生成',
  assemble_output: '成片合成'
};

// visibleChatSurfaces 保留说明卡，但同一时刻最多暴露一个需要用户提交的下一步。
function visibleChatSurfaces(surfaces) {
  const candidates = (surfaces || []).filter(
    (surface) => isVisibleSurface(surface) && !isCandidateAssetApprovalSurface(surface)
  );
  const actionable = candidates.filter(isActionableSurface);
  if (actionable.length <= 1) {
    return candidates;
  }
  const pendingApprovals = actionable.filter(
    (surface) => approvalIDFromSurface(surface) && !isTerminalApprovalStatus(surface?.payload?.status)
  );
  const selected = (pendingApprovals.length ? pendingApprovals : actionable)
    .slice()
    .sort(compareActionableSurfaces)[0];
  return candidates.filter((surface) => !isActionableSurface(surface) || surface === selected);
}

function isActionableSurface(surface) {
  return Boolean(approvalIDFromSurface(surface)) || isApprovalLikeSurface(surface) || surfaceFields(surface).some(isInputField);
}

function compareActionableSurfaces(left, right) {
  const leftApprovalRank = approvalArtifactRank(left);
  const rightApprovalRank = approvalArtifactRank(right);
  if (leftApprovalRank !== rightApprovalRank) {
    return leftApprovalRank - rightApprovalRank;
  }
  return timelineOrder(left, Number.MAX_SAFE_INTEGER) - timelineOrder(right, Number.MAX_SAFE_INTEGER);
}

function approvalArtifactRank(surface) {
  const artifactType = String(surface?.payload?.data?.artifact_type || surface?.payload?.artifact_type || '').toLowerCase();
  if (artifactType === 'creation_spec_revision') return 0;
  if (artifactType === 'storyboard_revision') return 1;
  return 2;
}

// selectCurrentCapabilityRun 把同一能力的 stage/operation 投影收敛成一张当前状态卡。
function selectCurrentCapabilityRun(toolRuns, activeApproval) {
  const byCapability = new Map();
  (toolRuns || []).forEach((toolRun) => {
    if (toolRun?.job_id) {
      return;
    }
    const key = canonicalCapabilityKey(toolRun?.tool_key);
    if (!key) {
      return;
    }
    const previous = byCapability.get(key);
    byCapability.set(key, preferredCapabilityProjection(previous, { ...toolRun, tool_key: key }));
  });
  const approvalCapability = approvalCapabilityKey(activeApproval);
  if (approvalCapability && byCapability.has(approvalCapability)) {
    return byCapability.get(approvalCapability);
  }
  const candidates = [...byCapability.values()].filter((toolRun) => {
    const status = String(toolRun?.status || '').toLowerCase();
    const planning = ['plan_creation_spec', 'plan_storyboard'].includes(toolRun?.tool_key);
    // A planning waiting_user state is actionable only while its authoritative
    // Approval is present. This also cleans up legacy histories that predate
    // the correlated Decision -> ToolRun terminal update.
    return status !== 'waiting_user' || !planning;
  });
  if (!candidates.length) {
    return null;
  }
  return candidates.sort(compareCapabilityRuns)[0];
}

function canonicalCapabilityKey(value) {
  const key = String(value || '').trim().toLowerCase();
  if (publicCapabilityOrder.includes(key)) {
    return key;
  }
  if (key === 'media_generator') {
    return 'generate_media';
  }
  if (key === 'video_assembler') {
    return 'assemble_output';
  }
  return '';
}

function approvalCapabilityKey(surface) {
  const artifactType = String(surface?.payload?.data?.artifact_type || surface?.payload?.artifact_type || '').toLowerCase();
  if (artifactType === 'creation_spec_revision') return 'plan_creation_spec';
  if (artifactType === 'storyboard_revision') return 'plan_storyboard';
  return canonicalCapabilityKey(surface?.payload?.data?.tool_key || surface?.payload?.tool_key);
}

function preferredCapabilityProjection(previous, incoming) {
  if (!previous) {
    return incoming;
  }
  const previousVersion = statusProjectionVersion(previous.status_version) ?? -1;
  const incomingVersion = statusProjectionVersion(incoming.status_version) ?? -1;
  if (previousVersion !== incomingVersion) {
    return incomingVersion > previousVersion ? incoming : previous;
  }
  const previousOperation = String(previous.data_model_key || '').startsWith('operation:') ? 1 : 0;
  const incomingOperation = String(incoming.data_model_key || '').startsWith('operation:') ? 1 : 0;
  if (previousOperation !== incomingOperation) {
    return incomingOperation > previousOperation ? incoming : previous;
  }
  return timelineOrder(incoming, -1) >= timelineOrder(previous, -1) ? incoming : previous;
}

function compareCapabilityRuns(left, right) {
  const statusDifference = capabilityStatusPriority(right.status) - capabilityStatusPriority(left.status);
  if (statusDifference) {
    return statusDifference;
  }
  const stageDifference = publicCapabilityOrder.indexOf(right.tool_key) - publicCapabilityOrder.indexOf(left.tool_key);
  if (stageDifference) {
    return stageDifference;
  }
  return timelineOrder(right, -1) - timelineOrder(left, -1);
}

function capabilityStatusPriority(status) {
  const normalized = String(status || '').toLowerCase();
  if (normalized === 'waiting_user') return 4;
  if (isInProgressStatus(normalized)) return 3;
  if (['failed', 'partial_failed'].includes(normalized)) return 2;
  return 1;
}

function capabilityDisplayName(key) {
  return publicCapabilityLabels[canonicalCapabilityKey(key)] || '创作进度';
}

function capabilityStatusMessage(key, status) {
  const normalized = String(status || '').toLowerCase();
  const label = capabilityDisplayName(key);
  if (normalized === 'waiting_user') {
    return `${label}已准备好，请完成下方唯一的审核后继续。`;
  }
  if (isInProgressStatus(normalized)) {
    return `${label}正在进行，完成后会自动同步到左侧故事板。`;
  }
  if (['failed', 'partial_failed', 'cancelled'].includes(normalized)) {
    return `${label}未完整完成，可以重试或在对话中调整需求。`;
  }
  if (canonicalCapabilityKey(key) === 'generate_media') {
    return '素材已生成，请在左侧故事板查看并统一确认。';
  }
  return `${label}已完成，结果已同步到工作区。`;
}

function generationJobSummary(jobs, candidateReviewReady = false) {
  const items = Array.isArray(jobs) ? jobs : [];
  if (!items.length) {
    return { tone: 'idle', label: '暂无后台生成任务' };
  }
  const statuses = items.map((job) => String(job?.status || job?.Status || '').toLowerCase());
  const running = statuses.filter((status) => isInProgressStatus(status)).length;
  const failed = statuses.filter((status) => ['failed', 'partial_failed', 'cancelled'].includes(status)).length;
  const completed = statuses.filter((status) => isTerminalGenerationJob(status) && !['failed', 'partial_failed', 'cancelled'].includes(status)).length;
  if (running) {
    return { tone: 'running', label: `${running} 项正在生成 · ${completed}/${items.length} 项已完成` };
  }
  if (failed) {
    return { tone: 'failed', label: `${completed}/${items.length} 项已完成 · ${failed} 项需要处理` };
  }
  return candidateReviewReady
    ? { tone: 'complete', label: '素材已生成，可在左侧故事板查看与统一确认' }
    : { tone: 'complete', label: '后台生成任务已完成' };
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

// JobStatusSummary 只显示聚合生成状态，不向用户暴露 Job/target/asset 内部标识。
function JobStatusSummary({ jobs, candidateReviewReady }) {
  const summary = generationJobSummary(jobs, candidateReviewReady);
  return (
    <div
      className={`aigc-job-strip aigc-job-summary aigc-job-summary--${summary.tone}`}
      aria-label="生成任务"
      aria-live="polite"
      role="status"
    >
      {summary.tone === 'running' ? (
        <LoaderCircle aria-hidden="true" size={14} />
      ) : summary.tone === 'failed' ? (
        <AlertCircle aria-hidden="true" size={14} />
      ) : (
        <CheckCircle2 aria-hidden="true" size={14} />
      )}
      <span>{summary.label}</span>
    </div>
  );
}

// ToolRunCard 渲染工具运行进度卡，通常由 update_card/tool_runs 更新。
function ToolRunCard({ toolRun, busy, onControl }) {
  const status = toolRun.status || 'running';
  const inProgress = isInProgressStatus(status);
  const failed = ['failed', 'partial_failed', 'cancelled'].includes(String(status || '').toLowerCase());
  const operationID = toolRun.operation_id;
  const canCancel = operationID && !toolRun.job_id && ['accepted', 'queued', 'waiting_jobs', 'waiting_provider', 'running', 'finalizing', 'retry_wait'].includes(status);
  const canRetry = operationID && !toolRun.job_id && ['failed', 'partial_failed'].includes(status);
  const capabilityKey = canonicalCapabilityKey(toolRun.tool_key);
  const displayName = capabilityDisplayName(capabilityKey);
  return (
    <article className={`aigc-tool-run aigc-tool-run--${status}`} aria-label={`${displayName}进度`}>
      <header>
        <div>
          {inProgress ? <LoaderCircle aria-hidden="true" size={15} /> : failed ? <AlertCircle aria-hidden="true" size={15} /> : <CheckCircle2 aria-hidden="true" size={15} />}
          <strong>{displayName}</strong>
        </div>
        <span>{statusLabel(status) || status}</span>
      </header>
      <p>{capabilityStatusMessage(capabilityKey, status)}</p>
      {failed && toolRun.error_message ? <p className="aigc-tool-run__error">生成未完成，请重试或调整需求。</p> : null}
      {canCancel || canRetry ? (
        <div className="aigc-tool-run__actions">
          {canCancel ? <button type="button" disabled={busy} onClick={() => void onControl?.(operationID, 'cancel')}>取消任务</button> : null}
          {canRetry ? <button type="button" disabled={busy} onClick={() => void onControl?.(operationID, 'retry_failed')}>重试失败项</button> : null}
        </div>
      ) : null}
    </article>
  );
}

function isInProgressStatus(status) {
  return ['queued', 'accepted', 'waiting_jobs', 'waiting_provider', 'running', 'finalizing', 'retry_wait', 'cancelling'].includes(String(status || '').toLowerCase());
}

// A2UISurfaceCard 渲染 Agent append_card 生成的聊天交互卡。
function A2UISurfaceCard({ surface, busy, sessionID, onAssetUploaded, onSubmit }) {
  const payload = surface?.payload || {};
  const data = payload.data || {};
  const approvalID = approvalIDFromSurface(surface);
  const invalidApprovalSurface = !approvalID && isApprovalLikeSurface(surface);
  const approvalTerminal = Boolean(approvalID) && isTerminalApprovalStatus(payload.status);
  // Approval 的决定控件由前端固定生成，不能把模型输出的普通字段当成审批入口。
  const fields = approvalID ? [approvalDecisionField()] : invalidApprovalSurface ? [] : surfaceFields(surface);
  const [values, setValues] = useState(() => initialSurfaceValues(fields));
  const title = approvalID ? approvalSurfaceTitle(surface) : payload.title || payload.label || '补充信息';
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
          <h2>{title}</h2>
          {payload.status ? <span>{approvalID ? '需要确认' : statusLabel(payload.status)}</span> : null}
        </header>
      ) : null}
      {payload.message ? <p>{payload.message}</p> : null}
      {hasComponentTree && approvalID ? (
        <details className="aigc-a2ui-card__details">
          <summary>查看完整审核内容</summary>
          <A2UIComponentTree
            surface={surface}
            data={data}
            values={values}
            approvalMode
            sessionID={sessionID}
            onAssetUploaded={onAssetUploaded}
            onValueChange={(key, value) => setValues((current) => ({ ...current, [key]: value }))}
          />
        </details>
      ) : hasComponentTree ? (
        <A2UIComponentTree
          surface={surface}
          data={data}
          values={values}
          approvalMode={Boolean(approvalID) || invalidApprovalSurface}
          sessionID={sessionID}
          onAssetUploaded={onAssetUploaded}
          onValueChange={(key, value) => setValues((current) => ({ ...current, [key]: value }))}
        />
      ) : null}
      {invalidApprovalSurface ? (
        <p className="aigc-a2ui-card__protocol-error" role="alert">
          审批卡缺少 approval_id，无法提交。请刷新页面或重新发起审核。
        </p>
      ) : null}
      {approvalID && !approvalTerminal ? (
        <div className="aigc-a2ui-card__next-step">
          <strong>下一步</strong>
          <span>确认后进入下一阶段；如需调整，请选择拒绝后在对话中说明。</span>
        </div>
      ) : null}
      <form
        className="aigc-a2ui-form"
        onSubmit={(event) => {
          event.preventDefault();
          void onSubmit(surface, values);
        }}
      >
        {approvalID || !hasComponentTree
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
          <button type="submit" disabled={busy || approvalTerminal || !hasInteractiveFields}>
            <CheckCircle2 aria-hidden="true" size={15} />
            <span>提交</span>
          </button>
        ) : null}
      </form>
    </article>
  );
}

// A2UIComponentTree 从 components/root 构建 A2UI 组件树。
function A2UIComponentTree({ surface, data, values, approvalMode, sessionID, onAssetUploaded, onValueChange }) {
  const payload = surface?.payload || {};
  // 审批卡仍展示 Markdown/预览等详情，但其交互字段必须由固定 Approval 表单提供。
  const displayComponents = approvalMode
    ? (payload.components || []).filter((component) => !componentField(component))
    : payload.components;
  const components = componentMap(displayComponents);
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
  const audioPreview = componentPayload(component, A2UI_COMPONENTS.AUDIO_PREVIEW);
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
  if (audioPreview) {
    return <A2UIAudioPreview item={audioPreview} />;
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
  if (type === 'audio_preview') {
    return <A2UIAudioPreview item={field} />;
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
  if (isAudioAsset(asset) && asset.url) {
    return (
      <figure className="aigc-a2ui-file-preview aigc-a2ui-file-preview--audio">
        <audio src={asset.url} controls preload="metadata" />
        <figcaption>{label}</figcaption>
      </figure>
    );
  }
  const document = (
    <>
      <FileText aria-hidden="true" size={18} />
      <span>{label}</span>
    </>
  );
  return asset.url ? (
    <a className="aigc-a2ui-file-preview aigc-a2ui-file-preview--document" href={asset.url} target="_blank" rel="noreferrer">
      {document}
    </a>
  ) : (
    <div className="aigc-a2ui-file-preview aigc-a2ui-file-preview--document">{document}</div>
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

// A2UIAudioPreview 渲染 A2UI 音频试听组件。
function A2UIAudioPreview({ item }) {
  const url = item.url;
  if (!url) {
    return null;
  }
  return (
    <figure className="aigc-a2ui-media">
      <audio src={url} controls preload="metadata" />
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

// resolveSessionID 从 URL/localStorage 读取会话；本地数据卷被清空后，旧缓存会自动失效并创建新会话。
async function resolveSessionID() {
  const params = new URLSearchParams(window.location.search);
  const explicit = params.get('session_id');
  const cached = readLocalSessionID();
  const candidate = explicit || cached;
  if (candidate) {
    const existing = await requestOptionalJSON(`/api/aigc/sessions/${candidate}/messages`);
    if (existing) {
      writeLocalSessionID(candidate);
      return candidate;
    }
    clearLocalSessionID();
  }
  return createSession();
}

// createSession 调用后端创建 demo 会话并缓存 session_id。
async function createSession() {
  const session = await requestJSON('/api/aigc/sessions', {
    method: 'POST',
    body: JSON.stringify({
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

// clearLocalSessionID 在后端已删除本地历史数据时移除失效会话指针。
function clearLocalSessionID() {
  try {
    if (typeof window.localStorage?.removeItem === 'function') {
      window.localStorage.removeItem(SESSION_STORAGE_KEY);
    }
  } catch {
    // Session recovery still continues when browser storage is unavailable.
  }
}

// applyStoryboardPatch 应用故事板 JSON Patch 或生成素材更新 hint。
function applyStoryboardPatch(current, patch, onGap) {
  if (!current) {
    return current;
  }
  const baseVersion = finiteVersion(patch?.base_version);
  const currentVersion = finiteVersion(current.version);
  if (baseVersion > 0 && baseVersion !== currentVersion) {
    if (baseVersion > currentVersion && typeof onGap === 'function') {
      onGap();
    }
    return current;
  }
  if (patch?.storyboard_id) {
    const currentID = current.id || current.storyboard_id;
    if (currentID && currentID !== patch.storyboard_id) {
      return current;
    }
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
  const revision = activeRevisionFromBoard(board);
  const dynamicTarget = (revision?.modules || board?.modules || [])
    .flatMap((module) => module.elements || [])
    .find((item) => item.id === targetID || item.key === targetID);
  if (dynamicTarget) {
    return dynamicTarget;
  }
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

// upsertVersionedByID 对带 status_version 的状态投影执行单调更新。
function upsertVersionedByID(items, item, key) {
  const id = item?.[key] || item?.id;
  const previous = items.find((existing) => (existing?.[key] || existing?.id) === id);
  if (previous && isOlderStatusProjection(previous, item)) {
    return items;
  }
  return upsertByID(items, item, key);
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
  // Job lifecycle patches only carry versioned nodes. Their version belongs to
  // each Job, not to the enclosing Operation/Stage, so the top-level gate must
  // not discard the whole patch after operation.accepted established a version.
  if (
    previous &&
    !isStableCapabilityToolRun(toolRun) &&
    !isNodeOnlyToolRunPatch(toolRun) &&
    isOlderStatusProjection(previous, toolRun)
  ) {
    return items;
  }
  return [mergeToolRun(previous, toolRun), ...next];
}

// 稳定 Capability 卡会跨 Operation 复用，顺序由 session SSE seq 保证，不能沿用上一批次的 status_version。
function isStableCapabilityToolRun(toolRun) {
  return ['tool_run:generate_media', 'tool_run:assemble_output'].includes(String(toolRun?.data_model_key || ''));
}

function isNodeOnlyToolRunPatch(toolRun) {
  return (
    statusProjectionVersion(toolRun?.status_version) == null &&
    toolRun?.status == null &&
    Array.isArray(toolRun?.nodes) &&
    toolRun.nodes.length > 0
  );
}

function isOlderStatusProjection(previous, incoming) {
  const previousVersion = statusProjectionVersion(previous?.status_version);
  if (previousVersion == null) {
    return false;
  }
  const incomingVersion = statusProjectionVersion(incoming?.status_version);
  return incomingVersion == null || incomingVersion < previousVersion;
}

function statusProjectionVersion(value) {
  const version = Number(value);
  return Number.isInteger(version) && version >= 0 ? version : null;
}

// mergeToolRun 合并工具运行卡，保留首次出现的时间线顺序。
function mergeToolRun(previous, incoming) {
  if (!previous) {
    return incoming;
  }
  const merged = {
    ...previous,
    ...incoming,
    timelineOrder: previous.timelineOrder ?? incoming.timelineOrder,
    nodes: mergeToolRunNodes(previous.nodes, incoming.nodes)
  };
  if (isStableCapabilityToolRun(incoming) && statusProjectionVersion(incoming.status_version) == null) {
    delete merged.status_version;
  }
  return merged;
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
      if (isOlderStatusProjection(merged[index], node)) {
        return;
      }
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
    status_version: toolRun.status_version,
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

// mergeHydratedSurfaces 把 REST 历史视图并入已经由 SSE 更新的实时视图。
// 已提交的普通 A2UI 表单仍以消息历史为准，不会被历史 SSE append 重新带回。
function mergeHydratedSurfaces(current, hydrated, submittedSurfaceIDs = [], terminalApprovalSurfaceIDs = new Set()) {
  const submitted = new Set(submittedSurfaceIDs || []);
  let merged = (hydrated || []).filter((surface) => !isTerminalApprovalSurface(surface, terminalApprovalSurfaceIDs));
  (current || []).forEach((surface) => {
    const id = surface?.id || surface?.card_id || surface?.ref;
    if (!id || submitted.has(id) || isTerminalApprovalSurface(surface, terminalApprovalSurfaceIDs)) {
      return;
    }
    merged = upsertSurface(merged, surface);
  });
  return merged;
}

// mergeStoryboardSnapshot 防止较慢的 REST 快照覆盖已经由 SSE 投影出的更高版本。
function mergeStoryboardSnapshot(current, incoming) {
  if (!incoming) {
    return current;
  }
  if (!current) {
    return incoming;
  }
  const currentVersion = finiteVersion(current.version);
  const incomingVersion = finiteVersion(incoming.version);
  if (incomingVersion > currentVersion) {
    return incoming;
  }
  if (incomingVersion < currentVersion) {
    return current;
  }
  const currentID = current.id || current.storyboard_id;
  const incomingID = incoming.id || incoming.storyboard_id;
  if (currentID && incomingID && currentID !== incomingID) {
    return current;
  }
  // 同版本时实时对象优先；REST 只补充 SSE 投影未携带的顶层字段。
  return { ...incoming, ...current };
}

function finiteVersion(value) {
  const version = Number(value);
  return Number.isFinite(version) && version >= 0 ? version : 0;
}

// freezeApprovalDecisionRequest 为一次 Approval 冻结同一 version/key；网络重试不得漂移为下一次决定。
function freezeApprovalDecisionRequest(cache, approvalID, decision, expectedDecisionVersion) {
  const id = String(approvalID || '').trim();
  const normalizedDecision = String(decision || '').trim().toLowerCase();
  const expectedVersion = finiteVersion(expectedDecisionVersion);
  const cacheKey = JSON.stringify([id, normalizedDecision, expectedVersion]);
  const existing = cache.get(cacheKey);
  if (existing) {
    return existing;
  }
  const request = {
    decision: normalizedDecision,
    expected_decision_version: expectedVersion,
    idempotency_key: `approval:${id}:decision:${expectedVersion + 1}:${normalizedDecision}`
  };
  cache.set(cacheKey, request);
  return request;
}

// freezeCandidateApprovalDecisionRequest 为故事板级候选确认冻结同一版本和幂等键。
function freezeCandidateApprovalDecisionRequest(cache, storyboardID, expectedStoryboardVersion) {
  const id = String(storyboardID || '').trim();
  const expectedVersion = finiteVersion(expectedStoryboardVersion);
  const cacheKey = JSON.stringify([id, expectedVersion, 'approved']);
  const existing = cache.get(cacheKey);
  if (existing) {
    return existing;
  }
  const request = {
    decision: 'approved',
    expected_storyboard_version: expectedVersion,
    idempotency_key: `candidate-approvals:${id}:v${expectedVersion}:approved`
  };
  cache.set(cacheKey, request);
  return request;
}

// candidateApprovalResultIDs 返回批量响应中已经到达终态、可从 UI 移除的 Approval。
function candidateApprovalResultIDs(result, bindings) {
  const allApprovalIDs = (bindings || []).map((binding) => String(binding?.approval_id || '').trim()).filter(Boolean);
  if (result?.summary?.complete || result?.summary?.all_approved) {
    return new Set(allApprovalIDs);
  }
  return new Set(
    (result?.results || [])
      .filter((item) => item?.applied || ['approved', 'stale', 'rejected', 'cancelled'].includes(String(item?.status || '').toLowerCase()))
      .map((item) => String(item?.approval_id || '').trim())
      .filter(Boolean)
  );
}

function approvalDecisionVersion(surface) {
  return surface?.payload?.data?.decision_version ?? surface?.payload?.decision_version ?? 0;
}

// approvalDecisionField 固定 Approval 的唯一交互协议，避免模型用普通表单伪造审批入口。
function approvalDecisionField() {
  return {
    key: 'decision',
    label: '审核决定',
    type: 'single_choice',
    required: true,
    options: [
      { value: 'approved', label: '确认' },
      { value: 'rejected', label: '拒绝' }
    ]
  };
}

function isApprovalDecision(decision) {
  return decision === 'approved' || decision === 'rejected';
}

function isTerminalApprovalStatus(status) {
  return ['approved', 'rejected', 'stale', 'expired', 'cancelled', 'canceled'].includes(String(status || '').trim().toLowerCase());
}

function approvalIDFromSurface(surface) {
  return String(surface?.payload?.data?.approval_id || surface?.payload?.approval_id || '').trim();
}

function approvalSurfaceTitle(surface) {
  const artifactType = String(surface?.payload?.data?.artifact_type || surface?.payload?.artifact_type || '').toLowerCase();
  if (artifactType === 'creation_spec_revision') return '确认创作规范';
  if (artifactType === 'storyboard_revision') return '确认故事板方案';
  return surface?.payload?.title || surface?.payload?.label || '确认当前方案';
}

// pendingPrimaryApprovalArtifactType 只识别系统主流程中待处理的 Spec/Storyboard Approval。
function pendingPrimaryApprovalArtifactType(surface) {
  if (!approvalIDFromSurface(surface) || isTerminalApprovalStatus(surface?.payload?.status)) {
    return '';
  }
  const artifactType = String(
    surface?.payload?.data?.artifact_type || surface?.payload?.artifact_type || ''
  ).trim().toLowerCase();
  return ['creation_spec_revision', 'storyboard_revision'].includes(artifactType) ? artifactType : '';
}

// reviewPreviewArtifactType 识别模型常见的 spec-review/storyboard-preview 说明卡。
function reviewPreviewArtifactType(surface) {
  const payload = surface?.payload || {};
  const explicitType = String(payload?.data?.artifact_type || payload?.artifact_type || '').trim().toLowerCase();
  if (['creation_spec_revision', 'storyboard_revision'].includes(explicitType)) {
    return explicitType;
  }
  const semanticText = [
    surface?.id,
    surface?.card_id,
    surface?.ref,
    payload?.title,
    payload?.label,
    payload?.data?.tool_key,
    payload?.tool_key
  ]
    .map((value) => String(value || '').trim().toLowerCase())
    .filter(Boolean)
    .join(' ');
  if (/storyboard|story[-_ ]?board|故事板/.test(semanticText)) {
    return 'storyboard_revision';
  }
  if (/creation[_ -]?spec|final[_ -]?video[_ -]?spec|spec[-_ ]?(review|preview)|创作规范|规格预览|规范预览/.test(semanticText)) {
    return 'creation_spec_revision';
  }
  return '';
}

function isNonInteractiveReviewPreviewSurface(surface) {
  return (
    !approvalIDFromSurface(surface) &&
    Boolean(reviewPreviewArtifactType(surface)) &&
    !surfaceFields(surface).some(isInputField)
  );
}

// isApprovalLikeSurface 识别缺少 approval_id 的伪审批表单，禁止降级成普通聊天消息提交。
function isApprovalLikeSurface(surface) {
  return surfaceFields(surface).some((field) => String(fieldKey(field)).trim().toLowerCase() === 'decision');
}

function approvalIDFromAction(action) {
  return String(action?.payload?.data?.approval_id || action?.payload?.approval_id || action?.card?.data?.approval_id || '').trim();
}

function isCandidateAssetApprovalSurface(surface) {
  const payload = surface?.payload || {};
  const artifactType = payload?.data?.artifact_type || payload?.artifact_type;
  return String(artifactType || '').trim().toLowerCase() === 'candidate_asset';
}

// candidateApprovalReviewState 只在本轮候选所属任务全部终态后开放统一确认。
function candidateApprovalReviewState(storyboard, jobs) {
  const bindings = (storyboard?.bindings || []).filter(
    (binding) => String(binding?.state || '').toLowerCase() === 'candidate' && String(binding?.approval_id || '').trim()
  );
  if (!storyboard || storyboard.pending_revision_id || bindings.length === 0) {
    return { ready: false, bindings: storyboard?.pending_revision_id ? [] : bindings, jobs: [] };
  }
  const jobList = Array.isArray(jobs) ? jobs : [];
  const candidateJobs = jobList.filter((job) => bindings.some((binding) => jobMatchesCandidateBinding(job, binding)));
  if (candidateJobs.length === 0) {
    return { ready: false, bindings, jobs: [] };
  }

  const batchIDs = new Set(candidateJobs.map((job) => String(job?.batch_id || '')).filter(Boolean));
  const operationIDs = new Set(candidateJobs.map((job) => String(job?.operation_id || '')).filter(Boolean));
  let relatedJobs;
  if (batchIDs.size > 0 || operationIDs.size > 0) {
    relatedJobs = jobList.filter(
      (job) => batchIDs.has(String(job?.batch_id || '')) || operationIDs.has(String(job?.operation_id || ''))
    );
  } else {
    const storyboardID = String(storyboard.id || storyboard.storyboard_id || '');
    const targetIDs = new Set(bindings.map((binding) => String(binding?.target_id || '')).filter(Boolean));
    relatedJobs = jobList.filter((job) => {
      if (String(job?.provider || '').toLowerCase() === 'assembly') {
        return false;
      }
      return (
        (storyboardID && String(job?.storyboard_id || '') === storyboardID) ||
        targetIDs.has(String(job?.target_id || job?.TargetID || ''))
      );
    });
    if (relatedJobs.length === 0) {
      relatedJobs = candidateJobs;
    }
  }
  const ready = relatedJobs.length > 0 && relatedJobs.every((job) => isTerminalGenerationJob(job?.status || job?.Status));
  return { ready, bindings, jobs: relatedJobs };
}

function jobMatchesCandidateBinding(job, binding) {
  const assetID = String(binding?.asset_id || '').trim();
  const resultAssetIDs = Array.isArray(job?.result_asset_ids) ? job.result_asset_ids.map(String) : [];
  if (assetID && resultAssetIDs.includes(assetID)) {
    return true;
  }
  const targetID = String(job?.target_id || job?.TargetID || '').trim();
  const assetSlot = String(job?.asset_slot || '').trim();
  return (
    targetID &&
    targetID === String(binding?.target_id || '').trim() &&
    (!assetSlot || !binding?.asset_slot || assetSlot === String(binding.asset_slot))
  );
}

function isTerminalGenerationJob(status) {
  return ['succeeded', 'failed', 'cancelled', 'completed', 'done', 'partial_failed'].includes(
    String(status || '').trim().toLowerCase()
  );
}

function approvalSurfaceKeys(surface, approvalID) {
  const keys = new Set();
  [surface?.id, surface?.surface_id, surface?.card_id, surface?.ref].forEach((value) => {
    const key = String(value || '').trim();
    if (key) {
      keys.add(key);
    }
  });
  const id = String(approvalID || approvalIDFromSurface(surface) || '').trim();
  if (id) {
    keys.add(id);
    keys.add(`approval:${id}`);
  }
  return keys;
}

function markApprovalTerminal(tombstones, surface, approvalID) {
  if (!tombstones) {
    return;
  }
  approvalSurfaceKeys(surface, approvalID).forEach((key) => tombstones.add(key));
}

function isTerminalApprovalSurface(surface, tombstones) {
  if (!tombstones?.size) {
    return false;
  }
  return [...approvalSurfaceKeys(surface)].some((key) => tombstones.has(key));
}

function isSameApprovalSurface(candidate, source, approvalID) {
  const sourceKeys = approvalSurfaceKeys(source, approvalID);
  return [...approvalSurfaceKeys(candidate)].some((key) => sourceKeys.has(key));
}

function isTerminalApprovalAction(action, targetID, approvalID) {
  return (
    isTerminalApprovalStatus(action?.payload?.status ?? action?.card?.status) &&
    (String(targetID || '').startsWith('approval:') || Boolean(approvalID))
  );
}

// applyA2UIActionEnvelope 按顺序执行 Agent 直出的 A2UI actions。
function applyA2UIActionEnvelope(envelope, context) {
  if (!isSupportedA2UIActionEnvelope(envelope)) {
    return;
  }
  // Agent 可以一次返回多个 action，前端按数组顺序同步应用。
  const actions = Array.isArray(envelope?.actions) ? envelope.actions : [];
  actions.forEach((action) => applyA2UIAction(action, context));
}

// applyA2UIAction 根据 action type 分发到新增卡片或更新卡片逻辑。
function applyA2UIAction(action, context) {
  const type = String(action?.type || '').trim();
  if (type === 'append_card') {
    appendA2UICard(action, context);
  } else if (type === 'update_card') {
    updateA2UICard(action, context);
  }
  if (shouldRefreshWorkspaceResources(action)) {
    void context.refreshSessionData?.(context.sessionID, { includeMessages: false });
  }
}

// shouldRefreshWorkspaceResources 响应后端终态资源提示；提示本身不创建任何聊天卡片。
function shouldRefreshWorkspaceResources(action) {
  const resources = action?.payload?.refresh_resources;
  return Array.isArray(resources) && resources.some((resource) => ['storyboard', 'assets', 'jobs'].includes(resource));
}

// appendA2UICard 新增 A2UI 卡片；后端下发的 card_id 已经是实例级唯一值。
function appendA2UICard(action, { setSurfaces, nextTimelineOrder, terminalApprovalSurfaceIDs }) {
  setSurfaces((items) =>
    reduceChatSurfaceAction(items, action, {
      timelineOrder: nextTimelineOrder(),
      terminalApprovalSurfaceIDs
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
      context.markResourceMutation?.('storyboard');
      context.setStoryboard((current) => mergeStoryboardSnapshot(current, action.payload.storyboard));
    }
    if (action.payload?.assets) {
      context.markResourceMutation?.('assets');
      context.setAssets(action.payload.assets);
    }
    if (payloadPatch) {
      context.markResourceMutation?.('storyboard');
      context.setStoryboard((current) =>
        applyStoryboardPatch(current, payloadPatch, () => {
          void context.refreshSessionData?.(context.sessionID, { includeMessages: false });
        })
      );
    } else if (Array.isArray(action.patch)) {
      context.markResourceMutation?.('storyboard');
      context.setStoryboard((current) => applyStoryboardPatch(current, { ops: action.patch }));
    }
    return;
  }
  if (targetSurface === 'tool_runs' && (action.payload?.tool_run || action.payload?.operation)) {
    const projectedRun = action.payload.tool_run || action.payload.operation;
    const projectedStatusVersion = statusProjectionVersion(
      projectedRun.status_version ?? action.payload?.status_version
    );
    const toolRun = {
      ...projectedRun,
      data_model_key: target.card_id || action.card_id || target.ref || action.ref || projectedRun.data_model_key,
      ...(projectedStatusVersion == null ? {} : { status_version: projectedStatusVersion }),
      timelineOrder: context.nextTimelineOrder()
    };
    context.setToolRuns((items) => upsertToolRun(items, toolRun));
    if (toolRun.job_id) {
      context.markResourceMutation?.('jobs');
      context.setJobs((items) => upsertVersionedByID(items, toolRunToJob(toolRun), 'job_id'));
      if (toolRun.status === 'succeeded' && toolRun.result_asset_ids?.length) {
        void context.refreshAssets(toolRun.session_id || context.sessionID);
      }
    }
    return;
  }

  context.setSurfaces((items) =>
    reduceChatSurfaceAction(items, action, {
      terminalApprovalSurfaceIDs: context.terminalApprovalSurfaceIDs
    })
  );
}

// reduceChatSurfaceAction 是聊天卡实时投影和历史回放共用的纯 reducer。
function reduceChatSurfaceAction(items, action, options = {}) {
  const type = String(action?.type || '').trim();
  const terminalApprovalSurfaceIDs = options.terminalApprovalSurfaceIDs || new Set();
  if (type === A2UI_ACTIONS.APPEND_CARD) {
    const card = normalizeActionCard(action);
    const cardID = action.card_id || card.card_id || action.ref || '';
    if (!cardID) {
      return items;
    }
    const surface = {
      id: cardID,
      surface_id: cardID,
      card_id: cardID,
      message_id: action.message_id || options.messageID,
      ref: action.ref,
      surface: action.surface || 'chat',
      payload: card,
      timelineOrder: options.timelineOrder
    };
    if (isTerminalApprovalStatus(card.status) && approvalIDFromSurface(surface)) {
      markApprovalTerminal(terminalApprovalSurfaceIDs, surface, approvalIDFromSurface(surface));
      return items.filter((item) => !isSameApprovalSurface(item, surface, approvalIDFromSurface(surface)));
    }
    if (isTerminalApprovalSurface(surface, terminalApprovalSurfaceIDs)) {
      return items;
    }
    const primaryApprovalType = pendingPrimaryApprovalArtifactType(surface);
    if (primaryApprovalType) {
      // 系统 Approval 已经承载完整审核详情；清掉同阶段先到的模型预览卡，避免双入口。
      return upsertSurface(
        items.filter(
          (item) =>
            !isNonInteractiveReviewPreviewSurface(item) ||
            reviewPreviewArtifactType(item) !== primaryApprovalType
        ),
        surface
      );
    }
    const previewType = reviewPreviewArtifactType(surface);
    if (
      previewType &&
      isNonInteractiveReviewPreviewSurface(surface) &&
      items.some((item) => pendingPrimaryApprovalArtifactType(item) === previewType)
    ) {
      // Approval 之后到达的 model preview 不进入 state；Decision 移除 Approval 时也不会重新显现。
      return items;
    }
    return upsertSurface(items, surface);
  }
  if (type !== A2UI_ACTIONS.UPDATE_CARD) {
    return items;
  }

  const target = action.target || {};
  const targetID = target.card_id || action.card_id || target.ref || action.ref;
  if (!targetID) {
    return items;
  }
  const approvalID = approvalIDFromAction(action);
  if (isTerminalApprovalAction(action, targetID, approvalID)) {
    markApprovalTerminal(terminalApprovalSurfaceIDs, { id: targetID }, approvalID);
    return items.filter((surface) => !isSameApprovalSurface(surface, { id: targetID }, approvalID));
  }
  return items.map((surface) => {
    if (surface.id !== targetID && surface.card_id !== targetID && surface.ref !== targetID) {
      return surface;
    }
    const actionPayload = action.payload && typeof action.payload === 'object' ? action.payload : {};
    const payload = applyCardPatch(
      { ...(surface.payload || {}), ...normalizeActionCard(action), ...actionPayload },
      action.patch || []
    );
    return mergeSurface(surface, { ...surface, payload });
  });
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

// requiredSurfaceFieldsMissing 在提交前统一校验所有必填输入组件。
// 组件树通常不在原生 form 节点内，因此不能只依赖浏览器 required 属性。
function requiredSurfaceFieldsMissing(surface, values) {
  return surfaceFields(surface)
    .filter((field) => isInputField(field) && Boolean(field.required))
    .filter((field) => !hasRequiredFieldValue(field, values?.[fieldKey(field)]));
}

function hasRequiredFieldValue(field, value) {
  const type = fieldType(field);
  if (type === 'multi_choice') {
    return Array.isArray(value) && value.some((item) => String(item ?? '').trim() !== '');
  }
  if (type === 'file_upload') {
    return fileUploadValueList(value).some((asset) => Boolean(fileAssetID(asset)));
  }
  return String(value ?? '').trim() !== '';
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
  if (type.startsWith('audio/')) {
    return 'audio';
  }
  if (type === 'application/pdf') {
    return 'pdf';
  }
  if (type.startsWith('text/')) {
    return 'text';
  }
  return '';
}

function slotUploadKind(mediaKind, file) {
  const kind = String(mediaKind || '').toLowerCase();
  if (['image', 'illustration', 'keyframe'].includes(kind)) {
    return 'image';
  }
  if (['video', 'audio', 'music', 'voice', 'text', 'script', 'lyrics'].includes(kind)) {
    if (['text', 'script', 'lyrics'].includes(kind)) {
      return String(file?.type || '').toLowerCase() === 'application/pdf' ? 'pdf' : 'text';
    }
    return ['music', 'voice'].includes(kind) ? 'audio' : kind;
  }
  return uploadKind({}, file);
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

// isAudioAsset 识别可在本地 Demo 中直接播放的音频产物。
function isAudioAsset(asset) {
  const kind = String(asset?.kind || '').toLowerCase();
  const mimeType = String(asset?.mime_type || asset?.mimeType || '').toLowerCase();
  return ['audio', 'music', 'voice'].includes(kind) || mimeType.startsWith('audio/');
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
  if (type === 'audio_preview') {
    return 'audio_preview';
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
  const revision = activeRevisionFromBoard(board);
  const module = (revision?.modules || board?.modules || []).find((item) => item?.elements?.length);
  if (module?.elements?.[0]) {
    const element = module.elements[0];
    return {
      type: element.semantic_type || module.semantic_type || 'element',
      id: element.id || element.key,
      label: element.title || element.key || element.id,
      moduleID: module.id,
      targetRevision: element.revision
    };
  }
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

function storyboardContainsTarget(board, selected) {
  const selectedID = selected?.id || selected?.targetID;
  if (!selectedID) {
    return false;
  }
  const revision = activeRevisionFromBoard(board);
  if ((revision?.modules || board?.modules || []).some((module) =>
    (module.elements || []).some((element) => (element.id || element.key) === selectedID)
  )) {
    return true;
  }
  if ((board?.key_elements || []).some((element) => element.key === selectedID)) {
    return true;
  }
  if ((board?.shots || []).some((shot) => shot.shot_id === selectedID)) {
    return true;
  }
  return (board?.audio_layers || []).some((layer) => layer.layer_id === selectedID);
}

function activeRevisionFromBoard(board) {
  if (!board) {
    return null;
  }
  if (board.active_revision) {
    return board.active_revision;
  }
  const revisions = Array.isArray(board.revisions) ? board.revisions : Object.values(board.revisions || {});
  return (
    revisions.find((revision) => revision.id === board.pending_revision_id) ||
    revisions.find((revision) => revision.status === 'reviewing') ||
    revisions.find((revision) => revision.id === board.active_revision_id) ||
    revisions.find((revision) => revision.status === 'active') ||
    null
  );
}

// messageRecordToChatMessage 把后端消息记录转换成前端聊天消息。
function messageRecordToChatMessage(record) {
  const role = String(record?.role || '').toLowerCase();
  if (role !== 'user' && role !== 'assistant') {
    return null;
  }
  // Assistant rows carrying ToolCalls are durable ReAct history, not a
  // user-facing chat reply. Live rendering already skips them; hydration must
  // do the same or an internal transition sentence appears only after reload.
  if (role === 'assistant' && record?.tool_calls) {
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
  const submittedSurfaceIDs = new Set();
  const terminalApprovalSurfaceIDs = new Set();
  records.forEach((record, index) => {
    const timelineOrder = historyTimelineOrder(record, index);
    const submittedCardID = submittedA2UICardID(record);
    if (submittedCardID) {
      submittedSurfaceIDs.add(submittedCardID);
      surfaces = removeSurfaceByID(surfaces, submittedCardID);
    }
    const envelope = parseA2UIActionEnvelopeContent(record?.content);
    if (envelope) {
      surfaces = restoreSurfacesFromEnvelope(surfaces, envelope, record, timelineOrder, terminalApprovalSurfaceIDs);
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
    submittedSurfaceIDs: [...submittedSurfaceIDs],
    terminalApprovalSurfaceIDs: [...terminalApprovalSurfaceIDs],
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
    if (!isSupportedA2UIActionEnvelope(envelope)) {
      return null;
    }
    return envelope;
  } catch {
    return null;
  }
}

// restoreSurfacesFromEnvelope 回放历史 ActionEnvelope 中的聊天卡新增和更新。
function restoreSurfacesFromEnvelope(surfaces, envelope, record, timelineOrder, terminalApprovalSurfaceIDs = new Set()) {
  return envelope.actions.reduce((items, action, actionIndex) => {
    const type = String(action?.type || '').trim();
    if (type === A2UI_ACTIONS.APPEND_CARD || type === A2UI_ACTIONS.UPDATE_CARD) {
      return reduceChatSurfaceAction(items, action, {
        messageID: record?.id,
        timelineOrder: timelineOrder + actionIndex / 100,
        terminalApprovalSurfaceIDs
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
