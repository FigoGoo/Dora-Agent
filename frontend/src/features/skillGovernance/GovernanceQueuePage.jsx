import { useCallback, useEffect, useRef, useState } from 'react';
import { RefreshCw, ShieldCheck } from 'lucide-react';
import {
  getSkillGovernanceDetailPath,
  SKILL_GOVERNANCE_CAPABILITY
} from '../../app/routes.js';
import { useAuthSession } from '../../platform/auth/authSession.js';
import { listGovernanceSkills } from './governanceApi.js';
import {
  SKILL_GOVERNANCE_CAPABILITY_REQUIRED_CODE,
  SKILL_GOVERNANCE_STATUSES
} from './governanceContract.js';

const STATUS_LABELS = Object.freeze({
  active: '正常',
  suspended: '已暂停',
  offline: '已下架'
});

export function GovernanceQueuePage({
  loadSkills = listGovernanceSkills,
  onNavigate = navigate
}) {
  const auth = useAuthSession();
  const retryBootstrap = auth.retryBootstrap;
  const [status, setStatus] = useState('active');
  const [state, setState] = useState({
    kind: 'loading',
    items: [],
    nextCursor: null,
    error: null,
    retryCursor: null,
    retryAppend: false
  });
  const itemsRef = useRef([]);
  const requestRef = useRef(null);
  const generationRef = useRef(0);
  const capabilityRefreshRef = useRef(false);

  const load = useCallback(async ({ cursor = null, append = false } = {}) => {
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    const generation = ++generationRef.current;
    if (!append) itemsRef.current = [];
    setState((current) => ({
      ...current,
      kind: append ? 'loading_more' : 'loading',
      items: append ? current.items : [],
      nextCursor: append ? current.nextCursor : null,
      error: null,
      retryCursor: cursor,
      retryAppend: append
    }));
    try {
      const result = await loadSkills({
        status,
        cursor,
        signal: controller.signal
      });
      if (generation !== generationRef.current || controller.signal.aborted) return;
      const items = append
        ? mergeGovernanceItems(itemsRef.current, result.items)
        : result.items;
      itemsRef.current = items;
      setState({
        kind: 'ready',
        items,
        nextCursor: result.nextCursor,
        error: null,
        retryCursor: null,
        retryAppend: false
      });
    } catch (error) {
      if (generation !== generationRef.current || controller.signal.aborted || error?.name === 'AbortError') return;
      const kind = errorKind(error);
      const clearItems = kind === 'forbidden';
      if (clearItems) itemsRef.current = [];
      setState((current) => ({
        ...current,
        kind,
        items: clearItems ? [] : current.items,
        nextCursor: clearItems ? null : current.nextCursor,
        error: publicError(error, '暂时无法加载 Skill 治理队列。'),
        retryCursor: cursor,
        retryAppend: append
      }));
      if (isCapabilityDenied(error) && !capabilityRefreshRef.current) {
        capabilityRefreshRef.current = true;
        retryBootstrap({ deniedCapability: SKILL_GOVERNANCE_CAPABILITY }).catch(() => {});
      }
    } finally {
      if (requestRef.current === controller) requestRef.current = null;
    }
  }, [loadSkills, retryBootstrap, status]);

  useEffect(() => {
    void load();
    return () => {
      generationRef.current += 1;
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, [load]);

  function refresh() {
    void load({ cursor: null, append: false });
  }

  return (
    <section className="skill-review-queue" aria-labelledby="skill-governance-queue-heading">
      <header className="skill-review-page__header">
        <div>
          <h2 id="skill-governance-queue-heading">治理队列</h2>
          <p>按当前发布快照检查治理状态；主动刷新会从第一页重新建立队列。</p>
        </div>
        <button type="button" className="secondary-button" disabled={state.kind === 'loading'} onClick={refresh}>
          <RefreshCw aria-hidden="true" size={16} />
          刷新队列
        </button>
      </header>

      <nav aria-label="治理状态筛选">
        {SKILL_GOVERNANCE_STATUSES.map((value) => (
          <button
            type="button"
            className="secondary-button"
            aria-pressed={status === value}
            key={value}
            onClick={() => setStatus(value)}
          >
            {STATUS_LABELS[value]}
          </button>
        ))}
      </nav>

      {state.kind === 'loading' ? <p role="status">正在加载 Skill 治理队列…</p> : null}
      {state.kind === 'error' ? (
        <GovernanceQueueError
          error={state.error}
          onRetry={() => load({ cursor: state.retryCursor, append: state.retryAppend })}
        />
      ) : null}
      {state.kind === 'forbidden' ? (
        <p role="alert">当前会话已失去 Skill 治理权限，正在重新确认权限。</p>
      ) : null}
      {state.kind === 'ready' && state.items.length === 0 ? (
        <section className="skill-state-panel">
          <ShieldCheck aria-hidden="true" size={42} />
          <h3>当前没有{STATUS_LABELS[status]}的 Skill</h3>
          <p>切换状态筛选或刷新队列以查看最新权威状态。</p>
        </section>
      ) : null}
      {state.items.length > 0 ? (
        <div className="skill-review-queue__list">
          {state.items.map((skill) => (
            <article key={skill.skillID} className="skill-review-card">
              <div>
                <span>{skill.category || '未分类'} · {STATUS_LABELS[skill.governanceStatus]}</span>
                <h3>{skill.name}</h3>
                <p>{skill.summary || '未填写简介'}</p>
                <small>发布于 {formatTime(skill.publishedAt)} · 治理纪元 {skill.governanceEpoch}</small>
              </div>
              <button
                type="button"
                className="secondary-button"
                onClick={() => onNavigate(getSkillGovernanceDetailPath(skill.skillID))}
              >
                查看治理详情
              </button>
            </article>
          ))}
        </div>
      ) : null}
      {state.nextCursor ? (
        <button
          type="button"
          className="secondary-button"
          disabled={state.kind === 'loading_more'}
          onClick={() => load({ cursor: state.nextCursor, append: true })}
        >
          {state.kind === 'loading_more' ? '正在加载…' : '加载更多'}
        </button>
      ) : null}
    </section>
  );
}

function GovernanceQueueError({ error, onRetry }) {
  return (
    <section className="skill-state-panel">
      <p role="alert">{error.message}</p>
      {error.requestID ? <small>请求 ID：{error.requestID}</small> : null}
      <button type="button" className="secondary-button" onClick={onRetry}>重试</button>
    </section>
  );
}

function mergeGovernanceItems(current, incoming) {
  const skillIDs = new Set(current.map((item) => item.skillID));
  const uniqueIncoming = incoming.filter((item) => {
    if (skillIDs.has(item.skillID)) return false;
    skillIDs.add(item.skillID);
    return true;
  });
  return [...current, ...uniqueIncoming];
}

function errorKind(error) {
  return isCapabilityDenied(error) ? 'forbidden' : 'error';
}

function isCapabilityDenied(error) {
  return Number(error?.status) === 403
    && String(error?.code || '') === SKILL_GOVERNANCE_CAPABILITY_REQUIRED_CODE;
}

function publicError(error, fallback) {
  return {
    message: String(error?.message || fallback),
    code: String(error?.code || 'SKILL_GOVERNANCE_QUEUE_UNAVAILABLE'),
    status: Number(error?.status) || 0,
    requestID: String(error?.requestID || '')
  };
}

function formatTime(value) {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
}

function navigate(path) {
  window.history.pushState({}, '', path);
  window.dispatchEvent(new Event('dora:navigate'));
}
