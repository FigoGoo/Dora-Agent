import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft, Blocks, RefreshCw } from 'lucide-react';
import { getSkillMarketDetail } from './skillMarketApi.js';
import { SKILL_MARKET_CAPABILITY_LABELS } from './skillMarketContract.js';

export function SkillMarketDetailPage({
  skillID,
  loadSkill = getSkillMarketDetail,
  onNavigate = navigate
}) {
  const [status, setStatus] = useState('loading');
  const [skill, setSkill] = useState(null);
  const [error, setError] = useState(null);
  const requestRef = useRef({ generation: 0, controller: null });

  const load = useCallback(async () => {
    requestRef.current.controller?.abort();
    const controller = new AbortController();
    const generation = requestRef.current.generation + 1;
    requestRef.current = { generation, controller };
    setStatus('loading');
    setError(null);
    setSkill(null);
    try {
      const result = await loadSkill(skillID, { signal: controller.signal });
      if (requestRef.current.generation !== generation || controller.signal.aborted) return;
      setSkill(result.skill);
      setStatus('ready');
    } catch (requestError) {
      if (requestRef.current.generation !== generation || controller.signal.aborted) return;
      setError(requestError);
      setStatus(requestError?.status === 404 ? 'not-found' : 'error');
    }
  }, [loadSkill, skillID]);

  useEffect(() => {
    load();
    return () => {
      requestRef.current.controller?.abort();
      requestRef.current.generation += 1;
    };
  }, [load]);

  if (status === 'loading') {
    return (
      <section className="skill-state-panel" aria-label="正在加载 Skill 详情">
        <RefreshCw className="skill-market-spinner" aria-hidden="true" size={38} />
        <h2>正在加载 Skill 详情</h2>
        <p role="status">正在读取当前公开发布信息。</p>
      </section>
    );
  }

  if (status === 'not-found') {
    return (
      <section className="skill-state-panel" aria-label="Skill 暂不可用">
        <Blocks aria-hidden="true" size={44} />
        <h2>Skill 暂不可用</h2>
        <p role="alert">该 Skill 不存在、尚未发布或当前已停止公开。</p>
        {error?.requestID ? <small>请求标识：{error.requestID}</small> : null}
        <button type="button" className="secondary-button" onClick={() => onNavigate('/skills')}>返回 Skill 市场</button>
      </section>
    );
  }

  if (status === 'error') {
    return (
      <section className="skill-state-panel" aria-label="Skill 详情加载失败">
        <Blocks aria-hidden="true" size={44} />
        <h2>Skill 详情加载失败</h2>
        <p role="alert">{error?.message || '公开 Skill 暂时无法读取，请稍后重试。'}</p>
        {error?.requestID ? <small>请求标识：{error.requestID}</small> : null}
        <div className="skills-page__actions">
          <button type="button" className="secondary-button" onClick={load}>重试</button>
          <button type="button" className="secondary-button" onClick={() => onNavigate('/skills')}>返回 Skill 市场</button>
        </div>
      </section>
    );
  }

  return (
    <article className="skill-market-detail" aria-labelledby="skill-market-detail-title">
      <button className="skill-market-detail__back" type="button" onClick={() => onNavigate('/skills')}>
        <ArrowLeft aria-hidden="true" size={17} />
        返回 Skill 市场
      </button>
      <header className="skill-market-detail__hero">
        <div className="skill-market-detail__icon" aria-hidden="true"><Blocks size={42} /></div>
        <div>
          <span className="skill-market-page__eyebrow">基础预览</span>
          <h2 id="skill-market-detail-title">{skill.name}</h2>
          <p>{skill.summary || '发布者暂未填写摘要。'}</p>
          <div className="skill-market-detail__publisher">
            <span>发布者：{skill.publisher.displayName}</span>
            <time dateTime={skill.publishedAt}>{formatPublishedAt(skill.publishedAt)}发布</time>
          </div>
        </div>
      </header>

      <p className="skill-market-page__scope" role="note">
        当前仅提供公开信息预览；搜索、收藏、费用、指标和跨发布者使用尚未开放。
      </p>

      <div className="skill-market-detail__grid">
        <section>
          <h3>公开详情</h3>
          <p>{skill.marketDetail || '发布者暂未填写详细说明。'}</p>
        </section>
        <section>
          <h3>输入说明</h3>
          <p>{skill.inputDescription || '暂无输入说明。'}</p>
        </section>
        <section>
          <h3>输出说明</h3>
          <p>{skill.outputDescription || '暂无输出说明。'}</p>
        </section>
        <section>
          <h3>声明能力</h3>
          <div className="skill-market-detail__chips">
            {skill.declaredCapabilityKeys.length
              ? skill.declaredCapabilityKeys.map((key) => <span key={key}>{SKILL_MARKET_CAPABILITY_LABELS[key]}</span>)
              : <span>未声明能力</span>}
          </div>
          <small>能力声明仅表示发布内容覆盖的领域，不表示当前可执行或已开放使用。</small>
        </section>
        <section>
          <h3>标签与分类</h3>
          <div className="skill-market-detail__chips">
            {skill.category ? <span>{skill.category}</span> : null}
            {skill.tags.map((tag) => <span key={tag}>{tag}</span>)}
            {!skill.category && skill.tags.length === 0 ? <span>未填写</span> : null}
          </div>
        </section>
      </div>

      {skill.examples.length ? (
        <section className="skill-market-detail__section">
          <h3>示例</h3>
          <ol className="skill-market-detail__examples">
            {skill.examples.map((example) => (
              <li key={`${example.input}\u0000${example.output}`}>
                <strong>输入</strong><p>{example.input}</p>
                <strong>输出</strong><p>{example.output}</p>
              </li>
            ))}
          </ol>
        </section>
      ) : null}

      {skill.starterPrompts.length ? (
        <section className="skill-market-detail__section">
          <h3>入门提示</h3>
          <ul>{skill.starterPrompts.map((prompt) => <li key={prompt}>{prompt}</li>)}</ul>
        </section>
      ) : null}

      <div className="skill-market-detail__grid">
        <section><h3>版权说明</h3><p>{skill.copyrightNotice || '暂无版权说明。'}</p></section>
        <section><h3>用户须知</h3><p>{skill.userNotice || '暂无用户须知。'}</p></section>
      </div>
    </article>
  );
}

function formatPublishedAt(value) {
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  }).format(new Date(value));
}

function navigate(path) {
  window.history.pushState({}, '', path);
  window.dispatchEvent(new Event('dora:navigate'));
}
