import { useCallback, useEffect, useRef, useState } from 'react';
import { Blocks, ChevronRight, Plus, RefreshCw } from 'lucide-react';
import { listSkillMarket } from './skillMarketApi.js';
import { SKILL_MARKET_CAPABILITY_LABELS } from './skillMarketContract.js';

export function SkillsPage({
  isLoggedIn,
  onLogin,
  onNavigate = navigate,
  loadMarket = listSkillMarket
}) {
  const [items, setItems] = useState([]);
  const [nextCursor, setNextCursor] = useState(null);
  const [status, setStatus] = useState('loading');
  const [error, setError] = useState(null);
  const [pageLoading, setPageLoading] = useState(false);
  const [pageError, setPageError] = useState(null);
  const requestRef = useRef({ generation: 0, controller: null });

  const loadPage = useCallback(async (cursor, append) => {
    requestRef.current.controller?.abort();
    const controller = new AbortController();
    const generation = requestRef.current.generation + 1;
    requestRef.current = { generation, controller };

    if (append) {
      setPageLoading(true);
      setPageError(null);
    } else {
      setStatus('loading');
      setError(null);
      setPageError(null);
      setItems([]);
      setNextCursor(null);
    }

    try {
      const result = await loadMarket({ cursor, signal: controller.signal });
      if (requestRef.current.generation !== generation || controller.signal.aborted) return;
      setItems((current) => append ? mergeBySkillID(current, result.items) : result.items);
      setNextCursor(result.nextCursor);
      setStatus('ready');
    } catch (requestError) {
      if (requestRef.current.generation !== generation || controller.signal.aborted) return;
      if (append) setPageError(requestError);
      else {
        setError(requestError);
        setStatus('error');
      }
    } finally {
      if (requestRef.current.generation === generation) setPageLoading(false);
    }
  }, [loadMarket]);

  useEffect(() => {
    loadPage(null, false);
    return () => {
      requestRef.current.controller?.abort();
      requestRef.current.generation += 1;
    };
  }, [loadPage]);

  return (
    <section className="skills-page skill-market-page" aria-labelledby="skill-market-list-title">
      <div className="skills-page__bar skill-market-page__header">
        <div>
          <span className="skill-market-page__eyebrow">基础预览</span>
          <h2 id="skill-market-list-title">Skill 市场</h2>
          <p>浏览平台审核发布的 Skill 公开信息。</p>
        </div>
        <div className="skills-page__actions">
          <button className="secondary-button" type="button" onClick={() => (
            isLoggedIn
              ? onNavigate('/my/skills')
              : onLogin('查看我的 Skill', '登录后管理自己创建和发布的 Skill。', 'mySkills')
          )}>我的 Skill</button>
          <button className="skills-page__create" type="button" onClick={() => (
            isLoggedIn
              ? onNavigate('/my/skills/new')
              : onLogin('创建 Skill', '登录后进入结构化 Skill Builder。', 'mySkills')
          )}>
            <Plus aria-hidden="true" size={18} />
            <span>创建 Skill</span>
          </button>
        </div>
      </div>

      <p className="skill-market-page__scope" role="note">
        当前仅提供基础预览；搜索、收藏、费用、指标和跨发布者使用尚未开放。
      </p>

      {status === 'loading' ? (
        <section className="skill-state-panel" aria-label="正在加载 Skill 市场">
          <RefreshCw className="skill-market-spinner" aria-hidden="true" size={38} />
          <h3>正在加载 Skill 市场</h3>
          <p role="status">正在读取最新发布的公开 Skill。</p>
        </section>
      ) : null}

      {status === 'error' ? (
        <MarketErrorPanel error={error} onRetry={() => loadPage(null, false)} />
      ) : null}

      {status === 'ready' && items.length === 0 ? (
        <section className="skill-state-panel" aria-label="Skill 市场为空">
          <Blocks aria-hidden="true" size={44} />
          <h3>暂时没有公开 Skill</h3>
          <p role="status">通过审核并处于公开状态的 Skill 会显示在这里。</p>
        </section>
      ) : null}

      {status === 'ready' && items.length > 0 ? (
        <>
          <div className="skills-page__grid" aria-label="公开 Skill 列表">
            {items.map((skill) => (
              <MarketSkillCard key={skill.skillID} skill={skill} onNavigate={onNavigate} />
            ))}
          </div>
          <div className="skill-market-page__pagination">
            {pageError ? (
              <div className="skill-market-page__page-error" role="alert">
                <span>{errorMessage(pageError)}</span>
                <button type="button" className="secondary-button" onClick={() => loadPage(nextCursor, true)}>重试加载下一页</button>
              </div>
            ) : null}
            {nextCursor && !pageError ? (
              <button
                type="button"
                className="secondary-button"
                disabled={pageLoading}
                onClick={() => loadPage(nextCursor, true)}
              >
                {pageLoading ? '正在加载下一页…' : '加载更多'}
              </button>
            ) : null}
            {!nextCursor && !pageError ? <p role="status">已显示全部公开 Skill</p> : null}
          </div>
        </>
      ) : null}
    </section>
  );
}

function MarketSkillCard({ skill, onNavigate }) {
  return (
    <article className="skill-library-card skill-market-card" data-testid="skill-market-card">
      <div className="skill-market-card__visual" aria-hidden="true">
        <Blocks size={38} />
        <span>基础预览</span>
      </div>
      <div className="skill-library-card__content">
        <div className="skill-library-card__author">发布者：{skill.publisher.displayName}</div>
        <div className="skill-library-card__title-row">
          <h3>{skill.name}</h3>
          {skill.category ? <span>{skill.category}</span> : null}
        </div>
        <p>{skill.summary || '发布者暂未填写摘要。'}</p>
        {skill.tags.length ? (
          <ul className="skill-market-tags" aria-label="标签">
            {skill.tags.map((tag) => <li key={tag}>{tag}</li>)}
          </ul>
        ) : null}
        <div className="skill-market-card__capabilities" aria-label="声明能力">
          {skill.declaredCapabilityKeys.length
            ? skill.declaredCapabilityKeys.map((key) => <span key={key}>{SKILL_MARKET_CAPABILITY_LABELS[key]}</span>)
            : <span>未声明能力</span>}
        </div>
        <div className="skill-market-card__meta">
          <time dateTime={skill.publishedAt}>{formatPublishedAt(skill.publishedAt)}发布</time>
          <button type="button" onClick={() => onNavigate(`/skills/${skill.skillID}`)}>
            查看 {skill.name} 详情
            <ChevronRight aria-hidden="true" size={16} />
          </button>
        </div>
      </div>
    </article>
  );
}

function MarketErrorPanel({ error, onRetry }) {
  return (
    <section className="skill-state-panel" aria-label="Skill 市场加载失败">
      <Blocks aria-hidden="true" size={44} />
      <h3>Skill 市场加载失败</h3>
      <p role="alert">{errorMessage(error)}</p>
      {error?.requestID ? <small>请求标识：{error.requestID}</small> : null}
      <button type="button" className="secondary-button" onClick={onRetry}>重试</button>
    </section>
  );
}

function mergeBySkillID(current, incoming) {
  const merged = new Map(current.map((skill) => [skill.skillID, skill]));
  incoming.forEach((skill) => {
    if (!merged.has(skill.skillID)) merged.set(skill.skillID, skill);
  });
  return [...merged.values()];
}

function errorMessage(error) {
  return error?.message || '公开 Skill 暂时无法读取，请稍后重试。';
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
