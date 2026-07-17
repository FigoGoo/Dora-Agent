const DELIVERABLE_LABELS = Object.freeze({
  video: '视频',
  image_set: '图片组',
  audio: '音频',
  mixed: '混合交付'
});

// CreationSpecCard 只渲染严格解析后的安全文本投影，不解释 Markdown 或服务端 HTML。
export function CreationSpecCard({ preview }) {
  if (!preview || preview.kind !== 'card') return null;
  return (
    <article
      className="creation-spec-card"
      aria-labelledby={`creation-spec-title-${preview.creationSpecID}`}
      data-creation-spec-id={preview.creationSpecID}
      data-creation-spec-version={preview.version}
    >
      <header className="creation-spec-card__header">
        <div>
          <p className="creation-spec-card__eyebrow">开发预览 · Draft · 未扣费/未激活</p>
          <h3 id={`creation-spec-title-${preview.creationSpecID}`}>{preview.title}</h3>
        </div>
        <span className="creation-spec-card__version">v{preview.version}</span>
      </header>

      <dl className="creation-spec-card__summary">
        <div><dt>交付类型</dt><dd>{DELIVERABLE_LABELS[preview.deliverableType]}</dd></div>
        <div><dt>受众</dt><dd>{preview.audience || '未指定'}</dd></div>
        <div><dt>语言</dt><dd>{preview.locale}</dd></div>
      </dl>

      <section aria-labelledby={`creation-spec-goal-${preview.creationSpecID}`}>
        <h4 id={`creation-spec-goal-${preview.creationSpecID}`}>创作目标</h4>
        <p>{preview.goal}</p>
      </section>

      <section aria-labelledby={`creation-spec-phases-${preview.creationSpecID}`}>
        <h4 id={`creation-spec-phases-${preview.creationSpecID}`}>执行阶段</h4>
        <ol className="creation-spec-card__phases">
          {preview.phases.map((phase) => (
            <li key={phase.key}>
              <strong>{phase.title}</strong>
              <p>{phase.objective}</p>
              <small>输出：{phase.output}</small>
            </li>
          ))}
        </ol>
      </section>

      <CardList
        id={`creation-spec-constraints-${preview.creationSpecID}`}
        title="约束"
        items={preview.constraints}
        emptyText="暂无额外约束"
      />
      <CardList
        id={`creation-spec-acceptance-${preview.creationSpecID}`}
        title="验收条件"
        items={preview.acceptanceCriteria}
      />
      <footer>最近更新：{preview.updatedAt}</footer>
    </article>
  );
}

function CardList({ id, title, items, emptyText = '' }) {
  return (
    <section aria-labelledby={id}>
      <h4 id={id}>{title}</h4>
      {items.length === 0 ? <p>{emptyText}</p> : (
        <ul>{items.map((item) => <li key={item}>{item}</li>)}</ul>
      )}
    </section>
  );
}
