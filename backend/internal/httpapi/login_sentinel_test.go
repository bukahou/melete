package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/bukahou/gokit/localauth"

	"github.com/bukahou/melete/backend/internal/account"
	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/auth"
)

// 内容轴哨兵（cross-exam 005 三十轮，geass-v3 提）：
//
//	同一个认证端点的【所有失败】必须返回同一个 (HTTP 状态码, 错误码) 对。
//	检法：拿各种输入打同一端点，断言响应集合的【基数为 1】。
//
// ⭐ 它是哨兵而非针对性测试，判据是「能不能在不知道缺陷是什么的情况下写出来」：
// 写它只需要知道「这是个认证端点」—— ⛔ 不需要知道有几条失败路径，
// 更不需要知道系统里存在「联邦账号」这回事。geass-v3 那个 500 缺陷
// （联邦账号落进兜底分支）本可被它直接抓住：集合基数 = 2。
//
// ⚠️ 2026-09-07（阶段 3）：实现换成了模块的 Guard，⛔ 哨兵本身一个字没改判据。
// ⭐ 这正是「哨兵不编码它猜会出什么错」的价值 —— 换了整套实现它照样适用。
//
// ⚠️ 作用域限定为【格式合法的凭据输入】：畸形请求体 400 这类不依赖凭据
// 内容的失败本就该与 401 不同，把它们算进来会误报。
type sentinelRepo struct{ acct *account.Account }

func (r *sentinelRepo) FindByUsername(context.Context, string) (*account.Account, error) {
	if r.acct == nil {
		return nil, account.ErrNotFound
	}
	return r.acct, nil
}
func (r *sentinelRepo) FindByID(context.Context, string) (*account.Account, error) {
	return nil, account.ErrNotFound
}
func (r *sentinelRepo) EstablishFederated(context.Context, string, string, string) (*account.Account, error) {
	return nil, account.ErrNotFound
}

// newSentinelServer 组一个只用内存存储的真实认证栈。
//
// ⭐ 用【真的】 localauth.Guard 而不是 mock：哨兵要看的是模块与本层拼起来
// 之后对外的样子，⛔ 换成 mock 就只剩本层，而缺陷恰恰爱藏在接缝处。
func newSentinelServer(t *testing.T, acct *account.Account, hash string) *Server {
	t.Helper()
	guard, err := localauth.New(
		localauth.TrustDirect(),
		localauth.AdmissionFunc(func(context.Context, localauth.AdmitRequest) error { return nil }),
		localauth.NewMemStore(), localauth.NewMemStore(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := localauth.NewSessionGuard(
		localauth.NewSessionMemStore(),
		func(context.Context, string) (localauth.AccountStatus, error) {
			return localauth.AccountStatus{Active: true}, nil
		},
		auth.RefreshTTL,
	)
	if err != nil {
		t.Fatal(err)
	}
	lookup := localauth.LookupFunc(func(context.Context, string) (string, bool, error) {
		if acct == nil {
			return "", false, nil
		}
		return hash, true, nil
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := auth.NewService(guard, sessions, &sentinelRepo{acct: acct}, lookup, "test-secret", log)
	return NewServer(nil, nil, nil, svc, nil, log)
}

// outcome 把一次调用压成「对外可观察的那个二元组」。
func outcome(resp api.PasswordLoginResponseObject, err error) string {
	if err != nil {
		return "500 internal" // 生成层把非 nil error 兜成 500
	}
	switch resp.(type) {
	case api.PasswordLogin401JSONResponse:
		return "401 bad_credentials"
	case api.PasswordLogin200JSONResponse:
		return "200 ok"
	default:
		return fmt.Sprintf("未知响应类型 %T", resp)
	}
}

func TestLoginFailureResponseCardinalityIsOne(t *testing.T) {
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
	for _, in := range inputs {
		s := newSentinelServer(t, in.acct, in.hash)
		got := outcome(s.PasswordLogin(context.Background(), api.PasswordLoginRequestObject{
			Body: &api.PasswordLoginJSONRequestBody{Username: in.username, Password: "wrong-pw"},
		}))
		seen[got] = append(seen[got], in.name)
	}

	if len(seen) != 1 {
		t.Fatalf("🔴 认证端点的失败响应集合基数 = %d，应为 1：%v\n"+
			"⚠️ 可区分的失败 = 枚举预言机，攻击者据此能判断用户名是否存在", len(seen), seen)
	}
	for k, v := range seen {
		t.Logf("全部 %d 组输入 → %q（%v）", len(v), k, v)
	}
}
