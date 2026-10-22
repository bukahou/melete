package httpapi

// PasswordRoutes 是「本地密码账号」这个功能的全部端点（相对 API 根路径）——
// MELETE_API_PASSWORD_LOGIN 没打开时它们一律 404（P9 #39，见 cmd/melete-api）。
//
// ⭐ 开关关掉的是一个【功能】，功能 = 这几个端点。端点代码照旧保留（#37）。
// ⚠️ 清单的老毛病是「新端点忘了登记 ⇒ 开关关不住它」—— 默认值通向不安全的一侧。
//
//	password_routes_test.go 因此要求契约里 /auth/ 下的【每一个】端点都被归类：
//	要么在这里，要么在测试里的「与密码无关」清单。新端点不归类，测试就红。
func PasswordRoutes() []string {
	return []string{
		"/auth/password",          // 密码登录
		"/auth/password/change",   // 改密码 / 首次设密
		"/auth/register/code",     // 注册：发验证码
		"/auth/register",          // 注册
		"/auth/recovery/code",     // 找回密码：发验证码
		"/auth/recovery/complete", // 找回密码：设新密码
	}
}
