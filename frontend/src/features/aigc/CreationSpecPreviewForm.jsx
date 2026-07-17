import { useEffect, useRef, useState } from 'react';
import { enqueueCreationSpecPreview } from './creationSpecPreviewApi.js';
import { createCreationSpecPreviewIntentLedger } from './creationSpecPreviewIntent.js';

const INITIAL_FORM = Object.freeze({
  goal: '',
  deliverableType: 'video',
  audience: '',
  locale: 'zh-CN',
  constraintsText: ''
});
const INITIAL_SUBMISSION = Object.freeze({
  kind: 'idle',
  inputID: '',
  requestID: '',
  error: null
});

// CreationSpecPreviewForm 只受理 Preview Intent；权威完成结果必须由 Snapshot/SSE 回投。
export function CreationSpecPreviewForm({
  sessionID,
  csrfToken,
  failure,
  initialGoal = '',
  enqueue = enqueueCreationSpecPreview,
  keyFactory
}) {
  const [form, setForm] = useState(() => ({
    ...INITIAL_FORM,
    goal: initialGoal == null ? '' : String(initialGoal)
  }));
  const [submission, setSubmission] = useState(INITIAL_SUBMISSION);
  const ledgerOwnerRef = useRef(null);
  const requestRef = useRef(null);
  const requestGenerationRef = useRef(0);

  if (!ledgerOwnerRef.current || ledgerOwnerRef.current.sessionID !== sessionID) {
    ledgerOwnerRef.current = {
      sessionID,
      ledger: createCreationSpecPreviewIntentLedger(keyFactory ? { keyFactory } : undefined)
    };
  }

  useEffect(() => () => {
    requestGenerationRef.current += 1;
    requestRef.current?.abort();
    requestRef.current = null;
  }, [sessionID]);

  // QuickCreate 的进程内交接可能在表单首次 render 后到达；仅填充仍为空的目标，绝不覆盖用户编辑。
  useEffect(() => {
    const handedOffGoal = initialGoal == null ? '' : String(initialGoal);
    if (!handedOffGoal) return;
    setForm((current) => current.goal === '' ? { ...current, goal: handedOffGoal } : current);
  }, [initialGoal]);

  useEffect(() => {
    if (!failure || (submission.inputID && failure.inputID !== submission.inputID)) return;
    setSubmission((current) => ({ ...current, kind: 'failure', error: failure }));
  }, [failure, submission.inputID]);

  const update = (field) => (event) => {
    setForm((current) => ({ ...current, [field]: event.target.value }));
    setSubmission((current) => current.kind === 'submitting'
      ? current
      : {
          ...INITIAL_SUBMISSION,
          kind: ['pending', 'editing', 'success', 'failure'].includes(current.kind) ? 'editing' : 'idle'
        });
  };

  const submit = async (event) => {
    event.preventDefault();
    if (submission.kind === 'submitting') return;
    let command;
    try {
      command = ledgerOwnerRef.current.ledger.claim({
        goal: form.goal,
        deliverableType: form.deliverableType,
        audience: form.audience,
        locale: form.locale,
        constraints: constraintsFromText(form.constraintsText)
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
        intent: command.intent,
        idempotencyKey: command.key,
        csrfToken,
        signal: controller.signal
      });
      if (generation !== requestGenerationRef.current || controller.signal.aborted) return;
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

  const disabled = submission.kind === 'submitting';
  return (
    <form className="creation-spec-preview-form" aria-labelledby="creation-spec-preview-form-title" onSubmit={submit}>
      <header>
        <h3 id="creation-spec-preview-form-title">创建目标预览</h3>
        <p>提交后只代表请求已受理；Draft 以工作台实时投影为准。</p>
      </header>

      <label htmlFor="creation-spec-goal">创作目标</label>
      <textarea
        id="creation-spec-goal"
        name="goal"
        required
        maxLength={2000}
        rows={4}
        disabled={disabled}
        value={form.goal}
        onChange={update('goal')}
        placeholder="例如：为新品发布制作一支 30 秒中文短片"
      />

      <div className="creation-spec-preview-form__grid">
        <label>
          <span>交付类型</span>
          <select disabled={disabled} value={form.deliverableType} onChange={update('deliverableType')}>
            <option value="video">视频</option>
            <option value="image_set">图片组</option>
            <option value="audio">音频</option>
            <option value="mixed">混合交付</option>
          </select>
        </label>
        <label>
          <span>语言</span>
          <select disabled={disabled} value={form.locale} onChange={update('locale')}>
            <option value="zh-CN">简体中文</option>
            <option value="en-US">English (US)</option>
          </select>
        </label>
      </div>

      <label htmlFor="creation-spec-audience">目标受众（可选）</label>
      <input
        id="creation-spec-audience"
        name="audience"
        maxLength={500}
        disabled={disabled}
        value={form.audience}
        onChange={update('audience')}
      />

      <label htmlFor="creation-spec-constraints">约束（每行一项，最多 8 项）</label>
      <textarea
        id="creation-spec-constraints"
        name="constraints"
        rows={3}
        disabled={disabled}
        value={form.constraintsText}
        onChange={update('constraintsText')}
      />

      <button type="submit" className="start-button" disabled={disabled}>
        {submission.kind === 'submitting' ? '正在提交…' : submission.kind === 'pending' ? '再次确认受理状态' : '生成开发预览'}
      </button>
      <SubmissionStatus submission={submission} />
    </form>
  );
}

function SubmissionStatus({ submission }) {
  if (submission.kind === 'submitting') return <p role="status">正在提交预览意图…</p>;
  if (submission.kind === 'pending') {
    return (
      <p role="status">
        请求已受理，正在等待 Creation Spec Draft。{submission.requestID ? `请求 ID：${submission.requestID}` : ''}
      </p>
    );
  }
  if (submission.kind === 'failure') {
    return (
      <p role="alert">
        预览生成失败：{submission.error?.summary || '请稍后重试。'}
        {submission.error?.resultCode ? `（${submission.error.resultCode}）` : ''}
      </p>
    );
  }
  if (submission.kind === 'editing') return <p role="status">表单语义已修改；再次提交会创建新的幂等意图。</p>;
  if (submission.kind === 'error') return <p role="alert">{submission.error?.message || '预览请求失败，请重试。'}</p>;
  return null;
}

function constraintsFromText(value) {
  return String(value || '').split(/\r?\n/).map((item) => item.trim()).filter(Boolean);
}

function publicPreviewError(error) {
  return {
    status: Number(error?.status) || 0,
    code: String(error?.code || 'CREATION_SPEC_PREVIEW_FAILED'),
    message: String(error?.message || '预览请求失败，请重试。'),
    requestID: String(error?.requestID || ''),
    retryable: Boolean(error?.retryable)
  };
}
