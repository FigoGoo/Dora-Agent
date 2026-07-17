const pendingGoals = new Map();

// stageQuickCreatePreviewGoal 只在当前页面内暂存 QuickCreate Goal；不会写入任何浏览器持久化介质。
export function stageQuickCreatePreviewGoal(projectID, goal) {
  const key = requireProjectID(projectID);
  pendingGoals.set(key, goal == null ? '' : String(goal));
}

// consumeQuickCreatePreviewGoal 按 Project 一次性领取 Goal；不匹配的 Project 无法读取或删除它。
export function consumeQuickCreatePreviewGoal(projectID) {
  const key = requireProjectID(projectID);
  if (!pendingGoals.has(key)) return null;
  const goal = pendingGoals.get(key);
  pendingGoals.delete(key);
  return goal;
}

function requireProjectID(projectID) {
  const key = String(projectID || '');
  if (!key) throw new TypeError('QuickCreate Preview Goal 交接需要 project_id');
  return key;
}
