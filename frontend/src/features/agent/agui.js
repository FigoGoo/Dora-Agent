export function reduceM1AguiEvents(events = []) {
  const state = {
    guide: null,
    routerDecision: null,
    graphPlan: null,
    board: null,
    boardPatch: null,
    boardPatches: [],
    suggestionChips: [],
    routerBanner: null,
    eventIds: []
  };
  const seen = new Set();
  const ordered = [...events].sort((left, right) => Number(left.sequence || 0) - Number(right.sequence || 0));

  ordered.forEach((event) => {
    if (!event?.event_id || seen.has(event.event_id)) {
      return;
    }
    seen.add(event.event_id);
    state.eventIds.push(event.event_id);

    const type = event.type || event.event_type;
    const payload = event.payload || {};
    if (type === 'creative.guide.presented') {
      state.guide = payload.creative_guide || null;
      state.suggestionChips = Array.isArray(state.guide?.suggested_prompts) ? state.guide.suggested_prompts : [];
    }
    if (type === 'creative.router.decided') {
      state.routerDecision = payload.router_decision || null;
      state.routerBanner = buildRouterBanner(state.routerDecision);
    }
    if (type === 'graph.plan.created') {
      state.graphPlan = {
        graphPlanId: payload.graph_plan_id,
        graphTemplateId: payload.graph_template_id,
        status: payload.graph_plan_status,
        digest: payload.graph_plan_digest,
        boardId: payload.board_id,
        valueDeliveredStage: payload.value_delivered_stage
      };
    }
    if (type === 'board.patch.applied') {
      state.boardPatch = {
        boardId: payload.board_id,
        patchId: payload.patch_id,
        baseVersion: payload.base_version,
        targetVersion: payload.target_version,
        operation: payload.operation,
        digest: payload.patch_digest
      };
      state.boardPatches = [...state.boardPatches, state.boardPatch];
    }
    if (type === 'board.snapshot.updated') {
      state.board = {
        boardId: payload.board_id,
        version: payload.board_version,
        status: payload.board_status,
        digest: payload.board_digest,
        changedElementIds: Array.isArray(payload.changed_element_ids) ? payload.changed_element_ids : [],
        snapshotRequired: Boolean(payload.snapshot_required)
      };
    }
  });

  return state;
}

function buildRouterBanner(decision) {
  if (!decision) {
    return null;
  }
  if (decision.decision === 'select_skill' || decision.decision === 'generic_creation') {
    return {
      decision: decision.decision,
      skillId: decision.skill_id,
      confidence: decision.confidence,
      text: `已选择 ${decision.skill_id}`
    };
  }
  if (decision.decision === 'clarify') {
    return {
      decision: decision.decision,
      skillId: null,
      confidence: decision.confidence,
      text: '需要补充创作信息'
    };
  }
  return {
    decision: decision.decision,
    skillId: decision.skill_id || null,
    confidence: decision.confidence,
    text: '已完成能力判断'
  };
}
