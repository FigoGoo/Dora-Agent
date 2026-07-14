package workspace

import (
	"fmt"
	"sync"
)

// StreamLimiter 在进程内实施实例、User 和 Session 三层 SSE 并发预算。
// 它只控制资源，不承载业务状态或断线恢复进度。
type StreamLimiter struct {
	mutex      sync.Mutex
	maxTotal   int
	maxUser    int
	maxSession int
	total      int
	byUser     map[string]int
	bySession  map[string]int
}

// NewStreamLimiter 创建固定并发预算；任一上限非正或层级倒置时拒绝构造。
func NewStreamLimiter(maxTotal, maxUser, maxSession int) (*StreamLimiter, error) {
	if maxTotal <= 0 || maxUser <= 0 || maxSession <= 0 || maxUser > maxTotal || maxSession > maxUser {
		return nil, fmt.Errorf("create Workspace stream limiter: invalid concurrency limits")
	}
	return &StreamLimiter{
		maxTotal: maxTotal, maxUser: maxUser, maxSession: maxSession,
		byUser: make(map[string]int), bySession: make(map[string]int),
	}, nil
}

// Acquire 在写 HTTP Header 前原子检查三层预算；成功返回必须恰好调用一次的幂等 release。
func (l *StreamLimiter) Acquire(userID, sessionID string) (release func(), ok bool) {
	l.mutex.Lock()
	if l.total >= l.maxTotal || l.byUser[userID] >= l.maxUser || l.bySession[sessionID] >= l.maxSession {
		l.mutex.Unlock()
		return func() {}, false
	}
	l.total++
	l.byUser[userID]++
	l.bySession[sessionID]++
	l.mutex.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mutex.Lock()
			defer l.mutex.Unlock()
			l.total--
			l.byUser[userID]--
			l.bySession[sessionID]--
			if l.byUser[userID] == 0 {
				delete(l.byUser, userID)
			}
			if l.bySession[sessionID] == 0 {
				delete(l.bySession, sessionID)
			}
		})
	}, true
}
