import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Send } from 'lucide-react';
import { adminApi } from '../../lib/api/admin.js';
import { readListPayload, toApiDateTime } from '../../lib/format.js';
import { AuditHint } from '../../components/admin/AuditHint.jsx';
import { Badge } from '../../components/admin/Badge.jsx';
import { Button } from '../../components/admin/Button.jsx';
import { DataTable } from '../../components/admin/DataTable.jsx';
import { PageHeader } from '../../components/admin/PageHeader.jsx';
import { SelectField } from '../../components/admin/SelectField.jsx';
import { TextField } from '../../components/admin/TextField.jsx';
import { useToast } from '../../components/admin/Toast.jsx';
import { validateRequiredFields } from './ResourceListPage.jsx';

const grantFields = [
  { name: 'target_id', label: '目标 ID', required: true },
  { name: 'points', label: '积分数量', required: true },
  { name: 'expires_at', label: '积分过期时间', required: true },
  { name: 'reason', label: '发放原因', required: true }
];

export function CreditGrantPage() {
  const [form, setForm] = useState({ target_type: 'user', target_id: '', points: '', reason: '', expires_at: '' });
  const [keyword, setKeyword] = useState('');
  const [errors, setErrors] = useState({});
  const toast = useToast();
  const targetsQuery = useQuery({
    queryKey: ['credit-targets', keyword, form.target_type],
    queryFn: () => adminApi.list('/api/admin/credits/grants/targets', { keyword, target_type: form.target_type, page_size: 10 })
  });
  const targets = readListPayload(targetsQuery.data).items;
  const grantMutation = useMutation({
    mutationFn: () =>
      adminApi.post('/api/admin/credits/grants', { ...form, points: Number(form.points), expires_at: toApiDateTime(form.expires_at) }),
    onSuccess: () => {
      toast?.notify('积分已发放，已记录审计');
      setForm({ target_type: 'user', target_id: '', points: '', reason: '', expires_at: '' });
      setErrors({});
    },
    onError: (error) => toast?.notify(error.message || '发放失败，请稍后重试。', 'danger', { title: '发放失败', traceId: error.traceId })
  });

  function updateForm(name, value) {
    setForm((current) => ({ ...current, [name]: value }));
    setErrors((current) => {
      if (!current[name]) {
        return current;
      }
      const next = { ...current };
      delete next[name];
      return next;
    });
  }

  function submit(event) {
    event.preventDefault();
    const nextErrors = validateRequiredFields(grantFields, form);
    if (Object.keys(nextErrors).length) {
      setErrors(nextErrors);
      toast?.notify('请补全必填字段后再发放。', 'warning', { title: '表单未完成' });
      return;
    }
    setErrors({});
    grantMutation.mutate();
  }

  return (
    <>
      <PageHeader title="积分发放" description="给个人或企业账户手动发放积分，必须记录原因和过期时间。" />
      <section className="admin-two-column">
        <form className="admin-form-card" onSubmit={submit} noValidate>
          <SelectField
            label="目标类型"
            value={form.target_type}
            onChange={(event) => updateForm('target_type', event.target.value)}
            options={[
              { label: '个人用户', value: 'user' },
              { label: '企业', value: 'enterprise' }
            ]}
          />
          <TextField label="搜索目标" value={keyword} onChange={(event) => setKeyword(event.target.value)} />
          <TextField label="目标 ID" value={form.target_id} onChange={(event) => updateForm('target_id', event.target.value)} required error={errors.target_id} />
          <TextField label="积分数量" type="number" value={form.points} onChange={(event) => updateForm('points', event.target.value)} required error={errors.points} />
          <TextField
            label="积分过期时间"
            type="datetime-local"
            value={form.expires_at}
            onChange={(event) => updateForm('expires_at', event.target.value)}
            required
            error={errors.expires_at}
          />
          <TextField label="发放原因" textarea rows={4} value={form.reason} onChange={(event) => updateForm('reason', event.target.value)} required error={errors.reason} />
          <AuditHint>积分发放成功后会写入积分流水和后台审计。</AuditHint>
          <Button type="submit" variant="primary" loading={grantMutation.isPending} icon={Send}>
            发放积分
          </Button>
        </form>
        <DataTable
          columns={[
            { key: 'display_name', title: '目标' },
            { key: 'target_id', title: 'ID' },
            { key: 'target_type', title: '类型', render: (row) => <Badge>{row.target_type}</Badge> }
          ]}
          rows={targets}
          rowKey="target_id"
          state={targetsQuery.isLoading ? 'loading' : targets.length ? 'success' : 'empty'}
          emptyText="暂无匹配目标"
        />
      </section>
    </>
  );
}
