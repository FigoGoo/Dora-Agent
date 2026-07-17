import { useId } from 'react';

const ELEMENT_TYPE_LABELS = Object.freeze({
  scene: '场景',
  shot: '镜头',
  narration: '旁白',
  caption: '字幕',
  audio: '音频'
});
const SLOT_TYPE_LABELS = Object.freeze({
  image: '图片',
  video: '视频',
  audio: '音频',
  voiceover: '配音',
  caption: '字幕'
});

// StoryboardPreviewCard 只渲染严格解析后的隔离 JSON Draft，不解释 Markdown、HTML 或生产 Storyboard 语义。
export function StoryboardPreviewCard({ preview }) {
  const failureTitleID = `storyboard-preview-failure-${useId()}`;
  if (!preview || preview.kind !== 'plan_storyboard_preview') return null;
  if (preview.status === 'failed') {
    return (
      <article
        className="storyboard-preview-card storyboard-preview-card--failed"
        aria-labelledby={failureTitleID}
        data-storyboard-preview-status="failed"
      >
        <p>开发预览 · 隔离 JSON Draft · 未激活/未扣费</p>
        <h3 id={failureTitleID}>故事板规划未完成</h3>
        <p role="alert">{preview.summary}</p>
        <p>失败类型：{preview.failureKind === 'runtime' ? '运行时失败' : '规划结果失败'}</p>
        {preview.retryable ? <p>可以稍后重新显式提交。</p> : null}
        <footer>结果码：{preview.resultCode}</footer>
      </article>
    );
  }
  if (preview.status !== 'completed') return null;
  const sectionByKey = new Map(preview.sections.map((section) => [section.key, section]));
  const slotsByElement = groupSlotsByElement(preview.slots);
  return (
    <article
      className="storyboard-preview-card"
      aria-labelledby={`storyboard-preview-title-${preview.storyboardPreviewID}`}
      data-storyboard-preview-id={preview.storyboardPreviewID}
      data-storyboard-preview-version={preview.version}
      data-storyboard-preview-status="completed"
    >
      <header>
        <p>开发预览 · 隔离 JSON Draft · 未激活/未扣费</p>
        <h3 id={`storyboard-preview-title-${preview.storyboardPreviewID}`}>{preview.title}</h3>
        <span>v{preview.version}</span>
      </header>
      <p>{preview.summary}</p>
      <dl>
        <div><dt>Creation Spec</dt><dd>Draft v{preview.creationSpecRef.version}</dd></div>
        <div><dt>章节</dt><dd>{preview.sections.length}</dd></div>
        <div><dt>元素</dt><dd>{preview.elements.length}</dd></div>
        <div><dt>槽位</dt><dd>{preview.slots.length}</dd></div>
      </dl>

      <section aria-labelledby={`storyboard-preview-sections-${preview.storyboardPreviewID}`}>
        <h4 id={`storyboard-preview-sections-${preview.storyboardPreviewID}`}>故事板章节</h4>
        <ol>
          {preview.sections.map((section) => (
            <li key={section.key}>
              <strong>{section.title}</strong>
              <p>{section.objective}</p>
            </li>
          ))}
        </ol>
      </section>

      <section aria-labelledby={`storyboard-preview-elements-${preview.storyboardPreviewID}`}>
        <h4 id={`storyboard-preview-elements-${preview.storyboardPreviewID}`}>规划元素</h4>
        <ol>
          {preview.elements.map((element) => (
            <li key={element.key}>
              <div>
                <strong>{element.order}. {element.title}</strong>
                <span>{ELEMENT_TYPE_LABELS[element.elementType]}</span>
              </div>
              <p>章节：{sectionByKey.get(element.sectionKey).title}</p>
              <p>{element.narrativePurpose}</p>
              <p>时长：{element.durationSeconds} 秒</p>
              {element.dependencyKeys.length > 0 ? (
                <p>依赖：{element.dependencyKeys.join('、')}</p>
              ) : null}
              <SlotList slots={slotsByElement.get(element.key) || []} />
            </li>
          ))}
        </ol>
      </section>
      <footer>结果码：{preview.resultCode} · 最近更新：{preview.updatedAt}</footer>
    </article>
  );
}

function SlotList({ slots }) {
  if (slots.length === 0) return <p>无需媒体槽位</p>;
  return (
    <ul aria-label="元素槽位">
      {slots.map((slot) => (
        <li key={slot.key}>
          <strong>{SLOT_TYPE_LABELS[slot.slotType]}</strong>
          <span>：{slot.purpose}{slot.required ? '（必需）' : '（可选）'}</span>
        </li>
      ))}
    </ul>
  );
}

function groupSlotsByElement(slots) {
  return slots.reduce((result, slot) => {
    const current = result.get(slot.elementKey) || [];
    current.push(slot);
    result.set(slot.elementKey, current);
    return result;
  }, new Map());
}
