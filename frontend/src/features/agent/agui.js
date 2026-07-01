export function reduceM1AguiEvents(events = []) {
  const state = {
    guide: null,
    routerDecision: null,
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
