import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft, RefreshCw, ShieldCheck } from 'lucide-react';
import { SKILL_REVIEW_CAPABILITY, SKILL_REVIEW_QUEUE_ROUTE } from '../../app/routes.js';
import { useAuthSession } from '../../platform/auth/authSession.js';
import {
  createSkillCommandLedger,
  useOptionalSkillCommandLedger
} from '../skills/skillCommandLedger.jsx';
import {
  approveSkillReview,
  createSkillReviewDecisionKey,
  getSkillReview
} from './skillReviewApi.js';
import {
  SKILL_REVIEW_ACTION_APPROVE,
  SKILL_REVIEW_CAPABILITY_REQUIRED_CODE
} from './skillReviewContract.js';
import { SkillDefinitionReview } from './SkillDefinitionReview.jsx';

const DEFAULT_CLIENT = Object.freeze({
  get: getSkillReview,
  approve: approveSkillReview,
  createKey: createSkillReviewDecisionKey
});
const STATUS_LABELS = Object.freeze({
  reviewing: '审核中', approved: '已批准并发布', rejected: '已驳回', withdrawn: '已撤回'
});

export function SkillReviewDetailPage({
  reviewID,
  csrfToken,
  client = DEFAULT_CLIENT,
  commandLedger: injectedCommandLedger,
  onNavigate = navigate
}) {
  const auth = useAuthSession();
  const retryBootstrap = auth.retryBootstrap;
  const inheritedCommandLedger = useOptionalSkillCommandLedger();
  const localCommandLedgerRef = useRef(null);
  if (!localCommandLedgerRef.current) localCommandLedgerRef.current = createSkillCommandLedger();
  const commandLedger = injectedCommandLedger || inheritedCommandLedger || localCommandLedgerRef.current;
  const scope = decisionScope(reviewID);
  const [state, setState] = useState({ kind: 'loading', review: null, error: null });
  const [decision, setDecision] = useState({ kind: 'idle', error: null });
  const loadGenerationRef = useRef(0);
  const decisionGenerationRef = useRef(0);
  const loadControllerRef = useRef(null);
  const decisionControllerRef = useRef(null);
  const capabilityRefreshRef = useRef(false);

  const load = useCallback(async () => {
    loadControllerRef.current?.abort();
    const controller = new AbortController();
    loadControllerRef.current = controller;
    const generation = ++loadGenerationRef.current;
    setState((current) => ({ ...current, kind: 'loading', error: null }));
    try {
      const result = await client.get(reviewID, { signal: controller.signal });
      if (generation !== loadGenerationRef.current || controller.signal.aborted) return;
      if (result.review.status !== 'reviewing') commandLedger.clear(scope);
      setState({ kind: 'ready', review: result.review, error: null });
      setDecision({ kind: 'idle', error: null });
    } catch (error) {
      if (generation !== loadGenerationRef.current || controller.signal.aborted || error?.name === 'AbortError') return;
      setState((current) => ({ ...current, kind: loadErrorKind(error), error: publicError(error, '暂时无法加载审核详情。') }));
      if (isCapabilityDenied(error) && !capabilityRefreshRef.current) {
        capabilityRefreshRef.current = true;
        decisionControllerRef.current?.abort();
        commandLedger.clear(scope);
        retryBootstrap({ deniedCapability: SKILL_REVIEW_CAPABILITY }).catch(() => {});
      }
    } finally {
      if (loadControllerRef.current === controller) loadControllerRef.current = null;
    }
  }, [client, commandLedger, retryBootstrap, reviewID, scope]);

  useEffect(() => {
    void load();
    return () => {
      loadGenerationRef.current += 1;
      decisionGenerationRef.current += 1;
      loadControllerRef.current?.abort();
      decisionControllerRef.current?.abort();
      loadControllerRef.current = null;
      decisionControllerRef.current = null;
    };
  }, [load]);

  async function approve() {
    const review = state.review;
    if (
      !review
      || review.status !== 'reviewing'
      || !review.allowedActions.includes(SKILL_REVIEW_ACTION_APPROVE)
      || decision.kind === 'submitting'
    ) return;
    const semantic = decisionSemantic(review);
    let command = commandLedger.get(scope);
    if (command && command.semantic !== semantic) {
      setDecision({
        kind: 'stale_command',
        error: publicError({
          code: 'SKILL_REVIEW_OUTCOME_UNKNOWN',
          message: '上一次审核决定仍绑定旧 ETag。请刷新权威详情后再创建新的决定意图。'
        })
      });
      return;
    }
    if (!command) {
      command = commandLedger.set(scope, {
        key: client.createKey(),
        semantic,
        reviewETag: review.reviewETag,
        decision: 'approved'
      });
    }
    decisionControllerRef.current?.abort();
    const controller = new AbortController();
    decisionControllerRef.current = controller;
    const generation = ++decisionGenerationRef.current;
    setDecision({ kind: 'submitting', error: null });
    try {
      const result = await client.approve({
        reviewID,
        idempotencyKey: command.key,
        reviewETag: command.reviewETag,
        csrfToken,
        signal: controller.signal
      });
      assertDecisionIdentity(result, review);
      commandLedger.clear(scope, command.key);
      if (generation !== decisionGenerationRef.current || controller.signal.aborted) return;
      setState((current) => ({
        kind: 'ready',
        error: null,
        review: {
          ...current.review,
          status: 'approved',
          updatedAt: result.review.decidedAt,
          currentPublished: {
            publishedSnapshotID: result.review.publishedSnapshotID,
            publishedAt: result.review.decidedAt,
            definition: current.review.definition
          },
          comparison: { hasCurrentPublished: true, sameContent: true },
          allowedActions: []
        }
      }));
      setDecision({ kind: 'succeeded', error: null });
    } catch (error) {
      if (generation !== decisionGenerationRef.current || controller.signal.aborted || error?.name === 'AbortError') return;
      const status = Number(error?.status) || 0;
      const code = String(error?.code || '');
      if (isCapabilityDenied(error)) {
        commandLedger.clear(scope, command.key);
        const failure = publicError(error, '当前会话已失去 Skill 审核权限。');
        loadGenerationRef.current += 1;
        loadControllerRef.current?.abort();
        setState({ kind: 'forbidden', review: null, error: failure });
        setDecision({ kind: 'forbidden', error: failure });
        if (!capabilityRefreshRef.current) {
          capabilityRefreshRef.current = true;
          retryBootstrap({ deniedCapability: SKILL_REVIEW_CAPABILITY }).catch(() => {});
        }
      } else if (status === 401 || status === 404 || isDefinitiveRejection(error)) {
        commandLedger.clear(scope, command.key);
        setDecision({ kind: 'failed', error: publicError(error, '审核决定未被接受。') });
      } else if (status === 409 && code === 'SKILL_REVIEW_CONFLICT') {
        setDecision({ kind: 'conflict', error: publicError(error, '审核状态已发生变化，请刷新权威详情。') });
      } else if (status === 409 && code === 'IDEMPOTENCY_CONFLICT') {
        setDecision({ kind: 'idempotency_conflict', error: publicError(error, '幂等键已绑定其他语义，不能自动换 Key。') });
      } else {
        setDecision({ kind: 'unknown', error: publicError(error, '审核结果暂时未知，请使用原请求重试。') });
      }
    } finally {
      if (decisionControllerRef.current === controller) decisionControllerRef.current = null;
    }
  }

  function refreshAfterConflict() {
    const command = commandLedger.get(scope);
    commandLedger.clear(scope, command?.key);
    setDecision({ kind: 'idle', error: null });
    void load();
  }

  if (state.kind === 'loading') {
    return <section className="skill-review-detail"><p role="status">正在加载冻结审核详情…</p></section>;
  }
  if (state.kind !== 'ready' || !state.review) {
    return (
      <section className="skill-review-detail">
        <button type="button" className="ghost-link" onClick={() => onNavigate(SKILL_REVIEW_QUEUE_ROUTE)}>
          <ArrowLeft aria-hidden="true" size={16} />返回审核队列
        </button>
        <section className="skill-state-panel">
          <h2>{state.kind === 'not_found' ? '审核记录不存在' : state.kind === 'forbidden' ? '无 Skill 审核权限' : '审核详情暂不可用'}</h2>
          <p role="alert">{state.error?.message}</p>
          {state.kind === 'error' ? <button type="button" className="secondary-button" onClick={() => load()}>重试</button> : null}
        </section>
      </section>
    );
  }

  const review = state.review;
  const canApprove = review.status === 'reviewing'
    && review.allowedActions.includes(SKILL_REVIEW_ACTION_APPROVE)
    && decision.kind === 'idle';
  return (
    <section className="skill-review-detail" aria-labelledby="skill-review-detail-heading">
      <header className="skill-review-page__header">
        <div>
          <button type="button" className="ghost-link" onClick={() => onNavigate(SKILL_REVIEW_QUEUE_ROUTE)}>
            <ArrowLeft aria-hidden="true" size={16} />返回审核队列
          </button>
          <h2 id="skill-review-detail-heading">{review.definition.name}</h2>
          <p>审核内容冻结于 {formatTime(review.submittedAt)}，不会跟随 Owner 后续草稿变化。</p>
        </div>
        <span className={`skill-review-status is-${review.status}`}>{STATUS_LABELS[review.status]}</span>
      </header>

      <dl className="skill-review-detail__meta">
        <div><dt>Review ID</dt><dd>{review.reviewID}</dd></div>
        <div><dt>Skill ID</dt><dd>{review.skillID}</dd></div>
        <div><dt>Owner ID</dt><dd>{review.ownerUserID}</dd></div>
        <div><dt>最近更新</dt><dd>{formatTime(review.updatedAt)}</dd></div>
      </dl>

      {decision.kind === 'succeeded' ? <p role="status">审核已批准，冻结内容已原子发布。</p> : null}
      {decision.error ? (
        <section className="skill-review-decision-feedback">
          <p role="alert">{decision.error.message}</p>
          {decision.error.requestID ? <small>请求 ID：{decision.error.requestID}</small> : null}
          {decision.kind === 'unknown' ? (
            <button type="button" className="secondary-button" onClick={approve}>使用原请求重试</button>
          ) : null}
          {decision.kind === 'conflict' || decision.kind === 'stale_command' ? (
            <button type="button" className="secondary-button" onClick={refreshAfterConflict}>
              <RefreshCw aria-hidden="true" size={15} />刷新权威详情并废弃旧命令
            </button>
          ) : null}
        </section>
      ) : null}

      <section className="skill-review-comparison" aria-label="审核内容对照">
        <div className="skill-review-comparison__summary">
          <ShieldCheck aria-hidden="true" size={20} />
          <strong>{review.comparison.hasCurrentPublished ? '存在当前发布内容' : '这是首次发布'}</strong>
          <span>{review.comparison.sameContent ? '当前发布内容与本次提交相同' : '本次提交与当前发布内容不同'}</span>
        </div>
        <SkillDefinitionReview definition={review.definition} title="本次冻结提交" />
        {review.currentPublished ? (
          <SkillDefinitionReview definition={review.currentPublished.definition} title="当前发布内容" />
        ) : null}
      </section>

      <footer className="skill-review-detail__actions">
        <button type="button" className="start-button" disabled={!canApprove} onClick={approve}>
          {decision.kind === 'submitting' ? '正在批准并发布…' : '批准并发布'}
        </button>
      </footer>
    </section>
  );
}

function decisionScope(reviewID) {
  return `skill-review-decision:${reviewID}`;
}

function decisionSemantic(review) {
  return `${review.reviewID}\u0000${review.reviewETag}\u0000approved`;
}

function loadErrorKind(error) {
  if (isCapabilityDenied(error)) return 'forbidden';
  if (Number(error?.status) === 404) return 'not_found';
  return 'error';
}

function isCapabilityDenied(error) {
  return Number(error?.status) === 403
    && String(error?.code || '') === SKILL_REVIEW_CAPABILITY_REQUIRED_CODE;
}

function isDefinitiveRejection(error) {
  const status = Number(error?.status) || 0;
  return status >= 400 && status < 500 && ![408, 409, 425, 429].includes(status);
}

function assertDecisionIdentity(result, review) {
  if (
    result?.review?.reviewID !== review.reviewID
    || result?.review?.skillID !== review.skillID
  ) {
    const error = new Error('审核决定响应资源身份与当前冻结详情不一致');
    error.code = 'INVALID_SKILL_REVIEW_RESPONSE';
    error.status = 502;
    throw error;
  }
}

function publicError(error, fallback = 'Skill 审核请求暂时失败。') {
  return {
    message: String(error?.message || fallback),
    code: String(error?.code || 'SKILL_REVIEW_REQUEST_FAILED'),
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
