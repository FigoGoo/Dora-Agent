// /workspace 是历史 AIGC Demo 的隔离入口；正式 W0 工作台只使用 Project 资源路由。
export const WORKSPACE_ROUTE = '/workspace';
export const PROJECT_WORKSPACE_ROUTE_PATTERN = /^\/projects\/([^/]+)\/workspace$/;

export const CLIENT_ROUTES = {
  home: '/',
  projects: '/projects',
  assets: '/assets',
  skills: '/skill',
  works: '/works',
  credits: '/credits'
};

export const PUBLIC_PAGES = new Set(['home']);
export const CLIENT_ROUTE_ALIASES = {
  '/explore': 'home',
  '/skills': 'skills'
};

const ROUTE_TO_PAGE = Object.entries(CLIENT_ROUTES).reduce((routes, [page, path]) => {
  routes[path] = page;
  return routes;
}, { ...CLIENT_ROUTE_ALIASES });

export function normalizePath(pathname) {
  const path = pathname.replace(/\/+$/, '');
  return path || '/';
}

export function getPageFromPath(pathname) {
  return ROUTE_TO_PAGE[normalizePath(pathname)] || 'home';
}

export function getPathForPage(page) {
  return CLIENT_ROUTES[page] || CLIENT_ROUTES.home;
}

export function getProjectWorkspacePath(projectID) {
  if (!projectID) {
    throw new TypeError('正式工作台路由需要 project_id');
  }
  return `/projects/${encodeURIComponent(projectID)}/workspace`;
}

export function matchProjectWorkspacePath(pathname) {
  const match = normalizePath(pathname).match(PROJECT_WORKSPACE_ROUTE_PATTERN);
  if (!match) {
    return null;
  }
  try {
    const projectID = decodeURIComponent(match[1]);
    return projectID ? { projectID } : null;
  } catch {
    return null;
  }
}
