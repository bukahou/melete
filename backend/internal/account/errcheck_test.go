package account

import (
	"context"
	"errors"
	"testing"
)

// 内容轴：任何认证失败都必须归一为 ErrBadCredentials，不得有错误类型逃到别的分支。
//
// geass-v3 报的形态（2026-09-04 cross-exam 005）：它的 Verify 只把
// ErrMismatchedHashAndPassword 译成"密码错"，而空 hash 得到的是 ErrHashTooShort，
// 落进 API 兜底 → 500。⛔ 那是状态码泄漏 —— 一次请求、无歧义、免疫网络抖动，
// 比时序泄漏好利用一个量级。
//
// melete 这里由结构保证而非由 dummy 的合法性保证：错误在调用点就被坍缩成 bool
// (`... == nil`)，错误值根本不进入后续分支，所以"不同错误类型走不同分支"这个缺陷
// 写不出来。实测过：即便 dummyHash 换成 ""，返回的仍是 ErrBadCredentials
// （而时序轴会抓住它 —— 两条轴各有各的守卫）。
func TestAuthFailuresCollapseToOneError(t *testing.T) {
	svc := NewService(&probeRepo{acct: nil})
	_, err := svc.VerifyPassword(context.Background(), "nobody", "pw")
	t.Logf("dummy=%q → 返回 err = %v （是 ErrBadCredentials? %v）",
		dummyHash, err, errors.Is(err, ErrBadCredentials))
	if !errors.Is(err, ErrBadCredentials) {
		t.Errorf("⛔ 内容轴泄漏：非法 dummy 让错误逃到了别的分支")
	}
}
