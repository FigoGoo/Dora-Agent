// Package contract_test 只承载尚在评审期的 Ingress/Command Receipt 契约语料校验，不提供生产 Runtime。
package contract_test

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"testing"
)

const (
	ingressCorpusSchemaVersionV1   = "session_lane_ingress_v1_corpus.v1"
	ingressSnapshotSchemaVersionV1 = "session_lane_ingress_snapshot.v1"
	ingressRequestSchemaVersionV1  = "session_lane_ingress_request.v1"
	ingressResultSchemaVersionV1   = "session_lane_ingress_result.v1"
	ingressQuerySchemaVersionV1    = "session_lane_ingress_query.v1"
	ingressCorpusPath              = "testdata/w2_r02_ingress/session_lane_ingress_v1.json"
	ingressManifestPath            = "testdata/w2_r02_ingress/manifest.json"
)

//go:embed testdata/w2_r02_ingress/*.json
var w2R02IngressCorpusFS embed.FS

var ingressSHA256PatternV1 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ingressManifestV1 struct {
	SchemaVersion string                  `json:"schema_version"`
	Files         []ingressManifestFileV1 `json:"files"`
}

type ingressManifestFileV1 struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type ingressCorpusV1 struct {
	SchemaVersion string             `json:"schema_version"`
	InitialState  ingressStateV1     `json:"initial_state"`
	Cases         []ingressCaseV1    `json:"cases"`
	ExactSets     ingressExactSetsV1 `json:"exact_sets"`
}

type ingressExactSetsV1 struct {
	CommandTypes      []string `json:"command_types"`
	CommandKinds      []string `json:"command_kinds"`
	SourceTypes       []string `json:"source_types"`
	ExecutionClasses  []string `json:"execution_classes"`
	AuthorityRefTypes []string `json:"authority_ref_types"`
	InputStatuses     []string `json:"input_statuses"`
	TurnKinds         []string `json:"turn_kinds"`
	TurnStatuses      []string `json:"turn_statuses"`
	MarkerTypes       []string `json:"marker_types"`
	Dispositions      []string `json:"dispositions"`
	QueryStatuses     []string `json:"query_statuses"`
	ErrorCodes        []string `json:"error_codes"`
}

type ingressStateV1 struct {
	StateID  string            `json:"state_id"`
	Snapshot ingressSnapshotV1 `json:"snapshot"`
}

type ingressCaseV1 struct {
	ID          string                    `json:"id"`
	FromState   string                    `json:"from_state"`
	SaveAsState string                    `json:"save_as_state"`
	Expect      string                    `json:"expect"`
	ErrorCode   string                    `json:"error_code"`
	Command     ingressCommandV1          `json:"command"`
	Expected    *ingressCaseExpectationV1 `json:"expected,omitempty"`
}

type ingressSnapshotV1 struct {
	SchemaVersion string                      `json:"schema_version"`
	Sessions      []ingressSessionV1          `json:"sessions"`
	Messages      []ingressMessageV1          `json:"messages"`
	Inputs        []ingressInputV1            `json:"inputs"`
	Turns         []ingressTurnV1             `json:"turns"`
	Receipts      []ingressCommandReceiptV1   `json:"receipts"`
	Markers       []ingressProjectionMarkerV1 `json:"markers"`
}

type ingressSessionV1 struct {
	SessionID      string `json:"session_id"`
	Version        int64  `json:"version"`
	LastMessageSeq int64  `json:"last_message_seq"`
	LastEnqueueSeq int64  `json:"last_enqueue_seq"`
	LastEventSeq   int64  `json:"last_event_seq"`
}

type ingressMessageV1 struct {
	MessageID     string `json:"message_id"`
	SessionID     string `json:"session_id"`
	MessageSeq    int64  `json:"message_seq"`
	ContentDigest string `json:"content_digest"`
}

type ingressInputV1 struct {
	InputID            string `json:"input_id"`
	SessionID          string `json:"session_id"`
	SourceType         string `json:"source_type"`
	SourceID           string `json:"source_id"`
	SourceDigest       string `json:"source_digest"`
	ExecutionClass     string `json:"execution_class"`
	AuthorityRefType   string `json:"authority_ref_type"`
	AuthorityRefID     string `json:"authority_ref_id"`
	AuthorityRefDigest string `json:"authority_ref_digest"`
	MessageID          string `json:"message_id"`
	ContentDigest      string `json:"content_digest"`
	TurnID             string `json:"turn_id"`
	EnqueueSeq         int64  `json:"enqueue_seq"`
	Status             string `json:"status"`
	Version            int64  `json:"version"`
}

type ingressTurnV1 struct {
	TurnID            string `json:"turn_id"`
	SessionID         string `json:"session_id"`
	InputID           string `json:"input_id"`
	TurnKind          string `json:"turn_kind"`
	SnapshotCutoffSeq int64  `json:"snapshot_cutoff_seq"`
	Status            string `json:"status"`
	Version           int64  `json:"version"`
}

type ingressCommandReceiptV1 struct {
	CommandID           string `json:"command_id"`
	CommandType         string `json:"command_type"`
	RequestDigest       string `json:"request_digest"`
	SessionID           string `json:"session_id"`
	ResultSchemaVersion string `json:"result_schema_version"`
	ResultDigest        string `json:"result_digest"`
	MessageID           string `json:"message_id"`
	InputID             string `json:"input_id"`
	TurnID              string `json:"turn_id"`
	EnqueueSeq          int64  `json:"enqueue_seq"`
	InputVersion        int64  `json:"input_version"`
	TurnVersion         int64  `json:"turn_version"`
	MarkerID            string `json:"marker_id"`
	CommittedTick       int64  `json:"committed_tick"`
}

type ingressProjectionMarkerV1 struct {
	MarkerID   string `json:"marker_id"`
	SessionID  string `json:"session_id"`
	InputID    string `json:"input_id"`
	EventSeq   int64  `json:"event_seq"`
	MarkerType string `json:"marker_type"`
}

type ingressCommandV1 struct {
	Kind                   string `json:"kind"`
	SchemaVersion          string `json:"schema_version"`
	CommandID              string `json:"command_id"`
	CommandType            string `json:"command_type"`
	RequestDigest          string `json:"request_digest"`
	SessionID              string `json:"session_id"`
	SourceType             string `json:"source_type"`
	SourceID               string `json:"source_id"`
	SourceDigest           string `json:"source_digest"`
	ExecutionClass         string `json:"execution_class"`
	AuthorityRefType       string `json:"authority_ref_type"`
	AuthorityRefID         string `json:"authority_ref_id"`
	AuthorityRefDigest     string `json:"authority_ref_digest"`
	TrustedAuthority       bool   `json:"trusted_authority"`
	MessagePresent         bool   `json:"message_present"`
	ContentDigest          string `json:"content_digest"`
	ExpectedSessionVersion int64  `json:"expected_session_version"`
	AllocatedMessageID     string `json:"allocated_message_id"`
	AllocatedInputID       string `json:"allocated_input_id"`
	AllocatedTurnID        string `json:"allocated_turn_id"`
	AllocatedMarkerID      string `json:"allocated_marker_id"`
	CallerEnqueueSeq       int64  `json:"caller_enqueue_seq"`
	CallerRunID            string `json:"caller_run_id"`
	CommittedTick          int64  `json:"committed_tick"`
	FailAt                 string `json:"fail_at"`
	ExpectedCommandType    string `json:"expected_command_type"`
	ExpectedRequestDigest  string `json:"expected_request_digest"`
}

type ingressCanonicalRequestV1 struct {
	SchemaVersion      string `json:"schema_version"`
	CommandType        string `json:"command_type"`
	SessionID          string `json:"session_id"`
	SourceType         string `json:"source_type"`
	SourceID           string `json:"source_id"`
	SourceDigest       string `json:"source_digest"`
	ExecutionClass     string `json:"execution_class"`
	AuthorityRefType   string `json:"authority_ref_type"`
	AuthorityRefID     string `json:"authority_ref_id"`
	AuthorityRefDigest string `json:"authority_ref_digest"`
	MessagePresent     bool   `json:"message_present"`
	ContentDigest      string `json:"content_digest"`
}

type ingressResultProjectionV1 struct {
	SchemaVersion string `json:"schema_version"`
	SessionID     string `json:"session_id"`
	MessageID     string `json:"message_id"`
	InputID       string `json:"input_id"`
	TurnID        string `json:"turn_id"`
	EnqueueSeq    int64  `json:"enqueue_seq"`
	InputVersion  int64  `json:"input_version"`
	TurnVersion   int64  `json:"turn_version"`
	MarkerID      string `json:"marker_id"`
	CommittedTick int64  `json:"committed_tick"`
}

type ingressCommandResultV1 struct {
	Disposition  string `json:"disposition"`
	QueryStatus  string `json:"query_status"`
	MessageID    string `json:"message_id"`
	InputID      string `json:"input_id"`
	TurnID       string `json:"turn_id"`
	EnqueueSeq   int64  `json:"enqueue_seq"`
	ResultDigest string `json:"result_digest"`
}

type ingressStateSummaryV1 struct {
	SessionVersion int64  `json:"session_version"`
	LastMessageSeq int64  `json:"last_message_seq"`
	LastEnqueueSeq int64  `json:"last_enqueue_seq"`
	LastEventSeq   int64  `json:"last_event_seq"`
	MessageCount   int    `json:"message_count"`
	InputCount     int    `json:"input_count"`
	TurnCount      int    `json:"turn_count"`
	ReceiptCount   int    `json:"receipt_count"`
	MarkerCount    int    `json:"marker_count"`
	LastInputID    string `json:"last_input_id"`
	LastSourceType string `json:"last_source_type"`
	LastClass      string `json:"last_class"`
	LastTurnKind   string `json:"last_turn_kind"`
	LastTurnCutoff int64  `json:"last_turn_cutoff"`
}

type ingressCaseExpectationV1 struct {
	State  ingressStateSummaryV1  `json:"state"`
	Result ingressCommandResultV1 `json:"result"`
}

func TestW2R02IngressCorpusManifest(t *testing.T) {
	entries, err := w2R02IngressCorpusFS.ReadDir("testdata/w2_r02_ingress")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name() != "manifest.json" || entries[1].Name() != "session_lane_ingress_v1.json" {
		t.Fatalf("W2-R02 Ingress Corpus 出现未登记文件或文件缺失: %v", entries)
	}
	raw, err := w2R02IngressCorpusFS.ReadFile(ingressManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("Ingress manifest JSON 非法: %v", err)
	}
	var manifest ingressManifestV1
	if err := strictDecode(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != "w2_r02_ingress_manifest.v1" || len(manifest.Files) != 1 {
		t.Fatalf("Ingress manifest 版本或文件数错误: %+v", manifest)
	}
	item := manifest.Files[0]
	if item.File != "session_lane_ingress_v1.json" || len(item.SHA256) != len("sha256:")+64 || item.SHA256[:len("sha256:")] != "sha256:" || !ingressSHA256PatternV1.MatchString(item.SHA256[len("sha256:"):]) {
		t.Fatalf("Ingress manifest 文件或摘要格式错误: %+v", item)
	}
	content, err := w2R02IngressCorpusFS.ReadFile("testdata/w2_r02_ingress/" + item.File)
	if err != nil {
		t.Fatal(err)
	}
	actual := sha256.Sum256(content)
	if got := "sha256:" + hex.EncodeToString(actual[:]); got != item.SHA256 {
		t.Fatalf("%s raw digest=%s want=%s", item.File, got, item.SHA256)
	}
}

func TestSessionLaneIngressV1Corpus(t *testing.T) {
	corpus := loadIngressCorpusV1(t)
	assertIngressExactSetsV1(t, corpus.ExactSets)
	if err := validateIngressSnapshotV1(corpus.InitialState.Snapshot); err != nil {
		t.Fatalf("初始 Ingress 状态非法: %v", err)
	}
	states := map[string]ingressSnapshotV1{corpus.InitialState.StateID: corpus.InitialState.Snapshot}
	requiredCases := []string{
		"ING-P01-enqueue-user-chat", "ING-P02-command-replay", "ING-P03-source-replay-alias",
		"ING-P04-second-user-next-seq", "ING-P05-enqueue-approval-turn", "ING-P06-enqueue-approval-projection",
		"ING-P07-enqueue-batch-explanation", "ING-P08-enqueue-batch-projection",
		"ING-P09-query-completed", "ING-P10-query-not-found", "ING-P11-query-conflict-no-leak",
		"ING-P12-query-source-alias-completed", "ING-P13-query-wrong-scope-no-leak",
		"ING-P14-second-session-independent-seq",
		"ING-N01-invalid-schema", "ING-N02-invalid-command-id", "ING-N03-invalid-digest-format",
		"ING-N04-request-digest-mismatch", "ING-N05-global-command-type-conflict",
		"ING-N06-command-semantic-conflict", "ING-N07-cross-session-command-conflict",
		"ING-N08-source-digest-conflict", "ING-N09-source-class-conflict", "ING-N10-untrusted-source",
		"ING-N11-source-classification-conflict", "ING-N12-user-message-required",
		"ING-N13-system-message-forbidden", "ING-N14-caller-enqueue-seq-forbidden",
		"ING-N15-caller-run-forbidden", "ING-N16-invalid-allocated-id", "ING-N17-transaction-failure-rolls-back",
		"ING-N18-unknown-session", "ING-N19-stale-session-version",
		"ING-N20-transaction-failure-after-message", "ING-N21-transaction-failure-after-input",
		"ING-N22-transaction-failure-after-marker", "ING-N23-transaction-failure-after-receipt",
		"ING-N24-allocated-message-collision", "ING-N25-allocated-input-collision",
		"ING-N26-allocated-turn-collision", "ING-N27-allocated-marker-collision",
		"ING-N28-alias-receipt-failure-rolls-back",
	}
	seen := make(map[string]struct{}, len(corpus.Cases))
	for _, fixture := range corpus.Cases {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			if _, exists := seen[fixture.ID]; exists {
				t.Fatalf("重复 Ingress case id %q", fixture.ID)
			}
			seen[fixture.ID] = struct{}{}
			before, exists := states[fixture.FromState]
			if !exists {
				t.Fatalf("未知 from_state %q", fixture.FromState)
			}
			after, result, err := applyIngressCommandV1(before, fixture.Command)
			if fixture.Expect == "reject" {
				if err == nil || errorCode(err) != fixture.ErrorCode {
					t.Fatalf("期望拒绝 code=%s，实际 err=%v code=%s", fixture.ErrorCode, err, errorCode(err))
				}
				if fixture.SaveAsState != "" || fixture.Expected != nil || result != (ingressCommandResultV1{}) {
					t.Fatal("拒绝向量不得保存状态、结果或 expected")
				}
				if !reflect.DeepEqual(after, before) {
					t.Fatal("拒绝 Ingress 命令不得改变 Snapshot")
				}
				return
			}
			if fixture.Expect != "accept" || fixture.ErrorCode != "" || err != nil || fixture.SaveAsState == "" || fixture.Expected == nil {
				t.Fatalf("合法 Ingress case 元数据或结果错误: expect=%q code=%q save=%q err=%v", fixture.Expect, fixture.ErrorCode, fixture.SaveAsState, err)
			}
			if err := validateIngressSnapshotV1(after); err != nil {
				t.Fatalf("Ingress 迁移后状态非法: %v", err)
			}
			got := ingressCaseExpectationV1{State: summarizeIngressStateV1(after, fixture.Command.SessionID), Result: result}
			if got != *fixture.Expected {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(fixture.Expected, "", "  ")
				t.Fatalf("Ingress 摘要不符\ngot=%s\nwant=%s", gotJSON, wantJSON)
			}
			if _, exists := states[fixture.SaveAsState]; exists {
				t.Fatalf("save_as_state %q 不得覆盖已有状态", fixture.SaveAsState)
			}
			states[fixture.SaveAsState] = after
		})
	}
	if len(seen) != len(requiredCases) {
		t.Fatalf("Ingress case exact-set 数量=%d want=%d", len(seen), len(requiredCases))
	}
	for _, id := range requiredCases {
		if _, exists := seen[id]; !exists {
			t.Fatalf("缺少固定 Ingress case %s", id)
		}
	}
}

func TestSessionLaneIngressV1RejectsCorruptReceiptSnapshot(t *testing.T) {
	corpus := loadIngressCorpusV1(t)
	var create ingressCommandV1
	for _, fixture := range corpus.Cases {
		if fixture.ID == "ING-P01-enqueue-user-chat" {
			create = fixture.Command
			break
		}
	}
	after, _, err := applyIngressCommandV1(corpus.InitialState.Snapshot, create)
	if err != nil {
		t.Fatalf("准备完整 Ingress Snapshot: %v", err)
	}
	assertCorrupt := func(t *testing.T, corrupt ingressSnapshotV1) {
		t.Helper()
		receiptIndex := len(corrupt.Receipts) - 1
		if _, err := resultFromIngressReceiptV1(corrupt, corrupt.Receipts[receiptIndex], "replayed"); errorCode(err) != "SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION" {
			t.Fatalf("损坏 receipt 必须返回完整性错误，实际 err=%v code=%s", err, errorCode(err))
		}
		if err := validateIngressSnapshotV1(corrupt); err == nil {
			t.Fatal("损坏 receipt 的 Snapshot 不得通过 invariant 校验")
		}
		rejected, result, err := applyIngressCommandV1(corrupt, create)
		if errorCode(err) != "SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION" || result != (ingressCommandResultV1{}) || !reflect.DeepEqual(rejected, corrupt) {
			t.Fatalf("损坏 Snapshot 必须 fail-closed 且原样返回: err=%v code=%s result=%+v", err, errorCode(err), result)
		}
	}
	t.Run("result_digest", func(t *testing.T) {
		corrupt := cloneIngressSnapshotV1(after)
		receiptIndex := len(corrupt.Receipts) - 1
		corrupt.Receipts[receiptIndex].ResultDigest = corrupt.Receipts[receiptIndex].RequestDigest
		assertCorrupt(t, corrupt)
	})
	t.Run("request_digest_binding", func(t *testing.T) {
		corrupt := cloneIngressSnapshotV1(after)
		receiptIndex := len(corrupt.Receipts) - 1
		corrupt.Receipts[receiptIndex].RequestDigest = corrupt.Receipts[receiptIndex].ResultDigest
		assertCorrupt(t, corrupt)
	})
}

func TestSessionLaneIngressV1ReplayUsesFrozenResultAfterRuntimeProgress(t *testing.T) {
	corpus := loadIngressCorpusV1(t)
	commands := make(map[string]ingressCommandV1, 2)
	for _, fixture := range corpus.Cases {
		if fixture.ID == "ING-P01-enqueue-user-chat" || fixture.ID == "ING-P03-source-replay-alias" {
			commands[fixture.ID] = fixture.Command
		}
	}
	after, created, err := applyIngressCommandV1(corpus.InitialState.Snapshot, commands["ING-P01-enqueue-user-chat"])
	if err != nil {
		t.Fatalf("准备创建结果: %v", err)
	}
	for index := range after.Inputs {
		if after.Inputs[index].InputID == created.InputID {
			after.Inputs[index].Status = "resolved"
			after.Inputs[index].Version = 10
		}
	}
	for index := range after.Turns {
		if after.Turns[index].TurnID == created.TurnID {
			after.Turns[index].Status = "completed"
			after.Turns[index].Version = 3
		}
	}
	if err := validateIngressSnapshotV1(after); err != nil {
		t.Fatalf("Runtime 推进后的 Snapshot 应保持 Ingress Receipt 可读: %v", err)
	}

	commandReplayState, commandReplay, err := applyIngressCommandV1(after, commands["ING-P01-enqueue-user-chat"])
	if err != nil || commandReplay.Disposition != "replayed" || commandReplay.ResultDigest != created.ResultDigest || !reflect.DeepEqual(commandReplayState, after) {
		t.Fatalf("Runtime 推进后 Command Replay 必须返回冻结结果: result=%+v err=%v", commandReplay, err)
	}
	sourceReplayState, sourceReplay, err := applyIngressCommandV1(after, commands["ING-P03-source-replay-alias"])
	if err != nil || sourceReplay.Disposition != "source_replayed" || sourceReplay.ResultDigest != created.ResultDigest {
		t.Fatalf("Runtime 推进后 Source Replay 必须返回冻结结果: result=%+v err=%v", sourceReplay, err)
	}
	if len(sourceReplayState.Receipts) != len(after.Receipts)+1 || findIngressReceiptV1(sourceReplayState.Receipts, commands["ING-P03-source-replay-alias"].CommandID) == nil {
		t.Fatal("Source Replay 必须用 alias Receipt 占用新的全局 CommandID")
	}
}

func loadIngressCorpusV1(t *testing.T) ingressCorpusV1 {
	t.Helper()
	raw, err := w2R02IngressCorpusFS.ReadFile(ingressCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("Ingress Corpus JSON 非法: %v", err)
	}
	var corpus ingressCorpusV1
	if err := strictDecode(raw, &corpus); err != nil {
		t.Fatalf("解析 Ingress Corpus: %v", err)
	}
	if corpus.SchemaVersion != ingressCorpusSchemaVersionV1 {
		t.Fatalf("Ingress Corpus schema_version=%q", corpus.SchemaVersion)
	}
	return corpus
}

func assertIngressExactSetsV1(t *testing.T, got ingressExactSetsV1) {
	t.Helper()
	assert := func(name string, actual, want []string) {
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("%s exact-set=%v want=%v", name, actual, want)
		}
	}
	assert("command_types", got.CommandTypes, []string{"ensure_project_session_v1", "ensure_project_session_v2", "enqueue_input_v1"})
	assert("command_kinds", got.CommandKinds, []string{"enqueue", "query"})
	assert("source_types", got.SourceTypes, []string{"user_message", "approval_continuation_result", "batch_continuation_result"})
	assert("execution_classes", got.ExecutionClasses, []string{"chat", "approval_continuation", "batch_explanation", "deterministic_projection"})
	assert("authority_ref_types", got.AuthorityRefTypes, []string{"authenticated_user", "approval_decision", "approval_invalidation", "batch_terminal_event"})
	assert("input_statuses", got.InputStatuses, []string{"pending", "claimed", "running", "retry_wait", "quarantined", "resolved", "dead"})
	assert("turn_kinds", got.TurnKinds, []string{"chat", "approval_continuation", "batch_explanation"})
	assert("turn_statuses", got.TurnStatuses, []string{"created", "running", "completed", "failed", "cancelled"})
	assert("marker_types", got.MarkerTypes, []string{"session.input.accepted"})
	assert("dispositions", got.Dispositions, []string{"created", "replayed", "source_replayed"})
	assert("query_statuses", got.QueryStatuses, []string{"completed", "not_found", "conflict"})
	assert("error_codes", got.ErrorCodes, []string{"SESSION_COMMAND_INVALID", "SESSION_COMMAND_VERSION_CONFLICT", "SESSION_COMMAND_CONFLICT", "SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION", "SESSION_INPUT_SOURCE_UNTRUSTED", "SESSION_INPUT_SOURCE_CONFLICT", "SESSION_INPUT_CLASSIFICATION_CONFLICT", "SESSION_INPUT_STATE_CONFLICT", "SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION"})
}

func applyIngressCommandV1(before ingressSnapshotV1, command ingressCommandV1) (ingressSnapshotV1, ingressCommandResultV1, error) {
	if err := validateIngressSnapshotV1(before); err != nil {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION", err.Error())
	}
	after := cloneIngressSnapshotV1(before)
	switch command.Kind {
	case "query":
		return queryIngressCommandV1(before, command)
	case "enqueue":
	default:
		return before, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "command.kind")
	}
	if command.SchemaVersion != ingressRequestSchemaVersionV1 || command.CommandType != "enqueue_input_v1" {
		return before, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "schema_version/command_type")
	}
	switch command.FailAt {
	case "", "after_message", "after_input", "after_turn", "after_marker", "after_receipt":
	default:
		return before, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "fail_at fixture")
	}
	if _, err := parseCanonicalUUIDv7(command.CommandID); err != nil {
		return before, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "command_id")
	}
	if _, err := parseCanonicalUUIDv7(command.SessionID); err != nil {
		return before, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "session_id")
	}
	if command.CommittedTick <= 0 {
		return before, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "committed_tick")
	}
	calculatedDigest, err := calculateIngressRequestDigestV1(command)
	if err != nil {
		return before, ingressCommandResultV1{}, err
	}
	if !ingressSHA256PatternV1.MatchString(command.RequestDigest) || subtle.ConstantTimeCompare([]byte(calculatedDigest), []byte(command.RequestDigest)) != 1 {
		return before, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "request_digest")
	}
	if receipt := findIngressReceiptV1(after.Receipts, command.CommandID); receipt != nil {
		if receipt.CommandType != command.CommandType {
			return before, ingressCommandResultV1{}, reject("SESSION_COMMAND_VERSION_CONFLICT", "global command type")
		}
		if receipt.RequestDigest != calculatedDigest || receipt.SessionID != command.SessionID {
			return before, ingressCommandResultV1{}, reject("SESSION_COMMAND_CONFLICT", "global command semantic conflict")
		}
		result, resultErr := resultFromIngressReceiptV1(after, *receipt, "replayed")
		if resultErr != nil {
			return before, ingressCommandResultV1{}, resultErr
		}
		return after, result, nil
	}
	if !command.TrustedAuthority {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_SOURCE_UNTRUSTED", "authority")
	}
	turnKind, mappingErr := validateIngressClassificationV1(command)
	if mappingErr != nil {
		return before, ingressCommandResultV1{}, mappingErr
	}
	if command.CallerEnqueueSeq != 0 || command.CallerRunID != "" {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_STATE_CONFLICT", "caller generated state")
	}
	if source := findIngressInputBySourceV1(after.Inputs, command.SessionID, command.SourceType, command.SourceID); source != nil {
		if source.SourceDigest != command.SourceDigest || source.ExecutionClass != command.ExecutionClass ||
			source.AuthorityRefType != command.AuthorityRefType || source.AuthorityRefID != command.AuthorityRefID ||
			source.AuthorityRefDigest != command.AuthorityRefDigest || source.ContentDigest != command.ContentDigest {
			return before, ingressCommandResultV1{}, reject("SESSION_INPUT_SOURCE_CONFLICT", "source semantic conflict")
		}
		originalReceipt := findIngressReceiptByInputV1(after.Receipts, source.InputID)
		if originalReceipt == nil {
			return before, ingressCommandResultV1{}, reject("SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION", "source replay receipt")
		}
		aliasReceipt := *originalReceipt
		aliasReceipt.CommandID = command.CommandID
		aliasReceipt.CommandType = command.CommandType
		aliasReceipt.RequestDigest = calculatedDigest
		after.Receipts = append(after.Receipts, aliasReceipt)
		if command.FailAt == "after_receipt" {
			return before, ingressCommandResultV1{}, reject("SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION", "injected alias receipt failure")
		}
		result, resultErr := resultFromIngressReceiptV1(after, aliasReceipt, "source_replayed")
		if resultErr != nil {
			return before, ingressCommandResultV1{}, resultErr
		}
		return after, result, nil
	}
	session := findIngressSessionPtrV1(&after, command.SessionID)
	if session == nil || command.ExpectedSessionVersion != session.Version {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_STATE_CONFLICT", "session version")
	}
	if _, err := parseCanonicalUUIDv7(command.AllocatedInputID); err != nil || findIngressInputV1(after.Inputs, command.AllocatedInputID) != nil {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_STATE_CONFLICT", "allocated input_id")
	}
	if _, err := parseCanonicalUUIDv7(command.AllocatedMarkerID); err != nil || findIngressMarkerV1(after.Markers, command.AllocatedMarkerID) != nil {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_STATE_CONFLICT", "allocated marker_id")
	}
	if command.MessagePresent {
		if _, err := parseCanonicalUUIDv7(command.AllocatedMessageID); err != nil || findIngressMessageV1(after.Messages, command.AllocatedMessageID) != nil {
			return before, ingressCommandResultV1{}, reject("SESSION_INPUT_STATE_CONFLICT", "allocated message_id")
		}
	} else if command.AllocatedMessageID != "" {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_STATE_CONFLICT", "unexpected message_id")
	}
	if turnKind != "" {
		if _, err := parseCanonicalUUIDv7(command.AllocatedTurnID); err != nil || findIngressTurnV1(after.Turns, command.AllocatedTurnID) != nil {
			return before, ingressCommandResultV1{}, reject("SESSION_INPUT_STATE_CONFLICT", "allocated turn_id")
		}
	} else if command.AllocatedTurnID != "" {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_STATE_CONFLICT", "unexpected turn_id")
	}

	session.LastEnqueueSeq++
	session.LastEventSeq++
	session.Version++
	messageID := ""
	if command.MessagePresent {
		session.LastMessageSeq++
		messageID = command.AllocatedMessageID
		after.Messages = append(after.Messages, ingressMessageV1{
			MessageID: messageID, SessionID: command.SessionID, MessageSeq: session.LastMessageSeq, ContentDigest: command.ContentDigest,
		})
		if command.FailAt == "after_message" {
			return before, ingressCommandResultV1{}, reject("SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION", "injected transaction failure")
		}
	}
	input := ingressInputV1{
		InputID: command.AllocatedInputID, SessionID: command.SessionID, SourceType: command.SourceType,
		SourceID: command.SourceID, SourceDigest: command.SourceDigest, ExecutionClass: command.ExecutionClass,
		AuthorityRefType: command.AuthorityRefType, AuthorityRefID: command.AuthorityRefID,
		AuthorityRefDigest: command.AuthorityRefDigest, MessageID: messageID, ContentDigest: command.ContentDigest,
		TurnID: command.AllocatedTurnID, EnqueueSeq: session.LastEnqueueSeq, Status: "pending", Version: 1,
	}
	after.Inputs = append(after.Inputs, input)
	if command.FailAt == "after_input" {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION", "injected transaction failure")
	}
	if turnKind != "" {
		after.Turns = append(after.Turns, ingressTurnV1{
			TurnID: command.AllocatedTurnID, SessionID: command.SessionID, InputID: input.InputID,
			TurnKind: turnKind, SnapshotCutoffSeq: input.EnqueueSeq, Status: "created", Version: 1,
		})
	}
	if command.FailAt == "after_turn" {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION", "injected transaction failure")
	}
	after.Markers = append(after.Markers, ingressProjectionMarkerV1{
		MarkerID: command.AllocatedMarkerID, SessionID: command.SessionID, InputID: input.InputID,
		EventSeq: session.LastEventSeq, MarkerType: "session.input.accepted",
	})
	if command.FailAt == "after_marker" {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION", "injected transaction failure")
	}
	projection := ingressResultProjectionV1{
		SchemaVersion: ingressResultSchemaVersionV1, SessionID: command.SessionID, MessageID: messageID,
		InputID: input.InputID, TurnID: input.TurnID, EnqueueSeq: input.EnqueueSeq, InputVersion: input.Version,
		MarkerID: command.AllocatedMarkerID, CommittedTick: command.CommittedTick,
	}
	if input.TurnID != "" {
		projection.TurnVersion = 1
	}
	resultDigest, digestErr := digestJSONV1(projection)
	if digestErr != nil {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION", digestErr.Error())
	}
	receipt := ingressCommandReceiptV1{
		CommandID: command.CommandID, CommandType: command.CommandType, RequestDigest: calculatedDigest,
		SessionID: command.SessionID, ResultSchemaVersion: ingressResultSchemaVersionV1, ResultDigest: resultDigest,
		MessageID: messageID, InputID: input.InputID, TurnID: input.TurnID, EnqueueSeq: input.EnqueueSeq,
		InputVersion: 1, TurnVersion: projection.TurnVersion, MarkerID: command.AllocatedMarkerID, CommittedTick: command.CommittedTick,
	}
	after.Receipts = append(after.Receipts, receipt)
	if command.FailAt == "after_receipt" {
		return before, ingressCommandResultV1{}, reject("SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION", "injected transaction failure")
	}
	result, resultErr := resultFromIngressReceiptV1(after, receipt, "created")
	if resultErr != nil {
		return before, ingressCommandResultV1{}, resultErr
	}
	return after, result, nil
}

func calculateIngressRequestDigestV1(command ingressCommandV1) (string, error) {
	if _, err := parseCanonicalUUIDv7(command.SourceID); err != nil {
		return "", reject("SESSION_COMMAND_INVALID", "source_id")
	}
	if _, err := parseCanonicalUUIDv7(command.AuthorityRefID); err != nil {
		return "", reject("SESSION_COMMAND_INVALID", "authority_ref_id")
	}
	if !ingressSHA256PatternV1.MatchString(command.SourceDigest) || !ingressSHA256PatternV1.MatchString(command.AuthorityRefDigest) {
		return "", reject("SESSION_COMMAND_INVALID", "source/authority digest")
	}
	if command.MessagePresent {
		if !ingressSHA256PatternV1.MatchString(command.ContentDigest) {
			return "", reject("SESSION_COMMAND_INVALID", "content_digest")
		}
	} else if command.ContentDigest != "" {
		return "", reject("SESSION_INPUT_CLASSIFICATION_CONFLICT", "system input content")
	}
	canonical := ingressCanonicalRequestV1{
		SchemaVersion: ingressRequestSchemaVersionV1, CommandType: command.CommandType, SessionID: command.SessionID,
		SourceType: command.SourceType, SourceID: command.SourceID, SourceDigest: command.SourceDigest,
		ExecutionClass: command.ExecutionClass, AuthorityRefType: command.AuthorityRefType,
		AuthorityRefID: command.AuthorityRefID, AuthorityRefDigest: command.AuthorityRefDigest,
		MessagePresent: command.MessagePresent, ContentDigest: command.ContentDigest,
	}
	return digestJSONV1(canonical)
}

func validateIngressClassificationV1(command ingressCommandV1) (string, error) {
	if command.AuthorityRefType == "" {
		return "", reject("SESSION_INPUT_SOURCE_UNTRUSTED", "authority_ref_type")
	}
	switch command.SourceType {
	case "user_message":
		if command.ExecutionClass != "chat" {
			return "", reject("SESSION_INPUT_CLASSIFICATION_CONFLICT", "user_message class")
		}
		if command.AuthorityRefType != "authenticated_user" {
			return "", reject("SESSION_INPUT_SOURCE_UNTRUSTED", "user authority")
		}
		if !command.MessagePresent {
			return "", reject("SESSION_INPUT_CLASSIFICATION_CONFLICT", "user message required")
		}
		return "chat", nil
	case "approval_continuation_result":
		if command.AuthorityRefType != "approval_decision" && command.AuthorityRefType != "approval_invalidation" {
			return "", reject("SESSION_INPUT_SOURCE_UNTRUSTED", "approval authority")
		}
		if command.MessagePresent {
			return "", reject("SESSION_INPUT_CLASSIFICATION_CONFLICT", "approval message forbidden")
		}
		switch command.ExecutionClass {
		case "approval_continuation":
			return "approval_continuation", nil
		case "deterministic_projection":
			return "", nil
		default:
			return "", reject("SESSION_INPUT_CLASSIFICATION_CONFLICT", "approval class")
		}
	case "batch_continuation_result":
		if command.AuthorityRefType != "batch_terminal_event" {
			return "", reject("SESSION_INPUT_SOURCE_UNTRUSTED", "batch authority")
		}
		if command.MessagePresent {
			return "", reject("SESSION_INPUT_CLASSIFICATION_CONFLICT", "batch message forbidden")
		}
		switch command.ExecutionClass {
		case "batch_explanation":
			return "batch_explanation", nil
		case "deterministic_projection":
			return "", nil
		default:
			return "", reject("SESSION_INPUT_CLASSIFICATION_CONFLICT", "batch class")
		}
	default:
		return "", reject("SESSION_INPUT_SOURCE_UNTRUSTED", "source_type")
	}
}

func queryIngressCommandV1(snapshot ingressSnapshotV1, command ingressCommandV1) (ingressSnapshotV1, ingressCommandResultV1, error) {
	if command.SchemaVersion != ingressQuerySchemaVersionV1 {
		return snapshot, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "query schema_version")
	}
	if _, err := parseCanonicalUUIDv7(command.CommandID); err != nil {
		return snapshot, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "query command_id")
	}
	if _, err := parseCanonicalUUIDv7(command.SessionID); err != nil {
		return snapshot, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "query identity")
	}
	if command.ExpectedCommandType != "enqueue_input_v1" || !ingressSHA256PatternV1.MatchString(command.ExpectedRequestDigest) {
		return snapshot, ingressCommandResultV1{}, reject("SESSION_COMMAND_INVALID", "query expected type/digest")
	}
	receipt := findIngressReceiptV1(snapshot.Receipts, command.CommandID)
	if receipt == nil {
		return snapshot, ingressCommandResultV1{QueryStatus: "not_found"}, nil
	}
	if receipt.CommandType != command.ExpectedCommandType || receipt.RequestDigest != command.ExpectedRequestDigest || receipt.SessionID != command.SessionID {
		return snapshot, ingressCommandResultV1{QueryStatus: "conflict"}, nil
	}
	result, err := resultFromIngressReceiptV1(snapshot, *receipt, "")
	if err != nil {
		return snapshot, ingressCommandResultV1{}, err
	}
	result.QueryStatus = "completed"
	return snapshot, result, nil
}

func resultFromIngressReceiptV1(snapshot ingressSnapshotV1, receipt ingressCommandReceiptV1, disposition string) (ingressCommandResultV1, error) {
	if receipt.CommandType != "enqueue_input_v1" {
		return ingressCommandResultV1{}, reject("SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION", "legacy receipt has no ingress result")
	}
	if receipt.ResultSchemaVersion != ingressResultSchemaVersionV1 || receipt.EnqueueSeq <= 0 || receipt.InputVersion <= 0 || receipt.CommittedTick <= 0 ||
		(receipt.TurnID == "" && receipt.TurnVersion != 0) || (receipt.TurnID != "" && receipt.TurnVersion <= 0) {
		return ingressCommandResultV1{}, reject("SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION", "receipt result metadata")
	}
	input := findIngressInputV1(snapshot.Inputs, receipt.InputID)
	marker := findIngressMarkerV1(snapshot.Markers, receipt.MarkerID)
	if input == nil || marker == nil || input.SessionID != receipt.SessionID || marker.SessionID != receipt.SessionID || marker.InputID != input.InputID ||
		input.MessageID != receipt.MessageID || input.TurnID != receipt.TurnID || input.EnqueueSeq != receipt.EnqueueSeq || input.Version < receipt.InputVersion {
		return ingressCommandResultV1{}, reject("SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION", "receipt target refs")
	}
	requestDigest, requestErr := calculateStoredIngressRequestDigestV1(*input)
	if requestErr != nil || subtle.ConstantTimeCompare([]byte(requestDigest), []byte(receipt.RequestDigest)) != 1 {
		return ingressCommandResultV1{}, reject("SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION", "receipt request digest")
	}
	if receipt.MessageID != "" && findIngressMessageV1(snapshot.Messages, receipt.MessageID) == nil {
		return ingressCommandResultV1{}, reject("SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION", "receipt message ref")
	}
	if receipt.TurnID != "" {
		turn := findIngressTurnV1(snapshot.Turns, receipt.TurnID)
		if turn == nil || turn.InputID != receipt.InputID || turn.Version < receipt.TurnVersion {
			return ingressCommandResultV1{}, reject("SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION", "receipt turn ref")
		}
	}
	projection := ingressResultProjectionV1{
		SchemaVersion: receipt.ResultSchemaVersion, SessionID: receipt.SessionID, MessageID: receipt.MessageID,
		InputID: receipt.InputID, TurnID: receipt.TurnID, EnqueueSeq: receipt.EnqueueSeq,
		InputVersion: receipt.InputVersion, TurnVersion: receipt.TurnVersion, MarkerID: receipt.MarkerID,
		CommittedTick: receipt.CommittedTick,
	}
	digest, err := digestJSONV1(projection)
	if err != nil || digest != receipt.ResultDigest {
		return ingressCommandResultV1{}, reject("SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION", "receipt result digest")
	}
	return ingressCommandResultV1{
		Disposition: disposition, MessageID: receipt.MessageID, InputID: receipt.InputID, TurnID: receipt.TurnID,
		EnqueueSeq: receipt.EnqueueSeq, ResultDigest: receipt.ResultDigest,
	}, nil
}

func calculateStoredIngressRequestDigestV1(input ingressInputV1) (string, error) {
	canonical := ingressCanonicalRequestV1{
		SchemaVersion: ingressRequestSchemaVersionV1, CommandType: "enqueue_input_v1", SessionID: input.SessionID,
		SourceType: input.SourceType, SourceID: input.SourceID, SourceDigest: input.SourceDigest,
		ExecutionClass: input.ExecutionClass, AuthorityRefType: input.AuthorityRefType,
		AuthorityRefID: input.AuthorityRefID, AuthorityRefDigest: input.AuthorityRefDigest,
		MessagePresent: input.MessageID != "", ContentDigest: input.ContentDigest,
	}
	return digestJSONV1(canonical)
}

func validateIngressSnapshotV1(snapshot ingressSnapshotV1) error {
	if snapshot.SchemaVersion != ingressSnapshotSchemaVersionV1 {
		return fmt.Errorf("schema_version=%q", snapshot.SchemaVersion)
	}
	sessions := make(map[string]ingressSessionV1, len(snapshot.Sessions))
	for _, session := range snapshot.Sessions {
		if _, err := parseCanonicalUUIDv7(session.SessionID); err != nil || session.Version <= 0 || session.LastMessageSeq < 0 || session.LastEnqueueSeq < 0 || session.LastEventSeq < 0 {
			return fmt.Errorf("非法 session %s", session.SessionID)
		}
		if _, exists := sessions[session.SessionID]; exists {
			return fmt.Errorf("重复 session_id")
		}
		sessions[session.SessionID] = session
	}
	messages := make(map[string]ingressMessageV1, len(snapshot.Messages))
	messageSeqs := make(map[string]map[int64]struct{})
	for _, message := range snapshot.Messages {
		if _, err := parseCanonicalUUIDv7(message.MessageID); err != nil || message.MessageSeq <= 0 || !ingressSHA256PatternV1.MatchString(message.ContentDigest) {
			return fmt.Errorf("非法 message %s", message.MessageID)
		}
		if _, exists := sessions[message.SessionID]; !exists {
			return fmt.Errorf("message 引用未知 session")
		}
		if _, exists := messages[message.MessageID]; exists {
			return fmt.Errorf("重复 message_id")
		}
		if messageSeqs[message.SessionID] == nil {
			messageSeqs[message.SessionID] = map[int64]struct{}{}
		}
		if _, exists := messageSeqs[message.SessionID][message.MessageSeq]; exists {
			return fmt.Errorf("重复 message_seq")
		}
		messageSeqs[message.SessionID][message.MessageSeq] = struct{}{}
		messages[message.MessageID] = message
	}
	inputs := make(map[string]ingressInputV1, len(snapshot.Inputs))
	sources := make(map[string]struct{}, len(snapshot.Inputs))
	inputByMessage := make(map[string]string, len(snapshot.Inputs))
	enqueueSeqs := make(map[string]map[int64]struct{})
	for _, input := range snapshot.Inputs {
		if _, err := parseCanonicalUUIDv7(input.InputID); err != nil || input.EnqueueSeq <= 0 || input.Version <= 0 || !validIngressInputStatusV1(input.Status) {
			return fmt.Errorf("非法 input %s", input.InputID)
		}
		if _, err := parseCanonicalUUIDv7(input.SourceID); err != nil {
			return fmt.Errorf("input source_id 非法")
		}
		if _, exists := sessions[input.SessionID]; !exists || !ingressSHA256PatternV1.MatchString(input.SourceDigest) || !ingressSHA256PatternV1.MatchString(input.AuthorityRefDigest) {
			return fmt.Errorf("input source/session 非法")
		}
		if _, err := parseCanonicalUUIDv7(input.AuthorityRefID); err != nil {
			return fmt.Errorf("input authority ref 非法")
		}
		if _, exists := inputs[input.InputID]; exists {
			return fmt.Errorf("重复 input_id")
		}
		if input.MessageID != "" {
			if inputByMessage[input.MessageID] != "" {
				return fmt.Errorf("一个 Message 不得绑定多个 Input")
			}
			inputByMessage[input.MessageID] = input.InputID
		}
		sourceKey := input.SessionID + "/" + input.SourceType + "/" + input.SourceID
		if _, exists := sources[sourceKey]; exists {
			return fmt.Errorf("重复 source key")
		}
		sources[sourceKey] = struct{}{}
		if enqueueSeqs[input.SessionID] == nil {
			enqueueSeqs[input.SessionID] = map[int64]struct{}{}
		}
		if _, exists := enqueueSeqs[input.SessionID][input.EnqueueSeq]; exists {
			return fmt.Errorf("重复 enqueue_seq")
		}
		enqueueSeqs[input.SessionID][input.EnqueueSeq] = struct{}{}
		inputs[input.InputID] = input
	}
	turns := make(map[string]ingressTurnV1, len(snapshot.Turns))
	turnByInput := make(map[string]string, len(snapshot.Turns))
	for _, turn := range snapshot.Turns {
		if _, err := parseCanonicalUUIDv7(turn.TurnID); err != nil || !validIngressTurnStatusV1(turn.Status) || turn.Version <= 0 || turn.SnapshotCutoffSeq <= 0 {
			return fmt.Errorf("非法 turn %s", turn.TurnID)
		}
		input, exists := inputs[turn.InputID]
		if !exists || input.SessionID != turn.SessionID || input.EnqueueSeq != turn.SnapshotCutoffSeq || input.TurnID != turn.TurnID {
			return fmt.Errorf("turn/input 因果引用不一致")
		}
		if _, exists := turns[turn.TurnID]; exists || turnByInput[turn.InputID] != "" {
			return fmt.Errorf("重复 turn")
		}
		turns[turn.TurnID] = turn
		turnByInput[turn.InputID] = turn.TurnID
	}
	for _, input := range snapshot.Inputs {
		if err := validateStoredIngressMappingV1(input, messages, turns); err != nil {
			return err
		}
	}
	markers := make(map[string]ingressProjectionMarkerV1, len(snapshot.Markers))
	markerByInput := make(map[string]string, len(snapshot.Markers))
	eventSeqs := make(map[string]map[int64]struct{})
	for _, marker := range snapshot.Markers {
		if _, err := parseCanonicalUUIDv7(marker.MarkerID); err != nil || marker.EventSeq <= 0 || marker.MarkerType != "session.input.accepted" {
			return fmt.Errorf("非法 marker %s", marker.MarkerID)
		}
		input, exists := inputs[marker.InputID]
		if !exists || input.SessionID != marker.SessionID {
			return fmt.Errorf("marker/input 因果引用不一致")
		}
		if _, exists := markers[marker.MarkerID]; exists || markerByInput[marker.InputID] != "" {
			return fmt.Errorf("重复 marker")
		}
		if eventSeqs[marker.SessionID] == nil {
			eventSeqs[marker.SessionID] = map[int64]struct{}{}
		}
		if _, exists := eventSeqs[marker.SessionID][marker.EventSeq]; exists {
			return fmt.Errorf("重复 event_seq")
		}
		eventSeqs[marker.SessionID][marker.EventSeq] = struct{}{}
		markers[marker.MarkerID] = marker
		markerByInput[marker.InputID] = marker.MarkerID
	}
	for inputID := range inputs {
		if markerByInput[inputID] == "" {
			return fmt.Errorf("input 缺 accepted marker")
		}
	}
	receiptIDs := make(map[string]struct{}, len(snapshot.Receipts))
	ingressReceiptByInput := make(map[string]int, len(snapshot.Inputs))
	frozenReceiptByInput := make(map[string]ingressCommandReceiptV1, len(snapshot.Inputs))
	for _, receipt := range snapshot.Receipts {
		if _, err := parseCanonicalUUIDv7(receipt.CommandID); err != nil || !ingressSHA256PatternV1.MatchString(receipt.RequestDigest) {
			return fmt.Errorf("非法 receipt %s", receipt.CommandID)
		}
		if _, exists := receiptIDs[receipt.CommandID]; exists {
			return fmt.Errorf("全局 command_id 重复")
		}
		receiptIDs[receipt.CommandID] = struct{}{}
		if _, exists := sessions[receipt.SessionID]; !exists {
			return fmt.Errorf("receipt 引用未知 session")
		}
		if receipt.CommandType == "enqueue_input_v1" {
			if !ingressSHA256PatternV1.MatchString(receipt.ResultDigest) {
				return fmt.Errorf("ingress receipt result_digest 非法")
			}
			if _, err := resultFromIngressReceiptV1(snapshot, receipt, ""); err != nil {
				return fmt.Errorf("ingress receipt 完整性: %w", err)
			}
			normalized := receipt
			normalized.CommandID = ""
			if frozen, exists := frozenReceiptByInput[receipt.InputID]; exists && frozen != normalized {
				return fmt.Errorf("同一 input 的 alias receipt 必须复用冻结结果")
			} else if !exists {
				frozenReceiptByInput[receipt.InputID] = normalized
			}
			ingressReceiptByInput[receipt.InputID]++
		} else if receipt.CommandType != "ensure_project_session_v1" && receipt.CommandType != "ensure_project_session_v2" {
			return fmt.Errorf("未知 command_type")
		}
	}
	for inputID := range inputs {
		if ingressReceiptByInput[inputID] < 1 {
			return fmt.Errorf("input 必须至少对应一条创建或 alias receipt")
		}
	}
	for sessionID, session := range sessions {
		if int64(len(messageSeqs[sessionID])) != session.LastMessageSeq || int64(len(enqueueSeqs[sessionID])) != session.LastEnqueueSeq || int64(len(eventSeqs[sessionID])) != session.LastEventSeq {
			return fmt.Errorf("session counter 不连续")
		}
		for seq := int64(1); seq <= session.LastMessageSeq; seq++ {
			if _, exists := messageSeqs[sessionID][seq]; !exists {
				return fmt.Errorf("message_seq 存在空洞")
			}
		}
		for seq := int64(1); seq <= session.LastEnqueueSeq; seq++ {
			if _, exists := enqueueSeqs[sessionID][seq]; !exists {
				return fmt.Errorf("enqueue_seq 存在空洞")
			}
		}
		for seq := int64(1); seq <= session.LastEventSeq; seq++ {
			if _, exists := eventSeqs[sessionID][seq]; !exists {
				return fmt.Errorf("event_seq 存在空洞")
			}
		}
	}
	return nil
}

func validIngressInputStatusV1(status string) bool {
	switch status {
	case "pending", "claimed", "running", "retry_wait", "quarantined", "resolved", "dead":
		return true
	default:
		return false
	}
}

func validIngressTurnStatusV1(status string) bool {
	switch status {
	case "created", "running", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func validateStoredIngressMappingV1(input ingressInputV1, messages map[string]ingressMessageV1, turns map[string]ingressTurnV1) error {
	switch input.SourceType {
	case "user_message":
		if input.ExecutionClass != "chat" || input.MessageID == "" || input.TurnID == "" || !ingressSHA256PatternV1.MatchString(input.ContentDigest) || input.AuthorityRefType != "authenticated_user" {
			return fmt.Errorf("user input 映射非法")
		}
		message, messageExists := messages[input.MessageID]
		turn, turnExists := turns[input.TurnID]
		if !messageExists || message.SessionID != input.SessionID || message.ContentDigest != input.ContentDigest || !turnExists || turn.TurnKind != "chat" {
			return fmt.Errorf("user input Message/Turn 引用非法")
		}
	case "approval_continuation_result":
		if input.MessageID != "" || input.ContentDigest != "" || (input.AuthorityRefType != "approval_decision" && input.AuthorityRefType != "approval_invalidation") {
			return fmt.Errorf("approval input 映射非法")
		}
		if input.ExecutionClass == "approval_continuation" {
			turn, exists := turns[input.TurnID]
			if !exists || turn.TurnKind != "approval_continuation" {
				return fmt.Errorf("approval continuation Turn 非法")
			}
		} else if input.ExecutionClass != "deterministic_projection" || input.TurnID != "" {
			return fmt.Errorf("approval projection 映射非法")
		}
	case "batch_continuation_result":
		if input.MessageID != "" || input.ContentDigest != "" || input.AuthorityRefType != "batch_terminal_event" {
			return fmt.Errorf("batch input 映射非法")
		}
		if input.ExecutionClass == "batch_explanation" {
			turn, exists := turns[input.TurnID]
			if !exists || turn.TurnKind != "batch_explanation" {
				return fmt.Errorf("batch explanation Turn 非法")
			}
		} else if input.ExecutionClass != "deterministic_projection" || input.TurnID != "" {
			return fmt.Errorf("batch projection 映射非法")
		}
	default:
		return fmt.Errorf("未知 source_type")
	}
	return nil
}

func summarizeIngressStateV1(snapshot ingressSnapshotV1, sessionID string) ingressStateSummaryV1 {
	var summary ingressStateSummaryV1
	var lastInputSeq int64
	if session := findIngressSessionPtrV1(&snapshot, sessionID); session != nil {
		summary.SessionVersion = session.Version
		summary.LastMessageSeq = session.LastMessageSeq
		summary.LastEnqueueSeq = session.LastEnqueueSeq
		summary.LastEventSeq = session.LastEventSeq
	}
	for _, message := range snapshot.Messages {
		if message.SessionID == sessionID {
			summary.MessageCount++
		}
	}
	for _, input := range snapshot.Inputs {
		if input.SessionID != sessionID {
			continue
		}
		summary.InputCount++
		if input.EnqueueSeq >= lastInputSeq {
			summary.LastInputID = input.InputID
			summary.LastSourceType = input.SourceType
			summary.LastClass = input.ExecutionClass
			lastInputSeq = input.EnqueueSeq
		}
	}
	for _, turn := range snapshot.Turns {
		if turn.SessionID == sessionID {
			summary.TurnCount++
			if turn.SnapshotCutoffSeq >= summary.LastTurnCutoff {
				summary.LastTurnKind = turn.TurnKind
				summary.LastTurnCutoff = turn.SnapshotCutoffSeq
			}
		}
	}
	for _, receipt := range snapshot.Receipts {
		if receipt.SessionID == sessionID {
			summary.ReceiptCount++
		}
	}
	for _, marker := range snapshot.Markers {
		if marker.SessionID == sessionID {
			summary.MarkerCount++
		}
	}
	return summary
}

func digestJSONV1(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func cloneIngressSnapshotV1(snapshot ingressSnapshotV1) ingressSnapshotV1 {
	clone := snapshot
	clone.Sessions = append([]ingressSessionV1(nil), snapshot.Sessions...)
	clone.Messages = append([]ingressMessageV1(nil), snapshot.Messages...)
	clone.Inputs = append([]ingressInputV1(nil), snapshot.Inputs...)
	clone.Turns = append([]ingressTurnV1(nil), snapshot.Turns...)
	clone.Receipts = append([]ingressCommandReceiptV1(nil), snapshot.Receipts...)
	clone.Markers = append([]ingressProjectionMarkerV1(nil), snapshot.Markers...)
	return clone
}

func findIngressSessionPtrV1(snapshot *ingressSnapshotV1, sessionID string) *ingressSessionV1 {
	for index := range snapshot.Sessions {
		if snapshot.Sessions[index].SessionID == sessionID {
			return &snapshot.Sessions[index]
		}
	}
	return nil
}

func findIngressReceiptV1(receipts []ingressCommandReceiptV1, commandID string) *ingressCommandReceiptV1 {
	for index := range receipts {
		if receipts[index].CommandID == commandID {
			return &receipts[index]
		}
	}
	return nil
}

func findIngressReceiptByInputV1(receipts []ingressCommandReceiptV1, inputID string) *ingressCommandReceiptV1 {
	for index := range receipts {
		if receipts[index].CommandType == "enqueue_input_v1" && receipts[index].InputID == inputID {
			return &receipts[index]
		}
	}
	return nil
}

func findIngressInputV1(inputs []ingressInputV1, inputID string) *ingressInputV1 {
	for index := range inputs {
		if inputs[index].InputID == inputID {
			return &inputs[index]
		}
	}
	return nil
}

func findIngressInputBySourceV1(inputs []ingressInputV1, sessionID, sourceType, sourceID string) *ingressInputV1 {
	for index := range inputs {
		if inputs[index].SessionID == sessionID && inputs[index].SourceType == sourceType && inputs[index].SourceID == sourceID {
			return &inputs[index]
		}
	}
	return nil
}

func findIngressMessageV1(messages []ingressMessageV1, messageID string) *ingressMessageV1 {
	for index := range messages {
		if messages[index].MessageID == messageID {
			return &messages[index]
		}
	}
	return nil
}

func findIngressTurnV1(turns []ingressTurnV1, turnID string) *ingressTurnV1 {
	for index := range turns {
		if turns[index].TurnID == turnID {
			return &turns[index]
		}
	}
	return nil
}

func findIngressMarkerV1(markers []ingressProjectionMarkerV1, markerID string) *ingressProjectionMarkerV1 {
	for index := range markers {
		if markers[index].MarkerID == markerID {
			return &markers[index]
		}
	}
	return nil
}
