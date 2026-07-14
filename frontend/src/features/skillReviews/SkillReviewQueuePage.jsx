import { useCallback, useEffect, useRef, useState } from 'react';
import { RefreshCw, ShieldCheck } from 'lucide-react';
import { getSkillReviewDetailPath, SKILL_REVIEW_CAPABILITY } from '../../app/routes.js';
import { useAuthSession } from '../../platform/auth/authSession.js';
import { listSkillReviews } from './skillReviewApi.js';
import { SKILL_REVIEW_CAPABILITY_REQUIRED_CODE } from './skillReviewContract.js';

export function SkillReviewQueuePage({ loadReviews = listSkillReviews, onNavigate = navigate }) {
  const auth = useAuthSession();
  const retryBootstrap = auth.retryBootstrap;
  const [state, setState] = useState({ kind: 'loading', items: [], nextCursor: null, error: null });
  const generationRef = useRef(0);
  const requestRef = useRef(null);
  const capabilityRefreshRef = useRef(false);
  const itemsRef = useRef([]);

  const load = useCallback(async ({ cursor = null, append = false } = {}) => {
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    const generation = ++generationRef.current;
    setState((current) => ({ ...current, kind: append ? 'loading_more' : 'loading', error: null }));
    try {
      const result = await loadReviews({ cursor, signal: controller.signal });
      if (generation !== generationRef.current || controller.signal.aborted) return;
      const items = append ? mergeReviewItems(itemsRef.current, result.items) : result.items;
      itemsRef.current = items;
      setState({
        kind: 'ready',
        items,
        nextCursor: result.nextCursor,
        error: null
      });
    } catch (error) {
      if (generation !== generationRef.current || controller.signal.aborted || error?.name === 'AbortError') return;
      const publicFailure = publicError(error);
      const kind = deniedKind(error);
      if (kind === 'forbidden' || kind === 'not_found') itemsRef.current = [];
      setState((current) => ({
        ...current,
        kind,
        items: kind === 'forbidden' || kind === 'not_found' ? [] : current.items,
        nextCursor: kind === 'forbidden' || kind === 'not_found' ? null : current.nextCursor,
        error: publicFailure
      }));
      if (isCapabilityDenied(error) && !capabilityRefreshRef.current) {
        capabilityRefreshRef.current = true;
        retryBootstrap({ deniedCapability: SKILL_REVIEW_CAPABILITY }).catch(() => {});
      }
    } finally {
      if (requestRef.current === controller) requestRef.current = null;
    }
  }, [loadReviews, retryBootstrap]);

  useEffect(() => {
    void load();
    return () => {
      generationRef.current += 1;
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, [load]);

  return (
    <section className="skill-review-queue" aria-labelledby="skill-review-queue-heading">
      <header className="skill-review-page__header">
        <div>
          <h2 id="skill-review-queue-heading">待审核 Skill</h2>
          <p>只展示最早提交优先的冻结审核内容；批准后会从队列移除。</p>
        </div>
        <button type="button" className="secondary-button" disabled={state.kind === 'loading'} onClick={() => load()}>
          <RefreshCw aria-hidden="true" size={16} />
          刷新队列
        </button>
      </header>

      {state.kind === 'loading' ? <p role="status">正在加载待审核 Skill…</p> : null}
      {state.kind === 'error' ? <ReviewQueueError error={state.error} onRetry={() => load()} /> : null}
      {state.kind === 'forbidden' ? <p role="alert">当前会话已失去 Skill 审核权限，正在重新确认权限。</p> : null}
      {state.kind === 'not_found' ? <p role="alert">审核队列不可访问。</p> : null}
      {state.kind === 'ready' && state.items.length === 0 ? (
        <section className="skill-state-panel">
          <ShieldCheck aria-hidden="true" size={42} />
          <h3>当前没有待审核 Skill</h3>
          <p>新的提交会按最早提交时间出现在这里。</p>
        </section>
      ) : null}
      {state.items.length > 0 ? (
        <div className="skill-review-queue__list">
          {state.items.map((review) => (
            <article key={review.reviewID} className="skill-review-card">
              <div>
                <span>{review.category || '未分类'}</span>
                <h3>{review.name}</h3>
                <p>{review.summary || '未填写简介'}</p>
                <small>提交于 {formatTime(review.submittedAt)}</small>
              </div>
              <button type="button" className="secondary-button" onClick={() => onNavigate(getSkillReviewDetailPath(review.reviewID))}>
                查看冻结详情
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

function ReviewQueueError({ error, onRetry }) {
  return (
    <section className="skill-state-panel">
      <p role="alert">{error.message}</p>
      {error.requestID ? <small>请求 ID：{error.requestID}</small> : null}
      <button type="button" className="secondary-button" onClick={onRetry}>重试</button>
    </section>
  );
}

function mergeReviewItems(current, incoming) {
  const reviewIDs = new Set(current.map((item) => item.reviewID));
  const skillIDs = new Set(current.map((item) => item.skillID));
  if (incoming.some((item) => reviewIDs.has(item.reviewID) || skillIDs.has(item.skillID))) {
    const error = new Error('审核队列分页返回了重复项目');
    error.code = 'INVALID_SKILL_REVIEW_RESPONSE';
    error.status = 502;
    throw error;
  }
  return [...current, ...incoming];
}

function deniedKind(error) {
  if (isCapabilityDenied(error)) return 'forbidden';
  if (Number(error?.status) === 404) return 'not_found';
  return 'error';
}

function isCapabilityDenied(error) {
  return Number(error?.status) === 403
    && String(error?.code || '') === SKILL_REVIEW_CAPABILITY_REQUIRED_CODE;
}

function publicError(error) {
  return {
    message: String(error?.message || '暂时无法加载审核队列。'),
    code: String(error?.code || 'SKILL_REVIEW_QUEUE_UNAVAILABLE'),
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
