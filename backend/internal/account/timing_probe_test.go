package account

import (
	"context"
	"testing"
	"time"
)

// 三条路径必须等时 —— 回归测试，锁住 2026-09-04 修掉的那个枚举预言机。
// 见 service.go 里 VerifyPassword 的注释：③（SSO 无密码账号）曾因 `||` 短路 0ms 返回。
type probeRepo struct{ acct *Account }

func (p *probeRepo) FindByUsername(_ context.Context, _ string) (*Account, error) {
	if p.acct == nil {
		return nil, ErrNotFound
	}
	return p.acct, nil
}
func (p *probeRepo) FindByID(context.Context, string) (*Account, error) { return nil, ErrNotFound }
func (p *probeRepo) EstablishFederated(context.Context, string, string, string) (*Account, error) {
	return nil, ErrNotFound
}

func median(d []time.Duration) time.Duration {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
	return d[len(d)/2]
}

func TestVerifyPasswordTiming(t *testing.T) {
	realHash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	cases := []struct {
		name string
		repo Repository
	}{
		{"① 用户不存在（烧 dummy）", &probeRepo{acct: nil}},
		{"② 用户存在 + 有密码", &probeRepo{acct: &Account{PasswordHash: &realHash}}},
		{"③ 用户存在 + SSO 无密码", &probeRepo{acct: &Account{PasswordHash: nil}}},
	}
	var got []time.Duration
	for _, c := range cases {
		svc := NewService(c.repo)
		var samples []time.Duration
		for i := 0; i < 15; i++ {
			start := time.Now()
			_, _ = svc.VerifyPassword(context.Background(), "probe", "wrong-password")
			samples = append(samples, time.Since(start))
		}
		med := median(samples)
		t.Logf("%-26s 中位数 = %v", c.name, med.Round(time.Microsecond))
		got = append(got, med)
	}

	// ⚠️ 2026-09-04：断言用【绝对下界】，不用「与路径①比」。
	//
	// 此前这里写的是 `med < base/2`，base 取自路径① —— 而路径①本身就是被检查项之一。
	// 于是把 dummyHash 换成 ""（bcrypt 立即返回 ErrHashTooShort，20ns）时，
	// base 也变成 0，三条路径全部"通过"—— 测试对整整一类缺陷（dummy 退化）是瞎的。
	// ⛔ 基准不得取自被检查的对象集合。绝对常量不由被检查项导出，任何一项失效都推不动它。
	//
	// 5ms 的来历：cost=10 实测 36ms、cost=12 实测 145ms；机器越慢这个值只会更大，
	// 不会更小。而缺陷形态是 20ns ~ 0s —— 中间隔着四个数量级，不存在需要调参的灰区。
	const bcryptFloor = 5 * time.Millisecond
	for i, med := range got {
		if med < bcryptFloor {
			t.Errorf("路径 %s 耗时 %v < %v —— bcrypt 没有真的跑（要么被分支跳过，要么 dummy 非法）",
				cases[i].name, med, bcryptFloor)
		}
	}
	// 其次才是三条之间彼此一致 —— 上面那条保证了比较的基准本身是有效的。
	for i, med := range got {
		if med < got[0]/2 || med > got[0]*2 {
			t.Errorf("路径 %s 耗时 %v 与基准 %v 相差超过一倍", cases[i].name, med, got[0])
		}
	}
}
