// Package clock 提供 Business Service 可注入的 UTC 时间来源。
package clock

import "time"

// System 使用系统时钟返回 UTC 时间，适用于生产 Runtime 的时间读取。
type System struct{}

// Now 返回当前 UTC 时间；调用方仍应在业务事务中冻结一次时间并复用。
func (System) Now() time.Time {
	return time.Now().UTC()
}
