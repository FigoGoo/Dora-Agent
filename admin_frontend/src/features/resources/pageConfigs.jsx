import { Badge } from '../../components/admin/Badge.jsx';
import { TraceIdCopy } from '../../components/admin/TraceIdCopy.jsx';
import { adminApi } from '../../lib/api/admin.js';
import { formatDateTime } from '../../lib/format.js';

const statusOptions = [
  { label: '启用', value: 'active' },
  { label: '停用', value: 'disabled' },
  { label: '草稿', value: 'draft' },
  { label: '已发布', value: 'published' },
  { label: '待审核', value: 'submitted' },
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

const modelBillingUnitOptions = [
  { label: '不配置计费', value: '' },
  { label: '按次生成 generation', value: 'generation' },
  { label: '按图片 image', value: 'image' },
  { label: '按视频 video', value: 'video' },
  { label: '按音频 audio', value: 'audio' },
  { label: '按调用 call', value: 'call' },
  { label: '按 Token token', value: 'token' }
];

const riskLevelOptions = [
  { label: '低风险', value: 'low' },
  { label: '中风险', value: 'medium' },
  { label: '高风险', value: 'high' }
];

const chargeModeOptions = [
  { label: '免费', value: 'free' },
  { label: '按调用', value: 'per_call' },
  { label: '按资产', value: 'per_asset' },
  { label: '模型生成', value: 'model_generation' },
  { label: 'Tool 用量', value: 'tool_usage' },
  { label: '业务价值', value: 'business_value' }
];

const toolBillingUnitOptions = [
  { label: '按资产', value: 'asset' },
  { label: '按调用', value: 'call' },
  { label: '按 Token', value: 'token' },
  { label: '按图片', value: 'image' },
  { label: '按视频', value: 'video' },
  { label: '按音频', value: 'audio' },
  { label: '按次生成', value: 'generation' }
];

const scopeTypeOptions = [
  { label: '空间', value: 'space' },
  { label: '企业', value: 'enterprise' },
  { label: '用户', value: 'user' }
];

const skillScopeOptions = [
  { label: '公开', value: 'public' },
  { label: '个人', value: 'personal' },
  { label: '企业', value: 'enterprise' }
];

const publicWorkStatusOptions = [
  { label: '公开中', value: 'active' },
  { label: '已下架', value: 'taken_down' },
  { label: '已取消', value: 'cancelled' }
];

const marketplaceReviewStatusOptions = [
  { label: '待审核', value: 'submitted' },
  { label: '审核中', value: 'reviewing' },
  { label: '已通过', value: 'approved' }
];

const marketplaceListingStatusOptions = [
  { label: '上架中', value: 'listed' },
  { label: '已暂停', value: 'suspended' },
  { label: '未上架', value: 'not_listed' }
];

const refundCaseStatusOptions = [
  { label: '退款申请', value: 'refund_requested' },
  { label: '退款审核中', value: 'refund_reviewing' },
  { label: '已反转', value: 'refund_reversed' },
  { label: '已拒绝', value: 'refund_rejected' }
];

const settlementStatusOptions = [
  { label: '待 hold', value: 'pending_hold' },
  { label: '可结算', value: 'eligible' },
  { label: '结算中', value: 'settling' },
  { label: '已结算', value: 'settled' },
  { label: '已反转', value: 'reversed' },
  { label: '已冻结', value: 'frozen' },
  { label: '失败', value: 'failed' }
];

const packageTypeOptions = [
  { label: '个人积分包', value: 'personal_credit_pack' },
  { label: '个人会员套餐', value: 'personal_membership' },
  { label: '企业积分包', value: 'enterprise_credit_pack' },
  { label: '企业套餐', value: 'enterprise_plan' },
  { label: '生成加购包', value: 'generation_addon' },
  { label: '创作者权益包', value: 'creator_benefit_pack' }
];

const packageTargetScopeOptions = [
  { label: '个人', value: 'personal' },
  { label: '企业', value: 'enterprise' },
  { label: '创作者', value: 'creator' }
];

const billingModeOptions = [
  { label: '一次性购买', value: 'one_time' },
  { label: '订阅', value: 'subscription' },
  { label: '合同', value: 'contract' }
];

const packageStatusOptions = [
  { label: '草稿', value: 'draft' },
  { label: '上架', value: 'active' },
  { label: '暂停', value: 'paused' },
  { label: '废弃', value: 'deprecated' },
  { label: '归档', value: 'archived' }
];

const paymentStatusOptions = [
  { label: '待支付', value: 'pending' },
  { label: '已支付', value: 'paid' },
  { label: '支付失败', value: 'failed' }
];

const contractStatusOptions = [
  { label: '生效中', value: 'active' },
  { label: '暂停', value: 'paused' },
  { label: '已结束', value: 'ended' }
];

const invoiceStatusOptions = [
  { label: '待开票', value: 'pending' },
  { label: '已开票', value: 'issued' },
  { label: '已作废', value: 'voided' }
];

const toolRowKey = (row) => row.tool_key || [row.tool_name, row.tool_type].filter(Boolean).join(':');
const reviewRowKey = (row) => row.review_id || row.skill_version_id || row.version_id;
const optionLabel = (options, value) => options.find((option) => option.value === value)?.label || value || '-';
const text = (key, title = key) => ({ key, title });
const statusLabel = (value) =>
  optionLabel([...statusOptions, ...publicWorkStatusOptions, ...marketplaceReviewStatusOptions, ...marketplaceListingStatusOptions, ...refundCaseStatusOptions, ...settlementStatusOptions, ...packageStatusOptions, ...paymentStatusOptions, ...contractStatusOptions, ...invoiceStatusOptions], value);
const statusTone = (value) =>
  ['active', 'published', 'passed', 'approved', 'listed', 'eligible', 'settled', 'paid', 'issued'].includes(value)
    ? 'success'
    : ['disabled', 'deprecated', 'archived', 'rejected', 'failed', 'taken_down', 'cancelled', 'suspended', 'refund_rejected', 'reversed', 'voided'].includes(value)
      ? 'danger'
      : ['submitted', 'pending_review', 'timeout', 'blocked', 'reviewing', 'refund_requested', 'refund_reviewing', 'pending_hold', 'settling', 'frozen', 'pending', 'paused', 'draft'].includes(value)
        ? 'warning'
        : 'neutral';
const statusColumn = { key: 'status', title: '状态', width: 120, render: (row) => <Badge tone={statusTone(row.status)}>{statusLabel(row.status)}</Badge> };
const createdColumn = { key: 'created_at', title: '创建时间', width: 180, render: (row) => formatDateTime(row.created_at || row.registered_at || row.published_at) };
const resourceTypeLabel = (value) => optionLabel(resourceTypeOptions, value);
const toolBillingUnitLabel = (value) => optionLabel(toolBillingUnitOptions, value);
const packageTypeLabel = (value) => optionLabel(packageTypeOptions, value);
const targetScopeLabel = (value) => optionLabel(packageTargetScopeOptions, value);
const billingModeLabel = (value) => optionLabel(billingModeOptions, value);
const secretRefStatusLabel = (value) =>
  ({
    configured: '已配置',
    missing: '未配置'
  })[value] ||
  value ||
  '未配置';
const toolTypeLabel = (value) =>
  ({
    builtin: '内置 Tool',
    http: 'HTTP Tool',
    rpc: 'RPC Tool',
    external: '外部 Tool',
    model_generation: '模型生成 Tool',
    browser: '浏览器 Tool',
    image_edit: '图片编辑 Tool'
  })[value] ||
  value ||
  '未分类';
const affectedSkillLabel = (skill) => `${skill.skill_name || skill.skill_id}（${statusLabel(skill.status)}）`;
const riskTone = (riskLevel) => (riskLevel === 'high' ? 'danger' : riskLevel === 'low' ? 'success' : 'warning');
const formatPoints = (value) => (Number(value) ? `${Number(value)} 积分` : '免费');
const formatCny = (value) => (Number(value) ? `¥${(Number(value) / 100).toFixed(2)}` : '免费');
const formatTimeout = (value) => {
  const ms = Number(value);
  if (!ms) {
    return '-';
  }
  return ms >= 1000 ? `${ms / 1000}s` : `${ms}ms`;
};

function ToolIdentityCell(row) {
  return (
    <div className="admin-table-cell-stack">
      <span className="admin-table-cell-main">{row.display_name || row.tool_name}</span>
      <span className="admin-table-cell-meta">{toolRowKey(row)}</span>
      {row.description ? <span className="admin-table-cell-desc">{row.description}</span> : null}
    </div>
  );
}

function ModelProviderCell(row) {
  return (
    <div className="admin-table-cell-stack">
      <span className="admin-table-cell-main">{row.provider_name || row.display_name || row.provider_id}</span>
      <span className="admin-table-cell-meta">{row.provider_code || row.provider_id}</span>
    </div>
  );
}

function ModelIdentityCell(row) {
  return (
    <div className="admin-table-cell-stack">
      <span className="admin-table-cell-main">{row.display_name || row.model_code}</span>
      <span className="admin-table-cell-meta">{row.model_code || row.model_id}</span>
    </div>
  );
}

function ModelProviderRefCell(row) {
  return (
    <div className="admin-table-cell-stack">
      <span className="admin-table-cell-main">{row.provider_name || row.provider_id}</span>
      <span className="admin-table-cell-meta">{row.provider_id}</span>
    </div>
  );
}

function MarketplaceSkillCell(row) {
  return (
    <div className="admin-table-cell-stack">
      <span className="admin-table-cell-main">{row.skill_name || row.name || row.skill_id}</span>
      <span className="admin-table-cell-meta">{[row.skill_version || row.version, row.skill_id].filter(Boolean).join(' · ')}</span>
      {row.skill_description ? <span className="admin-table-cell-desc">{row.skill_description}</span> : null}
    </div>
  );
}

function MarketplaceStatusBadge({ value }) {
  return <Badge tone={statusTone(value)}>{statusLabel(value)}</Badge>;
}

function ToolExecutionPolicyCell(row) {
  return (
    <div className="admin-table-cell-stack">
      <span className="admin-table-cell-inline">
        <Badge tone={row.allowed ? 'success' : 'danger'}>{row.allowed ? '允许' : '禁用'}</Badge>
        <Badge tone={riskTone(row.risk_level)}>{optionLabel(riskLevelOptions, row.risk_level)}</Badge>
        <Badge tone={row.requires_confirmation ? 'warning' : 'neutral'}>{row.requires_confirmation ? '需确认' : '免确认'}</Badge>
      </span>
      <span className="admin-table-cell-meta">超时 {formatTimeout(row.timeout_ms)}</span>
    </div>
  );
}

function ToolPricingPolicyCell(row) {
  return (
    <div className="admin-table-cell-stack">
      <span className="admin-table-cell-main">{optionLabel(chargeModeOptions, row.charge_mode)}</span>
      <span className="admin-table-cell-meta">
        {toolBillingUnitLabel(row.billing_unit)} · {formatPoints(row.unit_points)}
      </span>
      {row.pricing_policy_id ? <span className="admin-table-cell-meta">{row.pricing_policy_id}</span> : null}
    </div>
  );
}
export const defaultSystemSkillMarkdown = `# 未命名 Skill <名称>

## 说明 <说明>

当用户需要完成某类创作、分析或生成任务时触发。

## 输入 <输入>

用户可以先用自然语言描述目标。
如果缺少剧本或素材，向用户请求上传图片、PDF 或文本文件。
如果缺少风格偏好，提供写实、动画、电影感三个选项让用户选择。
如果目标不清楚，使用多行文本框让用户补充创作目标。
允许用户在看到故事板后继续修改风格、镜头和素材。

## 计划 <计划>

1. 判断用户输入是否完整。
2. 拆解执行阶段、依赖关系和确认点。
3. 生成阶段产物，并在用户要求修改时回到对应阶段重新执行。
4. 用户确认满意后输出最终结果。

## 工具引用 <工具引用>

<tool id="image_generate:model_generation">图片生成</tool>
<tool id="web_fetch:browser">网页读取</tool>

## AG-UI 元素引用 <AG-UI元素引用>

对话框内：
<agui id="confirm_card">确认卡片</agui>

对话框外：
<agui id="storyboard_panel">故事板面板</agui>
<agui id="asset_panel">资产面板</agui>

## 生成偏好 <生成偏好>

- 默认优先保持用户原始意图。
- 需要多阶段生成时，每个关键阶段先输出可审阅结果。

## 提示词写法 <提示词写法>

- 提示词使用清晰、具体、可执行的自然语言。
- 视频提示词按摄像机、主体、空间、音频顺序书写。

## 结果输出 <结果输出>

Agent 可以先输出故事板供用户审阅。
生成图片资产后更新资产面板。
生成视频资产后展示预览，并允许用户继续修改。
当用户满意后输出最终结果。`;

function cleanText(value) {
  return String(value || '').trim();
}

export function systemSkillCreateBody({ values, body }) {
  return {
    ...body,
    version: cleanText(body.version) || '0.1.0',
    skill_markdown: cleanText(values.skill_markdown)
  };
}

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
            { target_status: row.status === 'disabled' ? 'active' : 'disabled', preview_token: context.previewToken, reason: context.reason }
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
	      { key: 'skill_scope', title: '范围', render: (row) => optionLabel(skillScopeOptions, row.skill_scope) },
	      { key: 'latest_version_id', title: '最近版本', render: (row) => row.latest_version_id || '-' },
	      { key: 'active_test_case_count', title: '测试用例', render: (row) => `${row.active_test_case_count || 0}/3` },
	      { key: 'published_version_id', title: '发布版本', render: (row) => row.published_version_id || '-' },
	      { key: 'updated_at', title: '更新时间', render: (row) => formatDateTime(row.updated_at) }
	    ],
    create: {
      title: '创建系统 Skill 草稿',
      path: '/api/admin/skills/system',
      pagePath: '/admin/skills/system/new',
      body: systemSkillCreateBody,
      fields: [
        { name: 'skill_name', label: 'Skill 名称', group: '基础信息', groupLayout: 'dense', required: true },
        { name: 'skill_tags', label: 'Skill 标签', group: '基础信息', groupLayout: 'dense', type: 'array', singleLine: true, hint: '逗号分隔多个标签。' },
        { name: 'version', label: '版本', group: '基础信息', groupLayout: 'dense', defaultValue: '0.1.0' },
        {
          name: 'skill_markdown',
          label: 'Skill 内容 Markdown',
          group: 'Skill 内容',
          groupHint: '源码使用中文标签段落，工具与 AG-UI 引用用结构化标签保存。',
          groupLayout: 'content',
          span: 'full',
          type: 'skill-markdown',
          required: true,
          rows: 24,
          defaultValue: defaultSystemSkillMarkdown,
          placeholder: '# 未命名 Skill <名称>\n\n## 说明 <说明>\n\n当用户需要...'
        }
      ]
    },
    actions: [
      {
        label: '测试结果',
        formTitle: '保存系统 Skill 测试结果',
        modalSize: 'wide',
        path: (row) => `/api/admin/skills/system/${row.skill_id}/test`,
        fields: [
          { name: 'version_id', label: '版本 ID', group: '测试对象', groupLayout: 'dense', required: true, source: 'published_version_id' },
          { name: 'test_run_id', label: '测试运行 ID', group: '测试对象', groupLayout: 'dense', required: true },
          { name: 'test_case_id', label: '测试用例 ID', group: '测试对象', groupLayout: 'dense' },
          {
            name: 'status',
            label: '测试状态',
            group: '测试对象',
            groupLayout: 'dense',
            options: [
              { label: '通过', value: 'passed' },
              { label: '失败', value: 'failed' },
              { label: '阻断', value: 'blocked' },
              { label: '超时', value: 'timeout' },
              { label: '拒绝', value: 'rejected' }
            ],
            required: true
          },
          {
            name: 'actual_elements_json',
            label: '实际输出元素 JSON',
            group: '测试结果',
            groupLayout: 'split',
            span: 'half',
            type: 'json-string',
            defaultValue: '[]',
            required: true,
            rows: 9
          },
          {
            name: 'safety_evidence_json',
            label: '安全证据 JSON',
            group: '测试结果',
            groupLayout: 'split',
            span: 'half',
            type: 'json-string',
            defaultValue: '{}',
            required: true,
            rows: 9
          }
        ],
        idempotencyKey: ({ values }) => `skill_test:${values.test_run_id}`
      },
	      {
	        label: '发布',
	        formTitle: '发布系统 Skill',
	        visible: (row) => !['published', 'deprecated'].includes(row.status) && Boolean(row.latest_version_id) && Number(row.active_test_case_count || 0) >= 3,
	        path: (row) => `/api/admin/skills/system/${row.skill_id}/publish`,
	        fields: [{ name: 'version_id', label: '版本 ID', required: true, source: 'latest_version_id', hint: '发布要求该版本至少存在 3 个 active 测试用例。' }]
	      },
	      {
	        label: '废弃',
	        tone: 'danger',
	        visible: (row) => row.status !== 'deprecated',
	        confirmPath: (row) => `/api/admin/skills/system/${row.skill_id}/deprecate`,
        body: ({ reason }) => ({ reason }),
        reason: ({ reason }) => reason
      }
    ]
  },
  'skills/reviews': {
    key: 'marketplace-skill-reviews',
    title: 'Skill 审核',
    description: '审核创作者提交的市场 Skill，审核通过后发布版本并生成 marketplace listing。',
    listPath: '/api/admin/marketplace/skill-reviews',
    rowId: reviewRowKey,
    emptyText: '暂无市场 Skill 审核单',
    statusOptions: marketplaceReviewStatusOptions,
    columns: [
      { key: 'skill_name', title: 'Skill', render: MarketplaceSkillCell },
      { key: 'creator_user_id', title: '创建者', width: 180 },
      { key: 'status', title: '审核状态', width: 120, render: (row) => <MarketplaceStatusBadge value={row.status} /> },
      { key: 'version_status', title: '版本状态', width: 120, render: (row) => <MarketplaceStatusBadge value={row.version_status} /> },
      { key: 'usage_credits', title: '使用费', width: 100, render: (row) => formatPoints(row.usage_credits) },
      { key: 'listing_status', title: 'Listing', width: 120, render: (row) => <MarketplaceStatusBadge value={row.listing_status} /> },
      { key: 'submitted_at', title: '提交时间', width: 180, render: (row) => formatDateTime(row.submitted_at || row.created_at) }
    ],
    detail: true,
    actions: [
      {
        label: '通过',
        requireReason: false,
        visible: (row) => ['submitted', 'reviewing'].includes(row.status),
        confirmPath: (row) => `/api/admin/skill-reviews/${reviewRowKey(row)}/approve`,
        objectLabel: (row) => `Skill ${row.skill_name || row.skill_id || reviewRowKey(row)}`,
        impactItems: ['审核通过后会发布该 Skill 版本。', '系统会创建或更新 marketplace listing。', '操作会写入 Skill 审核记录。'],
        body: ({ reason }) => ({ reason }),
        successNotice: () => 'Skill 已审核通过并发布'
      }
    ]
  },
  'skills/marketplace': {
    key: 'marketplace-listings',
    title: 'Skill 市场',
    description: '治理已发布市场 Skill listing；暂停后新安装会被拦截，历史 run 继续按快照恢复。',
    listPath: '/api/admin/marketplace/listings',
    rowId: 'listing_id',
    emptyText: '暂无市场 listing',
    statusOptions: marketplaceListingStatusOptions,
    columns: [
      { key: 'skill_name', title: 'Skill', render: MarketplaceSkillCell },
      { key: 'creator_user_id', title: '创建者', width: 180 },
      { key: 'status', title: 'Listing 状态', width: 130, render: (row) => <MarketplaceStatusBadge value={row.status} /> },
      { key: 'usage_credits', title: '使用费', width: 100, render: (row) => formatPoints(row.usage_credits) },
      { key: 'value_delivered_stage', title: '交付阶段', width: 140 },
      { key: 'listed_at', title: '上架时间', width: 180, render: (row) => formatDateTime(row.listed_at || row.created_at) }
    ],
    detail: true,
    actions: [
      {
        label: '暂停',
        tone: 'danger',
        method: 'POST',
        visible: (row) => row.status !== 'suspended',
        path: (row) => `/api/admin/listings/${row.listing_id}/suspend`,
        fields: [
          {
            name: 'reason_code',
            label: '暂停原因码',
            required: true,
            group: '治理原因',
            groupHint: '原因码会进入治理记录，建议使用 policy_risk、quality_issue 或 abuse_report 等可追溯值。',
            placeholder: 'policy_risk'
          }
        ],
        body: ({ values }) => ({ reason_code: values.reason_code }),
        successNotice: () => 'Listing 已暂停'
      }
    ]
  },
  'skills/refunds': {
    key: 'skill-refunds',
    title: 'Skill 退款',
    description: '审核 Skill 使用费退款仲裁；通过后反转 usage charge 并把 settlement 置为 reversed。',
    listPath: '/api/admin/marketplace/refund-cases',
    rowId: 'refund_case_id',
    emptyText: '暂无退款仲裁单',
    keywordFilter: false,
    statusOptions: refundCaseStatusOptions,
    columns: [
      { key: 'refund_case_id', title: '退款单', width: 220 },
      { key: 'skill_name', title: 'Skill', render: MarketplaceSkillCell },
      { key: 'status', title: '状态', width: 130, render: (row) => <MarketplaceStatusBadge value={row.status} /> },
      { key: 'reason_code', title: '原因码', width: 150 },
      { key: 'estimated_credits', title: '用户积分', width: 100, render: (row) => formatPoints(row.estimated_credits) },
      { key: 'creator_credits', title: '创作者 hold', width: 120, render: (row) => formatPoints(row.creator_credits) },
      { key: 'updated_at', title: '更新时间', width: 180, render: (row) => formatDateTime(row.updated_at) }
    ],
    detail: true,
    actions: [
      {
        label: '通过退款',
        tone: 'danger',
        requireReason: false,
        visible: (row) => ['refund_requested', 'refund_reviewing'].includes(row.status),
        confirmPath: (row) => `/api/admin/refund-cases/${row.refund_case_id}/approve`,
        objectLabel: (row) => `退款单 ${row.refund_case_id}`,
        impactItems: ['usage 将进入 refunded / released / refund_reversed。', '对应 settlement 将置为 reversed。', '该操作会影响创作者 hold 金额。'],
        body: () => ({}),
        successNotice: () => '退款已通过并完成结算反转'
      }
    ]
  },
  'skills/settlements': {
    key: 'skill-settlements',
    title: 'Skill 结算',
    description: '治理 Skill 使用费 settlement hold、平台分成、创作者收益、反转和内部出账确认；不接外部打款通道。',
    listPath: '/api/admin/marketplace/settlements',
    rowId: 'settlement_id',
    emptyText: '暂无结算记录',
    keywordFilter: false,
    statusOptions: settlementStatusOptions,
    columns: [
      { key: 'settlement_id', title: '结算单', width: 220 },
      { key: 'skill_name', title: 'Skill', render: MarketplaceSkillCell },
      { key: 'status', title: '状态', width: 130, render: (row) => <MarketplaceStatusBadge value={row.status} /> },
      { key: 'gross_credits', title: '总额', width: 90, render: (row) => formatPoints(row.gross_credits) },
      { key: 'platform_fee_credits', title: '平台分成', width: 110, render: (row) => formatPoints(row.platform_fee_credits) },
      { key: 'creator_credits', title: '创作者收益', width: 120, render: (row) => formatPoints(row.creator_credits) },
      { key: 'hold_until', title: 'Hold 到期', width: 180, render: (row) => formatDateTime(row.hold_until) }
    ],
    detail: true,
    actions: [
      {
        label: '解除 hold',
        method: 'POST',
        visible: (row) => row.status === 'pending_hold',
        path: (row) => `/api/admin/settlements/${row.settlement_id}/release-hold`,
        fields: [
          {
            name: 'reason_code',
            label: '解除原因码',
            required: true,
            defaultValue: 'hold_period_completed',
            group: '结算治理',
            groupHint: '仅当 hold 到期且无退款/风控冻结时允许解除。',
            placeholder: 'hold_period_completed'
          }
        ],
        idempotencyKey: ({ row }) => `settlement_release:${row.settlement_id}`,
        body: ({ values }) => ({ reason_code: values.reason_code }),
        successNotice: () => 'Settlement hold 已解除'
      },
      {
        label: '确认出账',
        method: 'POST',
        visible: (row) => row.status === 'eligible',
        path: (row) => `/api/admin/settlements/${row.settlement_id}/confirm-payout`,
        fields: [
          {
            name: 'payout_reference',
            label: '出账引用',
            required: true,
            group: '结算治理',
            groupHint: '填写内部 ledger、批次号或人工出账凭证引用，不填写外部密钥或敏感账号。',
            placeholder: 'manual-ledger-20260701-001'
          },
          {
            name: 'reason_code',
            label: '确认原因码',
            required: true,
            defaultValue: 'manual_payout_confirmed',
            placeholder: 'manual_payout_confirmed'
          }
        ],
        idempotencyKey: ({ row, values }) => `settlement_payout:${row.settlement_id}:${values.payout_reference || 'pending'}`,
        body: ({ values }) => ({ payout_reference: values.payout_reference, reason_code: values.reason_code }),
        successNotice: () => 'Settlement 已确认出账'
      }
    ]
  },
  'models/providers': {
    key: 'model-providers',
    title: '模型供应商',
    description: '配置供应商密钥引用；密钥不明文回显。',
    listPath: '/api/admin/models/providers',
    rowId: 'provider_id',
    emptyText: '暂无供应商',
    keywordFilter: false,
    statusOptions: activeStatusOptions,
	    columns: [
	      { key: 'provider_name', title: '供应商', render: ModelProviderCell },
	      statusColumn,
	      { key: 'secret_ref_status', title: '密钥状态', render: (row) => <Badge tone={row.secret_ref_status === 'configured' ? 'success' : 'warning'}>{secretRefStatusLabel(row.secret_ref_status)}</Badge> },
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
          { name: 'provider_code', label: '供应商编码', required: true },
          { name: 'provider_name', label: '供应商名称', required: true },
          { name: 'provider_type', label: '供应商类型' },
          { name: 'base_url', label: 'Base URL' },
          { name: 'status', label: '状态', options: activeStatusOptions },
          { name: 'secret_key_ref', label: '密钥引用', secret: true },
	          { name: 'config', label: '扩展配置 JSON', type: 'json', defaultValue: '{}' }
	        ]
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
    linkedPanel: {
      title: '模型供应商',
      field: 'provider_id',
      source: 'modelProviderItems',
      allLabel: '全部供应商',
      emptyText: '暂无供应商',
      emptyResultText: '该供应商下暂无模型',
      selectedSummary: (count) => `当前供应商 ${count} 个模型`,
      allSummary: (count) => `当前筛选共 ${count} 个模型`
    },
    filters: [
      { name: 'resource_type', label: '资源类型', options: resourceTypeOptions }
    ],
	    columns: [
	      { key: 'display_name', title: '模型', render: ModelIdentityCell },
	      { key: 'provider_id', title: '供应商', render: ModelProviderRefCell },
	      { key: 'resource_type', title: '资源类型', render: (row) => resourceTypeLabel(row.resource_type) },
	      statusColumn,
	      { key: 'is_default', title: '默认', render: (row) => (row.is_default || row.default_for_resource ? '是' : '否') }
	    ],
    create: {
      title: '新增模型',
      path: '/api/admin/models',
      modalSize: 'xl',
      fields: [
        {
          name: 'provider_id',
          label: '供应商',
          group: '基础信息',
          groupHint: '先确定模型归属的供应商，再填写供应商侧真实模型编码。左侧已选供应商时会自动带入。',
          groupLayout: 'dense',
          required: true,
          optionSource: 'modelProviders',
          hint: '模型调用会使用该供应商的基础 URL、密钥和连通性配置。'
        },
        {
          name: 'model_code',
          label: '模型编码',
          group: '基础信息',
          groupLayout: 'dense',
          required: true,
          placeholder: '例如 gpt-image-1、flux-kontext-pro',
          hint: '填写供应商 API 接收的真实模型标识，不是后台展示名称。'
        },
        {
          name: 'display_name',
          label: '模型名称',
          group: '基础信息',
          groupLayout: 'dense',
          required: true,
          placeholder: '例如 Seed Image Model',
          hint: '后台和选择器里展示给管理员看的名称，可以使用中文。'
        },
        {
          name: 'resource_type',
          label: '资源类型',
          group: '基础信息',
          groupLayout: 'dense',
          required: true,
          options: resourceTypeOptions,
          hint: '决定模型用于图片、视频、音乐或文本任务，也影响默认模型设置。'
        },
        {
          name: 'status',
          label: '状态',
          group: '基础信息',
          groupLayout: 'dense',
          defaultValue: 'active',
          options: activeStatusOptions,
          hint: '启用后可被业务侧调用；停用只保留配置，不参与选择。'
        },
        {
          name: 'billing_unit',
          label: '计费单位',
          group: '计费配置',
          groupHint: '不准备启用模型扣费时整组留空。只要填写价格快照或积分单价，就必须选择计费单位并填写用户积分单价。',
          groupLayout: 'dense',
          options: modelBillingUnitOptions,
          hint: '选择积分统计单位，例如一次生成、一张图片或一次调用。'
        },
        {
          name: 'unit_points',
          label: '用户积分单价',
          group: '计费配置',
          groupLayout: 'dense',
          type: 'number',
          placeholder: '例如 2.5',
          hint: '每个计费单位扣除的用户积分；配置计费时必须大于 0。'
        },
        {
          name: 'min_charge_points',
          label: '最低扣费积分',
          group: '计费配置',
          groupLayout: 'dense',
          type: 'number',
          placeholder: '例如 1',
          hint: '可选。用于设置单次调用最低扣费；留空则不设置最低值。'
        },
        {
          name: 'pricing_snapshot_id',
          label: '价格快照 ID',
          group: '计费配置',
          groupLayout: 'dense',
          placeholder: '留空自动生成',
          hint: '可选。用于追踪本次价格版本；配置计费但留空时后端会自动生成。'
        },
        {
          name: 'credential_id',
          label: '凭证 ID',
          group: '运行绑定',
          groupHint: '大多数模型可以直接使用供应商默认密钥；只有模型需要独立凭证或特殊能力标签时才填写本区。',
          groupLayout: 'dense',
          placeholder: '通常留空',
          hint: '可选。填写供应商凭证记录 ID；留空时使用供应商默认密钥。'
        },
        {
          name: 'capability_tags',
          label: '能力标签',
          group: '运行绑定',
          groupLayout: 'dense',
          type: 'array',
          singleLine: true,
          placeholder: '例如 image_generation, high_quality',
          hint: '可选。用于 Skill 或路由筛选，多个标签用逗号分隔。'
        },
        {
          name: 'route_config',
          label: '路由配置 JSON',
          group: '高级路由参数',
          groupHint: '通常保持默认 {}。只有需要覆盖供应商参数、路由策略或运行时参数时才填写 JSON。',
          groupLayout: 'single',
          type: 'json',
          defaultValue: '{}',
          rows: 8,
          hint: '必须是合法 JSON，例如 {"quality":"high"}。'
        }
      ]
    },
    actions: [
      {
        label: '编辑',
        method: 'PATCH',
        modalSize: 'xl',
        path: (row) => `/api/admin/models/${row.model_id}`,
        fields: [
          {
            name: 'provider_id',
            label: '供应商',
            group: '基础信息',
            groupHint: '供应商决定模型调用的基础 URL、密钥和连接配置；变更后会影响该模型后续调用。',
            groupLayout: 'dense',
            required: true,
            optionSource: 'modelProviders',
            hint: '选择这个模型归属的供应商。'
          },
          {
            name: 'model_code',
            label: '模型编码',
            group: '基础信息',
            groupLayout: 'dense',
            required: true,
            hint: '供应商 API 使用的真实模型标识。'
          },
          {
            name: 'display_name',
            label: '模型名称',
            group: '基础信息',
            groupLayout: 'dense',
            required: true,
            hint: '后台展示名，不影响供应商调用。'
          },
          {
            name: 'resource_type',
            label: '资源类型',
            group: '基础信息',
            groupLayout: 'dense',
            required: true,
            options: resourceTypeOptions,
            hint: '决定该模型用于哪类生成任务。'
          },
          {
            name: 'status',
            label: '状态',
            group: '基础信息',
            groupLayout: 'dense',
            options: activeStatusOptions,
            hint: '停用后不再参与业务侧模型选择。'
          },
          {
            name: 'billing_unit',
            label: '计费单位',
            group: '计费配置',
            groupHint: '编辑计费会创建新的价格快照并失效旧快照；不修改计费时保持价格字段为空。',
            groupLayout: 'dense',
            options: modelBillingUnitOptions,
            hint: '选择本次新价格的积分统计单位。'
          },
          {
            name: 'unit_points',
            label: '用户积分单价',
            group: '计费配置',
            groupLayout: 'dense',
            type: 'number',
            hint: '每个计费单位扣除的用户积分；新价格必须大于 0。'
          },
          {
            name: 'min_charge_points',
            label: '最低扣费积分',
            group: '计费配置',
            groupLayout: 'dense',
            type: 'number',
            hint: '可选。单次调用最低扣费。'
          },
          {
            name: 'pricing_snapshot_id',
            label: '新价格快照 ID',
            group: '计费配置',
            groupLayout: 'dense',
            placeholder: '留空自动生成',
            hint: '可选。填写后作为新价格版本 ID；留空则后端自动生成。'
          },
          {
            name: 'credential_id',
            label: '凭证 ID',
            group: '运行绑定',
            groupHint: '运行绑定用于控制单个模型的特殊凭证或能力标签；没有特殊要求时可以留空。',
            groupLayout: 'dense',
            placeholder: '通常留空',
            hint: '可选。独立凭证记录 ID；留空使用供应商默认密钥。'
          },
          {
            name: 'capability_tags',
            label: '能力标签',
            group: '运行绑定',
            groupLayout: 'dense',
            type: 'array',
            singleLine: true,
            placeholder: '例如 image_generation, high_quality',
            hint: '可选。多个标签用逗号分隔。'
          },
          {
            name: 'route_config',
            label: '路由配置 JSON',
            group: '高级路由参数',
            groupHint: '通常保持 {}。只有需要覆盖供应商运行参数、路由策略或模型参数时才填写。',
            groupLayout: 'single',
            type: 'json',
            defaultValue: '{}',
            rows: 8,
            hint: '必须是合法 JSON，例如 {"quality":"high"}。'
          }
        ]
      },
	      {
	        label: '设为默认',
	        visible: (row) => row.status === 'active' && !(row.is_default || row.default_for_resource),
	        requireReason: false,
	        confirmPath: () => '/api/admin/models/default',
	        body: (ctx) => ({ model_id: ctx.row.model_id, resource_type: ctx.row.resource_type, pricing_snapshot_id: ctx.row.pricing_snapshot_id })
	      },
	      {
	        label: (row) => (row.status === 'disabled' ? '启用' : '停用'),
	        tone: 'danger',
	        visible: (row) => !(row.status === 'active' && (row.is_default || row.default_for_resource)),
	        confirmPath: (row) => `/api/admin/models/${row.model_id}/status`,
        body: ({ reason, row }) => ({ status: row.status === 'disabled' ? 'active' : 'disabled', reason }),
        reason: ({ reason }) => reason
      }
    ]
  },
  tools: {
    key: 'tools',
    title: 'Tool 管理',
    description: '管理 Tool 定义、执行策略、确认要求、白名单和计价策略。',
    listPath: '/api/admin/tools',
    rowId: toolRowKey,
    detail: true,
    emptyText: '暂无 Tool',
    keywordFilter: false,
    defaultPageSize: 50,
    groupBy: {
      field: 'tool_type',
      title: 'Tool 类型',
      allLabel: '全部 Tool',
      label: toolTypeLabel
    },
    statusOptions: activeStatusOptions,
	    columns: [
	      { key: 'tool_name', title: 'Tool', width: 260, render: ToolIdentityCell },
	      { key: 'tool_type', title: '类型', width: 140, render: (row) => toolTypeLabel(row.tool_type) },
	      statusColumn,
      { key: 'execution_policy', title: '执行策略', width: 300, render: ToolExecutionPolicyCell },
      { key: 'pricing_policy', title: '计价策略', width: 240, render: ToolPricingPolicyCell }
    ],
    create: {
      title: '注册 Tool',
      path: '/api/admin/tools',
      modalSize: 'xl',
      fields: [
        {
          name: 'tool_name',
          label: 'Tool 名称',
          group: '基础信息',
          groupHint: '只注册 Tool 元信息和治理策略，不创建或部署运行时执行器。',
          groupLayout: 'dense',
          required: true,
          placeholder: '例如 storyboard_extract'
        },
        { name: 'tool_type', label: 'Tool 类型', group: '基础信息', groupLayout: 'dense', defaultValue: 'builtin', required: true },
        { name: 'display_name', label: '展示名称', group: '基础信息', groupLayout: 'dense', required: true },
        { name: 'description', label: '说明', group: '基础信息', groupLayout: 'single', textarea: true, required: true, rows: 3 },
        { name: 'status', label: '状态', group: '基础信息', groupLayout: 'dense', defaultValue: 'active', options: activeStatusOptions },
        { name: 'version', label: '版本', group: '基础信息', groupLayout: 'dense', defaultValue: '1.0.0' },
        {
          name: 'input_schema_json',
          label: '输入 Schema JSON',
          group: 'Schema',
          groupHint: '描述 Agent 从自然语言中整理给 Tool 的输入结构。',
          groupLayout: 'split',
          span: 'half',
          type: 'json-string',
          defaultValue: '{}',
          rows: 7
        },
        {
          name: 'output_schema_json',
          label: '输出 Schema JSON',
          group: 'Schema',
          groupLayout: 'split',
          span: 'half',
          type: 'json-string',
          defaultValue: '{}',
          rows: 7
        },
        { name: 'allowed', label: '允许使用', group: '执行策略', groupLayout: 'dense', type: 'checkbox', defaultValue: true },
        { name: 'risk_level', label: '风险等级', group: '执行策略', groupLayout: 'dense', options: riskLevelOptions, defaultValue: 'low', required: true },
        { name: 'requires_confirmation', label: '需要确认', group: '执行策略', groupLayout: 'dense', type: 'checkbox' },
        { name: 'timeout_ms', label: '超时毫秒', group: '执行策略', groupLayout: 'dense', type: 'number', defaultValue: 30000 },
        { name: 'retry_policy', label: '重试策略 JSON', group: '执行策略 JSON', groupLayout: 'split', span: 'half', type: 'json', defaultValue: '{}', rows: 5 },
        { name: 'cancel_policy', label: '取消策略 JSON', group: '执行策略 JSON', groupLayout: 'split', span: 'half', type: 'json', defaultValue: '{}', rows: 5 },
        { name: 'charge_mode', label: '计费模式', group: '计费规则', groupLayout: 'dense', options: chargeModeOptions, defaultValue: 'per_call', required: true },
        { name: 'billing_unit', label: '计费单位', group: '计费规则', groupLayout: 'dense', options: toolBillingUnitOptions, defaultValue: 'call', required: true },
        { name: 'unit_points', label: '单价积分', group: '计费规则', groupLayout: 'dense', type: 'number', defaultValue: 0 },
        { name: 'free_quota', label: '免费额度', group: '计费规则', groupLayout: 'dense', type: 'number', defaultValue: 0 },
        { name: 'min_charge_points', label: '最低扣费积分', group: '计费规则', groupLayout: 'dense', type: 'number', defaultValue: 0 },
        { name: 'reason', label: '注册原因', group: '审计原因', groupLayout: 'single', textarea: true, required: true, rows: 4 }
      ]
    },
    actions: [
      {
        label: '影响预览',
        requireReason: false,
        preview: (row) => adminApi.post(`/api/admin/tools/${toolRowKey(row)}/impact-preview`, { tool_type: row.tool_type, target_status: row.status }),
        confirm: () => Promise.resolve({ status: 'previewed' }),
        objectLabel: (row) => `Tool ${toolRowKey(row)}`,
	        impactItems: (preview) => [
	          `影响 Skill 数量：${preview?.impact_count ?? 0}`,
	          `绑定记录数量：${preview?.affected_skill_bindings ?? 0}`,
	          ...((preview?.affected_skills || []).length
	            ? preview.affected_skills.map((skill) => `Skill：${affectedSkillLabel(skill)}`)
	            : (preview?.affected_skill_ids || []).map((skillID) => `Skill：${skillID}`))
	        ],
        reason: () => ''
      },
      {
        label: '策略',
        method: 'PATCH',
        modalSize: 'xl',
        path: (row) => `/api/admin/tools/${toolRowKey(row)}/policy`,
        fields: [
          { name: 'tool_type', label: 'Tool 类型', group: 'Tool 标识', groupLayout: 'dense', required: true, disabled: true },
          { name: 'allowed', label: '允许使用', group: '执行控制', groupLayout: 'dense', type: 'checkbox', defaultValue: true },
          { name: 'risk_level', label: '风险等级', group: '执行控制', groupLayout: 'dense', options: riskLevelOptions, required: true },
          { name: 'requires_confirmation', label: '需要确认', group: '执行控制', groupLayout: 'dense', type: 'checkbox' },
          { name: 'timeout_ms', label: '超时毫秒', group: '执行控制', groupLayout: 'dense', type: 'number', hint: '仅允许正整数；0 表示保持当前后端配置。' },
          {
            name: 'retry_policy',
            label: '重试策略 JSON',
            group: '执行策略 JSON',
            groupHint: '仅保存策略摘要，不展示模型或工具原始返回。',
            groupLayout: 'split',
            span: 'half',
            type: 'json',
            defaultValue: '{}',
            rows: 6
          },
          { name: 'cancel_policy', label: '取消策略 JSON', group: '执行策略 JSON', groupLayout: 'split', span: 'half', type: 'json', defaultValue: '{}', rows: 6 },
          { name: 'reason', label: '操作原因', group: '审计原因', groupLayout: 'single', textarea: true, required: true, rows: 4 }
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
        modalSize: 'wide',
        path: (row) => `/api/admin/tools/${toolRowKey(row)}/pricing-policy`,
        fields: [
          { name: 'tool_type', label: 'Tool 类型', group: 'Tool 标识', groupLayout: 'dense', required: true, disabled: true },
          { name: 'pricing_policy_id', label: '当前价格策略 ID', group: 'Tool 标识', groupLayout: 'dense', disabled: true, virtual: true },
          { name: 'charge_mode', label: '计费模式', group: '计费规则', groupLayout: 'dense', options: chargeModeOptions, required: true },
          { name: 'billing_unit', label: '计费单位', group: '计费规则', groupLayout: 'dense', defaultValue: 'call', required: true },
          { name: 'unit_points', label: '单价积分', group: '计费规则', groupLayout: 'dense', type: 'number', required: true },
          { name: 'free_quota', label: '免费额度', group: '计费规则', groupLayout: 'dense', type: 'number', hint: '每个计费周期内免费次数或数量。' },
          { name: 'min_charge_points', label: '最低扣费积分', group: '计费规则', groupLayout: 'dense', type: 'number' },
          { name: 'reason', label: '操作原因', group: '审计原因', groupLayout: 'single', textarea: true, required: true, rows: 4 }
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
        modalSize: 'wide',
        path: (row) => `/api/admin/tools/${toolRowKey(row)}/whitelist`,
        fields: [
          { name: 'tool_type', label: 'Tool 类型', group: 'Tool 标识', groupLayout: 'dense', required: true, disabled: true },
          {
            name: 'scope_type',
            label: '作用范围',
            group: '白名单规则',
            groupHint: '白名单是覆盖规则：同一用户/空间/企业命中后会覆盖全局 allowed 策略。',
            groupLayout: 'dense',
            options: scopeTypeOptions,
            required: true
          },
          { name: 'scope_id', label: '范围 ID', group: '白名单规则', groupLayout: 'dense', required: true },
          { name: 'allowed', label: '允许使用', group: '白名单规则', groupLayout: 'dense', type: 'checkbox', defaultValue: true },
          { name: 'reason', label: '操作原因', group: '审计原因', groupLayout: 'single', textarea: true, required: true, rows: 4 }
        ],
        reason: ({ values }) => values.reason,
        body: ({ values }) => ({
          tool_type: values.tool_type,
          scope_type: values.scope_type,
          scope_id: values.scope_id,
          allowed: values.allowed,
          reason: values.reason
        })
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
        { name: 'reason', label: '创建原因', required: true, rows: 3 }
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
  'billing/packages': {
    key: 'billing-packages',
    title: '付费套餐管理',
    description: '管理个人积分包、个人会员、企业套餐、生成加购包和创作者权益包。',
    listPath: '/api/admin/billing/packages',
    rowId: 'package_id',
    emptyText: '暂无付费套餐',
    keywordFilter: false,
    statusOptions: packageStatusOptions,
    filters: [
      { name: 'package_type', label: '套餐类型', options: packageTypeOptions },
      { name: 'target_scope', label: '面向对象', options: packageTargetScopeOptions }
    ],
    columns: [
      { key: 'name', title: '套餐', render: (row) => row.name || row.display_name || row.package_id },
      { key: 'package_type', title: '类型', render: (row) => packageTypeLabel(row.package_type) },
      { key: 'target_scope', title: '对象', render: (row) => targetScopeLabel(row.target_scope) },
      { key: 'billing_mode', title: '计费', render: (row) => billingModeLabel(row.billing_mode) },
      { key: 'price_amount', title: '价格', render: (row) => formatCny(row.price_amount) },
      { key: 'points', title: '积分', render: (row) => formatPoints(row.points) },
      statusColumn
    ],
    create: {
      title: '创建付费套餐',
      path: '/api/admin/billing/packages',
      fields: [
        { name: 'package_id', label: '套餐 ID', group: '基础信息', groupLayout: 'dense', required: true },
        { name: 'name', label: '套餐名称', group: '基础信息', groupLayout: 'dense', required: true },
        { name: 'package_type', label: '套餐类型', group: '基础信息', groupLayout: 'dense', options: packageTypeOptions, defaultValue: 'personal_credit_pack', required: true },
        { name: 'target_scope', label: '面向对象', group: '基础信息', groupLayout: 'dense', options: packageTargetScopeOptions, defaultValue: 'personal', required: true },
        { name: 'billing_mode', label: '计费模式', group: '定价', groupLayout: 'dense', options: billingModeOptions, defaultValue: 'one_time', required: true },
        { name: 'price_amount', label: '售价（分）', group: '定价', groupLayout: 'dense', type: 'number', required: true },
        { name: 'currency', label: '币种', group: '定价', groupLayout: 'dense', defaultValue: 'CNY', required: true },
        { name: 'granted_points', label: '发放积分', group: '积分', groupLayout: 'dense', type: 'number', required: true },
        { name: 'bonus_points', label: '赠送积分', group: '积分', groupLayout: 'dense', type: 'number' },
        { name: 'credit_expiry_policy', label: '积分有效期', group: '积分', groupLayout: 'dense', defaultValue: 'P1M', required: true },
        { name: 'spend_scope', label: '消费范围', group: '积分', groupLayout: 'single', type: 'array', defaultValue: 'tool_generation\nskill_usage', required: true },
        { name: 'settlement_eligible', label: '可用于创作者结算', group: '积分', groupLayout: 'dense', type: 'checkbox', defaultValue: true },
        { name: 'entitlement_policy', label: '权益策略 JSON', group: '权益策略', groupLayout: 'single', type: 'json', defaultValue: '{"priority_queue":false}' },
        { name: 'renewal_policy', label: '续费策略 JSON', group: '权益策略', groupLayout: 'single', type: 'json', defaultValue: '{"mode":"none"}' },
        { name: 'refund_policy', label: '退款策略 JSON', group: '权益策略', groupLayout: 'single', type: 'json', defaultValue: '{"mode":"unused_refund"}' },
        { name: 'visible_scope', label: '可见范围', group: '发布', groupLayout: 'dense', defaultValue: 'all_users' },
        { name: 'status', label: '状态', group: '发布', groupLayout: 'dense', options: packageStatusOptions, defaultValue: 'draft', required: true },
        { name: 'reason', label: '操作原因', group: '审计原因', groupLayout: 'single', textarea: true, required: true, rows: 3 }
      ]
    },
    actions: [
      {
        label: '编辑',
        method: 'PATCH',
        path: (row) => `/api/admin/billing/packages/${row.package_id}`,
        fields: [
          { name: 'package_id', label: '套餐 ID', group: '基础信息', groupLayout: 'dense', disabled: true, required: true },
          { name: 'name', label: '套餐名称', group: '基础信息', groupLayout: 'dense', required: true },
          { name: 'package_type', label: '套餐类型', group: '基础信息', groupLayout: 'dense', options: packageTypeOptions, required: true },
          { name: 'target_scope', label: '面向对象', group: '基础信息', groupLayout: 'dense', options: packageTargetScopeOptions, required: true },
          { name: 'billing_mode', label: '计费模式', group: '定价', groupLayout: 'dense', options: billingModeOptions, required: true },
          { name: 'price_amount', label: '售价（分）', group: '定价', groupLayout: 'dense', type: 'number', required: true },
          { name: 'currency', label: '币种', group: '定价', groupLayout: 'dense', required: true },
          { name: 'granted_points', label: '发放积分', group: '积分', groupLayout: 'dense', type: 'number', required: true },
          { name: 'bonus_points', label: '赠送积分', group: '积分', groupLayout: 'dense', type: 'number' },
          { name: 'credit_expiry_policy', label: '积分有效期', group: '积分', groupLayout: 'dense', required: true },
          { name: 'spend_scope', label: '消费范围', group: '积分', groupLayout: 'single', type: 'array', required: true },
          { name: 'settlement_eligible', label: '可用于创作者结算', group: '积分', groupLayout: 'dense', type: 'checkbox' },
          { name: 'entitlement_policy', label: '权益策略 JSON', group: '权益策略', groupLayout: 'single', type: 'json' },
          { name: 'renewal_policy', label: '续费策略 JSON', group: '权益策略', groupLayout: 'single', type: 'json' },
          { name: 'refund_policy', label: '退款策略 JSON', group: '权益策略', groupLayout: 'single', type: 'json' },
          { name: 'visible_scope', label: '可见范围', group: '发布', groupLayout: 'dense' },
          { name: 'status', label: '状态', group: '发布', groupLayout: 'dense', options: packageStatusOptions, required: true },
          { name: 'reason', label: '操作原因', group: '审计原因', groupLayout: 'single', textarea: true, required: true, rows: 3 }
        ]
      },
      {
        label: (row) => (row.status === 'active' ? '暂停' : '上架'),
        tone: 'danger',
        confirmPath: (row) => `/api/admin/billing/packages/${row.package_id}/status`,
        body: ({ reason, row }) => ({ status: row.status === 'active' ? 'paused' : 'active', reason }),
        reason: ({ reason }) => reason
      }
    ]
  },
  'billing/skus': {
    key: 'billing-skus',
    title: 'SKU 管理',
    description: '管理套餐在不同渠道、币种和活动价下的 SKU。',
    listPath: '/api/admin/billing/skus',
    rowId: 'sku_id',
    emptyText: '暂无 SKU',
    keywordFilter: false,
    statusOptions: activeStatusOptions,
    filters: [{ name: 'package_id', label: '套餐 ID' }],
    columns: [
      { key: 'sku_id', title: 'SKU' },
      { key: 'package_id', title: '套餐 ID' },
      { key: 'channel_code', title: '渠道' },
      { key: 'price_amount', title: '价格', render: (row) => formatCny(row.activity_price_amount || row.price_amount) },
      statusColumn
    ],
    create: {
      title: '创建 SKU',
      path: '/api/admin/billing/skus',
      fields: [
        { name: 'package_id', label: '套餐 ID', required: true },
        { name: 'sku_id', label: 'SKU ID' },
        { name: 'channel_code', label: '渠道', defaultValue: 'default' },
        { name: 'price_amount', label: '价格（分）', type: 'number', required: true },
        { name: 'currency', label: '币种', defaultValue: 'CNY', required: true },
        { name: 'activity_price_amount', label: '活动价（分）', type: 'number' },
        { name: 'effective_at', label: '生效时间', type: 'datetime-local' },
        { name: 'expired_at', label: '失效时间', type: 'datetime-local' },
        { name: 'reason', label: '创建原因', required: true, rows: 3 }
      ]
    }
  },
  'billing/orders': {
    key: 'billing-orders',
    title: '订单管理',
    description: '查看套餐订单、支付状态、积分批次和权益快照。',
    listPath: '/api/admin/billing/orders',
    rowId: 'order_id',
    emptyText: '暂无订单',
    keywordFilter: false,
    filters: [
      { name: 'payment_status', label: '支付状态', options: paymentStatusOptions },
      { name: 'account_id', label: '积分账户 ID' },
      { name: 'user_id', label: '用户 ID' }
    ],
    columns: [
      { key: 'order_id', title: '订单' },
      { key: 'package_id', title: '套餐' },
      { key: 'target_scope', title: '对象', render: (row) => targetScopeLabel(row.target_scope) },
      { key: 'price_amount', title: '金额', render: (row) => formatCny(row.price_amount) },
      { key: 'points', title: '积分', render: (row) => formatPoints(row.points) },
      { key: 'payment_status', title: '支付', render: (row) => <Badge tone={statusTone(row.payment_status)}>{statusLabel(row.payment_status)}</Badge> },
      createdColumn
    ]
  },
  'billing/credit-lots': {
    key: 'billing-credit-lots',
    title: '积分批次管理',
    description: '查看由套餐、兑换码和后台发放生成的积分批次、有效期和结算资格。',
    listPath: '/api/admin/billing/credit-lots',
    rowId: 'lot_id',
    emptyText: '暂无积分批次',
    keywordFilter: false,
    statusOptions: activeStatusOptions,
    filters: [
      { name: 'account_id', label: '账户 ID' },
      { name: 'source_type', label: '来源类型' }
    ],
    columns: [
      { key: 'lot_id', title: '批次 ID' },
      { key: 'account_id', title: '账户 ID' },
      { key: 'source_type', title: '来源' },
      { key: 'available_points', title: '可用', render: (row) => formatPoints(row.available_points) },
      { key: 'consumed_points', title: '已消耗', render: (row) => formatPoints(row.consumed_points) },
      { key: 'expires_at', title: '过期时间', render: (row) => formatDateTime(row.expires_at) },
      statusColumn
    ]
  },
  'billing/redeem-codes': {
    key: 'billing-redeem-codes',
    title: '兑换码管理',
    description: '财务视角查看和创建兑换码批次；不展示完整兑换码历史。',
    listPath: '/api/admin/billing/redeem-codes',
    rowId: 'batch_id',
    emptyText: '暂无兑换码批次',
    keywordFilter: false,
    statusOptions: activeStatusOptions,
    columns: [
      { key: 'batch_no', title: '批次', render: (row) => row.batch_no || row.batch_id },
      { key: 'account_type', title: '账户类型' },
      { key: 'points', title: '积分', render: (row) => formatPoints(row.points) },
      { key: 'count', title: '数量' },
      statusColumn
    ]
  },
  'billing/enterprise-contracts': {
    key: 'billing-enterprise-contracts',
    title: '企业合同管理',
    description: '查看企业套餐合同、席位额度、预算和审批策略。',
    listPath: '/api/admin/billing/enterprise-contracts',
    rowId: 'contract_id',
    emptyText: '暂无企业合同',
    keywordFilter: false,
    filters: [
      { name: 'enterprise_id', label: '企业 ID' },
      { name: 'contract_status', label: '合同状态', options: contractStatusOptions }
    ],
    columns: [
      { key: 'contract_id', title: '合同 ID' },
      { key: 'enterprise_id', title: '企业 ID' },
      { key: 'package_id', title: '套餐 ID' },
      { key: 'seat_quota', title: '席位' },
      { key: 'budget_points', title: '预算积分', render: (row) => formatPoints(row.budget_points) },
      { key: 'contract_status', title: '状态', render: (row) => <Badge tone={statusTone(row.contract_status)}>{statusLabel(row.contract_status)}</Badge> }
    ]
  },
  'billing/invoices': {
    key: 'billing-invoices',
    title: '发票 / 财务管理',
    description: '查看企业账单、发票状态和到期信息。',
    listPath: '/api/admin/billing/invoices',
    rowId: 'invoice_id',
    emptyText: '暂无发票记录',
    keywordFilter: false,
    filters: [
      { name: 'enterprise_id', label: '企业 ID' },
      { name: 'invoice_status', label: '发票状态', options: invoiceStatusOptions }
    ],
    columns: [
      { key: 'invoice_id', title: '发票 ID' },
      { key: 'enterprise_id', title: '企业 ID' },
      { key: 'amount', title: '金额', render: (row) => formatCny(row.amount) },
      { key: 'invoice_status', title: '状态', render: (row) => <Badge tone={statusTone(row.invoice_status)}>{statusLabel(row.invoice_status)}</Badge> },
      { key: 'due_at', title: '到期时间', render: (row) => formatDateTime(row.due_at) }
    ]
  },
  'billing/promotions': {
    key: 'billing-promotions',
    title: '促销活动',
    description: '查看测试环境套餐活动价和可见范围。',
    listPath: '/api/admin/billing/promotions',
    rowId: 'promotion_id',
    emptyText: '暂无促销活动',
    keywordFilter: false,
    statusOptions: activeStatusOptions,
    filters: [{ name: 'package_id', label: '套餐 ID' }],
    columns: [
      { key: 'promotion_name', title: '活动' },
      { key: 'package_id', title: '套餐 ID' },
      { key: 'visible_scope', title: '可见范围' },
      { key: 'starts_at', title: '开始时间', render: (row) => formatDateTime(row.starts_at) },
      statusColumn
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
        preview: (row, reason) => adminApi.previewTakeDownWork(row.public_work_id, { reason, notify_author: true }),
        confirm: (row, context) =>
          adminApi.confirmTakeDownWork(
            row.public_work_id,
            { reason: context.reason, preview_token: context.previewToken, notify_author: true }
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
    columns: [text('element_type', '元素类型'), { key: 'display_name', title: '展示名' }, statusColumn, { key: 'schema_version', title: 'Schema 版本' }]
  }
};
