import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { TextMaterialForm } from './TextMaterialForm.jsx';

const SECOND_ASSET_ID = '019f0000-0000-7000-8000-000000000032';

describe('TextMaterialForm', () => {
  it('creates, selects and submits the strict analyze_materials intent while treating 202 as pending', async () => {
    const user = userEvent.setup();
    const create = vi.fn().mockResolvedValue({
      material: material(SECOND_ASSET_ID, '新素材', '2026-07-17T11:00:00Z'),
      replayed: false,
      requestID: WORKSPACE_IDS.request
    });
    const enqueueAnalyze = vi.fn().mockResolvedValue({
      requestID: WORKSPACE_IDS.request,
      inputID: WORKSPACE_IDS.input,
      status: 'pending',
      replayed: false
    });
    render(
      <TextMaterialForm
        projectID={WORKSPACE_IDS.project}
        sessionID={WORKSPACE_IDS.session}
        csrfToken="csrf-material"
        load={vi.fn().mockResolvedValue({ items: [material(WORKSPACE_IDS.asset, '已有素材')], requestID: WORKSPACE_IDS.request })}
        create={create}
        enqueueAnalyze={enqueueAnalyze}
        materialKeyFactory={() => SECOND_ASSET_ID}
        analysisKeyFactory={() => WORKSPACE_IDS.request}
      />
    );

    await user.type(await screen.findByLabelText('文本素材正文'), '新素材');
    await user.click(screen.getByRole('button', { name: '保存文本素材' }));
    await waitFor(() => expect(create).toHaveBeenCalledWith(expect.objectContaining({
      projectID: WORKSPACE_IDS.project,
      content: '新素材',
      idempotencyKey: SECOND_ASSET_ID,
      csrfToken: 'csrf-material'
    })));
    expect(await screen.findByText('文本素材已保存并可选择。')).toBeInTheDocument();

    const existingCheckbox = screen.getByRole('checkbox', { name: /已有素材/ });
    await user.click(existingCheckbox);
    await user.type(screen.getByLabelText('分析目标'), '识别主题和可复用元素');
    await user.click(screen.getByRole('checkbox', { name: '叙事' }));
    await user.click(screen.getByRole('button', { name: '提交素材分析' }));

    await waitFor(() => expect(enqueueAnalyze).toHaveBeenCalledWith(expect.objectContaining({
      sessionID: WORKSPACE_IDS.session,
      idempotencyKey: WORKSPACE_IDS.request,
      csrfToken: 'csrf-material',
      intent: {
        schema_version: 'analyze_materials.preview.intent.v1',
        asset_ids: [WORKSPACE_IDS.asset, SECOND_ASSET_ID].sort(),
        analysis_goal: '识别主题和可复用元素',
        focus_dimensions: ['content', 'narrative'],
        output_language: 'zh-CN',
        expected_assets: [WORKSPACE_IDS.asset, SECOND_ASSET_ID].sort().map((assetID) => ({ asset_id: assetID, asset_version: 1 }))
      }
    })));
    expect(screen.getByText(/素材分析请求已受理/)).toHaveTextContent('SSE');
    expect(screen.queryByText('素材分析已完成')).not.toBeInTheDocument();
  });

  it('requires a selected material, goal and focus before enqueue', async () => {
    const user = userEvent.setup();
    const enqueueAnalyze = vi.fn();
    render(
      <TextMaterialForm
        projectID={WORKSPACE_IDS.project}
        sessionID={WORKSPACE_IDS.session}
        csrfToken="csrf"
        load={vi.fn().mockResolvedValue({ items: [material(WORKSPACE_IDS.asset, '已有素材')], requestID: WORKSPACE_IDS.request })}
        enqueueAnalyze={enqueueAnalyze}
      />
    );
    await screen.findByText('已有素材');
    await user.type(screen.getByLabelText('分析目标'), '分析素材');
    await user.click(screen.getByRole('button', { name: '提交素材分析' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('请选择 1..8 条素材');
    expect(enqueueAnalyze).not.toHaveBeenCalled();
  });
});

function material(assetID, content, createdAt = '2026-07-17T10:00:00Z') {
  return {
    assetID,
    assetVersion: 1,
    mediaType: 'text',
    status: 'ready',
    content,
    createdAt,
    createdAtMs: Date.parse(createdAt)
  };
}
