import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';

vi.mock('../../lib/api/admin.js', () => ({
  adminApi: {
    post: vi.fn(() => Promise.resolve({})),
    previewTakeDownWork: vi.fn(() => Promise.resolve({})),
    confirmTakeDownWork: vi.fn(() => Promise.resolve({}))
  }
}));

import { adminApi } from '../../lib/api/admin.js';
import { pageConfigs } from './pageConfigs.jsx';

describe('admin resource page configs', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  test('uses stable fallback ids for tools and skill reviews', async () => {
    const tool = { tool_name: 'draw', tool_type: 'builtin', status: 'active' };
    expect(pageConfigs.tools.rowId(tool)).toBe('draw:builtin');
    await pageConfigs.tools.actions[0].preview(tool, '例行检查');
    expect(adminApi.post).toHaveBeenCalledWith('/api/admin/tools/draw:builtin/impact-preview', {
      target_status: 'active',
      tool_type: 'builtin'
    });
    expect(pageConfigs.tools.actions[4].confirmPath(tool)).toBe('/api/admin/tools/draw:builtin/status');
    expect(pageConfigs.tools.actions[4].body({ reason: '例行检查', row: tool })).toEqual({
      tool_type: 'builtin',
      status: 'disabled',
      reason: '例行检查'
    });

    const review = { version_id: 'skv_1' };
    expect(pageConfigs['skills/reviews'].rowId(review)).toBe('skv_1');
    expect(pageConfigs['skills/reviews'].actions[1].path(review)).toBe('/api/admin/skills/reviews/skv_1/confirm');
    expect(pageConfigs['skills/reviews'].actions[1].body({ values: { reason: '内容不完整' } })).toEqual({
      decision: 'reject',
      reason: '内容不完整'
    });
  });

  test('describes tool management as a complete policy and pricing surface', () => {
    const config = pageConfigs.tools;
    expect(config.detail).toBe(true);
    expect(config.create).toBeUndefined();
    expect(config.defaultPageSize).toBe(50);
    expect(config.groupBy).toMatchObject({
      field: 'tool_type',
      title: 'Tool 类型',
      allLabel: '全部 Tool'
    });
    expect(config.columns.map((column) => column.key)).toEqual([
      'tool_name',
      'tool_type',
      'status',
      'execution_policy',
      'pricing_policy'
    ]);

    const pricingBody = config.actions[2].body({
      values: {
        tool_type: 'browser',
        charge_mode: 'per_call',
        billing_unit: 'call',
        unit_points: '9',
        free_quota: '2',
        min_charge_points: '1'
      }
    });
    expect(pricingBody).toEqual({
      tool_type: 'browser',
      charge_mode: 'per_call',
      billing_unit: 'call',
      unit_points: 9,
      free_quota: 2,
      min_charge_points: 1
    });

    expect(config.actions[2].modalSize).toBe('wide');
    expect(config.actions[3].modalSize).toBe('wide');
    expect(config.actions[3].body({ values: { tool_type: 'browser', scope_type: 'enterprise', scope_id: 'ent_1', allowed: false, reason: '企业禁用' } })).toEqual({
      tool_type: 'browser',
      scope_type: 'enterprise',
      scope_id: 'ent_1',
      allowed: false,
      reason: '企业禁用'
    });

    render(config.columns.find((column) => column.key === 'pricing_policy').render({ charge_mode: 'model_generation', billing_unit: 'asset', unit_points: 12 }));
    expect(screen.getByText('模型生成')).toBeInTheDocument();
    expect(screen.getByText('按资产 · 12 积分')).toBeInTheDocument();
  });

  test('sends fields required by backend confirmations', async () => {
    const model = { model_id: 'mdl_1', resource_type: 'image', pricing_snapshot_id: 'price_1' };
    expect(pageConfigs.models.actions[1].body({ row: model })).toEqual({
      model_id: 'mdl_1',
      resource_type: 'image',
      pricing_snapshot_id: 'price_1'
    });

    const publicWork = { public_work_id: 'pw_1' };
    await pageConfigs['works/public'].actions[0].confirm(publicWork, { reason: '内容风险', previewToken: 'prev_1' });
    expect(adminApi.confirmTakeDownWork).toHaveBeenCalledWith(
      'pw_1',
      { reason: '内容风险', preview_token: 'prev_1', notify_author: true }
    );
  });

  test('maps tool policy form fields to backend body', () => {
    const body = pageConfigs.tools.actions[1].body({
      values: {
        tool_type: 'builtin',
        allowed: true,
        risk_level: 'high',
        requires_confirmation: true,
        timeout_ms: '30000',
        retry_policy: '{"max":"1"}',
        cancel_policy: '{}'
      }
    });

    expect(body).toEqual({
      tool_type: 'builtin',
      allowed: true,
      risk_level: 'high',
      requires_confirmation: true,
      timeout_ms: 30000,
      retry_policy: { max: '1' },
      cancel_policy: {}
    });
  });

  test('uses markdown editor for complex skill creation content', () => {
    const create = pageConfigs['skills/system'].create;

    expect(create.pagePath).toBe('/admin/skills/system/new');
    expect(create.fields.find((field) => field.name === 'skill_markdown')).toMatchObject({
      group: 'Skill 内容',
      groupLayout: 'content',
      rows: 24,
      type: 'skill-markdown'
    });
    expect(create.fields.find((field) => field.name === 'skill_tags')).toMatchObject({ type: 'array', singleLine: true });
    expect(create.fields.find((field) => field.name === 'skill_markdown').defaultValue).toContain('## 输入 <输入>');
    expect(create.fields.find((field) => field.name === 'skill_markdown').defaultValue).toContain('如果缺少风格偏好');
    expect(create.fields.find((field) => field.name === 'skill_markdown').defaultValue).toContain('## 结果输出 <结果输出>');
    expect(create.fields.find((field) => field.name === 'skill_markdown').defaultValue).toContain('Agent 可以先输出故事板供用户审阅');
    expect(create.fields.find((field) => field.name === 'skill_markdown').defaultValue).toContain('<tool id="image_generate:model_generation">图片生成</tool>');
    expect(create.fields.find((field) => field.name === 'skill_markdown').defaultValue).toContain('<agui id="storyboard_panel">故事板面板</agui>');

    const testAction = pageConfigs['skills/system'].actions[0];
    expect(testAction.modalSize).toBe('wide');
    expect(testAction.fields.find((field) => field.name === 'safety_evidence_json')).toMatchObject({
      group: '测试结果',
      groupLayout: 'split',
      span: 'half',
      rows: 9
    });
  });

  test('sends system skill markdown as backend contract input', () => {
    const body = pageConfigs['skills/system'].create.body({
      body: {
        skill_name: '故事板助手',
        skill_tags: ['视频', '故事板'],
        version: '0.1.0'
      },
      values: {
        skill_markdown: `# 故事板助手 <名称>

## 说明 <说明>

当用户需要把素材整理成故事板时调用。

## 计划 <计划>

先分析素材，再生成镜头规划。

## 工具引用 <工具引用>

<tool id="image_generate:model_generation">图片生成</tool>

## AG-UI 元素引用 <AG-UI元素引用>

对话框外：
<agui id="storyboard_panel">故事板面板</agui>

## 结果输出 <结果输出>

故事板`
      }
    });

    expect(body).toMatchObject({
      skill_name: '故事板助手',
      skill_tags: ['视频', '故事板'],
      version: '0.1.0'
    });
    expect(body.skill_markdown).toContain('<tool id="image_generate:model_generation">图片生成</tool>');
    expect(body.skill_markdown).toContain('<agui id="storyboard_panel">故事板面板</agui>');
    expect(body.skill_spec_json).toBeUndefined();
    expect(body.input_schema_json).toBeUndefined();
    expect(body.output_schema_json).toBeUndefined();
  });

  test('keeps model provider row actions focused on edit and status changes', () => {
    expect(pageConfigs['models/providers'].actions.map((action) => (typeof action.label === 'function' ? action.label({ status: 'active' }) : action.label))).toEqual(['编辑', '停用']);
    expect(pageConfigs['models/providers'].actions[0].fields.map((field) => field.name)).toContain('provider_code');
    expect(pageConfigs['models/providers'].actions[1].body({ row: { status: 'active' } })).toEqual({ status: 'disabled' });
  });

  test('links models to provider-filtered model management through the side panel', () => {
    expect(pageConfigs.models.linkedPanel).toMatchObject({
      title: '模型供应商',
      field: 'provider_id',
      source: 'modelProviderItems'
    });
    expect(pageConfigs.models.filters.some((field) => field.name === 'provider_id')).toBe(false);
    expect(pageConfigs.models.create.fields[0]).toMatchObject({
      name: 'provider_id',
      optionSource: 'modelProviders'
    });
  });

  test('explains model form fields with grouped guidance', () => {
    const fields = pageConfigs.models.create.fields;

    expect(fields.find((field) => field.name === 'provider_id')).toMatchObject({
      group: '基础信息',
      groupHint: expect.stringContaining('真实模型编码'),
      hint: expect.stringContaining('基础 URL')
    });
    expect(fields.find((field) => field.name === 'billing_unit')).toMatchObject({
      group: '计费配置',
      groupHint: expect.stringContaining('整组留空'),
      options: expect.arrayContaining([{ label: '不配置计费', value: '' }])
    });
    expect(fields.find((field) => field.name === 'capability_tags')).toMatchObject({
      group: '运行绑定',
      singleLine: true,
      hint: expect.stringContaining('逗号分隔')
    });
    expect(fields.find((field) => field.name === 'route_config')).toMatchObject({
      group: '高级路由参数',
      hint: expect.stringContaining('合法 JSON')
    });
  });
});
