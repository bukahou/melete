package httpauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bukahou/melete/backend/internal/auth"
)

type okParser struct{}

func (okParser) ParseAccessToken(string) (auth.Claims, error) {
	return auth.Claims{UserID: "u", SessionID: "s"}, nil
}

// ⛔⛔ 免认证豁免必须是【完整路径】，⛔ 不是前缀。
//
// ⚠️ 锁住 2026-09-07 的一个我自己造出来的缺陷：新加的 /auth/sessions
// （会话列表，必须认证）落进了 `/auth/` 这个豁免前缀，端点等于没挂认证。
//
// ⭐ 这条测试要防的不是「那一个端点」，是那个【形状】：
// 前缀豁免让「以后每个加在 /auth/ 下的端点都默认公开」，
// 而加端点的人不会去读中间件。⇒ 默认值必须通向【要求认证】那一侧。
func TestExemptionIsExactPathNotPrefix(t *testing.T) {
	const public = "/api/v1/auth/password"
	mw := RequireUserExcept(okParser{}, public)

	var reached bool
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		if _, ok := AccountID(r.Context()); !ok {
			w.Header().Set("X-Authenticated", "no")
			return
		}
		w.Header().Set("X-Authenticated", "yes")
	}))

	cases := map[string]struct {
		path       string
		wantStatus int
		wantAuthed string // 到达 handler 时是否带身份
	}{
		"登记过的公开端点 → 放行且【不带】身份": {public, http.StatusOK, "no"},
		// 🔴 这一条是本测试的全部意义：它与 public 共享前缀 /api/v1/auth/
		"同前缀但未登记 → 必须要求认证":     {"/api/v1/auth/sessions", http.StatusUnauthorized, ""},
		"更深一层未登记 → 同样要求认证":     {"/api/v1/auth/sessions/revoke-others", http.StatusUnauthorized, ""},
		"完全无关的业务端点 → 要求认证":     {"/api/v1/me/progress", http.StatusUnauthorized, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			reached = false
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
			if rec.Code != c.wantStatus {
				t.Fatalf("%s → %d，期望 %d\n"+
					"⚠️ 若这里变成 200，说明豁免又变回前缀匹配了 —— "+
					"那意味着以后每个加在 /auth/ 下的端点都默认公开", c.path, rec.Code, c.wantStatus)
			}
			if c.wantAuthed != "" {
				if !reached {
					t.Fatal("公开端点没有到达 handler")
				}
				if got := rec.Header().Get("X-Authenticated"); got != c.wantAuthed {
					t.Fatalf("到达 handler 时身份状态 = %q，期望 %q", got, c.wantAuthed)
				}
			}
		})
	}
}

// 带 token 时，未登记的路径应当通过并带上身份 —— 判别力对照。
// ⛔ 少了它，一个「永远 401」的实现也会让上面全绿。
func TestAuthenticatedRequestPassesThrough(t *testing.T) {
	mw := RequireUserExcept(okParser{}, "/api/v1/auth/password")
	var gotID string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID, _ = AccountID(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	req.Header.Set("Authorization", "Bearer whatever")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotID != "u" {
		t.Fatalf("对照组：带 token 应放行并注入身份，得到 code=%d id=%q —— 本测试不具判别力", rec.Code, gotID)
	}
}
