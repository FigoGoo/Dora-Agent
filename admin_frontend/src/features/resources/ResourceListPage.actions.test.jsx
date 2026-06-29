import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, test, vi } from 'vitest';

vi.mock('../../lib/api/admin.js', () => ({
  adminApi: {
    list: vi.fn((path) => {
      if (path === '/api/admin/models/providers') {
        return Promise.resolve({
          items: [{ provider_id: 'mp_deepseek', provider_name: 'DeepSeek 模型渠道', provider_code: 'deepseek' }]
        });
      }
      return Promise.resolve({
        items: [
          {
            model_id: 'mdl_deepseek_v4_fast',
            display_name: 'DeepSeek V4 Fast',
            model_code: 'deepseek-v4-fast',
            provider_id: 'mp_deepseek',
            provider_name: 'DeepSeek 模型渠道',
            resource_type: 'text',
            status: 'active',
            pricing_snapshot_id: 'price_deepseek_v4_fast',
            is_default: false,
            default_for_resource: false
          }
        ],
        page_size: 10
      });
    }),
    post: vi.fn(() => Promise.resolve({}))
  }
}));

import { adminApi } from '../../lib/api/admin.js';
import { ToastProvider } from '../../components/admin/Toast.jsx';
import { ResourceListPage } from './ResourceListPage.jsx';
import { pageConfigs } from './pageConfigs.jsx';

function renderResourcePage(config) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false }
    }
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <MemoryRouter initialEntries={['/admin/models']}>
          <ResourceListPage config={config} />
        </MemoryRouter>
      </ToastProvider>
    </QueryClientProvider>
  );
}

describe('ResourceListPage actions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  test('sets a model as default without an audit reason header option', async () => {
    renderResourcePage(pageConfigs.models);

    await screen.findByText('DeepSeek V4 Fast');
    await userEvent.click(screen.getByRole('button', { name: '设为默认' }));
    await userEvent.click(screen.getByRole('button', { name: '确认执行' }));

    await waitFor(() => expect(adminApi.post).toHaveBeenCalledWith(
      '/api/admin/models/default',
      {
        model_id: 'mdl_deepseek_v4_fast',
        resource_type: 'text',
        pricing_snapshot_id: 'price_deepseek_v4_fast'
      },
      { idempotencyKey: undefined }
    ));
  });
});
