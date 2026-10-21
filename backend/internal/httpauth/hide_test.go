package httpauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// HidePaths 与 RequireUserExcept 同一纪律：【完整路径】，⛔ 不是前缀。
//
// ⭐ 两个方向都要有：列出的必须 404（防「从不生效」），没列出的必须放行
// （防「一律 404」或退化成前缀匹配）—— 只测一个方向，错的实现也能全绿。
func TestHidePathsIsExactPathNotPrefix(t *testing.T) {
	mw := HidePaths("/api/v1/auth/password", "/api/v1/auth/password/change")
	var reached bool
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))

	cases := map[string]struct {
		method, path string
		wantHidden   bool
	}{
		"列出的路径 → 404":      {http.MethodPost, "/api/v1/auth/password", true},
		"不论方法 → GET 也 404": {http.MethodGet, "/api/v1/auth/password", true},
		"尾随斜杠 → 同样 404":    {http.MethodPost, "/api/v1/auth/password/", true},
		"列出的另一条 → 404":     {http.MethodPost, "/api/v1/auth/password/change", true},
		// 🔴 下面两条是本测试的意义：它们以列出的路径为前缀（改成前缀匹配，红的正是这两条）
		"同前缀但没列出 → 放行":          {http.MethodPost, "/api/v1/auth/passwordless", false},
		"更深一层但没列出 → 放行":         {http.MethodPost, "/api/v1/auth/password/reset", false},
		"同一族之外的 /auth/ 端点 → 放行": {http.MethodPost, "/api/v1/auth/refresh", false},
		"完全无关的业务端点 → 放行":        {http.MethodGet, "/api/v1/me/progress", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			reached = false
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
			if c.wantHidden {
				if rec.Code != http.StatusNotFound || reached {
					t.Fatalf("%s %s → %d（到达 handler: %v），期望 404 且不到达", c.method, c.path, rec.Code, reached)
				}
				return
			}
			if !reached {
				t.Fatalf("%s %s 被拦下（%d），期望放行\n"+
					"⚠️ 若这里是 404，说明隐藏退化成了前缀匹配 —— 关掉一个功能会顺带藏掉别的端点",
					c.method, c.path, rec.Code)
			}
		})
	}
}
