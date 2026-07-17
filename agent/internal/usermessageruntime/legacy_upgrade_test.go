package usermessageruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
)

func TestLegacyUpgradeBlockerMakesWholeBatchZeroWrite(t *testing.T) {
	prompt := "legacy preview prompt"
	good := legacyUpgradeTestCandidate(t, prompt)
	blocked := good
	blocked.InputID = "blocked-input"
	blocked.EnqueueSeq = 2
	repository := &legacyUpgradeFakeRepository{preview: LegacyUpgradePreview{Candidates: []LegacyUpgradeCandidate{good, blocked}}}
	service := legacyUpgradeTestService(t, repository, prompt)

	preview, err := service.UpgradeBatch(context.Background(), 1)
	if !errors.Is(err, ErrLegacyUpgradeBlocked) || len(preview.Blockers) != 1 || preview.Blockers[0].Code != "not_first_input" {
		t.Fatalf("blocker 结果漂移: preview=%+v err=%v", preview, err)
	}
	if repository.writes != 0 {
		t.Fatalf("全批 preflight 遇到分页后 blocker 仍发生写入: writes=%d", repository.writes)
	}
}

func TestLegacyUpgradeCryptographicBlockerMakesWholeBatchZeroWrite(t *testing.T) {
	prompt := "legacy preview prompt"
	good := legacyUpgradeTestCandidate(t, prompt)
	blocked := good
	blocked.InputID = "blocked-input"
	blocked.ReceiptInputID = blocked.InputID
	blocked.MessageDigest = strings.Repeat("a", 64)
	repository := &legacyUpgradeFakeRepository{preview: LegacyUpgradePreview{Candidates: []LegacyUpgradeCandidate{good, blocked}}}
	service := legacyUpgradeTestService(t, repository, prompt)

	preview, err := service.UpgradeBatch(context.Background(), 1)
	if !errors.Is(err, ErrLegacyUpgradeBlocked) || len(preview.Blockers) != 1 || preview.Blockers[0].Code != "message_digest_mismatch" {
		t.Fatalf("密文 blocker 结果漂移: preview=%+v err=%v", preview, err)
	}
	if repository.writes != 0 {
		t.Fatalf("整批密文预检遇到 blocker 仍发生写入: writes=%d", repository.writes)
	}
}

func TestLegacyUpgradeAbsentConvergesPreparedAppliedVerified(t *testing.T) {
	prompt := "legacy preview prompt"
	candidate := legacyUpgradeTestCandidate(t, prompt)
	repository := &legacyUpgradeFakeRepository{preview: LegacyUpgradePreview{Candidates: []LegacyUpgradeCandidate{candidate}}}
	service := legacyUpgradeTestService(t, repository, prompt)

	if _, err := service.UpgradeBatch(context.Background(), 10); err != nil {
		t.Fatalf("legacy helper 未收敛: %v", err)
	}
	if repository.writes != 3 || repository.stage != LegacyUpgradeStageVerified {
		t.Fatalf("状态机调用漂移: writes=%d stage=%s", repository.writes, repository.stage)
	}
	if repository.turn.OutputID == "" || repository.turn.ModelCallID == "" || repository.turn.RecoveryEventID == "" || repository.turn.TerminalEventID == "" {
		t.Fatalf("applied 未冻结稳定 Turn 标识: %+v", repository.turn)
	}
}

type legacyUpgradeFakeRepository struct {
	preview LegacyUpgradePreview
	writes  int
	stage   string
	ledger  LegacyUpgradeLedger
	turn    session.UserMessageTurn
}

func (r *legacyUpgradeFakeRepository) Preview(context.Context, int) (LegacyUpgradePreview, error) {
	return r.preview, nil
}
func (r *legacyUpgradeFakeRepository) Prepare(_ context.Context, p LegacyUpgradePreparePlan) (LegacyUpgradeLedger, error) {
	r.writes++
	r.stage = LegacyUpgradeStagePrepared
	r.ledger = LegacyUpgradeLedger{InputID: p.Candidate.InputID, SessionID: p.Candidate.SessionID, Stage: r.stage,
		TurnID: p.TurnID, ContextDigest: p.ContextDigest, UpgradeGeneration: 1, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	return r.ledger, nil
}
func (r *legacyUpgradeFakeRepository) Apply(_ context.Context, p LegacyUpgradeApplyPlan) (LegacyUpgradeLedger, error) {
	r.writes++
	r.stage = LegacyUpgradeStageApplied
	r.turn = p.Turn
	r.ledger.Stage = r.stage
	r.ledger.Version++
	return r.ledger, nil
}
func (r *legacyUpgradeFakeRepository) Verify(_ context.Context, _ LegacyUpgradeVerifyPlan) (LegacyUpgradeLedger, error) {
	r.writes++
	r.stage = LegacyUpgradeStageVerified
	r.ledger.Stage = r.stage
	r.ledger.Version++
	return r.ledger, nil
}

type legacyUpgradeTestOpener struct{ plaintext string }

func (o legacyUpgradeTestOpener) Open(context.Context, session.ProtectedContent, string) ([]byte, error) {
	return []byte(o.plaintext), nil
}

type legacyUpgradeTestSnapshots struct{}

func (legacyUpgradeTestSnapshots) LoadSessionSkillSnapshotsV1(
	_ context.Context,
	sessionIDs []string,
) (map[string]session.LoadedSkillSnapshotV1, error) {
	result := make(map[string]session.LoadedSkillSnapshotV1, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		result[sessionID] = session.LoadedSkillSnapshotV1{SessionID: sessionID, Snapshot: skill.SessionSkillSnapshotV1{
			SchemaVersion: skill.SnapshotSchemaVersionV1, SnapshotKind: skill.SessionSkillSnapshotKindEmptyV1,
			SkillCount: 0, SnapshotSetDigest: skill.EmptySnapshotSetDigestHex, Skills: []skill.PublishedSkillSnapshotRefV1{},
		}}
	}
	return result, nil
}

type legacyUpgradeTestIDs struct{ next int }

func (g *legacyUpgradeTestIDs) New() (string, error) {
	g.next++
	return "id-" + string(rune('0'+g.next)), nil
}

func legacyUpgradeTestService(t *testing.T, repository LegacyUpgradeRepository, prompt string) *LegacyUpgradeService {
	t.Helper()
	service, err := NewLegacyUpgradeService(repository, legacyUpgradeTestOpener{plaintext: prompt}, legacyUpgradeTestSnapshots{},
		&legacyUpgradeTestIDs{}, skill.DefaultLimitsProfileV1())
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func legacyUpgradeTestCandidate(t *testing.T, prompt string) LegacyUpgradeCandidate {
	t.Helper()
	projectID := "019f0000-0000-7000-8000-0000000000ab"
	userID := "019f0000-0000-7000-8000-0000000000cd"
	request, digest, present, err := session.CalculateRequestDigest(projectID, userID, prompt, session.SkillSnapshotKindEmpty)
	if err != nil || !present {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(prompt))
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("prompt digest mismatch")
	}
	return LegacyUpgradeCandidate{
		CommandID: "command", CommandType: session.CommandTypeEnsureProjectSessionV1, RequestDigest: request,
		ReceiptSessionID: "session", ReceiptMessageID: "message", ReceiptInputID: "input",
		ReceiptSkillDigest: session.EmptySkillSnapshotDigest, ReceiptSkillCount: 0,
		SessionID: "session", ProjectID: projectID, UserID: userID, Status: "active",
		InputID: "input", InputSource: "user_message", InputSourceID: "command", InputMessageID: "message",
		InputStatus: "pending", EnqueueSeq: 1, Attempts: 0, InputLeaseIdle: true, InputFence: 0,
		LeaseIdle: true, LeaseFence: 0, MessageID: "message", MessageSeq: 1, MessageRole: "user",
		MessageSourceKind: "ensure_project_session", MessageSourceID: "command", MessageDigest: digest,
		MessageProtected: session.ProtectedContent{Ciphertext: []byte{1}, KeyVersion: "v1"},
		SnapshotKind:     session.SkillSnapshotKindEmpty, SnapshotDigest: session.EmptySkillSnapshotDigest,
		SnapshotCount: 0, AcceptedEventValid: true,
	}
}
