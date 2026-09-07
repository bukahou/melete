package account

// Account 是一个学习者账号。
//
// ⚠️ 2026-09-07（localauth 阶段 2）：底层从 account 表换成 users 表，
// id 从 BIGINT 自增换成 UUIDv7（案卷 §22.2）。本 struct 只保留 melete
// 自己用得到的字段 —— users 有 15 列，⛔ 不全搬进来。
//
// 本地密码与 Akasha 联邦两种登录方式归到同一行：
// 学习记录挂在 user id 上，与登录方式无关。
type Account struct {
	// ID canonical UUID 文本。⚠️ 与库里的 BINARY(16) 之间的转换只能走
	// userid.Encode / DecodeID —— 编解码只有一处。
	ID           string
	Username     string
	PasswordHash *string
	Display      *string
	Status       int
}

// 账号状态。⛔ 零值位不是可登录的那一侧（与 localauthx 同一套值）。
const (
	StatusUnknown  = 0
	StatusActive   = 1
	StatusInactive = 2
	StatusBanned   = 3
)

// CanLogin ⭐ 白名单，⛔ 不是黑名单。
// 写成 `!= StatusBanned` 会让新增的状态默认可登录 ——
// 一个同类系统实测栽过的缺陷正是这个形状。
func (a *Account) CanLogin() bool { return a != nil && a.Status == StatusActive }

// DisplayName 给界面一个总是可用的称呼。
func (a *Account) DisplayName() string {
	if a.Display != nil && *a.Display != "" {
		return *a.Display
	}
	if a.Username != "" {
		return a.Username
	}
	return "学习者"
}
