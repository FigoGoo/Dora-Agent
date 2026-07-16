package auth

import "context"

// Principal 可信用户身份，只能由成功校验权威 Web Session 的 Resolver 构造。
type Principal struct {
	// ID 用户唯一标识。
	ID string
	// DisplayName 用户安全展示名。
	DisplayName string
	// Email 供前端展示的脱敏邮箱。
	Email string
	// AccountStatus 用户账户状态，W0 成功会话固定为 active。
	AccountStatus string
	// Roles 是本次权威 Session Resolve 动态投影的闭集角色，普通用户为空数组。
	Roles []string
	// Capabilities 是本次权威 Session Resolve 动态投影的闭集能力，普通用户为空数组。
	Capabilities []string
}

// principalContextKey 是私有 Context Key 类型，防止外部包通过字符串伪造 Principal。
type principalContextKey struct{}

// resolvedSessionContextKey 是私有 Context Key 类型，防止业务 Handler 从 Header 伪造 Web Session 事实。
type resolvedSessionContextKey struct{}

// ContextWithPrincipal 仅供认证中间件在校验成功后写入可信 Principal；返回的 Context 继承原请求取消语义。
func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext 读取认证中间件放入私有 Key 的 Principal；未认证时返回 false。
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

// ContextWithResolvedSession 仅供认证中间件写入完整内部会话事实；这些字段不得映射到浏览器 DTO。
func ContextWithResolvedSession(ctx context.Context, session ResolvedSession) context.Context {
	return context.WithValue(ctx, resolvedSessionContextKey{}, session)
}

// ResolvedSessionFromContext 读取认证中间件已经权威校验的内部会话事实；未认证时返回 false。
func ResolvedSessionFromContext(ctx context.Context) (ResolvedSession, bool) {
	session, ok := ctx.Value(resolvedSessionContextKey{}).(ResolvedSession)
	return session, ok
}
