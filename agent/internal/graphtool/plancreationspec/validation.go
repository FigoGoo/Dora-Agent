package plancreationspec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const maxJSONBytes = 64 * 1024

var phaseKeyPattern = regexp.MustCompile(`^phase_[1-6]$`)

// DecodeIntent 对 HTTP/Tool 原始 JSON 执行大小、UTF-8、重复字段、未知字段、尾随值和全部领域边界校验。
func DecodeIntent(encoded []byte) (Intent, error) {
	if len(encoded) == 0 || len(encoded) > maxJSONBytes || !utf8.Valid(encoded) || !validJSONSurrogateEscapes(encoded) {
		return Intent{}, fmt.Errorf("decode preview intent: invalid JSON size or UTF-8")
	}
	if err := rejectDuplicateJSONFields(encoded); err != nil {
		return Intent{}, fmt.Errorf("decode preview intent: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var intent Intent
	if err := decoder.Decode(&intent); err != nil {
		return Intent{}, fmt.Errorf("decode preview intent: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Intent{}, fmt.Errorf("decode preview intent: trailing JSON")
	}
	if err := ValidateIntent(intent); err != nil {
		return Intent{}, err
	}
	return intent, nil
}

// ValidateIntent 校验冻结版本、枚举、NFC、长度、非 null 数组和精确去重约束。
func ValidateIntent(intent Intent) error {
	if intent.SchemaVersion != IntentSchemaVersion || !validText(intent.Goal, 1, 2000, false) {
		return fmt.Errorf("validate preview intent: invalid schema_version or goal")
	}
	if !validDeliverable(intent.DeliverableType) || !validLocale(intent.Locale) || intent.Constraints == nil || len(intent.Constraints) > 8 {
		return fmt.Errorf("validate preview intent: invalid enum or constraints")
	}
	if intent.Audience != nil && !validText(*intent.Audience, 0, 500, true) {
		return fmt.Errorf("validate preview intent: invalid audience")
	}
	if !validUniqueStrings(intent.Constraints, 1, 200) {
		return fmt.Errorf("validate preview intent: invalid or duplicate constraint")
	}
	return nil
}

// IntentDigest 计算严格 Intent JSON 的小写 SHA-256；字段顺序由具名 DTO 固定。
func IntentDigest(intent Intent) (string, error) {
	if err := ValidateIntent(intent); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(intent)
	if err != nil {
		return "", fmt.Errorf("encode preview intent digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// jsonMarshalIntent 只在 Prompt 边界编码已通过校验的具名 Intent，保持 audience 省略语义。
func jsonMarshalIntent(intent Intent) ([]byte, error) {
	if err := ValidateIntent(intent); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(intent)
	if err != nil {
		return nil, fmt.Errorf("encode preview intent: %w", err)
	}
	return encoded, nil
}

// DecodeAndValidateProposal 严格解析模型候选，并以可信 Intent 关闭枚举、目标和硬约束边界。
func DecodeAndValidateProposal(encoded []byte, intent Intent) (Content, Proposal, error) {
	if len(encoded) == 0 || len(encoded) > maxJSONBytes || !utf8.Valid(encoded) || !validJSONSurrogateEscapes(encoded) {
		return Content{}, Proposal{}, fmt.Errorf("validate proposal: invalid JSON size or UTF-8")
	}
	if err := rejectDuplicateJSONFields(encoded); err != nil {
		return Content{}, Proposal{}, fmt.Errorf("validate proposal: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var proposal Proposal
	if err := decoder.Decode(&proposal); err != nil {
		return Content{}, Proposal{}, fmt.Errorf("validate proposal: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Content{}, Proposal{}, fmt.Errorf("validate proposal: trailing JSON")
	}
	if proposal.SchemaVersion != ProposalSchemaVersion || proposal.DeliverableType != intent.DeliverableType || proposal.Goal != intent.Goal ||
		!validText(proposal.Title, 1, 80, false) || !validText(proposal.Goal, 1, 2000, false) ||
		!validText(proposal.Audience, 0, 500, true) || len(proposal.Phases) < 1 || len(proposal.Phases) > 6 ||
		proposal.Constraints == nil || len(proposal.Constraints) > 8 || proposal.AcceptanceCriteria == nil ||
		len(proposal.AcceptanceCriteria) < 1 || len(proposal.AcceptanceCriteria) > 8 {
		return Content{}, Proposal{}, fmt.Errorf("validate proposal: invalid scalar or collection boundary")
	}
	phaseKeys := make(map[string]struct{}, len(proposal.Phases))
	for _, phase := range proposal.Phases {
		if !phaseKeyPattern.MatchString(phase.Key) || !validText(phase.Title, 1, 80, false) ||
			!validText(phase.Objective, 1, 500, false) || !validText(phase.Output, 1, 500, false) {
			return Content{}, Proposal{}, fmt.Errorf("validate proposal: invalid phase")
		}
		if _, duplicated := phaseKeys[phase.Key]; duplicated {
			return Content{}, Proposal{}, fmt.Errorf("validate proposal: duplicate phase key")
		}
		phaseKeys[phase.Key] = struct{}{}
	}
	if !validUniqueStrings(proposal.Constraints, 1, 200) || !validUniqueStrings(proposal.AcceptanceCriteria, 1, 240) {
		return Content{}, Proposal{}, fmt.Errorf("validate proposal: invalid or duplicate list item")
	}
	constraintSet := make(map[string]struct{}, len(proposal.Constraints))
	for _, constraint := range proposal.Constraints {
		constraintSet[constraint] = struct{}{}
	}
	for _, required := range intent.Constraints {
		if _, exists := constraintSet[required]; !exists {
			return Content{}, Proposal{}, fmt.Errorf("validate proposal: required constraint is missing")
		}
	}
	if intent.Audience != nil && proposal.Audience != *intent.Audience {
		return Content{}, Proposal{}, fmt.Errorf("validate proposal: audience changed")
	}
	content := Content{
		Title: proposal.Title, Goal: proposal.Goal, DeliverableType: proposal.DeliverableType,
		Audience: proposal.Audience, Locale: intent.Locale, Phases: clonePhases(proposal.Phases),
		Constraints:        cloneRequiredStrings(proposal.Constraints),
		AcceptanceCriteria: cloneRequiredStrings(proposal.AcceptanceCriteria),
	}
	if err := ValidateContent(content); err != nil {
		return Content{}, Proposal{}, err
	}
	return content, proposal, nil
}

// ValidateContent 校验 Agent→Business 与 Business→Agent 共用内容的不变量。
func ValidateContent(content Content) error {
	if !validText(content.Title, 1, 80, false) || !validText(content.Goal, 1, 2000, false) ||
		!validText(content.Audience, 0, 500, true) || !validDeliverable(content.DeliverableType) || !validLocale(content.Locale) ||
		len(content.Phases) < 1 || len(content.Phases) > 6 || content.Constraints == nil || len(content.Constraints) > 8 ||
		content.AcceptanceCriteria == nil || len(content.AcceptanceCriteria) < 1 || len(content.AcceptanceCriteria) > 8 {
		return fmt.Errorf("validate creation spec content: invalid boundary")
	}
	phaseKeys := make(map[string]struct{}, len(content.Phases))
	for _, phase := range content.Phases {
		if !phaseKeyPattern.MatchString(phase.Key) || !validText(phase.Title, 1, 80, false) ||
			!validText(phase.Objective, 1, 500, false) || !validText(phase.Output, 1, 500, false) {
			return fmt.Errorf("validate creation spec content: invalid phase")
		}
		if _, duplicated := phaseKeys[phase.Key]; duplicated {
			return fmt.Errorf("validate creation spec content: duplicate phase")
		}
		phaseKeys[phase.Key] = struct{}{}
	}
	if !validUniqueStrings(content.Constraints, 1, 200) || !validUniqueStrings(content.AcceptanceCriteria, 1, 240) {
		return fmt.Errorf("validate creation spec content: invalid list")
	}
	return nil
}

// SaveRequestDigest 按跨 Module 固定字段顺序计算 Agent→Business Command 摘要。
// command_id、request_id、时间和 Run attempt 不参与语义，Unknown Outcome 必须复用原摘要。
func SaveRequestDigest(command DraftCommand) (string, error) {
	if !canonicalUUIDv7(command.TrustedContext.UserID) || !canonicalUUIDv7(command.TrustedContext.ProjectID) ||
		!canonicalUUIDv7(command.TrustedContext.ToolCallID) || command.DomainContext.ProjectVersion < 1 ||
		command.TrustedContext.ProjectID != command.DomainContext.ProjectID ||
		command.TrustedContext.ProjectID == "" || command.TrustedContext.PromptVersion == "" ||
		command.TrustedContext.ValidatorVersion == "" {
		return "", fmt.Errorf("compute save request digest: invalid trusted context")
	}
	if err := ValidateContent(command.Content); err != nil {
		return "", err
	}
	// saveDigestWire 的声明顺序就是 Business `encoding/json` producer 的冻结 canonical 顺序。
	wire := saveDigestWire{
		SchemaVersion:          SaveDigestSchemaVersion,
		UserID:                 command.TrustedContext.UserID,
		ProjectID:              command.TrustedContext.ProjectID,
		ExpectedProjectVersion: command.DomainContext.ProjectVersion,
		ToolCallID:             command.TrustedContext.ToolCallID,
		PromptVersion:          command.TrustedContext.PromptVersion,
		ValidatorVersion:       command.TrustedContext.ValidatorVersion,
		Content:                command.Content,
	}
	encoded, err := json.Marshal(wire)
	if err != nil || len(encoded) > maxJSONBytes {
		return "", fmt.Errorf("compute save request digest: encode canonical wire")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// ContentDigest 计算 Business Draft Content 的冻结 JSON 摘要，用于校验 RPC 回传资源。
func ContentDigest(content Content) (string, error) {
	if err := ValidateContent(content); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("compute creation spec content digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// ValidateResource 校验 Business 返回资源的绑定、内容和摘要一致性。
func ValidateResource(resource Resource, expectedProjectID string) error {
	if !canonicalUUIDv7(resource.ID) || !canonicalUUIDv7(resource.ProjectID) || resource.ProjectID != expectedProjectID ||
		resource.Version < 1 || resource.Status != "draft" || !validLowerSHA256(resource.ContentDigest) {
		return fmt.Errorf("validate creation spec resource: invalid identity or state")
	}
	actualDigest, err := ContentDigest(resource.Content)
	if err != nil || actualDigest != resource.ContentDigest {
		return fmt.Errorf("validate creation spec resource: content digest mismatch")
	}
	return nil
}

// ValidateResourceForCommand 除了校验 Business 自洽响应，还要求其内容精确绑定原保存命令。
// 这可阻止一个合法但属于旧命令的 Draft 被错误接受为本次 Save/Query 的结果。
func ValidateResourceForCommand(resource Resource, command DraftCommand) error {
	if err := ValidateResource(resource, command.TrustedContext.ProjectID); err != nil {
		return err
	}
	expectedDigest, err := ContentDigest(command.Content)
	if err != nil || resource.ContentDigest != expectedDigest {
		return fmt.Errorf("validate creation spec resource: command content digest mismatch")
	}
	return nil
}

// ValidateCard 校验持久化 Workspace/Event 共用 Card 的完整安全边界。
func ValidateCard(card Card) error {
	if card.SchemaVersion != CardSchemaVersion || !canonicalUUIDv7(card.CreationSpecID) ||
		!canonicalUUIDv7(card.ProjectID) || card.Version < 1 || card.Status != "draft" ||
		!validLowerSHA256(card.ContentDigest) || card.UpdatedAt.IsZero() || card.UpdatedAt.Location() != time.UTC {
		return fmt.Errorf("validate creation spec card: invalid identity or state")
	}
	content := Content{
		Title: card.Title, Goal: card.Goal, DeliverableType: card.DeliverableType,
		Audience: card.Audience, Locale: card.Locale, Phases: card.Phases,
		Constraints: card.Constraints, AcceptanceCriteria: card.AcceptanceCriteria,
	}
	if err := ValidateContent(content); err != nil {
		return fmt.Errorf("validate creation spec card: %w", err)
	}
	digest, err := ContentDigest(content)
	if err != nil || digest != card.ContentDigest {
		return fmt.Errorf("validate creation spec card: content digest mismatch")
	}
	return nil
}

// ValidateTerminalResult 校验可冻结 completed/failed Result；recovery_pending 绝不能进入终态回执。
func ValidateTerminalResult(result Result, trusted TrustedContext) error {
	if !validTrustedContext(trusted) || result.ReceiptRef.ToolCallID != trusted.ToolCallID ||
		result.ReceiptRef.BusinessCommandID != trusted.BusinessCommandID {
		return fmt.Errorf("validate creation spec result: receipt identity mismatch")
	}
	switch result.Status {
	case "completed":
		if result.ResultCode != ResultCodeCreated || result.ResourceRef == nil || result.Card == nil ||
			result.Summary != "" || result.Retryable || result.BusinessRequestDigest == "" {
			return fmt.Errorf("validate creation spec result: invalid completed shape")
		}
		if err := ValidateCard(*result.Card); err != nil {
			return err
		}
		if result.ResourceRef.ID != result.Card.CreationSpecID || result.ResourceRef.Version != result.Card.Version ||
			result.ResourceRef.Digest != result.Card.ContentDigest || result.ResourceRef.Status != result.Card.Status ||
			result.Card.ProjectID != trusted.ProjectID || !validLowerSHA256(result.BusinessRequestDigest) {
			return fmt.Errorf("validate creation spec result: resource/card mismatch")
		}
	case "failed":
		if result.ResultCode == "" || result.ResourceRef != nil || result.Card != nil || result.Summary == "" ||
			result.BusinessRequestDigest != "" {
			return fmt.Errorf("validate creation spec result: invalid failed shape")
		}
	default:
		return fmt.Errorf("validate creation spec result: terminal status is invalid")
	}
	return nil
}

// saveDigestWire 固定跨 Module 保存摘要的字段顺序，禁止改为 map。
type saveDigestWire struct {
	SchemaVersion          string  `json:"schema_version"`
	UserID                 string  `json:"user_id"`
	ProjectID              string  `json:"project_id"`
	ExpectedProjectVersion int64   `json:"expected_project_version"`
	ToolCallID             string  `json:"tool_call_id"`
	PromptVersion          string  `json:"prompt_version"`
	ValidatorVersion       string  `json:"validator_version"`
	Content                Content `json:"content"`
}

// durableDraftCommandWire 是 Save RPC 的完整可重放语义工件；故意不包含会随 Claim 变化的 Owner 与 Fence。
// 字段顺序属于密文工件摘要契约，变更时必须升级 DurableDraftCommandSchemaVersion。
type durableDraftCommandWire struct {
	SchemaVersion          string  `json:"schema_version"`
	RequestID              string  `json:"request_id"`
	CommandID              string  `json:"command_id"`
	RequestDigest          string  `json:"request_digest"`
	UserID                 string  `json:"user_id"`
	ProjectID              string  `json:"project_id"`
	ExpectedProjectVersion int64   `json:"expected_project_version"`
	ToolCallID             string  `json:"tool_call_id"`
	PromptVersion          string  `json:"prompt_version"`
	ValidatorVersion       string  `json:"validator_version"`
	Content                Content `json:"content"`
}

// EncodeDurableDraftCommand 严格编码 Save RPC 的全部稳定字段，并返回工件与 Content 摘要。
// 密文中不保存 Owner/Fence；进程重启后必须用当前 Claim 重建这两个并发边界。
func EncodeDurableDraftCommand(command DraftCommand) ([]byte, string, string, error) {
	if !validTrustedContext(command.TrustedContext) || command.DomainContext.ProjectID != command.TrustedContext.ProjectID ||
		command.DomainContext.ProjectVersion < 1 || !validLowerSHA256(command.RequestDigest) {
		return nil, "", "", fmt.Errorf("encode durable draft command: invalid trusted command")
	}
	recomputed, err := SaveRequestDigest(command)
	if err != nil || recomputed != command.RequestDigest {
		return nil, "", "", fmt.Errorf("encode durable draft command: request digest mismatch")
	}
	contentDigest, err := ContentDigest(command.Content)
	if err != nil {
		return nil, "", "", err
	}
	wire := durableDraftCommandWire{
		SchemaVersion: DurableDraftCommandSchemaVersion,
		RequestID:     command.TrustedContext.RequestID, CommandID: command.TrustedContext.BusinessCommandID,
		RequestDigest: command.RequestDigest, UserID: command.TrustedContext.UserID,
		ProjectID: command.TrustedContext.ProjectID, ExpectedProjectVersion: command.DomainContext.ProjectVersion,
		ToolCallID: command.TrustedContext.ToolCallID, PromptVersion: command.TrustedContext.PromptVersion,
		ValidatorVersion: command.TrustedContext.ValidatorVersion, Content: command.Content,
	}
	encoded, err := json.Marshal(wire)
	if err != nil || len(encoded) == 0 || len(encoded) > maxJSONBytes {
		return nil, "", "", fmt.Errorf("encode durable draft command: canonical JSON failed")
	}
	digest := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(digest[:]), contentDigest, nil
}

// DecodeDurableDraftCommand 严格解码 AEAD 明文，并以当前 Claim 重建 Owner/Fence。
// 所有稳定身份、版本 pin 和摘要必须与当前可信上下文一致，防止跨请求密文替换。
func DecodeDurableDraftCommand(encoded []byte, trusted TrustedContext) (DraftCommand, error) {
	if !validTrustedContext(trusted) || len(encoded) == 0 || len(encoded) > maxJSONBytes ||
		!utf8.Valid(encoded) || !validJSONSurrogateEscapes(encoded) {
		return DraftCommand{}, fmt.Errorf("decode durable draft command: invalid input")
	}
	if err := rejectDuplicateJSONFields(encoded); err != nil {
		return DraftCommand{}, fmt.Errorf("decode durable draft command: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var wire durableDraftCommandWire
	if err := decoder.Decode(&wire); err != nil {
		return DraftCommand{}, fmt.Errorf("decode durable draft command: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return DraftCommand{}, fmt.Errorf("decode durable draft command: trailing JSON")
	}
	if wire.SchemaVersion != DurableDraftCommandSchemaVersion || wire.RequestID != trusted.RequestID ||
		wire.CommandID != trusted.BusinessCommandID || wire.UserID != trusted.UserID ||
		wire.ProjectID != trusted.ProjectID || wire.ToolCallID != trusted.ToolCallID ||
		wire.PromptVersion != trusted.PromptVersion || wire.ValidatorVersion != trusted.ValidatorVersion ||
		wire.ExpectedProjectVersion < 1 || !validLowerSHA256(wire.RequestDigest) {
		return DraftCommand{}, fmt.Errorf("decode durable draft command: identity or version mismatch")
	}
	command := DraftCommand{
		TrustedContext: trusted,
		DomainContext:  DomainContext{ProjectID: wire.ProjectID, ProjectVersion: wire.ExpectedProjectVersion},
		Content:        wire.Content, RequestDigest: wire.RequestDigest,
	}
	recomputed, err := SaveRequestDigest(command)
	if err != nil || recomputed != command.RequestDigest {
		return DraftCommand{}, fmt.Errorf("decode durable draft command: request digest mismatch")
	}
	return command, nil
}

// rejectDuplicateJSONFields 递归拒绝任意 Object 层级同名字段，避免 last-write-wins 第二语义。
func rejectDuplicateJSONFields(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return fmt.Errorf("JSON contains trailing value")
	}
	return nil
}

// consumeUniqueJSONValue 递归消费一个 JSON 值，并在每个 Object 维护独立字段集合。
func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("JSON null is not allowed")
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			fieldToken, err := decoder.Token()
			if err != nil {
				return err
			}
			field, ok := fieldToken.(string)
			if !ok {
				return fmt.Errorf("JSON object field is not string")
			}
			if _, duplicated := seen[field]; duplicated {
				return fmt.Errorf("JSON contains duplicate field %q", field)
			}
			seen[field] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("JSON object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("JSON array is not closed")
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	return nil
}

// validText 要求字符串是合法 UTF-8、NFC、无首尾空白且 Unicode rune 数处于闭区间。
func validText(value string, minimum, maximum int, allowEmpty bool) bool {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) || strings.TrimSpace(value) != value {
		return false
	}
	length := utf8.RuneCountInString(value)
	if allowEmpty && length == 0 {
		return true
	}
	if length < minimum || length > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			return false
		}
	}
	return true
}

// validUniqueStrings 校验字符串数组边界并按规范化值精确去重。
func validUniqueStrings(values []string, minimum, maximum int) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validText(value, minimum, maximum, false) {
			return false
		}
		if _, duplicated := seen[value]; duplicated {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

// validDeliverable 校验稳定交付物枚举。
func validDeliverable(value string) bool {
	return value == "video" || value == "image_set" || value == "audio" || value == "mixed"
}

// validLocale 校验当前受支持 locale 精确集合。
func validLocale(value string) bool { return value == "zh-CN" || value == "en-US" }

// validLowerSHA256 校验无前缀小写 SHA-256 十六进制。
func validLowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

// canonicalUUIDv7 只接受 RFC 9562 UUIDv7 的小写规范字符串。
func canonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

// clonePhases 复制模型候选数组，避免后续调用方修改 Validator 已通过的内容。
func clonePhases(values []Phase) []Phase { return append([]Phase(nil), values...) }

// cloneRequiredStrings 保留严格 DTO 的“必填但可为空数组”语义，避免空切片复制后退化为 JSON null。
func cloneRequiredStrings(values []string) []string { return append([]string{}, values...) }

// validJSONSurrogateEscapes 在 encoding/json 把非法 Unicode surrogate 替换为 U+FFFD 前失败关闭。
func validJSONSurrogateEscapes(raw []byte) bool {
	inString := false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || index+1 >= len(raw) {
				continue
			}
			if raw[index+1] != 'u' {
				index++
				continue
			}
			code, ok := parseJSONHexCodeUnit(raw, index+2)
			if !ok {
				return false
			}
			if code >= 0xD800 && code <= 0xDBFF {
				next := index + 6
				if next+6 > len(raw) || raw[next] != '\\' || raw[next+1] != 'u' {
					return false
				}
				low, lowOK := parseJSONHexCodeUnit(raw, next+2)
				if !lowOK || low < 0xDC00 || low > 0xDFFF {
					return false
				}
				index += 11
				continue
			}
			if code >= 0xDC00 && code <= 0xDFFF {
				return false
			}
			index += 5
		}
	}
	return true
}

func parseJSONHexCodeUnit(raw []byte, start int) (uint16, bool) {
	if start < 0 || start+4 > len(raw) {
		return 0, false
	}
	var value uint16
	for _, character := range raw[start : start+4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += uint16(character - '0')
		case character >= 'a' && character <= 'f':
			value += uint16(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += uint16(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}
