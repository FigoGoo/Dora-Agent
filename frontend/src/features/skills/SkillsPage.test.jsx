import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SkillsPage } from './SkillsPage.jsx';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function ok(data) {
  return {
    ok: true,
    status: 200,
    json: async () => ({ code: 'OK', message: 'ok', data, trace_id: 'trace_frontend_marketplace' })
  };
}

describe('SkillsPage marketplace integration', () => {
  it('loads marketplace listings and installs a skill through the M5 API', async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async (url, init = {}) => {
      const path = String(url);
      if (path.startsWith('/api/marketplace/skills')) {
        return ok({
          items: [
            {
              listing_id: 'listing_city_tourism_creator_001',
              skill_id: 'skill_city_tourism_video',
              skill_version_id: 'sv_city_tourism_video_1',
              skill_version: '1.0.0',
              skill_name: '文旅城市名片',
              skill_description: '把城市、节气、路线和旁白串成文旅宣传片。',
              creator_user_id: 'creator_city_001',
              status: 'listed',
              pricing_model: 'fixed',
              usage_credits: 120
            }
          ],
          next_cursor: ''
        });
      }
      if (path.startsWith('/api/marketplace/my-skills')) {
        return ok({ items: [] });
      }
      if (path === '/api/marketplace/installations' && init.method === 'POST') {
        return ok({
          installation: {
            installation_id: 'sinst_city_tourism_video_001',
            account_id: 'acct_personal_001',
            account_scope: 'personal',
            listing_id: 'listing_city_tourism_creator_001',
            skill_id: 'skill_city_tourism_video',
            skill_name: '文旅城市名片',
            installed_version: '1.0.0',
            version_strategy: 'latest_published',
            status: 'installed',
            upgrade_status: 'none'
          },
          idempotent_replay: false
        });
      }
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<SkillsPage isLoggedIn onIntent={vi.fn()} onUseSkill={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getAllByTestId('skill-card')).toHaveLength(1);
    });
    expect(screen.getByText('文旅城市名片')).toBeInTheDocument();
    await user.click(within(screen.getAllByTestId('skill-card')[0]).getByRole('button', { name: '安装' }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/marketplace/installations', expect.objectContaining({ method: 'POST' }));
    });
    const postCall = fetchMock.mock.calls.find(([url, init]) => url === '/api/marketplace/installations' && init.method === 'POST');
    expect(JSON.parse(postCall[1].body)).toEqual(expect.objectContaining({ listing_id: 'listing_city_tourism_creator_001', target_scope: 'personal' }));
    expect(postCall[1].headers['Idempotency-Key']).toBe('install:listing_city_tourism_creator_001:personal');
    const installedCard = screen.getAllByTestId('skill-card')[0];
    expect(within(installedCard).getByText('已安装')).toBeInTheDocument();
    expect(within(installedCard).getByRole('button', { name: '使用' })).toBeInTheDocument();
  });

  it('keeps installation behind login when the user is anonymous', async () => {
    const user = userEvent.setup();
    const onIntent = vi.fn();
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    render(<SkillsPage isLoggedIn={false} onIntent={onIntent} />);

    await user.click(within(screen.getAllByTestId('skill-card')[0]).getByRole('button', { name: '安装' }));

    expect(fetchMock).not.toHaveBeenCalled();
    expect(onIntent).toHaveBeenCalledWith(expect.stringContaining('安装'), '登录后安装并加入我的 Skill。', 'skills');
  });

  it('creates a creator draft and submits it for review', async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async (url, init = {}) => {
      const path = String(url);
      if (path.startsWith('/api/marketplace/skills')) {
        return ok({ items: [], next_cursor: '' });
      }
      if (path.startsWith('/api/marketplace/my-skills')) {
        return ok({ items: [] });
      }
      if (path.startsWith('/api/creator/listings')) {
        return ok({ items: [] });
      }
      if (path === '/api/creator/analytics/skill-usage') {
        return ok({ usage_count: 0, revenue_hold_amount: 0, refund_count: 0, failure_code_summary: {} });
      }
      if (path === '/api/creator/skills' && init.method === 'POST') {
        return ok({
          skill: {
            skill_id: 'skill_creator_city_001',
            name: '文旅脚本策划',
            description: '把城市卖点拆成 Storyboard 和提示词。',
            visibility: 'review_only',
            version: 'v1',
            skill_version_id: 'skv_creator_city_001',
            version_status: 'draft',
            review_status: 'not_submitted',
            listing_status: 'not_listed',
            pricing_model: 'free',
            usage_credits: 0,
            value_delivered_stage: 'storyboard_ready'
          }
        });
      }
      if (path === '/api/creator/skills/skill_creator_city_001/versions/v1/submit' && init.method === 'POST') {
        return ok({
          skill_version: {
            skill_id: 'skill_creator_city_001',
            name: '文旅脚本策划',
            description: '把城市卖点拆成 Storyboard 和提示词。',
            visibility: 'review_only',
            version: 'v1',
            skill_version_id: 'skv_creator_city_001',
            version_status: 'submitted',
            review_status: 'submitted',
            listing_status: 'not_listed',
            pricing_model: 'free',
            usage_credits: 0,
            value_delivered_stage: 'storyboard_ready',
            submitted_at: '2026-07-01T07:00:00Z'
          }
        });
      }
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<SkillsPage isLoggedIn onIntent={vi.fn()} onUseSkill={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: '创作台' }));
    await user.type(screen.getByLabelText('Skill 名称'), '文旅脚本策划');
    await user.type(screen.getByLabelText('Skill 说明'), '把城市卖点拆成 Storyboard 和提示词。');
    await user.click(screen.getByRole('button', { name: '保存草稿' }));

    await screen.findByText('草稿已保存，可提交审核。');
    const createCall = fetchMock.mock.calls.find(([url, init]) => url === '/api/creator/skills' && init.method === 'POST');
    expect(JSON.parse(createCall[1].body)).toEqual(expect.objectContaining({ name: '文旅脚本策划', description: '把城市卖点拆成 Storyboard 和提示词。' }));

    const card = screen.getByTestId('creator-skill-card');
    expect(within(card).getByText('未提交')).toBeInTheDocument();
    await user.click(within(card).getByRole('button', { name: '提交审核' }));

    await screen.findByText('已提交审核，等待平台确认。');
    const submitCall = fetchMock.mock.calls.find(([url, init]) => url === '/api/creator/skills/skill_creator_city_001/versions/v1/submit' && init.method === 'POST');
    expect(submitCall[1].headers['Idempotency-Key']).toBe('creator-submit:skill_creator_city_001:v1');
    expect(within(screen.getByTestId('creator-skill-card')).getByText('已提交')).toBeInTheDocument();
    expect(within(screen.getByTestId('creator-skill-card')).getByRole('button', { name: '等待审核' })).toBeDisabled();
  });
});
