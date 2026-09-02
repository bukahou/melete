// Package httpauth 是 melete-api 的入站认证。
//
// 只有一层：Authorization: Bearer <access token>，由 internal/token 验签，
// accountID 取自其 sub。
//
// **曾经还有一层服务间共享密钥，已随「API 对公网暴露」一并移除**
// （见 docs/design/active/deployment.md §0）：原生 App 反编译即得共享密钥，
// 而公网攻击者本就不在集群内 —— 那一层挡不住任何人，只会阻碍 iOS 端。
// 现在身份完全由签名保证，与调用方是谁无关。
package httpauth

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey int

const accountKey ctxKey = iota

// TokenParser 是本包对 token 域的全部依赖 —— 接口定义在使用方，便于测试替换。
type TokenParser interface {
	ParseAccessToken(raw string) (int64, error)
}

// AccountID 从请求上下文取出已认证的账号 id。
// 只有经过 RequireUserExcept 的请求才有值。
func AccountID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(accountKey).(int64)
	return id, ok && id > 0
}

// RequireUserExcept 验签 access token 并把 accountID 注入上下文，
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
			id, err := parser.ParseAccessToken(raw)
			if err != nil {
				// 401 是给客户端的信号：iOS 的 interceptor 见到它会自动去 refresh
				writeJSONError(w, http.StatusUnauthorized, "访问令牌无效或已过期")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accountKey, id)))
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
