import { Plus } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { listProjects } from './projectListApi.js';

const LIFECYCLE_LABELS = Object.freeze({ active: '进行中', archived: '已归档' });

export function ProjectsPage({ onIntent = () => {}, loadProjects = listProjects, onNavigate = navigate }) {
  const [state, setState] = useState({
    kind: 'loading', items: [], nextAfter: null, error: null, pageError: null
  });
  const requestRef = useRef({ generation: 0, controller: null });

  const loadPage = useCallback(async ({ after = null, append = false } = {}) => {
    requestRef.current.controller?.abort();
    const controller = new AbortController();
    const generation = requestRef.current.generation + 1;
    requestRef.current = { generation, controller };
    setState((current) => ({
      ...current,
      kind: append ? 'loading_more' : 'loading',
      error: null,
      pageError: null,
      ...(append ? {} : { items: [], nextAfter: null })
    }));
    try {
      const result = await loadProjects({ after, signal: controller.signal });
      if (requestRef.current.generation !== generation || controller.signal.aborted) return;
      setState((current) => ({
        kind: 'ready',
        items: append ? mergeProjects(current.items, result.items) : result.items,
        nextAfter: result.nextAfter,
        error: null,
        pageError: null
      }));
    } catch (error) {
      if (requestRef.current.generation !== generation || controller.signal.aborted || error?.name === 'AbortError') return;
      setState((current) => append
        ? { ...current, kind: 'ready', pageError: publicError(error) }
        : { kind: 'error', items: [], nextAfter: null, error: publicError(error), pageError: null });
    }
  }, [loadProjects]);

  useEffect(() => {
    void loadPage();
    return () => {
      requestRef.current.controller?.abort();
      requestRef.current.generation += 1;
    };
  }, [loadPage]);

  return (
    <section className="mock-page projects-page" aria-labelledby="projects-title">
      <div className="project-gallery" aria-label="项目列表">
        <button
          className="project-tile project-tile--create"
          data-testid="project-card"
          type="button"
          aria-label="新建项目"
          onClick={() => onIntent('新建项目', '登录后会从 Prompt 创建新项目。')}
        >
          <span className="project-tile__cover">
            <span className="project-tile__empty">
              <span className="project-tile__plus"><Plus aria-hidden="true" size={22} /></span>
              <strong>创建新项目</strong>
            </span>
          </span>
          <span className="project-tile__body"><strong>新建项目</strong><small>开启您的创作之旅</small></span>
        </button>

        {state.kind === 'loading' ? <p role="status">正在加载项目…</p> : null}
        {state.kind === 'error' ? (
          <section className="skill-state-panel" aria-label="项目列表加载失败">
            <p role="alert">{state.error.message}</p>
            {state.error.requestID ? <small>请求标识：{state.error.requestID}</small> : null}
            <button type="button" className="secondary-button" onClick={() => loadPage()}>重试</button>
          </section>
        ) : null}
        {state.kind === 'ready' && state.items.length === 0 ? <p role="status">还没有项目，可以从新建项目开始。</p> : null}

        {state.items.map((item) => (
          <button
            className="project-tile"
            data-testid="project-card"
            type="button"
            aria-label={`继续创作 ${item.title}`}
            key={item.projectID}
            onClick={() => onNavigate(item.workspaceRef)}
          >
            <span className="project-tile__cover">
              <span className="project-tile__empty"><strong>{LIFECYCLE_LABELS[item.lifecycleStatus]}</strong></span>
            </span>
            <span className="project-tile__body">
              <strong>{item.title}</strong>
              <small>最后编辑于 {formatUpdatedAt(item.updatedAt)}</small>
            </span>
          </button>
        ))}
      </div>

      {state.pageError ? (
        <p role="alert">{state.pageError.message} <button type="button" onClick={() => loadPage({ after: state.nextAfter, append: true })}>重试</button></p>
      ) : null}
      {state.nextAfter && !state.pageError ? (
        <button
          type="button"
          className="secondary-button"
          disabled={state.kind === 'loading_more'}
          onClick={() => loadPage({ after: state.nextAfter, append: true })}
        >
          {state.kind === 'loading_more' ? '正在加载…' : '加载更多'}
        </button>
      ) : null}
    </section>
  );
}

function mergeProjects(current, incoming) {
  const merged = new Map(current.map((item) => [item.projectID, item]));
  incoming.forEach((item) => {
    if (!merged.has(item.projectID)) merged.set(item.projectID, item);
  });
  return [...merged.values()];
}

function publicError(error) {
  return {
    message: String(error?.message || '项目列表暂时无法读取，请稍后重试。'),
    requestID: String(error?.requestID || '')
  };
}

function formatUpdatedAt(value) {
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'
  }).format(new Date(value));
}

function navigate(path) {
  window.history.pushState({}, '', path);
  window.dispatchEvent(new Event('dora:navigate'));
}
