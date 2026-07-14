import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ownerSkillFixture } from '../../test/skillFixtures.js';
import { parseOwnerSkill } from './skillContract.js';
import { MySkillsPage } from './MySkillsPage.jsx';

describe('MySkillsPage', () => {
  it('groups by content status and keeps review and governance states separate', async () => {
    const published = parseOwnerSkill(ownerSkillFixture({
      content_status: 'published',
      has_unpublished_changes: true,
      review_status: 'reviewing',
      review_updated_at: '2026-07-14T10:00:00+08:00'
    }));
    const draft = parseOwnerSkill(ownerSkillFixture({
      skill_id: '019f0000-0000-7000-8000-000000000124',
      definition: { ...ownerSkillFixture().definition, name: '分镜草稿 Skill' },
      governance_status: 'suspended'
    }));
    const loadSkills = vi.fn().mockResolvedValue({ items: [published, draft], nextCursor: null });
    const onNavigate = vi.fn();

    render(<MySkillsPage loadSkills={loadSkills} onNavigate={onNavigate} />);

    await waitFor(() => expect(screen.getAllByTestId('owner-skill-card')).toHaveLength(2));
    expect(loadSkills).toHaveBeenCalledWith({ cursor: null, signal: expect.any(AbortSignal) });

    const publishedGroup = screen.getByRole('heading', { name: '已发布' }).closest('section');
    expect(within(publishedGroup).getByText('剧情短片 Skill')).toBeInTheDocument();
    expect(within(publishedGroup).getByText('有未发布修改')).toBeInTheDocument();
    expect(within(publishedGroup).getByText('审核中')).toBeInTheDocument();
    expect(within(publishedGroup).getByText('可用')).toBeInTheDocument();

    const draftGroup = screen.getByRole('heading', { name: '草稿' }).closest('section');
    expect(within(draftGroup).getByText('分镜草稿 Skill')).toBeInTheDocument();
    expect(within(draftGroup).getByText('已暂停')).toBeInTheDocument();
    expect(screen.queryByText(/\bv\d+(?:\.\d+)*\b|版本号\s*[:：]?\s*\d/i)).not.toBeInTheDocument();

    await userEvent.click(within(draftGroup).getByRole('button', { name: '编辑草稿' }));
    expect(onNavigate).toHaveBeenCalledWith('/my/skills/019f0000-0000-7000-8000-000000000124/edit');
  });

  it('renders a recoverable empty and error state', async () => {
    const loadSkills = vi.fn()
      .mockRejectedValueOnce(new Error('暂时不可用'))
      .mockResolvedValueOnce({ items: [], nextCursor: null });

    render(<MySkillsPage loadSkills={loadSkills} />);

    expect(await screen.findByRole('alert')).toHaveTextContent('暂时不可用');
    await userEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByRole('heading', { name: '还没有 Skill' })).toBeInTheDocument();
  });

  it('aborts an in-flight list request when the route unmounts', async () => {
    let resolveList;
    let requestSignal;
    const loadSkills = vi.fn(({ signal }) => {
      requestSignal = signal;
      return new Promise((resolve) => { resolveList = resolve; });
    });

    const { unmount } = render(<MySkillsPage loadSkills={loadSkills} />);
    await waitFor(() => expect(loadSkills).toHaveBeenCalledTimes(1));
    unmount();

    expect(requestSignal.aborted).toBe(true);
    await act(async () => {
      resolveList({ items: [], nextCursor: null });
      await Promise.resolve();
    });
  });
});
