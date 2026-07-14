// Package contract_test 只承载尚在评审期的跨语言契约语料校验，不提供生产 Runtime。
package contract_test

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"testing"

	"github.com/google/uuid"
)

const (
	sessionLaneCorpusSchemaVersionV1   = "session_lane_v1_corpus.v1"
	sessionLaneSnapshotSchemaVersionV1 = "session_lane_snapshot.v1"
	sessionLaneCorpusPath              = "testdata/w2_r02/session_lane_v1.json"
	sessionLaneManifestPath            = "testdata/w2_r02/manifest.json"
)

//go:embed testdata/w2_r02/*.json
var w2R02CorpusFS embed.FS

var sessionLaneBusinessDigestPatternV1 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type sessionLaneManifestV1 struct {
	SchemaVersion string                      `json:"schema_version"`
	Files         []sessionLaneManifestFileV1 `json:"files"`
}

type sessionLaneManifestFileV1 struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type sessionLaneCorpusV1 struct {
	SchemaVersion string                 `json:"schema_version"`
	InitialStates []sessionLaneStateV1   `json:"initial_states"`
	Cases         []sessionLaneCaseV1    `json:"cases"`
	ExactSets     sessionLaneExactSetsV1 `json:"exact_sets"`
}

type sessionLaneExactSetsV1 struct {
	InputStatuses []string `json:"input_statuses"`
	TurnStatuses  []string `json:"turn_statuses"`
	RunStatuses   []string `json:"run_statuses"`
	ProcessStates []string `json:"process_states"`
	CommandKinds  []string `json:"command_kinds"`
	ErrorCodes    []string `json:"error_codes"`
}

type sessionLaneStateV1 struct {
	StateID  string                `json:"state_id"`
	Snapshot sessionLaneSnapshotV1 `json:"snapshot"`
}

type sessionLaneCaseV1 struct {
	ID          string                     `json:"id"`
	FromState   string                     `json:"from_state"`
	SaveAsState string                     `json:"save_as_state"`
	Expect      string                     `json:"expect"`
	ErrorCode   string                     `json:"error_code"`
	Command     sessionLaneCommandV1       `json:"command"`
	Expected    *sessionLaneStateSummaryV1 `json:"expected,omitempty"`
}

type sessionLaneSnapshotV1 struct {
	SchemaVersion string               `json:"schema_version"`
	SessionID     string               `json:"session_id"`
	Lease         sessionLaneLeaseV1   `json:"lease"`
	Inputs        []sessionLaneInputV1 `json:"inputs"`
	Turns         []sessionLaneTurnV1  `json:"turns"`
	Runs          []sessionLaneRunV1   `json:"runs"`
}

type sessionLaneLeaseV1 struct {
	OwnerID        string `json:"owner_id"`
	LeaseUntilTick int64  `json:"lease_until_tick"`
	FenceToken     int64  `json:"fence_token"`
	Version        int64  `json:"version"`
}

type sessionLaneInputV1 struct {
	InputID             string `json:"input_id"`
	SourceType          string `json:"source_type"`
	SourceID            string `json:"source_id"`
	SourceDigest        string `json:"source_digest"`
	EnqueueSeq          int64  `json:"enqueue_seq"`
	Status              string `json:"status"`
	Attempts            int64  `json:"attempts"`
	AvailableAtTick     int64  `json:"available_at_tick"`
	Version             int64  `json:"version"`
	TurnID              string `json:"turn_id"`
	RunID               string `json:"run_id"`
	ClaimOwnerID        string `json:"claim_owner_id"`
	ClaimFence          int64  `json:"claim_fence"`
	CancelRequested     bool   `json:"cancel_requested"`
	CancelVersion       int64  `json:"cancel_version"`
	CancelCommandID     string `json:"cancel_command_id"`
	CancelRequestDigest string `json:"cancel_request_digest"`
	ResolutionCode      string `json:"resolution_code"`
}

type sessionLaneTurnV1 struct {
	TurnID   string `json:"turn_id"`
	InputID  string `json:"input_id"`
	TurnKind string `json:"turn_kind"`
	Status   string `json:"status"`
	Version  int64  `json:"version"`
}

type sessionLaneRunV1 struct {
	RunID              string `json:"run_id"`
	InputID            string `json:"input_id"`
	TurnID             string `json:"turn_id"`
	Status             string `json:"status"`
	Version            int64  `json:"version"`
	OwnerFence         int64  `json:"owner_fence"`
	RecoveredFromFence int64  `json:"recovered_from_fence"`
	EffectState        string `json:"effect_state"`
}

type sessionLaneCommandV1 struct {
	Kind                  string `json:"kind"`
	ProcessState          string `json:"process_state"`
	Trigger               string `json:"trigger"`
	NowTick               int64  `json:"now_tick"`
	OwnerID               string `json:"owner_id"`
	FenceToken            int64  `json:"fence_token"`
	LeaseDurationTicks    int64  `json:"lease_duration_ticks"`
	ExpectedLeaseVersion  int64  `json:"expected_lease_version"`
	InputID               string `json:"input_id"`
	ExpectedInputVersion  int64  `json:"expected_input_version"`
	ExpectedTurnVersion   int64  `json:"expected_turn_version"`
	ExpectedRunVersion    int64  `json:"expected_run_version"`
	ExpectedCancelVersion int64  `json:"expected_cancel_version"`
	CommandID             string `json:"command_id"`
	RequestDigest         string `json:"request_digest"`
	RunID                 string `json:"run_id"`
	RetryAtTick           int64  `json:"retry_at_tick"`
	EvidenceKind          string `json:"evidence_kind"`
	EvidenceDigest        string `json:"evidence_digest"`
	ReconciliationOutcome string `json:"reconciliation_outcome"`
	ResolutionCode        string `json:"resolution_code"`
	TerminalTarget        string `json:"terminal_target"`
}

type sessionLaneStateSummaryV1 struct {
	LeaseOwnerID       string `json:"lease_owner_id"`
	LeaseUntilTick     int64  `json:"lease_until_tick"`
	LeaseFence         int64  `json:"lease_fence"`
	LeaseVersion       int64  `json:"lease_version"`
	HeadInputID        string `json:"head_input_id"`
	HeadStatus         string `json:"head_status"`
	InputID            string `json:"input_id"`
	InputStatus        string `json:"input_status"`
	InputVersion       int64  `json:"input_version"`
	InputAttempts      int64  `json:"input_attempts"`
	InputClaimOwnerID  string `json:"input_claim_owner_id"`
	InputClaimFence    int64  `json:"input_claim_fence"`
	TurnID             string `json:"turn_id"`
	TurnStatus         string `json:"turn_status"`
	TurnVersion        int64  `json:"turn_version"`
	RunID              string `json:"run_id"`
	RunStatus          string `json:"run_status"`
	RunVersion         int64  `json:"run_version"`
	RunOwnerFence      int64  `json:"run_owner_fence"`
	RecoveredFromFence int64  `json:"recovered_from_fence"`
	RunEffectState     string `json:"run_effect_state"`
	CancelRequested    bool   `json:"cancel_requested"`
	CancelVersion      int64  `json:"cancel_version"`
	ResolutionCode     string `json:"resolution_code"`
}

func TestW2R02CorpusManifest(t *testing.T) {
	entries, err := w2R02CorpusFS.ReadDir("testdata/w2_r02")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name() != "manifest.json" || entries[1].Name() != "session_lane_v1.json" {
		t.Fatalf("W2-R02 Corpus 出现未登记文件或文件缺失: %v", entries)
	}
	raw, err := w2R02CorpusFS.ReadFile(sessionLaneManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("manifest JSON 非法: %v", err)
	}
	var manifest sessionLaneManifestV1
	if err := strictDecode(raw, &manifest); err != nil {
		t.Fatalf("解析 manifest: %v", err)
	}
	if manifest.SchemaVersion != "w2_r02_contract_corpus_manifest.v1" || len(manifest.Files) != 1 {
		t.Fatalf("manifest 版本或文件数错误: %+v", manifest)
	}
	item := manifest.Files[0]
	if item.File != "session_lane_v1.json" || !digestPattern.MatchString(item.SHA256) {
		t.Fatalf("manifest 文件或摘要格式错误: %+v", item)
	}
	content, err := w2R02CorpusFS.ReadFile("testdata/w2_r02/" + item.File)
	if err != nil {
		t.Fatal(err)
	}
	actual := sha256.Sum256(content)
	if got := "sha256:" + hex.EncodeToString(actual[:]); got != item.SHA256 {
		t.Fatalf("%s raw digest=%s want=%s", item.File, got, item.SHA256)
	}
}

func TestSessionLaneV1Corpus(t *testing.T) {
	corpus := loadSessionLaneCorpusV1(t)
	assertSessionLaneExactSetsV1(t, corpus.ExactSets)
	states := make(map[string]sessionLaneSnapshotV1, len(corpus.InitialStates)+len(corpus.Cases))
	for _, fixture := range corpus.InitialStates {
		if _, exists := states[fixture.StateID]; exists {
			t.Fatalf("重复 initial state %q", fixture.StateID)
		}
		if err := validateSessionLaneSnapshotV1(fixture.Snapshot); err != nil {
			t.Fatalf("初始状态 %s 非法: %v", fixture.StateID, err)
		}
		states[fixture.StateID] = fixture.Snapshot
	}

	requiredCases := []string{
		"SL-P01-claim-head", "SL-P02-start-run", "SL-P03-heartbeat-keeps-fence",
		"SL-P04-safe-retry-releases-lease", "SL-P05-due-reclaim-same-identities",
		"SL-P06-resume-run", "SL-P07-unknown-quarantines", "SL-P08-authorized-recovery-claim",
		"SL-P09-resume-after-quarantine", "SL-P10-resolve-frozen-result", "SL-P11-next-head-after-terminal",
		"SL-P12-expired-takeover", "SL-P13-takeover-resume-same-identities",
		"SL-P14-request-cancel-does-not-release", "SL-P15-commit-cancel-releases",
		"SL-P16-claim-next-after-cancel", "SL-P17-postgres-scan-claim-trigger",
		"SL-P18-heartbeat-during-drain", "SL-P19-graceful-handoff", "SL-P20-new-owner-after-handoff",
		"SL-P21-retry-unknown-quarantines", "SL-P22-cancel-request-replay",
		"SL-P23-pending-cancel-request", "SL-P24-claim-pending-cancel", "SL-P25-commit-pending-cancel",
		"SL-P26-authorized-effect-committed", "SL-P27-takeover-preserves-resolved-effect",
		"SL-P28-finalize-reconciled-effect", "SL-P29-claimed-takeover-preserves-not-started",
		"SL-N01-later-input-cannot-pass", "SL-N02-retry-head-not-due", "SL-N03-quarantine-needs-authority",
		"SL-N04-old-owner-heartbeat-stale", "SL-N05-old-owner-terminal-stale",
		"SL-N06-cancel-request-keeps-hol", "SL-N07-draining-stops-claim",
		"SL-N08-live-lease-blocks-other-owner", "SL-N09-heartbeat-version-conflict",
		"SL-N10-start-input-version-conflict", "SL-N11-retry-needs-proof",
		"SL-N12-quarantine-needs-unknown-proof", "SL-N13-terminal-needs-frozen-evidence",
		"SL-N14-handoff-needs-durable-evidence", "SL-N15-takeover-before-expiry",
		"SL-N16-forged-higher-fence", "SL-N17-cancel-commit-needs-request", "SL-N18-cancel-request-conflict",
		"SL-N19-stopped-cannot-heartbeat", "SL-N20-owner-write-at-expiry",
		"SL-N21-takeover-input-version-conflict", "SL-N22-takeover-turn-version-conflict",
		"SL-N23-takeover-run-version-conflict", "SL-N24-manual-recovery-cannot-clear-unknown",
		"SL-N25-heartbeat-must-bind-head", "SL-N26-heartbeat-cannot-shorten",
		"SL-N27-expired-held-lease-requires-takeover", "SL-N28-resolved-effect-cannot-quarantine",
		"SL-N29-unknown-effect-needs-authoritative-safe-point", "SL-N30-cancel-target-run-conflict",
		"SL-N31-cancel-version-conflict",
	}
	seen := make(map[string]struct{}, len(corpus.Cases))
	for _, fixture := range corpus.Cases {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			if _, exists := seen[fixture.ID]; exists {
				t.Fatalf("重复 case id %q", fixture.ID)
			}
			seen[fixture.ID] = struct{}{}
			before, exists := states[fixture.FromState]
			if !exists {
				t.Fatalf("未知 from_state %q", fixture.FromState)
			}
			after, err := applySessionLaneCommandV1(before, fixture.Command)
			if fixture.Expect == "reject" {
				if err == nil || errorCode(err) != fixture.ErrorCode {
					t.Fatalf("期望拒绝 code=%s，实际 err=%v code=%s", fixture.ErrorCode, err, errorCode(err))
				}
				if fixture.SaveAsState != "" || fixture.Expected != nil {
					t.Fatal("拒绝向量不得保存状态或声明 expected")
				}
				if !reflect.DeepEqual(after, before) {
					t.Fatal("拒绝命令不得改变任何 Snapshot 状态")
				}
				return
			}
			if fixture.Expect != "accept" || fixture.ErrorCode != "" || err != nil || fixture.SaveAsState == "" || fixture.Expected == nil {
				t.Fatalf("合法 case 元数据或结果错误: expect=%q code=%q save=%q expected=%v err=%v", fixture.Expect, fixture.ErrorCode, fixture.SaveAsState, fixture.Expected != nil, err)
			}
			if err := validateSessionLaneSnapshotV1(after); err != nil {
				t.Fatalf("迁移后状态非法: %v", err)
			}
			assertSessionLaneMutationScopeV1(t, before, after, fixture.Command.InputID)
			got, summaryErr := summarizeSessionLaneStateV1(after, fixture.Expected.InputID)
			if summaryErr != nil {
				t.Fatal(summaryErr)
			}
			if got != *fixture.Expected {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(fixture.Expected, "", "  ")
				t.Fatalf("状态摘要不符\ngot=%s\nwant=%s", gotJSON, wantJSON)
			}
			if _, exists := states[fixture.SaveAsState]; exists {
				t.Fatalf("save_as_state %q 不得覆盖已有状态", fixture.SaveAsState)
			}
			states[fixture.SaveAsState] = after
		})
	}
	if len(seen) != len(requiredCases) {
		t.Fatalf("case exact-set 数量=%d want=%d", len(seen), len(requiredCases))
	}
	for _, id := range requiredCases {
		if _, exists := seen[id]; !exists {
			t.Fatalf("缺少固定 case %s", id)
		}
	}
}

func TestSessionLaneV1SnapshotRejectsCrossInputRunReference(t *testing.T) {
	corpus := loadSessionLaneCorpusV1(t)
	snapshot := corpus.InitialStates[0].Snapshot
	for _, caseID := range []string{"SL-P01-claim-head", "SL-P02-start-run"} {
		var command sessionLaneCommandV1
		for _, fixture := range corpus.Cases {
			if fixture.ID == caseID {
				command = fixture.Command
				break
			}
		}
		var err error
		snapshot, err = applySessionLaneCommandV1(snapshot, command)
		if err != nil {
			t.Fatalf("准备 %s 状态: %v", caseID, err)
		}
	}
	snapshot.Inputs[1].RunID = snapshot.Inputs[0].RunID
	if err := validateSessionLaneSnapshotV1(snapshot); err == nil {
		t.Fatal("validator 必须拒绝跨 Input/Turn 的 Run 引用")
	}
	before := cloneSessionLaneSnapshotV1(snapshot)
	after, err := applySessionLaneCommandV1(snapshot, sessionLaneCommandV1{})
	if errorCode(err) != "SESSION_LANE_INVARIANT_VIOLATION" || !reflect.DeepEqual(after, before) {
		t.Fatalf("非法 Snapshot 命令必须原样拒绝: err=%v", err)
	}
}

func TestSessionLaneV1SnapshotRejectsResolvedRetryWaitEffect(t *testing.T) {
	corpus := loadSessionLaneCorpusV1(t)
	snapshot := corpus.InitialStates[0].Snapshot
	for _, caseID := range []string{"SL-P01-claim-head", "SL-P02-start-run", "SL-P03-heartbeat-keeps-fence", "SL-P04-safe-retry-releases-lease"} {
		var command sessionLaneCommandV1
		for _, fixture := range corpus.Cases {
			if fixture.ID == caseID {
				command = fixture.Command
				break
			}
		}
		var err error
		snapshot, err = applySessionLaneCommandV1(snapshot, command)
		if err != nil {
			t.Fatalf("准备 %s 状态: %v", caseID, err)
		}
	}
	snapshot.Runs[0].EffectState = "resolved"
	if err := validateSessionLaneSnapshotV1(snapshot); err == nil {
		t.Fatal("validator 必须拒绝 retry_wait + resolved effect")
	}
	before := cloneSessionLaneSnapshotV1(snapshot)
	after, err := applySessionLaneCommandV1(snapshot, sessionLaneCommandV1{})
	if errorCode(err) != "SESSION_LANE_INVARIANT_VIOLATION" || !reflect.DeepEqual(after, before) {
		t.Fatalf("非法 retry_wait Snapshot 命令必须原样拒绝: err=%v", err)
	}
}

func loadSessionLaneCorpusV1(t *testing.T) sessionLaneCorpusV1 {
	t.Helper()
	raw, err := w2R02CorpusFS.ReadFile(sessionLaneCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("Session Lane Corpus JSON 非法: %v", err)
	}
	var corpus sessionLaneCorpusV1
	if err := strictDecode(raw, &corpus); err != nil {
		t.Fatalf("解析 Session Lane Corpus: %v", err)
	}
	if corpus.SchemaVersion != sessionLaneCorpusSchemaVersionV1 {
		t.Fatalf("Corpus schema_version=%q", corpus.SchemaVersion)
	}
	return corpus
}

func assertSessionLaneExactSetsV1(t *testing.T, got sessionLaneExactSetsV1) {
	t.Helper()
	assertStrings := func(name string, actual, expected []string) {
		t.Helper()
		if fmt.Sprint(actual) != fmt.Sprint(expected) {
			t.Fatalf("%s exact-set=%v want=%v", name, actual, expected)
		}
	}
	assertStrings("input_statuses", got.InputStatuses, []string{"pending", "claimed", "running", "retry_wait", "quarantined", "resolved", "dead"})
	assertStrings("turn_statuses", got.TurnStatuses, []string{"created", "running", "completed", "failed", "cancelled"})
	assertStrings("run_statuses", got.RunStatuses, []string{"created", "running", "recovery_pending", "completed", "failed", "cancelled"})
	assertStrings("process_states", got.ProcessStates, []string{"accepting", "draining", "stopped"})
	assertStrings("command_kinds", got.CommandKinds, []string{"claim_head", "start_run", "renew_lease", "mark_retry_wait", "quarantine_unknown", "takeover_expired_lease", "record_cancel_request", "commit_cancellation", "finalize_result", "prepare_drain_handoff"})
	assertStrings("error_codes", got.ErrorCodes, []string{"SESSION_LANE_DRAINING", "SESSION_LEASE_VERSION_CONFLICT", "SESSION_INPUT_NOT_HEAD", "SESSION_INPUT_STATE_CONFLICT", "SESSION_LANE_HEAD_NOT_DUE", "SESSION_LANE_LEASE_HELD", "RECOVERY_EVIDENCE_REQUIRED", "UNKNOWN_OUTCOME_UNRESOLVED", "STALE_FENCE", "SESSION_LEASE_EXPIRED", "SESSION_INPUT_VERSION_CONFLICT", "TURN_STATE_CONFLICT", "RUN_STATE_CONFLICT", "TERMINAL_EVIDENCE_REQUIRED", "CANCEL_REQUEST_REQUIRED", "CANCEL_REQUEST_CONFLICT", "SESSION_LANE_INVARIANT_VIOLATION"})
}

func applySessionLaneCommandV1(before sessionLaneSnapshotV1, command sessionLaneCommandV1) (sessionLaneSnapshotV1, error) {
	after := cloneSessionLaneSnapshotV1(before)
	if err := validateSessionLaneSnapshotV1(after); err != nil {
		return before, reject("SESSION_LANE_INVARIANT_VIOLATION", err.Error())
	}
	inputIndex := findSessionLaneInputV1(after.Inputs, command.InputID)
	if inputIndex < 0 {
		return before, reject("SESSION_INPUT_STATE_CONFLICT", "input_id")
	}
	input := &after.Inputs[inputIndex]

	switch command.Kind {
	case "claim_head":
		if command.ProcessState == "draining" || command.ProcessState == "stopped" {
			return before, reject("SESSION_LANE_DRAINING", "process_state")
		}
		if command.ProcessState != "accepting" || !validClaimTriggerV1(command.Trigger) || command.OwnerID == "" || command.LeaseDurationTicks <= 0 {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "claim command")
		}
		if command.ExpectedLeaseVersion != after.Lease.Version {
			return before, reject("SESSION_LEASE_VERSION_CONFLICT", "lease.version")
		}
		head := sessionLaneHeadV1(after.Inputs)
		if head == nil || head.InputID != command.InputID {
			return before, reject("SESSION_INPUT_NOT_HEAD", "input_id")
		}
		previousStatus := input.Status
		cancelClaim := input.CancelRequested && command.EvidenceKind == "cancel_request" && sessionLaneBusinessDigestPatternV1.MatchString(command.EvidenceDigest)
		if input.CancelRequested && !cancelClaim {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "cancel-requested head requires cancel claim")
		}
		if input.Status == "retry_wait" && command.NowTick < input.AvailableAtTick && !cancelClaim {
			return before, reject("SESSION_LANE_HEAD_NOT_DUE", "available_at_tick")
		}
		if input.Status == "quarantined" && !validRecoveryEvidenceV1(command) {
			return before, reject("RECOVERY_EVIDENCE_REQUIRED", "evidence_kind")
		}
		if after.Lease.OwnerID != "" && command.NowTick < after.Lease.LeaseUntilTick {
			return before, reject("SESSION_LANE_LEASE_HELD", "lease_until_tick")
		}
		if after.Lease.OwnerID != "" {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "expired held lease requires takeover")
		}
		if input.Status == "claimed" && (command.EvidenceKind != "durable_handoff" || !sessionLaneBusinessDigestPatternV1.MatchString(command.EvidenceDigest)) {
			return before, reject("RECOVERY_EVIDENCE_REQUIRED", "handoff evidence")
		}
		if input.Status != "pending" && input.Status != "retry_wait" && input.Status != "quarantined" && input.Status != "claimed" {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "input.status")
		}
		if command.ExpectedInputVersion != input.Version {
			return before, reject("SESSION_INPUT_VERSION_CONFLICT", "input.version")
		}
		turn := findSessionLaneTurnPtrV1(&after, input.TurnID)
		if turn == nil || command.ExpectedTurnVersion != turn.Version {
			return before, reject("TURN_STATE_CONFLICT", "turn.version")
		}
		if input.RunID != "" {
			run := findSessionLaneRunPtrV1(&after, input.RunID)
			if run == nil || command.ExpectedRunVersion != run.Version {
				return before, reject("RUN_STATE_CONFLICT", "run.version")
			}
		} else if command.ExpectedRunVersion != 0 {
			return before, reject("RUN_STATE_CONFLICT", "run.version")
		}
		oldFence := after.Lease.FenceToken
		after.Lease.OwnerID = command.OwnerID
		after.Lease.LeaseUntilTick = command.NowTick + command.LeaseDurationTicks
		after.Lease.FenceToken++
		after.Lease.Version++
		input.Status = "claimed"
		input.Attempts++
		input.Version++
		input.ClaimOwnerID = command.OwnerID
		input.ClaimFence = after.Lease.FenceToken
		if input.TurnID != "" && input.RunID == "" && !cancelClaim {
			if _, err := parseCanonicalUUIDv7(command.RunID); err != nil {
				return before, reject("RUN_STATE_CONFLICT", "run_id")
			}
			input.RunID = command.RunID
			after.Runs = append(after.Runs, sessionLaneRunV1{
				RunID: command.RunID, InputID: input.InputID, TurnID: input.TurnID,
				Status: "created", Version: 1, OwnerFence: after.Lease.FenceToken, EffectState: "not_started",
			})
		} else if input.RunID != "" {
			run := findSessionLaneRunPtrV1(&after, input.RunID)
			if run == nil || (run.Status != "recovery_pending" && run.Status != "created") {
				return before, reject("RUN_STATE_CONFLICT", "run.status")
			}
			run.OwnerFence = after.Lease.FenceToken
			run.RecoveredFromFence = oldFence
			if previousStatus == "quarantined" && command.ReconciliationOutcome == "effect_not_started" {
				run.EffectState = "not_started"
			} else if previousStatus == "quarantined" && command.ReconciliationOutcome == "effect_committed" {
				run.EffectState = "resolved"
			}
			run.Version++
		}
	case "start_run":
		if err := guardSessionLaneOwnerV1(after, command); err != nil {
			return before, err
		}
		if command.ExpectedInputVersion != input.Version {
			return before, reject("SESSION_INPUT_VERSION_CONFLICT", "input.version")
		}
		if input.Status != "claimed" {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "input.status")
		}
		if input.CancelRequested {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "cancelled input cannot start")
		}
		turn := findSessionLaneTurnPtrV1(&after, input.TurnID)
		run := findSessionLaneRunPtrV1(&after, input.RunID)
		if turn == nil || command.ExpectedTurnVersion != turn.Version || (turn.Status != "created" && turn.Status != "running") {
			return before, reject("TURN_STATE_CONFLICT", "turn")
		}
		if run == nil || command.ExpectedRunVersion != run.Version || (run.Status != "created" && run.Status != "recovery_pending") {
			return before, reject("RUN_STATE_CONFLICT", "run")
		}
		if run.Status == "recovery_pending" && command.EvidenceKind != "recovery_safe_point" && command.EvidenceKind != "authoritative_no_effect_safe_point" {
			return before, reject("RECOVERY_EVIDENCE_REQUIRED", "run resume evidence")
		}
		if run.Status == "recovery_pending" && !sessionLaneBusinessDigestPatternV1.MatchString(command.EvidenceDigest) {
			return before, reject("RECOVERY_EVIDENCE_REQUIRED", "run resume digest")
		}
		if run.EffectState == "unknown" {
			if command.EvidenceKind != "authoritative_no_effect_safe_point" {
				return before, reject("UNKNOWN_OUTCOME_UNRESOLVED", "run.effect_state")
			}
			run.EffectState = "not_started"
		}
		if run.EffectState == "resolved" {
			return before, reject("RUN_STATE_CONFLICT", "resolved effect must finalize")
		}
		input.Status = "running"
		input.Version++
		if turn.Status == "created" {
			turn.Status = "running"
			turn.Version++
		}
		run.Status = "running"
		run.OwnerFence = after.Lease.FenceToken
		run.Version++
	case "renew_lease":
		if err := guardSessionLaneOwnerV1(after, command); err != nil {
			return before, err
		}
		if command.LeaseDurationTicks <= 0 {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "lease_duration_ticks")
		}
		newLeaseUntil := command.NowTick + command.LeaseDurationTicks
		if newLeaseUntil <= after.Lease.LeaseUntilTick {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "heartbeat must extend lease")
		}
		after.Lease.LeaseUntilTick = newLeaseUntil
		after.Lease.Version++
	case "mark_retry_wait":
		if err := guardSessionLaneOwnerV1(after, command); err != nil {
			return before, err
		}
		if command.ExpectedInputVersion != input.Version {
			return before, reject("SESSION_INPUT_VERSION_CONFLICT", "input.version")
		}
		if input.Status != "claimed" && input.Status != "running" {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "input.status")
		}
		run := findSessionLaneRunPtrV1(&after, input.RunID)
		if run == nil || command.ExpectedRunVersion != run.Version || (run.Status != "created" && run.Status != "running") {
			return before, reject("RUN_STATE_CONFLICT", "run")
		}
		if run.EffectState == "unknown" {
			return before, reject("UNKNOWN_OUTCOME_UNRESOLVED", "run.effect_state")
		}
		if run.EffectState == "resolved" {
			return before, reject("RUN_STATE_CONFLICT", "resolved effect cannot retry")
		}
		if command.EvidenceKind != "no_side_effect" || !sessionLaneBusinessDigestPatternV1.MatchString(command.EvidenceDigest) || command.RetryAtTick <= command.NowTick {
			return before, reject("RECOVERY_EVIDENCE_REQUIRED", "retry evidence")
		}
		input.Status = "retry_wait"
		input.AvailableAtTick = command.RetryAtTick
		input.Version++
		input.ClaimOwnerID = ""
		run.Status = "recovery_pending"
		run.Version++
		releaseSessionLaneLeaseV1(&after)
	case "quarantine_unknown":
		if input.Status == "retry_wait" {
			if command.ProcessState != "accepting" || after.Lease.OwnerID != "" || command.ExpectedLeaseVersion != after.Lease.Version {
				return before, reject("SESSION_LEASE_VERSION_CONFLICT", "idle lease")
			}
			head := sessionLaneHeadV1(after.Inputs)
			if head == nil || head.InputID != input.InputID {
				return before, reject("SESSION_INPUT_NOT_HEAD", "input_id")
			}
			if command.ExpectedInputVersion != input.Version {
				return before, reject("SESSION_INPUT_VERSION_CONFLICT", "input.version")
			}
			run := findSessionLaneRunPtrV1(&after, input.RunID)
			if run == nil || command.ExpectedRunVersion != run.Version || run.Status != "recovery_pending" {
				return before, reject("RUN_STATE_CONFLICT", "run")
			}
			if run.EffectState == "resolved" {
				return before, reject("RUN_STATE_CONFLICT", "resolved effect cannot quarantine")
			}
			if command.EvidenceKind != "unknown_outcome" || !sessionLaneBusinessDigestPatternV1.MatchString(command.EvidenceDigest) {
				return before, reject("RECOVERY_EVIDENCE_REQUIRED", "unknown evidence")
			}
			input.Status = "quarantined"
			input.Version++
			run.EffectState = "unknown"
			run.Version++
			break
		}
		if err := guardSessionLaneOwnerV1(after, command); err != nil {
			return before, err
		}
		if command.ExpectedInputVersion != input.Version {
			return before, reject("SESSION_INPUT_VERSION_CONFLICT", "input.version")
		}
		if input.Status != "claimed" && input.Status != "running" {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "input.status")
		}
		run := findSessionLaneRunPtrV1(&after, input.RunID)
		if run == nil || command.ExpectedRunVersion != run.Version || (run.Status != "created" && run.Status != "running" && run.Status != "recovery_pending") {
			return before, reject("RUN_STATE_CONFLICT", "run")
		}
		if run.EffectState == "resolved" {
			return before, reject("RUN_STATE_CONFLICT", "resolved effect cannot quarantine")
		}
		if command.EvidenceKind != "unknown_outcome" || !sessionLaneBusinessDigestPatternV1.MatchString(command.EvidenceDigest) {
			return before, reject("RECOVERY_EVIDENCE_REQUIRED", "unknown evidence")
		}
		input.Status = "quarantined"
		input.Version++
		input.ClaimOwnerID = ""
		runChanged := false
		if run.Status != "recovery_pending" {
			run.Status = "recovery_pending"
			runChanged = true
		}
		if run.EffectState != "unknown" {
			run.EffectState = "unknown"
			runChanged = true
		}
		if runChanged {
			run.Version++
		}
		releaseSessionLaneLeaseV1(&after)
	case "takeover_expired_lease":
		if command.ProcessState != "accepting" {
			return before, reject("SESSION_LANE_DRAINING", "takeover requires accepting")
		}
		if command.Trigger != "recovery_scan" || command.OwnerID == "" || command.LeaseDurationTicks <= 0 {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "takeover command")
		}
		if command.ExpectedLeaseVersion != after.Lease.Version {
			return before, reject("SESSION_LEASE_VERSION_CONFLICT", "lease.version")
		}
		if after.Lease.OwnerID == "" || command.NowTick < after.Lease.LeaseUntilTick {
			return before, reject("SESSION_LANE_LEASE_HELD", "lease not expired")
		}
		head := sessionLaneHeadV1(after.Inputs)
		if head == nil || head.InputID != command.InputID {
			return before, reject("SESSION_INPUT_NOT_HEAD", "input_id")
		}
		if input.Status != "claimed" && input.Status != "running" {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "input.status")
		}
		if command.ExpectedInputVersion != input.Version {
			return before, reject("SESSION_INPUT_VERSION_CONFLICT", "input.version")
		}
		turn := findSessionLaneTurnPtrV1(&after, input.TurnID)
		if turn == nil || command.ExpectedTurnVersion != turn.Version {
			return before, reject("TURN_STATE_CONFLICT", "turn.version")
		}
		if input.RunID != "" {
			run := findSessionLaneRunPtrV1(&after, input.RunID)
			if run == nil || command.ExpectedRunVersion != run.Version {
				return before, reject("RUN_STATE_CONFLICT", "run.version")
			}
		}
		previousStatus := input.Status
		oldFence := after.Lease.FenceToken
		after.Lease.OwnerID = command.OwnerID
		after.Lease.LeaseUntilTick = command.NowTick + command.LeaseDurationTicks
		after.Lease.FenceToken++
		after.Lease.Version++
		input.Status = "claimed"
		input.Attempts++
		input.Version++
		input.ClaimOwnerID = command.OwnerID
		input.ClaimFence = after.Lease.FenceToken
		if input.RunID != "" {
			run := findSessionLaneRunPtrV1(&after, input.RunID)
			if run == nil || (run.Status != "created" && run.Status != "running" && run.Status != "recovery_pending") {
				return before, reject("RUN_STATE_CONFLICT", "run.status")
			}
			if run.Status != "recovery_pending" {
				run.Status = "recovery_pending"
			}
			if previousStatus == "running" && run.EffectState != "resolved" {
				run.EffectState = "unknown"
			}
			run.OwnerFence = after.Lease.FenceToken
			run.RecoveredFromFence = oldFence
			run.Version++
		}
	case "record_cancel_request":
		if _, err := parseCanonicalUUIDv7(command.CommandID); err != nil || !sessionLaneBusinessDigestPatternV1.MatchString(command.RequestDigest) {
			return before, reject("CANCEL_REQUEST_CONFLICT", "cancel command identity")
		}
		if command.RunID != input.RunID {
			return before, reject("CANCEL_REQUEST_CONFLICT", "cancel target run")
		}
		if input.CancelCommandID != "" {
			if input.CancelCommandID == command.CommandID && input.CancelRequestDigest == command.RequestDigest {
				return after, nil
			}
			return before, reject("CANCEL_REQUEST_CONFLICT", "cancel command first-write-wins")
		}
		if command.ExpectedInputVersion != input.Version {
			return before, reject("SESSION_INPUT_VERSION_CONFLICT", "input.version")
		}
		if command.ExpectedCancelVersion != input.CancelVersion {
			return before, reject("CANCEL_REQUEST_CONFLICT", "cancel.version")
		}
		if isTerminalInputStatusV1(input.Status) {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "input.status")
		}
		input.CancelRequested = true
		input.CancelVersion++
		input.CancelCommandID = command.CommandID
		input.CancelRequestDigest = command.RequestDigest
		input.Version++
	case "commit_cancellation":
		if err := guardSessionLaneOwnerV1(after, command); err != nil {
			return before, err
		}
		if command.ExpectedInputVersion != input.Version {
			return before, reject("SESSION_INPUT_VERSION_CONFLICT", "input.version")
		}
		if command.ExpectedCancelVersion != input.CancelVersion {
			return before, reject("CANCEL_REQUEST_CONFLICT", "cancel.version")
		}
		if command.RunID != input.RunID {
			return before, reject("CANCEL_REQUEST_CONFLICT", "cancel target run")
		}
		if !input.CancelRequested {
			return before, reject("CANCEL_REQUEST_REQUIRED", "cancel_requested")
		}
		if command.EvidenceKind != "cancellation_committed" || !sessionLaneBusinessDigestPatternV1.MatchString(command.EvidenceDigest) {
			return before, reject("TERMINAL_EVIDENCE_REQUIRED", "cancellation evidence")
		}
		if input.RunID == "" {
			turn := findSessionLaneTurnPtrV1(&after, input.TurnID)
			if input.Status != "claimed" || turn == nil || command.ExpectedTurnVersion != turn.Version || turn.Status != "created" || command.ExpectedRunVersion != 0 {
				return before, reject("TURN_STATE_CONFLICT", "pre-run cancellation")
			}
			input.Status = "resolved"
			input.Version++
			input.ClaimOwnerID = ""
			input.ResolutionCode = command.ResolutionCode
			turn.Status = "cancelled"
			turn.Version++
			releaseSessionLaneLeaseV1(&after)
			break
		}
		if err := finalizeSessionLaneV1(&after, input, command, "resolved", "cancelled"); err != nil {
			return before, err
		}
	case "finalize_result":
		if err := guardSessionLaneOwnerV1(after, command); err != nil {
			return before, err
		}
		if command.ExpectedInputVersion != input.Version {
			return before, reject("SESSION_INPUT_VERSION_CONFLICT", "input.version")
		}
		if command.EvidenceKind != "frozen_receipt_projection_marker" || !sessionLaneBusinessDigestPatternV1.MatchString(command.EvidenceDigest) {
			return before, reject("TERMINAL_EVIDENCE_REQUIRED", "result evidence")
		}
		target := command.TerminalTarget
		if target == "" {
			target = "resolved"
		}
		turnRunStatus := "completed"
		if target == "dead" {
			if command.ResolutionCode != "runner_invariant_failed" {
				return before, reject("TERMINAL_EVIDENCE_REQUIRED", "failure resolution")
			}
			turnRunStatus = "failed"
		} else if target != "resolved" {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "terminal_target")
		}
		if err := finalizeSessionLaneV1(&after, input, command, target, turnRunStatus); err != nil {
			return before, err
		}
	case "prepare_drain_handoff":
		if command.ProcessState != "draining" {
			return before, reject("SESSION_LANE_DRAINING", "handoff requires draining")
		}
		if err := guardSessionLaneOwnerV1(after, command); err != nil {
			return before, err
		}
		if command.ExpectedInputVersion != input.Version {
			return before, reject("SESSION_INPUT_VERSION_CONFLICT", "input.version")
		}
		if command.EvidenceKind != "durable_handoff_receipt" || !sessionLaneBusinessDigestPatternV1.MatchString(command.EvidenceDigest) {
			return before, reject("RECOVERY_EVIDENCE_REQUIRED", "handoff evidence")
		}
		turn := findSessionLaneTurnPtrV1(&after, input.TurnID)
		if turn == nil || command.ExpectedTurnVersion != turn.Version || (turn.Status != "created" && turn.Status != "running") {
			return before, reject("TURN_STATE_CONFLICT", "turn")
		}
		run := findSessionLaneRunPtrV1(&after, input.RunID)
		if input.Status != "claimed" && input.Status != "running" {
			return before, reject("SESSION_INPUT_STATE_CONFLICT", "input.status")
		}
		if run == nil || command.ExpectedRunVersion != run.Version || (run.Status != "created" && run.Status != "running") {
			return before, reject("RUN_STATE_CONFLICT", "run")
		}
		if run.EffectState == "unknown" {
			return before, reject("UNKNOWN_OUTCOME_UNRESOLVED", "run.effect_state")
		}
		input.Status = "claimed"
		input.ClaimOwnerID = ""
		input.Version++
		run.Status = "recovery_pending"
		run.Version++
		releaseSessionLaneLeaseV1(&after)
	default:
		return before, reject("SESSION_INPUT_STATE_CONFLICT", "command.kind")
	}
	return after, nil
}

func guardSessionLaneOwnerV1(snapshot sessionLaneSnapshotV1, command sessionLaneCommandV1) error {
	if command.ProcessState != "accepting" && command.ProcessState != "draining" {
		return reject("SESSION_LANE_DRAINING", "process_state")
	}
	if command.OwnerID == "" || snapshot.Lease.OwnerID != command.OwnerID || snapshot.Lease.FenceToken != command.FenceToken {
		return reject("STALE_FENCE", "lease owner/fence")
	}
	if command.ExpectedLeaseVersion != snapshot.Lease.Version {
		return reject("SESSION_LEASE_VERSION_CONFLICT", "lease.version")
	}
	if command.NowTick >= snapshot.Lease.LeaseUntilTick {
		return reject("SESSION_LEASE_EXPIRED", "lease_until_tick")
	}
	head := sessionLaneHeadV1(snapshot.Inputs)
	if head == nil || head.InputID != command.InputID {
		return reject("SESSION_INPUT_NOT_HEAD", "input_id")
	}
	if head.ClaimOwnerID != command.OwnerID || head.ClaimFence != command.FenceToken {
		return reject("STALE_FENCE", "input claim provenance")
	}
	if head.RunID != "" {
		run := findSessionLaneRunPtrV1(&snapshot, head.RunID)
		if run == nil || run.OwnerFence != command.FenceToken {
			return reject("STALE_FENCE", "run owner fence")
		}
	}
	return nil
}

func finalizeSessionLaneV1(snapshot *sessionLaneSnapshotV1, input *sessionLaneInputV1, command sessionLaneCommandV1, inputStatus, turnRunStatus string) error {
	if input.Status != "claimed" && input.Status != "running" {
		return reject("SESSION_INPUT_STATE_CONFLICT", "input.status")
	}
	turn := findSessionLaneTurnPtrV1(snapshot, input.TurnID)
	run := findSessionLaneRunPtrV1(snapshot, input.RunID)
	if turn == nil || command.ExpectedTurnVersion != turn.Version || (turn.Status != "created" && turn.Status != "running") {
		return reject("TURN_STATE_CONFLICT", "turn")
	}
	if run == nil || command.ExpectedRunVersion != run.Version || (run.Status != "created" && run.Status != "running" && run.Status != "recovery_pending") {
		return reject("RUN_STATE_CONFLICT", "run")
	}
	if run.EffectState == "unknown" {
		return reject("UNKNOWN_OUTCOME_UNRESOLVED", "run.effect_state")
	}
	input.Status = inputStatus
	input.Version++
	input.ClaimOwnerID = ""
	input.ResolutionCode = command.ResolutionCode
	turn.Status = turnRunStatus
	turn.Version++
	run.Status = turnRunStatus
	run.EffectState = "resolved"
	run.Version++
	releaseSessionLaneLeaseV1(snapshot)
	return nil
}

func releaseSessionLaneLeaseV1(snapshot *sessionLaneSnapshotV1) {
	snapshot.Lease.OwnerID = ""
	snapshot.Lease.LeaseUntilTick = 0
	snapshot.Lease.Version++
}

func validateSessionLaneSnapshotV1(snapshot sessionLaneSnapshotV1) error {
	if snapshot.SchemaVersion != sessionLaneSnapshotSchemaVersionV1 {
		return fmt.Errorf("schema_version=%q", snapshot.SchemaVersion)
	}
	if _, err := parseCanonicalUUIDv7(snapshot.SessionID); err != nil {
		return fmt.Errorf("session_id: %w", err)
	}
	if snapshot.Lease.Version <= 0 || snapshot.Lease.FenceToken < 0 || (snapshot.Lease.OwnerID == "") != (snapshot.Lease.LeaseUntilTick == 0) {
		return fmt.Errorf("非法 lease")
	}
	validInput := stringSetV1("pending", "claimed", "running", "retry_wait", "quarantined", "resolved", "dead")
	validTurn := stringSetV1("created", "running", "completed", "failed", "cancelled")
	validRun := stringSetV1("created", "running", "recovery_pending", "completed", "failed", "cancelled")
	inputIDs := make(map[string]struct{}, len(snapshot.Inputs))
	seqs := make(map[int64]struct{}, len(snapshot.Inputs))
	lastSeq := int64(0)
	activeClaims := 0
	for _, input := range snapshot.Inputs {
		if _, err := parseCanonicalUUIDv7(input.InputID); err != nil {
			return fmt.Errorf("input_id: %w", err)
		}
		if _, err := parseCanonicalUUIDv7(input.SourceID); err != nil || !sessionLaneBusinessDigestPatternV1.MatchString(input.SourceDigest) || input.Version <= 0 || input.EnqueueSeq <= lastSeq || input.Attempts < 0 || input.CancelVersion < 0 {
			return fmt.Errorf("非法 input %s", input.InputID)
		}
		if input.SourceType != "user_message" && input.SourceType != "approval_continuation_result" && input.SourceType != "batch_continuation_result" {
			return fmt.Errorf("未知 source_type %q", input.SourceType)
		}
		if (input.CancelCommandID == "") != (input.CancelRequestDigest == "") || (input.CancelRequested && input.CancelCommandID == "") {
			return fmt.Errorf("cancel command provenance 不完整")
		}
		if input.CancelCommandID != "" {
			if _, err := parseCanonicalUUIDv7(input.CancelCommandID); err != nil || !sessionLaneBusinessDigestPatternV1.MatchString(input.CancelRequestDigest) {
				return fmt.Errorf("cancel command provenance 非法")
			}
		}
		if _, exists := validInput[input.Status]; !exists {
			return fmt.Errorf("未知 input status %q", input.Status)
		}
		if _, exists := inputIDs[input.InputID]; exists {
			return fmt.Errorf("重复 input_id %s", input.InputID)
		}
		if _, exists := seqs[input.EnqueueSeq]; exists {
			return fmt.Errorf("重复 enqueue_seq %d", input.EnqueueSeq)
		}
		inputIDs[input.InputID] = struct{}{}
		seqs[input.EnqueueSeq] = struct{}{}
		lastSeq = input.EnqueueSeq
		if input.Status == "running" && (input.ClaimOwnerID == "" || input.ClaimFence <= 0) {
			return fmt.Errorf("活动 input 缺 claim provenance")
		}
		if input.ClaimOwnerID != "" {
			activeClaims++
		}
		if input.Status == "claimed" && input.ClaimFence <= 0 {
			return fmt.Errorf("claimed input 缺 claim fence")
		}
		if input.TurnID == "" && input.RunID != "" {
			return fmt.Errorf("无 Turn input 不得有 Run")
		}
	}
	if activeClaims > 1 {
		return fmt.Errorf("同一 Session 不得有多个 active input")
	}
	if snapshot.Lease.OwnerID == "" && activeClaims != 0 {
		return fmt.Errorf("idle lease 不得残留 active claim owner")
	}
	if snapshot.Lease.OwnerID != "" {
		head := sessionLaneHeadV1(snapshot.Inputs)
		if head == nil || head.ClaimOwnerID != snapshot.Lease.OwnerID || head.ClaimFence != snapshot.Lease.FenceToken || (head.Status != "claimed" && head.Status != "running") {
			return fmt.Errorf("lease 与 HOL claim provenance 不一致")
		}
	}
	turnIDs := make(map[string]sessionLaneTurnV1, len(snapshot.Turns))
	turnByInput := make(map[string]string, len(snapshot.Turns))
	for _, turn := range snapshot.Turns {
		if _, err := parseCanonicalUUIDv7(turn.TurnID); err != nil || turn.Version <= 0 {
			return fmt.Errorf("非法 turn %s", turn.TurnID)
		}
		if _, exists := inputIDs[turn.InputID]; !exists {
			return fmt.Errorf("turn 引用未知 input")
		}
		if _, exists := validTurn[turn.Status]; !exists {
			return fmt.Errorf("未知 turn status %q", turn.Status)
		}
		if _, exists := turnIDs[turn.TurnID]; exists {
			return fmt.Errorf("重复 turn_id")
		}
		if turn.TurnKind != "chat" && turn.TurnKind != "approval_continuation" && turn.TurnKind != "batch_explanation" {
			return fmt.Errorf("未知 turn_kind %q", turn.TurnKind)
		}
		if _, exists := turnByInput[turn.InputID]; exists {
			return fmt.Errorf("一个 input 不得有多个 turn")
		}
		turnByInput[turn.InputID] = turn.TurnID
		turnIDs[turn.TurnID] = turn
	}
	runIDs := make(map[string]struct{}, len(snapshot.Runs))
	runByInput := make(map[string]string, len(snapshot.Runs))
	for _, run := range snapshot.Runs {
		if _, err := parseCanonicalUUIDv7(run.RunID); err != nil || run.Version <= 0 || run.OwnerFence <= 0 || run.RecoveredFromFence < 0 || run.RecoveredFromFence > run.OwnerFence {
			return fmt.Errorf("非法 run %s", run.RunID)
		}
		turn, exists := turnIDs[run.TurnID]
		if !exists || turn.InputID != run.InputID {
			return fmt.Errorf("run 因果引用不一致")
		}
		if _, exists := validRun[run.Status]; !exists {
			return fmt.Errorf("未知 run status %q", run.Status)
		}
		if _, exists := runIDs[run.RunID]; exists {
			return fmt.Errorf("重复 run_id")
		}
		if run.EffectState != "not_started" && run.EffectState != "resolved" && run.EffectState != "unknown" {
			return fmt.Errorf("未知 run effect_state %q", run.EffectState)
		}
		if (run.Status == "completed" || run.Status == "failed" || run.Status == "cancelled") && run.EffectState != "resolved" {
			return fmt.Errorf("terminal run 必须已解决 effect")
		}
		if _, exists := runByInput[run.InputID]; exists {
			return fmt.Errorf("一个 input 不得有多个 run")
		}
		runByInput[run.InputID] = run.RunID
		runIDs[run.RunID] = struct{}{}
	}
	referencedRunIDs := make(map[string]string, len(snapshot.Inputs))
	for _, input := range snapshot.Inputs {
		if input.TurnID != "" {
			turn, exists := turnIDs[input.TurnID]
			if !exists || turn.InputID != input.InputID {
				return fmt.Errorf("input/turn 因果引用不一致")
			}
		}
		if input.RunID != "" {
			run := findSessionLaneRunPtrV1(&snapshot, input.RunID)
			if run == nil {
				return fmt.Errorf("input 引用未知 run")
			}
			if run.InputID != input.InputID || run.TurnID != input.TurnID {
				return fmt.Errorf("input/run 因果引用不一致")
			}
			if otherInputID, exists := referencedRunIDs[input.RunID]; exists && otherInputID != input.InputID {
				return fmt.Errorf("多个 input 不得引用同一 run")
			}
			referencedRunIDs[input.RunID] = input.InputID
			if snapshot.Lease.OwnerID != "" && input.InputID == sessionLaneHeadV1(snapshot.Inputs).InputID {
				if run.OwnerFence != snapshot.Lease.FenceToken {
					return fmt.Errorf("active run fence 与 lane 不一致")
				}
			}
			if input.Status == "claimed" && input.ClaimOwnerID == "" {
				if snapshot.Lease.OwnerID != "" || run.Status != "recovery_pending" {
					return fmt.Errorf("无 owner 的 claimed input 必须是 durable handoff")
				}
			}
			if input.Status == "retry_wait" && (run.Status != "recovery_pending" || run.EffectState != "not_started") {
				return fmt.Errorf("retry_wait input 必须等待未开始副作用的 recovery run")
			}
		}
		if turnID, exists := turnByInput[input.InputID]; exists && turnID != input.TurnID {
			return fmt.Errorf("orphan turn")
		}
		if runID, exists := runByInput[input.InputID]; exists && runID != input.RunID {
			return fmt.Errorf("orphan run")
		}
	}
	return nil
}

func summarizeSessionLaneStateV1(snapshot sessionLaneSnapshotV1, inputID string) (sessionLaneStateSummaryV1, error) {
	index := findSessionLaneInputV1(snapshot.Inputs, inputID)
	if index < 0 {
		return sessionLaneStateSummaryV1{}, fmt.Errorf("expected input %s 不存在", inputID)
	}
	input := snapshot.Inputs[index]
	summary := sessionLaneStateSummaryV1{
		LeaseOwnerID: snapshot.Lease.OwnerID, LeaseUntilTick: snapshot.Lease.LeaseUntilTick,
		LeaseFence: snapshot.Lease.FenceToken, LeaseVersion: snapshot.Lease.Version,
		InputID: input.InputID, InputStatus: input.Status, InputVersion: input.Version,
		InputAttempts: input.Attempts, InputClaimOwnerID: input.ClaimOwnerID, InputClaimFence: input.ClaimFence,
		TurnID: input.TurnID, RunID: input.RunID, CancelRequested: input.CancelRequested,
		CancelVersion: input.CancelVersion, ResolutionCode: input.ResolutionCode,
	}
	if head := sessionLaneHeadV1(snapshot.Inputs); head != nil {
		summary.HeadInputID = head.InputID
		summary.HeadStatus = head.Status
	}
	if turn := findSessionLaneTurnPtrV1(&snapshot, input.TurnID); turn != nil {
		summary.TurnStatus = turn.Status
		summary.TurnVersion = turn.Version
	}
	if run := findSessionLaneRunPtrV1(&snapshot, input.RunID); run != nil {
		summary.RunStatus = run.Status
		summary.RunVersion = run.Version
		summary.RunOwnerFence = run.OwnerFence
		summary.RecoveredFromFence = run.RecoveredFromFence
		summary.RunEffectState = run.EffectState
	}
	return summary, nil
}

func cloneSessionLaneSnapshotV1(snapshot sessionLaneSnapshotV1) sessionLaneSnapshotV1 {
	clone := snapshot
	clone.Inputs = append([]sessionLaneInputV1(nil), snapshot.Inputs...)
	clone.Turns = append([]sessionLaneTurnV1(nil), snapshot.Turns...)
	clone.Runs = append([]sessionLaneRunV1(nil), snapshot.Runs...)
	return clone
}

func assertSessionLaneMutationScopeV1(t *testing.T, before, after sessionLaneSnapshotV1, targetInputID string) {
	t.Helper()
	if before.SchemaVersion != after.SchemaVersion || before.SessionID != after.SessionID {
		t.Fatal("状态命令不得改变 Snapshot/Session 身份")
	}
	beforeIndex := findSessionLaneInputV1(before.Inputs, targetInputID)
	afterIndex := findSessionLaneInputV1(after.Inputs, targetInputID)
	if beforeIndex < 0 || afterIndex < 0 || len(before.Inputs) != len(after.Inputs) {
		t.Fatal("状态命令不得新增、删除或替换 Input")
	}
	for index := range before.Inputs {
		if before.Inputs[index].InputID != targetInputID && !reflect.DeepEqual(before.Inputs[index], after.Inputs[index]) {
			t.Fatalf("状态命令越界修改非目标 Input %s", before.Inputs[index].InputID)
		}
	}
	oldInput, newInput := before.Inputs[beforeIndex], after.Inputs[afterIndex]
	if oldInput.InputID != newInput.InputID || oldInput.SourceType != newInput.SourceType || oldInput.SourceID != newInput.SourceID ||
		oldInput.SourceDigest != newInput.SourceDigest || oldInput.EnqueueSeq != newInput.EnqueueSeq || oldInput.TurnID != newInput.TurnID {
		t.Fatal("状态命令不得改变 Input 冻结身份或来源")
	}
	if oldInput.RunID != "" && oldInput.RunID != newInput.RunID {
		t.Fatal("状态命令不得替换稳定 RunID")
	}
	for _, oldTurn := range before.Turns {
		newTurn := findSessionLaneTurnPtrV1(&after, oldTurn.TurnID)
		if newTurn == nil {
			t.Fatalf("状态命令不得删除 Turn %s", oldTurn.TurnID)
		}
		if oldTurn.InputID != targetInputID && !reflect.DeepEqual(oldTurn, *newTurn) {
			t.Fatalf("状态命令越界修改非目标 Turn %s", oldTurn.TurnID)
		}
		if oldTurn.InputID == targetInputID && (oldTurn.TurnID != newTurn.TurnID || oldTurn.InputID != newTurn.InputID || oldTurn.TurnKind != newTurn.TurnKind) {
			t.Fatal("状态命令不得改变 Turn 冻结身份")
		}
	}
	for _, oldRun := range before.Runs {
		newRun := findSessionLaneRunPtrV1(&after, oldRun.RunID)
		if newRun == nil {
			t.Fatalf("状态命令不得删除 Run %s", oldRun.RunID)
		}
		if oldRun.InputID != targetInputID && !reflect.DeepEqual(oldRun, *newRun) {
			t.Fatalf("状态命令越界修改非目标 Run %s", oldRun.RunID)
		}
		if oldRun.InputID == targetInputID && (oldRun.RunID != newRun.RunID || oldRun.InputID != newRun.InputID || oldRun.TurnID != newRun.TurnID) {
			t.Fatal("状态命令不得改变 Run 冻结身份")
		}
	}
}

func findSessionLaneInputV1(inputs []sessionLaneInputV1, inputID string) int {
	for index := range inputs {
		if inputs[index].InputID == inputID {
			return index
		}
	}
	return -1
}

func sessionLaneHeadV1(inputs []sessionLaneInputV1) *sessionLaneInputV1 {
	for index := range inputs {
		if !isTerminalInputStatusV1(inputs[index].Status) {
			return &inputs[index]
		}
	}
	return nil
}

func findSessionLaneTurnPtrV1(snapshot *sessionLaneSnapshotV1, turnID string) *sessionLaneTurnV1 {
	for index := range snapshot.Turns {
		if snapshot.Turns[index].TurnID == turnID {
			return &snapshot.Turns[index]
		}
	}
	return nil
}

func findSessionLaneRunPtrV1(snapshot *sessionLaneSnapshotV1, runID string) *sessionLaneRunV1 {
	for index := range snapshot.Runs {
		if snapshot.Runs[index].RunID == runID {
			return &snapshot.Runs[index]
		}
	}
	return nil
}

func isTerminalInputStatusV1(status string) bool { return status == "resolved" || status == "dead" }

func validRecoveryEvidenceV1(command sessionLaneCommandV1) bool {
	return command.EvidenceKind == "authoritative_reconciliation" &&
		(command.ReconciliationOutcome == "effect_not_started" || command.ReconciliationOutcome == "effect_committed") &&
		sessionLaneBusinessDigestPatternV1.MatchString(command.EvidenceDigest)
}

func validClaimTriggerV1(trigger string) bool {
	return trigger == "redis_wake" || trigger == "postgres_scan" || trigger == "recovery_scan"
}

func stringSetV1(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func parseCanonicalUUIDv7(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, err
	}
	if parsed.Version() != 7 || parsed.String() != value {
		return uuid.Nil, fmt.Errorf("不是 canonical UUIDv7")
	}
	return parsed, nil
}
