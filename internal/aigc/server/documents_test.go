package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	aigcsession "github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	aigcskill "github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	aigcspec "github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
)

type fakeSpecReader struct {
	spec aigcspec.FinalVideoSpec
	err  error
}

func (f *fakeSpecReader) GetLatestBySession(_ context.Context, _ string) (aigcspec.FinalVideoSpec, error) {
	return f.spec, f.err
}

func docRouter(store *fakeSessionStore, skills *fakeSkillStore, specs FinalVideoSpecReader) http.Handler {
	return NewRouter(Config{
		Store:   store,
		Skills:  skills,
		Specs:   specs,
		Invoker: &fakeAgentInvoker{},
		NewID:   func() string { return "id" },
		Now:     func() time.Time { return time.Unix(0, 0).UTC() },
	})
}

func TestGetSessionSpecReturnsMarkdown(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1"}
	specs := &fakeSpecReader{spec: aigcspec.FinalVideoSpec{SessionID: "s1", Markdown: "# Final Video Spec\n内容"}}
	router := docRouter(store, &fakeSkillStore{records: map[string]aigcskill.SkillRecord{}}, specs)

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/spec", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got aigcspec.FinalVideoSpec
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Markdown == "" {
		t.Fatalf("Markdown 应非空")
	}
}

func TestGetSessionSpecNotFound(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1"}
	specs := &fakeSpecReader{err: aigcspec.ErrNotFound}
	router := docRouter(store, &fakeSkillStore{records: map[string]aigcskill.SkillRecord{}}, specs)

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/spec", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, 期望 404", rec.Code)
	}
}

func TestGetSessionSkillBound(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1", SkillID: "sk_travel"}
	skills := &fakeSkillStore{records: map[string]aigcskill.SkillRecord{
		"sk_travel": {ID: "sk_travel", Name: "人文纪录短片", Content: "<name>人文纪录短片</name>"},
	}}
	router := docRouter(store, skills, &fakeSpecReader{err: aigcspec.ErrNotFound})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/skill", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Bound   bool   `json:"bound"`
		ID      string `json:"id"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.Bound || got.ID != "sk_travel" || got.Content == "" {
		t.Fatalf("got = %+v", got)
	}
}

func TestGetSessionSkillUnbound(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1"} // SkillID == ""
	router := docRouter(store, &fakeSkillStore{records: map[string]aigcskill.SkillRecord{}}, &fakeSpecReader{err: aigcspec.ErrNotFound})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/skill", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Bound bool `json:"bound"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Bound {
		t.Fatalf("未绑应 bound=false")
	}
}
