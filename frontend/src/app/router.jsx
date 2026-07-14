import { useEffect, useState } from 'react';
import { AgentWorkspacePage } from '../pages/AgentWorkspacePage.jsx';
import { ClientAppPage } from '../pages/ClientAppPage.jsx';
import { ProjectWorkspacePage } from '../features/projects/ProjectWorkspacePage.jsx';
import { AUTH_SESSION_STATUS, useAuthSession } from '../platform/auth/authSession.js';
import { matchProjectWorkspacePath, normalizePath, PUBLIC_PAGES, WORKSPACE_ROUTE, getPageFromPath } from './routes.js';

export function AppRouter() {
  const [currentPath, setCurrentPath] = useState(() => (typeof window === 'undefined' ? '/' : normalizePath(window.location.pathname)));
  const auth = useAuthSession();
  const projectWorkspace = matchProjectWorkspacePath(currentPath);

  useEffect(() => {
    function syncRoute() {
      setCurrentPath(normalizePath(window.location.pathname));
    }
    window.addEventListener('popstate', syncRoute);
    window.addEventListener('dora:navigate', syncRoute);
    return () => {
      window.removeEventListener('popstate', syncRoute);
      window.removeEventListener('dora:navigate', syncRoute);
    };
  }, []);

  if (currentPath === WORKSPACE_ROUTE) {
    return import.meta.env.DEV
      ? <div data-legacy-demo-workspace="true"><AgentWorkspacePage /></div>
      : <main className="route-state"><h1>旧工作台已停用</h1><p>请从正式项目入口进入工作台。</p></main>;
  }

  const page = getPageFromPath(currentPath);
  const isProtected = Boolean(projectWorkspace) || !PUBLIC_PAGES.has(page);
  if (isProtected && auth.status !== AUTH_SESSION_STATUS.AUTHENTICATED) {
    return <ProtectedRouteState auth={auth} />;
  }
  if (projectWorkspace) {
    return <ProjectWorkspacePage key={projectWorkspace.projectID} projectID={projectWorkspace.projectID} />;
  }

  return <ClientAppPage />;
}

function ProtectedRouteState({ auth }) {
  if (auth.status === AUTH_SESSION_STATUS.BOOTSTRAPPING) {
    return <main className="route-state"><h1>正在确认登录状态</h1><p role="status">受保护内容将在认证完成后加载。</p></main>;
  }
  if (auth.status === AUTH_SESSION_STATUS.UNAVAILABLE) {
    return (
      <main className="route-state">
        <h1>认证服务暂不可用</h1>
        <p role="alert">{auth.error?.message || '暂时无法确认你的登录状态。'}</p>
        <button type="button" className="start-button" onClick={auth.retryBootstrap}>重试</button>
      </main>
    );
  }
  return (
    <main className="route-state">
      <h1>请先登录</h1>
      <p>该页面需要有效的服务端会话。</p>
      <button type="button" className="start-button" onClick={() => navigate('/')}>返回首页登录</button>
    </main>
  );
}

function navigate(path) {
  window.history.pushState({}, '', path);
  window.dispatchEvent(new Event('dora:navigate'));
}
