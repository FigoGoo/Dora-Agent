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
