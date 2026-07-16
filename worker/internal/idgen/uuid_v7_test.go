package idgen

import (
	"testing"

	"github.com/google/uuid"
)

// TestUUIDv7New 验证生成器只返回合法且不重复的 UUIDv7。
func TestUUIDv7New(t *testing.T) {
	generator := UUIDv7{}
	seen := make(map[string]struct{}, 100)
	for range 100 {
		value, err := generator.New()
		if err != nil {
			t.Fatalf("生成 UUIDv7 失败: %v", err)
		}
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.Version() != 7 || parsed.Variant() != uuid.RFC4122 {
			t.Fatalf("生成结果不是 RFC UUIDv7: %q, err=%v", value, err)
		}
		if _, exists := seen[value]; exists {
			t.Fatalf("生成了重复 UUIDv7: %q", value)
		}
		seen[value] = struct{}{}
	}
}
