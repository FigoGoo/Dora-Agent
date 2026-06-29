export const WORKSPACE_ROUTE = '/workspace';

export const CLIENT_ROUTES = {
  home: '/',
  projects: '/projects',
  assets: '/assets',
  skills: '/skills',
  works: '/works',
  explore: '/explore',
  credits: '/credits'
};

export const PUBLIC_PAGES = new Set(['home', 'explore']);

const ROUTE_TO_PAGE = Object.entries(CLIENT_ROUTES).reduce((routes, [page, path]) => {
  routes[path] = page;
  return routes;
}, {});

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
