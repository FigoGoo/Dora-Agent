package agentsessionrpc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/sessionv1"
	"github.com/bytedance/gopkg/cloud/metainfo"
	"github.com/cloudwego/kitex/client/callopt"
	"github.com/google/uuid"
)

type fakeProtocol struct {
	ensureContext context.Context
	ensureRequest *sessionv1.EnsureProjectSessionRequestV1
	ensureResult  *sessionv1.EnsureProjectSessionResponseV1
	ensureErr     error
	queryContext  context.Context
	queryRequest  *sessionv1.QueryProjectSessionCommandRequestV1
	queryResult   *sessionv1.QueryProjectSessionCommandResponseV1
	queryErr      error
}

func (protocol *fakeProtocol) EnsureProjectSessionV1(ctx context.Context, request *sessionv1.EnsureProjectSessionRequestV1, _ ...callopt.Option) (*sessionv1.EnsureProjectSessionResponseV1, error) {
	protocol.ensureContext = ctx
	protocol.ensureRequest = request
	return protocol.ensureResult, protocol.ensureErr
}
func (protocol *fakeProtocol) QueryProjectSessionCommandV1(ctx context.Context, request *sessionv1.QueryProjectSessionCommandRequestV1, _ ...callopt.Option) (*sessionv1.QueryProjectSessionCommandResponseV1, error) {
	protocol.queryContext = ctx
	protocol.queryRequest = request
	return protocol.queryResult, protocol.queryErr
}

type fixedIDGenerator struct{ value string }

func (generator fixedIDGenerator) New() (string, error) { return generator.value, nil }

var testRPCAuthSecret = []byte("fedcba9876543210fedcba9876543210")

func testAuthenticator(t *testing.T) *requestAuthenticator {
	t.Helper()
	authenticator, err := newRequestAuthenticator(testRPCAuthSecret, func() time.Time {
		return time.Date(2026, 7, 14, 6, 45, 0, 123000000, time.UTC)
	})
	if err != nil {
		t.Fatalf("newRequestAuthenticator: %v", err)
	}
	return authenticator
}

func ensureRequestForTest(t *testing.T, prompt string) project.EnsureSessionRequest {
	t.Helper()
	requestID, _ := uuid.NewV7()
	commandID, _ := uuid.NewV7()
	projectID, _ := uuid.NewV7()
	ownerID, _ := uuid.NewV7()
	normalized, promptDigest, present, err := project.NormalizeEnsureSessionPrompt(prompt)
	if err != nil {
		t.Fatal(err)
	}
	requestDigest, err := project.CalculateEnsureSessionRequestDigest(projectID.String(), ownerID.String(), present, promptDigest)
	if err != nil {
		t.Fatal(err)
	}
	return project.EnsureSessionRequest{
		RequestID: requestID.String(), CommandID: commandID.String(), RequestDigest: requestDigest,
		ProjectID: projectID.String(), OwnerUserID: ownerID.String(), InitialPrompt: normalized,
		PromptPresent: present, PromptDigest: promptDigest, RequestedAt: time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC),
	}
}

func receiptForTest(t *testing.T, request project.EnsureSessionRequest) *sessionv1.ProjectSessionReceiptV1 {
	t.Helper()
	sessionID, _ := uuid.NewV7()
	receipt := &sessionv1.ProjectSessionReceiptV1{
		CommandId: request.CommandID, SessionId: sessionID.String(), ResultVersion: 1,
		CompletedAtUnixMs: time.Date(2026, 7, 14, 4, 0, 1, 0, time.UTC).UnixMilli(),
	}
	if request.PromptPresent {
		messageID, _ := uuid.NewV7()
		inputID, _ := uuid.NewV7()
		receipt.MessageId = stringPointer(messageID.String())
		receipt.InputId = stringPointer(inputID.String())
	}
	return receipt
}

func TestClientEnsureMapsFrozenContract(t *testing.T) {
	request := ensureRequestForTest(t, " e\u0301 ")
	protocol := &fakeProtocol{}
	protocol.ensureResult = &sessionv1.EnsureProjectSessionResponseV1{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION, RequestId: request.RequestID,
		Disposition: sessionv1.EnsureDispositionV1_CREATED, Receipt: receiptForTest(t, request),
	}
	client := &Client{protocol: protocol, config: ClientConfig{RequestTimeout: time.Second}, auth: testAuthenticator(t)}

	receipt, err := client.Ensure(context.Background(), request)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if protocol.ensureRequest.InitialPrompt == nil || *protocol.ensureRequest.InitialPrompt != " é " ||
		protocol.ensureRequest.RequestDigest != request.RequestDigest.Hex() || protocol.ensureRequest.PromptDigest != request.PromptDigest.Hex() ||
		receipt.CommandID != request.CommandID || receipt.RequestDigest != request.RequestDigest || receipt.InputID == nil {
		t.Fatalf("contract mapping drifted: protocol=%+v receipt=%+v", protocol.ensureRequest, receipt)
	}
}

func TestClientEnsureRejectsMalformedReceiptAndRedactsRPCError(t *testing.T) {
	request := ensureRequestForTest(t, "")
	protocol := &fakeProtocol{ensureResult: &sessionv1.EnsureProjectSessionResponseV1{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION, RequestId: request.RequestID,
		Disposition: sessionv1.EnsureDispositionV1_CREATED, Receipt: &sessionv1.ProjectSessionReceiptV1{},
	}}
	client := &Client{protocol: protocol, config: ClientConfig{RequestTimeout: time.Second}, auth: testAuthenticator(t)}
	if _, err := client.Ensure(context.Background(), request); !errors.Is(err, project.ErrInvalidAgentReceipt) {
		t.Fatalf("expected invalid receipt, got %v", err)
	}
	protocol.ensureErr = errors.New("dial agent.internal secret-token")
	if _, err := client.Ensure(context.Background(), request); err != project.ErrAgentSessionUnavailable {
		t.Fatalf("RPC error was not redacted: %v", err)
	}
}

func TestClientQueryMapsThreeStatesAndExpectedDigest(t *testing.T) {
	request := ensureRequestForTest(t, "prompt")
	queryID, _ := uuid.NewV7()
	protocol := &fakeProtocol{}
	client := &Client{
		protocol: protocol, config: ClientConfig{RequestTimeout: time.Second},
		idgen: fixedIDGenerator{value: queryID.String()}, auth: testAuthenticator(t),
	}

	protocol.queryResult = &sessionv1.QueryProjectSessionCommandResponseV1{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION, RequestId: queryID.String(),
		Status: sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND,
	}
	result, err := client.Query(context.Background(), request.CommandID, request.RequestDigest)
	if err != nil || result.Status != project.QueryStatusNotFound || protocol.queryRequest.ExpectedRequestDigest != request.RequestDigest.Hex() {
		t.Fatalf("map not found: result=%+v request=%+v err=%v", result, protocol.queryRequest, err)
	}

	protocol.queryResult.Status = sessionv1.QueryProjectSessionCommandStatusV1_COMPLETED
	protocol.queryResult.Receipt = receiptForTest(t, request)
	result, err = client.Query(context.Background(), request.CommandID, request.RequestDigest)
	if err != nil || result.Status != project.QueryStatusCompleted || result.Receipt == nil || result.Receipt.RequestDigest != request.RequestDigest {
		t.Fatalf("map completed: result=%+v err=%v", result, err)
	}

	protocol.queryResult.Status = sessionv1.QueryProjectSessionCommandStatusV1_CONFLICT
	protocol.queryResult.Receipt = nil
	result, err = client.Query(context.Background(), request.CommandID, request.RequestDigest)
	if err != nil || result.Status != project.QueryStatusConflict {
		t.Fatalf("map conflict: result=%+v err=%v", result, err)
	}
}

func TestClientBindsServiceAuthenticationToEnsureSemanticRequest(t *testing.T) {
	request := ensureRequestForTest(t, "prompt")
	protocol := &fakeProtocol{ensureResult: &sessionv1.EnsureProjectSessionResponseV1{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION,
		RequestId:     request.RequestID,
		Disposition:   sessionv1.EnsureDispositionV1_CREATED,
		Receipt:       receiptForTest(t, request),
	}}
	client := &Client{protocol: protocol, config: ClientConfig{RequestTimeout: time.Second}, auth: testAuthenticator(t)}
	if _, err := client.Ensure(context.Background(), request); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	caller, callerOK := metainfo.GetValue(protocol.ensureContext, authCallerMetadataKey)
	issuedAt, issuedAtOK := metainfo.GetValue(protocol.ensureContext, authIssuedAtMetadataKey)
	signature, signatureOK := metainfo.GetValue(protocol.ensureContext, authSignatureMetadataKey)
	if !callerOK || !issuedAtOK || !signatureOK || caller != serviceAuthCaller || issuedAt != "1784011500123" {
		t.Fatalf("service auth metadata missing or drifted: caller=%q issued_at=%q", caller, issuedAt)
	}
	canonical := strings.Join([]string{
		serviceAuthSchema, serviceAuthCaller, serviceAuthTarget, "EnsureProjectSessionV1",
		request.RequestID, request.CommandID, request.RequestDigest.Hex(), issuedAt,
	}, "\n")
	mac := hmac.New(sha256.New, testRPCAuthSecret)
	_, _ = mac.Write([]byte(canonical))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		t.Fatalf("signature does not bind frozen semantic request: got %q want %q", signature, expected)
	}
}

func TestClientMapsStableServiceExceptions(t *testing.T) {
	for code, expected := range map[string]error{
		"IDEMPOTENCY_CONFLICT":     project.ErrAgentSessionConflict,
		"PROJECT_SESSION_CONFLICT": project.ErrAgentSessionConflict,
		"COMMAND_CONFLICT":         project.ErrAgentSessionConflict,
		"COMMAND_VERSION_CONFLICT": project.ErrAgentSessionConflict,
		"INVALID_ARGUMENT":         project.ErrAgentSessionInvalid,
		"SNAPSHOT_DIGEST_MISMATCH": project.ErrAgentSessionInvalid,
		"SNAPSHOT_LIMIT_EXCEEDED":  project.ErrAgentSessionInvalid,
		"PERSISTENCE_UNAVAILABLE":  project.ErrAgentSessionUnavailable,
	} {
		err := mapAgentServiceError(&sessionv1.SessionServiceExceptionV1{Code: code, Message: "safe", RequestId: "", Retryable: true})
		if !errors.Is(err, expected) {
			t.Fatalf("code %s: expected %v, got %v", code, expected, err)
		}
	}
}

func stringPointer(value string) *string { return &value }
