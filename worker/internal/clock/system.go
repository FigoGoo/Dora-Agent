// Package clock 提供 Business Worker 可注入的 UTC 时间来源。
package clock

import "time"

// System 使用系统时钟返回 UTC 时间；Lease 判断仍必须使用 PostgreSQL 当前时间。
type System struct{}

// Now 返回当前 UTC 时间，适用于非 Lease 的 Attempt 审计与请求时间。
func (System) Now() time.Time {
	return time.Now().UTC()
}
