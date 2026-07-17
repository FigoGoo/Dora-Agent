import { describe, expect, it } from 'vitest';
import {
  analyzeMaterialsFailureCardFixture,
  analyzeMaterialsPreviewCardFixture,
  WORKSPACE_IDS
} from '../../test/workspaceFixtures.js';
import {
  parseAnalyzeMaterialsEnqueueResponse,
  parseAnalyzeMaterialsIntent,
  parseAnalyzeMaterialsPreviewCard
} from './analyzeMaterialsPreviewContract.js';

describe('Analyze Materials Preview contract', () => {
  it.each(['completed', 'partial'])('strictly parses safe %s projections', (status) => {
    const fixture = analyzeMaterialsPreviewCardFixture({
      status,
      result_code: status === 'completed' ? 'MATERIAL_ANALYSIS_PREVIEW_COMPLETED' : 'MATERIAL_ANALYSIS_PREVIEW_PARTIAL',
      coverage: { ...analyzeMaterialsPreviewCardFixture().coverage, status }
    });
    const parsed = parseAnalyzeMaterialsPreviewCard(fixture);
    expect(parsed).toMatchObject({ kind: 'analyze_materials_preview', status, toolCallID: WORKSPACE_IDS.toolCall });
    expect(parsed.analysis.assetSummaries[0].summary).toContain('红色自行车');
  });

  it.each([['tool', false], ['runtime', true]])('strictly parses %s failure without success fields', (failureKind, retryable) => {
    const parsed = parseAnalyzeMaterialsPreviewCard(analyzeMaterialsFailureCardFixture({ failure_kind: failureKind, retryable }));
    expect(parsed).toMatchObject({ status: 'failed', failureKind, retryable, analysis: null });
  });

  it('fails closed on unknown root/nested fields, schema, status, UUID and failure unions', () => {
    expect(() => parseAnalyzeMaterialsPreviewCard({ ...analyzeMaterialsPreviewCardFixture(), provider_payload: 'secret' })).toThrow('字段集合');
    expect(() => parseAnalyzeMaterialsPreviewCard({ ...analyzeMaterialsPreviewCardFixture(), schema_version: 'future.v2' })).toThrow('schema_version');
    expect(() => parseAnalyzeMaterialsPreviewCard({ ...analyzeMaterialsPreviewCardFixture(), tool_call_id: 'v4' })).toThrow('UUIDv7');
    const nested = analyzeMaterialsPreviewCardFixture();
    nested.analysis.asset_summaries[0].debug = 'secret';
    expect(() => parseAnalyzeMaterialsPreviewCard(nested)).toThrow('字段集合');
    expect(() => parseAnalyzeMaterialsPreviewCard(analyzeMaterialsFailureCardFixture({ failure_kind: 'provider' }))).toThrow('failure_kind');
    expect(() => parseAnalyzeMaterialsPreviewCard(analyzeMaterialsFailureCardFixture({ analysis: {} }))).toThrow('字段集合');
  });

  it('requires typed Intent expected_assets exact-set and strict 202 response', () => {
    const intent = parseAnalyzeMaterialsIntent({
      schema_version: 'analyze_materials.preview.intent.v1', asset_ids: [WORKSPACE_IDS.asset],
      analysis_goal: '分析素材', focus_dimensions: ['visual'], output_language: 'zh-CN',
      expected_assets: [{ asset_id: WORKSPACE_IDS.asset, asset_version: 1 }]
    });
    expect(intent.expected_assets).toHaveLength(1);
    expect(() => parseAnalyzeMaterialsIntent({ ...intent, expected_assets: [] })).toThrow('数量');
    expect(parseAnalyzeMaterialsEnqueueResponse({
      schema_version: 'analyze_materials.preview.enqueue.v1', request_id: WORKSPACE_IDS.request,
      session_id: WORKSPACE_IDS.session, input_id: WORKSPACE_IDS.input, turn_id: WORKSPACE_IDS.turn,
      run_id: WORKSPACE_IDS.run, tool_call_id: WORKSPACE_IDS.toolCall, status: 'pending', replayed: false
    }, WORKSPACE_IDS.session)).toMatchObject({ status: 'pending', inputID: WORKSPACE_IDS.input });
  });
});
