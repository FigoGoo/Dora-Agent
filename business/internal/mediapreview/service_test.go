package mediapreview

import (
	"context"
	"os"
	"testing"
	"time"
)

type mediaPreviewServiceRepositoryStub struct {
	prepareCommand     PrepareCommand
	prepareAllocation  PreparationAllocation
	prepareResult      PrepareResult
	prepareErr         error
	finalizeCommand    FinalizeCommand
	finalizeAllocation FinalizationAllocation
	finalizeResult     FinalizeResult
	finalizeErr        error
	prepareCalls       int
	finalizeCalls      int
}

func (stub *mediaPreviewServiceRepositoryStub) Prepare(_ context.Context, command PrepareCommand, allocation PreparationAllocation) (PrepareResult, error) {
	stub.prepareCalls++
	stub.prepareCommand = command
	stub.prepareAllocation = allocation
	return stub.prepareResult, stub.prepareErr
}

func (stub *mediaPreviewServiceRepositoryStub) QueryPreparation(_ context.Context, _ PreparationQuery) (PreparationQueryResult, error) {
	return PreparationQueryResult{Status: QueryStatusNotFound}, nil
}

func (stub *mediaPreviewServiceRepositoryStub) Finalize(_ context.Context, command FinalizeCommand, allocation FinalizationAllocation) (FinalizeResult, error) {
	stub.finalizeCalls++
	stub.finalizeCommand = command
	stub.finalizeAllocation = allocation
	return stub.finalizeResult, stub.finalizeErr
}

func (stub *mediaPreviewServiceRepositoryStub) QueryFinalization(_ context.Context, _ FinalizationQuery) (FinalizationQueryResult, error) {
	return FinalizationQueryResult{Status: QueryStatusNotFound}, nil
}

func (stub *mediaPreviewServiceRepositoryStub) OpenReadyContent(_ context.Context, _ ContentQuery) (ReadyContent, *os.File, error) {
	return ReadyContent{}, nil, ErrNotFound
}

type mediaPreviewClockStub struct{ value time.Time }

func (clock mediaPreviewClockStub) Now() time.Time { return clock.value }

type mediaPreviewIDSequence struct {
	values []string
	index  int
}

func (sequence *mediaPreviewIDSequence) New() (string, error) {
	value := sequence.values[sequence.index]
	sequence.index++
	return value, nil
}

func TestServicePrepareAllocatesAssetPreparationAndKeys(t *testing.T) {
	command := mediaPreviewGenerateCommandFixture(t)
	preparationID := mediaPreviewTestUUIDv7(t)
	assetID := mediaPreviewTestUUIDv7(t)
	now := time.Date(2026, 7, 17, 14, 0, 0, 0, time.FixedZone("offset", 8*60*60))
	repository := &mediaPreviewServiceRepositoryStub{}
	service, err := NewService(repository, mediaPreviewClockStub{value: now}, &mediaPreviewIDSequence{values: []string{preparationID, assetID}})
	if err != nil {
		t.Fatal(err)
	}
	staging, final, _ := ObjectKeys(assetID, preparationID, command.ToolKey)
	repository.prepareResult = PrepareResult{
		Disposition: CommandDispositionCreated,
		Preparation: Preparation{
			PreparationID: preparationID, CommandID: command.CommandID, RequestDigest: command.RequestDigest,
			OperationID: command.OperationID, OwnerUserID: command.OwnerUserID, ProjectID: command.ProjectID,
			ToolKey: command.ToolKey, ScopeDigest: command.ScopeDigest, OutputProfile: command.OutputProfile,
			SourceRef: SourceRef{
				SourceType: SourceTypePromptPreview, SourceID: command.PromptSource.ID,
				SourceVersion: 1, SourceDigest: command.PromptSource.ContentDigest,
				TargetLocalKey: command.PromptSource.TargetLocalKey, TargetDigest: mediaPreviewTestDigest("target"),
			},
			AssetRef:         AssetRef{AssetID: assetID, Version: 1, Status: StatusReserved, MediaKind: MediaKindImage, MIMEType: MIMEPNG},
			StagingObjectKey: staging, FinalObjectKey: final, CreatedAt: now.UTC(),
		},
	}
	result, err := service.Prepare(context.Background(), command)
	if err != nil || result.Preparation.AssetRef.AssetID != assetID || repository.prepareCalls != 1 {
		t.Fatalf("Prepare() result=%+v calls=%d error=%v", result, repository.prepareCalls, err)
	}
	if repository.prepareAllocation.PreparationID != preparationID || repository.prepareAllocation.AssetID != assetID ||
		repository.prepareAllocation.StagingObjectKey != staging || repository.prepareAllocation.FinalObjectKey != final ||
		!repository.prepareAllocation.CreatedAt.Equal(now.UTC()) || repository.prepareAllocation.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected allocation: %+v", repository.prepareAllocation)
	}
}

func TestServiceRejectsInvalidCommandBeforeGeneratingIDsOrCallingRepository(t *testing.T) {
	repository := &mediaPreviewServiceRepositoryStub{}
	sequence := &mediaPreviewIDSequence{values: []string{mediaPreviewTestUUIDv7(t), mediaPreviewTestUUIDv7(t)}}
	service, err := NewService(repository, mediaPreviewClockStub{value: time.Now()}, sequence)
	if err != nil {
		t.Fatal(err)
	}
	command := mediaPreviewGenerateCommandFixture(t)
	command.OutputProfile = OutputProfileMP4
	if _, err := service.Prepare(context.Background(), command); err != ErrInvalidArgument {
		t.Fatalf("Prepare() error=%v", err)
	}
	if sequence.index != 0 || repository.prepareCalls != 0 {
		t.Fatalf("invalid command generated IDs or called repository: ids=%d calls=%d", sequence.index, repository.prepareCalls)
	}
}

func mediaPreviewGenerateCommandFixture(t *testing.T) PrepareCommand {
	t.Helper()
	return PrepareCommand{
		RequestID: mediaPreviewTestUUIDv7(t), CommandID: mediaPreviewTestUUIDv7(t), OperationID: mediaPreviewTestUUIDv7(t),
		RequestDigest: mediaPreviewTestDigest("prepare request"), OwnerUserID: mediaPreviewTestUUIDv7(t),
		ProjectID: mediaPreviewTestUUIDv7(t), ToolKey: ToolGenerateMedia,
		ScopeDigest: mediaPreviewTestDigest("scope"), OutputProfile: OutputProfilePNG,
		PromptSource: &PromptSource{
			ID: mediaPreviewTestUUIDv7(t), Version: 1, ContentDigest: mediaPreviewTestDigest("prompt"), TargetLocalKey: "slot_1",
		},
	}
}
