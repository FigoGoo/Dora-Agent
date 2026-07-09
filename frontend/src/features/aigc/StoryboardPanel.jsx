import { useState } from 'react';
import { Clapperboard, Film, Image, Music, Save } from 'lucide-react';

// StoryboardPanel 渲染左侧故事板业务组件，和右侧 A2UI 消息卡保持职责隔离。
export function StoryboardPanel({
  storyboard,
  selectedTarget,
  onSelectTarget,
  editing,
  onStartEdit,
  onChangeEdit,
  onSaveEdit,
  assetMap,
  statusLabel
}) {
  const keyElements = storyboard?.key_elements || [];
  const shots = storyboard?.shots || [];
  const audioLayers = storyboard?.audio_layers || [];

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

      <StoryboardSection title="关键元素" count={keyElements.length}>
        {keyElements.map((element, index) => {
          const selected = selectedTarget?.type === 'key_element' && selectedTarget.id === element.key;
          return (
            <button
              className={selected ? 'aigc-story-item is-selected' : 'aigc-story-item'}
              type="button"
              key={element.key}
              onClick={() => onSelectTarget({ type: 'key_element', id: element.key, label: element.name || element.key })}
            >
              <div className="aigc-story-item__icon">
                <Image aria-hidden="true" size={16} />
              </div>
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
            </button>
          );
        })}
      </StoryboardSection>

      <StoryboardSection title="分镜" count={shots.length}>
        {shots.map((shot, index) => {
          const selected = selectedTarget?.type === 'shot' && selectedTarget.id === shot.shot_id;
          return (
            <button
              className={selected ? 'aigc-shot-item is-selected' : 'aigc-shot-item'}
              type="button"
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
              <div className="aigc-shot-preview">
                <AssetImage asset={assetMap.get(shot.keyframe_asset_id)} fallback={<Film aria-hidden="true" size={20} />} />
              </div>
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
            </button>
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
    </section>
  );
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
function InlineEditable({ editing, path, value, multiline = false, onStart, onChange, onSave }) {
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
        {value}
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
  if (!asset?.url || failed) {
    return fallback;
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
