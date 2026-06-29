export const WORKSPACE_ROUTE = '/workspace';

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
