import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, test, vi } from 'vitest';

vi.mock('../../services/adminApi.js', () => ({
  adminApi: {
    list: vi.fn((path) => {
      if (path === '/api/admin/models/providers') {
        return Promise.resolve({
          items: [{ provider_id: 'mp_deepseek', provider_name: 'DeepSeek 模型渠道', provider_code: 'deepseek' }]
        });
      }
      if (path === '/api/admin/skills/system') {
        return Promise.resolve({
          items: [
            {
              skill_id: 'sk_seed_storyboard',
              skill_name: '故事板助手',
              skill_key: 'seed_storyboard',
              status: 'draft',
              latest_version_id: 'skv_seed_storyboard_100',
              active_test_case_count: 0
            }
          ],
          page_size: 10
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

import { adminApi } from '../../services/adminApi.js';
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

  test('records a system skill test with generated trace and idempotency metadata', async () => {
    const action = pageConfigs['skills/system'].actions[0];
    const values = {
      version_id: 'skv_seed_storyboard_100',
      test_run_id: 'skrun_manual_001',
      trace_id: 'trace_skrun_manual_001',
      test_case_id: '',
      status: 'passed',
      actual_elements_json: '[]',
      safety_evidence_json: JSON.stringify({
        scene: 'skill_test',
        target_type: 'skill_test_prompt',
        target_ref_id: 'skrun_manual_001',
        evaluated_object_digest: 'sha256:skrun_manual_001',
        policy_version: 'local-manual',
        evidence_version: '2026-06-30',
        result: 'passed',
        source_run_id: 'skrun_manual_001',
        trace_id: 'trace_skrun_manual_001',
        expires_at: '2027-01-01T00:00:00.000Z'
      }, null, 2)
    };
    renderResourcePage({
      ...pageConfigs['skills/system'],
      actions: [{ ...action, initialValues: () => values }]
    });

    await screen.findByText('故事板助手');
    await userEvent.click(screen.getByRole('button', { name: '记录测试结果' }));

    expect(screen.getByLabelText('版本 ID')).toHaveValue('skv_seed_storyboard_100');
    expect(screen.getByLabelText('测试运行 ID')).toHaveValue('skrun_manual_001');
    expect(screen.getByLabelText('Trace ID')).toHaveValue('trace_skrun_manual_001');
    expect(screen.getByText('临时调试入口')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: '保存测试记录' }));

    await waitFor(() => expect(adminApi.post).toHaveBeenCalledWith(
      '/api/admin/skills/system/sk_seed_storyboard/test',
      {
        version_id: 'skv_seed_storyboard_100',
        test_run_id: 'skrun_manual_001',
        status: 'passed',
        actual_elements_json: '[]',
        safety_evidence_json: values.safety_evidence_json
      },
      {
        idempotencyKey: 'skill_test:skrun_manual_001',
        headers: { 'X-Trace-Id': 'trace_skrun_manual_001' }
      }
    ));
  });
});
