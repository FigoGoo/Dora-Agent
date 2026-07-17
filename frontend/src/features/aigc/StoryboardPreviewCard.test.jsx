import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { parseStoryboardPreviewCard } from './planStoryboardPreviewContract.js';
import { StoryboardPreviewCard } from './StoryboardPreviewCard.jsx';

const IDS = Object.freeze({
  project: '019f0000-0000-7000-8000-000000000001',
  input: '019f0000-0000-7000-8000-000000000004',
  turn: '019f0000-0000-7000-8000-000000000005',
  run: '019f0000-0000-7000-8000-000000000006',
  toolCall: '019f0000-0000-7000-8000-000000000007',
  creationSpec: '019f0000-0000-7000-8000-000000000008',
  storyboardPreview: '019f0000-0000-7000-8000-000000000009'
});

describe('StoryboardPreviewCard', () => {
  it('renders the isolated JSON Draft with local relationships and never executes hostile text', () => {
    const hostile = '<img src=x onerror=alert(1)>';
    const preview = parseStoryboardPreviewCard(completedCard({
      title: hostile,
      summary: '<script>alert(1)</script>',
      sections: [
        { key: 'section_1', title: hostile, objective: '<svg onload=alert(1)>' },
        { key: 'section_2', title: '演示与收尾', objective: '展示价值并给出行动号召' }
      ],
      slots: [
        { key: 'slot_1', element_key: 'element_1', slot_type: 'image', purpose: hostile, required: true },
        { key: 'slot_2', element_key: 'element_2', slot_type: 'video', purpose: '功能演示画面', required: false }
      ]
    }));
    const { container } = render(<StoryboardPreviewCard preview={preview} />);
    const card = screen.getByRole('article');

    expect(within(card).getByText('开发预览 · 隔离 JSON Draft · 未激活/未扣费')).toBeInTheDocument();
    expect(within(card).getByRole('heading', { name: '故事板章节' })).toBeInTheDocument();
    expect(within(card).getByRole('heading', { name: '规划元素' })).toBeInTheDocument();
    expect(within(card).getByText('依赖：element_1')).toBeInTheDocument();
    expect(within(card).getByText('依赖：element_2')).toBeInTheDocument();
    expect(within(card).getByText('：功能演示画面（可选）')).toBeInTheDocument();
    expect(screen.getAllByText(hostile).length).toBeGreaterThan(1);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('script')).toBeNull();
    expect(container.querySelector('svg')).toBeNull();
  });

  it('exposes only the root Storyboard Preview resource ID and never production Storyboard/Element IDs', () => {
    const preview = parseStoryboardPreviewCard(completedCard());
    const { container } = render(<StoryboardPreviewCard preview={preview} />);
    const card = screen.getByRole('article');

    expect(card).toHaveAttribute('data-storyboard-preview-id', IDS.storyboardPreview);
    expect(card).not.toHaveAttribute('data-storyboard-id');
    expect(card).not.toHaveAttribute('data-turn-id');
    expect(card).not.toHaveAttribute('data-tool-call-id');
    expect(container.querySelector('[data-element-id]')).toBeNull();
    expect(container.querySelector('[data-section-id]')).toBeNull();
    expect(container.querySelector('[data-slot-id]')).toBeNull();
    expect(container.innerHTML).not.toContain(IDS.project);
    expect(container.innerHTML).not.toContain(IDS.creationSpec);
    expect(container.innerHTML).not.toContain(IDS.input);
    expect(container.innerHTML).not.toContain(IDS.turn);
    expect(container.innerHTML).not.toContain(IDS.run);
    expect(container.innerHTML).not.toContain(IDS.toolCall);
    expect(within(card).getByText('Draft v1')).toBeInTheDocument();
  });

  it('renders a failed Card without completed-only resource or provenance fields', () => {
    const preview = parseStoryboardPreviewCard(failedCard());
    const { container } = render(<StoryboardPreviewCard preview={preview} />);
    const card = screen.getByRole('article');

    expect(within(card).getByRole('heading', { name: '故事板规划未完成' })).toBeInTheDocument();
    expect(within(card).getByRole('alert')).toHaveTextContent('故事板运行时未完成');
    expect(within(card).getByText('失败类型：运行时失败')).toBeInTheDocument();
    expect(within(card).getByText('可以稍后重新显式提交。')).toBeInTheDocument();
    expect(card).not.toHaveAttribute('data-storyboard-preview-id');
    expect(container.innerHTML).not.toContain(IDS.project);
    expect(container.innerHTML).not.toContain(IDS.creationSpec);
    expect(container.innerHTML).not.toContain(IDS.input);
    expect(container.innerHTML).not.toContain(IDS.turn);
    expect(container.innerHTML).not.toContain(IDS.run);
    expect(container.innerHTML).not.toContain(IDS.toolCall);
  });
});

function completedCard(overrides = {}) {
  return {
    schema_version: 'storyboard.preview.card.v1',
    input_id: IDS.input,
    turn_id: IDS.turn,
    run_id: IDS.run,
    tool_call_id: IDS.toolCall,
    status: 'completed',
    result_code: 'STORYBOARD_PREVIEW_DRAFT_CREATED',
    updated_at: '2026-07-17T10:00:00Z',
    storyboard_preview_id: IDS.storyboardPreview,
    project_id: IDS.project,
    creation_spec_ref: { id: IDS.creationSpec, version: 1, content_digest: 'a'.repeat(64) },
    version: 1,
    content_digest: 'b'.repeat(64),
    title: '新品短片故事板',
    summary: '通过三个连续元素完成开场、演示与收尾。',
    sections: [
      { key: 'section_1', title: '开场', objective: '建立产品认知' },
      { key: 'section_2', title: '演示与收尾', objective: '展示价值并给出行动号召' }
    ],
    elements: [
      {
        key: 'element_1', section_key: 'section_1', order: 1, element_type: 'scene', title: '产品登场',
        narrative_purpose: '建立视觉焦点', duration_seconds: 5, source_phase_key: 'phase_1', dependency_keys: []
      },
      {
        key: 'element_2', section_key: 'section_2', order: 2, element_type: 'shot', title: '功能演示',
        narrative_purpose: '展示核心卖点', duration_seconds: 15, source_phase_key: 'phase_2', dependency_keys: ['element_1']
      },
      {
        key: 'element_3', section_key: 'section_2', order: 3, element_type: 'caption', title: '行动号召',
        narrative_purpose: '推动转化', duration_seconds: 10, source_phase_key: 'phase_3', dependency_keys: ['element_2']
      }
    ],
    slots: [
      { key: 'slot_1', element_key: 'element_1', slot_type: 'image', purpose: '产品主视觉', required: true },
      { key: 'slot_2', element_key: 'element_2', slot_type: 'video', purpose: '功能演示画面', required: true }
    ],
    ...overrides
  };
}

function failedCard() {
  return {
    schema_version: 'storyboard.preview.card.v1',
    input_id: IDS.input,
    turn_id: IDS.turn,
    run_id: IDS.run,
    tool_call_id: IDS.toolCall,
    status: 'failed',
    result_code: 'PLAN_STORYBOARD_RUNTIME_FAILED',
    updated_at: '2026-07-17T10:00:00Z',
    failure_kind: 'runtime',
    summary: '故事板运行时未完成。',
    retryable: true
  };
}
