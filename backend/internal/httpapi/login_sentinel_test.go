package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/bukahou/melete/backend/internal/account"
	"github.com/bukahou/melete/backend/internal/api"
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
// ⚠️ 作用域限定为【格式合法的凭据输入】：畸形请求体 400、限流 429 这类
// 不依赖凭据内容的失败本就该与 401 不同，把它们算进来会误报。
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
	realHash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	inputs := []struct {
		name     string
		acct     *account.Account
		username string
		password string
	}{
		{"用户不存在", nil, "nobody", "pw"},
		{"用户存在 + 密码错", &account.Account{PasswordHash: &realHash}, "u", "wrong"},
		{"用户存在 + 无本地密码（联邦账号）", &account.Account{}, "u", "pw"},
		{"空用户名", nil, "", "pw"},
		{"空密码", &account.Account{PasswordHash: &realHash}, "u", ""},
		{"超长口令（>72 字节）", &account.Account{PasswordHash: &realHash}, "u", strings.Repeat("a", 200)},
		{"用户名含 NUL 与非 UTF-8", nil, "a\x00b\xff", "pw"},
	}

	seen := map[string][]string{}
	for _, in := range inputs {
		svc := account.NewService(&sentinelRepo{acct: in.acct})
		srv := NewServer(nil, nil, svc, nil, nil, nil, log)
		got := outcome(srv.PasswordLogin(context.Background(), api.PasswordLoginRequestObject{
			Body: &api.PasswordLoginJSONRequestBody{Username: in.username, Password: in.password},
		}))
		t.Logf("%-34s → %s", in.name, got)
		seen[got] = append(seen[got], in.name)
	}

	if len(seen) != 1 {
		for out, names := range seen {
			t.Errorf("⛔ 响应集合基数 %d ≠ 1：%q ← %v", len(seen), out, names)
		}
	}
}
