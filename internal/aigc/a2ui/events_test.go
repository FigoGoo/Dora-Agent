package a2ui

import (
	"encoding/json"
	"testing"
)

func TestSkillSelectedPayloadJSON(t *testing.T) {
	if EventSkillSelected != "skill.selected" {
		t.Fatalf("EventSkillSelected = %q", EventSkillSelected)
	}
	b, err := json.Marshal(SkillSelectedPayload{
		SkillID:   "sk_travel",
		SkillName: "人文纪录短片",
		Reason:    "文旅题材",
		Fallback:  false,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	want := `{"skill_id":"sk_travel","skill_name":"人文纪录短片","reason":"文旅题材"}`
	if got != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
}
