import { useEffect, useMemo, useRef, useState } from 'react';
import { createPreviewUUIDV7 } from '../aigc/creationSpecPreviewIntent.js';
import {
  enqueueAssembleOutputPreview,
  enqueueGenerateMediaPreview
} from './mediaPreviewApi.js';
import {
  normalizeAssembleOutputPreviewRequest,
  normalizeGenerateMediaPreviewRequest
} from './mediaPreviewContract.js';

// MediaPreviewControls 只提交冻结引用；Prompt 正文、Object Key 与 ffmpeg 参数不会进入请求。
export function MediaPreviewControls({
  sessionID,
  csrfToken,
  promptPreview,
  mediaCards = [],
  enqueueGenerate = enqueueGenerateMediaPreview,
  enqueueAssemble = enqueueAssembleOutputPreview,
  keyFactory = createPreviewUUIDV7
}) {
  const imageTargets = useMemo(
    () => promptPreview?.status === 'completed'
      ? promptPreview.prompts.filter((item) => item.mediaKind === 'image')
      : [],
    [promptPreview]
  );
  const readyPNGs = useMemo(
    () => mediaCards.filter((card) => card.status === 'completed' && card.toolKey === 'generate_media' &&
      card.assetRef?.status === 'ready' && card.assetRef?.mediaKind === 'image'),
    [mediaCards]
  );
  const [targetKey, setTargetKey] = useState('');
  const [sourceAssetID, setSourceAssetID] = useState('');
  const [generateState, setGenerateState] = useState(idle());
  const [assembleState, setAssembleState] = useState(idle());
  const generateLedger = useRef({ owner: '', semantic: '', key: '' });
  const assembleLedger = useRef({ owner: '', semantic: '', key: '' });
  const generateAbort = useRef(null);
  const assembleAbort = useRef(null);

  useEffect(() => {
    setTargetKey((current) => imageTargets.some((item) => item.targetLocalKey === current)
      ? current
      : imageTargets[0]?.targetLocalKey || '');
  }, [imageTargets]);

  useEffect(() => {
    setSourceAssetID((current) => readyPNGs.some((card) => card.assetRef.id === current)
      ? current
      : readyPNGs[0]?.assetRef.id || '');
  }, [readyPNGs]);

  useEffect(() => {
    generateAbort.current?.abort();
    assembleAbort.current?.abort();
    generateLedger.current = { owner: sessionID, semantic: '', key: '' };
    assembleLedger.current = { owner: sessionID, semantic: '', key: '' };
    setGenerateState(idle());
    setAssembleState(idle());
    return () => {
      generateAbort.current?.abort();
      assembleAbort.current?.abort();
    };
  }, [sessionID]);

  const submitGenerate = async (event) => {
    event.preventDefault();
    if (generateState.kind === 'submitting') return;
    let request;
    try {
      request = normalizeGenerateMediaPreviewRequest({ promptPreview, targetLocalKey: targetKey });
    } catch (error) {
      setGenerateState(failed(error));
      return;
    }
    const semantic = JSON.stringify(request);
    const ledger = stableLedger(generateLedger, sessionID, semantic, keyFactory);
    generateAbort.current?.abort();
    const controller = new AbortController();
    generateAbort.current = controller;
    setGenerateState({ kind: 'submitting', requestID: '', inputID: '', error: null });
    try {
      const receipt = await enqueueGenerate({
        sessionID, request, idempotencyKey: ledger.key, csrfToken, signal: controller.signal
      });
      if (!controller.signal.aborted) setGenerateState(pending(receipt));
    } catch (error) {
      if (!controller.signal.aborted && error?.name !== 'AbortError') setGenerateState(failed(error));
    } finally {
      if (generateAbort.current === controller) generateAbort.current = null;
    }
  };

  const submitAssemble = async (event) => {
    event.preventDefault();
    if (assembleState.kind === 'submitting') return;
    let request;
    try {
      const mediaCard = readyPNGs.find((card) => card.assetRef.id === sourceAssetID);
      request = normalizeAssembleOutputPreviewRequest({ mediaCard });
    } catch (error) {
      setAssembleState(failed(error));
      return;
    }
    const semantic = JSON.stringify(request);
    const ledger = stableLedger(assembleLedger, sessionID, semantic, keyFactory);
    assembleAbort.current?.abort();
    const controller = new AbortController();
    assembleAbort.current = controller;
    setAssembleState({ kind: 'submitting', requestID: '', inputID: '', error: null });
    try {
      const receipt = await enqueueAssemble({
        sessionID, request, idempotencyKey: ledger.key, csrfToken, signal: controller.signal
      });
      if (!controller.signal.aborted) setAssembleState(pending(receipt));
    } catch (error) {
      if (!controller.signal.aborted && error?.name !== 'AbortError') setAssembleState(failed(error));
    } finally {
      if (assembleAbort.current === controller) assembleAbort.current = null;
    }
  };

  return (
    <section className="media-preview-controls" aria-labelledby="media-preview-controls-title">
      <header>
        <h2 id="media-preview-controls-title">本地媒体开发预览</h2>
        <p>确定性 PNG 与固定 2 秒 MP4；零计费、零 Approval、零 TOS。</p>
      </header>
      <form aria-label="生成确定性 PNG" onSubmit={submitGenerate}>
        <label htmlFor="generate-media-target">Prompt 图片目标</label>
        <select id="generate-media-target" value={targetKey} disabled={generateState.kind === 'submitting' || imageTargets.length === 0}
          onChange={(event) => { setTargetKey(event.target.value); setGenerateState(idle()); }}>
          {imageTargets.length === 0 ? <option value="">请先完成包含图片目标的 Prompt Draft</option> : null}
          {imageTargets.map((target) => <option key={target.targetLocalKey} value={target.targetLocalKey}>{target.targetLocalKey} · {target.purpose}</option>)}
        </select>
        <button type="submit" className="start-button" disabled={!targetKey || generateState.kind === 'submitting'}>
          {generateState.kind === 'submitting' ? '正在受理…' : '生成测试 PNG'}
        </button>
        <SubmissionState state={generateState} label="PNG" />
      </form>
      <form aria-label="装配固定 MP4" onSubmit={submitAssemble}>
        <label htmlFor="assemble-output-source">Ready PNG</label>
        <select id="assemble-output-source" value={sourceAssetID} disabled={assembleState.kind === 'submitting' || readyPNGs.length === 0}
          onChange={(event) => { setSourceAssetID(event.target.value); setAssembleState(idle()); }}>
          {readyPNGs.length === 0 ? <option value="">请先等待 PNG Worker 完成</option> : null}
          {readyPNGs.map((card) => <option key={card.assetRef.id} value={card.assetRef.id}>{card.assetRef.id}</option>)}
        </select>
        <button type="submit" className="start-button" disabled={!sourceAssetID || assembleState.kind === 'submitting'}>
          {assembleState.kind === 'submitting' ? '正在受理…' : '装配测试 MP4'}
        </button>
        <SubmissionState state={assembleState} label="MP4" />
      </form>
    </section>
  );
}

function SubmissionState({ state, label }) {
  if (state.kind === 'submitting') return <p role="status">正在提交 {label} 开发预览…</p>;
  if (state.kind === 'pending') return <p role="status">{label} 请求已受理，等待 Worker 终态 SSE。请求 ID：{state.requestID}</p>;
  if (state.kind === 'error') return <p role="alert">{state.error?.message || `${label} 请求失败，请重试。`}</p>;
  return null;
}

function stableLedger(ref, owner, semantic, factory) {
  if (ref.current.owner !== owner || ref.current.semantic !== semantic) {
    ref.current = { owner, semantic, key: factory() };
  }
  return ref.current;
}

function idle() {
  return { kind: 'idle', requestID: '', inputID: '', error: null };
}

function pending(receipt) {
  return { kind: 'pending', requestID: receipt.requestID, inputID: receipt.inputID, error: null };
}

function failed(error) {
  return {
    kind: 'error', requestID: '', inputID: '',
    error: { code: String(error?.code || 'MEDIA_PREVIEW_FAILED'), message: String(error?.message || '媒体开发预览请求失败。') }
  };
}
