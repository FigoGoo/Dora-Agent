package contract_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestW2R00R03R04OwnerDecisionMatricesV1 固定 R00/R03/R04 稳定决策编号及其失败关闭边界，防止计划引用再次退化为位置式待决项。
func TestW2R00R03R04OwnerDecisionMatricesV1(t *testing.T) {
	t.Parallel()

	repoRoot := findOwnerDecisionMatrixRepoRootV1(t)
	r00 := readOwnerDecisionMatrixFileV1(t, repoRoot, "docs/design/agent/w2-r00-owner-decision-matrix-v1.md")
	r03 := readOwnerDecisionMatrixFileV1(t, repoRoot, "docs/design/agent/w2-r03-owner-decision-matrix-v1.md")
	r04 := readOwnerDecisionMatrixFileV1(t, repoRoot, "docs/design/agent/w2-r04-owner-decision-matrix-v1.md")

	assertOwnerDecisionIDExactSetV1(t, r00, "R00", 14)
	assertOwnerDecisionIDExactSetV1(t, r03, "R03", 14)
	assertOwnerDecisionIDExactSetV1(t, r04, "R04", 20)
	assertOwnerDecisionMatrixFragmentsV1(t, "R00", r00, []string{
		"awaiting_owner_decision",
		"candidate_incomplete_not_ballot_ready",
		"scope_derivation_pending",
		"implementation_status=prohibited",
		"status=expansion_frozen",
		"candidate_evidence=[]",
		"当前不得生成 `DR-W2-R00-v1`",
		"W2-B0a/W2-B1` 均未解锁",
	})
	assertR00DecisionReadinessV1(t, r00)
	assertOwnerDecisionMatrixFragmentsV1(t, "R03", r03, []string{
		"decision_status=awaiting_owner_decision",
		"implementation_status=prohibited",
		"status=expansion_frozen",
		"candidate_evidence=[]",
		"R03-D01`～`R03-D14",
		"生产实现均未解锁",
	})
	assertOwnerDecisionMatrixFragmentsV1(t, "R04", r04, []string{
		"decision_status=awaiting_owner_decision",
		"implementation_status=prohibited",
		"status=expansion_frozen",
		"candidate_unactivated",
		"R04-D01`～`R04-D20",
		"生产实现均未解锁",
	})

	closure := readOwnerDecisionMatrixFileV1(t, repoRoot, "docs/design/cross-module/w2-owner-decision-closure-v1.md")
	projectPlan := readOwnerDecisionMatrixFileV1(t, repoRoot, "docs/requirements/project-development-plan.md")
	for _, fragment := range []string{
		"w2-r00-owner-decision-matrix-v1.md",
		"DR-W2-R01-v1.json",
		"w2-r03-owner-decision-matrix-v1.md",
		"w2-r04-owner-decision-matrix-v1.md",
		"R00-D01`～`R00-D14",
		"R01-D01`～`D06",
		"R03-D01`～`D14",
		"R04-D01`～`D20",
	} {
		if !strings.Contains(closure, fragment) {
			t.Fatalf("P4 收口包缺少稳定矩阵引用 %q", fragment)
		}
	}
	for _, fragment := range []string{
		"w2-r00-owner-decision-matrix-v1.md",
		"DR-W2-R01-v1.json",
		"w2-r03-owner-decision-matrix-v1.md",
		"w2-r04-owner-decision-matrix-v1.md",
		"R00-D01`～`D14",
		"R01-D01`～`D06",
		"R03-D01`～`D14",
		"R04-D01`～`D20",
		"生产实现与 Harness 继续失败关闭",
	} {
		if !strings.Contains(projectPlan, fragment) {
			t.Fatalf("Canonical 计划缺少稳定矩阵引用 %q", fragment)
		}
	}
}

// assertR00DecisionReadinessV1 固定 R00 的 open-item crosswalk 与逐项 readiness，避免为不完整候选生成接受请求。
func assertR00DecisionReadinessV1(t *testing.T, document string) {
	t.Helper()

	for _, fragment := range []string{
		"`R00-D01` | P4-C11 / Gate | `scope_derivation_pending`",
		"`R00-D05` | `BILL-OPEN-004` | `candidate_incomplete_not_ballot_ready`",
		"`R00-D06` | `BILL-OPEN-005` / P4-C11 | `awaiting_owner_decision`",
		"源契约的 Owner 在本决定被接受前继续保持“未登记、不得预填”",
		"`R00-D07` | `BILL-OPEN-005` | `candidate_incomplete_not_ballot_ready`",
		"`R00-D08` | `BILL-OPEN-006` | `candidate_incomplete_not_ballot_ready`",
		"`R00-D09` | `BILL-OPEN-007` | `candidate_incomplete_not_ballot_ready`",
		"`R00-D11` | `BILL-OPEN-009` | `candidate_incomplete_not_ballot_ready`",
		"`R00-D13` | `BILL-OPEN-011` | `candidate_incomplete_not_ballot_ready`",
		"严格待决请求不能给 `candidate_incomplete_not_ballot_ready` 项提供“接受推荐”能力",
	} {
		if !strings.Contains(document, fragment) {
			t.Fatalf("R00 Owner 决策矩阵缺少 readiness 边界 %q", fragment)
		}
	}

	pattern := regexp.MustCompile(`BILL-OPEN-[0-9]{3}`)
	seen := make(map[string]struct{})
	for _, openID := range pattern.FindAllString(document, -1) {
		seen[openID] = struct{}{}
	}
	actual := make([]string, 0, len(seen))
	for openID := range seen {
		actual = append(actual, openID)
	}
	sort.Strings(actual)
	expected := make([]string, 0, 12)
	for ordinal := 1; ordinal <= 12; ordinal++ {
		expected = append(expected, fmt.Sprintf("BILL-OPEN-%03d", ordinal))
	}
	if strings.Join(actual, ",") != strings.Join(expected, ",") {
		t.Fatalf("R00 Billing open-item exact-set=%v want=%v", actual, expected)
	}
}

// assertOwnerDecisionIDExactSetV1 验证文档出现的稳定决策 ID 恰好覆盖从 01 到指定上限的连续集合。
func assertOwnerDecisionIDExactSetV1(t *testing.T, document, gate string, count int) {
	t.Helper()

	pattern := regexp.MustCompile(regexp.QuoteMeta(gate) + `-D[0-9]{2}`)
	seen := make(map[string]struct{})
	for _, decisionID := range pattern.FindAllString(document, -1) {
		seen[decisionID] = struct{}{}
	}
	actual := make([]string, 0, len(seen))
	for decisionID := range seen {
		actual = append(actual, decisionID)
	}
	sort.Strings(actual)

	expected := make([]string, 0, count)
	for ordinal := 1; ordinal <= count; ordinal++ {
		expected = append(expected, gate+"-D"+twoDigitOwnerDecisionOrdinalV1(ordinal))
	}
	if strings.Join(actual, ",") != strings.Join(expected, ",") {
		t.Fatalf("%s stable decision exact-set 不符 actual=%v want=%v", gate, actual, expected)
	}
}

// twoDigitOwnerDecisionOrdinalV1 把小于一百的决策序号编码为稳定两位十进制文本。
func twoDigitOwnerDecisionOrdinalV1(ordinal int) string {
	return fmt.Sprintf("%02d", ordinal)
}

// assertOwnerDecisionMatrixFragmentsV1 验证矩阵保留待决、实现禁止和当前 Gate 状态等关键失败关闭表述。
func assertOwnerDecisionMatrixFragmentsV1(t *testing.T, gate, document string, fragments []string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(document, fragment) {
			t.Fatalf("%s Owner 决策矩阵缺少失败关闭片段 %q", gate, fragment)
		}
	}
}

// readOwnerDecisionMatrixFileV1 读取仓库内治理文档原始文本，读取失败时立即终止对应测试。
func readOwnerDecisionMatrixFileV1(t *testing.T, repoRoot, relativePath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("读取 %s: %v", relativePath, err)
	}
	return string(content)
}

// findOwnerDecisionMatrixRepoRootV1 从当前 Agent Module 向上定位包含 go.work 的仓库根目录。
func findOwnerDecisionMatrixRepoRootV1(t *testing.T) string {
	t.Helper()

	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("读取当前目录: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(current, "go.work")); statErr == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("无法定位仓库根目录")
		}
		current = parent
	}
}
