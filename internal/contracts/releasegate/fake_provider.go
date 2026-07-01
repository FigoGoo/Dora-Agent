package releasegate

import (
	"errors"
	"fmt"
)

var RequiredFakeProviderBehaviors = []string{
	"deterministic_success",
	"async_pending",
	"partial_success",
	"transient_failure",
	"terminal_failure",
	"slow_callback",
}

type FakeProviderManifest struct {
	SchemaVersion     string             `json:"schema_version"`
	Status            string             `json:"status"`
	Owner             string             `json:"owner"`
	UpdatedAt         string             `json:"updated_at"`
	Purpose           string             `json:"purpose"`
	Providers         []FakeProvider     `json:"providers"`
	BehaviorContracts []BehaviorContract `json:"behavior_contracts"`
	RedisStreams      []string           `json:"redis_streams"`
	CacheKeys         []string           `json:"cache_keys"`
	IdempotencyRule   string             `json:"idempotency_rule"`
}

type FakeProvider struct {
	ProviderID         string   `json:"provider_id"`
	Capability         string   `json:"capability"`
	SupportedBehaviors []string `json:"supported_behaviors"`
	OutputDigestPolicy string   `json:"output_digest_policy"`
}

type BehaviorContract struct {
	BehaviorID           string   `json:"behavior_id"`
	Trigger              string   `json:"trigger"`
	ExpectedResult       string   `json:"expected_result"`
	Retryable            bool     `json:"retryable"`
	MustEmitStreamEvents []string `json:"must_emit_stream_events"`
}

type ProviderScenarios struct {
	SchemaVersion string             `json:"schema_version"`
	Status        string             `json:"status"`
	Owner         string             `json:"owner"`
	UpdatedAt     string             `json:"updated_at"`
	Scenarios     []ProviderScenario `json:"scenarios"`
}

type ProviderScenario struct {
	CaseID     string         `json:"case_id"`
	BehaviorID string         `json:"behavior_id"`
	ProviderID string         `json:"provider_id"`
	Request    map[string]any `json:"request"`
	Expected   map[string]any `json:"expected"`
}

type FakeProviderExecution struct {
	CaseID                                string
	ProviderID                            string
	BehaviorID                            string
	Status                                string
	OutputDigest                          string
	DedupeKey                             string
	Streams                               []string
	InitialStatus                         string
	FinalStatus                           string
	FirstAttemptStatus                    string
	RetryAttempt                          int
	ErrorCode                             string
	CreditHoldStatus                      string
	CallbackAfterMS                       int
	CommittedAssetCount                   int
	FailedAssetCount                      int
	FailedAssetsMustNotBeCharged          bool
	ReleasedCreditsMustEqualFrozenCredits bool
	WorkerRestartRequired                 bool
	DuplicateCallbackMustBeIgnored        bool
}

func ValidateFakeProviderArtifacts(manifest FakeProviderManifest, scenarios ProviderScenarios) error {
	if manifest.SchemaVersion != "fake_provider_manifest.v1" || manifest.Status != "active" {
		return errors.New("fake provider manifest must be active fake_provider_manifest.v1")
	}
	if scenarios.SchemaVersion != "fake_provider_scenarios.v1" || scenarios.Status != "active" {
		return errors.New("fake provider scenarios must be active fake_provider_scenarios.v1")
	}
	if len(manifest.Providers) == 0 || len(manifest.BehaviorContracts) == 0 || len(scenarios.Scenarios) == 0 {
		return errors.New("fake provider providers, behavior contracts and scenarios are required")
	}
	manifestBehaviors := make(map[string]struct{}, len(manifest.BehaviorContracts))
	for index, contract := range manifest.BehaviorContracts {
		if contract.BehaviorID == "" || contract.Trigger == "" || contract.ExpectedResult == "" {
			return fmt.Errorf("behavior_contract %d missing required fields", index+1)
		}
		if len(contract.MustEmitStreamEvents) == 0 {
			return fmt.Errorf("behavior_contract %q must emit stream events", contract.BehaviorID)
		}
		manifestBehaviors[contract.BehaviorID] = struct{}{}
	}
	providerBehaviors := map[string]struct{}{}
	for index, provider := range manifest.Providers {
		if provider.ProviderID == "" || provider.Capability == "" || provider.OutputDigestPolicy == "" {
			return fmt.Errorf("provider %d missing required fields", index+1)
		}
		for _, behavior := range provider.SupportedBehaviors {
			providerBehaviors[behavior] = struct{}{}
		}
	}
	scenarioBehaviors := map[string]struct{}{}
	for index, scenario := range scenarios.Scenarios {
		if scenario.CaseID == "" || scenario.ProviderID == "" || scenario.BehaviorID == "" || scenario.Expected == nil {
			return fmt.Errorf("scenario %d missing required fields", index+1)
		}
		scenarioBehaviors[scenario.BehaviorID] = struct{}{}
	}
	for _, behavior := range RequiredFakeProviderBehaviors {
		if _, ok := manifestBehaviors[behavior]; !ok {
			return fmt.Errorf("manifest missing behavior %q", behavior)
		}
		if _, ok := providerBehaviors[behavior]; !ok {
			return fmt.Errorf("providers do not support behavior %q", behavior)
		}
		if _, ok := scenarioBehaviors[behavior]; !ok {
			return fmt.Errorf("scenarios missing behavior %q", behavior)
		}
	}
	if len(manifest.RedisStreams) == 0 || len(manifest.CacheKeys) == 0 || manifest.IdempotencyRule == "" {
		return errors.New("redis_streams, cache_keys and idempotency_rule are required")
	}
	return nil
}

func SimulateFakeProviderScenario(scenario ProviderScenario) (FakeProviderExecution, error) {
	if scenario.CaseID == "" || scenario.ProviderID == "" || scenario.BehaviorID == "" {
		return FakeProviderExecution{}, errors.New("fake provider scenario identity is required")
	}
	if scenario.Expected == nil {
		return FakeProviderExecution{}, fmt.Errorf("scenario %q expected result is required", scenario.CaseID)
	}

	execution := FakeProviderExecution{
		CaseID:     scenario.CaseID,
		ProviderID: scenario.ProviderID,
		BehaviorID: scenario.BehaviorID,
	}
	switch scenario.BehaviorID {
	case "deterministic_success":
		status, err := requiredString(scenario.Expected, "status")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		outputDigest, err := requiredString(scenario.Expected, "output_digest")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		dedupeKey, err := requiredString(scenario.Expected, "dedupe_key")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		stream, err := requiredString(scenario.Expected, "stream")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		execution.Status = status
		execution.OutputDigest = outputDigest
		execution.DedupeKey = dedupeKey
		execution.Streams = []string{stream}
	case "async_pending":
		initialStatus, err := requiredString(scenario.Expected, "initial_status")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		finalStatus, err := requiredString(scenario.Expected, "final_status")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		callbackAfterMS, err := requiredInt(scenario.Expected, "callback_after_ms")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		streams, err := requiredStringSlice(scenario.Expected, "streams")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		execution.InitialStatus = initialStatus
		execution.FinalStatus = finalStatus
		execution.Status = finalStatus
		execution.CallbackAfterMS = callbackAfterMS
		execution.Streams = streams
	case "partial_success":
		status, err := requiredString(scenario.Expected, "status")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		committedAssetCount, err := requiredInt(scenario.Expected, "committed_asset_count")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		failedAssetCount, err := requiredInt(scenario.Expected, "failed_asset_count")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		failedAssetsMustNotBeCharged, err := requiredBool(scenario.Expected, "failed_assets_must_not_be_charged")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		execution.Status = status
		execution.CommittedAssetCount = committedAssetCount
		execution.FailedAssetCount = failedAssetCount
		execution.FailedAssetsMustNotBeCharged = failedAssetsMustNotBeCharged
	case "transient_failure":
		firstAttemptStatus, err := requiredString(scenario.Expected, "first_attempt_status")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		retryAttempt, err := requiredInt(scenario.Expected, "retry_attempt")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		finalStatus, err := requiredString(scenario.Expected, "final_status")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		errorCode, err := requiredString(scenario.Expected, "error_code")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		execution.FirstAttemptStatus = firstAttemptStatus
		execution.RetryAttempt = retryAttempt
		execution.FinalStatus = finalStatus
		execution.Status = finalStatus
		execution.ErrorCode = errorCode
	case "terminal_failure":
		status, err := requiredString(scenario.Expected, "status")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		errorCode, err := requiredString(scenario.Expected, "error_code")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		creditHoldStatus, err := requiredString(scenario.Expected, "credit_hold_status")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		releasedCreditsMustEqualFrozenCredits, err := requiredBool(scenario.Expected, "released_credits_must_equal_frozen_credits")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		execution.Status = status
		execution.ErrorCode = errorCode
		execution.CreditHoldStatus = creditHoldStatus
		execution.ReleasedCreditsMustEqualFrozenCredits = releasedCreditsMustEqualFrozenCredits
	case "slow_callback":
		workerRestartRequired, err := requiredBool(scenario.Expected, "worker_restart_required")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		resumeStatus, err := requiredString(scenario.Expected, "resume_status")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		duplicateCallbackMustBeIgnored, err := requiredBool(scenario.Expected, "duplicate_callback_must_be_ignored")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		callbackAfterMS, err := requiredInt(scenario.Expected, "callback_after_ms")
		if err != nil {
			return FakeProviderExecution{}, err
		}
		execution.WorkerRestartRequired = workerRestartRequired
		execution.Status = resumeStatus
		execution.DuplicateCallbackMustBeIgnored = duplicateCallbackMustBeIgnored
		execution.CallbackAfterMS = callbackAfterMS
	default:
		return FakeProviderExecution{}, fmt.Errorf("unsupported fake provider behavior %q", scenario.BehaviorID)
	}
	if execution.Status == "" && execution.FinalStatus == "" {
		return FakeProviderExecution{}, fmt.Errorf("scenario %q did not produce a terminal status", scenario.CaseID)
	}
	return execution, nil
}

func IsRequiredFakeProviderBehavior(value string) bool {
	for _, behavior := range RequiredFakeProviderBehaviors {
		if behavior == value {
			return true
		}
	}
	return false
}

func requiredString(values map[string]any, key string) (string, error) {
	value, ok := values[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return text, nil
}

func requiredBool(values map[string]any, key string) (bool, error) {
	value, ok := values[key]
	if !ok {
		return false, fmt.Errorf("%s is required", key)
	}
	flag, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return flag, nil
}

func requiredInt(values map[string]any, key string) (int, error) {
	value, ok := values[key]
	if !ok {
		return 0, fmt.Errorf("%s is required", key)
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case float64:
		if typed != float64(int(typed)) {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		return int(typed), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func requiredStringSlice(values map[string]any, key string) ([]string, error) {
	value, ok := values[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	rawItems, ok := value.([]any)
	if !ok || len(rawItems) == 0 {
		return nil, fmt.Errorf("%s must be a non-empty string array", key)
	}
	items := make([]string, 0, len(rawItems))
	for index, rawItem := range rawItems {
		item, ok := rawItem.(string)
		if !ok || item == "" {
			return nil, fmt.Errorf("%s[%d] must be a non-empty string", key, index)
		}
		items = append(items, item)
	}
	return items, nil
}
