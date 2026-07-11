import { useEffect, useState } from 'react';
import { Check, Clapperboard, Film, Image, Layers3, Music, RefreshCw, Save, WandSparkles } from 'lucide-react';

// StoryboardPanel 渲染左侧故事板业务组件，和右侧 A2UI 消息卡保持职责隔离。
export function StoryboardPanel({
  storyboard,
  selectedTarget,
  onSelectTarget,
  editing,
  onStartEdit,
  onChangeEdit,
  onSaveEdit,
  onRegenerateTarget,
  onActivateCandidate,
  candidateApprovalReview,
  onConfirmCandidateAssets,
  candidateApprovalBusy,
  onBindAsset,
  onUploadAsset,
  assetMap,
  statusLabel
}) {
  const keyElements = storyboard?.key_elements || [];
  const shots = storyboard?.shots || [];
  const audioLayers = storyboard?.audio_layers || [];
  const activeRevision = activeStoryboardRevision(storyboard);
  const modules = activeRevision?.modules || storyboard?.modules || [];
  const dependencies = activeRevision?.dependencies || storyboard?.dependencies || [];
  const bindings = storyboard?.bindings || [];
  const assetActionsDisabled = Boolean(storyboard?.pending_revision_id);
  const unifiedCandidateApprovalIDs = new Set(
    (candidateApprovalReview?.bindings || []).map((binding) => String(binding?.approval_id || '')).filter(Boolean)
  );

  return (
    <section className="aigc-storyboard-pane" aria-label="故事板">
      <div className="aigc-pane-header">
        <div>
          <Clapperboard aria-hidden="true" size={17} />
          <strong>故事板</strong>
        </div>
        <span>{statusLabel(storyboard?.status) || '未生成'}</span>
      </div>

      <div className="aigc-selected-target">
        <span>当前绑定</span>
        <strong>{selectedTarget?.label || '选择故事板项'}</strong>
      </div>

      {candidateApprovalReview?.ready ? (
        <section className="aigc-candidate-review" aria-label="统一素材确认">
          <div>
            <Check aria-hidden="true" size={16} />
            <strong>本轮素材已生成完成</strong>
            <span>{candidateApprovalReview.bindings.length} 项待确认</span>
          </div>
          <p>确认后将统一采用本轮全部候选素材，并继续后续生成流程。</p>
          <button type="button" disabled={candidateApprovalBusy} onClick={() => onConfirmCandidateAssets?.()}>
            <Check aria-hidden="true" size={15} />
            <span>{candidateApprovalBusy ? '确认中…' : '确认并采用全部素材'}</span>
          </button>
        </section>
      ) : null}

      {modules.length > 0 ? (
        <div className="aigc-dynamic-storyboard">
          {assetActionsDisabled ? (
            <p className="aigc-storyboard-review-notice" role="status">
              故事板方案待审核，请先确认或拒绝后再生成、采用或填入素材。
            </p>
          ) : null}
          {modules
            .slice()
            .sort((left, right) => (left.order || 0) - (right.order || 0))
            .map((module) => (
              <DynamicModule
                key={module.id || module.key}
                module={module}
                dependencies={dependencies}
                selectedTarget={selectedTarget}
                onSelectTarget={onSelectTarget}
                editing={editing}
                onStartEdit={onStartEdit}
                onChangeEdit={onChangeEdit}
                onSaveEdit={onSaveEdit}
                onRegenerateTarget={onRegenerateTarget}
                onActivateCandidate={onActivateCandidate}
                unifiedCandidateApprovalIDs={unifiedCandidateApprovalIDs}
                onBindAsset={onBindAsset}
                onUploadAsset={onUploadAsset}
                assetActionsDisabled={assetActionsDisabled}
                bindings={bindings}
                assetMap={assetMap}
                statusLabel={statusLabel}
              />
            ))}
        </div>
      ) : (
        <LegacyStoryboard
          keyElements={keyElements}
          shots={shots}
          audioLayers={audioLayers}
          selectedTarget={selectedTarget}
          onSelectTarget={onSelectTarget}
          editing={editing}
          onStartEdit={onStartEdit}
          onChangeEdit={onChangeEdit}
          onSaveEdit={onSaveEdit}
          assetMap={assetMap}
          statusLabel={statusLabel}
        />
      )}
    </section>
  );
}

function LegacyStoryboard({
  keyElements,
  shots,
  audioLayers,
  selectedTarget,
  onSelectTarget,
  editing,
  onStartEdit,
  onChangeEdit,
  onSaveEdit,
  assetMap,
  statusLabel
}) {
  return (
    <>

      <StoryboardSection title="关键元素" count={keyElements.length}>
        {keyElements.map((element, index) => {
          const selected = selectedTarget?.type === 'key_element' && selectedTarget.id === element.key;
          return (
            <article
              className={selected ? 'aigc-story-item is-selected' : 'aigc-story-item'}
              key={element.key}
              onClick={() => onSelectTarget({ type: 'key_element', id: element.key, label: element.name || element.key })}
            >
              <button
                className="aigc-story-item__icon"
                type="button"
                aria-label={`选择关键元素 ${element.name || element.key}`}
                onClick={(event) => {
                  event.stopPropagation();
                  onSelectTarget({ type: 'key_element', id: element.key, label: element.name || element.key });
                }}
              >
                <Image aria-hidden="true" size={16} />
              </button>
              <div className="aigc-story-item__body">
                <InlineEditable
                  editing={editing}
                  path={`/key_elements/${index}/name`}
                  value={element.name || element.key}
                  onStart={onStartEdit}
                  onChange={onChangeEdit}
                  onSave={onSaveEdit}
                />
                <p>{element.description || element.prompt || '待补充描述'}</p>
                <AssetStrip ids={element.asset_ids} assetMap={assetMap} />
              </div>
              <StatusPill status={element.status} statusLabel={statusLabel} />
            </article>
          );
        })}
      </StoryboardSection>

      <StoryboardSection title="分镜" count={shots.length}>
        {shots.map((shot, index) => {
          const selected = selectedTarget?.type === 'shot' && selectedTarget.id === shot.shot_id;
          return (
            <article
              className={selected ? 'aigc-shot-item is-selected' : 'aigc-shot-item'}
              key={shot.shot_id}
              onClick={() =>
                onSelectTarget({
                  type: 'shot',
                  id: shot.shot_id,
                  field: 'keyframe_asset_id',
                  label: `镜头 ${shot.index || index + 1}`
                })
              }
            >
              <button
                className="aigc-shot-preview"
                type="button"
                aria-label={`选择镜头 ${shot.index || index + 1}`}
                onClick={(event) => {
                  event.stopPropagation();
                  onSelectTarget({
                    type: 'shot',
                    id: shot.shot_id,
                    field: 'keyframe_asset_id',
                    label: `镜头 ${shot.index || index + 1}`
                  });
                }}
              >
                <AssetImage asset={assetMap.get(shot.keyframe_asset_id)} fallback={<Film aria-hidden="true" size={20} />} />
              </button>
              <div className="aigc-shot-copy">
                <div className="aigc-shot-copy__meta">
                  <strong>镜头 {shot.index || index + 1}</strong>
                  <span>{shot.duration_sec ? `${shot.duration_sec}s` : statusLabel(shot.status) || '规划中'}</span>
                </div>
                <InlineEditable
                  multiline
                  editing={editing}
                  path={`/shots/${index}/scene_description`}
                  value={shot.scene_description || shot.prompt || '待补充分镜描述'}
                  onStart={onStartEdit}
                  onChange={onChangeEdit}
                  onSave={onSaveEdit}
                />
                <p>{shot.camera_design || shot.narration || '镜头设计待完善'}</p>
              </div>
            </article>
          );
        })}
      </StoryboardSection>

      <StoryboardSection title="旁白与音乐" count={audioLayers.length}>
        {audioLayers.map((layer, index) => {
          const selected = selectedTarget?.type === 'audio_layer' && selectedTarget.id === layer.layer_id;
          return (
            <button
              className={selected ? 'aigc-story-item is-selected' : 'aigc-story-item'}
              type="button"
              key={layer.layer_id}
              onClick={() => onSelectTarget({ type: 'audio_layer', id: layer.layer_id, label: layer.type || `音频 ${index + 1}` })}
            >
              <div className="aigc-story-item__icon">
                <Music aria-hidden="true" size={16} />
              </div>
              <div className="aigc-story-item__body">
                <strong>{layer.type || `音频 ${index + 1}`}</strong>
                <p>{layer.description || layer.prompt || '待规划音频层'}</p>
              </div>
              <StatusPill status={layer.status} statusLabel={statusLabel} />
            </button>
          );
        })}
      </StoryboardSection>
    </>
  );
}

function DynamicModule({
  module,
  dependencies,
  selectedTarget,
  onSelectTarget,
  editing,
  onStartEdit,
  onChangeEdit,
  onSaveEdit,
  onRegenerateTarget,
  onActivateCandidate,
  unifiedCandidateApprovalIDs,
  onBindAsset,
  onUploadAsset,
  assetActionsDisabled,
  bindings,
  assetMap,
  statusLabel
}) {
  const elements = module.elements || [];
  const capabilities = module.capabilities || {};
  return (
    <section className="aigc-dynamic-module">
      <header className="aigc-dynamic-module__header">
        <div>
          <Layers3 aria-hidden="true" size={16} />
          <div>
            <strong>{module.title || module.semantic_type || module.key}</strong>
            {module.description ? <p>{module.description}</p> : null}
          </div>
        </div>
        <span>{capabilities.has_timeline ? '时间线 · ' : ''}{elements.length}/{module.planned_count || elements.length}</span>
      </header>
      <div className="aigc-dynamic-module__elements">
        {elements.map((element) => (
          <DynamicElement
            key={element.id || element.key}
            module={module}
            element={element}
            dependencies={dependencies}
            capabilities={capabilities}
            selected={selectedTarget?.id === (element.id || element.key)}
            onSelectTarget={onSelectTarget}
            editing={editing}
            onStartEdit={onStartEdit}
            onChangeEdit={onChangeEdit}
            onSaveEdit={onSaveEdit}
            onRegenerateTarget={onRegenerateTarget}
            onActivateCandidate={onActivateCandidate}
            unifiedCandidateApprovalIDs={unifiedCandidateApprovalIDs}
            onBindAsset={onBindAsset}
            onUploadAsset={onUploadAsset}
            assetActionsDisabled={assetActionsDisabled}
            bindings={bindings}
            assetMap={assetMap}
            statusLabel={statusLabel}
          />
        ))}
      </div>
    </section>
  );
}

function DynamicElement({
  module,
  element,
  dependencies,
  capabilities,
  selected,
  onSelectTarget,
  editing,
  onStartEdit,
  onChangeEdit,
  onSaveEdit,
  onRegenerateTarget,
  onActivateCandidate,
  unifiedCandidateApprovalIDs,
  onBindAsset,
  onUploadAsset,
  assetActionsDisabled,
  bindings,
  assetMap,
  statusLabel
}) {
  const targetID = element.id || element.key;
  const promptSlots = element.prompt_slots || [];
  const assetSlots = element.asset_slots || [];
  const summary = elementSummary(element);
  const dependencySummary = elementDependencySummary(element, dependencies);
  return (
    <article className={selected ? 'aigc-dynamic-element is-selected' : 'aigc-dynamic-element'}>
      <button
        className="aigc-dynamic-element__select"
        type="button"
        onClick={() =>
          onSelectTarget({
            type: element.semantic_type || module.semantic_type || 'element',
            id: targetID,
            label: element.title || element.key || targetID,
            moduleID: module.id,
            targetRevision: element.revision
          })
        }
      >
        <div>
          <WandSparkles aria-hidden="true" size={15} />
          <strong>{element.title || element.key || targetID}</strong>
        </div>
        <StatusPill status={element.review_state || aggregateElementStatus(assetSlots, promptSlots)} statusLabel={statusLabel} />
      </button>
      {summary ? <p className="aigc-dynamic-element__summary">{summary}</p> : null}
      {dependencySummary ? <p className="aigc-dynamic-element__summary">依赖：{dependencySummary}</p> : null}

      {(capabilities.requires_prompt || promptSlots.length > 0) && (
        <div className="aigc-prompt-slots">
          {promptSlots.map((slot) => {
            const prompt = promptSlotValue(slot, element);
            const editKey = `prompt:${targetID}:${slot.purpose || 'default'}`;
            return (
              <div className="aigc-prompt-slot" key={slot.purpose || slot.prompt_ref || editKey}>
                <div className="aigc-prompt-slot__meta">
                  <span>{slot.purpose || 'Prompt'}</span>
                  <small>v{slot.revision || 1} · {statusLabel(slot.status) || slot.status || 'ready'}</small>
                </div>
                <InlineEditable
                  multiline
                  editing={editing}
                  path={editKey}
                  value={prompt}
                  placeholder="点击补充提示词"
                  onStart={({ path, value }) =>
                    onStartEdit({
                      kind: 'prompt',
                      path,
                      value,
                      targetID,
                      targetRevision: element.revision || 0,
                      purpose: slot.purpose || 'default',
                      promptRevision: slot.revision || 0
                    })
                  }
                  onChange={onChangeEdit}
                  onSave={onSaveEdit}
                />
              </div>
            );
          })}
        </div>
      )}

      {(capabilities.requires_asset || assetSlots.length > 0) && (
        <div className="aigc-dynamic-assets">
          {assetSlots.map((slot) => {
            const activeBinding = findBinding(bindings, slot.active_binding_id);
            const candidateBindings = (slot.candidate_ids || []).map((id) => findBinding(bindings, id)).filter(Boolean);
            const activeAsset = activeBinding?.asset_id ? assetMap.get(activeBinding.asset_id) : null;
            const availableAssets = compatibleAssets(assetMap, slot).filter(
              (asset) => asset.id !== activeBinding?.asset_id && !candidateBindings.some((binding) => binding.asset_id === asset.id)
            );
            return (
              <div className="aigc-dynamic-asset-slot" key={slot.key}>
                <div className="aigc-dynamic-asset-slot__preview">
                  <AssetPreview asset={activeAsset} fallback={<Image aria-hidden="true" size={18} />} />
                </div>
                <div>
                  <strong>{slot.role || slot.key}</strong>
                  <span>{statusLabel(slot.status) || slot.status || 'missing'} · epoch {slot.generation_epoch || 0}</span>
                  {candidateBindings.map((binding) => {
                    const candidateAsset = assetMap.get(binding.asset_id);
                    const waitsForUnifiedApproval = unifiedCandidateApprovalIDs?.has(String(binding.approval_id || ''));
                    return (
                      <div className="aigc-candidate-preview" key={binding.id}>
                        <AssetPreview asset={candidateAsset} fallback={<span>候选素材</span>} />
                        {waitsForUnifiedApproval ? (
                          <span className="aigc-candidate-pending">待统一确认</span>
                        ) : (
                          <button
                            className="aigc-candidate-button"
                            type="button"
                            disabled={assetActionsDisabled}
                            onClick={() => onActivateCandidate?.({ targetID, targetRevision: element.revision || 0, slot, binding })}
                          >
                            <Check aria-hidden="true" size={13} />采用候选
                          </button>
                        )}
                      </div>
                    );
                  })}
                  {availableAssets.length > 0 ? (
                    <select
                      className="aigc-asset-picker"
                      aria-label={`为${slot.role || slot.key}选择已有素材`}
                      disabled={assetActionsDisabled}
                      defaultValue=""
                      onChange={(event) => {
                        const assetID = event.target.value;
                        event.target.value = '';
                        if (assetID) {
                          onBindAsset?.({ targetID, targetRevision: element.revision || 0, slot, assetID });
                        }
                      }}
                    >
                      <option value="">填入已有素材…</option>
                      {availableAssets.map((asset) => (
                        <option value={asset.id} key={asset.id}>{asset.filename || asset.id}</option>
                      ))}
                    </select>
                  ) : null}
                  <label className={`aigc-asset-upload${assetActionsDisabled ? ' is-disabled' : ''}`}>
                    <span>上传素材</span>
                    <input
                      type="file"
                      accept={slotUploadAccept(slot.media_kind)}
                      disabled={assetActionsDisabled}
                      onChange={(event) => {
                        const file = event.target.files?.[0];
                        event.target.value = '';
                        if (file) {
                          onUploadAsset?.({ targetID, targetRevision: element.revision || 0, slot, file });
                        }
                      }}
                    />
                  </label>
                </div>
                {isGeneratableMediaKind(slot.media_kind) ? (
                  <button
                    className="aigc-regenerate-button"
                    type="button"
                    disabled={assetActionsDisabled}
                    onClick={() => onRegenerateTarget?.({ targetID, targetRevision: element.revision || 0, slot })}
                    aria-label={`重新生成${slot.role || slot.key}`}
                  >
                    <RefreshCw aria-hidden="true" size={14} />
                  </button>
                ) : null}
              </div>
            );
          })}
        </div>
      )}
    </article>
  );
}

function activeStoryboardRevision(storyboard) {
  if (!storyboard) {
    return null;
  }
  if (storyboard.active_revision) {
    return storyboard.active_revision;
  }
  const revisions = Array.isArray(storyboard.revisions) ? storyboard.revisions : Object.values(storyboard.revisions || {});
  return (
    revisions.find((revision) => revision.id === storyboard.pending_revision_id) ||
    revisions.find((revision) => revision.status === 'reviewing') ||
    revisions.find((revision) => revision.id === storyboard.active_revision_id) ||
    revisions.find((revision) => revision.status === 'active') ||
    null
  );
}

function promptSlotValue(slot, element) {
  return slot.prompt || slot.content || slot.text || element.content?.prompts?.[slot.purpose] || slot.prompt_ref || '';
}

function elementSummary(element) {
  const content = element.content || {};
  for (const key of ['summary', 'description', 'scene_description', 'text', 'lyrics', 'dialogue', 'script', 'beat', 'timing', 'timeline', 'narration']) {
    if (content[key] != null && content[key] !== '') {
      return displayContentValue(content[key]);
    }
  }
  const fallback = Object.fromEntries(Object.entries(content).filter(([key, value]) => !['prompts', 'dependencies'].includes(key) && value != null && value !== ''));
  return displayContentValue(fallback);
}

function elementDependencySummary(element, revisionDependencies = []) {
  const targetID = element.id || element.key;
  const edges = (revisionDependencies || []).filter((edge) => edge.from_target_id === targetID || edge.to_target_id === targetID);
  const edgeLabels = edges.map((edge) => {
    const source = `${edge.from_target_id || '?'}${edge.from_slot ? `:${edge.from_slot}` : ''}`;
    const target = `${edge.to_target_id || '?'}${edge.to_slot ? `:${edge.to_slot}` : ''}`;
    return `${source} → ${target}${edge.relation ? `（${edge.relation}）` : ''}`;
  });
  const local = displayContentValue(element.dependencies || element.content?.dependencies || '');
  return [...(local ? [local] : []), ...edgeLabels].join('；');
}

function displayContentValue(value) {
  if (Array.isArray(value)) {
    return value.map((item) => displayContentValue(item)).filter(Boolean).join('、');
  }
  if (value && typeof value === 'object') {
    return Object.entries(value).map(([key, item]) => `${key}: ${displayContentValue(item)}`).join('；');
  }
  return value == null ? '' : String(value).trim();
}

function findBinding(bindings, id) {
  return (bindings || []).find((binding) => binding.id === id) || null;
}

function compatibleAssets(assetMap, slot) {
  const mediaKind = String(slot?.media_kind || '').toLowerCase();
  return Array.from(assetMap?.values?.() || []).filter((asset) => {
    if (asset.availability && asset.availability !== 'available') {
      return false;
    }
    if (['image', 'illustration', 'keyframe'].includes(mediaKind)) {
      return asset.kind === 'image' || asset.kind === 'reference';
    }
    if (mediaKind === 'video') {
      return asset.kind === 'video';
    }
    if (['audio', 'music', 'voice'].includes(mediaKind)) {
      return asset.kind === 'audio';
    }
    if (['text', 'script', 'lyrics'].includes(mediaKind)) {
      return asset.kind === 'text' || asset.kind === 'pdf';
    }
    return false;
  });
}

function slotUploadAccept(mediaKind) {
  const kind = String(mediaKind || '').toLowerCase();
  if (['image', 'illustration', 'keyframe'].includes(kind)) return 'image/*';
  if (kind === 'video') return 'video/*';
  if (['audio', 'music', 'voice'].includes(kind)) return 'audio/*';
  if (['text', 'script', 'lyrics'].includes(kind)) return 'text/*,application/pdf';
  return '';
}

function isGeneratableMediaKind(mediaKind) {
  return ['image', 'illustration', 'keyframe', 'video', 'audio', 'music', 'voice'].includes(String(mediaKind || '').toLowerCase());
}

function aggregateElementStatus(assetSlots, promptSlots) {
  if (assetSlots.some((slot) => slot.status === 'candidate')) {
    return 'reviewing';
  }
  if (assetSlots.some((slot) => slot.status === 'stale') || promptSlots.some((slot) => slot.status === 'stale')) {
    return 'stale';
  }
  if (assetSlots.length > 0 && assetSlots.every((slot) => !slot.required || slot.status === 'active')) {
    return 'ready';
  }
  if (assetSlots.length === 0 && promptSlots.length > 0 && promptSlots.every((slot) => slot.status === 'ready' && promptSlotValue(slot, { content: {} }))) {
    return 'ready';
  }
  return 'draft';
}

// StoryboardSection 渲染左侧故事板分组，并在为空时展示占位文案。
function StoryboardSection({ title, count, children }) {
  return (
    <section className="aigc-story-section">
      <header>
        <strong>{title}</strong>
        <span>{count}</span>
      </header>
      <div>{count > 0 ? children : <p className="aigc-empty">等待生成</p>}</div>
    </section>
  );
}

// InlineEditable 提供故事板字段的内联编辑能力。
function InlineEditable({ editing, path, value, placeholder = '', multiline = false, onStart, onChange, onSave }) {
  const active = editing?.path === path;
  if (!active) {
    return (
      <span
        className="aigc-inline-edit"
        onClick={() => onStart({ path, value })}
        onKeyDown={(event) => {
          if (event.key === 'Enter') {
            onStart({ path, value });
          }
        }}
        role="button"
        tabIndex={0}
      >
        {value || placeholder}
      </span>
    );
  }
  const Input = multiline ? 'textarea' : 'input';
  return (
    <span className="aigc-inline-editor" onClick={(event) => event.stopPropagation()}>
      <Input value={editing.value} rows={multiline ? 2 : undefined} onChange={(event) => onChange({ ...editing, value: event.target.value })} />
      <button type="button" onClick={onSave} aria-label="保存修改">
        <Save aria-hidden="true" size={14} />
      </button>
    </span>
  );
}

// AssetStrip 渲染故事板目标绑定的最多四个素材缩略图。
function AssetStrip({ ids, assetMap }) {
  if (!ids?.length) {
    return null;
  }
  return (
    <div className="aigc-asset-strip">
      {ids.slice(0, 4).map((id) => {
        const asset = assetMap.get(id);
        return <AssetImage asset={asset} fallback={<span>{id.slice(0, 4)}</span>} key={id} />;
      })}
    </div>
  );
}

// AssetImage 渲染素材图片，加载失败时回退到占位内容。
function AssetImage({ asset, fallback = null }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [asset?.url]);
  if (!asset?.url || failed) {
    return fallback;
  }
  return <img src={asset.url} alt="" loading="lazy" onError={() => setFailed(true)} />;
}

function AssetPreview({ asset, fallback = null }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [asset?.url]);
  if (!asset?.url || failed) {
    return fallback;
  }
  if (asset.kind === 'video' || String(asset.mime_type || '').startsWith('video/')) {
    return <video src={asset.url} muted controls preload="metadata" onError={() => setFailed(true)} />;
  }
  if (asset.kind === 'audio' || String(asset.mime_type || '').startsWith('audio/')) {
    return <audio src={asset.url} controls preload="metadata" onError={() => setFailed(true)} />;
  }
  return <img src={asset.url} alt="" loading="lazy" onError={() => setFailed(true)} />;
}

// StatusPill 渲染故事板项或任务的状态标签。
function StatusPill({ status, statusLabel }) {
  const label = statusLabel(status);
  if (!label) {
    return null;
  }
  return <span className={`aigc-status-pill aigc-status-pill--${status}`}>{label}</span>;
}
