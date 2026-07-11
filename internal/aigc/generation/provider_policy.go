package generation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	DefaultImageModel       = "gpt-image-2"
	MaxImageOutputVariants  = 4
	DefaultVideoModel       = "doubao-seedance-2-0-fast-260128"
	MaxVideoDurationSeconds = 30
	MaxVideoFramesPerSecond = 60
	MaxProviderPromptRunes  = 20000
)

var (
	allowedImageModels = map[string]struct{}{
		DefaultImageModel: {},
	}
	allowedImageSizes = map[string]struct{}{
		"1024x1024": {}, "1024x1536": {}, "1536x1024": {},
	}
	allowedVideoModels = map[string]struct{}{
		DefaultVideoModel: {}, "doubao-seedance-2-0-260128": {},
	}
	allowedVideoRatios = map[string]struct{}{
		"16:9": {}, "9:16": {}, "1:1": {}, "4:3": {}, "3:4": {}, "21:9": {},
	}
	allowedVideoResolutions = map[string]struct{}{
		"480p": {}, "720p": {}, "1080p": {},
	}
)

// ValidateProviderJob is the server-side trust boundary for provider
// parameters. Storyboard content may be proposed by an LLM, so a ToolInfo enum
// is not sufficient protection for Graph-internal provider calls.
func ValidateProviderJob(job GenerationJob) error {
	payload := job.Payload
	prompt := strings.TrimSpace(stringValue(payload["prompt"]))
	if (job.Provider == ProviderImage2 || job.Provider == ProviderSeedance) && prompt == "" {
		return fmt.Errorf("provider prompt is required")
	}
	if len([]rune(prompt)) > MaxProviderPromptRunes {
		return fmt.Errorf("provider prompt exceeds %d characters", MaxProviderPromptRunes)
	}

	switch strings.TrimSpace(job.Provider) {
	case ProviderImage2:
		model := valueOrDefault(stringValue(payload["model"]), DefaultImageModel)
		if _, ok := allowedImageModels[model]; !ok {
			return fmt.Errorf("unsupported image model %q", model)
		}
		size := valueOrDefault(stringValue(payload["size"]), "1024x1024")
		if _, ok := allowedImageSizes[size]; !ok {
			return fmt.Errorf("unsupported image size %q", size)
		}
		n, err := boundedInteger(payload["n"], 1)
		if err != nil || n < 1 || n > MaxImageOutputVariants {
			return fmt.Errorf("image output count n must be an integer between 1 and %d", MaxImageOutputVariants)
		}
	case ProviderSeedance:
		model := valueOrDefault(stringValue(payload["model"]), DefaultVideoModel)
		if _, ok := allowedVideoModels[model]; !ok {
			return fmt.Errorf("unsupported video model %q", model)
		}
		if ratio := stringValue(payload["ratio"]); ratio != "" {
			if _, ok := allowedVideoRatios[ratio]; !ok {
				return fmt.Errorf("unsupported video ratio %q", ratio)
			}
		}
		if resolution := strings.ToLower(stringValue(payload["resolution"])); resolution != "" {
			if _, ok := allowedVideoResolutions[resolution]; !ok {
				return fmt.Errorf("unsupported video resolution %q", resolution)
			}
		}
		duration, err := boundedInteger(payload["duration_seconds"], 0)
		if err != nil || duration < 0 || duration > MaxVideoDurationSeconds {
			return fmt.Errorf("video duration_seconds must be an integer between 0 and %d", MaxVideoDurationSeconds)
		}
		fps, err := boundedInteger(payload["fps"], 0)
		if err != nil || fps < 0 || fps > MaxVideoFramesPerSecond {
			return fmt.Errorf("video fps must be an integer between 0 and %d", MaxVideoFramesPerSecond)
		}
	}
	return nil
}

// OutputVariantCount is used by preflight and final settlement so estimates
// account for every requested image instead of charging a flat one-image rate.
func OutputVariantCount(job GenerationJob) int64 {
	if strings.TrimSpace(job.Provider) != ProviderImage2 {
		return 1
	}
	n, err := boundedInteger(job.Payload["n"], 1)
	if err != nil || n < 1 {
		return 1
	}
	return int64(n)
}

// ProviderErrorRetryable classifies failures from legacy synchronous handlers
// and native provider adapters. Validation/configuration/authorization failures
// are permanent; transport, storage and 5xx-like failures default to retryable.
func ProviderErrorRetryable(err error) bool {
	if err == nil {
		return false
	}
	var execution *ExecutionError
	if errors.As(err, &execution) {
		return execution.Retryable
	}
	message := strings.ToLower(err.Error())
	permanentMarkers := []string{
		" is required", "required for", "not configured", "not registered", "unsupported", "invalid ",
		"must be ", "api key", "unauthorized", "forbidden", "bad request",
		" 400", "status 400", " 401", "status 401", " 403", "status 403",
		" 404", "status 404", " 405", "status 405", " 413", "status 413",
		" 422", "status 422",
	}
	for _, marker := range permanentMarkers {
		if strings.Contains(message, marker) {
			return false
		}
	}
	return true
}

func boundedInteger(value any, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int8:
		return int(typed), nil
	case int16:
		return int(typed), nil
	case int32:
		return int(typed), nil
	case int64:
		return int(typed), nil
	case uint:
		return int(typed), nil
	case uint8:
		return int(typed), nil
	case uint16:
		return int(typed), nil
	case uint32:
		return int(typed), nil
	case uint64:
		if uint64(int(typed)) != typed {
			return 0, fmt.Errorf("integer overflows")
		}
		return int(typed), nil
	case float64:
		integer := int(typed)
		if float64(integer) != typed {
			return 0, fmt.Errorf("not an integer")
		}
		return integer, nil
	case float32:
		integer := int(typed)
		if float32(integer) != typed {
			return 0, fmt.Errorf("not an integer")
		}
		return integer, nil
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err
	default:
		return 0, fmt.Errorf("unsupported integer type %T", value)
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
