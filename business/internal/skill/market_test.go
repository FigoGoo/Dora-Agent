package skill

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

type marketRepositoryStub struct {
	listBoundary *MarketPageBoundary
	listLimit    int
	listPage     MarketPublishedPage
	listErr      error
	listCalls    int
	detailID     string
	detail       MarketPublishedSkill
	detailErr    error
	detailCalls  int
}

func (repository *marketRepositoryStub) ListPublished(_ context.Context, boundary *MarketPageBoundary, limit int) (MarketPublishedPage, error) {
	repository.listCalls++
	repository.listBoundary = boundary
	repository.listLimit = limit
	return repository.listPage, repository.listErr
}

func (repository *marketRepositoryStub) FindPublishedByID(_ context.Context, skillID string) (MarketPublishedSkill, error) {
	repository.detailCalls++
	repository.detailID = skillID
	return repository.detail, repository.detailErr
}

func newMarketTestPublishedSkill(t *testing.T, publishedAt time.Time) MarketPublishedSkill {
	t.Helper()
	definition, err := NormalizeDefinitionV1(validDefinitionForTest())
	if err != nil {
		t.Fatal(err)
	}
	return MarketPublishedSkill{
		SkillID: newGovernanceTestUUIDv7(t), PublisherID: newGovernanceTestUUIDv7(t),
		PublisherDisplayName: "Dora Creator", Definition: definition, PublishedAt: publishedAt.UTC(),
	}
}

func TestMarketServiceListUsesPublicCursorAndWhitelist(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 123000, time.UTC)
	items := make([]MarketPublishedSkill, defaultMarketPageSize)
	for index := range items {
		items[index] = newMarketTestPublishedSkill(t, now.Add(-time.Duration(index)*time.Second))
	}
	first := items[0]
	last := items[len(items)-1]
	repository := &marketRepositoryStub{listPage: MarketPublishedPage{Items: items, HasMore: true}}
	service, err := NewMarketService(repository)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ListPublished(context.Background(), "")
	if err != nil {
		t.Fatalf("ListPublished() error = %v", err)
	}
	if repository.listLimit != defaultMarketPageSize || repository.listBoundary != nil || len(result.Items) != defaultMarketPageSize || result.NextCursor == "" {
		t.Fatalf("unexpected Market list: repository=%+v result=%+v", repository, result)
	}
	item := result.Items[0]
	if item.SkillID != first.SkillID || item.Publisher.PublisherID != first.PublisherID || item.CoverAsset != nil ||
		len(item.Tags) != len(first.Definition.Tags) || strings.Join(item.DeclaredCapabilityKeys, ",") == "" {
		t.Fatalf("unsafe or incomplete list projection: %+v", item)
	}
	boundary, err := decodeMarketCursor(result.NextCursor)
	if err != nil || boundary.SkillID != last.SkillID || !boundary.PublishedAt.Equal(last.PublishedAt) {
		t.Fatalf("Market cursor boundary=%+v err=%v", boundary, err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(result.NextCursor)
	if err != nil || strings.Contains(string(decoded), "snapshot") || !strings.Contains(string(decoded), last.SkillID) {
		t.Fatalf("Market cursor exposed a snapshot or omitted public Skill ID: %q err=%v", decoded, err)
	}
}

func TestMarketServiceDetailMapsSafeFieldsAndClonesArrays(t *testing.T) {
	item := newMarketTestPublishedSkill(t, time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC))
	repository := &marketRepositoryStub{detail: item}
	service, _ := NewMarketService(repository)
	detail, err := service.FindPublishedByID(context.Background(), item.SkillID)
	if err != nil {
		t.Fatal(err)
	}
	if repository.detailID != item.SkillID || detail.SkillID != item.SkillID ||
		detail.InputDescription != item.Definition.InputDescription || detail.MarketDetail != item.Definition.MarketListing.Detail ||
		len(detail.Examples) != len(item.Definition.Examples) || detail.CoverAsset != nil {
		t.Fatalf("unexpected Market detail: %+v", detail)
	}
	detail.Tags[0] = "changed"
	detail.StarterPrompts[0] = "changed"
	if item.Definition.Tags[0] == "changed" || item.Definition.StarterPrompts[0] == "changed" {
		t.Fatal("Market detail shared mutable arrays with repository state")
	}
}

func TestMarketServiceRejectsCursorAndBrokenRepositoryPage(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	item := newMarketTestPublishedSkill(t, now)
	repository := &marketRepositoryStub{}
	service, _ := NewMarketService(repository)
	invalidCursors := []string{
		"not-base64!",
		base64.RawURLEncoding.EncodeToString([]byte(`{"schema_version":"skill_market_cursor.v1","published_at_unix_nano":1,"skill_id":"` + item.SkillID + `","extra":true}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"schema_version":"skill_market_cursor.v1","published_at_unix_nano":1,"skill_id":"` + item.SkillID + `"}{}`)),
		strings.Repeat("a", 1025),
	}
	for _, cursor := range invalidCursors {
		if _, err := service.ListPublished(context.Background(), cursor); !errors.Is(err, ErrInvalidMarketRequest) {
			t.Fatalf("invalid cursor %q error = %v", cursor, err)
		}
	}
	if repository.listCalls != 0 {
		t.Fatalf("invalid cursors reached repository %d times", repository.listCalls)
	}

	repository.listPage = MarketPublishedPage{Items: []MarketPublishedSkill{item, item}}
	if _, err := service.ListPublished(context.Background(), ""); !errors.Is(err, ErrPersistence) {
		t.Fatalf("duplicate page error = %v", err)
	}
	repository.listPage = MarketPublishedPage{Items: []MarketPublishedSkill{item}, HasMore: true}
	if _, err := service.ListPublished(context.Background(), ""); !errors.Is(err, ErrPersistence) {
		t.Fatalf("short has-more page error = %v", err)
	}
	repository.detail = item
	if _, err := service.FindPublishedByID(context.Background(), "not-a-uuid"); !errors.Is(err, ErrInvalidMarketRequest) || repository.detailCalls != 0 {
		t.Fatalf("invalid detail path error=%v calls=%d", err, repository.detailCalls)
	}
}

func TestMarketServicePreservesNotFoundAndContextErrors(t *testing.T) {
	repository := &marketRepositoryStub{detailErr: ErrMarketNotFound, listErr: context.Canceled}
	service, _ := NewMarketService(repository)
	if _, err := service.ListPublished(context.Background(), ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("list context error = %v", err)
	}
	if _, err := service.FindPublishedByID(context.Background(), newGovernanceTestUUIDv7(t)); !errors.Is(err, ErrMarketNotFound) {
		t.Fatalf("detail not-found error = %v", err)
	}
}
