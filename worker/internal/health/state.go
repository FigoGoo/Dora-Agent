// Package health 保存 Business Worker 的进程存活和就绪投影。
package health

import "sync/atomic"

// State 保存当前 Worker 是否已完成依赖检查。
type State struct {
	ready atomic.Bool
}

// NewState 创建初始未就绪的健康状态。
func NewState() *State { return &State{} }

// SetReady 原子更新就绪状态。
func (s *State) SetReady(ready bool) { s.ready.Store(ready) }

// IsReady 返回当前 Worker 是否完成依赖检查。
func (s *State) IsReady() bool { return s.ready.Load() }
