package assetanalysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

var (
	lowerSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reasonCodePattern  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
	versionPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+-]{0,127}$`)
)

// Service 实现授权后版本校验、证据联合验证、稳定排序与快照摘要。
type Service struct {
	repository Repository
}

// NewService 创建素材分析输入预览领域服务。
func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("create asset analysis service: repository is nil")
	}
	return &Service{repository: repository}, nil
}

// BatchGet 严格按授权、版本、证据顺序构造完整快照。
func (service *Service) BatchGet(ctx context.Context, query Query) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, ErrInvalidArgument
	}
	if err := validateQuery(query); err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	assetIDs := make([]string, len(query.Targets))
	for index, target := range query.Targets {
		assetIDs[index] = target.AssetID
	}
	assets, err := service.repository.BatchGetAuthorized(ctx, RepositoryQuery{
		UserID: query.UserID, ProjectID: query.ProjectID, AssetIDs: assetIDs,
	})
	if err != nil {
		return Snapshot{}, mapRepositoryError(err)
	}
	if err := validateExactAssetSet(assets, assetIDs); err != nil {
		return Snapshot{}, err
	}

	assetByID := make(map[string]*Asset, len(assets))
	for index := range assets {
		assetByID[assets[index].ID] = &assets[index]
	}
	// 版本断言必须位于 exact-set 授权成功之后，避免泄漏不可访问素材版本。
	for _, target := range query.Targets {
		if target.ExpectedAssetVersion != nil && assetByID[target.AssetID].Version != *target.ExpectedAssetVersion {
			return Snapshot{}, ErrVersionConflict
		}
	}

	evidenceCount := 0
	evidenceIDs := make(map[string]struct{})
	for index := range assets {
		asset := &assets[index]
		if err := validateAsset(*asset); err != nil {
			return Snapshot{}, err
		}
		evidenceCount += len(asset.Evidence)
		if evidenceCount > MaxEvidence {
			return Snapshot{}, ErrLimitExceeded
		}
		for _, evidence := range asset.Evidence {
			if _, exists := evidenceIDs[evidence.ID]; exists {
				return Snapshot{}, ErrEvidenceConflict
			}
			evidenceIDs[evidence.ID] = struct{}{}
		}
		sort.Slice(asset.Evidence, func(left, right int) bool {
			if asset.Evidence[left].Kind != asset.Evidence[right].Kind {
				return asset.Evidence[left].Kind < asset.Evidence[right].Kind
			}
			return asset.Evidence[left].ID < asset.Evidence[right].ID
		})
	}
	sort.Slice(assets, func(left, right int) bool { return assets[left].ID < assets[right].ID })
	token, err := snapshotToken(assets)
	if err != nil {
		return Snapshot{}, ErrEvidenceConflict
	}
	return Snapshot{SnapshotToken: token, ResponseComplete: true, Assets: assets}, nil
}

func validateQuery(query Query) error {
	if query.SchemaVersion != RPCSchemaVersion || !CanonicalUUIDv7(query.RequestID) ||
		!CanonicalUUIDv7(query.UserID) || !CanonicalUUIDv7(query.ProjectID) ||
		len(query.Targets) < 1 || len(query.Targets) > MaxAssets {
		return ErrInvalidArgument
	}
	previous := ""
	for _, target := range query.Targets {
		if !CanonicalUUIDv7(target.AssetID) || (previous != "" && target.AssetID <= previous) {
			return ErrInvalidArgument
		}
		if target.ExpectedAssetVersion != nil && *target.ExpectedAssetVersion < 1 {
			return ErrInvalidArgument
		}
		previous = target.AssetID
	}
	return nil
}

func validateExactAssetSet(assets []Asset, expected []string) error {
	if len(assets) != len(expected) {
		return ErrNotFound
	}
	actual := make([]string, len(assets))
	for index, asset := range assets {
		actual[index] = asset.ID
	}
	sort.Strings(actual)
	for index := range expected {
		if actual[index] != expected[index] || (index > 0 && actual[index] == actual[index-1]) {
			return ErrNotFound
		}
	}
	return nil
}

func validateAsset(asset Asset) error {
	if !CanonicalUUIDv7(asset.ID) || asset.Version < 1 ||
		(asset.MediaType != MediaTypeText && asset.MediaType != MediaTypeImage) {
		return ErrEvidenceConflict
	}
	for _, evidence := range asset.Evidence {
		if err := validateEvidence(asset, evidence); err != nil {
			return err
		}
	}
	return nil
}

func validateEvidence(asset Asset, evidence Evidence) error {
	if !CanonicalUUIDv7(evidence.ID) || evidence.AssetID != asset.ID ||
		evidence.AssetVersion != asset.Version || evidence.MediaType != asset.MediaType ||
		!validEvidenceKind(evidence.MediaType, evidence.Kind) || !validAvailability(evidence.Availability) {
		return ErrEvidenceConflict
	}
	if evidence.Availability != AvailabilityReady {
		if !reasonCodePattern.MatchString(evidence.ReasonCode) || evidence.Content != "" || evidence.ContentDigest != "" ||
			evidence.ExtractorSchemaVersion != "" || evidence.ExtractorVersion != "" || evidence.Locator != nil {
			return ErrEvidenceConflict
		}
		return nil
	}
	if evidence.ReasonCode != "" || !validContent(evidence.Content) ||
		!lowerSHA256Pattern.MatchString(evidence.ContentDigest) ||
		!versionPattern.MatchString(evidence.ExtractorSchemaVersion) ||
		!versionPattern.MatchString(evidence.ExtractorVersion) || evidence.Locator == nil ||
		!validLocator(evidence.MediaType, *evidence.Locator) {
		return ErrEvidenceConflict
	}
	digest := sha256.Sum256([]byte(evidence.Content))
	if hex.EncodeToString(digest[:]) != evidence.ContentDigest {
		return ErrEvidenceConflict
	}
	return nil
}

func validEvidenceKind(mediaType MediaType, kind EvidenceKind) bool {
	return (mediaType == MediaTypeText && kind == EvidenceKindTextSegment) ||
		(mediaType == MediaTypeImage && (kind == EvidenceKindVisualDescription || kind == EvidenceKindSafetyLabel))
}

func validAvailability(value Availability) bool {
	switch value {
	case AvailabilityReady, AvailabilityMissing, AvailabilityFailed, AvailabilityRedacted, AvailabilityUnsupported:
		return true
	default:
		return false
	}
}

func validContent(value string) bool {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length < 1 || length > 2000 || strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t' {
			return false
		}
	}
	return true
}

func validLocator(mediaType MediaType, locator Locator) bool {
	switch locator.Kind {
	case LocatorKindTextRange:
		return mediaType == MediaTypeText && locator.TextStart != nil && locator.TextEnd != nil && locator.TextSourceLength != nil &&
			*locator.TextStart >= 0 && *locator.TextStart < *locator.TextEnd && *locator.TextEnd <= *locator.TextSourceLength &&
			noImageCoordinates(locator)
	case LocatorKindImageWhole:
		return mediaType == MediaTypeImage && noTextCoordinates(locator) && noImageCoordinates(locator)
	case LocatorKindImageRegion:
		return mediaType == MediaTypeImage && noTextCoordinates(locator) && locator.ImageX != nil && locator.ImageY != nil &&
			locator.ImageWidth != nil && locator.ImageHeight != nil && *locator.ImageX >= 0 && *locator.ImageY >= 0 &&
			*locator.ImageWidth > 0 && *locator.ImageHeight > 0 && int64(*locator.ImageX)+int64(*locator.ImageWidth) <= 10000 &&
			int64(*locator.ImageY)+int64(*locator.ImageHeight) <= 10000
	default:
		return false
	}
}

func noTextCoordinates(locator Locator) bool {
	return locator.TextStart == nil && locator.TextEnd == nil && locator.TextSourceLength == nil
}

func noImageCoordinates(locator Locator) bool {
	return locator.ImageX == nil && locator.ImageY == nil && locator.ImageWidth == nil && locator.ImageHeight == nil
}

// CanonicalUUIDv7 只接受小写连字符格式 UUIDv7。
func CanonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

type canonicalSnapshot struct {
	Assets []canonicalAsset `json:"assets"`
}

type canonicalAsset struct {
	AssetID  string              `json:"asset_id"`
	Version  int64               `json:"asset_version"`
	Evidence []canonicalEvidence `json:"evidence"`
}

type canonicalEvidence struct {
	ID                     string       `json:"evidence_id"`
	Kind                   EvidenceKind `json:"evidence_kind"`
	Availability           Availability `json:"availability"`
	ReasonCode             string       `json:"reason_code"`
	ContentDigest          string       `json:"content_digest"`
	ExtractorSchemaVersion string       `json:"extractor_schema_version"`
	ExtractorVersion       string       `json:"extractor_version"`
	Locator                *Locator     `json:"locator"`
}

func snapshotToken(assets []Asset) (string, error) {
	canonical := canonicalSnapshot{Assets: make([]canonicalAsset, len(assets))}
	for assetIndex, asset := range assets {
		canonical.Assets[assetIndex] = canonicalAsset{AssetID: asset.ID, Version: asset.Version, Evidence: make([]canonicalEvidence, len(asset.Evidence))}
		for evidenceIndex, evidence := range asset.Evidence {
			canonical.Assets[assetIndex].Evidence[evidenceIndex] = canonicalEvidence{
				ID: evidence.ID, Kind: evidence.Kind, Availability: evidence.Availability, ReasonCode: evidence.ReasonCode,
				ContentDigest: evidence.ContentDigest, ExtractorSchemaVersion: evidence.ExtractorSchemaVersion,
				ExtractorVersion: evidence.ExtractorVersion, Locator: evidence.Locator,
			}
		}
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func mapRepositoryError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrNotFound) {
		return err
	}
	return ErrPersistence
}
