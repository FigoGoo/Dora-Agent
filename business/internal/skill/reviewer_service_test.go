package skill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNormalizeAndCanonicalizeEnforcesOneMiBBoundary(t *testing.T) {
	definition := validDefinitionForTest()
	definition.Examples = make([]SkillExampleV1, 100)
	for index := range definition.Examples {
		definition.Examples[index] = SkillExampleV1{Input: fmt.Sprintf("input-%03d", index), Output: fmt.Sprintf("output-%03d-", index)}
	}
	for {
		_, canonical, _, err := normalizeAndCanonicalize(definition)
		if err != nil {
			t.Fatalf("build boundary definition: %v", err)
		}
		remaining := MaxCanonicalDefinitionBytes - len(canonical)
		if remaining == 0 {
			break
		}
		if remaining < 0 {
			t.Fatalf("test fixture crossed boundary by %d bytes", -remaining)
		}
		grew := false
		for index := range definition.Examples {
			room := maxBodyTextBytes - len(definition.Examples[index].Output)
			if room <= 0 {
				continue
			}
			add := remaining
			if add > room {
				add = room
			}
			definition.Examples[index].Output += strings.Repeat("x", add)
			grew = true
			break
		}
		if !grew {
			t.Fatal("fixture cannot reach one MiB boundary")
		}
	}
	_, canonical, _, err := normalizeAndCanonicalize(definition)
	if err != nil || len(canonical) != MaxCanonicalDefinitionBytes {
		t.Fatalf("exact one MiB should be valid: len=%d err=%v", len(canonical), err)
	}
	for index := range definition.Examples {
		if len(definition.Examples[index].Output) < maxBodyTextBytes {
			definition.Examples[index].Output += "y"
			break
		}
	}
	if _, _, _, err := normalizeAndCanonicalize(definition); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("one MiB + 1 should fail closed, got %v", err)
	}
}

func TestServiceReviewerDetailUsesFrozenDefinitionAndStrongETag(t *testing.T) {
	ownerID, _ := uuid.NewV7()
	reviewerID, _ := uuid.NewV7()
	state := newSkillServiceState(t, ownerID.String(), validDefinitionForTest(), 2)
	reviewID, _ := uuid.NewV7()
	state.LatestReview = &ReviewSubmission{
		ID: reviewID.String(), SkillID: state.Skill.ID, ContentRevisionID: state.Draft.ID,
		ContentDigest: state.Draft.ContentDigest, Status: ReviewStatusReviewing, Version: 3,
		SubmittedByUserID: ownerID.String(), SubmittedAt: state.Skill.CreatedAt, UpdatedAt: state.Skill.UpdatedAt,
	}
	repository := &skillServiceRepository{state: state}
	service, _ := NewService(repository, skillServiceTestClock{now: time.Now()}, skillServiceTestIDs{})
	detail, err := service.FindReviewDetail(context.Background(), ReviewerPrincipal{
		UserID: reviewerID.String(), Capabilities: []string{ReviewCapability},
	}, reviewID.String())
	if err != nil {
		t.Fatal(err)
	}
	if detail.Definition.Name != state.Draft.Definition.Name || detail.ReviewETag != ReviewETag(*state.LatestReview) ||
		detail.CurrentPublished != nil || detail.Comparison.HasCurrentPublished || detail.Comparison.SameContent ||
		len(detail.AllowedActions) != 1 || detail.AllowedActions[0] != CommandTypeApproveAndPublish {
		t.Fatalf("invalid frozen detail: %+v", detail)
	}
	if err := ValidateStrongReviewETag(detail.ReviewETag); err != nil {
		t.Fatalf("generated review ETag is not canonical: %v", err)
	}
}

func TestServiceApproveBuildsDedicatedDecisionCommandWithoutReviewPreread(t *testing.T) {
	ownerID, _ := uuid.NewV7()
	reviewerID, _ := uuid.NewV7()
	requestID, _ := uuid.NewV7()
	state := newSkillServiceState(t, ownerID.String(), validDefinitionForTest(), 1)
	reviewID, _ := uuid.NewV7()
	state.LatestReview = &ReviewSubmission{
		ID: reviewID.String(), SkillID: state.Skill.ID, ContentRevisionID: state.Draft.ID,
		ContentDigest: state.Draft.ContentDigest, Status: ReviewStatusReviewing, Version: 1,
		SubmittedByUserID: ownerID.String(), SubmittedAt: state.Skill.CreatedAt, UpdatedAt: state.Skill.CreatedAt,
	}
	repository := &skillServiceRepository{state: state}
	service, _ := NewService(repository, skillServiceTestClock{now: time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)}, skillServiceTestIDs{})
	ifMatch := ReviewETag(*state.LatestReview)
	result, err := service.ApproveAndPublish(context.Background(), ApproveAndPublishServiceCommand{
		Reviewer: ReviewerPrincipal{UserID: reviewerID.String(), Capabilities: []string{ReviewCapability}},
		ReviewID: reviewID.String(), IdempotencyKey: "review-decision-1", Decision: string(ReviewStatusApproved),
		IfMatch: ifMatch, RequestID: requestID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	command := repository.approveCommand
	if command.IfMatch != ifMatch || command.RequestID != requestID.String() || command.KeyDigest == (Digest{}) ||
		command.SemanticDigest != reviewDecisionSemanticDigest(reviewID.String(), string(ReviewStatusApproved), ifMatch) ||
		result.Review.Status != ReviewStatusApproved || result.Review.PublishedSnapshotID == "" || result.Review.AllowedActions == nil {
		t.Fatalf("decision command/result drifted: command=%+v result=%+v", command, result)
	}
}

func TestValidateStrongReviewETagRejectsWeakListsAndWhitespace(t *testing.T) {
	valid := `"sr1-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`
	if err := ValidateStrongReviewETag(valid); err != nil {
		t.Fatalf("valid tag rejected: %v", err)
	}
	for _, value := range []string{"*", `W/` + valid, valid + "," + valid, " " + valid, valid + " ", `"sr1-not-base64"`} {
		if !errors.Is(ValidateStrongReviewETag(value), ErrInvalidReviewRequest) {
			t.Fatalf("invalid ETag accepted: %q", value)
		}
	}
}
