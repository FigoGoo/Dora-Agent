import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { toolCatalogFixture } from '../../test/toolCatalogFixtures.js';
import { WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { parseToolCatalogResponse } from './toolCatalogContract.js';
import { ToolCatalogPanel } from './ToolCatalogPanel.jsx';

describe('ToolCatalogPanel', () => {
  it('renders the six server-projected definitions in order, all disabled and pending design review', async () => {
    const catalog = parseToolCatalogResponse(toolCatalogFixture());
    const loadCatalog = vi.fn().mockResolvedValue(catalog);
    const storage = { clear: vi.fn(), getItem: vi.fn(), removeItem: vi.fn(), setItem: vi.fn() };
    vi.stubGlobal('localStorage', storage);

    render(<ToolCatalogPanel sessionID={WORKSPACE_IDS.session} loadCatalog={loadCatalog} />);

    const list = await screen.findByRole('list', { name: '不可用工具定义' });
    const items = within(list).getAllByRole('listitem');
    expect(items).toHaveLength(6);
    expect(items.map((node) => node.querySelector('strong')?.textContent)).toEqual([
      '流程规划', '素材分析', '故事板设计', '媒体生成', '提示词写法', '视频剪辑'
    ]);
    items.forEach((node, index) => {
      expect(node).toHaveAttribute('aria-disabled', 'true');
      expect(node).toHaveAttribute('data-tool-order', String(index + 1));
      expect(node).toHaveAttribute('data-tool-availability', 'unavailable');
      expect(within(node).getByText('设计评审中')).toBeInTheDocument();
      expect(within(node).getByText('不可用')).toBeInTheDocument();
    });
    expect(within(screen.getByRole('region', { name: '工具目录' })).queryByRole('button')).not.toBeInTheDocument();
    expect(storage.getItem).not.toHaveBeenCalled();
    expect(storage.setItem).not.toHaveBeenCalled();
    expect(loadCatalog).toHaveBeenCalledWith(WORKSPACE_IDS.session, {
      signal: expect.any(AbortSignal)
    });
  });

  it('shows a stable retryable error without local fallback and retries explicitly', async () => {
    const user = userEvent.setup();
    const loadCatalog = vi.fn()
      .mockRejectedValueOnce(Object.assign(new Error('upstream detail'), {
        status: 503,
        code: 'DEPENDENCY_UNAVAILABLE',
        requestID: WORKSPACE_IDS.request
      }))
      .mockResolvedValueOnce(parseToolCatalogResponse(toolCatalogFixture()));

    render(<ToolCatalogPanel sessionID={WORKSPACE_IDS.session} loadCatalog={loadCatalog} />);

    expect(await screen.findByRole('alert')).toHaveTextContent('工具目录暂时不可用，请重试。');
    expect(screen.getByText(`请求 ID：${WORKSPACE_IDS.request}`)).toBeInTheDocument();
    expect(screen.queryByRole('list', { name: '不可用工具定义' })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '重试加载工具目录' }));
    expect(await screen.findByRole('list', { name: '不可用工具定义' })).toBeInTheDocument();
    expect(loadCatalog).toHaveBeenCalledTimes(2);
  });

  it('aborts the old request and ignores its late result when the ready Session changes', async () => {
    const requests = [];
    const loadCatalog = vi.fn((sessionID, { signal }) => new Promise((resolve) => {
      requests.push({ sessionID, signal, resolve });
    }));
    const secondSessionID = '019f0000-0000-7000-8000-000000000099';
    const { rerender } = render(
      <ToolCatalogPanel sessionID={WORKSPACE_IDS.session} loadCatalog={loadCatalog} />
    );
    await waitFor(() => expect(requests).toHaveLength(1));

    rerender(<ToolCatalogPanel sessionID={secondSessionID} loadCatalog={loadCatalog} />);
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(requests[0].signal.aborted).toBe(true);

    requests[1].resolve(parseToolCatalogResponse(toolCatalogFixture()));
    expect(await screen.findByText('流程规划')).toBeInTheDocument();
    requests[0].resolve(parseToolCatalogResponse(toolCatalogFixture()));
    await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(6));
    expect(screen.getByText('流程规划')).toBeInTheDocument();
  });
});
