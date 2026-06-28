package stream

import "sync"

type Event struct {
	ID       string
	Sequence int64
	Type     string
	Data     []byte
}

type Manager struct {
	mu     sync.RWMutex
	events map[string][]Event
}

func NewManager() *Manager {
	return &Manager{events: map[string][]Event{}}
}

func (m *Manager) Append(runID string, event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[runID] = append(m.events[runID], event)
}

func (m *Manager) Replay(runID string, afterSequence int64, limit int) []Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	out := []Event{}
	for _, event := range m.events[runID] {
		if event.Sequence <= afterSequence {
			continue
		}
		out = append(out, event)
		if len(out) >= limit {
			break
		}
	}
	return out
}
