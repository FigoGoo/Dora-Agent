// /workspace 是历史 AIGC Demo 的隔离入口；正式 W0 工作台只使用 Project 资源路由。
export const WORKSPACE_ROUTE = '/workspace';
export const PROJECT_WORKSPACE_ROUTE_PATTERN = /^\/projects\/([^/]+)\/workspace$/;
export const LEGACY_SKILLS_ROUTE = '/skill';
export const OWNER_SKILL_NEW_ROUTE = '/my/skills/new';
export const OWNER_SKILL_EDIT_ROUTE_PATTERN = /^\/my\/skills\/([^/]+)\/edit$/;
export const PUBLIC_SKILL_DETAIL_ROUTE_PATTERN = /^\/skills\/([^/]+)$/;
export const SKILL_REVIEW_QUEUE_ROUTE = '/admin/skills/reviews';
export const SKILL_REVIEW_DETAIL_ROUTE_PATTERN = /^\/admin\/skills\/reviews\/([^/]+)$/;
export const SKILL_REVIEW_CAPABILITY = 'skill.review';
const UUID_V7_ROUTE_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export const CLIENT_ROUTES = {
  home: '/',
  projects: '/projects',
  assets: '/assets',
  skills: '/skills',
  mySkills: '/my/skills',
  skillReviews: SKILL_REVIEW_QUEUE_ROUTE,
  works: '/works',
  credits: '/credits'
};

export const PUBLIC_PAGES = new Set(['home', 'skills', 'skillDetail']);
export const CLIENT_ROUTE_ALIASES = {
  '/explore': 'home',
  [LEGACY_SKILLS_ROUTE]: 'skills'
};

const ROUTE_TO_PAGE = Object.entries(CLIENT_ROUTES).reduce((routes, [page, path]) => {
  routes[path] = page;
  return routes;
}, { ...CLIENT_ROUTE_ALIASES });

export function normalizePath(pathname) {
  if (pathname !== SKILL_REVIEW_QUEUE_ROUTE && pathname.startsWith(`${SKILL_REVIEW_QUEUE_ROUTE}/`)) {
    return pathname;
  }
  const path = pathname.replace(/\/+$/, '');
  return path || '/';
}

export function getPageFromPath(pathname) {
  const path = normalizePath(pathname);
  if (
    path === OWNER_SKILL_NEW_ROUTE
    || OWNER_SKILL_EDIT_ROUTE_PATTERN.test(path)
    || (path.startsWith('/my/skills/') && path.endsWith('/edit'))
  ) return 'skillBuilder';
  if (path.startsWith(`${SKILL_REVIEW_QUEUE_ROUTE}/`)) return 'skillReviewDetail';
  if (PUBLIC_SKILL_DETAIL_ROUTE_PATTERN.test(path) || path.startsWith('/skills/')) return 'skillDetail';
  return ROUTE_TO_PAGE[path] || 'home';
}

// 公开详情只接受原始、无编码替代和无尾斜杠的规范小写 UUIDv7 路径。
export function matchPublicSkillDetailPath(pathname) {
  const match = String(pathname || '').match(PUBLIC_SKILL_DETAIL_ROUTE_PATTERN);
  if (!match) return null;
  const skillID = match[1];
  return UUID_V7_ROUTE_PATTERN.test(skillID) ? { skillID } : null;
}

export function matchSkillReviewDetailPath(pathname) {
  const match = normalizePath(pathname).match(SKILL_REVIEW_DETAIL_ROUTE_PATTERN);
  if (!match) return null;
  const reviewID = match[1];
  return UUID_V7_ROUTE_PATTERN.test(reviewID) ? { reviewID } : null;
}

export function getSkillReviewDetailPath(reviewID) {
  if (!UUID_V7_ROUTE_PATTERN.test(String(reviewID || ''))) {
    throw new TypeError('Skill 审核详情路由需要规范 UUIDv7 review_id');
  }
  return `${SKILL_REVIEW_QUEUE_ROUTE}/${reviewID}`;
}

export function getRequiredCapabilityForPage(page) {
  return page === 'skillReviews' || page === 'skillReviewDetail'
    ? SKILL_REVIEW_CAPABILITY
    : '';
}

export function canAccessPage(page, capabilities, deniedCapabilities = []) {
  const required = getRequiredCapabilityForPage(page);
  return !required || (
    Array.isArray(capabilities)
    && capabilities.includes(required)
    && !(Array.isArray(deniedCapabilities) && deniedCapabilities.includes(required))
  );
}

export function matchOwnerSkillBuilderPath(pathname) {
  const path = normalizePath(pathname);
  if (path === OWNER_SKILL_NEW_ROUTE) return { skillID: '' };
  const match = path.match(OWNER_SKILL_EDIT_ROUTE_PATTERN);
  if (!match) return null;
  try {
    const skillID = decodeURIComponent(match[1]);
    return UUID_V7_ROUTE_PATTERN.test(skillID) ? { skillID } : null;
  } catch {
    return null;
  }
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
