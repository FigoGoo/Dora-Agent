package health

import "testing"

// TestStateTransitions 验证健康状态在启动和退出阶段可以原子切换。
func TestStateTransitions(t *testing.T) {
	state := NewState()
	if state.IsReady() {
		t.Fatal("新建状态不应立即就绪")
	}
	state.SetReady(true)
	if !state.IsReady() {
		t.Fatal("设置就绪后应返回 true")
	}
	state.SetReady(false)
	if state.IsReady() {
		t.Fatal("退出阶段应恢复未就绪")
	}
}
