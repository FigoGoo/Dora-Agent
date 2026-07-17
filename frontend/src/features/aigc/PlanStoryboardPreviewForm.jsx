import { useEffect, useRef, useState } from 'react';
import { enqueuePlanStoryboardPreview } from './planStoryboardPreviewApi.js';
import { createPlanStoryboardPreviewIntentLedger } from './planStoryboardPreviewIntent.js';

const INITIAL_FORM = Object.freeze({
  planningInstruction: '',
  targetDurationSeconds: ''
});
const INITIAL_SUBMISSION = Object.freeze({
  kind: 'idle',
  inputID: '',
  requestID: '',
  error: null
});

// PlanStoryboardPreviewForm 只允许用户填写规划语义；CreationSpec 引用始终来自已验证的当前 Card。
export function PlanStoryboardPreviewForm({
  sessionID,
  csrfToken,
  creationSpec,
  failure,
  initialPlanningInstruction = '',
  enqueue = enqueuePlanStoryboardPreview,
  keyFactory
}) {
  const [form, setForm] = useState(() => ({
    ...INITIAL_FORM,
    planningInstruction: initialPlanningInstruction == null ? '' : String(initialPlanningInstruction)
  }));
  const [submission, setSubmission] = useState(INITIAL_SUBMISSION);
  const ledgerOwnerRef = useRef(null);
  const acceptedReceiptsRef = useRef(new Map());
  const requestRef = useRef(null);
  const requestGenerationRef = useRef(0);
  const creationSpecRef = creationSpecRefFromCard(creationSpec);
  const resourceIdentity = creationSpecRef
    ? `${creationSpecRef.id}:${creationSpecRef.version}:${creationSpecRef.contentDigest}`
    : '';
  const ledgerOwner = `${sessionID || ''}:${resourceIdentity}`;

  if (!ledgerOwnerRef.current || ledgerOwnerRef.current.owner !== ledgerOwner) {
    acceptedReceiptsRef.current.clear();
    ledgerOwnerRef.current = {
      owner: ledgerOwner,
      ledger: createPlanStoryboardPreviewIntentLedger(keyFactory ? { keyFactory } : undefined)
    };
  }

  useEffect(() => {
    requestGenerationRef.current += 1;
    requestRef.current?.abort();
    requestRef.current = null;
    acceptedReceiptsRef.current.clear();
    setSubmission(INITIAL_SUBMISSION);
    return () => {
      requestGenerationRef.current += 1;
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, [ledgerOwner]);

  useEffect(() => {
    const initial = initialPlanningInstruction == null ? '' : String(initialPlanningInstruction);
    if (!initial) return;
    setForm((current) => current.planningInstruction === ''
      ? { ...current, planningInstruction: initial }
      : current);
  }, [initialPlanningInstruction]);

  useEffect(() => {
    if (!failure || !submission.inputID || failure.inputID !== submission.inputID) return;
    setSubmission((current) => ({ ...current, kind: 'failure', error: failure }));
  }, [failure, submission.inputID]);

  const update = (field) => (event) => {
    setForm((current) => ({ ...current, [field]: event.target.value }));
    setSubmission((current) => current.kind === 'submitting'
      ? current
      : {
          ...INITIAL_SUBMISSION,
          kind: ['pending', 'editing', 'failure', 'error'].includes(current.kind) ? 'editing' : 'idle'
        });
  };

  const submit = async (event) => {
    event.preventDefault();
    if (submission.kind === 'submitting' || !creationSpecRef) return;
    let command;
    try {
      command = ledgerOwnerRef.current.ledger.claim({
        creationSpecRef,
        toolIntent: {
          planningInstruction: form.planningInstruction,
          targetDurationSeconds: form.targetDurationSeconds
        }
      });
    } catch (error) {
      setSubmission({ ...INITIAL_SUBMISSION, kind: 'error', error: publicPreviewError(error) });
      return;
    }

    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    const generation = ++requestGenerationRef.current;
    setSubmission({ ...INITIAL_SUBMISSION, kind: 'submitting' });
    try {
      const accepted = await enqueue({
        sessionID,
        creationSpecRef: command.creationSpecRef,
        toolIntent: command.toolIntent,
        idempotencyKey: command.key,
        csrfToken,
        signal: controller.signal
      });
      if (generation !== requestGenerationRef.current || controller.signal.aborted) return;
      recordAcceptedReceipt(acceptedReceiptsRef.current, command.key, accepted);
      setSubmission({
        ...INITIAL_SUBMISSION,
        kind: 'pending',
        inputID: accepted.inputID,
        requestID: accepted.requestID
      });
    } catch (error) {
      if (generation !== requestGenerationRef.current || controller.signal.aborted || error?.name === 'AbortError') return;
      setSubmission({ ...INITIAL_SUBMISSION, kind: 'error', error: publicPreviewError(error) });
    } finally {
      if (requestRef.current === controller) requestRef.current = null;
    }
  };

  const disabled = submission.kind === 'submitting' || !creationSpecRef;
  return (
    <form className="plan-storyboard-preview-form" aria-labelledby="plan-storyboard-preview-form-title" onSubmit={submit}>
      <header>
        <h3 id="plan-storyboard-preview-form-title">故事板 JSON Draft</h3>
        <p>
          {creationSpecRef
            ? `基于当前 Creation Spec Draft v${creationSpecRef.version}；提交后以工作台投影为准。`
            : '需要先生成可用的 Creation Spec Draft。'}
        </p>
      </header>

      <label htmlFor="plan-storyboard-planning-instruction">故事板规划要求</label>
      <textarea
        id="plan-storyboard-planning-instruction"
        name="planning_instruction"
        required
        maxLength={1000}
        rows={4}
        disabled={disabled}
        value={form.planningInstruction}
        onChange={update('planningInstruction')}
        placeholder="例如：按开场、核心演示和收尾行动号召规划节奏清晰的故事板"
      />

      <label htmlFor="plan-storyboard-target-duration">目标时长（秒，可选）</label>
      <input
        id="plan-storyboard-target-duration"
        name="target_duration_seconds"
        type="number"
        min="5"
        max="600"
        step="1"
        inputMode="numeric"
        disabled={disabled}
        value={form.targetDurationSeconds}
        onChange={update('targetDurationSeconds')}
      />

      <button type="submit" className="start-button" disabled={disabled}>
        {submission.kind === 'submitting'
          ? '正在提交…'
          : submission.kind === 'pending' ? '再次确认受理状态' : '生成故事板开发预览'}
      </button>
      <SubmissionStatus submission={submission} />
    </form>
  );
}

// 同一 Idempotency-Key 的 pending/replayed 回执必须永久绑定首次预分配身份，避免后续 Card/Failure 错误归因。
function recordAcceptedReceipt(receipts, idempotencyKey, accepted) {
  const fields = ['requestID', 'inputID', 'turnID', 'runID', 'toolCallID'];
  if (!accepted || accepted.status !== 'pending' || typeof accepted.replayed !== 'boolean' ||
      fields.some((field) => typeof accepted[field] !== 'string' || accepted[field] === '')) {
    throw previewReceiptError('Storyboard Preview 返回了无效的 pending 回执');
  }
  const identity = fields.map((field) => accepted[field]).join(':');
  const previous = receipts.get(idempotencyKey);
  if (previous && (!accepted.replayed || previous !== identity)) {
    throw previewReceiptError('Storyboard Preview 同键重放身份不一致');
  }
  if (!previous) receipts.set(idempotencyKey, identity);
}

function previewReceiptError(message) {
  const error = new Error(message);
  error.code = 'PLAN_STORYBOARD_PREVIEW_REPLAY_MISMATCH';
  return error;
}

function SubmissionStatus({ submission }) {
  if (submission.kind === 'submitting') return <p role="status">正在提交 Storyboard Preview typed Input…</p>;
  if (submission.kind === 'pending') {
    return (
      <p role="status">
        请求已受理，正在等待 Storyboard JSON Draft。
        {submission.requestID ? `请求 ID：${submission.requestID}` : ''}
      </p>
    );
  }
  if (submission.kind === 'failure') {
    return (
      <p role="alert">
        故事板规划未完成：{submission.error?.summary || '请稍后重试。'}
        {submission.error?.resultCode ? `（${submission.error.resultCode}）` : ''}
      </p>
    );
  }
  if (submission.kind === 'editing') return <p role="status">规划语义已修改；再次提交会创建新的幂等意图。</p>;
  if (submission.kind === 'error') return <p role="alert">{submission.error?.message || '故事板预览请求失败，请重试。'}</p>;
  return null;
}

function creationSpecRefFromCard(creationSpec) {
  if (!creationSpec || creationSpec.kind !== 'card' || creationSpec.status !== 'draft') return null;
  return {
    id: creationSpec.creationSpecID,
    version: creationSpec.version,
    contentDigest: creationSpec.contentDigest
  };
}

function publicPreviewError(error) {
  return {
    status: Number(error?.status) || 0,
    code: String(error?.code || 'PLAN_STORYBOARD_PREVIEW_FAILED'),
    message: String(error?.message || '故事板预览请求失败，请重试。'),
    requestID: String(error?.requestID || ''),
    retryable: Boolean(error?.retryable)
  };
}
