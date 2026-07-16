package skill

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const defaultMarketPageSize = 20

var (
	// ErrInvalidMarketRequest 表示公开市场路径或游标不满足冻结格式。
	ErrInvalidMarketRequest = errors.New("invalid skill market request")
	// ErrMarketNotFound 表示 Skill 不存在、未发布或当前不允许公开。
	ErrMarketNotFound = errors.New("skill market target not found")
)

// MarketPageBoundary 是公开市场按发布时间和公开 Skill ID 倒序分页的内部边界。
type MarketPageBoundary struct {
	PublishedAt time.Time
	SkillID     string
}

// MarketPublishedSkill 是 Repository 校验完 current published 逻辑关联后交给应用服务的内部投影。
type MarketPublishedSkill struct {
	SkillID              string
	PublisherID          string
	PublisherDisplayName string
	Definition           SkillDefinitionV1
	PublishedAt          time.Time
}

// MarketPublishedPage 是 Repository 使用一次集合查询返回的公开候选页。
type MarketPublishedPage struct {
	Items   []MarketPublishedSkill
	HasMore bool
}

// MarketRepository 是公开 Market Service 消费的最小持久化边界。
type MarketRepository interface {
	ListPublished(context.Context, *MarketPageBoundary, int) (MarketPublishedPage, error)
	FindPublishedByID(context.Context, string) (MarketPublishedSkill, error)
}

// MarketPublisherDTO 是公开发布者安全身份投影。
type MarketPublisherDTO struct {
	PublisherID string `json:"publisher_id"`
	DisplayName string `json:"display_name"`
}

// MarketCoverAssetDTO 预留未来已冻结的公开封面结构；W1-E 始终编码为 null。
type MarketCoverAssetDTO struct{}

// MarketListItemDTO 是公开列表白名单字段，不包含完整 Definition 或内部发布事实。
type MarketListItemDTO struct {
	SkillID                string               `json:"skill_id"`
	Name                   string               `json:"name"`
	Summary                string               `json:"summary"`
	Category               string               `json:"category"`
	Tags                   []string             `json:"tags"`
	Publisher              MarketPublisherDTO   `json:"publisher"`
	PublishedAt            string               `json:"published_at"`
	CoverAsset             *MarketCoverAssetDTO `json:"cover_asset"`
	DeclaredCapabilityKeys []string             `json:"declared_capability_keys"`
}

// MarketExampleDTO 是公开详情允许展示的输入输出示例。
type MarketExampleDTO struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

// MarketDetailDTO 是公开详情白名单字段。
type MarketDetailDTO struct {
	SkillID                string               `json:"skill_id"`
	Name                   string               `json:"name"`
	Summary                string               `json:"summary"`
	Category               string               `json:"category"`
	Tags                   []string             `json:"tags"`
	Publisher              MarketPublisherDTO   `json:"publisher"`
	PublishedAt            string               `json:"published_at"`
	CoverAsset             *MarketCoverAssetDTO `json:"cover_asset"`
	DeclaredCapabilityKeys []string             `json:"declared_capability_keys"`
	InputDescription       string               `json:"input_description"`
	OutputDescription      string               `json:"output_description"`
	Examples               []MarketExampleDTO   `json:"examples"`
	StarterPrompts         []string             `json:"starter_prompts"`
	MarketDetail           string               `json:"market_detail"`
	CopyrightNotice        string               `json:"copyright_notice"`
	UserNotice             string               `json:"user_notice"`
}

// MarketListResult 是公开列表应用结果；空 NextCursor 表示末页。
type MarketListResult struct {
	Items      []MarketListItemDTO
	NextCursor string
}

// MarketService 提供匿名 Market 的严格白名单读取。
type MarketService struct {
	repository MarketRepository
}

// NewMarketService 创建公开 Market 应用服务。
func NewMarketService(repository MarketRepository) (*MarketService, error) {
	if repository == nil {
		return nil, ErrPersistence
	}
	return &MarketService{repository: repository}, nil
}

// ListPublished 返回 newest-first 的公开 Skill 页。
func (service *MarketService) ListPublished(ctx context.Context, cursor string) (MarketListResult, error) {
	boundary, err := decodeMarketCursor(cursor)
	if err != nil {
		return MarketListResult{}, err
	}
	page, err := service.repository.ListPublished(ctx, boundary, defaultMarketPageSize)
	if err != nil {
		return MarketListResult{}, normalizeMarketRepositoryError(err)
	}
	if len(page.Items) > defaultMarketPageSize || (page.HasMore && len(page.Items) != defaultMarketPageSize) {
		return MarketListResult{}, ErrPersistence
	}
	result := MarketListResult{Items: make([]MarketListItemDTO, 0, len(page.Items))}
	seen := make(map[string]struct{}, len(page.Items))
	for index, item := range page.Items {
		if err := validateMarketPublishedSkill(item); err != nil {
			return MarketListResult{}, err
		}
		if _, exists := seen[item.SkillID]; exists {
			return MarketListResult{}, ErrPersistence
		}
		seen[item.SkillID] = struct{}{}
		if index > 0 && !marketItemBefore(page.Items[index-1], item) {
			return MarketListResult{}, ErrPersistence
		}
		result.Items = append(result.Items, marketListItemDTO(item))
	}
	if page.HasMore {
		last := page.Items[len(page.Items)-1]
		result.NextCursor, err = encodeMarketCursor(MarketPageBoundary{PublishedAt: last.PublishedAt, SkillID: last.SkillID})
		if err != nil {
			return MarketListResult{}, ErrPersistence
		}
	}
	return result, nil
}

// FindPublishedByID 返回一个公开 Skill 详情。
func (service *MarketService) FindPublishedByID(ctx context.Context, skillID string) (MarketDetailDTO, error) {
	if !isUUIDv7(skillID) {
		return MarketDetailDTO{}, ErrInvalidMarketRequest
	}
	item, err := service.repository.FindPublishedByID(ctx, skillID)
	if err != nil {
		return MarketDetailDTO{}, normalizeMarketRepositoryError(err)
	}
	if item.SkillID != skillID || validateMarketPublishedSkill(item) != nil {
		return MarketDetailDTO{}, ErrPersistence
	}
	return marketDetailDTO(item), nil
}

type marketCursorV1 struct {
	SchemaVersion       string `json:"schema_version"`
	PublishedAtUnixNano int64  `json:"published_at_unix_nano"`
	SkillID             string `json:"skill_id"`
}

func encodeMarketCursor(boundary MarketPageBoundary) (string, error) {
	if boundary.PublishedAt.IsZero() || boundary.PublishedAt.UTC().UnixNano() <= 0 || !isUUIDv7(boundary.SkillID) {
		return "", ErrInvalidMarketRequest
	}
	encoded, err := json.Marshal(marketCursorV1{
		SchemaVersion:       "skill_market_cursor.v1",
		PublishedAtUnixNano: boundary.PublishedAt.UTC().UnixNano(),
		SkillID:             boundary.SkillID,
	})
	if err != nil {
		return "", ErrPersistence
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeMarketCursor(cursor string) (*MarketPageBoundary, error) {
	if cursor == "" {
		return nil, nil
	}
	if len(cursor) > 1024 {
		return nil, ErrInvalidMarketRequest
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != cursor {
		return nil, ErrInvalidMarketRequest
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var decoded marketCursorV1
	if err := decoder.Decode(&decoded); err != nil || ensureDecoderEOF(decoder) != nil ||
		decoded.SchemaVersion != "skill_market_cursor.v1" || decoded.PublishedAtUnixNano <= 0 || !isUUIDv7(decoded.SkillID) {
		return nil, ErrInvalidMarketRequest
	}
	return &MarketPageBoundary{PublishedAt: time.Unix(0, decoded.PublishedAtUnixNano).UTC(), SkillID: decoded.SkillID}, nil
}

func validateMarketPublishedSkill(item MarketPublishedSkill) error {
	if !isUUIDv7(item.SkillID) || !isUUIDv7(item.PublisherID) || item.PublishedAt.IsZero() || item.PublishedAt.UTC().UnixNano() <= 0 ||
		strings.TrimSpace(item.PublisherDisplayName) == "" || utf8.RuneCountInString(item.PublisherDisplayName) > 160 {
		return ErrPersistence
	}
	for _, character := range item.PublisherDisplayName {
		if unicode.IsControl(character) {
			return ErrPersistence
		}
	}
	_, _, err := CanonicalDefinitionV1(item.Definition)
	if err != nil {
		return ErrPersistence
	}
	return nil
}

func marketItemBefore(previous MarketPublishedSkill, current MarketPublishedSkill) bool {
	if previous.PublishedAt.Equal(current.PublishedAt) {
		return previous.SkillID > current.SkillID
	}
	return previous.PublishedAt.After(current.PublishedAt)
}

func marketListItemDTO(item MarketPublishedSkill) MarketListItemDTO {
	definition := item.Definition
	return MarketListItemDTO{
		SkillID: item.SkillID, Name: definition.Name, Summary: definition.Summary, Category: definition.Category,
		Tags:        cloneMarketStrings(definition.Tags),
		Publisher:   MarketPublisherDTO{PublisherID: item.PublisherID, DisplayName: item.PublisherDisplayName},
		PublishedAt: item.PublishedAt.UTC().Format(time.RFC3339Nano), CoverAsset: nil,
		DeclaredCapabilityKeys: declaredCapabilityKeys(definition),
	}
}

func marketDetailDTO(item MarketPublishedSkill) MarketDetailDTO {
	list := marketListItemDTO(item)
	examples := make([]MarketExampleDTO, len(item.Definition.Examples))
	for index, example := range item.Definition.Examples {
		examples[index] = MarketExampleDTO{Input: example.Input, Output: example.Output}
	}
	return MarketDetailDTO{
		SkillID: list.SkillID, Name: list.Name, Summary: list.Summary, Category: list.Category,
		Tags: list.Tags, Publisher: list.Publisher, PublishedAt: list.PublishedAt, CoverAsset: nil,
		DeclaredCapabilityKeys: list.DeclaredCapabilityKeys,
		InputDescription:       item.Definition.InputDescription, OutputDescription: item.Definition.OutputDescription,
		Examples: examples, StarterPrompts: cloneMarketStrings(item.Definition.StarterPrompts),
		MarketDetail:    item.Definition.MarketListing.Detail,
		CopyrightNotice: item.Definition.MarketListing.CopyrightNotice, UserNotice: item.Definition.MarketListing.UserNotice,
	}
}

func cloneMarketStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func declaredCapabilityKeys(definition SkillDefinitionV1) []string {
	result := make([]string, 0, 6)
	for _, capability := range []struct {
		key   string
		value CapabilityGuidanceV1
	}{
		{key: "plan_creation_spec", value: definition.PlanCreationSpec},
		{key: "analyze_materials", value: definition.AnalyzeMaterials},
		{key: "plan_storyboard", value: definition.PlanStoryboard},
		{key: "generate_media", value: definition.GenerateMedia},
		{key: "write_prompts", value: definition.WritePrompts},
		{key: "assemble_output", value: definition.AssembleOutput},
	} {
		if capability.value.Applicability == "enabled" {
			result = append(result, capability.key)
		}
	}
	return result
}

func normalizeMarketRepositoryError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, ErrMarketNotFound):
		return ErrMarketNotFound
	default:
		return ErrPersistence
	}
}
