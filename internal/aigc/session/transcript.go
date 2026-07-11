package session

import (
	"sort"
	"strings"
)

type transcriptGroup struct {
	key      string
	messages []MessageRecord
	userSeq  *int64
	firstSeq int64
	current  bool
}

// ApplyMessageWindow turns the append-only physical message log into the
// causal transcript consumed by a durable Agent turn. Messages written by one
// turn share RunID. That lets a user message queued while its predecessor is
// still running be placed after the predecessor's later-appended
// assistant/tool output.
//
// ThroughSeq applies to whole user-owned groups, so excluding a later user also
// excludes that user's output and can never produce an orphan assistant reply.
// Limit is a soft message budget applied only after causal ordering. A whole
// run is retained when including any message from that run would otherwise
// split its user/tool-call/tool-result chain. Consequently a single run may
// make the returned transcript larger than Limit.
func ApplyMessageWindow(records []MessageRecord, window MessageWindow) []MessageRecord {
	if len(records) == 0 {
		return nil
	}
	physical := append([]MessageRecord(nil), records...)
	sort.SliceStable(physical, func(i, j int) bool {
		if physical[i].Seq != physical[j].Seq {
			return physical[i].Seq < physical[j].Seq
		}
		return physical[i].ID < physical[j].ID
	})

	groupsByKey := make(map[string]*transcriptGroup)
	groups := make([]*transcriptGroup, 0)
	currentMessageID := strings.TrimSpace(window.CurrentMessageID)
	for _, record := range physical {
		key := strings.TrimSpace(record.RunID)
		if key == "" {
			// Legacy/unattributed rows must not collapse into one artificial run.
			key = "message:" + record.ID
		}
		group := groupsByKey[key]
		if group == nil {
			group = &transcriptGroup{key: key, firstSeq: record.Seq}
			groupsByKey[key] = group
			groups = append(groups, group)
		}
		group.messages = append(group.messages, record)
		if record.Seq < group.firstSeq {
			group.firstSeq = record.Seq
		}
		if strings.EqualFold(record.Role, "user") {
			if group.userSeq == nil || record.Seq < *group.userSeq {
				value := record.Seq
				group.userSeq = &value
			}
		}
		if currentMessageID != "" && record.ID == currentMessageID {
			group.current = true
		}
	}

	visible := groups[:0]
	for _, group := range groups {
		if window.ThroughSeq != nil && group.userSeq != nil && *group.userSeq > *window.ThroughSeq {
			continue
		}
		sort.SliceStable(group.messages, func(i, j int) bool {
			leftUser := strings.EqualFold(group.messages[i].Role, "user")
			rightUser := strings.EqualFold(group.messages[j].Role, "user")
			if leftUser != rightUser {
				return leftUser
			}
			if group.messages[i].Seq != group.messages[j].Seq {
				return group.messages[i].Seq < group.messages[j].Seq
			}
			return group.messages[i].ID < group.messages[j].ID
		})
		visible = append(visible, group)
	}
	sort.SliceStable(visible, func(i, j int) bool {
		if visible[i].current != visible[j].current {
			return !visible[i].current
		}
		leftSeq, rightSeq := visible[i].firstSeq, visible[j].firstSeq
		if visible[i].userSeq != nil {
			leftSeq = *visible[i].userSeq
		}
		if visible[j].userSeq != nil {
			rightSeq = *visible[j].userSeq
		}
		if leftSeq != rightSeq {
			return leftSeq < rightSeq
		}
		return visible[i].key < visible[j].key
	})

	if window.Limit > 0 {
		messageCount := 0
		firstVisible := len(visible)
		for index := len(visible) - 1; index >= 0; index-- {
			groupSize := len(visible[index].messages)
			if firstVisible < len(visible) && messageCount+groupSize > window.Limit {
				break
			}
			firstVisible = index
			messageCount += groupSize
		}
		visible = visible[firstVisible:]
	}

	logical := make([]MessageRecord, 0, len(physical))
	for _, group := range visible {
		logical = append(logical, group.messages...)
	}
	return logical
}
