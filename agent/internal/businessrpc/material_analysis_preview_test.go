package businessrpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
	"github.com/cloudwego/kitex/client/callopt"
)

const (
	materialAnalysisTestRequestID  = "019f68e8-1010-7000-8000-000000000010"
	materialAnalysisTestUserID     = "019f68e8-1011-7000-8000-000000000011"
	materialAnalysisTestProjectID  = "019f68e8-1012-7000-8000-000000000012"
	materialAnalysisTestTextAsset  = "019f68e8-2010-7000-8000-000000000010"
	materialAnalysisTestImageAsset = "019f68e8-2020-7000-8000-000000000020"
	materialAnalysisTestExtraAsset = "019f68e8-2030-7000-8000-000000000030"
)

type materialAnalysisProtocolStub struct {
	calls   int
	request *foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1
	invoke  func(context.Context, *foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error)
}

func (stub *materialAnalysisProtocolStub) BatchGetAssetAnalysisInputsPreviewV1(
	ctx context.Context,
	request *foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
	_ ...callopt.Option,
) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
	stub.calls++
	stub.request = request
	return stub.invoke(ctx, request)
}

type materialAnalysisIDGenerator struct {
	id  string
	err error
}

func (generator materialAnalysisIDGenerator) New() (string, error) {
	return generator.id, generator.err
}

// TestMaterialAnalysisPreviewMapsExactContractAndOwnsCopies 钉住一次 RPC、optional version、全部枚举/locator 和双向无 alias。
func TestMaterialAnalysisPreviewMapsExactContractAndOwnsCopies(t *testing.T) {
	response := materialAnalysisProtocolResponse()
	stub := &materialAnalysisProtocolStub{invoke: func(
		ctx context.Context,
		_ *foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
	) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			t.Fatal("Evidence RPC 缺少显式 context timeout")
		}
		return response, nil
	}}
	client := materialAnalysisClient(stub, time.Second)
	query := materialAnalysisQuery()

	snapshot, err := client.BatchGetAssetAnalysisInputs(context.Background(), query)
	if err != nil {
		t.Fatalf("加载 Evidence: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("RPC 调用次数=%d want=1", stub.calls)
	}
	if stub.request == nil || stub.request.SchemaVersion != foundationv1.ASSET_ANALYSIS_INPUTS_PREVIEW_RPC_SCHEMA_VERSION ||
		stub.request.RequestId != materialAnalysisTestRequestID || stub.request.UserId != materialAnalysisTestUserID ||
		stub.request.ProjectId != materialAnalysisTestProjectID || len(stub.request.Targets) != 2 {
		t.Fatalf("请求映射错误: %+v", stub.request)
	}
	if stub.request.Targets[0].AssetId != materialAnalysisTestTextAsset ||
		stub.request.Targets[0].ExpectedAssetVersion != nil ||
		stub.request.Targets[1].AssetId != materialAnalysisTestImageAsset ||
		stub.request.Targets[1].ExpectedAssetVersion == nil || *stub.request.Targets[1].ExpectedAssetVersion != 3 {
		t.Fatalf("target 排序或 optional expected version 映射错误: %+v", stub.request.Targets)
	}
	if snapshot.SchemaVersion != analyzematerials.EvidenceSnapshotSchemaVersion ||
		snapshot.SnapshotToken != "snapshot-preview-1" || !snapshot.ResponseComplete || len(snapshot.Assets) != 2 {
		t.Fatalf("快照 envelope 映射错误: %+v", snapshot)
	}
	if got := snapshot.Assets[0].Evidence[0]; got.MediaType != "text" || got.EvidenceKind != "text_segment" ||
		got.Availability != "ready" || got.Locator.Kind != "text_range" || got.Locator.Start != 0 ||
		got.Locator.End != 8 || got.Locator.SourceLength != 20 {
		t.Fatalf("text Evidence 映射错误: %+v", got)
	}
	imageEvidence := snapshot.Assets[1].Evidence
	if len(imageEvidence) != 6 || imageEvidence[0].EvidenceKind != "visual_description" ||
		imageEvidence[0].Locator.Kind != "image_whole" || imageEvidence[1].EvidenceKind != "safety_label" ||
		imageEvidence[1].Locator.Kind != "image_region" || imageEvidence[1].Locator.X != 100 ||
		imageEvidence[1].Locator.Y != 200 || imageEvidence[1].Locator.Width != 300 || imageEvidence[1].Locator.Height != 400 {
		t.Fatalf("image Evidence/locator 映射错误: %+v", imageEvidence)
	}
	availabilities := []string{imageEvidence[0].Availability, imageEvidence[1].Availability, imageEvidence[2].Availability,
		imageEvidence[3].Availability, imageEvidence[4].Availability, imageEvidence[5].Availability}
	if strings.Join(availabilities, ",") != "ready,ready,missing,failed,redacted,unsupported" {
		t.Fatalf("availability 映射错误: %v", availabilities)
	}

	query.Targets[0].AssetID = materialAnalysisTestExtraAsset
	if stub.request.Targets[1].AssetId != materialAnalysisTestImageAsset {
		t.Fatal("请求 target 与内部 query 发生 alias")
	}
	originalAssetID := snapshot.Assets[0].AssetID
	originalContent := snapshot.Assets[0].Evidence[0].Content
	originalEnd := snapshot.Assets[0].Evidence[0].Locator.End
	response.Assets[0].AssetId = materialAnalysisTestExtraAsset
	*response.Assets[0].Evidence[0].Content = "响应已修改"
	*response.Assets[0].Evidence[0].Locator.TextEnd = 19
	if snapshot.Assets[0].AssetID != originalAssetID || snapshot.Assets[0].Evidence[0].Content != originalContent ||
		snapshot.Assets[0].Evidence[0].Locator.End != originalEnd {
		t.Fatal("返回快照与 Kitex response 发生 alias")
	}
	snapshot.Assets[1].Evidence[1].Content = "内部快照已修改"
	snapshot.Assets[1].Evidence[1].Locator.Width = 999
	if *response.Assets[1].Evidence[1].Content == "内部快照已修改" || *response.Assets[1].Evidence[1].Locator.ImageWidth == 999 {
		t.Fatal("Kitex response 与返回快照发生反向 alias")
	}
}

// TestMaterialAnalysisPreviewServiceCodeMappingIsExactAndSafe 钉住服务码分类，Retryable 与原始 Message 均不得改变或泄漏 Agent 结果。
func TestMaterialAnalysisPreviewServiceCodeMappingIsExactAndSafe(t *testing.T) {
	testCases := []struct {
		code string
		want string
	}{
		{code: "NOT_FOUND", want: analyzematerials.ResultCodeMaterialsNotAvailable},
		{code: "ASSET_ANALYSIS_VERSION_CONFLICT", want: analyzematerials.ResultCodeSnapshotInvalid},
		{code: "LIMIT_EXCEEDED", want: analyzematerials.ResultCodeSnapshotInvalid},
		{code: "ASSET_ANALYSIS_EVIDENCE_CONFLICT", want: analyzematerials.ResultCodeEvidenceConflict},
		{code: "FEATURE_DISABLED", want: analyzematerials.ResultCodeInternal},
		{code: "PREVIEW_UNAVAILABLE", want: analyzematerials.ResultCodeInternal},
		{code: "PERSISTENCE_UNAVAILABLE", want: analyzematerials.ResultCodeInternal},
		{code: "INVALID_ARGUMENT", want: analyzematerials.ResultCodeInternal},
		{code: "UNRECOGNIZED", want: analyzematerials.ResultCodeInternal},
	}
	for _, fixture := range testCases {
		for _, retryable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/retryable=%t", fixture.code, retryable), func(t *testing.T) {
				serviceError := &foundationv1.FoundationServiceExceptionV1{
					Code: fixture.code, Message: "secret-business-evidence-detail", Retryable: retryable,
				}
				stub := &materialAnalysisProtocolStub{invoke: func(
					context.Context,
					*foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
				) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
					return nil, serviceError
				}}
				_, err := materialAnalysisClient(stub, time.Second).BatchGetAssetAnalysisInputs(context.Background(), materialAnalysisQuery())
				if analyzematerials.ErrorResultCode(err) != fixture.want {
					t.Fatalf("结果码=%q want=%q err=%v", analyzematerials.ErrorResultCode(err), fixture.want, err)
				}
				if err == nil || strings.Contains(err.Error(), serviceError.Message) || strings.Contains(err.Error(), fixture.code) {
					t.Fatalf("原始 ServiceException 泄漏: %v", err)
				}
				if stub.calls != 1 {
					t.Fatalf("RPC 调用次数=%d want=1", stub.calls)
				}
			})
		}
	}
}

// TestMaterialAnalysisPreviewTransportAndContextErrors 验证技术错误安全折叠，父取消和 Adapter 超时保持 context 语义且不重试。
func TestMaterialAnalysisPreviewTransportAndContextErrors(t *testing.T) {
	t.Run("transport is internal and safe", func(t *testing.T) {
		stub := &materialAnalysisProtocolStub{invoke: func(
			context.Context,
			*foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
		) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
			return nil, errors.New("secret transport reset")
		}}
		_, err := materialAnalysisClient(stub, time.Second).BatchGetAssetAnalysisInputs(context.Background(), materialAnalysisQuery())
		if analyzematerials.ErrorResultCode(err) != analyzematerials.ResultCodeInternal || strings.Contains(err.Error(), "secret") {
			t.Fatalf("transport error 未安全折叠: %v", err)
		}
		if stub.calls != 1 {
			t.Fatalf("RPC 调用次数=%d want=1", stub.calls)
		}
	})

	for _, fixture := range []struct {
		name string
		err  error
		want error
	}{
		{name: "rpc canceled", err: fmt.Errorf("secret wrapped: %w", context.Canceled), want: context.Canceled},
		{name: "rpc deadline", err: fmt.Errorf("secret wrapped: %w", context.DeadlineExceeded), want: context.DeadlineExceeded},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			stub := &materialAnalysisProtocolStub{invoke: func(
				context.Context,
				*foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
			) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
				return nil, fixture.err
			}}
			_, err := materialAnalysisClient(stub, time.Second).BatchGetAssetAnalysisInputs(context.Background(), materialAnalysisQuery())
			if err != fixture.want || stub.calls != 1 {
				t.Fatalf("context error=%v want=%v calls=%d", err, fixture.want, stub.calls)
			}
		})
	}

	t.Run("adapter timeout", func(t *testing.T) {
		stub := &materialAnalysisProtocolStub{invoke: func(
			ctx context.Context,
			_ *foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
		) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		started := time.Now()
		_, err := materialAnalysisClient(stub, 20*time.Millisecond).BatchGetAssetAnalysisInputs(context.Background(), materialAnalysisQuery())
		if err != context.DeadlineExceeded || stub.calls != 1 {
			t.Fatalf("timeout error=%v calls=%d", err, stub.calls)
		}
		if time.Since(started) > time.Second {
			t.Fatal("Adapter timeout 未在有界时间内返回")
		}
	})

	t.Run("parent canceled", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		cancel()
		stub := &materialAnalysisProtocolStub{invoke: func(
			ctx context.Context,
			_ *foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
		) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		_, err := materialAnalysisClient(stub, time.Second).BatchGetAssetAnalysisInputs(parent, materialAnalysisQuery())
		if err != context.Canceled || stub.calls != 1 {
			t.Fatalf("parent cancel error=%v calls=%d", err, stub.calls)
		}
	})
}

// TestMaterialAnalysisPreviewRejectsUnknownEnums 在生成枚举新增时失败关闭，禁止 String() 隐式接纳未知值。
func TestMaterialAnalysisPreviewRejectsUnknownEnums(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1)
		want   string
	}{
		{name: "asset media", want: analyzematerials.ResultCodeSnapshotInvalid, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) {
			response.Assets[0].MediaType = foundationv1.AssetAnalysisPreviewMediaTypeV1(99)
		}},
		{name: "evidence media", want: analyzematerials.ResultCodeEvidenceConflict, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) {
			response.Assets[0].Evidence[0].MediaType = foundationv1.AssetAnalysisPreviewMediaTypeV1(99)
		}},
		{name: "evidence kind", want: analyzematerials.ResultCodeEvidenceConflict, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) {
			response.Assets[0].Evidence[0].EvidenceKind = foundationv1.AssetAnalysisPreviewEvidenceKindV1(99)
		}},
		{name: "availability", want: analyzematerials.ResultCodeEvidenceConflict, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) {
			response.Assets[0].Evidence[0].Availability = foundationv1.AssetAnalysisPreviewAvailabilityV1(99)
		}},
		{name: "locator", want: analyzematerials.ResultCodeEvidenceConflict, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) {
			response.Assets[0].Evidence[0].Locator.Kind = foundationv1.AssetAnalysisPreviewLocatorKindV1(99)
		}},
	}
	for _, fixture := range testCases {
		t.Run(fixture.name, func(t *testing.T) {
			response := materialAnalysisProtocolResponse()
			fixture.mutate(response)
			stub := &materialAnalysisProtocolStub{invoke: func(
				context.Context,
				*foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
			) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
				return response, nil
			}}
			_, err := materialAnalysisClient(stub, time.Second).BatchGetAssetAnalysisInputs(context.Background(), materialAnalysisQuery())
			if analyzematerials.ErrorResultCode(err) != fixture.want || stub.calls != 1 {
				t.Fatalf("结果码=%q want=%q calls=%d err=%v", analyzematerials.ErrorResultCode(err), fixture.want, stub.calls, err)
			}
		})
	}
}

// TestMaterialAnalysisPreviewValidatesMappedSnapshot 覆盖 schema/request、完整 exact-set、版本、digest 与 locator 门禁。
func TestMaterialAnalysisPreviewValidatesMappedSnapshot(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1
		want   string
	}{
		{name: "nil response", want: analyzematerials.ResultCodeSnapshotInvalid, mutate: func(*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			return nil
		}},
		{name: "wrong rpc schema", want: analyzematerials.ResultCodeSnapshotInvalid, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			response.SchemaVersion = "asset_analysis_inputs.preview.rpc.v2"
			return response
		}},
		{name: "wrong request echo", want: analyzematerials.ResultCodeSnapshotInvalid, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			response.RequestId = materialAnalysisTestUserID
			return response
		}},
		{name: "empty snapshot token", want: analyzematerials.ResultCodeSnapshotInvalid, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			response.SnapshotToken = ""
			return response
		}},
		{name: "response incomplete", want: analyzematerials.ResultCodeSnapshotInvalid, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			response.ResponseComplete = false
			return response
		}},
		{name: "asset missing", want: analyzematerials.ResultCodeSnapshotInvalid, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			response.Assets = response.Assets[:1]
			return response
		}},
		{name: "asset extra", want: analyzematerials.ResultCodeSnapshotInvalid, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			response.Assets = append(response.Assets, &foundationv1.AssetAnalysisPreviewAssetV1{AssetId: materialAnalysisTestExtraAsset, AssetVersion: 1, MediaType: foundationv1.AssetAnalysisPreviewMediaTypeV1_TEXT, Evidence: []*foundationv1.AssetAnalysisPreviewEvidenceV1{}})
			return response
		}},
		{name: "asset duplicate", want: analyzematerials.ResultCodeSnapshotInvalid, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			response.Assets[1] = response.Assets[0]
			return response
		}},
		{name: "expected version mismatch", want: analyzematerials.ResultCodeSnapshotInvalid, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			response.Assets[1].AssetVersion = 4
			for _, evidence := range response.Assets[1].Evidence {
				evidence.AssetVersion = 4
			}
			return response
		}},
		{name: "content digest mismatch", want: analyzematerials.ResultCodeEvidenceConflict, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			response.Assets[0].Evidence[0].ContentDigest = materialAnalysisStringPointer(strings.Repeat("a", 64))
			return response
		}},
		{name: "invalid locator", want: analyzematerials.ResultCodeEvidenceConflict, mutate: func(response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1) *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
			response.Assets[0].Evidence[0].Locator.TextEnd = materialAnalysisInt64Pointer(21)
			return response
		}},
	}
	for _, fixture := range testCases {
		t.Run(fixture.name, func(t *testing.T) {
			response := fixture.mutate(materialAnalysisProtocolResponse())
			stub := &materialAnalysisProtocolStub{invoke: func(
				context.Context,
				*foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
			) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
				return response, nil
			}}
			_, err := materialAnalysisClient(stub, time.Second).BatchGetAssetAnalysisInputs(context.Background(), materialAnalysisQuery())
			if analyzematerials.ErrorResultCode(err) != fixture.want || stub.calls != 1 {
				t.Fatalf("结果码=%q want=%q calls=%d err=%v", analyzematerials.ErrorResultCode(err), fixture.want, stub.calls, err)
			}
		})
	}
}

// TestMaterialAnalysisPreviewRejectsInvalidQueryBeforeRPC 保证 Agent 不向 Business 发送非规范或重复 target。
func TestMaterialAnalysisPreviewRejectsInvalidQueryBeforeRPC(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		mutate func(*analyzematerials.EvidenceQuery)
	}{
		{name: "duplicate target", mutate: func(query *analyzematerials.EvidenceQuery) { query.Targets[1].AssetID = query.Targets[0].AssetID }},
		{name: "negative version", mutate: func(query *analyzematerials.EvidenceQuery) { query.Targets[0].ExpectedVersion = -1 }},
		{name: "non canonical asset", mutate: func(query *analyzematerials.EvidenceQuery) {
			query.Targets[0].AssetID = strings.ToUpper(query.Targets[0].AssetID)
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			query := materialAnalysisQuery()
			fixture.mutate(&query)
			stub := &materialAnalysisProtocolStub{invoke: func(
				context.Context,
				*foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
			) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
				t.Fatal("非法 query 不应调用 RPC")
				return nil, nil
			}}
			_, err := materialAnalysisClient(stub, time.Second).BatchGetAssetAnalysisInputs(context.Background(), query)
			if analyzematerials.ErrorResultCode(err) != analyzematerials.ResultCodeSnapshotInvalid || stub.calls != 0 {
				t.Fatalf("结果码=%q calls=%d err=%v", analyzematerials.ErrorResultCode(err), stub.calls, err)
			}
		})
	}
}

func materialAnalysisClient(stub *materialAnalysisProtocolStub, timeout time.Duration) *Client {
	return &Client{
		materialAnalysisPreview: stub,
		config:                  config.BusinessRPCConfig{RequestTimeout: timeout},
		idgen:                   materialAnalysisIDGenerator{id: materialAnalysisTestRequestID},
	}
}

func materialAnalysisQuery() analyzematerials.EvidenceQuery {
	return analyzematerials.EvidenceQuery{
		UserID: materialAnalysisTestUserID, ProjectID: materialAnalysisTestProjectID,
		Targets: []analyzematerials.AssetTarget{
			{AssetID: materialAnalysisTestImageAsset, ExpectedVersion: 3},
			{AssetID: materialAnalysisTestTextAsset},
		},
	}
}

func materialAnalysisProtocolResponse() *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1 {
	textContent := "可验证文字片段"
	imageDescription := "一只猫站在窗边"
	safetyLabel := "未发现明显安全风险"
	return &foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1{
		SchemaVersion: foundationv1.ASSET_ANALYSIS_INPUTS_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:     materialAnalysisTestRequestID, SnapshotToken: "snapshot-preview-1", ResponseComplete: true,
		Assets: []*foundationv1.AssetAnalysisPreviewAssetV1{
			{
				AssetId: materialAnalysisTestTextAsset, AssetVersion: 2,
				MediaType: foundationv1.AssetAnalysisPreviewMediaTypeV1_TEXT,
				Evidence: []*foundationv1.AssetAnalysisPreviewEvidenceV1{
					materialAnalysisReadyEvidence(
						"019f68e8-3010-7000-8000-000000000010", materialAnalysisTestTextAsset, 2,
						foundationv1.AssetAnalysisPreviewMediaTypeV1_TEXT,
						foundationv1.AssetAnalysisPreviewEvidenceKindV1_TEXT_SEGMENT,
						textContent,
						&foundationv1.AssetAnalysisPreviewLocatorV1{
							Kind:      foundationv1.AssetAnalysisPreviewLocatorKindV1_TEXT_RANGE,
							TextStart: materialAnalysisInt64Pointer(0), TextEnd: materialAnalysisInt64Pointer(8),
							TextSourceLength: materialAnalysisInt64Pointer(20),
						},
					),
				},
			},
			{
				AssetId: materialAnalysisTestImageAsset, AssetVersion: 3,
				MediaType: foundationv1.AssetAnalysisPreviewMediaTypeV1_IMAGE,
				Evidence: []*foundationv1.AssetAnalysisPreviewEvidenceV1{
					materialAnalysisReadyEvidence(
						"019f68e8-3020-7000-8000-000000000020", materialAnalysisTestImageAsset, 3,
						foundationv1.AssetAnalysisPreviewMediaTypeV1_IMAGE,
						foundationv1.AssetAnalysisPreviewEvidenceKindV1_VISUAL_DESCRIPTION,
						imageDescription,
						&foundationv1.AssetAnalysisPreviewLocatorV1{Kind: foundationv1.AssetAnalysisPreviewLocatorKindV1_IMAGE_WHOLE},
					),
					materialAnalysisReadyEvidence(
						"019f68e8-3030-7000-8000-000000000030", materialAnalysisTestImageAsset, 3,
						foundationv1.AssetAnalysisPreviewMediaTypeV1_IMAGE,
						foundationv1.AssetAnalysisPreviewEvidenceKindV1_SAFETY_LABEL,
						safetyLabel,
						&foundationv1.AssetAnalysisPreviewLocatorV1{
							Kind:   foundationv1.AssetAnalysisPreviewLocatorKindV1_IMAGE_REGION,
							ImageX: materialAnalysisInt32Pointer(100), ImageY: materialAnalysisInt32Pointer(200),
							ImageWidth: materialAnalysisInt32Pointer(300), ImageHeight: materialAnalysisInt32Pointer(400),
						},
					),
					materialAnalysisUnavailableEvidence(
						"019f68e8-3035-7000-8000-000000000035", foundationv1.AssetAnalysisPreviewEvidenceKindV1_VISUAL_DESCRIPTION,
						foundationv1.AssetAnalysisPreviewAvailabilityV1_MISSING, "VISUAL_NOT_READY",
					),
					materialAnalysisUnavailableEvidence(
						"019f68e8-3040-7000-8000-000000000040", foundationv1.AssetAnalysisPreviewEvidenceKindV1_VISUAL_DESCRIPTION,
						foundationv1.AssetAnalysisPreviewAvailabilityV1_FAILED, "VISUAL_EXTRACTION_FAILED",
					),
					materialAnalysisUnavailableEvidence(
						"019f68e8-3050-7000-8000-000000000050", foundationv1.AssetAnalysisPreviewEvidenceKindV1_SAFETY_LABEL,
						foundationv1.AssetAnalysisPreviewAvailabilityV1_REDACTED, "SAFETY_REDACTED",
					),
					materialAnalysisUnavailableEvidence(
						"019f68e8-3060-7000-8000-000000000060", foundationv1.AssetAnalysisPreviewEvidenceKindV1_VISUAL_DESCRIPTION,
						foundationv1.AssetAnalysisPreviewAvailabilityV1_UNSUPPORTED, "PREVIEW_UNSUPPORTED",
					),
				},
			},
		},
	}
}

func materialAnalysisReadyEvidence(
	evidenceID string,
	assetID string,
	assetVersion int64,
	mediaType foundationv1.AssetAnalysisPreviewMediaTypeV1,
	evidenceKind foundationv1.AssetAnalysisPreviewEvidenceKindV1,
	content string,
	locator *foundationv1.AssetAnalysisPreviewLocatorV1,
) *foundationv1.AssetAnalysisPreviewEvidenceV1 {
	digest := materialAnalysisDigest(content)
	return &foundationv1.AssetAnalysisPreviewEvidenceV1{
		EvidenceId: evidenceID, AssetId: assetID, AssetVersion: assetVersion,
		MediaType: mediaType, EvidenceKind: evidenceKind,
		Availability:           foundationv1.AssetAnalysisPreviewAvailabilityV1_READY,
		ContentDigest:          &digest,
		ExtractorSchemaVersion: materialAnalysisStringPointer("extractor.preview.v1"),
		ExtractorVersion:       materialAnalysisStringPointer("extractor-v1"),
		Locator:                locator,
		Content:                &content,
	}
}

func materialAnalysisUnavailableEvidence(
	evidenceID string,
	evidenceKind foundationv1.AssetAnalysisPreviewEvidenceKindV1,
	availability foundationv1.AssetAnalysisPreviewAvailabilityV1,
	reasonCode string,
) *foundationv1.AssetAnalysisPreviewEvidenceV1 {
	return &foundationv1.AssetAnalysisPreviewEvidenceV1{
		EvidenceId: evidenceID, AssetId: materialAnalysisTestImageAsset, AssetVersion: 3,
		MediaType: foundationv1.AssetAnalysisPreviewMediaTypeV1_IMAGE, EvidenceKind: evidenceKind,
		Availability: availability, ReasonCode: &reasonCode,
	}
}

func materialAnalysisDigest(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func materialAnalysisStringPointer(value string) *string { return &value }
func materialAnalysisInt64Pointer(value int64) *int64    { return &value }
func materialAnalysisInt32Pointer(value int32) *int32    { return &value }
