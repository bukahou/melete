package auth

import (
	"context"
	"testing"

	"github.com/bukahou/gokit/localauth"
	"golang.org/x/crypto/bcrypt"
)

// ⭐ 从 internal/account 搬来的资产（阶段 3 换了实现，性质不变）。
//
// 锁住的是 cross-exam 005 实施期由 geass-v3 自查发现的【真实认证绕过】：
// 代换 dummy hash 之后若把比对结果直接当认证结论，拿 dummy 的明文
// 就能登入【任何不存在的用户名】—— 而源码常量的明文是公开的。
//
// ⚠️ 现在这条防线在模块里：LookupFunc 报 found=false 时，模块传【空串】
// 给 Verifier（见其 guard.go ③ 的注释），而空串不是合法 bcrypt hash，
// ⛔ 所以「比对通过」这件事在这条路径上不可能发生。
// ⭐ 缺陷因此【不可表达】，而不是被一道判断挡住 —— 判断会被后人简化掉。
func TestUnknownUserNeverAuthenticates(t *testing.T) {
	if testing.Short() {
		t.Skip("要跑 bcrypt，-short 下跳过")
	}
	g := newProbeGuard(t)
	notFound := localauth.LookupFunc(func(context.Context, string) (string, bool, error) {
		return "", false, nil
	})

	// 拿各种"可能刚好等于 dummy 明文"的东西试 —— 包括空串本身。
	for _, pw := range []string{"", " ", "dummy", "password", "\x00", "any-plaintext-at-all"} {
		out, err := g.Login(context.Background(), "", "ghost-user", pw, notFound)
		if err == nil && out.Allowed {
			t.Fatalf("🔴 用口令 %q 登入了不存在的用户 —— 认证绕过", pw)
		}
	}
}

// ⛔ 空串 / 畸形串绝不能被当成一个能匹配的哈希。
//
// ⚠️ 判别力自检在下半段：先确认一个【真】哈希在同样的调用下是能通过的，
// 否则上半段全绿也可能只是因为登录整个坏掉了。
func TestEmptyHashNeverMatches(t *testing.T) {
	if testing.Short() {
		t.Skip("要跑 bcrypt，-short 下跳过")
	}
	g := newProbeGuard(t)
	for _, h := range []string{"", "not-a-hash", "$2a$10$", "*"} {
		lookup := localauth.LookupFunc(func(context.Context, string) (string, bool, error) {
			return h, true, nil
		})
		out, err := g.Login(context.Background(), "", "u", "whatever", lookup)
		if err == nil && out.Allowed {
			t.Fatalf("🔴 畸形哈希 %q 竟然匹配成功", h)
		}
	}

	// 判别力对照：真哈希 + 正确口令必须【通过】。
	// ⛔ 少了这段，一个「永远拒绝」的实现也会让上面全绿。
	real, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	ok := localauth.LookupFunc(func(context.Context, string) (string, bool, error) {
		return string(real), true, nil
	})
	out, err := g.Login(context.Background(), "", "u", "s3cret", ok)
	if err != nil || !out.Allowed {
		t.Fatalf("对照组：正确口令应当通过 —— 本测试不具判别力（err=%v allowed=%v）", err, out.Allowed)
	}
}
