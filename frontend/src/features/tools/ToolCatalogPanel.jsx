import { useCallback, useEffect, useRef, useState } from 'react';
import { loadToolCatalog } from './toolCatalogApi.js';

const INITIAL_STATE = Object.freeze({ kind: 'loading', items: [], requestID: '', error: null });

export function ToolCatalogPanel({ sessionID, loadCatalog = loadToolCatalog }) {
  const [state, setState] = useState(INITIAL_STATE);
  const generationRef = useRef(0);
  const requestRef = useRef(null);

  const load = useCallback(() => {
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    const generation = ++generationRef.current;
    setState({ kind: 'loading', items: [], requestID: '', error: null });

    Promise.resolve()
      .then(() => loadCatalog(sessionID, { signal: controller.signal }))
      .then((catalog) => {
        if (generation !== generationRef.current || controller.signal.aborted) return;
        setState({
          kind: 'ready',
          items: catalog.items,
          requestID: catalog.requestID,
          error: null
        });
      })
      .catch((error) => {
        if (generation !== generationRef.current || controller.signal.aborted || error?.name === 'AbortError') return;
        setState({ kind: 'error', items: [], requestID: '', error: publicCatalogError(error) });
      })
      .finally(() => {
        if (requestRef.current === controller) requestRef.current = null;
      });
  }, [loadCatalog, sessionID]);

  useEffect(() => {
    load();
    return () => {
      generationRef.current += 1;
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, [load]);

  return (
    <section
      id="workspace-toolbox"
      className="tool-catalog-panel"
      aria-labelledby="tool-catalog-title"
      data-tool-catalog-state={state.kind}
      tabIndex={-1}
    >
      <header>
        <div>
          <h2 id="tool-catalog-title">工具目录</h2>
          <p>当前仅展示已评审的目录定义，不提供选择或运行入口。</p>
        </div>
      </header>

      {state.kind === 'loading' ? <p role="status">正在加载工具目录…</p> : null}
      {state.kind === 'error' ? (
        <section className="tool-catalog-panel__error">
          <p role="alert">工具目录暂时不可用，请重试。</p>
          {state.error.requestID ? <small>请求 ID：{state.error.requestID}</small> : null}
          <button type="button" className="secondary-button" onClick={load}>重试加载工具目录</button>
        </section>
      ) : null}
      {state.kind === 'ready' ? (
        <ol className="tool-catalog-panel__list" aria-label="不可用工具定义">
          {state.items.map((item) => (
            <li
              key={item.toolKey}
              className="tool-catalog-panel__item"
              aria-disabled="true"
              data-tool-key={item.toolKey}
              data-tool-order={item.order}
              data-tool-availability={item.availability}
            >
              <span className="tool-catalog-panel__order" aria-hidden="true">{item.order}</span>
              <span>
                <strong>{item.displayName}</strong>
                <small>设计评审中</small>
              </span>
              <span className="tool-catalog-panel__availability">不可用</span>
            </li>
          ))}
        </ol>
      ) : null}
    </section>
  );
}

function publicCatalogError(error) {
  return {
    code: String(error?.code || 'TOOL_CATALOG_UNAVAILABLE'),
    status: Number(error?.status) || 0,
    requestID: String(error?.requestID || ''),
    retryable: true
  };
}
