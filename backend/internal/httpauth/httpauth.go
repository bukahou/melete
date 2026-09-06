// Package httpauth 是 melete-api 的入站认证。
//
// 只有一层：Authorization: Bearer <access token>，由 internal/auth 验签。
//
// **曾经还有一层服务间共享密钥，已随「API 对公网暴露」一并移除**
// （见 docs/design/active/deployment.md §0）：原生 App 反编译即得共享密钥，
// 而公网攻击者本就不在集群内 —— 那一层挡不住任何人，只会阻碍 iOS 端。
package httpauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/bukahou/melete/backend/internal/auth"
)

type ctxKey int

const (
	accountKey ctxKey = iota
	sessionKey
)

// TokenParser 是本包对认证域的全部依赖 —— 接口定义在使用方，便于测试替换。
type TokenParser interface {
	ParseAccessToken(raw string) (auth.Claims, error)
}

// AccountID 从请求上下文取出已认证的账号 id（canonical UUID 文本）。
//
// ⚠️ 2026-09-07 起是 string 而不是 int64（案卷 §22.2 的 UUIDv7）。
// 空串一律视为未认证 —— ⛔ 与旧的 `id > 0` 同一条纪律：
// 零值不得被当成一个合法身份。
func AccountID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(accountKey).(string)
	return id, ok && id != ""
}

// SessionID 取出当前这张票所属的会话 id。
//
// ⭐ 「登出其它设备」要靠它决定保留哪一条 —— 而那个信息只能来自
// 【当前这张票】。⛔ 绝不能由客户端在请求体里指定，否则任何人都能构造
// 一个「保留别人的会话、踢掉我的」的请求。
//
// ⚠️ 可能为空（阶段 3 之前签发的票没有 sid）。调用方必须处理空值，
// ⛔ 且不得在空值时退化成「登出全部」——那会把一次误操作放大成全员掉线。
func SessionID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(sessionKey).(string)
	return id, ok && id != ""
}

// RequireUserExcept 验签 access token 并把身份注入上下文，
// 放行 exempt 中列出的路径前缀（认证端点在拿到 token 之前本就无 token 可验）。
//
// 为什么是「一个中间件 + 路径豁免」而不是「两组路由各挂各的」：
// 生成的路由表只能向同一个 chi 路由器注册一次，注册两遍会 panic。
func RequireUserExcept(parser TokenParser, exempt ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range exempt {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			raw, ok := bearerToken(r)
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "缺少访问令牌")
				return
			}
			claims, err := parser.ParseAccessToken(raw)
			if err != nil {
				// 401 是给客户端的信号：拦截器见到它会自动去 refresh
				writeJSONError(w, http.StatusUnauthorized, "访问令牌无效或已过期")
				return
			}
			ctx := context.WithValue(r.Context(), accountKey, claims.UserID)
			ctx = context.WithValue(ctx, sessionKey, claims.SessionID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	return h[len(prefix):], true
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"message":"` + msg + `"}`))
}

// ── 可信客户端 IP ────────────────────────────────────────────────

const clientIPKey ctxKey = 2

// ClientIP 取出中间件解析好的可信客户端 IP。
func ClientIP(ctx context.Context) (string, bool) {
	ip, ok := ctx.Value(clientIPKey).(string)
	return ip, ok && ip != ""
}

// ResolveClientIP 用模块的策略解析一次客户端 IP 并放进上下文。
//
// ⭐ 为什么解析在中间件而不是在守卫里：解析需要 HTTP 头，而模块的调用方
// 常常隔着一次 RPC。模块因此只收结果不做解析（见其 Guard.Login ① 的注释）。
//
// ⚠️ 解析失败【不阻断请求】——它意味着请求没走预期链路（例如没有 CF 头）。
// 模块收到空串会降级成「只按账号维度退避」，⛔ 而不是拒绝服务。
// ⚠️ 但失败必须留痕：⛔ 不得逐个请求告警（不需要任何凭据就能触发，
// 逐个告警本身是 DoS），所以这里只写 debug 级。
func ResolveClientIP(strategy interface {
	ClientIP(r *http.Request) (string, error)
}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, err := strategy.ClientIP(r)
			if err != nil {
				ip = ""
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, ip)))
		})
	}
}
