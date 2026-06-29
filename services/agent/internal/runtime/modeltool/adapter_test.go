package modeltool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	einoruntime "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino"
)

func TestLocalAdapterGenerate(t *testing.T) {
	adapter := LocalAdapter{}
	result, err := adapter.Generate(t.Context(), Snapshot{
		ModelID: "mdl_1", ResourceType: "image", ProviderRuntimeRef: "local:test", TimeoutMS: 1000,
	}, einoruntime.UserPrompt("make image"))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Status != "completed" || result.ArtifactCount != 1 || len(result.Artifacts) != 1 {
		t.Fatalf("unexpected local result: %#v", result)
	}
	if result.Artifacts[0].ElementType != "image_ref" || result.Artifacts[0].Checksum == "" {
		t.Fatalf("unexpected artifact: %#v", result.Artifacts[0])
	}
}

func TestLocalAdapterValidatesRuntimeInput(t *testing.T) {
	adapter := LocalAdapter{}
	_, err := adapter.Generate(t.Context(), Snapshot{ModelID: "mdl_1", ResourceType: "image"}, einoruntime.UserPrompt("make image"))
	if err == nil {
		t.Fatal("expected missing provider_runtime_ref error")
	}
	_, err = adapter.Generate(t.Context(), Snapshot{ModelID: "mdl_1", ResourceType: "image", ProviderRuntimeRef: "local:test"}, nil)
	if err == nil {
		t.Fatal("expected missing prompt error")
	}
}

func TestDeepSeekAdapterGenerateUsesChatCompletions(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_test",
			"object":  "chat.completion",
			"created": 1782751200,
			"model":   "deepseek-v4-flash",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "这是 DeepSeek V4 Flash 的真实对话输出摘要。",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 16, "total_tokens": 28},
		})
	}))
	defer server.Close()

	adapter := DeepSeekAdapter{
		BaseURL:    server.URL,
		APIKey:     "sk-test-secret",
		Model:      "deepseek-v4-flash",
		MaxTokens:  512,
		HTTPClient: server.Client(),
	}
	result, err := adapter.Generate(t.Context(), Snapshot{
		ModelID: "mdl_deepseek_v4_flash", ResourceType: "image", ProviderRuntimeRef: "deepseek:deepseek-v4-flash", TimeoutMS: 1000,
	}, einoruntime.UserPrompt("请输出一段可观察的对话结果"))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if gotAuth != "Bearer sk-test-secret" {
		t.Fatalf("unexpected auth header: %q", gotAuth)
	}
	if gotBody["model"] != "deepseek-v4-flash" {
		t.Fatalf("unexpected model body: %#v", gotBody)
	}
	if gotBody["stream"] != false {
		t.Fatalf("stream should be disabled: %#v", gotBody)
	}
	if result.Status != "completed" || result.ArtifactCount != 1 || len(result.Artifacts) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	artifact := result.Artifacts[0]
	if artifact.MetadataSummary["adapter"] != "deepseek_chat_completions" || artifact.MetadataSummary["provider"] != "deepseek" {
		t.Fatalf("unexpected metadata: %#v", artifact.MetadataSummary)
	}
	if artifact.ContentType != "image/png" || artifact.Name != "deepseek-v4-output.png" {
		t.Fatalf("unexpected image artifact: content_type=%s name=%s", artifact.ContentType, artifact.Name)
	}
	if !strings.Contains(artifact.MetadataSummary["output_preview"], "DeepSeek V4 Flash") {
		t.Fatalf("output preview missing response: %#v", artifact.MetadataSummary)
	}
	if strings.Contains(strings.Join(metadataValues(artifact.MetadataSummary), "\n"), "sk-test-secret") {
		t.Fatalf("metadata leaked api key: %#v", artifact.MetadataSummary)
	}
}

func TestDeepSeekAdapterRequiresAPIKey(t *testing.T) {
	adapter := DeepSeekAdapter{BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}
	_, err := adapter.Generate(t.Context(), Snapshot{
		ModelID: "mdl_deepseek_v4_flash", ResourceType: "image", ProviderRuntimeRef: "deepseek:deepseek-v4-flash",
	}, einoruntime.UserPrompt("hello"))
	if err == nil || !strings.Contains(err.Error(), "api key") {
		t.Fatalf("expected api key error, got %v", err)
	}
}

func TestTrimForPreviewIsRuneSafe(t *testing.T) {
	got := trimForPreview("城市香水广告", 4)
	if got != "城市香水..." {
		t.Fatalf("unexpected preview: %q", got)
	}
	if strings.Contains(got, "\uFFFD") {
		t.Fatalf("preview contains replacement rune: %q", got)
	}
}

func metadataValues(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
