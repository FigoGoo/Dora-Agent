import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import {
  inputFixture,
  projectBootstrapFixture,
  promptPreviewCardFixture,
  storyboardPreviewCardFixture,
  WORKSPACE_IDS,
  workspaceSnapshotV5Fixture
} from '../../test/workspaceFixtures.js';
import { parseToolCatalogResponse } from '../tools/toolCatalogContract.js';
import { toolCatalogFixture } from '../../test/toolCatalogFixtures.js';
import { ProjectWorkspacePage } from './ProjectWorkspacePage.jsx';

describe('ProjectWorkspacePage media runtime', () => {
  it('restores completed media and submits the two frozen local preview intents', async () => {
    const user = userEvent.setup();
    const enqueueGenerate = vi.fn().mockResolvedValue({
      requestID: WORKSPACE_IDS.request,
      inputID: WORKSPACE_IDS.mediaInput
    });
    const enqueueAssemble = vi.fn().mockResolvedValue({
      requestID: WORKSPACE_IDS.request,
      inputID: WORKSPACE_IDS.mediaInput
    });

    render(
      <ProjectWorkspacePage
        projectID={WORKSPACE_IDS.project}
        bootstrap={vi.fn().mockResolvedValue(projectBootstrapFixture())}
        loadSnapshot={vi.fn().mockResolvedValue(mediaSnapshot())}
        loadToolCatalog={vi.fn().mockResolvedValue(parseToolCatalogResponse(toolCatalogFixture()))}
        openStream={vi.fn(() => ({ close: vi.fn() }))}
        generateMediaPreviewEnabled
        assembleOutputPreviewEnabled
        enqueueGenerateMediaPreview={enqueueGenerate}
        enqueueAssembleOutputPreview={enqueueAssemble}
        mediaPreviewCsrfToken="csrf-media"
        mediaPreviewKeyFactory={() => '019f0000-0000-7000-8000-000000000099'}
      />
    );

    const region = await screen.findByRole('region', { name: '本地媒体开发预览结果' });
    expect(within(region).getByRole('img', { name: '本地确定性媒体开发预览' }))
      .toHaveAttribute('src', `/api/v1/projects/${WORKSPACE_IDS.project}/media-preview-assets/${WORKSPACE_IDS.mediaAsset}/content`);

    await user.click(within(region).getByRole('button', { name: '生成测试 PNG' }));
    expect(enqueueGenerate).toHaveBeenCalledWith(expect.objectContaining({
      sessionID: WORKSPACE_IDS.session,
      csrfToken: 'csrf-media',
      idempotencyKey: '019f0000-0000-7000-8000-000000000099',
      request: expect.objectContaining({
        schema_version: 'generate_media.preview.enqueue-request.v1',
        tool_intent: expect.objectContaining({ target_local_key: 'slot_1' })
      })
    }));

    await user.click(within(region).getByRole('button', { name: '装配测试 MP4' }));
    expect(enqueueAssemble).toHaveBeenCalledWith(expect.objectContaining({
      sessionID: WORKSPACE_IDS.session,
      csrfToken: 'csrf-media',
      request: expect.objectContaining({
        schema_version: 'assemble_output.preview.enqueue-request.v1',
        source_asset_ref: expect.objectContaining({ id: WORKSPACE_IDS.mediaAsset })
      })
    }));
  });
});

function mediaSnapshot() {
  return workspaceSnapshotV5Fixture({
    inputs: [
      inputFixture({
        id: WORKSPACE_IDS.storyboardInput,
        message_id: null,
        source_type: 'plan_storyboard_preview',
        status: 'resolved',
        enqueue_seq: 1
      }),
      inputFixture({
        id: WORKSPACE_IDS.promptInput,
        message_id: null,
        source_type: 'write_prompts_preview',
        status: 'resolved',
        enqueue_seq: 2
      }),
      inputFixture({
        id: WORKSPACE_IDS.mediaInput,
        message_id: null,
        source_type: 'generate_media_preview_request',
        status: 'resolved',
        enqueue_seq: 3
      }),
      inputFixture({
        id: WORKSPACE_IDS.mediaTerminalInput,
        message_id: null,
        source_type: 'media_job_preview_terminal',
        status: 'resolved',
        enqueue_seq: 4
      })
    ],
    plan_storyboard_preview: storyboardPreviewCardFixture(),
    write_prompts_preview: promptPreviewCardFixture(),
    media_previews: [acceptedPNG(), completedPNG()],
    event_high_watermark: 8
  });
}

function acceptedPNG() {
  return {
    schema_version: 'media_preview.card.v1',
    input_id: WORKSPACE_IDS.mediaInput,
    turn_id: WORKSPACE_IDS.mediaTurn,
    run_id: WORKSPACE_IDS.mediaRun,
    tool_call_id: WORKSPACE_IDS.mediaToolCall,
    tool_key: 'generate_media',
    status: 'accepted',
    result_code: 'MEDIA_PREVIEW_ACCEPTED',
    updated_at: '2026-07-17T12:00:00Z',
    operation_id: WORKSPACE_IDS.mediaOperation,
    batch_id: WORKSPACE_IDS.mediaBatch,
    asset_ref: {
      id: WORKSPACE_IDS.mediaAsset,
      version: 1,
      status: 'reserved',
      media_kind: 'image',
      mime_type: 'image/png'
    }
  };
}

function completedPNG() {
  return {
    ...acceptedPNG(),
    status: 'completed',
    result_code: 'MEDIA_PREVIEW_COMPLETED',
    job_id: WORKSPACE_IDS.mediaJob,
    asset_ref: {
      id: WORKSPACE_IDS.mediaAsset,
      version: 1,
      status: 'ready',
      media_kind: 'image',
      mime_type: 'image/png',
      content_digest: 'a'.repeat(64),
      size_bytes: 8192
    },
    content_url: `/api/v1/projects/${WORKSPACE_IDS.project}/media-preview-assets/${WORKSPACE_IDS.mediaAsset}/content`
  };
}
