// Package health 保存 Business Service 的进程存活和就绪投影。
package health

import "sync/atomic"

// State 保存当前实例是否可以接收新流量。
type State struct {
	ready atomic.Bool
}

// NewState 创建初始未就绪的健康状态，避免依赖尚未完成时接收流量。
func NewState() *State {
	return &State{}
}

// SetReady 原子更新就绪状态，供启动和优雅退出流程控制流量。
func (s *State) SetReady(ready bool) {
	s.ready.Store(ready)
}

// IsReady 返回当前实例是否完成依赖检查并允许接收新流量。
func (s *State) IsReady() bool {
	return s.ready.Load()
}
