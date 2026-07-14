import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { QuickCreateSkillPicker } from './QuickCreateSkillPicker.jsx';
import { PROJECT_QUICK_CREATE_MAX_SKILL_COUNT } from './projectQuickCreate.js';

const IDS = Object.freeze({
  published: '019f0000-0000-7000-8000-000000000131',
  draft: '019f0000-0000-7000-8000-000000000132',
  suspended: '019f0000-0000-7000-8000-000000000133'
});

describe('QuickCreate Skill picker', () => {
  it('loads all pages and only selects published active Skills', async () => {
    let resolveFirstPage;
    const firstPage = new Promise((resolve) => {
      resolveFirstPage = resolve;
    });
    const loadSkills = vi.fn()
      .mockReturnValueOnce(firstPage)
      .mockResolvedValueOnce({
        items: [
          skill(IDS.draft, '草稿 Skill', 'draft', 'active'),
          skill(IDS.suspended, '暂停 Skill', 'published', 'suspended')
        ],
        nextCursor: null
      });
    const user = userEvent.setup();
    render(<PickerHarness loadSkills={loadSkills} />);

    await user.click(screen.getByRole('button', { name: 'Skill' }));
    const dialog = screen.getByRole('dialog', { name: '选择 QuickCreate Skill' });
    expect(within(dialog).getByRole('status')).toHaveTextContent('正在加载');
    await act(async () => resolveFirstPage({
      items: [skill(IDS.published, '可用 Skill', 'published', 'active')],
      nextCursor: 'page-2'
    }));
    const published = await within(dialog).findByRole('checkbox', { name: '选择 可用 Skill' });
    expect(published).toBeEnabled();
    expect(within(dialog).getByRole('checkbox', { name: '选择 草稿 Skill' })).toBeDisabled();
    expect(within(dialog).getByRole('checkbox', { name: '选择 暂停 Skill' })).toBeDisabled();
    expect(within(dialog).getByText('草稿尚未发布')).toBeInTheDocument();
    expect(within(dialog).getByText('已暂停')).toBeInTheDocument();
    expect(loadSkills).toHaveBeenNthCalledWith(1, expect.objectContaining({ cursor: undefined }));
    expect(loadSkills).toHaveBeenNthCalledWith(2, expect.objectContaining({ cursor: 'page-2' }));

    await user.click(published);
    expect(screen.getByRole('button', { name: 'Skill，已选择 1 个' })).toHaveClass('is-active');
  });

  it('renders empty and retryable error states explicitly', async () => {
    const loadSkills = vi.fn()
      .mockRejectedValueOnce(new Error('Skill 服务暂不可用'))
      .mockResolvedValueOnce({ items: [], nextCursor: null });
    const user = userEvent.setup();
    render(<PickerHarness loadSkills={loadSkills} />);

    await user.click(screen.getByRole('button', { name: 'Skill' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Skill 服务暂不可用');
    await user.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByText('还没有 Skill')).toBeInTheDocument();
    expect(screen.getByText(/先在“我的 Skill”中创建并发布/)).toBeInTheDocument();
  });

  it('uses login for anonymous users and disables changes while submitting', async () => {
    const onLogin = vi.fn();
    const loadSkills = vi.fn();
    const user = userEvent.setup();
    const { rerender } = render(
      <PickerHarness isAuthenticated={false} onLogin={onLogin} loadSkills={loadSkills} />
    );

    await user.click(screen.getByRole('button', { name: 'Skill' }));
    expect(onLogin).toHaveBeenCalledWith('选择 Skill', expect.stringContaining('已发布'));
    expect(loadSkills).not.toHaveBeenCalled();

    rerender(<PickerHarness isAuthenticated isDisabled onLogin={onLogin} loadSkills={loadSkills} />);
    expect(screen.getByRole('button', { name: 'Skill' })).toBeDisabled();
  });

  it('drops a stale selection when a refreshed Owner projection is no longer usable', async () => {
    const loadSkills = vi.fn().mockResolvedValue({
      items: [skill(IDS.published, '已下架 Skill', 'published', 'offline')],
      nextCursor: null
    });
    const user = userEvent.setup();
    render(<PickerHarness loadSkills={loadSkills} initialSelection={[IDS.published]} />);

    await user.click(screen.getByRole('button', { name: 'Skill，已选择 1 个' }));
    await screen.findByText('已下架');
    await act(async () => {});
    expect(screen.getByRole('button', { name: 'Skill' })).not.toHaveClass('is-active');
  });

  it('refreshes the Owner projection every time it reopens', async () => {
    const loadSkills = vi.fn()
      .mockResolvedValueOnce({
        items: [skill(IDS.published, '可用 Skill', 'published', 'active')],
        nextCursor: null
      })
      .mockResolvedValueOnce({
        items: [skill(IDS.published, '已暂停 Skill', 'published', 'suspended')],
        nextCursor: null
      });
    const user = userEvent.setup();
    render(<PickerHarness loadSkills={loadSkills} initialSelection={[IDS.published]} />);

    await user.click(screen.getByRole('button', { name: 'Skill，已选择 1 个' }));
    expect(await screen.findByText('已发布 · 可用')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '关闭 Skill 选择' }));
    await user.click(screen.getByRole('button', { name: 'Skill，已选择 1 个' }));

    expect(await screen.findByText('已暂停')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Skill' })).not.toHaveClass('is-active');
    expect(loadSkills).toHaveBeenCalledTimes(2);
  });

  it('offers an explicit refresh action for the current Owner projection', async () => {
    const loadSkills = vi.fn()
      .mockResolvedValueOnce({
        items: [skill(IDS.published, '可用 Skill', 'published', 'active')],
        nextCursor: null
      })
      .mockResolvedValueOnce({
        items: [skill(IDS.published, '已暂停 Skill', 'published', 'suspended')],
        nextCursor: null
      });
    const user = userEvent.setup();
    render(<PickerHarness loadSkills={loadSkills} initialSelection={[IDS.published]} />);

    await user.click(screen.getByRole('button', { name: 'Skill，已选择 1 个' }));
    expect(await screen.findByText('已发布 · 可用')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '刷新 Skill 列表' }));

    expect(await screen.findByText('已暂停')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Skill' })).not.toHaveClass('is-active');
  });

  it('disables an additional selection at the frozen 16-item limit', async () => {
    const items = Array.from({ length: PROJECT_QUICK_CREATE_MAX_SKILL_COUNT + 1 }, (_, index) => (
      skill(skillID(index + 1), `可用 Skill ${index + 1}`, 'published', 'active')
    ));
    const loadSkills = vi.fn().mockResolvedValue({ items, nextCursor: null });
    const user = userEvent.setup();
    render(<PickerHarness
      loadSkills={loadSkills}
      initialSelection={items.slice(0, PROJECT_QUICK_CREATE_MAX_SKILL_COUNT).map((item) => item.skillID)}
    />);

    await user.click(screen.getByRole('button', { name: 'Skill，已选择 16 个' }));
    expect(await screen.findByRole('status')).toHaveTextContent('已达上限');
    expect(screen.getByRole('checkbox', { name: '选择 可用 Skill 17' })).toBeDisabled();
    expect(screen.getByText('已达到选择上限')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: '选择 可用 Skill 1' })).toBeEnabled();
  });

  it('moves focus into the dialog and restores it after Escape', async () => {
    const loadSkills = vi.fn().mockResolvedValue({ items: [], nextCursor: null });
    const user = userEvent.setup();
    render(<PickerHarness loadSkills={loadSkills} />);

    const trigger = screen.getByRole('button', { name: 'Skill' });
    await user.click(trigger);
    expect(screen.getByRole('button', { name: '关闭 Skill 选择' })).toHaveFocus();

    await user.keyboard('{Escape}');
    expect(screen.queryByRole('dialog', { name: '选择 QuickCreate Skill' })).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });
});

function PickerHarness({
  isAuthenticated = true,
  isDisabled = false,
  initialSelection = [],
  loadSkills,
  onLogin = vi.fn()
}) {
  const [selectedSkillIDs, setSelectedSkillIDs] = useState(initialSelection);
  return (
    <QuickCreateSkillPicker
      isAuthenticated={isAuthenticated}
      isDisabled={isDisabled}
      selectedSkillIDs={selectedSkillIDs}
      onChange={setSelectedSkillIDs}
      onLogin={onLogin}
      loadSkills={loadSkills}
    />
  );
}

function skill(skillID, name, contentStatus, governanceStatus) {
  return {
    skillID,
    definition: { name },
    contentStatus,
    governanceStatus
  };
}

function skillID(index) {
  return `019f0000-0000-7000-8000-${String(index).padStart(12, '0')}`;
}
