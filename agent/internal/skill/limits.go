package skill

import "fmt"

const (
	maxRuntimeNameBytes   = 160
	maxRuntimeTextBytes   = 32 * 1024
	maxInitialPromptBytes = 64 * 1024
)

// LimitsProfileV1 是双端部署前必须对齐的 Session Skill Snapshot 有界资源剖面。
type LimitsProfileV1 struct {
	// ProfileVersion 固定为 session_skill_snapshot_limits.v1。
	ProfileVersion string
	// MaxItems 限制单 Session 的 Skill 数量。
	MaxItems int
	// MaxRuntimeContentBytesPerItem 限制单项 Runtime canonical UTF-8 字节数。
	MaxRuntimeContentBytesPerItem int
	// MaxTotalRuntimeContentBytes 限制整个 Snapshot 的 Runtime canonical UTF-8 字节数。
	MaxTotalRuntimeContentBytes int
	// MaxSnapshotMetadataBytes 限制 metadata canonical 数组字节数。
	MaxSnapshotMetadataBytes int
	// MaxExamplesPerItem 限制每个 Skill 的示例数量。
	MaxExamplesPerItem int
	// MaxStarterPromptsPerItem 限制每个 Skill 的起始提示词数量。
	MaxStarterPromptsPerItem int
	// MaxAllowedGraphToolKeysPerItem 限制声明的 Graph Tool key 数量。
	MaxAllowedGraphToolKeysPerItem int
	// MaxPublicToolRefsPerItem 在 W1 必须为零。
	MaxPublicToolRefsPerItem int
	// MaxRPCRequestBytes 是 Kitex transport 层应配置的请求上限。
	MaxRPCRequestBytes int
	// MaxOutboxPlaintextBytes 是 Business 加密前整包明文上限。
	MaxOutboxPlaintextBytes int
}

// DefaultLimitsProfileV1 返回设计冻结的生产建议默认值。
func DefaultLimitsProfileV1() LimitsProfileV1 {
	return LimitsProfileV1{
		ProfileVersion:                 "session_skill_snapshot_limits.v1",
		MaxItems:                       16,
		MaxRuntimeContentBytesPerItem:  64 * 1024,
		MaxTotalRuntimeContentBytes:    256 * 1024,
		MaxSnapshotMetadataBytes:       128 * 1024,
		MaxExamplesPerItem:             16,
		MaxStarterPromptsPerItem:       16,
		MaxAllowedGraphToolKeysPerItem: 6,
		MaxPublicToolRefsPerItem:       0,
		MaxRPCRequestBytes:             2 * 1024 * 1024,
		MaxOutboxPlaintextBytes:        2 * 1024 * 1024,
	}
}

// ProtocolCeilingsV1 返回设计冻结的协议硬上限，部署配置不得超过这些值。
func ProtocolCeilingsV1() LimitsProfileV1 {
	return LimitsProfileV1{
		ProfileVersion:                 "session_skill_snapshot_limits.v1",
		MaxItems:                       32,
		MaxRuntimeContentBytesPerItem:  128 * 1024,
		MaxTotalRuntimeContentBytes:    1024 * 1024,
		MaxSnapshotMetadataBytes:       256 * 1024,
		MaxExamplesPerItem:             32,
		MaxStarterPromptsPerItem:       32,
		MaxAllowedGraphToolKeysPerItem: 6,
		MaxPublicToolRefsPerItem:       0,
		MaxRPCRequestBytes:             4 * 1024 * 1024,
		MaxOutboxPlaintextBytes:        4 * 1024 * 1024,
	}
}

// Validate 校验 profile 版本、正值关系、W1 public Tool 关闭和协议 ceiling。
func (profile LimitsProfileV1) Validate() error {
	ceiling := ProtocolCeilingsV1()
	if profile.ProfileVersion != ceiling.ProfileVersion {
		return fmt.Errorf("%w: unsupported limits profile", ErrInvalidContract)
	}
	values := []struct {
		name    string
		value   int
		ceiling int
	}{
		{"max_items", profile.MaxItems, ceiling.MaxItems},
		{"max_runtime_content_bytes_per_item", profile.MaxRuntimeContentBytesPerItem, ceiling.MaxRuntimeContentBytesPerItem},
		{"max_total_runtime_content_bytes", profile.MaxTotalRuntimeContentBytes, ceiling.MaxTotalRuntimeContentBytes},
		{"max_snapshot_metadata_bytes", profile.MaxSnapshotMetadataBytes, ceiling.MaxSnapshotMetadataBytes},
		{"max_examples_per_item", profile.MaxExamplesPerItem, ceiling.MaxExamplesPerItem},
		{"max_starter_prompts_per_item", profile.MaxStarterPromptsPerItem, ceiling.MaxStarterPromptsPerItem},
		{"max_allowed_graph_tool_keys_per_item", profile.MaxAllowedGraphToolKeysPerItem, ceiling.MaxAllowedGraphToolKeysPerItem},
		{"max_rpc_request_bytes", profile.MaxRPCRequestBytes, ceiling.MaxRPCRequestBytes},
		{"max_outbox_plaintext_bytes", profile.MaxOutboxPlaintextBytes, ceiling.MaxOutboxPlaintextBytes},
	}
	for _, item := range values {
		if item.value <= 0 || item.value > item.ceiling {
			return fmt.Errorf("%w: %s outside protocol range", ErrInvalidContract, item.name)
		}
	}
	if profile.MaxPublicToolRefsPerItem != 0 {
		return fmt.Errorf("%w: public tool refs must remain disabled", ErrInvalidContract)
	}
	if profile.MaxRuntimeContentBytesPerItem > profile.MaxTotalRuntimeContentBytes {
		return fmt.Errorf("%w: per-item runtime limit exceeds total", ErrInvalidContract)
	}
	if profile.MaxSnapshotMetadataBytes > profile.MaxRPCRequestBytes ||
		profile.MaxTotalRuntimeContentBytes > profile.MaxRPCRequestBytes {
		return fmt.Errorf("%w: snapshot limits exceed RPC limit", ErrInvalidContract)
	}
	return nil
}

// ValidateProducerLimitsV1 确认 Business Producer 不会发送超过任一 Agent Consumer 接收能力的负载。
func ValidateProducerLimitsV1(producer, consumer LimitsProfileV1) error {
	if err := producer.Validate(); err != nil {
		return fmt.Errorf("producer limits: %w", err)
	}
	if err := consumer.Validate(); err != nil {
		return fmt.Errorf("consumer limits: %w", err)
	}
	pairs := []struct {
		name     string
		producer int
		consumer int
	}{
		{"max_items", producer.MaxItems, consumer.MaxItems},
		{"max_runtime_content_bytes_per_item", producer.MaxRuntimeContentBytesPerItem, consumer.MaxRuntimeContentBytesPerItem},
		{"max_total_runtime_content_bytes", producer.MaxTotalRuntimeContentBytes, consumer.MaxTotalRuntimeContentBytes},
		{"max_snapshot_metadata_bytes", producer.MaxSnapshotMetadataBytes, consumer.MaxSnapshotMetadataBytes},
		{"max_examples_per_item", producer.MaxExamplesPerItem, consumer.MaxExamplesPerItem},
		{"max_starter_prompts_per_item", producer.MaxStarterPromptsPerItem, consumer.MaxStarterPromptsPerItem},
		{"max_allowed_graph_tool_keys_per_item", producer.MaxAllowedGraphToolKeysPerItem, consumer.MaxAllowedGraphToolKeysPerItem},
		{"max_rpc_request_bytes", producer.MaxRPCRequestBytes, consumer.MaxRPCRequestBytes},
		{"max_outbox_plaintext_bytes", producer.MaxOutboxPlaintextBytes, consumer.MaxOutboxPlaintextBytes},
	}
	for _, pair := range pairs {
		if pair.producer > pair.consumer {
			return fmt.Errorf("%w: producer %s exceeds consumer", ErrInvalidContract, pair.name)
		}
	}
	return nil
}
