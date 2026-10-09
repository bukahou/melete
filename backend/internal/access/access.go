// Package access 是「谁能看哪个题库、谁能改谁的档位」的唯一归属地（P9 裁决 #27–#30）。
//
// 设计见 docs/design/active/bank-access.md。三档用户：
//
//	普通   注册即是，user_permissions 空集        → 只看公开题库
//	高级   content:private                       → 公开 + 全部私有题库
//	admin  user:manage（或 *）                    → 全部；可升降级普通 / 高级用户
//
// ⭐ 「能不能看」只有一条规则：题库是公开的 ∨ 用户的档位 ≥ 高级。
// 它被收进 Scope 这一个值里，所有和题库有关的地方只拿 Scope 判断 —— ⛔ 不得在别处另写一遍。
//
// ⛔ 不缓存（进程内或别处）：升降级要立刻生效。权限也 ⛔ 不进登录令牌，否则降级要等令牌过期。
// ⛔ 读权限出错时调用方必须返回错误，⛔ 不得当成「普通用户」放行，也 ⛔ 不得当成「有权限」。
package access

import (
	"context"
	"errors"
	"time"

	"github.com/bukahou/melete/backend/internal/userid"
)

// Tier 是用户的档位。
type Tier string

const (
	TierBasic    Tier = "basic"
	TierAdvanced Tier = "advanced"
	TierAdmin    Tier = "admin"
)

// 权限值（user_permissions.permission）。⛔ 只认这几个常量 —— 写进库里的任何其它值都不产生效力。
const (
	PermPrivate = "content:private" // 高级：可以看私有题库
	PermManage  = "user:manage"     // admin：升降级普通 / 高级用户，并能看全部
	PermAll     = "*"               // 超级（由超级用户 = 直接操作数据库的人写入；应用里不授予）
)

// 题库的可见性（bank.visibility）。
const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"
)

// TierOf 按「最高那一档」从权限集合推出档位。空集 = 普通，这是正常状态，⛔ 不是错误。
func TierOf(perms []string) Tier {
	tier := TierBasic
	for _, p := range perms {
		switch p {
		case PermManage, PermAll:
			return TierAdmin
		case PermPrivate:
			tier = TierAdvanced
		}
	}
	return tier
}

// Scope 是一个用户「能看哪些题库」。零值 = 只能看公开题库（fail-closed 的缺省）。
type Scope struct{ seesPrivate bool }

// ScopeFor 由档位得出 Scope。
func ScopeFor(t Tier) Scope { return Scope{seesPrivate: t == TierAdvanced || t == TierAdmin} }

// Allows 判断某个可见性的题库能不能看。未知的可见性值一律按私有处理。
func (s Scope) Allows(visibility string) bool {
	return visibility == VisibilityPublic || s.seesPrivate
}

// BankFilter 返回一个 SQL 条件片段，用来在查询里只留下看得到的题库。
// ⚠️ alias 必须是调用方代码里的常量（表别名），⛔ 绝不传入任何来自请求的字符串。
// 片段里不含参数：看得到私有题库时恒真，否则只留公开题库。
func (s Scope) BankFilter(alias string) string {
	if s.seesPrivate {
		return "1 = 1"
	}
	return alias + ".visibility = '" + VisibilityPublic + "'"
}

var (
	// ErrNotFound：对象（题目 / 作答 / 用户）不存在。
	ErrNotFound = errors.New("不存在")
	// ErrTargetIsAdmin：admin 不能操作任何 admin，包括自己（P9 #29）。
	ErrTargetIsAdmin = errors.New("不能修改 admin 账号")
	// ErrInvalidTier：只能设为普通或高级 —— 应用里没有授予 admin 的入口（P9 #28）。
	ErrInvalidTier = errors.New("只能设为普通或高级")
	// ErrNotAdmin：调用方不是 admin。纵深防御的第二道 —— 接口层已经挡过一次。
	ErrNotAdmin = errors.New("需要 admin")
)

// User 是 admin 页面上的一行。显示名与邮箱都来自 Akasha，只用于辨认，⛔ 不用于认证。
type User struct {
	ID          userid.UserID
	DisplayName string
	Email       string // upstream_email（Akasha 带来的）；没有则为空
	AvatarURL   string
	Tier        Tier
	CreatedAt   time.Time
	LastLoginAt *time.Time
}

// Service 是 access 的门面。
type Service interface {
	// Resolve 读出用户的档位与 Scope。出错时返回 error —— ⛔ 调用方不得降级成「普通」继续。
	Resolve(ctx context.Context, userID userid.UserID) (Tier, Scope, error)
	// QuestionVisibility / AttemptVisibility：按对象编号找到所属题库的可见性。
	// 用于路径里不带题库名的接口。对象不存在返回 ErrNotFound。
	QuestionVisibility(ctx context.Context, questionID int64) (string, error)
	AttemptVisibility(ctx context.Context, attemptID int64) (string, error)

	// Profile：自己的资料与档位（「我的」页面）。
	Profile(ctx context.Context, userID userid.UserID) (User, error)

	// ListUsers：admin 页面的用户列表，按注册时间倒序分页。
	ListUsers(ctx context.Context, actorID userid.UserID, page, pageSize int) ([]User, int, error)
	// SetTier：admin 把普通 / 高级用户升降级。目标是 admin（含自己）⇒ ErrTargetIsAdmin。
	SetTier(ctx context.Context, actorID, targetID userid.UserID, tier Tier) error
}
