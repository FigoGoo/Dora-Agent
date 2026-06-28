import { Badge } from '../../components/admin/Badge.jsx';
import { TraceIdCopy } from '../../components/admin/TraceIdCopy.jsx';
import { adminApi } from '../../lib/api/admin.js';
import { formatDateTime } from '../../lib/format.js';

const statusOptions = [
  { label: '启用', value: 'active' },
  { label: '停用', value: 'disabled' },
  { label: '草稿', value: 'draft' },
  { label: '已发布', value: 'published' },
  { label: '待审核', value: 'pending_review' },
  { label: '废弃', value: 'deprecated' }
];

const activeStatusOptions = [
  { label: '启用', value: 'active' },
  { label: '停用', value: 'disabled' }
];

const resourceTypeOptions = [
  { label: '图片', value: 'image' },
  { label: '音乐', value: 'music' },
  { label: '视频', value: 'video' },
  { label: '文本', value: 'text' }
];

const riskLevelOptions = [
  { label: '低风险', value: 'low' },
  { label: '中风险', value: 'medium' },
  { label: '高风险', value: 'high' }
];

const chargeModeOptions = [
  { label: '免费', value: 'free' },
  { label: '模型生成', value: 'model_generation' },
  { label: 'Tool 用量', value: 'tool_usage' },
  { label: '业务价值', value: 'business_value' }
];

const publicWorkStatusOptions = [
  { label: '公开中', value: 'active' },
  { label: '已下架', value: 'taken_down' },
  { label: '已取消', value: 'cancelled' }
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
    keywordFilter: false,
    statusOptions: activeStatusOptions,
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
    statusOptions: activeStatusOptions,
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
      { key: 'skill_scope', title: '范围' },
      { key: 'published_version_id', title: '发布版本', render: (row) => row.published_version_id || '-' },
      { key: 'updated_at', title: '更新时间', render: (row) => formatDateTime(row.updated_at) }
    ],
    create: {
      title: '创建系统 Skill 草稿',
      path: '/api/admin/skills/system',
      fields: [
        { name: 'skill_key', label: 'Skill Key', required: true },
        { name: 'skill_name', label: 'Skill 名称', required: true },
        { name: 'version', label: '版本', required: true },
        { name: 'route_hints', label: '路由提示 JSON', type: 'json', defaultValue: '{}' },
        { name: 'skill_spec_json', label: 'Skill Spec JSON', type: 'json-string', required: true },
        { name: 'input_schema_json', label: '输入 Schema JSON', type: 'json-string', defaultValue: '{}' },
        { name: 'output_schema_json', label: '输出 Schema JSON', type: 'json-string', defaultValue: '{}' },
        { name: 'memory_policy_json', label: 'Memory 策略 JSON', type: 'json-string', defaultValue: '{}' },
        { name: 'confirmation_policy_json', label: '确认策略 JSON', type: 'json-string', defaultValue: '{"requires_confirmation":false}' }
      ]
    },
    actions: [
      {
        label: '测试结果',
        formTitle: '保存系统 Skill 测试结果',
        path: (row) => `/api/admin/skills/system/${row.skill_id}/test`,
        fields: [
          { name: 'version_id', label: '版本 ID', required: true, source: 'published_version_id' },
          { name: 'test_run_id', label: '测试运行 ID', required: true },
          { name: 'test_case_id', label: '测试用例 ID' },
          { name: 'status', label: '测试状态', options: [{ label: '通过', value: 'passed' }, { label: '失败', value: 'failed' }, { label: '阻断', value: 'blocked' }, { label: '超时', value: 'timeout' }, { label: '拒绝', value: 'rejected' }], required: true },
          { name: 'actual_elements_json', label: '实际输出元素 JSON', type: 'json-string', defaultValue: '[]', required: true },
          { name: 'safety_evidence_json', label: '安全证据 JSON', type: 'json-string', defaultValue: '{}', required: true }
        ],
        idempotencyKey: ({ values }) => `skill_test:${values.test_run_id}`
      },
      {
        label: '发布',
        formTitle: '发布系统 Skill',
        path: (row) => `/api/admin/skills/system/${row.skill_id}/publish`,
        fields: [{ name: 'version_id', label: '版本 ID', required: true, source: 'published_version_id' }]
      },
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
        requireReason: false,
        confirmPath: (row) => `/api/admin/skills/reviews/${reviewRowKey(row)}/confirm`,
        body: ({ reason }) => ({ decision: 'approve', reason }),
        reason: ({ reason }) => reason
      },
      {
        label: '拒绝',
        tone: 'danger',
        requireReason: false,
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
    keywordFilter: false,
    statusOptions: activeStatusOptions,
    columns: [
      { key: 'provider_name', title: '供应商', render: (row) => row.provider_name || row.display_name || '-' },
      { key: 'provider_code', title: '编码', render: (row) => row.provider_code || '-' },
      statusColumn,
      { key: 'secret_ref_status', title: '密钥状态', render: (row) => <Badge>{row.secret_ref_status || '-'}</Badge> },
      { key: 'updated_at', title: '更新时间', render: (row) => formatDateTime(row.updated_at) }
    ],
    create: {
      title: '新增供应商',
      path: '/api/admin/models/providers',
      fields: [
        { name: 'provider_code', label: '供应商编码' },
        { name: 'provider_name', label: '供应商名称', required: true },
        { name: 'provider_type', label: '供应商类型', defaultValue: 'generation' },
        { name: 'base_url', label: 'Base URL' },
        { name: 'status', label: '状态', defaultValue: 'active', options: activeStatusOptions },
        { name: 'secret_key_ref', label: '密钥引用', secret: true },
        { name: 'config', label: '扩展配置 JSON', type: 'json', defaultValue: '{}' }
      ]
    },
    actions: [
      {
        label: '编辑',
        method: 'PATCH',
        path: (row) => `/api/admin/models/providers/${row.provider_id}`,
        fields: [
          { name: 'provider_name', label: '供应商名称', required: true },
          { name: 'provider_type', label: '供应商类型' },
          { name: 'base_url', label: 'Base URL' },
          { name: 'status', label: '状态', options: activeStatusOptions },
          { name: 'secret_key_ref', label: '密钥引用', secret: true },
          { name: 'config', label: '扩展配置 JSON', type: 'json', defaultValue: '{}' }
        ]
      },
      {
        label: '连通性测试',
        formTitle: '供应商连通性测试',
        path: (row) => `/api/admin/models/providers/${row.provider_id}/connectivity-test`,
        fields: [
          { name: 'model_id', label: '测试模型 ID' },
          { name: 'test_reason', label: '测试原因', defaultValue: '后台手动连通性测试', required: true }
        ],
        successNotice: (data) => `连通性测试：${data.test_status || '已完成'}`
      },
      {
        label: (row) => (row.status === 'disabled' ? '启用' : '停用'),
        tone: 'danger',
        method: 'PATCH',
        path: (row) => `/api/admin/models/providers/${row.provider_id}`,
        fields: [{ name: 'reason', label: '操作原因', textarea: true, required: true }],
        body: ({ row }) => ({ status: row.status === 'disabled' ? 'active' : 'disabled' }),
        reason: ({ values }) => values.reason
      }
    ]
  },
  models: {
    key: 'models',
    title: '模型管理',
    description: '管理模型类型、默认模型、状态和用户积分价格快照。',
    listPath: '/api/admin/models',
    rowId: 'model_id',
    emptyText: '暂无模型',
    keywordFilter: false,
    statusOptions: activeStatusOptions,
    filters: [{ name: 'resource_type', label: '资源类型', options: resourceTypeOptions }],
    columns: [
      { key: 'display_name', title: '模型' },
      { key: 'model_code', title: '模型编码', render: (row) => row.model_code || '-' },
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
        { name: 'model_code', label: '模型编码', required: true },
        { name: 'display_name', label: '模型名称', required: true },
        { name: 'resource_type', label: '资源类型', required: true, options: resourceTypeOptions },
        { name: 'pricing_snapshot_id', label: '价格快照 ID' },
        { name: 'billing_unit', label: '计费单位', defaultValue: 'generation' },
        { name: 'unit_points', label: '用户积分单价', type: 'number' },
        { name: 'min_charge_points', label: '最低扣费积分', type: 'number' },
        { name: 'credential_id', label: '凭证 ID' },
        { name: 'capability_tags', label: '能力标签', type: 'array', hint: '每行或逗号分隔一个标签。' },
        { name: 'route_config', label: '路由配置 JSON', type: 'json', defaultValue: '{}' },
        { name: 'status', label: '状态', defaultValue: 'active', options: activeStatusOptions }
      ]
    },
    actions: [
      {
        label: '编辑',
        method: 'PATCH',
        path: (row) => `/api/admin/models/${row.model_id}`,
        fields: [
          { name: 'model_code', label: '模型编码', required: true },
          { name: 'display_name', label: '模型名称', required: true },
          { name: 'status', label: '状态', options: activeStatusOptions },
          { name: 'pricing_snapshot_id', label: '新价格快照 ID' },
          { name: 'billing_unit', label: '计费单位' },
          { name: 'unit_points', label: '用户积分单价', type: 'number' },
          { name: 'min_charge_points', label: '最低扣费积分', type: 'number' },
          { name: 'credential_id', label: '凭证 ID' },
          { name: 'capability_tags', label: '能力标签', type: 'array' },
          { name: 'route_config', label: '路由配置 JSON', type: 'json', defaultValue: '{}' }
        ]
      },
      {
        label: '设为默认',
        confirmPath: () => '/api/admin/models/default',
        body: (ctx) => ({ model_id: ctx.row.model_id, resource_type: ctx.row.resource_type, pricing_snapshot_id: ctx.row.pricing_snapshot_id })
      },
      {
        label: (row) => (row.status === 'disabled' ? '启用' : '停用'),
        tone: 'danger',
        confirmPath: (row) => `/api/admin/models/${row.model_id}/status`,
        body: ({ reason, row }) => ({ status: row.status === 'disabled' ? 'active' : 'disabled', reason }),
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
    keywordFilter: false,
    statusOptions: activeStatusOptions,
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
        requireReason: false,
        preview: (row) => adminApi.post(`/api/admin/tools/${toolRowKey(row)}/impact-preview`, { tool_type: row.tool_type, target_status: row.status }),
        confirm: () => Promise.resolve({ status: 'previewed' }),
        objectLabel: (row) => `Tool ${toolRowKey(row)}`,
        impactItems: (preview) => [`影响 Skill 数量：${preview?.impact_count ?? 0}`, ...(preview?.affected_skill_ids || [])],
        reason: () => ''
      },
      {
        label: '策略',
        method: 'PATCH',
        path: (row) => `/api/admin/tools/${toolRowKey(row)}/policy`,
        fields: [
          { name: 'tool_type', label: 'Tool 类型', required: true },
          { name: 'allowed', label: '允许使用', type: 'checkbox', defaultValue: true },
          { name: 'risk_level', label: '风险等级', options: riskLevelOptions, required: true },
          { name: 'requires_confirmation', label: '需要确认', type: 'checkbox' },
          { name: 'timeout_ms', label: '超时毫秒', type: 'number' },
          { name: 'retry_policy', label: '重试策略 JSON', type: 'json', defaultValue: '{}' },
          { name: 'cancel_policy', label: '取消策略 JSON', type: 'json', defaultValue: '{}' },
          { name: 'reason', label: '操作原因', textarea: true, required: true }
        ],
        reason: ({ values }) => values.reason,
        body: ({ values }) => ({
          tool_type: values.tool_type,
          allowed: values.allowed,
          risk_level: values.risk_level,
          requires_confirmation: values.requires_confirmation,
          timeout_ms: values.timeout_ms ? Number(values.timeout_ms) : 0,
          retry_policy: values.retry_policy ? JSON.parse(values.retry_policy) : {},
          cancel_policy: values.cancel_policy ? JSON.parse(values.cancel_policy) : {}
        })
      },
      {
        label: '计价',
        method: 'PATCH',
        path: (row) => `/api/admin/tools/${toolRowKey(row)}/pricing-policy`,
        fields: [
          { name: 'tool_type', label: 'Tool 类型', required: true },
          { name: 'charge_mode', label: '计费模式', options: chargeModeOptions, required: true },
          { name: 'billing_unit', label: '计费单位', defaultValue: 'call', required: true },
          { name: 'unit_points', label: '单价积分', type: 'number', required: true },
          { name: 'free_quota', label: '免费额度', type: 'number' },
          { name: 'min_charge_points', label: '最低扣费积分', type: 'number' },
          { name: 'reason', label: '操作原因', textarea: true, required: true }
        ],
        reason: ({ values }) => values.reason,
        body: ({ values }) => ({
          tool_type: values.tool_type,
          charge_mode: values.charge_mode,
          billing_unit: values.billing_unit,
          unit_points: Number(values.unit_points),
          free_quota: values.free_quota ? Number(values.free_quota) : 0,
          min_charge_points: values.min_charge_points ? Number(values.min_charge_points) : 0
        })
      },
      {
        label: '白名单',
        method: 'PUT',
        path: (row) => `/api/admin/tools/${toolRowKey(row)}/whitelist`,
        fields: [
          { name: 'tool_type', label: 'Tool 类型', required: true },
          { name: 'scope_type', label: '范围类型', options: [{ label: '空间', value: 'space' }, { label: '企业', value: 'enterprise' }, { label: '用户', value: 'user' }], required: true },
          { name: 'scope_id', label: '范围 ID', required: true },
          { name: 'allowed', label: '允许使用', type: 'checkbox', defaultValue: true },
          { name: 'reason', label: '操作原因', textarea: true, required: true }
        ],
        reason: ({ values }) => values.reason
      },
      {
        label: (row) => (row.status === 'disabled' ? '启用' : '停用'),
        tone: 'danger',
        confirmPath: (row) => `/api/admin/tools/${toolRowKey(row)}/status`,
        body: ({ reason, row }) => ({ tool_type: row.tool_type, status: row.status === 'disabled' ? 'active' : 'disabled', reason }),
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
    keywordFilter: false,
    statusOptions: activeStatusOptions,
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
        { name: 'account_type', label: '账户类型', defaultValue: 'personal', options: [{ label: '个人', value: 'personal' }, { label: '企业', value: 'enterprise' }] },
        { name: 'bind_target_type', label: '绑定类型', defaultValue: 'none', options: [{ label: '不绑定', value: 'none' }, { label: '用户', value: 'user' }, { label: '企业', value: 'enterprise' }, { label: '渠道', value: 'channel' }] },
        { name: 'bind_target_id', label: '绑定对象 ID' },
        { name: 'channel', label: '渠道' },
        { name: 'reason', label: '创建原因', required: true }
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
        reason: ({ reason }) => reason,
        successNotice: (data) => `导出任务已创建：${data.export_job_id || data.status || '已提交'}`
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
    statusOptions: publicWorkStatusOptions,
    filters: [
      { name: 'category', label: '分类' },
      { name: 'tag', label: '标签' },
      { name: 'resource_type', label: '资源类型', options: resourceTypeOptions }
    ],
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
    keywordFilter: false,
    filters: [
      { name: 'business_action', label: '操作类型' },
      { name: 'trace_id', label: 'Trace ID' }
    ],
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
    keywordFilter: false,
    statusOptions,
    columns: [text('element_type'), { key: 'display_name', title: '展示名' }, statusColumn, { key: 'schema_version', title: 'Schema 版本' }]
  }
};
