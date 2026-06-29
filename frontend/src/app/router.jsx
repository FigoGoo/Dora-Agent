import { AgentWorkspacePage } from '../pages/AgentWorkspacePage.jsx';
import { ClientAppPage } from '../pages/ClientAppPage.jsx';
import { WORKSPACE_ROUTE, normalizePath } from './routes.js';

export function AppRouter() {
  const currentPath = typeof window === 'undefined' ? '/' : normalizePath(window.location.pathname);

  return currentPath === WORKSPACE_ROUTE ? <AgentWorkspacePage /> : <ClientAppPage />;
}
