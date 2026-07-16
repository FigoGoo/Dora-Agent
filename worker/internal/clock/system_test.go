package clock

import (
	"testing"
	"time"
)

// TestSystemNowReturnsUTC 验证生产时钟不会把本机时区扩散到 Worker 审计记录。
func TestSystemNowReturnsUTC(t *testing.T) {
	before := time.Now().UTC()
	got := (System{}).Now()
	after := time.Now().UTC()
	if got.Location() != time.UTC {
		t.Fatalf("System.Now() location = %v, want UTC", got.Location())
	}
	if got.Before(before) || got.After(after) {
		t.Fatalf("System.Now() = %v, want between %v and %v", got, before, after)
	}
}
