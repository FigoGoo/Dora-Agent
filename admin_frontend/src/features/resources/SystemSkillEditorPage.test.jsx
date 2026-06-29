import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, test, vi } from 'vitest';

vi.mock('../../services/adminApi.js', () => ({
  adminApi: {
    list: vi.fn(() =>
      Promise.resolve({
        items: [
          {
            tool_key: 'storyboard_extract:builtin',
            tool_name: 'storyboard_extract',
            tool_type: 'builtin',
            display_name: '分镜提取',
            description: '从剧本文本中提取镜头、人物和场景信息。'
          }
        ]
      })
    ),
    post: vi.fn(() => Promise.resolve({}))
  }
}));

import { adminApi } from '../../services/adminApi.js';
import { SystemSkillEditorPage } from './SystemSkillEditorPage.jsx';

function renderEditor() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false }
    }
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/admin/skills/system/new']}>
        <SystemSkillEditorPage />
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe('SystemSkillEditorPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  test('renders a full-page system skill editor and submits markdown contract input', async () => {
    renderEditor();

    expect(screen.getByRole('heading', { name: '创建系统 Skill 草稿' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '返回列表' })).toHaveAttribute('href', '/admin/skills/system');
    expect(screen.getByText('基础信息')).toBeInTheDocument();
    expect(screen.getByText('引用与校验')).toBeInTheDocument();
    expect(await screen.findByText('<tool id="storyboard_extract:builtin">分镜提取</tool>')).toBeInTheDocument();
    expect(screen.getByText('从剧本文本中提取镜头、人物和场景信息。')).toBeInTheDocument();
    expect(screen.getByLabelText('Skill 标签').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Skill 内容 Markdown').value).toContain('<tool id="image_generate:model_generation">图片生成</tool>');

    await userEvent.clear(screen.getByLabelText('Skill 名称'));
    await userEvent.type(screen.getByLabelText('Skill 名称'), '故事板助手');
    await userEvent.clear(screen.getByLabelText('Skill 标签'));
    await userEvent.type(screen.getByLabelText('Skill 标签'), '视频,故事板');
    await userEvent.click(screen.getByRole('button', { name: '保存草稿' }));

    await waitFor(() => expect(adminApi.post).toHaveBeenCalledTimes(1));
    expect(adminApi.post).toHaveBeenCalledWith(
      '/api/admin/skills/system',
      expect.objectContaining({
        skill_name: '故事板助手',
        skill_tags: ['视频', '故事板'],
        version: '0.1.0',
        skill_markdown: expect.stringContaining('<agui id="storyboard_panel">故事板面板</agui>')
      })
    );
  });
});
