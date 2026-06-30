# Admin Frontend Foundation Implementation Plan

状态：archived
owner：前端责任域；文档与契约责任域
更新时间：2026-06-30
适用范围：管理端前端基础架构迁移执行计划；当前事实源已由 `docs/technical/frontend/features/2026-06-30-管理端前端基础架构设计.md` 承接

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize `admin_frontend` into the approved app/pages/features/components/services/utils/assets structure without changing behavior, API contracts, routes, DOM semantics, or styles.

**Architecture:** Move infrastructure first and keep compatibility re-exports at old paths. Route pages through `src/pages`, move shell code to `src/components/layout`, and move service/tool modules to `src/services` and `src/utils`. Keep `ResourceListPage.jsx`, `pageConfigs.jsx`, shared admin components, CSS class names, and API payload semantics unchanged.

**Tech Stack:** Vite 8, React 19, React Router 7, TanStack React Query 5, Vitest, oxlint, JavaScript / JSX only.

---

## Files And Responsibilities

- Create `admin_frontend/src/app/providers.jsx`: wraps `QueryClientProvider` and `BrowserRouter`.
- Create `admin_frontend/src/app/router.jsx`: owns the `Routes` tree currently inside `App.jsx`.
- Modify `admin_frontend/src/app/App.jsx`: render `AppRouter` only.
- Modify `admin_frontend/src/main.jsx`: render `AppProviders` and import moved stylesheet.
- Create `admin_frontend/src/components/layout/AdminShell.jsx`: moved admin shell implementation.
- Create `admin_frontend/src/components/layout/navGroups.js`: moved nav config.
- Modify `admin_frontend/src/layout/AdminShell.jsx`: compatibility re-export only.
- Create `admin_frontend/src/pages/LoginPage.jsx`: route-level wrapper for auth login.
- Create `admin_frontend/src/pages/RotatePasswordPage.jsx`: route-level wrapper for password rotation.
- Create `admin_frontend/src/pages/DashboardPage.jsx`: route-level wrapper for dashboard.
- Create `admin_frontend/src/pages/ResourceListPage.jsx`: route-level wrapper for resource list.
- Create `admin_frontend/src/pages/CreditGrantPage.jsx`: route-level wrapper for credit grants.
- Create `admin_frontend/src/pages/SystemSkillEditorPage.jsx`: route-level wrapper for system skill editor.
- Create `admin_frontend/src/services/http.js`: moved HTTP client from `lib/api/client.js`.
- Create `admin_frontend/src/services/adminApi.js`: moved admin API facade from `lib/api/admin.js`.
- Create `admin_frontend/src/services/session.js`: moved session store from `lib/auth/session.js`.
- Modify `admin_frontend/src/lib/api/client.js`: compatibility re-export from `services/http.js`.
- Modify `admin_frontend/src/lib/api/admin.js`: compatibility re-export from `services/adminApi.js`.
- Modify `admin_frontend/src/lib/auth/session.js`: compatibility re-export from `services/session.js`.
- Create `admin_frontend/src/utils/format.js`: moved formatting helpers from `lib/format.js`.
- Modify `admin_frontend/src/lib/format.js`: compatibility re-export from `utils/format.js`.
- Move `admin_frontend/src/styles/admin.css` to `admin_frontend/src/assets/styles/admin.css` without editing CSS content.
- Move tests for service and utility modules to `admin_frontend/src/services/*.test.js` and `admin_frontend/src/utils/format.test.js`.
- Modify feature/page/component imports to use `services`, `utils`, and `components/layout` target paths.
- Modify `docs/technical/frontend/features/2026-06-30-管理端前端基础架构设计.md`: record implementation evidence after verification.

---

### Task 0: Baseline Verification

**Files:**
- Read only: `admin_frontend/**`

- [ ] **Step 1: Confirm branch and clean worktree**

Run:

```bash
git status --short --branch
```

Expected:

```text
## codex/iteration-2026-06-30
```

No modified or untracked files should be present before code migration starts.

- [ ] **Step 2: Run the existing admin frontend test suite**

Run:

```bash
pnpm --dir admin_frontend test -- --run
```

Expected: Vitest exits `0`. Record the number of passed test files and tests for the final report.

- [ ] **Step 3: Run the existing lint command**

Run:

```bash
pnpm --dir admin_frontend lint
```

Expected: oxlint exits `0`.

- [ ] **Step 4: Run the existing production build**

Run:

```bash
pnpm --dir admin_frontend build
```

Expected: Vite build exits `0`.

- [ ] **Step 5: Commit no files**

Do not commit after baseline verification. Continue only if the current baseline is understood. If any baseline command fails, stop and report the pre-existing failure before editing files.

---

### Task 1: Move Services And Utilities With Compatibility Exports

**Files:**
- Create: `admin_frontend/src/services/http.js`
- Create: `admin_frontend/src/services/adminApi.js`
- Create: `admin_frontend/src/services/session.js`
- Create: `admin_frontend/src/utils/format.js`
- Modify: `admin_frontend/src/lib/api/client.js`
- Modify: `admin_frontend/src/lib/api/admin.js`
- Modify: `admin_frontend/src/lib/auth/session.js`
- Modify: `admin_frontend/src/lib/format.js`
- Move: `admin_frontend/src/lib/api/client.test.js` to `admin_frontend/src/services/http.test.js`
- Move: `admin_frontend/src/lib/auth/session.test.js` to `admin_frontend/src/services/session.test.js`
- Move: `admin_frontend/src/lib/format.test.js` to `admin_frontend/src/utils/format.test.js`

- [ ] **Step 1: Move source modules**

Run:

```bash
mkdir -p admin_frontend/src/services admin_frontend/src/utils
git mv admin_frontend/src/lib/api/client.js admin_frontend/src/services/http.js
git mv admin_frontend/src/lib/api/admin.js admin_frontend/src/services/adminApi.js
git mv admin_frontend/src/lib/auth/session.js admin_frontend/src/services/session.js
git mv admin_frontend/src/lib/format.js admin_frontend/src/utils/format.js
```

Expected: the four implementation files exist under `services` and `utils`.

- [ ] **Step 2: Update `services/http.js` session import**

Change the import at the top of `admin_frontend/src/services/http.js` to:

```js
import { clearAdminSession, getAdminSession, renewAdminSession } from './session.js';
```

Keep the rest of the file behavior unchanged.

- [ ] **Step 3: Update `services/adminApi.js` HTTP import**

Change `admin_frontend/src/services/adminApi.js` to import from the moved HTTP client:

```js
import { adminRequest } from './http.js';
```

Keep the exported `adminApi` object and all method bodies unchanged.

- [ ] **Step 4: Add compatibility export for old HTTP client path**

Create `admin_frontend/src/lib/api/client.js` with exactly:

```js
export {
  ApiError,
  adminRequest,
  buildQuery,
  parseApiError
} from '../../services/http.js';
```

- [ ] **Step 5: Add compatibility export for old admin API path**

Create `admin_frontend/src/lib/api/admin.js` with exactly:

```js
export { adminApi } from '../../services/adminApi.js';
```

- [ ] **Step 6: Add compatibility export for old session path**

Create `admin_frontend/src/lib/auth/session.js` with exactly:

```js
export {
  clearAdminSession,
  getAdminSession,
  renewAdminSession,
  saveAdminSession,
  subscribeAdminSession
} from '../../services/session.js';
```

- [ ] **Step 7: Add compatibility export for old format path**

Create `admin_frontend/src/lib/format.js` with exactly:

```js
export { formatDateTime, readListPayload, statusTone, toApiDateTime } from '../utils/format.js';
```

- [ ] **Step 8: Move service and utility tests**

Run:

```bash
git mv admin_frontend/src/lib/api/client.test.js admin_frontend/src/services/http.test.js
git mv admin_frontend/src/lib/auth/session.test.js admin_frontend/src/services/session.test.js
git mv admin_frontend/src/lib/format.test.js admin_frontend/src/utils/format.test.js
```

- [ ] **Step 9: Update `http.test.js` imports**

At the top of `admin_frontend/src/services/http.test.js`, use:

```js
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { adminRequest, parseApiError } from './http.js';
import { getAdminSession, saveAdminSession } from './session.js';
```

Do not change test bodies.

- [ ] **Step 10: Verify moved service tests**

Run:

```bash
pnpm --dir admin_frontend test -- src/services/http.test.js src/services/session.test.js src/utils/format.test.js --run
```

Expected: the moved tests pass.

- [ ] **Step 11: Commit service and utility migration**

Run:

```bash
git add admin_frontend/src/services admin_frontend/src/utils admin_frontend/src/lib/api admin_frontend/src/lib/auth admin_frontend/src/lib/format.js
git commit -m "重组管理端服务与工具目录"
```

---

### Task 2: Move Styles Into Assets And Add App Providers

**Files:**
- Move: `admin_frontend/src/styles/admin.css` to `admin_frontend/src/assets/styles/admin.css`
- Create: `admin_frontend/src/app/providers.jsx`
- Modify: `admin_frontend/src/main.jsx`

- [ ] **Step 1: Move the CSS file without editing content**

Run:

```bash
mkdir -p admin_frontend/src/assets/styles
git mv admin_frontend/src/styles/admin.css admin_frontend/src/assets/styles/admin.css
```

Expected: `git diff -- admin_frontend/src/assets/styles/admin.css` should show a pure rename or identical file content.

- [ ] **Step 2: Create `app/providers.jsx`**

Create `admin_frontend/src/app/providers.jsx` with exactly:

```jsx
import { QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import { queryClient } from './queryClient.js';

export function AppProviders({ children }) {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>{children}</BrowserRouter>
    </QueryClientProvider>
  );
}
```

- [ ] **Step 3: Simplify `main.jsx`**

Replace `admin_frontend/src/main.jsx` with:

```jsx
import React from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './app/App.jsx';
import { AppProviders } from './app/providers.jsx';
import './assets/styles/admin.css';

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <AppProviders>
      <App />
    </AppProviders>
  </React.StrictMode>
);
```

- [ ] **Step 4: Verify provider and stylesheet migration**

Run:

```bash
pnpm --dir admin_frontend test -- src/app/auth.test.jsx --run
pnpm --dir admin_frontend build
```

Expected: both commands exit `0`.

- [ ] **Step 5: Commit provider and style migration**

Run:

```bash
git add admin_frontend/src/main.jsx admin_frontend/src/app/providers.jsx admin_frontend/src/assets/styles/admin.css admin_frontend/src/styles
git commit -m "收口管理端Provider与样式入口"
```

---

### Task 3: Add Page Entry Wrappers And Move Router Out Of App

**Files:**
- Create: `admin_frontend/src/pages/LoginPage.jsx`
- Create: `admin_frontend/src/pages/RotatePasswordPage.jsx`
- Create: `admin_frontend/src/pages/DashboardPage.jsx`
- Create: `admin_frontend/src/pages/ResourceListPage.jsx`
- Create: `admin_frontend/src/pages/CreditGrantPage.jsx`
- Create: `admin_frontend/src/pages/SystemSkillEditorPage.jsx`
- Create: `admin_frontend/src/app/router.jsx`
- Modify: `admin_frontend/src/app/App.jsx`

- [ ] **Step 1: Create page wrappers**

Create these files exactly:

`admin_frontend/src/pages/LoginPage.jsx`

```jsx
export { LoginPage } from '../features/auth/LoginPage.jsx';
```

`admin_frontend/src/pages/RotatePasswordPage.jsx`

```jsx
export { RotatePasswordPage } from '../features/auth/RotatePasswordPage.jsx';
```

`admin_frontend/src/pages/DashboardPage.jsx`

```jsx
export { DashboardPage } from '../features/dashboard/DashboardPage.jsx';
```

`admin_frontend/src/pages/ResourceListPage.jsx`

```jsx
export { ResourceListPage } from '../features/resources/ResourceListPage.jsx';
```

`admin_frontend/src/pages/CreditGrantPage.jsx`

```jsx
export { CreditGrantPage } from '../features/resources/CreditGrantPage.jsx';
```

`admin_frontend/src/pages/SystemSkillEditorPage.jsx`

```jsx
export { SystemSkillEditorPage } from '../features/resources/SystemSkillEditorPage.jsx';
```

- [ ] **Step 2: Create `app/router.jsx`**

Create `admin_frontend/src/app/router.jsx` with:

```jsx
import { Navigate, Route, Routes } from 'react-router-dom';
import { RequireAdminSession } from './auth.js';
import { AdminShell } from '../layout/AdminShell.jsx';
import { LoginPage } from '../pages/LoginPage.jsx';
import { RotatePasswordPage } from '../pages/RotatePasswordPage.jsx';
import { DashboardPage } from '../pages/DashboardPage.jsx';
import { ResourceListPage } from '../pages/ResourceListPage.jsx';
import { CreditGrantPage } from '../pages/CreditGrantPage.jsx';
import { SystemSkillEditorPage } from '../pages/SystemSkillEditorPage.jsx';
import { pageConfigs } from '../features/resources/pageConfigs.jsx';

export function AppRouter() {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/admin" replace />} />
      <Route path="/admin/login" element={<LoginPage />} />
      <Route element={<RequireAdminSession />}>
        <Route path="/admin/rotate-password" element={<RotatePasswordPage />} />
        <Route path="/admin" element={<AdminShell />}>
          <Route index element={<DashboardPage />} />
          <Route path="skills/system/new" element={<SystemSkillEditorPage />} />
          {Object.entries(pageConfigs).map(([path, config]) => (
            <Route key={path} path={path} element={<ResourceListPage config={config} />} />
          ))}
          <Route path="credits/grants" element={<CreditGrantPage />} />
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/admin" replace />} />
    </Routes>
  );
}
```

- [ ] **Step 3: Simplify `App.jsx`**

Replace `admin_frontend/src/app/App.jsx` with:

```jsx
import { AppRouter } from './router.jsx';

export function App() {
  return <AppRouter />;
}
```

- [ ] **Step 4: Verify routes still compile through tests**

Run:

```bash
pnpm --dir admin_frontend test -- src/app/auth.test.jsx src/features/resources/pageConfigs.test.jsx --run
pnpm --dir admin_frontend build
```

Expected: both commands exit `0`.

- [ ] **Step 5: Commit router and page wrappers**

Run:

```bash
git add admin_frontend/src/app/App.jsx admin_frontend/src/app/router.jsx admin_frontend/src/pages
git commit -m "拆分管理端路由与页面入口"
```

---

### Task 4: Move Admin Layout And Navigation Config

**Files:**
- Create: `admin_frontend/src/components/layout/navGroups.js`
- Create: `admin_frontend/src/components/layout/AdminShell.jsx`
- Modify: `admin_frontend/src/layout/AdminShell.jsx`
- Modify: `admin_frontend/src/app/router.jsx`
- Modify: `admin_frontend/src/features/dashboard/DashboardPage.jsx`

- [ ] **Step 1: Move AdminShell implementation**

Run:

```bash
mkdir -p admin_frontend/src/components/layout
git mv admin_frontend/src/layout/AdminShell.jsx admin_frontend/src/components/layout/AdminShell.jsx
```

- [ ] **Step 2: Create `navGroups.js`**

Move the existing `navGroups` export from `AdminShell.jsx` into `admin_frontend/src/components/layout/navGroups.js`.

The new file must export the same array shape:

```js
import {
  Blocks,
  BookOpenCheck,
  Boxes,
  ClipboardList,
  Coins,
  Gauge,
  Shield,
  Sparkles,
  UserCog,
  Users,
  WandSparkles,
  Wrench
} from 'lucide-react';

export const navGroups = [
  { title: '概览', items: [{ to: '/admin', label: '后台首页', icon: Gauge, end: true }] },
  {
    title: '账号与用户',
    items: [
      { to: '/admin/admins', label: '管理员账号', icon: UserCog },
      { to: '/admin/users', label: '用户管理', icon: Users }
    ]
  },
  {
    title: '能力配置',
    items: [
      { to: '/admin/skills/system', label: '系统 Skill', icon: WandSparkles },
      { to: '/admin/skills/reviews', label: 'Skill 审核', icon: BookOpenCheck },
      { to: '/admin/models/providers', label: '模型供应商', icon: Blocks },
      { to: '/admin/models', label: '模型管理', icon: Sparkles, end: true },
      { to: '/admin/tools', label: 'Tool 管理', icon: Wrench }
    ]
  },
  {
    title: '运营与审计',
    items: [
      { to: '/admin/credits/grants', label: '积分发放', icon: Coins },
      { to: '/admin/credits/codes', label: '兑换码', icon: ClipboardList },
      { to: '/admin/works/public', label: '精选作品', icon: Boxes },
      { to: '/admin/audit-logs', label: '审计日志', icon: Shield },
      { to: '/admin/asset-element-types', label: '资产元素类型', icon: Boxes }
    ]
  }
];
```

- [ ] **Step 3: Update moved `AdminShell.jsx` imports**

At the top of `admin_frontend/src/components/layout/AdminShell.jsx`, import only the icon used by the shell and use target-layer imports:

```jsx
import { useSyncExternalStore } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { LogOut } from 'lucide-react';
import { adminApi } from '../../services/adminApi.js';
import { clearAdminSession, getAdminSession, subscribeAdminSession } from '../../services/session.js';
import { IconButton } from '../admin/IconButton.jsx';
import { ToastProvider, useToast } from '../admin/Toast.jsx';
import { navGroups } from './navGroups.js';
```

Remove the inline `navGroups` definition from this file. Keep `AdminShellInner`, `AdminShell`, JSX, class names, and logout behavior unchanged.

- [ ] **Step 4: Add old layout compatibility re-export**

Create `admin_frontend/src/layout/AdminShell.jsx` with exactly:

```jsx
export { AdminShell } from '../components/layout/AdminShell.jsx';
export { navGroups } from '../components/layout/navGroups.js';
```

- [ ] **Step 5: Update router to use new layout path**

In `admin_frontend/src/app/router.jsx`, change the shell import to:

```jsx
import { AdminShell } from '../components/layout/AdminShell.jsx';
```

- [ ] **Step 6: Update dashboard navigation import**

In `admin_frontend/src/features/dashboard/DashboardPage.jsx`, change the nav import to:

```jsx
import { navGroups } from '../../components/layout/navGroups.js';
```

- [ ] **Step 7: Verify dashboard and shell behavior**

Run:

```bash
pnpm --dir admin_frontend test -- src/features/resources/pageConfigs.test.jsx --run
pnpm --dir admin_frontend build
```

Expected: both commands exit `0`.

- [ ] **Step 8: Commit layout migration**

Run:

```bash
git add admin_frontend/src/components/layout admin_frontend/src/layout/AdminShell.jsx admin_frontend/src/app/router.jsx admin_frontend/src/features/dashboard/DashboardPage.jsx
git commit -m "迁移管理端布局与导航配置"
```

---

### Task 5: Update Source Imports To Target Paths

**Files:**
- Modify: `admin_frontend/src/app/auth.js`
- Modify: `admin_frontend/src/components/admin/Badge.jsx`
- Modify: `admin_frontend/src/features/auth/LoginPage.jsx`
- Modify: `admin_frontend/src/features/auth/RotatePasswordPage.jsx`
- Modify: `admin_frontend/src/features/dashboard/DashboardPage.jsx`
- Modify: `admin_frontend/src/features/resources/CreditGrantPage.jsx`
- Modify: `admin_frontend/src/features/resources/ResourceListPage.jsx`
- Modify: `admin_frontend/src/features/resources/SystemSkillEditorPage.jsx`
- Modify: `admin_frontend/src/features/resources/pageConfigs.jsx`
- Modify tests that mock or import old service paths.

- [ ] **Step 1: Update app auth session import**

In `admin_frontend/src/app/auth.js`, use:

```js
import { getAdminSession } from '../services/session.js';
```

- [ ] **Step 2: Update shared component utility import**

In `admin_frontend/src/components/admin/Badge.jsx`, use:

```js
import { statusTone } from '../../utils/format.js';
```

- [ ] **Step 3: Update auth feature imports**

In `admin_frontend/src/features/auth/LoginPage.jsx`, use:

```js
import { adminApi } from '../../services/adminApi.js';
import { saveAdminSession } from '../../services/session.js';
```

In `admin_frontend/src/features/auth/RotatePasswordPage.jsx`, use:

```js
import { adminApi } from '../../services/adminApi.js';
import { saveAdminSession } from '../../services/session.js';
```

- [ ] **Step 4: Update dashboard imports**

In `admin_frontend/src/features/dashboard/DashboardPage.jsx`, use:

```js
import { adminApi } from '../../services/adminApi.js';
import { navGroups } from '../../components/layout/navGroups.js';
```

- [ ] **Step 5: Update resource feature imports**

In `admin_frontend/src/features/resources/ResourceListPage.jsx`, use:

```js
import { adminApi } from '../../services/adminApi.js';
import { readListPayload, toApiDateTime } from '../../utils/format.js';
```

In `admin_frontend/src/features/resources/SystemSkillEditorPage.jsx`, use:

```js
import { adminApi } from '../../services/adminApi.js';
import { readListPayload } from '../../utils/format.js';
```

In `admin_frontend/src/features/resources/CreditGrantPage.jsx`, use:

```js
import { adminApi } from '../../services/adminApi.js';
import { readListPayload, toApiDateTime } from '../../utils/format.js';
```

In `admin_frontend/src/features/resources/pageConfigs.jsx`, use:

```js
import { adminApi } from '../../services/adminApi.js';
import { formatDateTime } from '../../utils/format.js';
```

- [ ] **Step 6: Update feature tests to mock target API path**

Replace `../../lib/api/admin.js` with `../../services/adminApi.js` in these tests:

```text
admin_frontend/src/features/resources/ResourceListPage.actions.test.jsx
admin_frontend/src/features/resources/SystemSkillEditorPage.test.jsx
admin_frontend/src/features/resources/pageConfigs.test.jsx
```

Keep mock bodies unchanged.

- [ ] **Step 7: Search for old imports**

Run:

```bash
rg -n "lib/api|lib/auth|lib/format|\\.\\./\\.\\./layout|\\.\\./layout" admin_frontend/src
```

Expected output is limited to compatibility files under `admin_frontend/src/lib/**` and `admin_frontend/src/layout/AdminShell.jsx`. If source files outside those compatibility files appear, update their imports to target paths.

- [ ] **Step 8: Run targeted tests**

Run:

```bash
pnpm --dir admin_frontend test -- src/app/auth.test.jsx src/features/resources/ResourceListPage.actions.test.jsx src/features/resources/SystemSkillEditorPage.test.jsx src/features/resources/pageConfigs.test.jsx src/services/http.test.js src/services/session.test.js src/utils/format.test.js --run
```

Expected: all listed tests pass.

- [ ] **Step 9: Commit import migration**

Run:

```bash
git add admin_frontend/src
git commit -m "更新管理端导入路径到目标架构"
```

---

### Task 6: Full Verification And Architecture Document Update

**Files:**
- Modify: `docs/technical/frontend/features/2026-06-30-管理端前端基础架构设计.md`

- [ ] **Step 1: Run full admin frontend tests**

Run:

```bash
pnpm --dir admin_frontend test -- --run
```

Expected: Vitest exits `0`.

- [ ] **Step 2: Run lint**

Run:

```bash
pnpm --dir admin_frontend lint
```

Expected: oxlint exits `0`.

- [ ] **Step 3: Run production build**

Run:

```bash
pnpm --dir admin_frontend build
```

Expected: Vite exits `0`.

- [ ] **Step 4: Verify no unintended style edits**

Run:

```bash
git diff -- admin_frontend/src/assets/styles/admin.css
```

Expected: no CSS line edits beyond the path move. If there are content changes, revert only the unintended CSS changes.

- [ ] **Step 5: Verify target directories exist**

Run:

```bash
test -f admin_frontend/src/app/router.jsx
test -f admin_frontend/src/app/providers.jsx
test -f admin_frontend/src/components/layout/AdminShell.jsx
test -f admin_frontend/src/components/layout/navGroups.js
test -f admin_frontend/src/services/http.js
test -f admin_frontend/src/services/adminApi.js
test -f admin_frontend/src/services/session.js
test -f admin_frontend/src/utils/format.js
test -f admin_frontend/src/assets/styles/admin.css
```

Expected: command exits `0`.

- [ ] **Step 6: Update architecture implementation record**

In `docs/technical/frontend/features/2026-06-30-管理端前端基础架构设计.md`, update the `实现记录` section to:

```markdown
## 实现记录

- 实现状态：已完成管理端基础架构迁移，保持 JavaScript / JSX 技术栈。
- 验证命令：
  - `pnpm --dir admin_frontend test -- --run`
  - `pnpm --dir admin_frontend lint`
  - `pnpm --dir admin_frontend build`
- 证据路径：本地命令输出；如补充浏览器 smoke，截图放入 `admin_frontend/visual-checks/`。
- 未执行原因：无。
- 遗留风险：`ResourceListPage.jsx` 和 `pageConfigs.jsx` 仍然较大，后续可单独设计第二阶段领域内聚拆分。
```

Do not mark route smoke as completed unless a browser or equivalent route verification was actually run.

- [ ] **Step 7: Update architecture archive checklist**

In the same document, mark completed items in `归档条件`:

```markdown
## 归档条件

- [x] 目标目录迁移完成。
- [x] 测试计划全部通过或记录未执行原因。
- [x] 后续新增管理端代码默认使用 `pages`、`services`、`utils`、`components/layout` 目标路径。
- [ ] 若后续拆分 `ResourceListPage.jsx`，另起独立技术设计文档。
```

- [ ] **Step 8: Commit verification documentation**

Run:

```bash
git add docs/technical/frontend/features/2026-06-30-管理端前端基础架构设计.md
git commit -m "记录管理端架构迁移验证结果"
```

---

### Task 7: Final Audit

**Files:**
- Read only unless verification finds a specific issue.

- [ ] **Step 1: Inspect final status**

Run:

```bash
git status --short --branch
```

Expected: clean working tree on `codex/iteration-2026-06-30`.

- [ ] **Step 2: Inspect commit list**

Run:

```bash
git log --oneline --decorate -6
```

Expected: latest commits include the architecture design commit and implementation commits with Chinese messages.

- [ ] **Step 3: Inspect final diff from implementation base**

Run:

```bash
git diff --stat d502405..HEAD
```

Expected: changes are limited to `admin_frontend/src/**` architecture migration files and the implementation record in the architecture design document.

- [ ] **Step 4: Final response**

Report:

```text
已完成管理端前端基础架构迁移。
未引入 TypeScript，未修改 CSS 内容，未改变管理端路由和 API 语义。
验证：pnpm --dir admin_frontend test -- --run；pnpm --dir admin_frontend lint；pnpm --dir admin_frontend build。
```

Include any verification failures if they occurred, with exact command and failure summary.

---

## Self-Review

Spec coverage:

- Directory target is covered by Tasks 1, 2, 3, and 4.
- Route extraction is covered by Task 3.
- Provider extraction is covered by Task 2.
- Layout and nav split is covered by Task 4.
- Service and utility migration is covered by Tasks 1 and 5.
- CSS move without content changes is covered by Task 2 and Task 6.
- No TypeScript and no style/function changes are covered by verification in Task 6.
- Documentation implementation record is covered by Task 6.

Placeholder scan:

- The plan contains no unfinished placeholder steps.
- Each code-changing step includes exact file paths and exact code snippets or concrete move commands.

Consistency check:

- `services/http.js` imports `./session.js`.
- `services/adminApi.js` imports `./http.js`.
- Compatibility files import from the target paths using relative paths valid from their old locations.
- `app/router.jsx` initially imports `AdminShell` through the compatibility path and Task 4 updates it to the target layout path.
