package orchestration

import (
	"strings"
	"testing"
)

func TestLightTemplatesAreValid(t *testing.T) {
	if len(LightTemplates) != 4 {
		t.Fatalf("v1 ships exactly 4 light templates, got %d", len(LightTemplates))
	}
	seen := map[string]struct{}{}
	for _, template := range LightTemplates {
		if strings.TrimSpace(template.Key) == "" || strings.TrimSpace(template.Name) == "" || strings.TrimSpace(template.Description) == "" {
			t.Fatalf("template three-elements are required: %+v", template)
		}
		if _, dup := seen[template.Key]; dup {
			t.Fatalf("duplicate template key %s", template.Key)
		}
		seen[template.Key] = struct{}{}
		if err := template.Node.Validate(); err != nil {
			t.Fatalf("template %s node: %v", template.Key, err)
		}
		if template.Node.ToolKey != "generate_media" {
			t.Fatalf("v1 light template node must be generate_media, got %s", template.Node.ToolKey)
		}
		if !strings.Contains(string(template.Node.Arguments), "session_deliverable") {
			t.Fatalf("template %s must target session_deliverable", template.Key)
		}
	}
	catalog := LightTemplateCatalogText()
	for _, key := range []string{"image_creation", "video_creation", "music_creation", "audio_creation"} {
		if !strings.Contains(catalog, key) {
			t.Fatalf("catalog text must mention %s", key)
		}
	}
}
