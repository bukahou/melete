package account

import (
	"context"
	"crypto/rand"
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
	FindByID(ctx context.Context, id string) (*Account, error)
}

type service struct{ repo Repository }

func NewService(repo Repository) Service { return &service{repo: repo} }

// dummyHash 用户不存在 / 无密码时拿来烧的那个 hash。
//
// ⚠️ 2026-09-04：由【启动时随机生成】，不再是源码常量。
//
// 起因是一个真实的认证绕过（cross-exam 005 实施期由 geass-v3 自查发现，
// 三家同款代换）：代换 dummy 之后若把比对结果直接当认证结论，
// 拿 dummy 的明文当口令就能登入【任何不存在的用户名】——
// 而源码常量的明文是公开的，谁看到仓库谁就知道。
//
// melete 这里【本来就挡住了】：下方 `a == nil ||` 在返回前先判账号是否存在，
// 比对为真也不放行。但那是一道判断，判断会被后人「简化」掉。
// 改成随机生成之后，⛔ 连「知道明文」这件事本身都不成立 —— 缺陷不可表达，
// 比一道正确的判断更硬。两者并存，不是二选一。
//
// 代价：启动时多算一次 bcrypt（cost 10 实测 36ms），不在请求路径上。
//
// ⚠️ 它的 cost 必须与 account.password_hash 里所有真 hash 的 cost 一致 ——
// bcrypt 的 cost 编码在 hash 串自身（"$2a$10$" 里的 10），校验耗时由**存的那个 hash**
// 决定，与任何配置变量无关。所以「两条路径复用同一个 cost 变量」在 bcrypt 上做不到：
// 真路径的 cost 来自每个用户自己的 hash。日后调高 cost 时，legacy 行仍是旧 cost，
// 响应快慢就会泄露账号的年代 —— 换 cost 必须连带处理存量行，不是改一个常量。
// 实测：cost 10 = 36ms，cost 12 = 145ms，4 倍差远在噪声之上。
//
// ⚠️ 是 var 不是 const —— 测试要能换成「明文已知」的 hash，
// 才验得出「即便比对通过也不放行」这条性质。不可替换 = 不可验证。
var dummyHash = mustGenerateDummyHash()

// mustGenerateDummyHash 用一段随机口令现算一个 hash。
//
// ⚠️ cost 取 bcrypt.DefaultCost，与新账号一致。存量账号若是更低的 cost，
// 校验耗时会短于 dummy —— 那是本项目已登记的数据侧缺口（改 cost 是一次
// 要连带处理存量行的迁移），不是这里能解决的。
func mustGenerateDummyHash() string {
	var pw [32]byte
	// crypto/rand.Read 按 Go 文档【永不返回错误】—— 底层失败时它自己让进程崩溃。
	// 这个分支因此是死代码，留着只为「不忽略返回的 error」这条惯例。
	// ⚠️ 别把它当成一道真的防线：真正的保证来自标准库，不来自这里。
	if _, err := rand.Read(pw[:]); err != nil {
		panic("生成 dummy hash 失败，熵源不可用: " + err.Error())
	}
	h, err := bcrypt.GenerateFromPassword(pw[:], bcrypt.DefaultCost)
	if err != nil {
		panic("生成 dummy hash 失败: " + err.Error())
	}
	return string(h)
}

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

// EstablishSSO 确立 Akasha 联邦账号。
//
// ⛔ provider 写死 "akasha"：melete 只接这一个上游（案卷 §35 把
// 「OIDC 回调后建号」划归 akasha 范围，本次只换存储表不改编排）。
// ⚠️ 将来接第二个上游时，provider 要从调用方传进来 ——
// 而 identities 的主键是 (provider, subject)，schema 已经支持。
func (s *service) EstablishSSO(ctx context.Context, sub, display string) (*Account, error) {
	return s.repo.EstablishFederated(ctx, "akasha", sub, display)
}

func (s *service) FindByID(ctx context.Context, id string) (*Account, error) {
	return s.repo.FindByID(ctx, id)
}
