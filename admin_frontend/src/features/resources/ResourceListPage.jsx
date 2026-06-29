import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { Link, useSearchParams } from 'react-router-dom';
import { adminApi } from '../../services/adminApi.js';
import { readListPayload, toApiDateTime } from '../../utils/format.js';
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
import { SkillMarkdownEditor } from '../../components/admin/SkillMarkdownEditor.jsx';
import { TextField } from '../../components/admin/TextField.jsx';
import { useToast } from '../../components/admin/Toast.jsx';

function asLabel(value, row) {
  return typeof value === 'function' ? value(row) : value;
}

function rowActionVariant(action) {
  return action.tone === 'danger' ? 'danger-ghost' : action.tone || 'ghost';
}

export function visibleRowActions(actions = [], row) {
  return actions.filter((action) => action.visible?.(row) !== false);
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

export function initialForm(fields = [], source = {}) {
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
    if (field.virtual) {
      continue;
    }
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
  const fields = config.create?.fields || [];
  const body = prepareBody(values, fields);
  return config.create?.body ? config.create.body({ values, body, fields }) : body;
}

function requiredFieldEmpty(field, value) {
  if (field.type === 'checkbox') {
    return !value;
  }
  if (Array.isArray(value)) {
    return value.length === 0;
  }
  return value === undefined || value === null || String(value).trim() === '';
}

export function validateRequiredFields(fields = [], values = {}) {
  return fields.reduce((errors, field) => {
    if (!field.required || field.virtual) {
      return errors;
    }
    if (requiredFieldEmpty(field, values[field.name])) {
      errors[field.name] = `请填写${field.label || field.name}`;
    }
    return errors;
  }, {});
}

function removeFieldError(errors = {}, name) {
  if (!errors[name]) {
    return errors;
  }
  const next = { ...errors };
  delete next[name];
  return next;
}

function hasErrors(errors = {}) {
  return Object.keys(errors).length > 0;
}

function notifyError(toast, error, title = '操作失败') {
  toast?.notify(error?.message || '请求失败，请稍后重试。', 'danger', {
    title,
    traceId: error?.traceId
  });
}

export function initialFilters(config, searchParams) {
  const filters = { page_size: config.defaultPageSize || 10, page_token: '' };
  if (config.keywordFilter !== false) {
    filters.keyword = searchParams?.get('keyword') || '';
  }
  if (config.statusOptions?.length) {
    filters.status = searchParams?.get('status') || '';
  }
  if (config.linkedPanel?.field) {
    filters[config.linkedPanel.field] = searchParams?.get(config.linkedPanel.queryName || config.linkedPanel.field) || config.linkedPanel.defaultValue || '';
  }
  for (const field of config.filters || []) {
    filters[field.name] = searchParams?.get(field.queryName || field.name) || field.defaultValue || '';
  }
  return filters;
}

function fieldsUseOptionSource(fields = [], source) {
  return fields.some((field) => field.optionSource === source);
}

function configUsesOptionSource(config, source) {
  return (
    config.linkedPanel?.source === source ||
    config.linkedPanel?.source === `${source}Items` ||
    fieldsUseOptionSource(config.filters, source) ||
    fieldsUseOptionSource(config.create?.fields, source) ||
    (config.actions || []).some((action) => fieldsUseOptionSource(action.fields, source))
  );
}

export function modelProviderItems(data) {
  return readListPayload(data).items.map((provider) => {
    const label = provider.provider_name || provider.display_name || provider.provider_code || provider.provider_id;
    return {
      label,
      value: provider.provider_id,
      meta: provider.provider_code || provider.provider_id
    };
  });
}

function modelProviderOptions(data) {
  return modelProviderItems(data).map((provider) => ({
    label: provider.meta && provider.meta !== provider.label ? `${provider.label} (${provider.meta})` : provider.label,
    value: provider.value
  }));
}

function fieldOptions(field, optionContext = {}) {
  if (field.optionSource) {
    return optionContext[field.optionSource] || [];
  }
  return field.options || [];
}

function rowGroupValue(row, groupBy) {
  return String(row?.[groupBy?.field] || groupBy?.fallbackValue || '未分类');
}

export function buildGroupItems(rows = [], groupBy) {
  if (!groupBy) {
    return [];
  }
  const counts = new Map();
  for (const row of rows) {
    const value = rowGroupValue(row, groupBy);
    counts.set(value, (counts.get(value) || 0) + 1);
  }
  const items = Array.from(counts.entries())
    .map(([value, count]) => ({
      value,
      count,
      label: groupBy.label?.(value) || value
    }))
    .sort((left, right) => left.label.localeCompare(right.label));
  return [{ value: '', label: groupBy.allLabel || '全部', count: rows.length }, ...items];
}

export function filterRowsByGroup(rows = [], groupBy, selectedGroup = '') {
  if (!groupBy || !selectedGroup) {
    return rows;
  }
  return rows.filter((row) => rowGroupValue(row, groupBy) === selectedGroup);
}

export function buildQueryParams(filters, config) {
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
  if (config.linkedPanel?.field) {
    const value = filters[config.linkedPanel.field];
    if (value !== undefined && value !== null && value !== '') {
      params[config.linkedPanel.queryName || config.linkedPanel.field] = value;
    }
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

const DETAIL_LABELS = {
  account: '管理员账号',
  account_type: '账户类型',
  action: '操作',
  actor_id: '操作人',
  active: '启用',
  admin_id: '管理员 ID',
  allowed: '允许使用',
  audit_id: '审计 ID',
  base_url: 'Base URL',
  batch_id: '批次 ID',
  batch_no: '批次号',
  billing_unit: '计费单位',
  bind_target_id: '绑定对象 ID',
  bind_target_type: '绑定类型',
  capability_tags: '能力标签',
  category: '分类',
  channel: '渠道',
  charge_mode: '计费模式',
  code_expires_at: '兑换码过期时间',
  confirmation_policy_json: '确认策略',
  config: '扩展配置',
  count: '数量',
  created_at: '创建时间',
  created_by: '创建人',
  creator_id: '创建者',
  credential_id: '凭证 ID',
  credit_expires_at: '积分过期时间',
  current_status: '当前状态',
  cancel_policy: '取消策略',
  default_for_resource: '默认资源类型',
  description: '说明',
  display_order: '展示顺序',
  display_slot: '展示位置',
  display_name: '展示名称',
  draft_enabled: '草稿态可用',
  editable: '可编辑',
  element_name: '元素名称',
  element_type: '元素类型',
  email_masked: '邮箱',
  enterprise_id: '企业 ID',
  enterprise_memberships: '企业成员关系',
  error_code: '错误码',
  execution_policy_summary_json: '执行策略摘要',
  expires_at: '过期时间',
  export_job_id: '导出任务 ID',
  final_enabled: '最终态可用',
  free_quota: '免费额度',
  impact_summary: '影响说明',
  input_schema_json: '输入 Schema',
  is_default: '默认',
  last_login_at: '最近登录时间',
  metadata_summary: '摘要',
  memory_policy_json: 'Memory 策略',
  min_charge_points: '最低扣费积分',
  model_code: '模型编码',
  model_id: '模型 ID',
  must_rotate_password: '需改密',
  operator_id_masked: '操作人',
  operator_type: '操作人类型',
  output_elements: '输出元素',
  output_schema_json: '输出 Schema',
  phone_masked: '手机号',
  points: '积分',
  personal_space_id: '个人空间 ID',
  pricing_snapshot_id: '价格快照 ID',
  provider_code: '供应商编码',
  provider_id: '供应商 ID',
  provider_name: '供应商名称',
  provider_type: '供应商类型',
  public_nickname: '公开昵称',
  public_work_id: '公开作品 ID',
  published_at: '发布时间',
  published_version_id: '发布版本',
  recent_audit_refs: '最近审计引用',
  referable: '可引用',
  registered_at: '注册时间',
  reason: '原因',
  render_hint: '渲染提示',
  render_hint_json: '渲染提示 JSON',
  required: '必填',
  requires_confirmation: '需要确认',
  resource_id: '资源 ID',
  resource_type: '资源类型',
  result: '结果',
  retry_policy: '重试策略',
  risk_level: '风险等级',
  role: '角色',
  route_config: '路由配置',
  route_hints: '路由提示',
  safety_evidence_json: '安全证据',
  schema_hint_json: 'Schema 提示 JSON',
  schema_json: 'Schema JSON',
  schema_version: 'Schema 版本',
  secret_ref_status: '密钥状态',
  skill_id: 'Skill ID',
  skill_key: 'Skill Key',
  skill_name: 'Skill 名称',
  skill_scope: 'Skill 范围',
  skill_spec_json: 'Skill 定义',
  sort_order: '排序',
  spaces: '空间',
  space_id: '空间 ID',
  space_type: '空间类型',
  status: '状态',
  summary: '摘要',
  submitted_at: '提交时间',
  tag: '标签',
  target_id: '目标 ID',
  target_type: '目标类型',
  target_status: '目标状态',
  test_case_id: '测试用例 ID',
  test_reason: '测试原因',
  test_run_id: '测试运行 ID',
  title: '标题',
  tool_key: 'Tool Key',
  tool_name: 'Tool 名称',
  tool_type: 'Tool 类型',
  trace_id: 'Trace ID',
  timeout_ms: '超时毫秒',
  unit_points: '用户积分单价',
  updated_at: '更新时间',
  updated_by: '更新人',
  usage_stage: '使用阶段',
  use_draft: '使用草稿态',
  use_final: '使用最终态',
  user_id: '用户 ID',
  version: '版本',
  version_id: '版本 ID'
};

const HIDDEN_DETAIL_KEYS = new Set(['access_token', 'csrf_token', 'password_hash', 'secret_key_ref']);
const DETAIL_FIRST_KEYS = [
  'id',
  'admin_id',
  'user_id',
  'skill_id',
  'model_id',
  'provider_id',
  'tool_key',
  'public_work_id',
  'batch_id',
  'audit_id',
  'title',
  'display_name',
  'skill_name',
  'provider_name',
  'tool_name',
  'status'
];

function isRecord(value) {
  return value && typeof value === 'object' && !Array.isArray(value);
}

function fallbackLabel(key) {
  if (!key) {
    return '-';
  }
  return key
    .split('_')
    .map((part) => part.toUpperCase())
    .join(' ');
}

function detailLabel(key, labels = {}) {
  return labels[key] || DETAIL_LABELS[key] || fallbackLabel(key);
}

function applyFieldLabels(labels, fields = []) {
  for (const field of fields) {
    if (field?.name && field?.label) {
      labels[field.name] = field.label;
    }
  }
}

export function buildDetailLabels(config = {}) {
  const labels = { ...DETAIL_LABELS, ...config.detailLabels };
  for (const column of config.columns || []) {
    if (column?.key && typeof column.title === 'string') {
      labels[column.key] = column.title;
    }
  }
  applyFieldLabels(labels, config.filters);
  applyFieldLabels(labels, config.create?.fields);
  for (const action of config.actions || []) {
    applyFieldLabels(labels, action.fields);
  }
  return labels;
}

function booleanText(value) {
  return value ? '是' : '否';
}

const DETAIL_VALUE_LABELS = {
  status: {
    active: '启用',
    disabled: '停用',
    draft: '草稿',
    submitted: '待审核',
    pending_review: '待审核',
    published: '已发布',
    deprecated: '废弃',
    rejected: '已拒绝',
    passed: '通过',
    failed: '失败',
    blocked: '已拦截',
    taken_down: '已下架',
    cancelled: '已取消'
  },
  resource_type: {
    image: '图片',
    music: '音乐',
    audio: '音频',
    video: '视频',
    text: '文本',
    data: '数据'
  },
  tool_type: {
    builtin: '内置 Tool',
    http: 'HTTP Tool',
    rpc: 'RPC Tool',
    external: '外部 Tool',
    model_generation: '模型生成 Tool',
    browser: '浏览器 Tool',
    image_edit: '图片编辑 Tool'
  },
  risk_level: {
    low: '低风险',
    medium: '中风险',
    high: '高风险'
  },
  charge_mode: {
    free: '免费',
    per_call: '按调用',
    per_asset: '按资产',
    model_generation: '模型生成',
    tool_usage: 'Tool 用量',
    business_value: '业务价值'
  },
  billing_unit: {
    generation: '按次生成',
    image: '按图片',
    video: '按视频',
    audio: '按音频',
    call: '按调用',
    token: '按 Token',
    asset: '按资产'
  },
  provider_type: {
    openai_compatible: 'OpenAI 兼容',
    volcengine: '火山引擎',
    custom: '自定义'
  },
  skill_scope: {
    public: '公开',
    personal: '个人',
    enterprise: '企业'
  },
  scope_type: {
    space: '空间',
    enterprise: '企业',
    user: '用户'
  },
  operator_type: {
    admin: '管理员',
    user: '用户',
    system: '系统'
  },
  secret_ref_status: {
    configured: '已配置',
    missing: '未配置'
  }
};

function detailDisplayValue(key, value) {
  if (typeof value !== 'string') {
    return value;
  }
  return DETAIL_VALUE_LABELS[key]?.[value] || value;
}

function detailValueClass(value) {
  const valueText = typeof value === 'string' ? value : '';
  return valueText.includes('\n') || valueText.length > 80 ? 'admin-detail-value admin-detail-value--multiline' : 'admin-detail-value';
}

function normalizeDetailEntries(row, labels = {}) {
  return Object.entries(row || {})
    .filter(([key, value]) => !HIDDEN_DETAIL_KEYS.has(key) && value !== undefined)
    .sort(([left], [right]) => {
      const leftIndex = DETAIL_FIRST_KEYS.indexOf(left);
      const rightIndex = DETAIL_FIRST_KEYS.indexOf(right);
      if (leftIndex !== -1 || rightIndex !== -1) {
        return (leftIndex === -1 ? 999 : leftIndex) - (rightIndex === -1 ? 999 : rightIndex);
      }
      const leftKnown = labels[left] || DETAIL_LABELS[left];
      const rightKnown = labels[right] || DETAIL_LABELS[right];
      if (leftKnown && !rightKnown) {
        return -1;
      }
      if (!leftKnown && rightKnown) {
        return 1;
      }
      return left.localeCompare(right);
    });
}

function jsonLikeString(value) {
  if (typeof value !== 'string') {
    return '';
  }
  const trimmed = value.trim();
  if (!(trimmed.startsWith('{') || trimmed.startsWith('['))) {
    return '';
  }
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return '';
  }
}

function DetailValue({ name = '', value, labels = {} }) {
  if (value === null || value === undefined || value === '') {
    return '-';
  }
  if (typeof value === 'boolean') {
    return booleanText(value);
  }
  if (Array.isArray(value)) {
    if (!value.length) {
      return '-';
    }
    return (
      <div className="admin-detail-stack">
        {value.map((item, index) => (
          <div key={isRecord(item) ? JSON.stringify(item) : `${item}-${index}`} className="admin-detail-nested">
            <DetailValue value={item} labels={labels} />
          </div>
        ))}
      </div>
    );
  }
  if (isRecord(value)) {
    return (
      <dl className="admin-detail-nested">
        {normalizeDetailEntries(value, labels).map(([key, nestedValue]) => (
          <div key={key}>
            <dt>{detailLabel(key, labels)}</dt>
            <dd>
              <DetailValue name={key} value={nestedValue} labels={labels} />
            </dd>
          </div>
        ))}
      </dl>
    );
  }
  const displayValue = detailDisplayValue(name, value);
  const formattedJson = jsonLikeString(value);
  if (formattedJson) {
    return <pre className="admin-detail-code">{formattedJson}</pre>;
  }
  return <span className={detailValueClass(displayValue)}>{String(displayValue)}</span>;
}

export function RowDetails({ row, labels = {} }) {
  if (!row) {
    return null;
  }
  return (
    <dl className="admin-detail-list">
      {normalizeDetailEntries(row, labels).map(([key, value]) => (
        <div key={key}>
          <dt>{detailLabel(key, labels)}</dt>
          <dd>
            <DetailValue name={key} value={value} labels={labels} />
          </dd>
        </div>
      ))}
    </dl>
  );
}

function shouldUseTextarea(field) {
  if (field.singleLine) {
    return false;
  }
  if (field.textarea || field.type === 'json' || field.type === 'json-string' || field.type === 'array') {
    return true;
  }
  return /(description|summary|reason|comment|changelog|policy|config|schema|spec|prompt|evidence|metadata|notes?)/i.test(field.name);
}

function fieldRows(field) {
  if (field.rows) {
    return field.rows;
  }
  if (field.type === 'json' || field.type === 'json-string') {
    return 10;
  }
  if (field.type === 'array') {
    return 5;
  }
  if (shouldUseTextarea(field)) {
    return 4;
  }
  return undefined;
}

function fieldClassName(field) {
  const areaClass = field.area ? `admin-form-field--area-${String(field.area).replace(/[^a-z0-9_-]/gi, '')}` : '';
  return [
    'admin-form-field',
    areaClass,
    field.span ? `admin-form-field--${field.span}` : '',
    !field.area && field.span !== 'half' && (field.span === 'full' || shouldUseTextarea(field) || field.secret) ? 'admin-form-field--full' : ''
  ]
    .filter(Boolean)
    .join(' ');
}

function formSections(fields = []) {
  return fields.reduce((sections, field) => {
    const title = field.group || '';
    const layout = field.groupLayout || 'default';
    const hint = field.groupHint || '';
    const last = sections[sections.length - 1];
    if (!last || last.title !== title || last.layout !== layout) {
      sections.push({ title, layout, hint, fields: [field] });
      return sections;
    }
    last.fields.push(field);
    return sections;
  }, []);
}

function sectionClassName(section) {
  return ['admin-form-section', section.layout !== 'default' ? `admin-form-section--${section.layout}` : ''].filter(Boolean).join(' ');
}

export function createSourceFromFilters(config, filters = {}) {
  const field = config.linkedPanel?.field;
  if (!field || !filters[field]) {
    return {};
  }
  return { [field]: filters[field] };
}

export function ResourceForm({ fields = [], values, setValues, errors = {}, onFieldChange, optionContext = {} }) {
  const sections = formSections(fields);
  const fieldHint = (field) => {
    const lengthHint = field.maxLength ? `${String(values[field.name] || '').length}/${field.maxLength}` : '';
    return [field.hint, lengthHint].filter(Boolean).join(' · ');
  };
  const renderField = (field) => {
    const fieldError = errors[field.name];
    const updateField = (value) => {
      setValues({ ...values, [field.name]: value });
      onFieldChange?.(field.name);
    };
    const fieldNode = (() => {
      if (field.type === 'checkbox') {
        return (
          <label className="admin-check-field">
            <input
              type="checkbox"
              checked={Boolean(values[field.name])}
              disabled={field.disabled}
              onChange={(event) => updateField(event.target.checked)}
            />
            <span>{field.label}</span>
            {fieldError ? <small className="admin-field__error">{fieldError}</small> : null}
          </label>
        );
      }
      if (field.secret) {
        return (
          <SecretInput
            label={field.label}
            value={values[field.name] || ''}
            onChange={(event) => updateField(event.target.value)}
            required={field.required}
            error={fieldError}
            hint={fieldHint(field)}
          />
        );
      }
      const options = fieldOptions(field, optionContext);
      if (field.optionSource || options.length) {
        const selectOptions = options.length ? options : [{ label: '暂无可选项', value: '' }];
        return (
          <SelectField
            label={field.label}
            value={values[field.name] || ''}
            onChange={(event) => updateField(event.target.value)}
            options={selectOptions}
            disabled={field.disabled || !options.length}
            required={field.required}
            error={fieldError}
            hint={fieldHint(field)}
          />
        );
      }
      if (field.type === 'skill-markdown') {
        return (
          <SkillMarkdownEditor
            label={field.label}
            value={values[field.name] || ''}
            onChange={(event) => updateField(event.target.value)}
            rows={fieldRows(field)}
            hint={fieldHint(field)}
            required={field.required}
            error={fieldError}
          />
        );
      }
      return (
        <TextField
          label={field.label}
          type={field.type === 'json' || field.type === 'json-string' || field.type === 'array' ? 'text' : field.type || 'text'}
          textarea={shouldUseTextarea(field)}
          rows={fieldRows(field)}
          hint={fieldHint(field)}
          placeholder={field.placeholder}
          maxLength={field.maxLength}
          hideLabel={field.hideLabel}
          disabled={field.disabled}
          value={values[field.name] || ''}
          onChange={(event) => updateField(event.target.value)}
          required={field.required}
          error={fieldError}
        />
      );
    })();
    return (
      <div key={field.name} className={fieldClassName(field)}>
        {fieldNode}
      </div>
    );
  };

  return (
    <div className="admin-form-layout">
      {sections.map((section, index) => (
        <section key={`${section.title || 'default'}-${index}`} className={sectionClassName(section)}>
          {section.title ? <h3 className="admin-form-section-title">{section.title}</h3> : null}
          {section.hint ? <p className="admin-form-section-copy">{section.hint}</p> : null}
          <div className="admin-form-grid">{section.fields.map(renderField)}</div>
        </section>
      ))}
    </div>
  );
}

function FilterFields({ fields = [], values, setValues, optionContext = {} }) {
  return fields.map((field) => {
    const value = values[field.name] || '';
    const options = fieldOptions(field, optionContext);
    if (field.optionSource || options.length) {
      return (
        <SelectField
          key={field.name}
          label={field.label}
          value={value}
          onChange={(event) => setValues({ ...values, [field.name]: event.target.value, page_token: '' })}
          options={[{ label: field.allLabel || '全部', value: '' }, ...options]}
          disabled={field.disabled}
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

function sourceItems(source, optionContext = {}) {
  return optionContext[source] || [];
}

function linkedPanelSummary(panel, selectedValue, rows) {
  if (selectedValue) {
    return panel.selectedSummary?.(rows.length) || `当前供应商 ${rows.length} 个模型`;
  }
  return panel.allSummary?.(rows.length) || `当前筛选共 ${rows.length} 个模型`;
}

function LinkedResourcePanel({ panel, items = [], selectedValue = '', onSelect, loading, error }) {
  const selectedItem = items.find((item) => item.value === selectedValue);
  return (
    <aside className="admin-resource-group-panel admin-resource-group-panel--linked" aria-label={panel.title || '关联资源'}>
      <div className="admin-resource-group-panel__head">
        <span>{panel.title || '关联资源'}</span>
        <strong>{items.length}</strong>
      </div>
      <div className="admin-resource-group-list admin-resource-link-list">
        <button type="button" className={!selectedValue ? 'is-active' : ''} onClick={() => onSelect('')}>
          <span>{panel.allLabel || '全部'}</span>
        </button>
        {loading ? <p className="admin-resource-link-state">供应商加载中...</p> : null}
        {error ? <p className="admin-resource-link-state admin-resource-link-state--danger">供应商加载失败</p> : null}
        {!loading && !error && !items.length ? <p className="admin-resource-link-state">{panel.emptyText || '暂无供应商'}</p> : null}
        {items.map((item) => (
          <button key={item.value} type="button" className={item.value === selectedValue ? 'is-active' : ''} onClick={() => onSelect(item.value)}>
            <span>{item.label}</span>
          </button>
        ))}
        {selectedValue && !selectedItem && !loading ? <p className="admin-resource-link-state">当前供应商不在候选列表中</p> : null}
      </div>
    </aside>
  );
}

export function ResourceListPage({ config }) {
  const [searchParams] = useSearchParams();
  const [filters, setFilters] = useState(() => initialFilters(config, searchParams));
  const [selected, setSelected] = useState(null);
  const [selectedGroup, setSelectedGroup] = useState('');
  const [formOpen, setFormOpen] = useState(false);
  const [formValues, setFormValues] = useState(() => initialForm(config.create?.fields));
  const [formErrors, setFormErrors] = useState({});
  const [actionForm, setActionForm] = useState(null);
  const [confirm, setConfirm] = useState(null);
  const queryClient = useQueryClient();
  const toast = useToast();
  const needsModelProviders = configUsesOptionSource(config, 'modelProviders');
  const providerOptionsQuery = useQuery({
    queryKey: ['admin-option-source', 'model-providers'],
    queryFn: () => adminApi.list('/api/admin/models/providers', { page_size: 100 }),
    enabled: needsModelProviders,
    staleTime: 60_000
  });
  const providerItems = useMemo(() => modelProviderItems(providerOptionsQuery.data), [providerOptionsQuery.data]);
  const optionContext = useMemo(
    () => ({
      modelProviders: modelProviderOptions(providerOptionsQuery.data),
      modelProviderItems: providerItems
    }),
    [providerOptionsQuery.data, providerItems]
  );

  const queryParams = useMemo(() => buildQueryParams(filters, config), [filters, config]);
  const detailLabels = useMemo(() => buildDetailLabels(config), [config]);

  const query = useQuery({
    queryKey: ['admin-resource', config.key, queryParams],
    queryFn: () => adminApi.list(config.listPath, queryParams)
  });
  const payload = readListPayload(query.data);
  const rows = payload.items;
  const groupItems = useMemo(() => buildGroupItems(rows, config.groupBy), [rows, config.groupBy]);
  const visibleRows = useMemo(() => filterRowsByGroup(rows, config.groupBy, selectedGroup), [rows, config.groupBy, selectedGroup]);
  const activeGroup = groupItems.find((item) => item.value === selectedGroup);
  const linkedItems = config.linkedPanel ? sourceItems(config.linkedPanel.source, optionContext) : [];
  const selectedLinkedValue = config.linkedPanel?.field ? filters[config.linkedPanel.field] || '' : '';
  const selectedLinkedItem = linkedItems.find((item) => item.value === selectedLinkedValue);
  const tableState = query.isLoading ? 'loading' : query.isError ? 'error' : visibleRows.length ? 'success' : 'empty';
  const selectedId = resolveRowIdentifier(selected, config.rowId);
  const canShowDetails = Boolean(config.detailPath || config.detail);

  const createInitialValues = () => initialForm(config.create?.fields, createSourceFromFilters(config, filters));
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['admin-resource', config.key] });
  const createMutation = useMutation({
    mutationFn: (body) => adminApi.post(config.create.path, body),
    onSuccess: () => {
      setFormOpen(false);
      setFormValues(createInitialValues());
      toast?.notify('已保存');
      refresh();
    },
    onError: (error) => notifyError(toast, error, '保存失败')
  });
  const actionMutation = useMutation({
    mutationFn: async ({ action, row, reason, previewToken, values }) => {
      if (action.confirm) {
        return action.confirm(row, { reason, previewToken, row, values });
      }
      const fields = action.fields || [];
      const body = action.body?.({ reason, row, values, previewToken }) || prepareBody(values || {}, fields);
      const options = {
        idempotencyKey: action.idempotencyKey?.({ row, values, body }),
        headers: action.headers?.({ row, values, body })
      };
      return apiMethod(action.method || 'POST')(action.path?.(row) || action.confirmPath(row), body, options);
    },
    onSuccess: (data, variables) => {
      setConfirm(null);
      setActionForm(null);
      toast?.notify(variables.action.successNotice?.(data) || '操作已完成，已记录审计');
      refresh();
    },
    onError: (error) => notifyError(toast, error)
  });

  async function openAction(action, row) {
    if (action.fields?.length) {
      setActionForm({ action, row, values: action.initialValues?.(row) || initialForm(action.fields, row), error: null, errors: {} });
      return;
    }
    if (action.preview) {
      setConfirm({ action, row, previewLoading: true });
      try {
        const preview = await action.preview(row, '后台操作预览');
        setConfirm({ action, row, preview, previewToken: preview.preview_token });
      } catch (error) {
        notifyError(toast, error, '预览失败');
        setConfirm(null);
      }
      return;
    }
    setConfirm({ action, row });
  }

  const columns = [
    ...config.columns,
    ...(config.actions?.length || canShowDetails
      ? [
          {
            key: '__actions',
            title: '操作',
            width: 220,
            render: (row) => (
              <div className="admin-row-actions">
                {canShowDetails ? (
                  <Button type="button" variant="ghost" size="row" onClick={() => setSelected(row)}>
                    详情
                  </Button>
                ) : null}
                {visibleRowActions(config.actions, row).map((action) => (
                  action.to ? (
                    <Link key={asLabel(action.label, row)} className={`admin-btn admin-btn--${rowActionVariant(action)} admin-btn--row`} to={action.to(row)}>
                      <span>{asLabel(action.label, row)}</span>
                    </Link>
                  ) : (
                    <Button key={asLabel(action.label, row)} type="button" variant={rowActionVariant(action)} size="row" onClick={() => openAction(action, row)}>
                      {asLabel(action.label, row)}
                    </Button>
                  )
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
    const errors = validateRequiredFields(config.create?.fields || [], formValues);
    if (hasErrors(errors)) {
      setFormErrors(errors);
      toast?.notify('请补全必填字段后再保存。', 'warning', { title: '表单未完成' });
      return;
    }
    setFormErrors({});
    try {
      createMutation.mutate(prepareCreateBody(formValues, config));
    } catch (error) {
      notifyError(toast, error, '表单格式错误');
    }
  }

  function submitActionForm(event) {
    event.preventDefault();
    const errors = validateRequiredFields(actionForm?.action?.fields || [], actionForm?.values || {});
    if (hasErrors(errors)) {
      setActionForm({ ...actionForm, errors });
      toast?.notify('请补全必填字段后再保存。', 'warning', { title: '表单未完成' });
      return;
    }
    try {
      actionMutation.mutate({ action: actionForm.action, row: actionForm.row, values: actionForm.values });
    } catch (error) {
      setActionForm({ ...actionForm, error });
    }
  }

  function selectLinkedValue(value) {
    if (!config.linkedPanel?.field) {
      return;
    }
    setFilters({ ...filters, [config.linkedPanel.field]: value, page_token: '' });
  }

  return (
    <>
      <PageHeader
        title={config.title}
        description={config.description}
        actions={
          config.create ? (
            config.create.pagePath ? (
              <Link className="admin-btn admin-btn--primary admin-btn--md" to={config.create.pagePath}>
                <Plus aria-hidden="true" size={16} />
                <span>新增</span>
              </Link>
            ) : (
              <Button
                type="button"
                variant="primary"
                icon={Plus}
                onClick={() => {
                  setFormErrors({});
                  setFormValues(createInitialValues());
                  setFormOpen(true);
                }}
              >
                新增
              </Button>
            )
          ) : null
        }
      />
      <FilterBar
        showKeyword={config.keywordFilter !== false}
        keyword={filters.keyword}
        onKeywordChange={(keyword) => setFilters({ ...filters, keyword, page_token: '' })}
        status={filters.status}
        onStatusChange={(status) => setFilters({ ...filters, status, page_token: '' })}
        statusOptions={config.statusOptions || []}
        onClear={() => setFilters(initialFilters(config))}
      >
        <FilterFields fields={config.filters} values={filters} setValues={setFilters} optionContext={optionContext} />
      </FilterBar>
      {config.linkedPanel ? (
        <div className="admin-resource-split admin-resource-split--linked">
          <LinkedResourcePanel
            panel={config.linkedPanel}
            items={linkedItems}
            selectedValue={selectedLinkedValue}
            onSelect={selectLinkedValue}
            loading={providerOptionsQuery.isLoading}
            error={providerOptionsQuery.isError}
          />
          <section className="admin-resource-table-panel">
            <div className="admin-resource-table-panel__head">
              <div>
                <h2>{selectedLinkedItem?.label || config.linkedPanel.allLabel || config.title}</h2>
                <p>{linkedPanelSummary(config.linkedPanel, selectedLinkedValue, rows)}</p>
              </div>
            </div>
            <DataTable
              columns={columns}
              rows={rows}
              rowKey={config.rowId}
              state={tableState}
              emptyText={selectedLinkedValue ? config.linkedPanel.emptyResultText || '该供应商暂无模型' : config.emptyText}
              errorText={query.error?.message}
            />
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
          </section>
        </div>
      ) : config.groupBy ? (
        <div className="admin-resource-split">
          <aside className="admin-resource-group-panel" aria-label={config.groupBy.title || '资源分组'}>
            <div className="admin-resource-group-panel__head">
              <span>{config.groupBy.title || '分组'}</span>
              <strong>{rows.length}</strong>
            </div>
            <div className="admin-resource-group-list">
              {groupItems.map((item) => (
                <button
                  key={item.value || '__all'}
                  type="button"
                  className={item.value === selectedGroup ? 'is-active' : ''}
                  onClick={() => setSelectedGroup(item.value)}
                >
                  <span>{item.label}</span>
                  <strong>{item.count}</strong>
                </button>
              ))}
            </div>
          </aside>
          <section className="admin-resource-table-panel">
            <div className="admin-resource-table-panel__head">
              <div>
                <h2>{activeGroup?.label || config.groupBy.allLabel || config.title}</h2>
                <p>{selectedGroup ? `当前类型 ${visibleRows.length} 个 Tool` : `当前筛选共 ${rows.length} 个 Tool`}</p>
              </div>
            </div>
            <DataTable columns={columns} rows={visibleRows} rowKey={config.rowId} state={tableState} emptyText={selectedGroup ? '该类型暂无 Tool' : config.emptyText} errorText={query.error?.message} />
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
          </section>
        </div>
      ) : (
        <>
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
        </>
      )}
      <Drawer open={Boolean(selected)} title={`${config.title}详情`} onClose={() => setSelected(null)}>
        {detailQuery.isLoading ? <p>加载中...</p> : null}
        {detailQuery.isError ? (
          <Alert tone="danger" title="详情加载失败" traceId={detailQuery.error.traceId}>
            {detailQuery.error.message}
          </Alert>
        ) : null}
        <RowDetails row={detailQuery.data || selected} labels={detailLabels} />
      </Drawer>
      <Modal
        open={formOpen}
        title={config.create?.title}
        onClose={() => {
          setFormErrors({});
          setFormOpen(false);
        }}
        size={config.create?.modalSize || 'lg'}
        footer={
          <>
            <Button
              type="button"
              variant="secondary"
              onClick={() => {
                setFormErrors({});
                setFormOpen(false);
              }}
            >
              取消
            </Button>
            <Button type="submit" form="admin-resource-form" variant="primary" loading={createMutation.isPending}>
              保存
            </Button>
          </>
        }
      >
        <form id="admin-resource-form" onSubmit={submitCreate} noValidate>
          <ResourceForm
            fields={config.create?.fields || []}
            values={formValues}
            setValues={setFormValues}
            errors={formErrors}
            onFieldChange={(name) => setFormErrors((current) => removeFieldError(current, name))}
            optionContext={optionContext}
          />
          <AuditHint />
        </form>
      </Modal>
      <Modal
        open={Boolean(actionForm)}
        title={actionForm?.action?.formTitle || asLabel(actionForm?.action?.label || '后台操作', actionForm?.row)}
        onClose={() => setActionForm(null)}
        size={actionForm?.action?.modalSize || 'lg'}
        footer={
          <>
            <Button type="button" variant="secondary" onClick={() => setActionForm(null)}>
              取消
            </Button>
            <Button type="submit" form="admin-action-form" variant={actionForm?.action?.tone === 'danger' ? 'danger' : 'primary'} loading={actionMutation.isPending}>
              {actionForm?.action?.submitLabel || '保存'}
            </Button>
          </>
        }
      >
        {actionForm?.error ? (
          <Alert tone="danger" title="表单格式错误">
            {actionForm.error.message}
          </Alert>
        ) : null}
        {actionForm?.action?.description ? (
          <Alert tone="info" title={actionForm.action.descriptionTitle || '操作说明'}>
            {actionForm.action.description}
          </Alert>
        ) : null}
        <form id="admin-action-form" onSubmit={submitActionForm} noValidate>
          <ResourceForm
            fields={actionForm?.action?.fields || []}
            values={actionForm?.values || {}}
            setValues={(values) => setActionForm((current) => (current ? { ...current, values, error: null } : current))}
            errors={actionForm?.errors || {}}
            onFieldChange={(name) => setActionForm((current) => (current ? { ...current, errors: removeFieldError(current.errors, name) } : current))}
            optionContext={optionContext}
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
