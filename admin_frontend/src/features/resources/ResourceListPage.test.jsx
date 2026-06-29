import { render, screen } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import {
  buildDetailLabels,
  buildGroupItems,
  buildQueryParams,
  createSourceFromFilters,
  filterRowsByGroup,
  initialFilters,
  modelProviderItems,
  prepareBody,
  prepareCreateBody,
  resolveRowIdentifier,
  RowDetails,
  validateRequiredFields,
  visibleRowActions
} from './ResourceListPage.jsx';

describe('ResourceListPage helpers', () => {
  test('resolves row identifiers from either a function or a field name', () => {
    expect(resolveRowIdentifier({ tool_name: 'draw', tool_type: 'builtin' }, (row) => `${row.tool_name}:${row.tool_type}`)).toBe('draw:builtin');
    expect(resolveRowIdentifier({ admin_id: 'adm_1' }, 'admin_id')).toBe('adm_1');
  });

  test('normalizes datetime-local and numeric create fields before posting', () => {
    const body = prepareCreateBody(
      {
        code_expires_at: '2026-07-06T08:30',
        count: '20',
        points: '50',
        reason: '运营活动'
      },
      {
        create: {
          fields: [
            { name: 'code_expires_at', type: 'datetime-local' },
            { name: 'count', type: 'number' },
            { name: 'points', type: 'number' },
            { name: 'reason' }
          ]
        }
      }
    );

    expect(body.code_expires_at).toBe(new Date('2026-07-06T08:30').toISOString());
    expect(body.count).toBe(20);
    expect(body.points).toBe(50);
    expect(body.reason).toBe('运营活动');
  });

  test('normalizes JSON, JSON string, array and checkbox action fields', () => {
    const body = prepareBody(
      {
        route_config: '{"timeout_ms":30000}',
        skill_spec_json: '{"steps":[]}',
        capability_tags: 'image\nfast',
        requires_confirmation: true
      },
      [
        { name: 'route_config', type: 'json' },
        { name: 'skill_spec_json', type: 'json-string' },
        { name: 'capability_tags', type: 'array' },
        { name: 'requires_confirmation', type: 'checkbox' }
      ]
    );

    expect(body.route_config).toEqual({ timeout_ms: 30000 });
    expect(body.skill_spec_json).toBe('{"steps":[]}');
    expect(body.capability_tags).toEqual(['image', 'fast']);
    expect(body.requires_confirmation).toBe(true);
  });

  test('validates required fields before submitting admin forms', () => {
    const errors = validateRequiredFields(
      [
        { name: 'account', label: '管理员账号', required: true },
        { name: 'reason', label: '操作原因', required: true },
        { name: 'memo', label: '备注' },
        { name: 'draft_note', label: '草稿说明', required: true, virtual: true }
      ],
      { account: '', reason: '补充后台账号' }
    );

    expect(errors).toEqual({ account: '请填写管理员账号' });
  });

  test('skips virtual fields and allows create forms to compose backend payloads', () => {
    const body = prepareCreateBody(
      {
        skill_key: 'storyboard',
        invocation_rule: '用户需要分镜时调用'
      },
      {
        create: {
          fields: [
            { name: 'skill_key' },
            { name: 'invocation_rule', virtual: true }
          ],
          body: ({ values, body: prepared }) => ({
            ...prepared,
            skill_spec_json: JSON.stringify({ invocation_rule: values.invocation_rule })
          })
        }
      }
    );

    expect(body).toEqual({
      skill_key: 'storyboard',
      skill_spec_json: '{"invocation_rule":"用户需要分镜时调用"}'
    });
  });

  test('builds Chinese detail labels from table columns and form fields', () => {
    const labels = buildDetailLabels({
      columns: [{ key: 'public_nickname', title: '公开昵称' }],
      create: {
        fields: [{ name: 'reason', label: '创建原因' }]
      },
      actions: [{ fields: [{ name: 'test_reason', label: '测试原因' }] }]
    });

    expect(labels.public_nickname).toBe('公开昵称');
    expect(labels.reason).toBe('创建原因');
    expect(labels.test_reason).toBe('测试原因');
    expect(labels.trace_id).toBe('Trace ID');
  });

  test('renders common enum detail values as Chinese labels', () => {
    render(
      <RowDetails
        row={{
          status: 'active',
          tool_type: 'model_generation',
          risk_level: 'medium',
          charge_mode: 'model_generation',
          billing_unit: 'asset'
        }}
      />
    );

    expect(screen.getByText('启用')).toBeInTheDocument();
    expect(screen.getByText('模型生成 Tool')).toBeInTheDocument();
    expect(screen.getByText('中风险')).toBeInTheDocument();
    expect(screen.getByText('模型生成')).toBeInTheDocument();
    expect(screen.getByText('按资产')).toBeInTheDocument();
  });

  test('builds grouped resource indexes and filters rows by active group', () => {
    const rows = [
      { tool_name: 'draw', tool_type: 'builtin' },
      { tool_name: 'fetch', tool_type: 'http' },
      { tool_name: 'clip', tool_type: 'builtin' }
    ];
    const groupBy = {
      field: 'tool_type',
      allLabel: '全部 Tool',
      label: (value) => ({ builtin: '内置 Tool', http: 'HTTP Tool' })[value] || value
    };

    expect(buildGroupItems(rows, groupBy)).toEqual([
      { value: '', label: '全部 Tool', count: 3 },
      { value: 'http', label: 'HTTP Tool', count: 1 },
      { value: 'builtin', label: '内置 Tool', count: 2 }
    ]);
    expect(filterRowsByGroup(rows, groupBy, 'builtin')).toEqual([
      { tool_name: 'draw', tool_type: 'builtin' },
      { tool_name: 'clip', tool_type: 'builtin' }
    ]);
  });

  test('builds linked provider filters for model management', () => {
    const config = {
      defaultPageSize: 10,
      keywordFilter: false,
      statusOptions: [{ label: '启用', value: 'active' }],
      linkedPanel: { field: 'provider_id', queryName: 'provider_id' },
      filters: [{ name: 'resource_type' }]
    };
    const searchParams = new URLSearchParams('provider_id=mp_seed&resource_type=image&status=active');
    const filters = initialFilters(config, searchParams);

    expect(filters).toMatchObject({
      provider_id: 'mp_seed',
      resource_type: 'image',
      status: 'active'
    });
    expect(buildQueryParams(filters, config)).toMatchObject({
      provider_id: 'mp_seed',
      resource_type: 'image',
      status: 'active',
      page_size: 10
    });
    expect(createSourceFromFilters(config, filters)).toEqual({ provider_id: 'mp_seed' });
  });

  test('normalizes model provider items for linked side panel', () => {
    expect(
      modelProviderItems({
        items: [
          {
            provider_id: 'mp_seed',
            provider_name: 'Seed Provider',
            provider_code: 'seed-provider',
            status: 'active'
          }
        ]
      })
    ).toEqual([
      {
        label: 'Seed Provider',
        value: 'mp_seed',
        meta: 'seed-provider'
      }
    ]);
  });

  test('filters row actions by per-row visibility rules', () => {
    const actions = [
      { label: '编辑' },
      { label: '设为默认', visible: (row) => !row.is_default },
      { label: '停用', visible: (row) => !(row.status === 'active' && row.is_default) }
    ];

    expect(visibleRowActions(actions, { status: 'active', is_default: true }).map((action) => action.label)).toEqual(['编辑']);
    expect(visibleRowActions(actions, { status: 'active', is_default: false }).map((action) => action.label)).toEqual(['编辑', '设为默认', '停用']);
  });
});
