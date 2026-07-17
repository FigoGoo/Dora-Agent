import { useEffect, useRef, useState } from 'react';
import { enqueueWritePromptsPreview } from './writePromptsPreviewApi.js';
import { createWritePromptsPreviewIntentLedger } from './writePromptsPreviewIntent.js';

const INITIAL_FORM = Object.freeze({ writingInstruction: '', outputLanguage: '' });
const INITIAL_SUBMISSION = Object.freeze({ kind: 'idle', inputID: '', requestID: '', error: null });

// WritePromptsPreviewForm 只允许填写写作语义；Source Ref 始终来自当前严格 Storyboard Card。
export function WritePromptsPreviewForm({
  sessionID,
  csrfToken,
  storyboardPreview,
  failure,
  enqueue = enqueueWritePromptsPreview,
  keyFactory
}) {
  const [form, setForm] = useState(INITIAL_FORM);
  const [submission, setSubmission] = useState(INITIAL_SUBMISSION);
  const ledgerOwnerRef = useRef(null);
  const acceptedReceiptsRef = useRef(new Map());
  const requestRef = useRef(null);
  const requestGenerationRef = useRef(0);
  const storyboardPreviewRef = storyboardPreviewRefFromCard(storyboardPreview);
  const resourceIdentity = storyboardPreviewRef
    ? `${storyboardPreviewRef.id}:${storyboardPreviewRef.version}:${storyboardPreviewRef.contentDigest}`
    : '';
  const ledgerOwner = `${sessionID || ''}:${resourceIdentity}`;

  if (!ledgerOwnerRef.current || ledgerOwnerRef.current.owner !== ledgerOwner) {
    acceptedReceiptsRef.current.clear();
    ledgerOwnerRef.current = {
      owner: ledgerOwner,
      ledger: createWritePromptsPreviewIntentLedger(keyFactory ? { keyFactory } : undefined)
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
    if (!failure || !submission.inputID || failure.inputID !== submission.inputID) return;
    setSubmission((current) => ({ ...current, kind: 'failure', error: failure }));
  }, [failure, submission.inputID]);

  const update = (field) => (event) => {
    setForm((current) => ({ ...current, [field]: event.target.value }));
    setSubmission((current) => current.kind === 'submitting'
      ? current
      : { ...INITIAL_SUBMISSION, kind: ['pending', 'editing', 'failure', 'error'].includes(current.kind) ? 'editing' : 'idle' });
  };

  const submit = async (event) => {
    event.preventDefault();
    if (submission.kind === 'submitting' || !storyboardPreviewRef) return;
    let command;
    try {
      command = ledgerOwnerRef.current.ledger.claim({
        storyboardPreviewRef,
        toolIntent: form
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
        storyboardPreviewRef: command.storyboardPreviewRef,
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

  const disabled = submission.kind === 'submitting' || !storyboardPreviewRef;
  return (
    <form className="write-prompts-preview-form" aria-labelledby="write-prompts-preview-form-title" onSubmit={submit}>
      <header>
        <h3 id="write-prompts-preview-form-title">Prompt JSON Draft</h3>
        <p>
          {storyboardPreviewRef
            ? `自动绑定当前 Storyboard Draft v${storyboardPreviewRef.version} 的全部 ${storyboardPreview.slots.length} 个槽位。`
            : '需要当前 Storyboard Draft 至少包含一个媒体槽位。'}
        </p>
      </header>

      <label htmlFor="write-prompts-writing-instruction">提示词写作要求</label>
      <textarea
        id="write-prompts-writing-instruction"
        name="writing_instruction"
        required
        maxLength={1000}
        rows={4}
        disabled={disabled}
        value={form.writingInstruction}
        onChange={update('writingInstruction')}
        placeholder="例如：为每个媒体槽位编写清晰、可直接复用的生成提示词"
      />

      <label htmlFor="write-prompts-output-language">输出语言（可选）</label>
      <select
        id="write-prompts-output-language"
        name="output_language"
        disabled={disabled}
        value={form.outputLanguage}
        onChange={update('outputLanguage')}
      >
        <option value="">使用运行时默认语言</option>
        <option value="zh-CN">简体中文（zh-CN）</option>
        <option value="en-US">English (en-US)</option>
      </select>

      <button type="submit" className="start-button" disabled={disabled}>
        {submission.kind === 'submitting'
          ? '正在提交…'
          : submission.kind === 'pending' ? '再次确认受理状态' : '生成提示词开发预览'}
      </button>
      <SubmissionStatus submission={submission} />
    </form>
  );
}

function storyboardPreviewRefFromCard(storyboardPreview) {
  if (!storyboardPreview || storyboardPreview.kind !== 'plan_storyboard_preview'
      || storyboardPreview.status !== 'completed' || !Array.isArray(storyboardPreview.slots)
      || storyboardPreview.slots.length < 1) return null;
  return {
    id: storyboardPreview.storyboardPreviewID,
    version: storyboardPreview.version,
    contentDigest: storyboardPreview.contentDigest
  };
}

function recordAcceptedReceipt(receipts, idempotencyKey, accepted) {
  const fields = ['requestID', 'inputID', 'turnID', 'runID', 'toolCallID'];
  if (!accepted || accepted.status !== 'pending' || typeof accepted.replayed !== 'boolean'
      || fields.some((field) => typeof accepted[field] !== 'string' || accepted[field] === '')) {
    throw previewReceiptError('Prompt Preview 返回了无效的 pending 回执');
  }
  const identity = fields.map((field) => accepted[field]).join(':');
  const previous = receipts.get(idempotencyKey);
  if (previous && (!accepted.replayed || previous !== identity)) {
    throw previewReceiptError('Prompt Preview 同键重放身份不一致');
  }
  if (!previous) receipts.set(idempotencyKey, identity);
}

function SubmissionStatus({ submission }) {
  if (submission.kind === 'submitting') return <p role="status">正在提交 Write Prompts typed Input…</p>;
  if (submission.kind === 'pending') {
    return <p role="status">请求已受理，正在等待 Prompt JSON Draft。{submission.requestID ? `请求 ID：${submission.requestID}` : ''}</p>;
  }
  if (submission.kind === 'failure') {
    return (
      <p role="alert">
        提示词生成未完成：{submission.error?.summary || '请稍后重试。'}
        {submission.error?.resultCode ? `（${submission.error.resultCode}）` : ''}
      </p>
    );
  }
  if (submission.kind === 'editing') return <p role="status">写作语义已修改；再次提交会创建新的幂等意图。</p>;
  if (submission.kind === 'error') return <p role="alert">{submission.error?.message || '提示词预览请求失败，请重试。'}</p>;
  return null;
}

function previewReceiptError(message) {
  const error = new Error(message);
  error.code = 'WRITE_PROMPTS_PREVIEW_REPLAY_MISMATCH';
  return error;
}

function publicPreviewError(error) {
  return {
    status: Number(error?.status) || 0,
    code: String(error?.code || 'WRITE_PROMPTS_PREVIEW_FAILED'),
    message: String(error?.message || '提示词预览请求失败，请重试。'),
    requestID: String(error?.requestID || ''),
    retryable: Boolean(error?.retryable)
  };
}
