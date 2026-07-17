package localseed

import (
	"net"
	"net/url"
)

// IsSafeLocalBusinessDSN 只接受本地 Smoke 数据库、专用角色和唯一明文 sslmode 参数。
// canonical Trial 使用隔离的 dora_business_test；日常本地联调保留 dora_business。
func IsSafeLocalBusinessDSN(dsn string) bool {
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Opaque != "" ||
		parsed.User == nil || parsed.User.Username() != "dora_business_app" ||
		(parsed.Path != "/dora_business" && parsed.Path != "/dora_business_test") ||
		parsed.Fragment != "" {
		return false
	}
	password, passwordPresent := parsed.User.Password()
	query := parsed.Query()
	sslModes, sslModePresent := query["sslmode"]
	// pgx 允许 query 覆盖 authority/path；因此只允许唯一 sslmode，拒绝 host/user/dbname 等覆盖参数。
	if !passwordPresent || password == "" || len(query) != 1 || !sslModePresent || len(sslModes) != 1 || sslModes[0] != "disable" {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
