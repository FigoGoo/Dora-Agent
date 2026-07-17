import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { creationSpecPreviewCardFixture } from '../../test/workspaceFixtures.js';
import { parseCreationSpecPreviewCard } from './creationSpecPreviewContract.js';
import { CreationSpecCard } from './CreationSpecCard.jsx';

describe('CreationSpecCard', () => {
  it('shows all Draft sections and renders hostile text without creating executable markup', () => {
    const hostile = '<img src=x onerror=alert(1)>';
    const preview = parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({
      title: hostile,
      goal: '<script>alert(1)</script>',
      audience: '<svg onload=alert(1)>',
      phases: [{ key: 'phase_1', title: hostile, objective: '目标', output: '结果' }],
      constraints: [hostile],
      acceptance_criteria: ['不得执行任何 HTML']
    }));
    const { container } = render(<CreationSpecCard preview={preview} />);
    const card = screen.getByRole('article');
    expect(within(card).getByText('开发预览 · Draft · 未扣费/未激活')).toBeInTheDocument();
    expect(within(card).getByText('视频')).toBeInTheDocument();
    expect(within(card).getByRole('heading', { name: '执行阶段' })).toBeInTheDocument();
    expect(within(card).getByRole('heading', { name: '约束' })).toBeInTheDocument();
    expect(within(card).getByRole('heading', { name: '验收条件' })).toBeInTheDocument();
    expect(screen.getAllByText(hostile).length).toBeGreaterThan(1);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('script')).toBeNull();
    expect(container.querySelector('svg')).toBeNull();
  });
});
