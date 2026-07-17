import { describe, expect, it } from 'vitest';
import { parsePromptPreviewCard } from '../aigc/writePromptsPreviewContract.js';
import { promptPreviewCardFixture, WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import {
  normalizeAssembleOutputPreviewRequest,
  normalizeGenerateMediaPreviewRequest,
  parseAssembleOutputPreviewRequest,
  parseGenerateMediaPreviewRequest,
  parseMediaPreviewCard,
  parseMediaPreviewEnqueue,
  parseMediaPreviewProjection
} from './mediaPreviewContract.js';

const IDS = Object.freeze({
  operation: '019f0000-0000-7000-8000-000000000040',
  batch: '019f0000-0000-7000-8000-000000000041',
  job: '019f0000-0000-7000-8000-000000000042',
  png: '019f0000-0000-7000-8000-000000000043',
  mp4: '019f0000-0000-7000-8000-000000000044'
});

describe('Media Preview contract', () => {
  it('binds Generate Media exclusively to the current Prompt Draft image target', () => {
    const promptPreview = parsePromptPreviewCard(promptPreviewCardFixture());
    const request = normalizeGenerateMediaPreviewRequest({ promptPreview, targetLocalKey: 'slot_1' });
    expect(parseGenerateMediaPreviewRequest(request)).toEqual({
      schema_version: 'generate_media.preview.enqueue-request.v1',
      prompt_preview_ref: { id: WORKSPACE_IDS.promptPreview, version: 1, content_digest: 'e'.repeat(64) },
      tool_intent: {
        schema_version: 'generate_media.intent.v3preview1',
        prompt_preview_id: WORKSPACE_IDS.promptPreview,
        expected_prompt_version: 1,
        expected_prompt_content_digest: 'e'.repeat(64),
        target_local_key: 'slot_1',
        output_profile: 'png_640x360.v1'
      }
    });
    expect(() => normalizeGenerateMediaPreviewRequest({ promptPreview, targetLocalKey: 'slot_2' })).toThrow('图片目标');
    expect(() => parseGenerateMediaPreviewRequest({ ...request, prompt: 'secret' })).toThrow('字段集合');
    expect(() => parseGenerateMediaPreviewRequest({
      ...request,
      tool_intent: { ...request.tool_intent, expected_prompt_content_digest: 'f'.repeat(64) }
    })).toThrow('不一致');
  });

  it('binds Assemble Output exclusively to a completed ready PNG', () => {
    const png = parseMediaPreviewCard(completedPNG());
    const request = normalizeAssembleOutputPreviewRequest({ mediaCard: png });
    expect(parseAssembleOutputPreviewRequest(request)).toMatchObject({
      source_asset_ref: { id: IDS.png, version: 1, content_digest: 'a'.repeat(64) },
      tool_intent: {
        source_asset_id: IDS.png,
        output_profile: 'mp4_h264_640x360_2s.v1'
      }
    });
    expect(() => normalizeAssembleOutputPreviewRequest({ mediaCard: parseMediaPreviewCard(acceptedPNG()) })).toThrow('ready PNG');
    expect(() => parseAssembleOutputPreviewRequest({
      ...request,
      tool_intent: { ...request.tool_intent, codec: 'copy' }
    })).toThrow('字段集合');
  });

  it('strictly parses enqueue, accepted, completed and both failed card unions', () => {
    expect(parseMediaPreviewEnqueue(enqueue(), WORKSPACE_IDS.session, 'generate_media')).toMatchObject({
      toolKey: 'generate_media', status: 'pending', replayed: false
    });
    expect(parseMediaPreviewCard(acceptedPNG())).toMatchObject({
      status: 'accepted', operationID: IDS.operation, assetRef: { id: IDS.png, status: 'reserved' }
    });
    expect(parseMediaPreviewCard(completedPNG())).toMatchObject({
      status: 'completed', jobID: IDS.job,
      contentURL: `/api/v1/projects/${WORKSPACE_IDS.project}/media-preview-assets/${IDS.png}/content`,
      assetRef: { contentDigest: 'a'.repeat(64), sizeBytes: 8192 }
    });
    expect(parseMediaPreviewCard(earlyFailed())).toMatchObject({ status: 'failed', operationID: '', errorCode: 'INVALID_ARGUMENT' });
    expect(parseMediaPreviewCard(terminalFailed())).toMatchObject({ status: 'failed', jobID: IDS.job, errorCode: 'ARTIFACT_INVALID' });
  });

  it('rejects arbitrary content URLs, wrong media union, unknown fields and duplicate projections', () => {
    expect(() => parseMediaPreviewCard({ ...completedPNG(), content_url: 'https://evil.example/file' })).toThrow('受保护媒体端点');
    expect(() => parseMediaPreviewCard({
      ...completedPNG(), asset_ref: { ...completedPNG().asset_ref, mime_type: 'video/mp4' }
    })).toThrow('mime_type');
    expect(() => parseMediaPreviewCard({ ...acceptedPNG(), object_key: '../secret' })).toThrow('字段集合');
    expect(() => parseMediaPreviewProjection([acceptedPNG(), acceptedPNG()])).toThrow('重复');
    expect(() => parseMediaPreviewEnqueue({ ...enqueue(), session_id: IDS.operation }, WORKSPACE_IDS.session, 'generate_media')).toThrow('session_id');
  });
});

function enqueue() {
  return {
    schema_version: 'media_preview.enqueue.v1', request_id: WORKSPACE_IDS.request,
    session_id: WORKSPACE_IDS.session, input_id: WORKSPACE_IDS.promptInput,
    turn_id: WORKSPACE_IDS.promptTurn, run_id: WORKSPACE_IDS.promptRun,
    tool_call_id: WORKSPACE_IDS.promptToolCall, tool_key: 'generate_media', status: 'pending', replayed: false
  };
}

function acceptedPNG() {
  return {
    schema_version: 'media_preview.card.v1', input_id: WORKSPACE_IDS.promptInput,
    turn_id: WORKSPACE_IDS.promptTurn, run_id: WORKSPACE_IDS.promptRun,
    tool_call_id: WORKSPACE_IDS.promptToolCall, tool_key: 'generate_media', status: 'accepted',
    result_code: 'MEDIA_PREVIEW_ACCEPTED', updated_at: '2026-07-17T08:00:00Z',
    operation_id: IDS.operation, batch_id: IDS.batch,
    asset_ref: { id: IDS.png, version: 1, status: 'reserved', media_kind: 'image', mime_type: 'image/png' }
  };
}

function completedPNG() {
  return {
    ...acceptedPNG(), status: 'completed', result_code: 'MEDIA_PREVIEW_COMPLETED', job_id: IDS.job,
    asset_ref: {
      id: IDS.png, version: 1, status: 'ready', media_kind: 'image', mime_type: 'image/png',
      content_digest: 'a'.repeat(64), size_bytes: 8192
    },
    content_url: `/api/v1/projects/${WORKSPACE_IDS.project}/media-preview-assets/${IDS.png}/content`
  };
}

function earlyFailed() {
  return {
    schema_version: 'media_preview.card.v1', input_id: WORKSPACE_IDS.promptInput,
    turn_id: WORKSPACE_IDS.promptTurn, run_id: WORKSPACE_IDS.promptRun,
    tool_call_id: WORKSPACE_IDS.promptToolCall, tool_key: 'generate_media', status: 'failed',
    result_code: 'MEDIA_PREVIEW_FAILED', updated_at: '2026-07-17T08:00:00Z', error_code: 'INVALID_ARGUMENT'
  };
}

function terminalFailed() {
  return {
    ...earlyFailed(), operation_id: IDS.operation, batch_id: IDS.batch, job_id: IDS.job,
    asset_ref: { id: IDS.png, version: 1, status: 'failed', media_kind: 'image', mime_type: 'image/png' },
    error_code: 'ARTIFACT_INVALID'
  };
}
