package tools

import (
	"context"
	"encoding/json"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestPipelineToolsReturnStandardEnvelope(t *testing.T) {
	tests := []struct {
		name string
		tool interface {
			Info(context.Context) (*schema.ToolInfo, error)
			InvokableRun(context.Context, string, ...einotool.Option) (string, error)
		}
		wantName string
		args     string
	}{
		{
			name:     "resource prepare",
			tool:     ResourcePrepareAnalyzeTool{},
			wantName: ResourcePrepareAnalyzeToolKey,
			args:     `{"session_id":"s1","asset_ids":["asset-1"],"brief":"分析剧本"}`,
		},
		{
			name:     "multimodal analyze",
			tool:     MultimodalAnalyzeTool{},
			wantName: MultimodalAnalyzeToolKey,
			args:     `{"session_id":"s1","asset_ids":["asset-1"],"brief":"分析参考图"}`,
		},
		{
			name:     "write prompt",
			tool:     WritePromptTool{},
			wantName: WritePromptToolKey,
			args:     `{"session_id":"s1","target_id":"shot-1","prompt":"竹林拔剑"}`,
		},
		{
			name:     "video assembler",
			tool:     VideoAssemblerTool{},
			wantName: VideoAssemblerToolKey,
			args:     `{"session_id":"s1","storyboard_id":"storyboard-1","video_asset_ids":["asset-video-1"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := tt.tool.Info(context.Background())
			if err != nil {
				t.Fatalf("Info() error = %v", err)
			}
			if info.Name != tt.wantName {
				t.Fatalf("tool name = %q", info.Name)
			}
			out, err := tt.tool.InvokableRun(context.Background(), tt.args)
			if err != nil {
				t.Fatalf("InvokableRun() error = %v", err)
			}
			var result ToolResultEnvelope[map[string]any]
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatalf("decode result: %v", err)
			}
			if result.Status != ToolStatusOK || result.Data["session_id"] != "s1" {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}
