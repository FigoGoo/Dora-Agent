import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { adminApi } from '../../lib/api/admin.js';
import { readListPayload, toApiDateTime } from '../../lib/format.js';
import { Alert } from '../../components/admin/Alert.jsx';
import { AuditHint } from '../../components/admin/AuditHint.jsx';
import { Button } from '../../components/admin/Button.jsx';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog.jsx';
import { DataTable } from '../../components/admin/DataTable.jsx';
import { Drawer } from '../../components/admin/Drawer.jsx';
import { FilterBar } from '../../components/admin/FilterBar.jsx';
import { Modal } from '../../components/admin/Modal.jsx';
import { PageHeader } from '../../components/admin/PageHeader.jsx';
import { Pagination } from '../../components/admin/Pagination.jsx';
import { SecretInput } from '../../components/admin/SecretInput.jsx';
import { SelectField } from '../../components/admin/SelectField.jsx';
import { TextField } from '../../components/admin/TextField.jsx';
import { useToast } from '../../components/admin/Toast.jsx';

function asLabel(value, row) {
  return typeof value === 'function' ? value(row) : value;
}

function rowActionVariant(action) {
  return action.tone === 'danger' ? 'danger-ghost' : action.tone || 'ghost';
}

function initialForm(fields = []) {
  return fields.reduce((acc, field) => {
    acc[field.name] = field.defaultValue ?? '';
    return acc;
  }, {});
}

export function resolveRowIdentifier(row, rowId) {
  if (!row) {
    return '';
  }
  if (typeof rowId === 'function') {
    return rowId(row) || row.id || row.key || '';
  }
  return row[rowId] || row.id || row.key || '';
}

export function prepareCreateBody(values, config) {
  const body = { ...values };
  for (const field of config.create?.fields || []) {
    if (field.type === 'datetime-local' && body[field.name]) {
      body[field.name] = toApiDateTime(body[field.name]);
    }
    if (field.type === 'number' && body[field.name] !== '') {
      body[field.name] = Number(body[field.name]);
    }
  }
  return body;
}

function isRecord(value) {
  return value && typeof value === 'object' && !Array.isArray(value);
}

function DetailValue({ value }) {
  if (Array.isArray(value)) {
    if (!value.length) {
      return '-';
    }
    return (
      <div className="admin-detail-stack">
        {value.map((item, index) => (
          <div key={isRecord(item) ? JSON.stringify(item) : `${item}-${index}`} className="admin-detail-nested">
            <DetailValue value={item} />
          </div>
        ))}
      </div>
    );
  }
  if (isRecord(value)) {
    return (
      <dl className="admin-detail-nested">
        {Object.entries(value).map(([key, nestedValue]) => (
          <div key={key}>
            <dt>{key}</dt>
            <dd>
              <DetailValue value={nestedValue} />
            </dd>
          </div>
        ))}
      </dl>
    );
  }
  return String(value ?? '-');
}

function RowDetails({ row }) {
  if (!row) {
    return null;
  }
  return (
    <dl className="admin-detail-list">
      {Object.entries(row).map(([key, value]) => (
        <div key={key}>
          <dt>{key}</dt>
          <dd>
            <DetailValue value={value} />
          </dd>
        </div>
      ))}
    </dl>
  );
}

function ResourceForm({ config, values, setValues }) {
  return (
    <div className="admin-form-grid">
      {config.create.fields.map((field) => {
        if (field.secret) {
          return (
            <SecretInput
              key={field.name}
              label={field.label}
              value={values[field.name] || ''}
              onChange={(event) => setValues({ ...values, [field.name]: event.target.value })}
            />
          );
        }
        if (field.options) {
          return (
            <SelectField
              key={field.name}
              label={field.label}
              value={values[field.name] || ''}
              onChange={(event) => setValues({ ...values, [field.name]: event.target.value })}
              options={field.options}
            />
          );
        }
        return (
          <TextField
            key={field.name}
            label={field.label}
            type={field.type || 'text'}
            textarea={field.textarea}
            rows={field.textarea ? 6 : undefined}
            value={values[field.name] || ''}
            onChange={(event) => setValues({ ...values, [field.name]: event.target.value })}
          />
        );
      })}
    </div>
  );
}

export function ResourceListPage({ config }) {
  const [filters, setFilters] = useState({ keyword: '', status: '', page_size: 10, page_token: '' });
  const [selected, setSelected] = useState(null);
  const [formOpen, setFormOpen] = useState(false);
  const [formValues, setFormValues] = useState(() => initialForm(config.create?.fields));
  const [confirm, setConfirm] = useState(null);
  const [notice, setNotice] = useState(null);
  const queryClient = useQueryClient();
  const toast = useToast();

  const queryParams = useMemo(
    () => ({
      page_size: filters.page_size,
      page_token: filters.page_token,
      status: filters.status,
      keyword: filters.keyword
    }),
    [filters]
  );

  const query = useQuery({
    queryKey: ['admin-resource', config.key, queryParams],
    queryFn: () => adminApi.list(config.listPath, queryParams)
  });
  const payload = readListPayload(query.data);
  const rows = payload.items;
  const tableState = query.isLoading ? 'loading' : query.isError ? 'error' : rows.length ? 'success' : 'empty';
  const selectedId = resolveRowIdentifier(selected, config.rowId);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['admin-resource', config.key] });
  const createMutation = useMutation({
    mutationFn: (body) => adminApi.post(config.create.path, body, { reason: body.reason }),
    onSuccess: () => {
      setFormOpen(false);
      setFormValues(initialForm(config.create?.fields));
      toast?.notify('已保存');
      refresh();
    },
    onError: (error) => setNotice(error)
  });
  const actionMutation = useMutation({
    mutationFn: async ({ action, row, reason, previewToken }) => {
      if (action.confirm) {
        return action.confirm(row, { reason, previewToken, row });
      }
      return adminApi.post(action.confirmPath(row), action.body?.({ reason, row }) || {}, { reason: action.reason?.({ reason, row }) || reason });
    },
    onSuccess: () => {
      setConfirm(null);
      toast?.notify('操作已完成，已记录审计');
      refresh();
    },
    onError: (error) => setNotice(error)
  });

  async function openAction(action, row) {
    setNotice(null);
    if (action.preview) {
      setConfirm({ action, row, previewLoading: true });
      try {
        const preview = await action.preview(row, '后台操作预览');
        setConfirm({ action, row, preview, previewToken: preview.preview_token });
      } catch (error) {
        setNotice(error);
        setConfirm(null);
      }
      return;
    }
    setConfirm({ action, row });
  }

  const columns = [
    ...config.columns,
    ...(config.actions?.length || config.detailPath
      ? [
          {
            key: '__actions',
            title: '操作',
            width: 220,
            render: (row) => (
              <div className="admin-row-actions">
                {config.detailPath ? (
                  <Button type="button" variant="ghost" size="row" onClick={() => setSelected(row)}>
                    详情
                  </Button>
                ) : null}
                {config.actions?.map((action) => (
                  <Button key={asLabel(action.label, row)} type="button" variant={rowActionVariant(action)} size="row" onClick={() => openAction(action, row)}>
                    {asLabel(action.label, row)}
                  </Button>
                ))}
              </div>
            )
          }
        ]
      : [])
  ];

  const detailQuery = useQuery({
    queryKey: ['admin-resource-detail', config.key, selectedId],
    enabled: Boolean(selected && config.detailPath),
    queryFn: () => adminApi.get(config.detailPath(selected))
  });

  function submitCreate(event) {
    event.preventDefault();
    createMutation.mutate(prepareCreateBody(formValues, config));
  }

  return (
    <>
      <PageHeader
        title={config.title}
        description={config.description}
        actions={
          config.create ? (
            <Button type="button" variant="primary" icon={Plus} onClick={() => setFormOpen(true)}>
              新增
            </Button>
          ) : null
        }
      />
      {notice ? (
        <Alert tone="danger" title="操作失败" traceId={notice.traceId}>
          {notice.message}
        </Alert>
      ) : null}
      <FilterBar
        keyword={filters.keyword}
        onKeywordChange={(keyword) => setFilters({ ...filters, keyword, page_token: '' })}
        status={filters.status}
        onStatusChange={(status) => setFilters({ ...filters, status, page_token: '' })}
        statusOptions={config.statusOptions || []}
        onClear={() => setFilters({ keyword: '', status: '', page_size: 10, page_token: '' })}
      />
      <DataTable columns={columns} rows={rows} rowKey={config.rowId} state={tableState} emptyText={config.emptyText} errorText={query.error?.message} />
      <Pagination
        pageSize={filters.page_size}
        total={payload.total}
        pageToken={payload.nextPageToken}
        previousDisabled={!filters.page_token}
        nextDisabled={rows.length < filters.page_size}
        onPageSizeChange={(page_size) => setFilters({ ...filters, page_size, page_token: '' })}
        onPrevious={() => setFilters({ ...filters, page_token: '' })}
        onNext={() => setFilters({ ...filters, page_token: String((Number(filters.page_token) || 0) + filters.page_size) })}
      />
      <Drawer open={Boolean(selected)} title={`${config.title}详情`} onClose={() => setSelected(null)}>
        {detailQuery.isLoading ? <p>加载中...</p> : null}
        {detailQuery.isError ? (
          <Alert tone="danger" title="详情加载失败" traceId={detailQuery.error.traceId}>
            {detailQuery.error.message}
          </Alert>
        ) : null}
        <RowDetails row={detailQuery.data || selected} />
      </Drawer>
      <Modal
        open={formOpen}
        title={config.create?.title}
        onClose={() => setFormOpen(false)}
        size="lg"
        footer={
          <>
            <Button type="button" variant="secondary" onClick={() => setFormOpen(false)}>
              取消
            </Button>
            <Button type="submit" form="admin-resource-form" variant="primary" loading={createMutation.isPending}>
              保存
            </Button>
          </>
        }
      >
        <form id="admin-resource-form" onSubmit={submitCreate}>
          <ResourceForm config={config} values={formValues} setValues={setFormValues} />
          <AuditHint />
        </form>
      </Modal>
      <ConfirmDialog
        open={Boolean(confirm)}
        title={asLabel(confirm?.action?.label || '确认操作', confirm?.row)}
        tone={confirm?.action?.tone || 'danger'}
        objectLabel={confirm?.action?.objectLabel?.(confirm?.row) || resolveRowIdentifier(confirm?.row, config.rowId) || config.title}
        impactItems={
          typeof confirm?.action?.impactItems === 'function'
            ? confirm.action.impactItems(confirm.preview)
            : confirm?.action?.impactItems || ['操作会写入审计日志。']
        }
        previewToken={confirm?.previewToken}
        loading={actionMutation.isPending || confirm?.previewLoading}
        onClose={() => setConfirm(null)}
        onConfirm={({ reason, previewToken }) => actionMutation.mutate({ action: confirm.action, row: confirm.row, reason, previewToken })}
      />
    </>
  );
}
