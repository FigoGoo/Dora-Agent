import { describe, expect, it } from 'vitest';
import { reduceM1AguiEvents } from './agui.js';

describe('M1 AG-UI reducer', () => {
  it('merges guide and router decision events by sequence and event id', () => {
    const guide = {
      event_id: 'evt_guide',
      type: 'creative.guide.presented',
      sequence: 2,
      payload: {
        creative_guide: {
          guide_id: 'guide_sess_1',
          suggested_prompts: [{ prompt_id: 'p1', label: '城市文旅视频', text: '帮我做杭州文旅视频' }],
          supported_output_types: ['video'],
          default_actions: ['free_creation']
        }
      }
    };
    const router = {
      event_id: 'evt_router',
      type: 'creative.router.decided',
      sequence: 3,
      payload: {
        router_decision: {
          decision: 'select_skill',
          skill_id: 'skill_city_tourism_video',
          confidence: 0.92,
          candidate_skills: [{ skill_id: 'skill_city_tourism_video', why: '命中城市文旅宣传视频场景' }]
        }
      }
    };

    const state = reduceM1AguiEvents([router, guide, guide]);

    expect(state.guide.guide_id).toBe('guide_sess_1');
    expect(state.routerDecision.skill_id).toBe('skill_city_tourism_video');
    expect(state.suggestionChips).toHaveLength(1);
    expect(state.routerBanner).toEqual({
      decision: 'select_skill',
      skillId: 'skill_city_tourism_video',
      confidence: 0.92,
      text: '已选择 skill_city_tourism_video'
    });
    expect(state.eventIds).toEqual(['evt_guide', 'evt_router']);
  });
});
