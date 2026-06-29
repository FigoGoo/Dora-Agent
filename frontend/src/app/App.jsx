import { AgentWorkspacePage, LandingPage } from '../features/landing/LandingPage.jsx';
import { WORKSPACE_ROUTE, normalizePath } from '../features/landing/landingRoutes.js';

export function App() {
  const currentPath = typeof window === 'undefined' ? '/' : normalizePath(window.location.pathname);

  return currentPath === WORKSPACE_ROUTE ? <AgentWorkspacePage /> : <LandingPage />;
}
