import { describe, expect, it } from 'vitest';
import {
  creationSpecPreviewCardFixture,
  WORKSPACE_IDS
} from '../../test/workspaceFixtures.js';
import {
  CreationSpecPreviewContractError,
  normalizeCreationSpecPreviewIntent,
  parseCreationSpecPreviewCard,
  parseCreationSpecPreviewEnqueueResponse,
  parseCreationSpecPreviewFailure,
  parseCreationSpecPreviewIntent,
  parseCreationSpecPreviewProjection
} from './creationSpecPreviewContract.js';

describe('Creation Spec Preview strict contract', () => {
  it('normalizes the frozen intent and omits an empty optional audience', () => {
    expect(normalizeCreationSpecPreviewIntent({
      goal: '  制作新品短片  ',
      deliverableType: 'video',
      audience: ' ',
      locale: 'zh-CN',
      constraints: []
    })).toEqual({
      schema_version: 'plan_creation_spec.preview.intent.v1',
      goal: '制作新品短片',
      deliverable_type: 'video',
      locale: 'zh-CN',
      constraints: []
    });
  });

  it('rejects unknown intent fields, explicit null, duplicate constraints and invalid enums', () => {
    const base = {
      schema_version: 'plan_creation_spec.preview.intent.v1',
      goal: '制作新品短片',
      deliverable_type: 'video',
      locale: 'zh-CN',
      constraints: []
    };
    expect(() => parseCreationSpecPreviewIntent({ ...base, user_id: WORKSPACE_IDS.user })).toThrow('字段集合');
    expect(() => parseCreationSpecPreviewIntent({ ...base, audience: null })).toThrow('字符串');
    expect(() => parseCreationSpecPreviewIntent({ ...base, constraints: ['高清', '高清'] })).toThrow('重复');
    expect(() => parseCreationSpecPreviewIntent({ ...base, deliverable_type: 'document' })).toThrow('未知枚举');
  });

  it('preserves a present-empty audience on the strict wire DTO', () => {
    expect(parseCreationSpecPreviewIntent({
      schema_version: 'plan_creation_spec.preview.intent.v1',
      goal: '制作新品短片',
      deliverable_type: 'video',
      audience: '',
      locale: 'zh-CN',
      constraints: []
    })).toHaveProperty('audience', '');
  });

  it('accepts only an exact 202 enqueue envelope bound to the requested Session', () => {
    const parsed = parseCreationSpecPreviewEnqueueResponse({
      schema_version: 'plan_creation_spec.preview.enqueue.v1',
      request_id: WORKSPACE_IDS.previewRequest,
      session_id: WORKSPACE_IDS.session,
      input_id: WORKSPACE_IDS.previewInput,
      status: 'pending'
    }, WORKSPACE_IDS.session);
    expect(parsed).toMatchObject({ inputID: WORKSPACE_IDS.previewInput, status: 'pending' });
    expect(() => parseCreationSpecPreviewEnqueueResponse({
      schema_version: 'plan_creation_spec.preview.enqueue.v1',
      request_id: WORKSPACE_IDS.previewRequest,
      session_id: WORKSPACE_IDS.session,
      input_id: WORKSPACE_IDS.previewInput,
      status: 'completed'
    }, WORKSPACE_IDS.session)).toThrow('status');
  });

  it('strictly maps a complete card and fails closed on fields, digest, phase, or project drift', () => {
    const parsed = parseCreationSpecPreviewCard(creationSpecPreviewCardFixture(), {
      expectedProjectID: WORKSPACE_IDS.project
    });
    expect(parsed).toMatchObject({
      kind: 'card',
      creationSpecID: WORKSPACE_IDS.creationSpec,
      deliverableType: 'video',
      version: 1
    });
    expect(() => parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({ html: '<b>unsafe</b>' }))).toThrow('字段集合');
    expect(() => parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({ content_digest: 'ABC' }))).toThrow('SHA-256');
    expect(() => parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({
      phases: [{ key: 'intro', title: '策划', objective: '冻结结构', output: '大纲' }]
    }))).toThrow('冻结格式');
    expect(() => parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({
      project_id: '019f0000-0000-7000-8000-000000000099'
    }), { expectedProjectID: WORKSPACE_IDS.project })).toThrow('project_id 不一致');
  });

  it('degrades only an unknown card schema without exposing untrusted fields', () => {
    const projection = parseCreationSpecPreviewProjection(creationSpecPreviewCardFixture({
      schema_version: 'creation_spec.preview.card.v2',
      title: '<img src=x onerror=alert(1)>'
    }));
    expect(projection).toEqual({ kind: 'unsupported', schemaVersion: 'creation_spec.preview.card.v2' });
    expect(projection).not.toHaveProperty('title');
    expect(() => parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({
      schema_version: 'creation_spec.preview.card.v2'
    }))).toThrow(CreationSpecPreviewContractError);
  });

  it('strictly maps a safe persistent failure', () => {
    expect(parseCreationSpecPreviewFailure({
      input_id: WORKSPACE_IDS.previewInput,
      result_code: 'CREATION_SPEC_PREVIEW_INVALID',
      summary: '目标信息不足。',
      retryable: false
    })).toMatchObject({ inputID: WORKSPACE_IDS.previewInput, retryable: false });
    expect(() => parseCreationSpecPreviewFailure({
      input_id: WORKSPACE_IDS.previewInput,
      result_code: 'bad code',
      summary: '目标信息不足。',
      retryable: false
    })).toThrow('错误码');
  });
});
