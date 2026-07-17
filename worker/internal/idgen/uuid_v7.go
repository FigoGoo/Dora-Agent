// Package idgen 提供 Business Worker 应用侧稳定标识生成器。
package idgen

import (
	"fmt"

	"github.com/google/uuid"
)

// UUIDv7 生成按时间有序且符合 RFC 9562 的 UUIDv7 字符串。
type UUIDv7 struct{}

// New 生成一个新的 UUIDv7；随机源失败时返回错误且不产生降级标识。
func (UUIDv7) New() (string, error) {
	id, err := UUIDv7{}.NewUUID()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// NewUUID 生成可直接用于类型化 DTO 和 GORM Model 的 UUIDv7 值。
func (UUIDv7) NewUUID() (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generate Worker UUIDv7: %w", err)
	}
	return id, nil
}
