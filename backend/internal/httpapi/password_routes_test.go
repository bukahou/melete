package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/bukahou/melete/backend/internal/api"
)

// routesOnly 只为把契约的路由注册出来 —— 一个方法都不实现，测试里也不会被调用。
type routesOnly struct{ api.StrictServerInterface }

// 契约里 /auth/ 下的每一个端点都必须被归类：属于本地密码账号（开关关着时 404），或者不属于。
//
// ⚠️ 锁住 PasswordRoutes 那张清单的老毛病：新端点忘了登记 ⇒ 开关关不住它。
// ⭐ 两个方向都查：
//
//	① 契约里新出现的 /auth/ 端点没归类 → 红（逼加端点的人表态）
//	② 清单里的某一条不是契约端点（拼错了）→ 红 —— 否则那个真端点就漏在开关外面
func TestEveryAuthRouteIsClassified(t *testing.T) {
	// 与本地密码无关的 /auth/ 端点：开关关着时照常可用
	notPassword := map[string]bool{
		"/auth/refresh":                true, // 第三方登录后同样要刷新
		"/auth/logout":                 true,
		"/auth/sso":                    true, // 用第三方的 id_token 换本站 token
		"/auth/sessions":               true, // 登录设备
		"/auth/sessions/revoke-others": true,
		"/auth/email/code":             true, // 改邮箱：账号资料，不是密码
		"/auth/email/confirm":          true,
	}
	password := map[string]bool{}
	for _, p := range PasswordRoutes() {
		password[p] = true
	}

	r := chi.NewRouter()
	api.HandlerFromMux(api.NewStrictHandler(routesOnly{}, nil), r)
	seen := map[string]bool{}
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/auth/") {
			return nil
		}
		seen[route] = true
		switch {
		case password[route] && notPassword[route]:
			t.Errorf("%s %s 同时出现在两张清单里", method, route)
		case !password[route] && !notPassword[route]:
			t.Errorf("%s %s 没有归类：它属于本地密码账号吗？\n"+
				"  属于 → 加进 PasswordRoutes（开关关着时 404）\n"+
				"  不属于 → 加进本测试的 notPassword", method, route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 阳性对照：一条都没走到 ⇒ 装置失效，下面的反查全都会「通过」得毫无意义
	if len(seen) == 0 {
		t.Fatal("一条 /auth/ 路由都没走到 —— 路由表没注册出来")
	}
	for p := range password {
		if !seen[p] {
			t.Errorf("PasswordRoutes 里的 %s 不是契约里的端点（拼错了？）—— 真端点就漏在开关外面了", p)
		}
	}
	for p := range notPassword {
		if !seen[p] {
			t.Errorf("notPassword 里的 %s 不是契约里的端点（契约删了它？那就从清单里去掉）", p)
		}
	}
}
