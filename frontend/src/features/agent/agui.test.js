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

  it('merges M2 graph and board events without invoking generation state', () => {
    const graph = {
      event_id: 'evt_graph',
      type: 'graph.plan.created',
      sequence: 4,
      payload: {
        graph_plan_id: 'gplan_1',
        graph_template_id: 'gtemplate_generic_creation',
        graph_plan_status: 'compiled',
        graph_plan_digest: 'sha256:graph',
        board_id: 'board_1',
        value_delivered_stage: 'storyboard_ready'
      }
    };
    const snapshot = {
      event_id: 'evt_board',
      type: 'board.snapshot.updated',
      sequence: 5,
      payload: {
        board_id: 'board_1',
        board_version: 1,
        board_status: 'ready',
        board_digest: 'sha256:board',
        changed_element_ids: ['elem_1', 'elem_2'],
        snapshot_required: true
      }
    };
    const patch = {
      event_id: 'evt_patch',
      type: 'board.patch.applied',
      sequence: 6,
      payload: {
        board_id: 'board_1',
        patch_id: 'patch_1',
        base_version: 1,
        target_version: 2,
        operation: 'update_element',
        patch_digest: 'sha256:patch'
      }
    };

    const state = reduceM1AguiEvents([patch, snapshot, graph, snapshot]);

    expect(state.graphPlan).toMatchObject({ graphPlanId: 'gplan_1', boardId: 'board_1', status: 'compiled' });
    expect(state.board).toMatchObject({ boardId: 'board_1', version: 1, status: 'ready', snapshotRequired: true });
    expect(state.board.changedElementIds).toEqual(['elem_1', 'elem_2']);
    expect(state.boardPatch).toMatchObject({ patchId: 'patch_1', targetVersion: 2, operation: 'update_element' });
    expect(state.boardPatches).toHaveLength(1);
    expect(state.eventIds).toEqual(['evt_graph', 'evt_board', 'evt_patch']);
  });
});
