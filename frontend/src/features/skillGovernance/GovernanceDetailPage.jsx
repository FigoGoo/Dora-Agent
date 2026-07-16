import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft, RefreshCw, ShieldCheck } from 'lucide-react';
import {
  SKILL_GOVERNANCE_CAPABILITY,
  SKILL_GOVERNANCE_QUEUE_ROUTE
} from '../../app/routes.js';
import { useAuthSession } from '../../platform/auth/authSession.js';
import { SkillDefinitionReview } from '../skillReviews/SkillDefinitionReview.jsx';
import {
  createSkillCommandLedger,
  useOptionalSkillCommandLedger
} from '../skills/skillCommandLedger.jsx';
import {
  createGovernanceDecisionKey,
  decideGovernanceSkill,
  getGovernanceSkill
} from './governanceApi.js';
import { SKILL_GOVERNANCE_CAPABILITY_REQUIRED_CODE } from './governanceContract.js';

const DEFAULT_CLIENT = Object.freeze({
  get: getGovernanceSkill,
  decide: decideGovernanceSkill,
  createKey: createGovernanceDecisionKey
});
const STATUS_LABELS = Object.freeze({
  active: '正常',
  suspended: '已暂停',
  offline: '已永久下架'
});
const ACTION_LABELS = Object.freeze({
  suspend: '暂停',
  resume: '恢复',
  offline: '永久下架'
});
const REASON_CODES_BY_ACTION = Object.freeze({
  suspend: Object.freeze([
    'content_safety',
    'copyright_risk',
    'privacy_risk',
    'fraud_or_abuse',
    'tool_dependency_risk',
    'policy_violation',
    'incident_containment'
  ]),
  resume: Object.freeze([
    'risk_cleared',
    'appeal_approved',
    'incident_resolved',
    'dependency_restored',
    'policy_remediated'
  ]),
  offline: Object.freeze([
    'content_safety',
    'copyright_risk',
    'privacy_risk',
    'fraud_or_abuse',
    'tool_dependency_risk',
    'policy_violation',
    'owner_request',
    'repeated_violation'
  ])
});
const REASON_LABELS = Object.freeze({
  content_safety: '内容安全',
  copyright_risk: '版权风险',
  privacy_risk: '隐私风险',
  fraud_or_abuse: '欺诈或滥用',
  tool_dependency_risk: 'Tool 依赖风险',
  policy_violation: '政策违规',
  incident_containment: '事件止损',
  risk_cleared: '风险已解除',
  appeal_approved: '申诉已批准',
  incident_resolved: '事件已解决',
  dependency_restored: '依赖已恢复',
  policy_remediated: '政策问题已整改',
  owner_request: 'Owner 请求',
  repeated_violation: '重复违规'
});
const APPROVAL_REFERENCE_PATTERN = /^[A-Z][A-Z0-9_]{1,31}-[A-Za-z0-9][A-Za-z0-9._-]{0,126}$/;

export function GovernanceDetailPage({
  skillID,
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
  const scope = decisionScope(skillID);
  const [state, setState] = useState({ kind: 'loading', skill: null, error: null });
  const [decision, setDecision] = useState({ kind: 'idle', error: null });
  const [form, setForm] = useState({ action: '', reasonCode: '', approvalReference: '' });
  const loadGenerationRef = useRef(0);
  const decisionGenerationRef = useRef(0);
  const loadControllerRef = useRef(null);
  const decisionControllerRef = useRef(null);
  const capabilityRefreshRef = useRef(false);

  const load = useCallback(async ({ preserveContent = false, preserveDecision = false } = {}) => {
    loadControllerRef.current?.abort();
    const controller = new AbortController();
    loadControllerRef.current = controller;
    const generation = ++loadGenerationRef.current;
    setState((current) => ({
      kind: preserveContent && current.skill ? 'refreshing' : 'loading',
      skill: preserveContent ? current.skill : null,
      error: null
    }));
    try {
      const result = await client.get(skillID, { signal: controller.signal });
      if (generation !== loadGenerationRef.current || controller.signal.aborted) return;
      assertSkillIdentity(result, skillID);
      setState({ kind: 'ready', skill: result.skill, error: null });
      setForm({ action: '', reasonCode: '', approvalReference: '' });
      if (!preserveDecision) {
        const pending = commandLedger.get(scope);
        setDecision(pending
          ? {
              kind: 'pending',
              error: publicError({
                code: 'SKILL_GOVERNANCE_OUTCOME_UNKNOWN',
                message: '存在结果未知的治理命令，只能使用原请求重试。'
              })
            }
          : { kind: 'idle', error: null });
      }
    } catch (error) {
      if (generation !== loadGenerationRef.current || controller.signal.aborted || error?.name === 'AbortError') return;
      const kind = loadErrorKind(error);
      setState({ kind, skill: null, error: publicError(error, '暂时无法加载 Skill 治理详情。') });
      if (kind === 'forbidden') {
        decisionControllerRef.current?.abort();
        commandLedger.clear(scope);
        setDecision({ kind: 'forbidden', error: publicError(error, '当前会话已失去 Skill 治理权限。') });
        requestCapabilityRefresh(retryBootstrap, capabilityRefreshRef);
      }
    } finally {
      if (loadControllerRef.current === controller) loadControllerRef.current = null;
    }
  }, [client, commandLedger, retryBootstrap, scope, skillID]);

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

  function selectAction(action) {
    if (!state.skill?.allowedActions.includes(action) || decision.kind === 'submitting') return;
    setForm({ action, reasonCode: '', approvalReference: '' });
    if (!['unknown', 'pending', 'stale_command', 'idempotency_conflict'].includes(decision.kind)) {
      setDecision({ kind: 'idle', error: null });
    }
  }

  function submitDecision(event) {
    event.preventDefault();
    const skill = state.skill;
    if (!skill || !skill.allowedActions.includes(form.action) || decision.kind === 'submitting') return;
    if (!REASON_CODES_BY_ACTION[form.action]?.includes(form.reasonCode)
      || !APPROVAL_REFERENCE_PATTERN.test(form.approvalReference)) {
      setDecision({
        kind: 'failed',
        error: publicError({ message: '请选择冻结原因闭集并填写有效的外部审批引用。' })
      });
      return;
    }

    const semantic = decisionSemantic({
      skillID,
      action: form.action,
      reasonCode: form.reasonCode,
      approvalReference: form.approvalReference,
      governanceETag: skill.governanceETag
    });
    let command = commandLedger.get(scope);
    if (command && command.semantic !== semantic) {
      setDecision({
        kind: 'stale_command',
        error: publicError({
          code: 'SKILL_GOVERNANCE_OUTCOME_UNKNOWN',
          message: '上一次治理命令绑定了其他 action、原因、审批引用或 ETag，只能重试原命令。'
        })
      });
      return;
    }
    if (!command) {
      command = commandLedger.set(scope, {
        key: client.createKey(),
        semantic,
        skillID,
        action: form.action,
        reasonCode: form.reasonCode,
        approvalReference: form.approvalReference,
        governanceETag: skill.governanceETag
      });
    }
    void executeDecision(command);
  }

  function retryPendingDecision() {
    const command = commandLedger.get(scope);
    if (!command || decision.kind === 'submitting') return;
    void executeDecision(command);
  }

  async function executeDecision(command) {
    decisionControllerRef.current?.abort();
    const controller = new AbortController();
    decisionControllerRef.current = controller;
    const generation = ++decisionGenerationRef.current;
    setDecision({ kind: 'submitting', error: null });
    try {
      const result = await client.decide({
        skillID: command.skillID,
        action: command.action,
        reasonCode: command.reasonCode,
        approvalReference: command.approvalReference,
        idempotencyKey: command.key,
        governanceETag: command.governanceETag,
        csrfToken,
        signal: controller.signal
      });
      assertSkillIdentity(result, skillID);
      commandLedger.clear(scope, command.key);
      if (generation !== decisionGenerationRef.current || controller.signal.aborted) return;
      setState((current) => ({
        kind: 'ready',
        error: null,
        skill: {
          ...current.skill,
          ...result.skill
        }
      }));
      setForm({ action: '', reasonCode: '', approvalReference: '' });
      setDecision({ kind: 'succeeded', error: null });
    } catch (error) {
      if (generation !== decisionGenerationRef.current || controller.signal.aborted || error?.name === 'AbortError') return;
      const status = Number(error?.status) || 0;
      const code = String(error?.code || '');
      if (isCapabilityDenied(error)) {
        commandLedger.clear(scope, command.key);
        const failure = publicError(error, '当前会话已失去 Skill 治理权限。');
        loadGenerationRef.current += 1;
        loadControllerRef.current?.abort();
        setState({ kind: 'forbidden', skill: null, error: failure });
        setDecision({ kind: 'forbidden', error: failure });
        requestCapabilityRefresh(retryBootstrap, capabilityRefreshRef);
      } else if (status === 409 && code === 'SKILL_GOVERNANCE_CONFLICT') {
        commandLedger.clear(scope, command.key);
        setDecision({
          kind: 'conflict',
          error: publicError(error, '治理状态已发生变化，正在刷新权威详情。')
        });
        void load({ preserveContent: true });
      } else if (status === 409 && code === 'IDEMPOTENCY_CONFLICT') {
        setDecision({
          kind: 'idempotency_conflict',
          error: publicError(error, '幂等键已绑定其他语义，不能自动换 Key。')
        });
      } else if (status === 401 || status === 404 || isDefinitiveRejection(error)) {
        commandLedger.clear(scope, command.key);
        setDecision({ kind: 'failed', error: publicError(error, '治理命令未被接受。') });
        if (status === 404) {
          setState({ kind: 'not_found', skill: null, error: publicError(error, '该 Skill 治理对象不存在。') });
        }
      } else {
        setDecision({
          kind: 'unknown',
          error: publicError(error, '治理结果暂时未知，只能使用原请求重试。')
        });
      }
    } finally {
      if (decisionControllerRef.current === controller) decisionControllerRef.current = null;
    }
  }

  if (state.kind === 'loading') {
    return <section className="skill-review-detail"><p role="status">正在加载 Skill 治理详情…</p></section>;
  }
  if (!state.skill || !['ready', 'refreshing'].includes(state.kind)) {
    return (
      <section className="skill-review-detail">
        <button type="button" className="ghost-link" onClick={() => onNavigate(SKILL_GOVERNANCE_QUEUE_ROUTE)}>
          <ArrowLeft aria-hidden="true" size={16} />返回治理队列
        </button>
        <section className="skill-state-panel">
          <h2>{state.kind === 'not_found'
            ? 'Skill 治理对象不存在'
            : state.kind === 'forbidden'
              ? '无 Skill 治理权限'
              : '治理详情暂不可用'}</h2>
          <p role="alert">{state.error?.message}</p>
          {state.kind === 'error'
            ? <button type="button" className="secondary-button" onClick={() => load()}>重试</button>
            : null}
        </section>
      </section>
    );
  }

  const skill = state.skill;
  const actionLocked = ['submitting', 'unknown', 'pending', 'stale_command', 'idempotency_conflict'].includes(decision.kind);
  const canSubmit = skill.allowedActions.includes(form.action)
    && REASON_CODES_BY_ACTION[form.action]?.includes(form.reasonCode)
    && APPROVAL_REFERENCE_PATTERN.test(form.approvalReference)
    && !actionLocked;
  return (
    <section className="skill-review-detail" aria-labelledby="skill-governance-detail-heading">
      <header className="skill-review-page__header">
        <div>
          <button type="button" className="ghost-link" onClick={() => onNavigate(SKILL_GOVERNANCE_QUEUE_ROUTE)}>
            <ArrowLeft aria-hidden="true" size={16} />返回治理队列
          </button>
          <h2 id="skill-governance-detail-heading">{skill.definition.name}</h2>
          <p>当前发布内容只读；治理处置不会修改或替换不可变发布快照。</p>
        </div>
        <span className={`skill-review-status is-${skill.governanceStatus}`}>
          {STATUS_LABELS[skill.governanceStatus]}
        </span>
      </header>

      <dl className="skill-review-detail__meta">
        <div><dt>Skill ID</dt><dd>{skill.skillID}</dd></div>
        <div><dt>发布时间</dt><dd>{formatTime(skill.publishedAt)}</dd></div>
        <div><dt>治理状态</dt><dd>{STATUS_LABELS[skill.governanceStatus]}</dd></div>
        <div><dt>治理纪元</dt><dd>{skill.governanceEpoch}</dd></div>
      </dl>

      {state.kind === 'refreshing' ? <p role="status">正在刷新权威治理详情…</p> : null}
      {decision.kind === 'succeeded' ? <p role="status">治理处置已完成，页面已更新为权威结果。</p> : null}
      {decision.error ? (
        <section className="skill-review-decision-feedback">
          <p role="alert">{decision.error.message}</p>
          {decision.error.requestID ? <small>请求 ID：{decision.error.requestID}</small> : null}
          {['unknown', 'pending', 'stale_command'].includes(decision.kind) ? (
            <button type="button" className="secondary-button" onClick={retryPendingDecision}>
              使用原命令重试
            </button>
          ) : null}
          {decision.kind === 'conflict' && state.kind === 'refreshing' ? (
            <span><RefreshCw aria-hidden="true" size={15} />正在废弃旧命令并刷新…</span>
          ) : null}
        </section>
      ) : null}

      <section className="skill-review-comparison" aria-label="当前发布 Skill Definition">
        <div className="skill-review-comparison__summary">
          <ShieldCheck aria-hidden="true" size={20} />
          <strong>当前发布 SkillDefinitionV1</strong>
          <span>以下内容来自 current published snapshot，仅供治理判断。</span>
        </div>
        <SkillDefinitionReview definition={skill.definition} title="当前发布内容" />
      </section>

      {skill.allowedActions.length === 0 ? (
        <section className="skill-state-panel">
          <h3>该 Skill 已永久下架</h3>
          <p>offline 是终态，没有恢复或其他治理动作。</p>
        </section>
      ) : (
        <section aria-label="治理动作">
          <div className="skill-review-detail__actions">
            {skill.allowedActions.map((action) => (
              <button
                type="button"
                className="secondary-button"
                aria-pressed={form.action === action}
                disabled={actionLocked}
                key={action}
                onClick={() => selectAction(action)}
              >
                {ACTION_LABELS[action]}
              </button>
            ))}
          </div>

          {form.action ? (
            <form className="skill-review-definition" aria-label={`${ACTION_LABELS[form.action]}治理表单`} onSubmit={submitDecision}>
              <label>
                原因代码
                <select
                  aria-label="原因代码"
                  required
                  disabled={actionLocked}
                  value={form.reasonCode}
                  onChange={(event) => setForm((current) => ({ ...current, reasonCode: event.target.value }))}
                >
                  <option value="">请选择原因</option>
                  {REASON_CODES_BY_ACTION[form.action].map((reasonCode) => (
                    <option value={reasonCode} key={reasonCode}>
                      {REASON_LABELS[reasonCode]}（{reasonCode}）
                    </option>
                  ))}
                </select>
              </label>
              <label>
                外部审批引用
                <input
                  aria-label="外部审批引用"
                  required
                  maxLength={160}
                  pattern="[A-Z][A-Z0-9_]{1,31}-[A-Za-z0-9][A-Za-z0-9._-]{0,126}"
                  disabled={actionLocked}
                  value={form.approvalReference}
                  onChange={(event) => setForm((current) => ({ ...current, approvalReference: event.target.value }))}
                />
              </label>
              <button type="submit" className="start-button" disabled={!canSubmit}>
                {decision.kind === 'submitting' ? '正在提交治理处置…' : `提交${ACTION_LABELS[form.action]}处置`}
              </button>
            </form>
          ) : null}
        </section>
      )}
    </section>
  );
}

function decisionScope(skillID) {
  return `skill-governance-decision:${skillID}`;
}

function decisionSemantic({ skillID, action, reasonCode, approvalReference, governanceETag }) {
  return [skillID, action, reasonCode, approvalReference, governanceETag].join('\u0000');
}

function loadErrorKind(error) {
  if (isCapabilityDenied(error)) return 'forbidden';
  if (Number(error?.status) === 404) return 'not_found';
  return 'error';
}

function isCapabilityDenied(error) {
  return Number(error?.status) === 403
    && String(error?.code || '') === SKILL_GOVERNANCE_CAPABILITY_REQUIRED_CODE;
}

function isDefinitiveRejection(error) {
  const status = Number(error?.status) || 0;
  return status >= 400 && status < 500 && ![408, 409, 425, 429].includes(status);
}

function assertSkillIdentity(result, skillID) {
  if (result?.skill?.skillID !== skillID) {
    const error = new Error('治理响应资源身份与当前 Skill 不一致');
    error.code = 'INVALID_SKILL_GOVERNANCE_RESPONSE';
    error.status = 502;
    throw error;
  }
}

function requestCapabilityRefresh(retryBootstrap, capabilityRefreshRef) {
  if (capabilityRefreshRef.current) return;
  capabilityRefreshRef.current = true;
  retryBootstrap({ deniedCapability: SKILL_GOVERNANCE_CAPABILITY }).catch(() => {});
}

function publicError(error, fallback = 'Skill 治理请求暂时失败。') {
  return {
    message: String(error?.message || fallback),
    code: String(error?.code || 'SKILL_GOVERNANCE_REQUEST_FAILED'),
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
