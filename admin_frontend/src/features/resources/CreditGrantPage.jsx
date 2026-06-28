import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Send } from 'lucide-react';
import { adminApi } from '../../lib/api/admin.js';
import { readListPayload, toApiDateTime } from '../../lib/format.js';
import { Alert } from '../../components/admin/Alert.jsx';
import { AuditHint } from '../../components/admin/AuditHint.jsx';
import { Badge } from '../../components/admin/Badge.jsx';
import { Button } from '../../components/admin/Button.jsx';
import { DataTable } from '../../components/admin/DataTable.jsx';
import { PageHeader } from '../../components/admin/PageHeader.jsx';
import { SelectField } from '../../components/admin/SelectField.jsx';
import { TextField } from '../../components/admin/TextField.jsx';
import { useToast } from '../../components/admin/Toast.jsx';

export function CreditGrantPage() {
  const [form, setForm] = useState({ target_type: 'user', target_id: '', points: '', reason: '', expires_at: '' });
  const [keyword, setKeyword] = useState('');
  const [error, setError] = useState(null);
  const toast = useToast();
  const targetsQuery = useQuery({
    queryKey: ['credit-targets', keyword, form.target_type],
    queryFn: () => adminApi.list('/api/admin/credits/grants/targets', { keyword, target_type: form.target_type, page_size: 10 })
  });
  const targets = readListPayload(targetsQuery.data).items;
  const grantMutation = useMutation({
    mutationFn: () =>
      adminApi.post(
        '/api/admin/credits/grants',
        { ...form, points: Number(form.points), expires_at: toApiDateTime(form.expires_at) },
        { reason: form.reason }
      ),
    onSuccess: () => {
      toast?.notify('积分已发放，已记录审计');
      setForm({ target_type: 'user', target_id: '', points: '', reason: '', expires_at: '' });
    },
    onError: setError
  });

  function submit(event) {
    event.preventDefault();
    setError(null);
    grantMutation.mutate();
  }

  return (
    <>
      <PageHeader title="积分发放" description="给个人或企业账户手动发放积分，必须记录原因和过期时间。" />
      {error ? (
        <Alert tone="danger" title="发放失败" traceId={error.traceId}>
          {error.message}
        </Alert>
      ) : null}
      <section className="admin-two-column">
        <form className="admin-form-card" onSubmit={submit}>
          <SelectField
            label="目标类型"
            value={form.target_type}
            onChange={(event) => setForm({ ...form, target_type: event.target.value })}
            options={[
              { label: '个人用户', value: 'user' },
              { label: '企业', value: 'enterprise' }
            ]}
          />
          <TextField label="搜索目标" value={keyword} onChange={(event) => setKeyword(event.target.value)} />
          <TextField label="目标 ID" value={form.target_id} onChange={(event) => setForm({ ...form, target_id: event.target.value })} />
          <TextField label="积分数量" type="number" value={form.points} onChange={(event) => setForm({ ...form, points: event.target.value })} />
          <TextField
            label="积分过期时间"
            type="datetime-local"
            value={form.expires_at}
            onChange={(event) => setForm({ ...form, expires_at: event.target.value })}
          />
          <TextField label="发放原因" textarea rows={4} value={form.reason} onChange={(event) => setForm({ ...form, reason: event.target.value })} />
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
