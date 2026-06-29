import { useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { Clock3, FileCheck2, LogIn, ShieldCheck } from 'lucide-react';
import { adminApi } from '../../lib/api/admin.js';
import { saveAdminSession } from '../../lib/auth/session.js';
import { getAdminEntryPath } from '../../app/auth.js';
import { Alert } from '../../components/admin/Alert.jsx';
import { Button } from '../../components/admin/Button.jsx';
import { TextField } from '../../components/admin/TextField.jsx';

const devDefaultAccount = import.meta.env.DEV ? 'admin@dora.local' : '';

export function LoginPage() {
  const [form, setForm] = useState({ account: devDefaultAccount, password: '' });
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();

  async function submit(event) {
    event.preventDefault();
    setError(null);
    setLoading(true);
    try {
      const session = await adminApi.login(form);
      saveAdminSession(session);
      const fallback = getAdminEntryPath(session);
      navigate(session.must_rotate_password ? fallback : location.state?.from || fallback, { replace: true });
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="admin-login" aria-labelledby="admin-login-title">
      <section className="admin-login__shell">
        <aside className="admin-login__context" aria-label="后台入口说明">
          <div className="admin-login__context-brand">
            <img src="/dora-admin-mark-dense-generated.png" alt="" aria-hidden="true" />
            <div>
              <span>Dora-Agent</span>
              <strong>管理端</strong>
            </div>
          </div>
          <div className="admin-login__context-copy">
            <p>Admin Console</p>
            <h2>配置、审核、审计入口</h2>
          </div>
          <ul className="admin-login__checks">
            <li>
              <ShieldCheck aria-hidden="true" size={17} />
              <span>独立后台登录态</span>
            </li>
            <li>
              <Clock3 aria-hidden="true" size={17} />
              <span>7 天免登录，活动会话自动续期</span>
            </li>
            <li>
              <FileCheck2 aria-hidden="true" size={17} />
              <span>关键操作写入审计</span>
            </li>
          </ul>
        </aside>
        <form onSubmit={submit} className="admin-login__panel">
          <div className="admin-login__brand">
            <img src="/dora-admin-mark-compact-generated.png" alt="" aria-hidden="true" />
            <div>
              <strong>Dora-Agent</strong>
              <h1 id="admin-login-title">平台后台登录</h1>
              <p>仅限平台管理员使用，不复用普通用户登录态。</p>
            </div>
          </div>
          {error ? (
            <Alert tone="danger" title="登录失败" traceId={error.traceId}>
              {error.message}
            </Alert>
          ) : null}
          <TextField
            label="管理员账号"
            value={form.account}
            autoComplete="username"
            onChange={(event) => setForm({ ...form, account: event.target.value })}
          />
          <TextField
            label="密码"
            type="password"
            value={form.password}
            autoComplete="current-password"
            onChange={(event) => setForm({ ...form, password: event.target.value })}
          />
          <Button type="submit" variant="primary" loading={loading} icon={LogIn}>
            登录后台
          </Button>
          <p className="admin-login__footnote">登录异常或账号停用请联系当前平台管理员处理。</p>
        </form>
      </section>
    </main>
  );
}
