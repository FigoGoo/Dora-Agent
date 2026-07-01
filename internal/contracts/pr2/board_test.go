package pr2

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCreateCityTourismBoardFixture(t *testing.T) {
	var fixture struct {
		Expected struct {
			CreativeBoard CreativeBoard     `json:"creative_board"`
			Elements      []CreativeElement `json:"elements"`
		} `json:"expected"`
	}
	readFixture(t, "tests/fixtures/contracts/board/create_city_tourism_board.json", &fixture)

	if err := ValidateBoardCreation(fixture.Expected.CreativeBoard, fixture.Expected.Elements); err != nil {
		t.Fatalf("fixture violates board creation contract: %v", err)
	}
}

func TestPatchReplayStoryboardFixture(t *testing.T) {
	var fixture struct {
		InitialSnapshot  BoardSnapshot `json:"initial_snapshot"`
		Patches          []BoardPatch  `json:"patches"`
		ExpectedSnapshot BoardSnapshot `json:"expected_snapshot"`
	}
	readFixture(t, "tests/fixtures/contracts/board/patch_replay_storyboard.json", &fixture)

	if err := ValidatePatchReplay(fixture.InitialSnapshot, fixture.Patches, fixture.ExpectedSnapshot); err != nil {
		t.Fatalf("fixture violates board patch replay contract: %v", err)
	}
}

func TestApproveBoardForToolPlanFixture(t *testing.T) {
	var fixture struct {
		BoardBefore   CreativeBoard `json:"board_before"`
		ApprovalPatch BoardPatch    `json:"approval_patch"`
		BoardAfter    CreativeBoard `json:"board_after"`
	}
	readFixture(t, "tests/fixtures/contracts/board/approve_board_for_toolplan.json", &fixture)

	if err := ValidateBoardApproval(fixture.BoardBefore, fixture.ApprovalPatch, fixture.BoardAfter); err != nil {
		t.Fatalf("fixture violates board approval contract: %v", err)
	}
}

func TestBoardApprovalRejectsCombinedToolPlanShortcut(t *testing.T) {
	var fixture struct {
		BoardBefore   CreativeBoard `json:"board_before"`
		ApprovalPatch BoardPatch    `json:"approval_patch"`
		BoardAfter    CreativeBoard `json:"board_after"`
	}
	readFixture(t, "tests/fixtures/contracts/board/approve_board_for_toolplan.json", &fixture)

	fixture.ApprovalPatch.TargetVersion = fixture.ApprovalPatch.BaseVersion
	if err := ValidateBoardApproval(fixture.BoardBefore, fixture.ApprovalPatch, fixture.BoardAfter); err == nil {
		t.Fatalf("approval must require a versioned approve_board patch before PR-3 ToolPlan")
	}
}

func readFixture(t *testing.T, relativePath string, target any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
}
