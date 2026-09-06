package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bukahou/gokit/localauth"
	"github.com/go-chi/chi/v5"

	"github.com/bukahou/melete/backend/internal/account"
	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/auth"
	"github.com/bukahou/melete/backend/internal/httpauth"
)

// 端到端版哨兵 —— 与 login_sentinel_test.go 是【两个不同层面】，不是重复。
//
// ⚠️ cross-exam 005 三十一轮 geass-v3 指出：handler 层那个版本不经过中间件，
// 于是它对【中间件引入的凭据相关差异】是瞎的。
//
// ⚠️ 2026-09-07（阶段 3）：它当初盯的限流中间件已按裁决 ⑥ 整个删除。
// ⛔ 但这个哨兵【不该跟着删】—— 它的性质不是「限流键对不对」，而是
// 「经过完整 HTTP 栈之后，凭据相关的失败是否仍然不可区分」。
// 现在栈里还有 ResolveClientIP 与 RequireUserExcept 两层中间件，
// ⭐ 而将来加回限流时，这个哨兵是现成的第一道检查。
//
// ⚠️ 作用域仍限定为【格式合法的凭据输入】：畸形请求体 400 这类
// 不依赖凭据内容的失败本就该不同，算进来会误报。
func TestLoginSentinelThroughMiddleware(t *testing.T) {
	if testing.Short() {
		t.Skip("要跑 bcrypt，-short 下跳过")
	}
	const realHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

	inputs := []struct {
		name     string
		acct     *account.Account
		hash     string
		username string
	}{
		{"用户不存在", nil, "", "nobody"},
		{"用户存在 + 密码错", &account.Account{}, realHash, "u1"},
		{"联邦账号无本地密码", &account.Account{}, "", "u2"},
		{"空用户名", nil, "", ""},
		{"超长用户名", &account.Account{}, realHash, strings.Repeat("x", 300)},
	}

	seen := map[string][]string{}
	for i, in := range inputs {
		s := newSentinelServer(t, in.acct, in.hash)

		r := chi.NewRouter()
		// ⭐ 走【真实的】中间件链，⛔ 不是直接调 handler。
		r.Use(httpauth.ResolveClientIP(localauth.TrustDirect()))
		r.Use(httpauth.RequireUserExcept(stubParser{}, "/auth/"))
		r.Post("/auth/login", func(w http.ResponseWriter, req *http.Request) {
			resp, err := s.PasswordLogin(req.Context(), api.PasswordLoginRequestObject{
				Body: &api.PasswordLoginJSONRequestBody{Username: in.username, Password: "wrong-pw"},
			})
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if _, ok := resp.(api.PasswordLogin401JSONResponse); ok {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "用户名或密码错误"})
				return
			}
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		// ⭐ 每组一个互不相同的来源 —— 当年是为了避开限流键坍缩。
		// ⚠️ 限流已删，但这条保留：将来加回限流时，靠"组数 < 阈值"侥幸
		// 通过的版本会在阈值调小的那天 FAIL，而这个版本不会。
		req.RemoteAddr = fmt.Sprintf("203.0.113.%d:12345", i+1)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		body := strings.TrimSpace(rec.Body.String())
		seen[fmt.Sprintf("%d %s", rec.Code, body)] = append(seen[fmt.Sprintf("%d %s", rec.Code, body)], in.name)
	}

	if len(seen) != 1 {
		t.Fatalf("🔴 经过中间件后，认证失败的响应集合基数 = %d，应为 1：%v", len(seen), seen)
	}
	for k, v := range seen {
		t.Logf("全部 %d 组 → %q", len(v), k)
	}
}

// stubParser 让认证中间件可构造。⚠️ /auth/ 是豁免路径，本测试里它不会被调用；
// ⛔ 但仍要真的挂上去 —— 少挂一层中间件，这个哨兵就退化成 handler 层版本。
type stubParser struct{}

func (stubParser) ParseAccessToken(string) (auth.Claims, error) { return auth.Claims{}, nil }
