import {
  SKILL_CAPABILITY_FIELDS,
  SKILL_CAPABILITY_LABELS
} from '../skills/skillContract.js';

export function SkillDefinitionReview({ definition, title }) {
  return (
    <section className="skill-review-definition" aria-label={title}>
      <header>
        <h3>{title}</h3>
        <span>{definition.schema_version}</span>
      </header>
      <dl className="skill-review-definition__facts">
        <DefinitionFact label="名称" value={definition.name} />
        <DefinitionFact label="简介" value={definition.summary || '未填写'} />
        <DefinitionFact label="分类" value={definition.category || '未分类'} />
        <DefinitionFact label="标签" value={definition.tags.length ? definition.tags.join('、') : '无'} />
        <DefinitionFact label="输入说明" value={definition.input_description || '未填写'} />
        <DefinitionFact label="输出说明" value={definition.output_description || '未填写'} />
        <DefinitionFact label="调用规则" value={definition.invocation_rules || '未填写'} />
      </dl>

      <section className="skill-review-definition__capabilities" aria-label={`${title}六个能力字段`}>
        <h4>六个能力字段</h4>
        {SKILL_CAPABILITY_FIELDS.map((field) => {
          const capability = definition[field];
          return (
            <article key={field}>
              <strong>{SKILL_CAPABILITY_LABELS[field]}</strong>
              <small>{capability.applicability === 'enabled' ? '适用' : '不适用'}</small>
              <p>{capability.applicability === 'enabled' ? capability.guidance : capability.not_applicable_reason}</p>
            </article>
          );
        })}
      </section>

      <DefinitionList title="示例" empty="无示例">
        {definition.examples.map((example, index) => (
          <article key={`${example.input}-${example.output}`}>
            <strong>示例 {index + 1}</strong>
            <p>输入：{example.input}</p>
            <p>输出：{example.output}</p>
          </article>
        ))}
      </DefinitionList>
      <DefinitionList title="开场提示" empty="无开场提示">
        {definition.starter_prompts.map((prompt) => <p key={prompt}>{prompt}</p>)}
      </DefinitionList>

      <section className="skill-review-definition__market" aria-label={`${title}市场信息`}>
        <h4>市场信息</h4>
        <p>{definition.market_listing.detail || '未填写市场详情'}</p>
        <small>版权：{definition.market_listing.copyright_notice || '未填写'}</small>
        <small>用户须知：{definition.market_listing.user_notice || '未填写'}</small>
        <small>公共 Tool：{definition.public_tool_refs.length === 0 ? '无' : definition.public_tool_refs.join('、')}</small>
      </section>
    </section>
  );
}

function DefinitionFact({ label, value }) {
  return <div><dt>{label}</dt><dd>{value}</dd></div>;
}

function DefinitionList({ title, empty, children }) {
  const items = Array.isArray(children) ? children : children ? [children] : [];
  return (
    <section className="skill-review-definition__list">
      <h4>{title}</h4>
      {items.length ? items : <p>{empty}</p>}
    </section>
  );
}
