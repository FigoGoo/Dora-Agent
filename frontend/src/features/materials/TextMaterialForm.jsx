import { useEffect, useRef, useState } from 'react';
import { enqueueAnalyzeMaterialsPreview } from '../aigc/analyzeMaterialsPreviewApi.js';
import { createPreviewUUIDV7 } from '../aigc/creationSpecPreviewIntent.js';
import { createTextMaterial, loadTextMaterials } from './textMaterialApi.js';
import { normalizeTextMaterialContent } from './textMaterialContract.js';

const FOCUS_OPTIONS = Object.freeze([
  ['content', '内容'],
  ['visual', '视觉'],
  ['narrative', '叙事'],
  ['risk', '风险']
]);

// TextMaterialForm 提供“创建/列出/选择文本素材 → 提交 analyze_materials typed Intent”的最小工作台纵切。
export function TextMaterialForm({
  projectID,
  sessionID,
  csrfToken,
  load = loadTextMaterials,
  create = createTextMaterial,
  enqueueAnalyze = enqueueAnalyzeMaterialsPreview,
  materialKeyFactory = createPreviewUUIDV7,
  analysisKeyFactory = createPreviewUUIDV7
}) {
  const [materialsState, setMaterialsState] = useState({ kind: 'loading', items: [], error: null });
  const [content, setContent] = useState('');
  const [createState, setCreateState] = useState({ kind: 'idle', requestID: '', error: null });
  const [selectedAssetIDs, setSelectedAssetIDs] = useState([]);
  const [analysisGoal, setAnalysisGoal] = useState('');
  const [focusDimensions, setFocusDimensions] = useState(['content']);
  const [outputLanguage, setOutputLanguage] = useState('zh-CN');
  const [analysisState, setAnalysisState] = useState({ kind: 'idle', requestID: '', inputID: '', error: null });
  const createLedgerRef = useRef({ owner: '', semantic: '', key: '' });
  const analysisLedgerRef = useRef({ owner: '', semantic: '', key: '' });
  const createRequestRef = useRef(null);
  const analysisRequestRef = useRef(null);

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    setMaterialsState({ kind: 'loading', items: [], error: null });
    load({ projectID, signal: controller.signal }).then((result) => {
      if (!active || controller.signal.aborted) return;
      setMaterialsState({ kind: 'ready', items: result.items, error: null });
    }).catch((error) => {
      if (!active || controller.signal.aborted || error?.name === 'AbortError') return;
      setMaterialsState({ kind: 'error', items: [], error: publicError(error, 'TEXT_MATERIAL_LIST_FAILED') });
    });
    return () => {
      active = false;
      controller.abort();
    };
  }, [load, projectID]);

  useEffect(() => {
    const owner = `${projectID}:${sessionID}`;
    createLedgerRef.current = { owner: projectID, semantic: '', key: '' };
    analysisLedgerRef.current = { owner, semantic: '', key: '' };
    createRequestRef.current?.abort();
    analysisRequestRef.current?.abort();
    setSelectedAssetIDs([]);
    setCreateState({ kind: 'idle', requestID: '', error: null });
    setAnalysisState({ kind: 'idle', requestID: '', inputID: '', error: null });
    return () => {
      createRequestRef.current?.abort();
      analysisRequestRef.current?.abort();
    };
  }, [projectID, sessionID]);

  const submitCreate = async (event) => {
    event.preventDefault();
    if (createState.kind === 'submitting') return;
    let normalized;
    try {
      normalized = normalizeTextMaterialContent(content);
    } catch (error) {
      setCreateState({ kind: 'error', requestID: '', error: publicError(error, 'TEXT_MATERIAL_INVALID') });
      return;
    }
    const semantic = normalized;
    let ledger = createLedgerRef.current;
    if (ledger.owner !== projectID || ledger.semantic !== semantic) {
      ledger = { owner: projectID, semantic, key: materialKeyFactory() };
      createLedgerRef.current = ledger;
    }

    createRequestRef.current?.abort();
    const controller = new AbortController();
    createRequestRef.current = controller;
    setCreateState({ kind: 'submitting', requestID: '', error: null });
    try {
      const result = await create({
        projectID, content: normalized, idempotencyKey: ledger.key, csrfToken, signal: controller.signal
      });
      if (controller.signal.aborted) return;
      setMaterialsState((current) => ({
        kind: 'ready',
        items: sortMaterials([result.material, ...current.items.filter((item) => item.assetID !== result.material.assetID)]),
        error: null
      }));
      setSelectedAssetIDs((current) => current.includes(result.material.assetID)
        ? current
        : current.length < 8 ? [...current, result.material.assetID].sort() : current);
      setCreateState({ kind: 'saved', requestID: result.requestID, error: null, replayed: result.replayed });
    } catch (error) {
      if (controller.signal.aborted || error?.name === 'AbortError') return;
      setCreateState({ kind: 'error', requestID: '', error: publicError(error, 'TEXT_MATERIAL_CREATE_FAILED') });
    } finally {
      if (createRequestRef.current === controller) createRequestRef.current = null;
    }
  };

  const toggleAsset = (assetID) => (event) => {
    setSelectedAssetIDs((current) => {
      if (!event.target.checked) return current.filter((value) => value !== assetID);
      if (current.includes(assetID) || current.length >= 8) return current;
      return [...current, assetID].sort();
    });
    setAnalysisState((current) => current.kind === 'submitting'
      ? current
      : { kind: 'editing', requestID: '', inputID: '', error: null });
  };

  const toggleFocus = (focus) => (event) => {
    setFocusDimensions((current) => {
      if (event.target.checked) return current.includes(focus) ? current : [...current, focus];
      return current.filter((value) => value !== focus);
    });
    setAnalysisState((current) => current.kind === 'submitting'
      ? current
      : { kind: 'editing', requestID: '', inputID: '', error: null });
  };

  const submitAnalyze = async (event) => {
    event.preventDefault();
    if (analysisState.kind === 'submitting') return;
    const selected = [...selectedAssetIDs].sort();
    const goal = analysisGoal.normalize('NFC').trim();
    const focuses = FOCUS_OPTIONS.map(([value]) => value).filter((value) => focusDimensions.includes(value));
    if (selected.length < 1 || selected.length > 8 || [...goal].length < 1 || [...goal].length > 1000 || focuses.length < 1) {
      setAnalysisState({
        kind: 'error', requestID: '', inputID: '',
        error: { message: '请选择 1..8 条素材，并填写分析目标和至少一个关注维度。', code: 'ANALYZE_MATERIALS_INTENT_INVALID' }
      });
      return;
    }
    const materialByID = new Map(materialsState.items.map((item) => [item.assetID, item]));
    if (selected.some((assetID) => !materialByID.has(assetID))) {
      setAnalysisState({
        kind: 'error', requestID: '', inputID: '',
        error: { message: '素材列表已经变化，请刷新后重新选择。', code: 'ANALYZE_MATERIALS_ASSET_STALE' }
      });
      return;
    }
    const intent = {
      schema_version: 'analyze_materials.preview.intent.v1',
      asset_ids: selected,
      analysis_goal: goal,
      focus_dimensions: focuses,
      output_language: outputLanguage,
      expected_assets: selected.map((assetID) => ({ asset_id: assetID, asset_version: materialByID.get(assetID).assetVersion }))
    };
    const semantic = JSON.stringify(intent);
    const owner = `${projectID}:${sessionID}`;
    let ledger = analysisLedgerRef.current;
    if (ledger.owner !== owner || ledger.semantic !== semantic) {
      ledger = { owner, semantic, key: analysisKeyFactory() };
      analysisLedgerRef.current = ledger;
    }

    analysisRequestRef.current?.abort();
    const controller = new AbortController();
    analysisRequestRef.current = controller;
    setAnalysisState({ kind: 'submitting', requestID: '', inputID: '', error: null });
    try {
      const accepted = await enqueueAnalyze({
        sessionID, intent, idempotencyKey: ledger.key, csrfToken, signal: controller.signal
      });
      if (controller.signal.aborted) return;
      setAnalysisState({
        kind: 'pending', requestID: accepted.requestID, inputID: accepted.inputID, error: null
      });
    } catch (error) {
      if (controller.signal.aborted || error?.name === 'AbortError') return;
      setAnalysisState({
        kind: 'error', requestID: '', inputID: '', error: publicError(error, 'ANALYZE_MATERIALS_PREVIEW_FAILED')
      });
    } finally {
      if (analysisRequestRef.current === controller) analysisRequestRef.current = null;
    }
  };

  const analysisDisabled = analysisState.kind === 'submitting';
  const createDisabled = createState.kind === 'submitting';

  return (
    <section className="text-material-panel" aria-labelledby="text-material-panel-title">
      <header>
        <h2 id="text-material-panel-title">文本素材与素材分析</h2>
        <p>当前最小纵切只支持文本；分析结果以工作台 SSE 投影为准。</p>
      </header>

      <form className="text-material-form" aria-label="创建文本素材" onSubmit={submitCreate}>
        <label htmlFor="text-material-content">文本素材正文</label>
        <textarea
          id="text-material-content"
          required
          maxLength={2000}
          rows={5}
          disabled={createDisabled}
          value={content}
          onChange={(event) => {
            setContent(event.target.value);
            setCreateState((current) => current.kind === 'submitting'
              ? current
              : { kind: 'editing', requestID: '', error: null });
          }}
          placeholder="粘贴需要分析的文案、脚本、资料摘要或产品说明"
        />
        <button type="submit" className="start-button" disabled={createDisabled}>
          {createDisabled ? '正在保存…' : '保存文本素材'}
        </button>
        <CreateStatus state={createState} />
      </form>

      <section className="text-material-list" aria-labelledby="text-material-list-title">
        <h3 id="text-material-list-title">当前项目素材</h3>
        {materialsState.kind === 'loading' ? <p role="status">正在加载文本素材…</p> : null}
        {materialsState.kind === 'error' ? <p role="alert">{materialsState.error?.message || '文本素材加载失败，请刷新重试。'}</p> : null}
        {materialsState.kind === 'ready' && materialsState.items.length === 0 ? <p>还没有文本素材，请先保存一条。</p> : null}
        {materialsState.kind === 'ready' && materialsState.items.length > 0 ? (
          <ul>
            {materialsState.items.map((material) => {
              const checked = selectedAssetIDs.includes(material.assetID);
              const maxReached = selectedAssetIDs.length >= 8 && !checked;
              return (
                <li key={material.assetID}>
                  <label>
                    <input
                      type="checkbox"
                      name="text_material_asset_ids"
                      value={material.assetID}
                      checked={checked}
                      disabled={analysisDisabled || maxReached}
                      onChange={toggleAsset(material.assetID)}
                    />
                    <span>{material.content}</span>
                  </label>
                </li>
              );
            })}
          </ul>
        ) : null}
        <p>{selectedAssetIDs.length}/8 条已选择</p>
      </section>

      <form className="analyze-materials-form" aria-label="分析文本素材" onSubmit={submitAnalyze}>
        <label htmlFor="analyze-materials-goal">分析目标</label>
        <textarea
          id="analyze-materials-goal"
          required
          maxLength={1000}
          rows={4}
          disabled={analysisDisabled}
          value={analysisGoal}
          onChange={(event) => {
            setAnalysisGoal(event.target.value);
            setAnalysisState((current) => current.kind === 'submitting'
              ? current
              : { kind: 'editing', requestID: '', inputID: '', error: null });
          }}
          placeholder="例如：识别素材主题、核心卖点和可复用叙事元素"
        />

        <fieldset disabled={analysisDisabled}>
          <legend>关注维度（至少一项）</legend>
          {FOCUS_OPTIONS.map(([value, label]) => (
            <label key={value}>
              <input
                type="checkbox"
                name="focus_dimensions"
                value={value}
                checked={focusDimensions.includes(value)}
                onChange={toggleFocus(value)}
              />
              {label}
            </label>
          ))}
        </fieldset>

        <label htmlFor="analyze-materials-language">输出语言</label>
        <select
          id="analyze-materials-language"
          disabled={analysisDisabled}
          value={outputLanguage}
          onChange={(event) => {
            setOutputLanguage(event.target.value);
            setAnalysisState({ kind: 'editing', requestID: '', inputID: '', error: null });
          }}
        >
          <option value="zh-CN">简体中文</option>
          <option value="en-US">English (US)</option>
        </select>

        <button type="submit" className="start-button" disabled={analysisDisabled || materialsState.kind !== 'ready'}>
          {analysisDisabled ? '正在提交…' : analysisState.kind === 'pending' ? '再次确认受理状态' : '提交素材分析'}
        </button>
        <AnalysisStatus state={analysisState} />
      </form>
    </section>
  );
}

function CreateStatus({ state }) {
  if (state.kind === 'submitting') return <p role="status">正在保存文本素材…</p>;
  if (state.kind === 'saved') return <p role="status">{state.replayed ? '文本素材已按原幂等键重放。' : '文本素材已保存并可选择。'}</p>;
  if (state.kind === 'editing') return <p role="status">正文已修改；保存时会使用新的素材 ID。</p>;
  if (state.kind === 'error') return <p role="alert">{state.error?.message || '文本素材保存失败，请重试。'}</p>;
  return null;
}

function AnalysisStatus({ state }) {
  if (state.kind === 'submitting') return <p role="status">正在提交素材分析 typed Intent…</p>;
  if (state.kind === 'pending') {
    return (
      <p role="status">
        素材分析请求已受理，正在等待工作台 SSE 返回最终结果。
        {state.requestID ? `请求 ID：${state.requestID}` : ''}
      </p>
    );
  }
  if (state.kind === 'editing') return <p role="status">分析语义已修改；再次提交会使用新的幂等键。</p>;
  if (state.kind === 'error') return <p role="alert">{state.error?.message || '素材分析请求失败，请重试。'}</p>;
  return null;
}

function sortMaterials(items) {
  return [...items].sort((left, right) => {
    if (left.createdAtMs !== right.createdAtMs) return right.createdAtMs - left.createdAtMs;
    return right.assetID.localeCompare(left.assetID);
  });
}

function publicError(error, fallbackCode) {
  return {
    status: Number(error?.status) || 0,
    code: String(error?.code || fallbackCode),
    message: String(error?.message || '请求失败，请重试。'),
    requestID: String(error?.requestID || ''),
    retryable: Boolean(error?.retryable)
  };
}
