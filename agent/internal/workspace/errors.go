package workspace

import "errors"

var (
	// ErrNotFound 表示 Session 不存在或与可信 User/Project 绑定不一致，统一防止资源枚举。
	ErrNotFound = errors.New("workspace session not found")
	// ErrPersistence 表示 PostgreSQL Snapshot 或 EventLog 补读暂不可用。
	ErrPersistence = errors.New("workspace persistence unavailable")
	// ErrContentUnavailable 表示任一消息正文无法完成 Key、AEAD、UTF-8 或 Digest 校验。
	ErrContentUnavailable = errors.New("workspace content unavailable")
	// ErrSnapshotTooLarge 表示完整 Snapshot 超过配置上限，禁止静默截断。
	ErrSnapshotTooLarge = errors.New("workspace snapshot exceeds configured bounds")
	// ErrInvalidCursor 表示 Cursor 编码非法、溢出或超前于 Event 高水位。
	ErrInvalidCursor = errors.New("workspace event cursor is invalid")
	// ErrCursorExpired 表示 Cursor 早于在线可重放水位，客户端必须完整回源 Snapshot。
	ErrCursorExpired = errors.New("workspace event cursor expired")
	// ErrEventGap 表示 EventLog 补读结果不连续，禁止跳过权威事件继续推进 Cursor。
	ErrEventGap = errors.New("workspace event sequence gap")
	// ErrProjectionInvalid 表示持久事件类型、版本、聚合绑定或强类型 Payload 不符合冻结契约。
	ErrProjectionInvalid = errors.New("workspace event projection invalid")
)
