package session

import (
	"encoding/json"
	"testing"
)

func TestApplyMessageWindowOrdersRunsAndFiltersWholeLaterGroup(t *testing.T) {
	records := []MessageRecord{
		{ID: "a-user", RunID: "run-a", Role: "user", Content: "A", Seq: 1},
		{ID: "b-user", RunID: "run-b", Role: "user", Content: "B", Seq: 2},
		{ID: "c-user", RunID: "run-c", Role: "user", Content: "C", Seq: 3},
		{ID: "a-out", RunID: "run-a", Role: "assistant", Content: "A-out", Seq: 4},
		{ID: "b-out", RunID: "run-b", Role: "assistant", Content: "B-out", Seq: 5},
		{ID: "c-out", RunID: "run-c", Role: "assistant", Content: "C-out", Seq: 6},
	}
	through := int64(2)
	got := ApplyMessageWindow(records, MessageWindow{ThroughSeq: &through, CurrentMessageID: "b-user"})
	want := []string{"A", "A-out", "B", "B-out"}
	if len(got) != len(want) {
		t.Fatalf("window=%+v", got)
	}
	for index, content := range want {
		if got[index].Content != content {
			t.Fatalf("window[%d]=%q, want=%q; all=%+v", index, got[index].Content, content, got)
		}
	}
}

func TestApplyMessageWindowZeroBoundaryAndLogicalLimit(t *testing.T) {
	records := []MessageRecord{
		{ID: "system", RunID: "system-run", Role: "system", Content: "system", Seq: 1},
		{ID: "a-user", RunID: "run-a", Role: "user", Content: "A", Seq: 2},
		{ID: "a-out", RunID: "run-a", Role: "assistant", Content: "A-out", Seq: 3},
	}
	zero := int64(0)
	got := ApplyMessageWindow(records, MessageWindow{ThroughSeq: &zero})
	if len(got) != 1 || got[0].Content != "system" {
		t.Fatalf("zero boundary must remove the entire user-owned group: %+v", got)
	}

	got = ApplyMessageWindow(records, MessageWindow{Limit: 2})
	if len(got) != 2 || got[0].Content != "A" || got[1].Content != "A-out" {
		t.Fatalf("limit was not applied after logical ordering: %+v", got)
	}
}

func TestApplyMessageWindowLimitKeepsUserToolCallAndResultTogether(t *testing.T) {
	toolCalls, err := json.Marshal([]map[string]any{{"id": "call-1", "name": "plan_storyboard"}})
	if err != nil {
		t.Fatal(err)
	}
	records := []MessageRecord{
		{ID: "user", RunID: "run-a", Role: "user", Content: "plan", Seq: 1},
		{ID: "assistant", RunID: "run-a", Role: "assistant", ToolCalls: toolCalls, Seq: 2},
		{ID: "tool", RunID: "run-a", Role: "tool", ToolCallID: "call-1", Content: "done", Seq: 3},
	}

	got := ApplyMessageWindow(records, MessageWindow{Limit: 1})
	if len(got) != len(records) {
		t.Fatalf("causal run was split by limit: %+v", got)
	}
	for index := range records {
		if got[index].ID != records[index].ID {
			t.Fatalf("window[%d]=%q, want %q; all=%+v", index, got[index].ID, records[index].ID, got)
		}
	}
}

func TestApplyMessageWindowLimitDropsWholePredecessorRun(t *testing.T) {
	records := []MessageRecord{
		{ID: "a-user", RunID: "run-a", Role: "user", Content: "A", Seq: 1},
		{ID: "a-assistant", RunID: "run-a", Role: "assistant", Seq: 2},
		{ID: "a-tool", RunID: "run-a", Role: "tool", ToolCallID: "call-a", Seq: 3},
		{ID: "b-user", RunID: "run-b", Role: "user", Content: "B", Seq: 4},
		{ID: "b-out", RunID: "run-b", Role: "assistant", Content: "B-out", Seq: 5},
	}

	got := ApplyMessageWindow(records, MessageWindow{Limit: 3})
	if len(got) != 2 || got[0].ID != "b-user" || got[1].ID != "b-out" {
		t.Fatalf("limit must select complete run groups: %+v", got)
	}
}

func TestApplyMessageWindowLimitRetainsCompleteCurrentMessageRun(t *testing.T) {
	records := []MessageRecord{
		{ID: "previous-user", RunID: "run-previous", Role: "user", Content: "previous", Seq: 1},
		{ID: "current-user", RunID: "run-current", Role: "user", Content: "current", Seq: 2},
		{ID: "later-user", RunID: "run-later", Role: "user", Content: "later", Seq: 3},
		{ID: "previous-out", RunID: "run-previous", Role: "assistant", Content: "previous-out", Seq: 4},
		{ID: "current-call", RunID: "run-current", Role: "assistant", Seq: 5},
		{ID: "current-tool", RunID: "run-current", Role: "tool", ToolCallID: "call-current", Seq: 6},
	}
	through := int64(2)

	got := ApplyMessageWindow(records, MessageWindow{Limit: 1, ThroughSeq: &through, CurrentMessageID: "current-user"})
	want := []string{"current-user", "current-call", "current-tool"}
	if len(got) != len(want) {
		t.Fatalf("current run was not retained in full: %+v", got)
	}
	for index, id := range want {
		if got[index].ID != id {
			t.Fatalf("window[%d]=%q, want %q; all=%+v", index, got[index].ID, id, got)
		}
	}
}
