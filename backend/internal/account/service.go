package account

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// ErrBadCredentials 统一「用户不存在」与「密码错误」——
// 登录失败不区分原因，避免用户名枚举。
var ErrBadCredentials = errors.New("用户名或密码错误")

// Service 是账号领域对外的门面。
type Service interface {
	VerifyPassword(ctx context.Context, username, password string) (*Account, error)
	EstablishSSO(ctx context.Context, sub, display string) (*Account, error)
	FindByID(ctx context.Context, id int64) (*Account, error)
}

type service struct{ repo Repository }

func NewService(repo Repository) Service { return &service{repo: repo} }

// dummyHash 用户不存在 / 无密码时拿来烧的那个 hash。
//
// ⚠️ 它的 cost 必须与 account.password_hash 里所有真 hash 的 cost 一致 ——
// bcrypt 的 cost 编码在 hash 串自身（"$2a$10$" 里的 10），校验耗时由**存的那个 hash**
// 决定，与任何配置变量无关。所以「两条路径复用同一个 cost 变量」在 bcrypt 上做不到：
// 真路径的 cost 来自每个用户自己的 hash。日后调高 cost 时，legacy 行仍是旧 cost，
// 响应快慢就会泄露账号的年代 —— 换 cost 必须连带处理存量行，不是改一个常量。
// 实测：cost 10 = 36ms，cost 12 = 145ms，4 倍差远在噪声之上。
const dummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// VerifyPassword 校验用户名密码。
//
// ⚠️ 2026-09-04 实测修复 —— 这里曾经有【三】条路径而不是两条：
//
//	① 用户不存在        → 烧 dummy      36ms
//	② 用户存在有密码    → 烧真 hash     36ms
//	③ 用户存在但无密码（纯 Akasha 账号）→ `a.PasswordHash == nil ||` 短路，
//	   bcrypt 【根本没跑】 → 0ms
//
// 于是「近乎瞬时的失败」= 这个用户名存在且是 SSO 账号 —— 枚举预言机原样成立。
// 当年那句「烧一次 bcrypt 让两种失败耗时一致」防住了 ①②，而 ③ 是它没数到的那条。
//
// 现在的写法只留【一处】bcrypt 调用且无条件执行：先选 hash 再统一校验。
// 「某条路径跳过了工作」这个缺陷因此写不出来 —— 要重现它得先新增第二个调用点。
func (s *service) VerifyPassword(ctx context.Context, username, password string) (*Account, error) {
	a, err := s.repo.FindByUsername(ctx, username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	// 选 hash：真账号用自己的，其余一律用 dummy。分支只决定【用哪个 hash】，不决定【跑不跑】。
	hash := dummyHash
	if a != nil && a.PasswordHash != nil {
		hash = *a.PasswordHash
	}
	matched := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil

	if a == nil || a.PasswordHash == nil || !matched {
		return nil, ErrBadCredentials
	}
	return a, nil
}

func (s *service) EstablishSSO(ctx context.Context, sub, display string) (*Account, error) {
	return s.repo.UpsertByAkashaSub(ctx, sub, display)
}

func (s *service) FindByID(ctx context.Context, id int64) (*Account, error) {
	return s.repo.FindByID(ctx, id)
}
