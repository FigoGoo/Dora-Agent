import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ProjectsPage } from './ProjectsPage.jsx';

const FIRST_ID = '019f0000-0000-7000-8000-000000000211';
const SECOND_ID = '019f0000-0000-7000-8000-000000000212';

describe('ProjectsPage', () => {
  it('loads real project pages and navigates to the formal workspace reference', async () => {
    const user = userEvent.setup();
    const loadProjects = vi.fn()
      .mockResolvedValueOnce({ items: [projectItem(FIRST_ID, '第一个项目')], nextAfter: 'next_1' })
      .mockResolvedValueOnce({ items: [projectItem(SECOND_ID, '第二个项目')], nextAfter: null });
    const onNavigate = vi.fn();
    render(<ProjectsPage loadProjects={loadProjects} onNavigate={onNavigate} />);

    await user.click(await screen.findByRole('button', { name: '继续创作 第一个项目' }));
    expect(onNavigate).toHaveBeenCalledWith(`/projects/${FIRST_ID}/workspace`);

    await user.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByRole('button', { name: '继续创作 第二个项目' })).toBeInTheDocument();
    expect(loadProjects).toHaveBeenLastCalledWith(expect.objectContaining({ after: 'next_1' }));
    expect(screen.getAllByTestId('project-card')).toHaveLength(3);
  });

  it('shows an actionable error without restoring mock projects', async () => {
    const loadProjects = vi.fn().mockRejectedValue(new Error('项目服务暂时不可用'));
    render(<ProjectsPage loadProjects={loadProjects} />);
    expect(await screen.findByRole('alert')).toHaveTextContent('项目服务暂时不可用');
    expect(screen.getAllByTestId('project-card')).toHaveLength(1);
  });
});

function projectItem(projectID, title) {
  return {
    projectID,
    title,
    lifecycleStatus: 'active',
    recentRunStatus: 'running',
    initialPromptStatus: 'accepted',
    updatedAt: '2026-07-17T09:08:07.123Z',
    workspaceRef: `/projects/${projectID}/workspace`
  };
}
