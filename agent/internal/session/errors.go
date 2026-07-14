package session

import "errors"

var (
	// ErrInvalidCommand 表示命令字段、UUIDv7、摘要或 Prompt 边界不符合冻结契约。
	ErrInvalidCommand = errors.New("invalid ensure project session command")
	// ErrCommandConflict 表示同一 CommandID 已绑定不同语义摘要，禁止覆盖既有回执。
	ErrCommandConflict = errors.New("session command idempotency conflict")
	// ErrProjectSessionConflict 表示同一 Project 已由另一稳定命令建立默认 Session。
	ErrProjectSessionConflict = errors.New("project already has a different default session command")
	// ErrPersistence 表示 Session 基础事实未能原子提交或读取。
	ErrPersistence = errors.New("persist session foundation")
	// ErrInvalidProtectedContentEnvelope 表示保护结果不符合 W0 自描述 AEAD Envelope 二进制格式。
	ErrInvalidProtectedContentEnvelope = errors.New("invalid protected content envelope")
	// ErrContentProtection 表示正文保护失败；该稳定错误不得携带 KMS、算法、地址或 Secret 详情。
	ErrContentProtection = errors.New("protect sensitive session content")
	// ErrContentUnavailable 表示密钥、Envelope、AEAD、UTF-8 或摘要校验失败，错误不得区分具体原因。
	ErrContentUnavailable = errors.New("session content unavailable")
)
