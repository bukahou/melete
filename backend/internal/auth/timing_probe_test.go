package auth

import (
	"context"
	"testing"
	"time"

	"github.com/bukahou/gokit/localauth"
	"golang.org/x/crypto/bcrypt"
)

// ⭐ 这个文件是从 internal/account 搬过来的资产（阶段 3 删掉了那边的实现）。
//
// ⚠️ 它测的【性质】不随实现变：三条失败路径必须等时。
// 实现从 melete 自己的 VerifyPassword 换成了模块的 Guard，
// 但「哪三条路径」与「为什么必须等时」一个字没变 ——
// ⛔ 所以这个测试不该跟着实现一起删。
//
// 锁住的是 2026-09-04 修掉的那个枚举预言机：
//
//	① 用户不存在              → 烧 dummy   36ms
//	② 用户存在有密码          → 烧真 hash  36ms
//	③ 用户存在但【无密码】（纯 OIDC 账号）→ 曾因 `||` 短路，bcrypt 根本没跑 → 0ms
//
// ③ 就是那条漏掉的路径：「近乎瞬时的失败」= 这个用户名存在且是 SSO 账号。

func newProbeGuard(t *testing.T) *localauth.Guard {
	t.Helper()
	g, err := localauth.New(
		localauth.TrustDirect(),
		localauth.AdmissionFunc(func(context.Context, localauth.AdmitRequest) error { return nil }),
		localauth.NewMemStore(), localauth.NewMemStore(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func median(d []time.Duration) time.Duration {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
	return d[len(d)/2]
}

func TestLoginPathsAreTimeEqual(t *testing.T) {
	if testing.Short() {
		t.Skip("要跑 bcrypt，-short 下跳过")
	}
	real, err := bcrypt.GenerateFromPassword([]byte("correct-horse"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}

	// ⚠️ clientIP 传空串 = 关掉 IP 维度退避。
	//
	// ⭐ 这不是为了让测试好过，而是因为 IP 维度是模块【唯一会提前返回、
	// 从而省下 bcrypt】的地方（账号维度刻意不省，否则「被锁的账号返回得快」
	// 又是一个预言机）。带 IP 跑的话，几次之后就在测退避而不是在测等时性。
	const ip = ""
	paths := map[string]localauth.LookupFunc{
		"①用户不存在":    func(context.Context, string) (string, bool, error) { return "", false, nil },
		"②有密码但错":    func(context.Context, string) (string, bool, error) { return string(real), true, nil },
		"③纯OIDC无密码": func(context.Context, string) (string, bool, error) { return "", true, nil },
	}

	got := map[string]time.Duration{}
	for name, lookup := range paths {
		g := newProbeGuard(t)
		var samples []time.Duration
		for i := range 7 {
			start := time.Now()
			out, err := g.Login(context.Background(), ip, "probe", "wrong-password", lookup)
			samples = append(samples, time.Since(start))
			if err == nil && out.Allowed {
				t.Fatalf("%s 第 %d 次竟然登录成功了", name, i)
			}
		}
		got[name] = median(samples)
		t.Logf("%-12s P50 = %v", name, got[name])
	}

	// ⭐ 绝对下限，⛔ 不用「与其它路径比」——
	// 那样一个「三条都是 0ms」的实现会全部通过（本项目 P3 吃过这种账：
	// 基线取自被测对象，测试恒绿）。
	const floor = 5 * time.Millisecond
	for name, d := range got {
		if d < floor {
			t.Fatalf("🔴 %s 的 P50 只有 %v（< %v）—— 这条路径没有烧 bcrypt，"+
				"失败耗时可区分 = 用户名枚举预言机", name, d, floor)
		}
	}
	// 三条之间也不该差太多。⚠️ 阈值放宽到 15ms：bcrypt 本身有抖动，
	// 而我们要挡的是【数量级】差异（20ns vs 36ms），不是微秒级噪声。
	var lo, hi time.Duration
	for _, d := range got {
		if lo == 0 || d < lo {
			lo = d
		}
		if d > hi {
			hi = d
		}
	}
	if hi-lo > 15*time.Millisecond {
		t.Fatalf("🔴 三条路径耗时差 %v（%v ~ %v）—— 可区分即可枚举", hi-lo, lo, hi)
	}
}
