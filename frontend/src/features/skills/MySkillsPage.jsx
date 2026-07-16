import { Plus } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { listOwnerSkills } from './skillApi.js';

const CONTENT_STATUS_LABELS = Object.freeze({ draft: '草稿', published: '已发布' });
const REVIEW_STATUS_LABELS = Object.freeze({
  reviewing: '审核中', approved: '审核通过', rejected: '审核驳回', withdrawn: '已撤回'
});
const GOVERNANCE_STATUS_LABELS = Object.freeze({ active: '可用', suspended: '已暂停', offline: '已下架' });

export function MySkillsPage({ loadSkills = listOwnerSkills, onNavigate = navigate }) {
  const [state, setState] = useState({ kind: 'loading', items: [], nextCursor: null, error: null });
  const generationRef = useRef(0);
  const requestRef = useRef(null);

  const load = useCallback(async ({ cursor = null, append = false } = {}) => {
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    const generation = ++generationRef.current;
    setState((current) => ({
      ...current,
      kind: append ? 'loading_more' : 'loading',
      error: null
    }));
    try {
      const result = await loadSkills({ cursor, signal: controller.signal });
      if (generation !== generationRef.current || controller.signal.aborted) return;
      setState((current) => ({
        kind: 'ready',
        items: append ? mergeSkills(current.items, result.items) : result.items,
        nextCursor: result.nextCursor,
        error: null
      }));
    } catch (error) {
      if (generation !== generationRef.current || controller.signal.aborted || error?.name === 'AbortError') return;
      setState((current) => ({ ...current, kind: 'error', error: publicError(error) }));
    } finally {
      if (requestRef.current === controller) requestRef.current = null;
    }
  }, [loadSkills]);

  useEffect(() => {
    void load();
    return () => {
      generationRef.current += 1;
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, [load]);

  const published = state.items.filter((skill) => skill.contentStatus === 'published');
  const drafts = state.items.filter((skill) => skill.contentStatus === 'draft');

  return (
    <section className="owner-skills-page" aria-labelledby="my-skills-section-title">
      <header className="owner-skills-page__header">
        <div>
          <h2 id="my-skills-section-title">我的 Skill</h2>
          <p>管理草稿、当前发布内容与审核信息；这里不会展示版本明细。</p>
        </div>
        <button className="skills-page__create" type="button" onClick={() => onNavigate('/my/skills/new')}>
          <Plus aria-hidden="true" size={18} />
          <span>创建 Skill</span>
        </button>
      </header>

      {state.kind === 'loading' ? <p role="status">正在加载我的 Skill…</p> : null}
      {state.kind === 'error' ? (
        <section className="skill-state-panel">
          <p role="alert">{state.error.message}</p>
          <button type="button" className="secondary-button" onClick={() => load()}>重试</button>
        </section>
      ) : null}
      {state.kind !== 'loading' && state.kind !== 'error' && state.items.length === 0 ? (
        <section className="skill-state-panel">
          <h3>还没有 Skill</h3>
          <p>创建第一个结构化草稿后，它会出现在这里。</p>
        </section>
      ) : null}

      {published.length > 0 ? <OwnerSkillGroup title="已发布" items={published} onNavigate={onNavigate} /> : null}
      {drafts.length > 0 ? <OwnerSkillGroup title="草稿" items={drafts} onNavigate={onNavigate} /> : null}

      {state.nextCursor ? (
        <button
          type="button"
          className="secondary-button"
          disabled={state.kind === 'loading_more'}
          onClick={() => load({ cursor: state.nextCursor, append: true })}
        >
          {state.kind === 'loading_more' ? '正在加载…' : '加载更多'}
        </button>
      ) : null}
    </section>
  );
}

function OwnerSkillGroup({ title, items, onNavigate }) {
  return (
    <section className="owner-skill-group" aria-labelledby={`owner-skill-group-${title}`}>
      <h3 id={`owner-skill-group-${title}`}>{title}</h3>
      <div className="skills-page__grid">
        {items.map((skill) => (
          <article className="skill-library-card owner-skill-card" data-testid="owner-skill-card" key={skill.skillID}>
            <div className="skill-library-card__content">
              <span className="skill-library-card__author">{skill.definition.category || '未分类'}</span>
              <div className="skill-library-card__title-row"><h4>{skill.definition.name}</h4></div>
              <p>{skill.definition.summary || '尚未填写简介'}</p>
              <div className="owner-skill-card__statuses">
                <strong>{CONTENT_STATUS_LABELS[skill.contentStatus]}</strong>
                {skill.hasUnpublishedChanges ? <span>有未发布修改</span> : null}
                {skill.reviewStatus ? <span>{REVIEW_STATUS_LABELS[skill.reviewStatus]}</span> : null}
                <span>{GOVERNANCE_STATUS_LABELS[skill.governanceStatus]}</span>
              </div>
              {skill.reviewReasonCode ? <p role="note">审核原因：{skill.reviewReasonCode}</p> : null}
              {skill.allowedActions.includes('edit_draft') ? (
                <button
                  type="button"
                  className="secondary-button"
                  onClick={() => onNavigate(`/my/skills/${encodeURIComponent(skill.skillID)}/edit`)}
                >
                  编辑草稿
                </button>
              ) : null}
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}

function mergeSkills(current, incoming) {
  const byID = new Map(current.map((item) => [item.skillID, item]));
  incoming.forEach((item) => byID.set(item.skillID, item));
  return [...byID.values()];
}

function publicError(error) {
  return {
    message: String(error?.message || '暂时无法加载我的 Skill。'),
    code: String(error?.code || 'SKILL_LIST_UNAVAILABLE'),
    requestID: String(error?.requestID || '')
  };
}

function navigate(path) {
  window.history.pushState({}, '', path);
  window.dispatchEvent(new Event('dora:navigate'));
}
