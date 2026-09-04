package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bukahou/melete/backend/internal/account"
	"github.com/bukahou/melete/backend/internal/httpauth"
)

// 端到端版哨兵 —— 与 login_sentinel_test.go 是【两个不同层面】，不是重复。
//
// ⚠️ cross-exam 005 三十一轮 geass-v3 指出：handler 层的那个版本不经过中间件，
// 于是它对【中间件引入的凭据相关差异】是瞎的。最典型的就是限流键：
// melete 的键是 clientIP（已实测），但若哪天有人把用户名并进键里，
// 那就是一条真的凭据相关泄漏 —— 而 handler 层哨兵永远看不见它。
//
// 反过来，端到端版本自身有个陷阱（同样是它指出的）：
// 若各组输入落在【同一个限流键】上，第 N+1 组会拿到 429，
// ⛔ 哨兵报警而系统是对的 —— 一个在防护生效时误报的哨兵会教人把哨兵关掉。
// ⚠️ 而"测试时关掉限流"不是解法：那是让验证工具关掉被验证系统的一部分。
//
// 正解是让每组输入落在互不相同的限流键上（这里 = 不同的 RemoteAddr），
// ⛔ 而不是靠"输入组数少于阈值"侥幸通过 —— 后者让哨兵的正确性
// 依赖一个它不控制的参数。melete 阈值是 10，若哪天调成 5，靠侥幸的版本会 FAIL。
func TestLoginSentinelThroughMiddleware(t *testing.T) {
	realHash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

	inputs := []struct {
		name     string
		acct     *account.Account
		username string
	}{
		{"用户不存在", nil, "nobody"},
		{"用户存在 + 密码错", &account.Account{PasswordHash: &realHash}, "u1"},
		{"联邦账号无本地密码", &account.Account{}, "u2"},
		{"空用户名", nil, ""},
		{"超长口令用户名", &account.Account{PasswordHash: &realHash}, strings.Repeat("x", 300)},
	}

	seen := map[int][]string{}
	for i, in := range inputs {
		svc := account.NewService(&sentinelRepo{acct: in.acct})
		r := chi.NewRouter()
		r.Use(httpauth.NewRateLimit(10, time.Minute).Middleware("/auth/"))
		r.Post("/auth/login", func(w http.ResponseWriter, req *http.Request) {
			_, err := svc.VerifyPassword(req.Context(), in.username, "pw")
			if err == account.ErrBadCredentials {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"message": "用户名或密码错误"})
				return
			}
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		// ⭐ 每组一个互不相同的限流键，而不是靠"组数 < 阈值"侥幸
		req.RemoteAddr = fmt.Sprintf("203.0.113.%d:12345", i+1)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		t.Logf("%-24s → %d", in.name, rec.Code)
		seen[rec.Code] = append(seen[rec.Code], in.name)
	}

	if len(seen) != 1 {
		for code, names := range seen {
			t.Errorf("⛔ 响应集合基数 %d ≠ 1：%d ← %v", len(seen), code, names)
		}
	}
}
