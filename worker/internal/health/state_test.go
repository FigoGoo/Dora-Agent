package health

import "testing"

// TestState 验证 Worker 健康状态初始失败关闭并支持原子切换。
func TestState(t *testing.T) {
	state := NewState()
	if state.IsReady() {
		t.Fatal("新状态不应立即就绪")
	}
	state.SetReady(true)
	if !state.IsReady() {
		t.Fatal("状态应切换为就绪")
	}
}
