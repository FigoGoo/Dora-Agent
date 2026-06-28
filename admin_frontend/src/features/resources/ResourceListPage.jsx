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

function toFormDateTime(value) {
  if (!value) {
    return '';
  }
  const parsed = new Date(value);
  if (!Number.isFinite(parsed.getTime())) {
    return '';
  }
  return parsed.toISOString().slice(0, 16);
}

function valueFromSource(field, source = {}) {
  const raw = source[field.source || field.name] ?? field.defaultValue ?? '';
  if (field.type === 'checkbox') {
    return Boolean(raw);
  }
  if (field.type === 'datetime-local') {
    return toFormDateTime(raw);
  }
  if (field.type === 'json' || field.type === 'json-string') {
    if (!raw) {
      return field.defaultValue ?? '';
    }
    return typeof raw === 'string' ? raw : JSON.stringify(raw, null, 2);
  }
  if (field.type === 'array') {
    return Array.isArray(raw) ? raw.join('\n') : raw;
  }
  return raw;
}

function initialForm(fields = [], source = {}) {
  return fields.reduce((acc, field) => {
    acc[field.name] = valueFromSource(field, source);
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

function parseArrayValue(value) {
  if (Array.isArray(value)) {
    return value;
  }
  return String(value || '')
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function prepareBody(values, fields = []) {
  const body = {};
  for (const field of fields) {
    let value = values[field.name];
    if ((value === '' || value === undefined || value === null) && !field.required && !field.includeEmpty) {
      continue;
    }
    if (field.type === 'datetime-local' && value) {
      value = toApiDateTime(value);
    }
    if (field.type === 'number' && value !== '') {
      value = Number(value);
    }
    if (field.type === 'checkbox') {
      value = Boolean(value);
    }
    if (field.type === 'json' && value) {
      value = JSON.parse(value);
    }
    if (field.type === 'json-string' && value) {
      JSON.parse(value);
      value = String(value);
    }
    if (field.type === 'array') {
      value = parseArrayValue(value);
    }
    body[field.name] = value;
  }
  return body;
}

export function prepareCreateBody(values, config) {
  return prepareBody(values, config.create?.fields || []);
}

function initialFilters(config) {
  const filters = { page_size: 10, page_token: '' };
  if (config.keywordFilter !== false) {
    filters.keyword = '';
  }
  if (config.statusOptions?.length) {
    filters.status = '';
  }
  for (const field of config.filters || []) {
    filters[field.name] = field.defaultValue ?? '';
  }
  return filters;
}

function buildQueryParams(filters, config) {
  const params = {
    page_size: filters.page_size,
    page_token: filters.page_token
  };
  if (config.keywordFilter !== false && filters.keyword) {
    params[config.keywordParam || 'keyword'] = filters.keyword;
  }
  if (config.statusOptions?.length && filters.status) {
    params.status = filters.status;
  }
  for (const field of config.filters || []) {
    const value = filters[field.name];
    if (value !== undefined && value !== null && value !== '') {
      params[field.queryName || field.name] = value;
    }
  }
  return params;
}

function apiMethod(method) {
  const key = method.toLowerCase();
  return adminApi[key] || adminApi.post;
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

function ResourceForm({ fields = [], values, setValues }) {
  return (
    <div className="admin-form-grid">
      {fields.map((field) => {
        if (field.type === 'checkbox') {
          return (
            <label key={field.name} className="admin-check-field">
              <input
                type="checkbox"
                checked={Boolean(values[field.name])}
                onChange={(event) => setValues({ ...values, [field.name]: event.target.checked })}
              />
              <span>{field.label}</span>
            </label>
          );
        }
        if (field.secret) {
          return (
            <SecretInput
              key={field.name}
              label={field.label}
              value={values[field.name] || ''}
              onChange={(event) => setValues({ ...values, [field.name]: event.target.value })}
              required={field.required}
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
            type={field.type === 'json' || field.type === 'json-string' || field.type === 'array' ? 'text' : field.type || 'text'}
            textarea={field.textarea || field.type === 'json' || field.type === 'json-string' || field.type === 'array'}
            rows={field.rows || (field.type === 'json' || field.type === 'json-string' ? 8 : field.type === 'array' ? 4 : field.textarea ? 6 : undefined)}
            hint={field.hint}
            value={values[field.name] || ''}
            onChange={(event) => setValues({ ...values, [field.name]: event.target.value })}
            required={field.required}
          />
        );
      })}
    </div>
  );
}

function FilterFields({ fields = [], values, setValues }) {
  return fields.map((field) => {
    const value = values[field.name] || '';
    if (field.options) {
      return (
        <SelectField
          key={field.name}
          label={field.label}
          value={value}
          onChange={(event) => setValues({ ...values, [field.name]: event.target.value, page_token: '' })}
          options={[{ label: field.allLabel || '全部', value: '' }, ...field.options]}
        />
      );
    }
    return (
      <TextField
        key={field.name}
        label={field.label}
        type={field.type || 'text'}
        value={value}
        onChange={(event) => setValues({ ...values, [field.name]: event.target.value, page_token: '' })}
      />
    );
  });
}

export function ResourceListPage({ config }) {
  const [filters, setFilters] = useState(() => initialFilters(config));
  const [selected, setSelected] = useState(null);
  const [formOpen, setFormOpen] = useState(false);
  const [formValues, setFormValues] = useState(() => initialForm(config.create?.fields));
  const [actionForm, setActionForm] = useState(null);
  const [confirm, setConfirm] = useState(null);
  const [notice, setNotice] = useState(null);
  const queryClient = useQueryClient();
  const toast = useToast();

  const queryParams = useMemo(() => buildQueryParams(filters, config), [filters, config]);

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
    mutationFn: async ({ action, row, reason, previewToken, values }) => {
      if (action.confirm) {
        return action.confirm(row, { reason, previewToken, row, values });
      }
      const fields = action.fields || [];
      const body = action.body?.({ reason, row, values, previewToken }) || prepareBody(values || {}, fields);
      const requestReason = action.reason?.({ reason, row, values, body }) || reason || values?.reason;
      const options = {
        reason: requestReason,
        idempotencyKey: action.idempotencyKey?.({ row, values, body })
      };
      return apiMethod(action.method || 'POST')(action.path?.(row) || action.confirmPath(row), body, options);
    },
    onSuccess: (data, variables) => {
      setConfirm(null);
      setActionForm(null);
      toast?.notify(variables.action.successNotice?.(data) || '操作已完成，已记录审计');
      refresh();
    },
    onError: (error) => setNotice(error)
  });

  async function openAction(action, row) {
    setNotice(null);
    if (action.fields?.length) {
      setActionForm({ action, row, values: initialForm(action.fields, row), error: null });
      return;
    }
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
    try {
      createMutation.mutate(prepareCreateBody(formValues, config));
    } catch (error) {
      setNotice(error);
    }
  }

  function submitActionForm(event) {
    event.preventDefault();
    try {
      actionMutation.mutate({ action: actionForm.action, row: actionForm.row, values: actionForm.values });
    } catch (error) {
      setActionForm({ ...actionForm, error });
    }
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
        showKeyword={config.keywordFilter !== false}
        keyword={filters.keyword}
        onKeywordChange={(keyword) => setFilters({ ...filters, keyword, page_token: '' })}
        status={filters.status}
        onStatusChange={(status) => setFilters({ ...filters, status, page_token: '' })}
        statusOptions={config.statusOptions || []}
        onClear={() => setFilters(initialFilters(config))}
      >
        <FilterFields fields={config.filters} values={filters} setValues={setFilters} />
      </FilterBar>
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
          <ResourceForm fields={config.create?.fields || []} values={formValues} setValues={setFormValues} />
          <AuditHint />
        </form>
      </Modal>
      <Modal
        open={Boolean(actionForm)}
        title={actionForm?.action?.formTitle || asLabel(actionForm?.action?.label || '后台操作', actionForm?.row)}
        onClose={() => setActionForm(null)}
        size="lg"
        footer={
          <>
            <Button type="button" variant="secondary" onClick={() => setActionForm(null)}>
              取消
            </Button>
            <Button type="submit" form="admin-action-form" variant={actionForm?.action?.tone === 'danger' ? 'danger' : 'primary'} loading={actionMutation.isPending}>
              保存
            </Button>
          </>
        }
      >
        {actionForm?.error ? (
          <Alert tone="danger" title="表单格式错误">
            {actionForm.error.message}
          </Alert>
        ) : null}
        <form id="admin-action-form" onSubmit={submitActionForm}>
          <ResourceForm
            fields={actionForm?.action?.fields || []}
            values={actionForm?.values || {}}
            setValues={(values) => setActionForm({ ...actionForm, values, error: null })}
          />
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
        requireReason={confirm?.action?.requireReason !== false}
        loading={actionMutation.isPending || confirm?.previewLoading}
        onClose={() => setConfirm(null)}
        onConfirm={({ reason, previewToken }) => actionMutation.mutate({ action: confirm.action, row: confirm.row, reason, previewToken })}
      />
    </>
  );
}
