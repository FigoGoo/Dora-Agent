import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { parsePromptPreviewCard } from '../aigc/writePromptsPreviewContract.js';
import { promptPreviewCardFixture, WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { parseMediaPreviewCard } from './mediaPreviewContract.js';
import { MediaPreviewControls } from './MediaPreviewControls.jsx';

const PNG_ASSET_ID = '019f0000-0000-7000-8000-000000000043';

describe('MediaPreviewControls', () => {
  it('shows only image Prompt targets and submits frozen Generate semantics', async () => {
    const user = userEvent.setup();
    const enqueueGenerate = vi.fn().mockResolvedValue(accepted('generate_media'));
    render(<MediaPreviewControls
      sessionID={WORKSPACE_IDS.session}
      csrfToken="csrf-media"
      promptPreview={parsePromptPreviewCard(promptPreviewCardFixture())}
      enqueueGenerate={enqueueGenerate}
      enqueueAssemble={vi.fn()}
      keyFactory={() => WORKSPACE_IDS.request}
    />);
    expect(screen.getByRole('option', { name: /slot_1/ })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /slot_2/ })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '生成测试 PNG' }));
    expect(await screen.findByText(/PNG 请求已受理/)).toBeInTheDocument();
    expect(enqueueGenerate).toHaveBeenCalledWith(expect.objectContaining({
      sessionID: WORKSPACE_IDS.session,
      idempotencyKey: WORKSPACE_IDS.request,
      csrfToken: 'csrf-media',
      request: expect.objectContaining({
        tool_intent: expect.objectContaining({ target_local_key: 'slot_1', output_profile: 'png_640x360.v1' })
      }),
      signal: expect.any(AbortSignal)
    }));
  });

  it('enables Assemble only for a completed ready PNG and reuses the same semantic key', async () => {
    const user = userEvent.setup();
    const keyFactory = vi.fn().mockReturnValue(WORKSPACE_IDS.request);
    const enqueueAssemble = vi.fn().mockResolvedValue(accepted('assemble_output'));
    render(<MediaPreviewControls
      sessionID={WORKSPACE_IDS.session}
      csrfToken="csrf-media"
      promptPreview={null}
      mediaCards={[parseMediaPreviewCard(completedPNG())]}
      enqueueGenerate={vi.fn()}
      enqueueAssemble={enqueueAssemble}
      keyFactory={keyFactory}
    />);
    expect(screen.getByRole('option', { name: PNG_ASSET_ID })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '装配测试 MP4' }));
    await screen.findByText(/MP4 请求已受理/);
    await user.click(screen.getByRole('button', { name: '装配测试 MP4' }));
    await waitFor(() => expect(enqueueAssemble).toHaveBeenCalledTimes(2));
    expect(keyFactory).toHaveBeenCalledTimes(1);
    expect(enqueueAssemble.mock.calls[0][0]).toMatchObject({
      idempotencyKey: WORKSPACE_IDS.request,
      request: { tool_intent: { source_asset_id: PNG_ASSET_ID, output_profile: 'mp4_h264_640x360_2s.v1' } }
    });
  });
});

function accepted(toolKey) {
  return { requestID: WORKSPACE_IDS.request, inputID: WORKSPACE_IDS.promptInput, toolKey, status: 'pending', replayed: false };
}

function completedPNG() {
  return {
    schema_version: 'media_preview.card.v1', input_id: WORKSPACE_IDS.promptInput,
    turn_id: WORKSPACE_IDS.promptTurn, run_id: WORKSPACE_IDS.promptRun,
    tool_call_id: WORKSPACE_IDS.promptToolCall, tool_key: 'generate_media', status: 'completed',
    result_code: 'MEDIA_PREVIEW_COMPLETED', updated_at: '2026-07-17T08:00:00Z',
    operation_id: '019f0000-0000-7000-8000-000000000040',
    batch_id: '019f0000-0000-7000-8000-000000000041',
    job_id: '019f0000-0000-7000-8000-000000000042',
    asset_ref: {
      id: PNG_ASSET_ID, version: 1, status: 'ready', media_kind: 'image', mime_type: 'image/png',
      content_digest: 'a'.repeat(64), size_bytes: 8192
    },
    content_url: `/api/v1/projects/${WORKSPACE_IDS.project}/media-preview-assets/${PNG_ASSET_ID}/content`
  };
}
