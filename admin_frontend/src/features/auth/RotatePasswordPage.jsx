import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FileCheck2, KeyRound, LockKeyhole, ShieldCheck } from 'lucide-react';
import { adminApi } from '../../services/adminApi.js';
import { saveAdminSession } from '../../services/session.js';
import { Alert } from '../../components/admin/Alert.jsx';
import { Button } from '../../components/admin/Button.jsx';
import { TextField } from '../../components/admin/TextField.jsx';

export function RotatePasswordPage() {
  const [form, setForm] = useState({ current_password: '', new_password: '', reason: '初始管理员首次登录改密' });
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  async function submit(event) {
    event.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const session = await adminApi.rotatePassword(form);
      saveAdminSession(session);
      navigate('/admin', { replace: true });
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="admin-login" aria-labelledby="admin-rotate-title">
      <section className="admin-login__shell">
        <aside className="admin-login__context" aria-label="改密要求说明">
          <div className="admin-login__context-brand">
            <img src="/dora-admin-mark-dense-generated.png" alt="" aria-hidden="true" />
            <div>
              <span>Dora-Agent</span>
              <strong>安全校验</strong>
            </div>
          </div>
          <div className="admin-login__context-copy">
            <p>First Login</p>
            <h2>轮换初始密码后继续</h2>
          </div>
          <ul className="admin-login__checks">
            <li>
              <LockKeyhole aria-hidden="true" size={17} />
              <span>初始凭证不可长期使用</span>
            </li>
            <li>
              <ShieldCheck aria-hidden="true" size={17} />
              <span>改密后进入独立后台会话</span>
            </li>
            <li>
              <FileCheck2 aria-hidden="true" size={17} />
              <span>改密动作写入后台审计</span>
            </li>
          </ul>
        </aside>
        <form onSubmit={submit} className="admin-login__panel">
          <div className="admin-login__brand">
            <img src="/dora-admin-mark-compact-generated.png" alt="" aria-hidden="true" />
            <div>
              <strong>安全要求</strong>
              <h1 id="admin-rotate-title">轮换初始密码</h1>
              <p>完成后才能进入后台业务页面。</p>
            </div>
          </div>
          {error ? (
            <Alert tone="danger" title="改密失败" traceId={error.traceId}>
              {error.message}
            </Alert>
          ) : null}
          <TextField
            label="当前密码"
            type="password"
            value={form.current_password}
            autoComplete="current-password"
            onChange={(event) => setForm({ ...form, current_password: event.target.value })}
          />
          <TextField
            label="新密码"
            type="password"
            value={form.new_password}
            autoComplete="new-password"
            onChange={(event) => setForm({ ...form, new_password: event.target.value })}
          />
          <TextField label="操作原因" value={form.reason} onChange={(event) => setForm({ ...form, reason: event.target.value })} />
          <Button type="submit" variant="primary" loading={loading} icon={KeyRound}>
            保存并进入后台
          </Button>
        </form>
      </section>
    </main>
  );
}
