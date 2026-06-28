import { Badge } from '../../components/admin/Badge.jsx';
import { TraceIdCopy } from '../../components/admin/TraceIdCopy.jsx';
import { adminApi } from '../../lib/api/admin.js';
import { formatDateTime } from '../../lib/format.js';

const statusOptions = [
  { label: '启用', value: 'active' },
  { label: '停用', value: 'disabled' },
  { label: '草稿', value: 'draft' },
  { label: '已发布', value: 'published' },
  { label: '待审核', value: 'pending_review' }
];

const text = (key) => ({ key, title: key });
const statusColumn = { key: 'status', title: '状态', width: 120, render: (row) => <Badge>{row.status}</Badge> };
const createdColumn = { key: 'created_at', title: '创建时间', width: 180, render: (row) => formatDateTime(row.created_at || row.registered_at || row.published_at) };
const toolRowKey = (row) => row.tool_key || [row.tool_name, row.tool_type].filter(Boolean).join(':');
const reviewRowKey = (row) => row.review_id || row.version_id;

export const pageConfigs = {
  admins: {
    key: 'admins',
    title: '管理员账号',
    description: '新增和停用平台管理员，第一版所有管理员权限相同。',
    listPath: '/api/admin/admins',
    emptyText: '暂无管理员账号',
    statusOptions,
    rowId: 'admin_id',
    columns: [
      { key: 'account', title: '账号' },
      statusColumn,
      { key: 'must_rotate_password', title: '需改密', render: (row) => (row.must_rotate_password ? '是' : '否') },
      createdColumn
    ],
    create: {
      title: '新增管理员',
      path: '/api/admin/admins',
      reasonField: 'reason',
      fields: [
        { name: 'account', label: '管理员账号', required: true },
        { name: 'initial_password', label: '初始密码', type: 'password', required: true },
        { name: 'reason', label: '新增原因', required: true }
      ]
    },
    actions: [
      {
        label: '停用',
        tone: 'danger',
        objectLabel: (row) => `管理员 ${row.account}`,
        impactItems: ['该管理员将无法继续登录后台。', '操作会写入审计日志。'],
        confirmPath: (row) => `/api/admin/admins/${row.admin_id}/disable`,
        body: ({ reason }) => ({ reason }),
        reason: ({ reason }) => reason
      }
    ]
  },
  users: {
    key: 'users',
    title: '用户管理',
    description: '只管理账号基础状态，不展示用户私有资产、会话、黑板、提示词或私有素材。',
    listPath: '/api/admin/users',
    detailPath: (row) => `/api/admin/users/${row.user_id}`,
    emptyText: '暂无用户',
    statusOptions,
    rowId: 'user_id',
    columns: [
      { key: 'public_nickname', title: '公开昵称', render: (row) => row.public_nickname || row.display_name || '-' },
      { key: 'user_id', title: '用户 ID' },
      statusColumn,
      { key: 'email_masked', title: '邮箱', render: (row) => row.email_masked || '-' },
      { key: 'phone_masked', title: '手机号', render: (row) => row.phone_masked || '-' },
      createdColumn
    ],
    actions: [
      {
        label: (row) => (row.status === 'disabled' ? '启用' : '禁用'),
        tone: 'danger',
        preview: (row, reason) =>
          adminApi.previewUserStatus(row.user_id, { target_status: row.status === 'disabled' ? 'active' : 'disabled', reason }),
        confirm: (row, context) =>
          adminApi.confirmUserStatus(
            row.user_id,
            { target_status: row.status === 'disabled' ? 'active' : 'disabled', preview_token: context.previewToken, reason: context.reason },
            context.reason
          ),
        objectLabel: (row) => `用户 ${row.user_id}`,
        impactItems: (preview) => preview?.impact_summary || ['用户账号状态将变化。', '操作会写入审计日志。']
      }
    ]
  },
  'skills/system': {
    key: 'system-skills',
    title: '系统 Skill',
    description: '平台内置 Skill 创建、测试、发布和废弃；发布前必须通过测试。',
    listPath: '/api/admin/skills/system',
    rowId: 'skill_id',
    emptyText: '暂无系统 Skill',
    statusOptions,
    columns: [
      { key: 'skill_name', title: 'Skill 名称' },
      { key: 'skill_key', title: 'Key' },
      statusColumn,
      { key: 'version', title: '版本' },
      createdColumn
    ],
    create: {
      title: '创建系统 Skill 草稿',
      path: '/api/admin/skills/system',
      fields: [
        { name: 'skill_key', label: 'Skill Key', required: true },
        { name: 'skill_name', label: 'Skill 名称', required: true },
        { name: 'version', label: '版本', required: true },
        { name: 'skill_spec_json', label: 'Skill Spec JSON', textarea: true }
      ]
    },
    actions: [
      { label: '测试', confirmPath: (row) => `/api/admin/skills/system/${row.skill_id}/test`, body: () => ({ test_case_ids: [] }) },
      { label: '发布', confirmPath: (row) => `/api/admin/skills/system/${row.skill_id}/publish`, body: (ctx) => ({ version_id: ctx.row.version_id || ctx.row.version || '' }) },
      {
        label: '废弃',
        tone: 'danger',
        confirmPath: (row) => `/api/admin/skills/system/${row.skill_id}/deprecate`,
        body: ({ reason }) => ({ reason }),
        reason: ({ reason }) => reason
      }
    ]
  },
  'skills/reviews': {
    key: 'skill-reviews',
    title: 'Skill 审核',
    description: '审核用户或企业提交的 Skill，审核时不得篡改创建者内容。',
    listPath: '/api/admin/skills/reviews',
    rowId: reviewRowKey,
    emptyText: '暂无待审核 Skill',
    columns: [
      { key: 'skill_name', title: 'Skill', render: (row) => row.skill_name || row.skill_id || '-' },
      { key: 'creator_id', title: '创建者' },
      statusColumn,
      { key: 'submitted_at', title: '提交时间', render: (row) => formatDateTime(row.submitted_at || row.created_at) }
    ],
    actions: [
      {
        label: '通过',
        confirmPath: (row) => `/api/admin/skills/reviews/${reviewRowKey(row)}/confirm`,
        body: ({ reason }) => ({ decision: 'approve', reason }),
        reason: ({ reason }) => reason
      },
      {
        label: '拒绝',
        tone: 'danger',
        confirmPath: (row) => `/api/admin/skills/reviews/${reviewRowKey(row)}/confirm`,
        body: ({ reason }) => ({ decision: 'reject', reason }),
        reason: ({ reason }) => reason
      }
    ]
  },
  'models/providers': {
    key: 'model-providers',
    title: '模型供应商',
    description: '配置供应商密钥引用和连通性测试；密钥不明文回显。',
    listPath: '/api/admin/models/providers',
    rowId: 'provider_id',
    emptyText: '暂无供应商',
    statusOptions,
    columns: [
      { key: 'provider_name', title: '供应商', render: (row) => row.provider_name || row.display_name || '-' },
      statusColumn,
      { key: 'secret_ref_status', title: '密钥状态', render: (row) => <Badge>{row.secret_ref_status || '-'}</Badge> }
    ],
    create: {
      title: '新增供应商',
      path: '/api/admin/models/providers',
      fields: [
        { name: 'provider_name', label: '供应商名称', required: true },
        { name: 'status', label: '状态', defaultValue: 'active' },
        { name: 'secret_key_ref', label: '密钥引用', secret: true, required: true }
      ]
    },
    actions: [{ label: '连通性测试', confirmPath: (row) => `/api/admin/models/providers/${row.provider_id}/connectivity-test`, body: () => ({ test_reason: '后台手动连通性测试' }) }]
  },
  models: {
    key: 'models',
    title: '模型管理',
    description: '管理模型类型、默认模型、状态和用户积分价格快照。',
    listPath: '/api/admin/models',
    rowId: 'model_id',
    emptyText: '暂无模型',
    statusOptions,
    columns: [
      { key: 'display_name', title: '模型' },
      { key: 'provider_id', title: '供应商' },
      { key: 'resource_type', title: '资源类型' },
      statusColumn,
      { key: 'is_default', title: '默认', render: (row) => (row.is_default || row.default_for_resource ? '是' : '否') }
    ],
    create: {
      title: '新增模型',
      path: '/api/admin/models',
      fields: [
        { name: 'provider_id', label: '供应商 ID', required: true },
        { name: 'display_name', label: '模型名称', required: true },
        { name: 'resource_type', label: '资源类型', required: true },
        { name: 'pricing_snapshot_id', label: '价格快照 ID', required: true },
        { name: 'status', label: '状态', defaultValue: 'enabled' }
      ]
    },
    actions: [
      {
        label: '设为默认',
        confirmPath: () => '/api/admin/models/default',
        body: (ctx) => ({ model_id: ctx.row.model_id, resource_type: ctx.row.resource_type, pricing_snapshot_id: ctx.row.pricing_snapshot_id })
      },
      {
        label: '停用',
        tone: 'danger',
        confirmPath: (row) => `/api/admin/models/${row.model_id}/status`,
        body: ({ reason }) => ({ status: 'disabled', reason }),
        reason: ({ reason }) => reason
      }
    ]
  },
  tools: {
    key: 'tools',
    title: 'Tool 管理',
    description: '管理 Tool 启停、风险等级、白名单和计价策略。',
    listPath: '/api/admin/tools',
    rowId: toolRowKey,
    emptyText: '暂无 Tool',
    statusOptions,
    columns: [
      { key: 'tool_name', title: 'Tool' },
      { key: 'tool_type', title: '类型' },
      statusColumn,
      { key: 'risk_level', title: '风险', render: (row) => <Badge tone={row.risk_level === 'high' ? 'danger' : 'warning'}>{row.risk_level}</Badge> },
      { key: 'charge_mode', title: '计费' }
    ],
    actions: [
      {
        label: '影响预览',
        preview: (row, reason) => adminApi.post(`/api/admin/tools/${toolRowKey(row)}/impact-preview`, { target_status: row.status, reason }),
        confirm: () => Promise.resolve({ status: 'previewed' }),
        objectLabel: (row) => `Tool ${toolRowKey(row)}`,
        impactItems: (preview) => [`影响 Skill 数量：${preview?.impact_count ?? 0}`, ...(preview?.affected_skill_ids || [])]
      },
      {
        label: '停用',
        tone: 'danger',
        confirmPath: (row) => `/api/admin/tools/${toolRowKey(row)}/status`,
        body: ({ reason, row }) => ({ tool_type: row.tool_type, status: 'disabled', reason }),
        reason: ({ reason }) => reason
      }
    ]
  },
  'credits/codes': {
    key: 'redeem-codes',
    title: '兑换码管理',
    description: '创建、停用和导出兑换码批次；审计日志不保存完整兑换码。',
    listPath: '/api/admin/credits/codes',
    rowId: 'batch_id',
    emptyText: '暂无兑换码批次',
    statusOptions,
    columns: [
      { key: 'batch_no', title: '批次', render: (row) => row.batch_no || row.batch_id },
      statusColumn,
      { key: 'account_type', title: '账户类型' },
      { key: 'bind_target_type', title: '绑定类型' },
      { key: 'points', title: '积分' },
      { key: 'count', title: '数量' }
    ],
    create: {
      title: '创建兑换码批次',
      path: '/api/admin/credits/codes',
      fields: [
        { name: 'count', label: '数量', type: 'number', required: true },
        { name: 'points', label: '每码积分', type: 'number', required: true },
        { name: 'code_expires_at', label: '兑换码过期时间', type: 'datetime-local', required: true },
        { name: 'credit_expires_at', label: '积分过期时间', type: 'datetime-local', required: true },
        { name: 'account_type', label: '账户类型', defaultValue: 'personal' },
        { name: 'bind_target_type', label: '绑定类型', defaultValue: 'none' },
        { name: 'reason', label: '创建原因' }
      ]
    },
    actions: [
      {
        label: '停用',
        tone: 'danger',
        confirmPath: (row) => `/api/admin/credits/codes/${row.batch_id}/disable`,
        body: ({ reason }) => ({ reason }),
        reason: ({ reason }) => reason
      },
      {
        label: '导出',
        tone: 'danger',
        confirmPath: (row) => `/api/admin/credits/codes/${row.batch_id}/export`,
        body: ({ reason }) => ({ export_reason: reason }),
        reason: ({ reason }) => reason
      }
    ]
  },
  'works/public': {
    key: 'public-works',
    title: '精选作品管理',
    description: '查询公开作品并下架风险作品；下架不删除源资产。',
    listPath: '/api/admin/works/public',
    rowId: 'public_work_id',
    emptyText: '暂无公开作品',
    statusOptions,
    columns: [
      { key: 'title', title: '作品' },
      { key: 'public_work_id', title: '公开 ID' },
      statusColumn,
      { key: 'published_at', title: '发布时间', render: (row) => formatDateTime(row.published_at) }
    ],
    actions: [
      {
        label: '下架',
        tone: 'danger',
        preview: (row, reason) => adminApi.previewTakeDownWork(row.public_work_id, { reason, notify_author: true }, reason),
        confirm: (row, context) =>
          adminApi.confirmTakeDownWork(
            row.public_work_id,
            { reason: context.reason, preview_token: context.previewToken, notify_author: true },
            context.reason
          ),
        objectLabel: (row) => `公开作品 ${row.title || row.public_work_id}`,
        impactItems: (preview) => [
          `当前状态：${preview?.current_status || '-'}`,
          '公开链接将不可访问。',
          preview?.notify_author ? '将通知作者。' : '不会通知作者。'
        ]
      }
    ]
  },
  'audit-logs': {
    key: 'audit-logs',
    title: '审计日志',
    description: '查看后台关键操作摘要，不展示密钥、完整兑换码和私有创作内容。',
    listPath: '/api/admin/audit-logs',
    rowId: 'audit_id',
    emptyText: '暂无审计日志',
    columns: [
      { key: 'action', title: '操作' },
      { key: 'actor_id', title: '操作人' },
      { key: 'resource_type', title: '资源类型' },
      { key: 'resource_id', title: '资源 ID' },
      { key: 'trace_id', title: 'trace_id', render: (row) => <TraceIdCopy traceId={row.trace_id} /> },
      createdColumn
    ]
  },
  'asset-element-types': {
    key: 'asset-element-types',
    title: '资产元素类型',
    description: '查看平台内置资产元素类型和状态，第一版不作为业务内容后台。',
    listPath: '/api/admin/asset-element-types',
    rowId: 'element_type',
    emptyText: '暂无资产元素类型',
    statusOptions,
    columns: [text('element_type'), { key: 'display_name', title: '展示名' }, statusColumn, { key: 'schema_version', title: 'Schema 版本' }]
  }
};
