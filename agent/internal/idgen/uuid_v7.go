// Package idgen 提供 Agent Service 应用侧稳定标识生成器。
package idgen

import (
	"fmt"

	"github.com/google/uuid"
)

// UUIDv7 生成按时间有序且符合 RFC 9562 的 UUIDv7 字符串。
type UUIDv7 struct{}

// New 生成一个新的 UUIDv7；随机源失败时返回错误且不产生降级标识。
func (UUIDv7) New() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate Agent UUIDv7: %w", err)
	}
	return id.String(), nil
}
