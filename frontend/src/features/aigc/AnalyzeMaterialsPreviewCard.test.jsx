import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { analyzeMaterialsFailureCardFixture, analyzeMaterialsPreviewCardFixture } from '../../test/workspaceFixtures.js';
import { parseAnalyzeMaterialsPreviewCard } from './analyzeMaterialsPreviewContract.js';
import { AnalyzeMaterialsPreviewCard } from './AnalyzeMaterialsPreviewCard.jsx';

describe('AnalyzeMaterialsPreviewCard', () => {
  it('renders a non-authoritative read-only success projection', () => {
    render(<AnalyzeMaterialsPreviewCard preview={parseAnalyzeMaterialsPreviewCard(analyzeMaterialsPreviewCardFixture())} />);
    expect(screen.getByRole('heading', { name: '素材分析已完成' })).toBeInTheDocument();
    expect(screen.getByText(/开发预览 · 非权威结果/)).toBeInTheDocument();
    expect(screen.getByText('素材以城市中的红色自行车为主体。')).toBeInTheDocument();
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('renders runtime failure without leaking implementation details', () => {
    render(<AnalyzeMaterialsPreviewCard preview={parseAnalyzeMaterialsPreviewCard(analyzeMaterialsFailureCardFixture({ failure_kind: 'runtime' }))} />);
    expect(screen.getByRole('heading', { name: '素材分析运行未完成' })).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('素材证据尚不足以生成可信分析');
  });
});
