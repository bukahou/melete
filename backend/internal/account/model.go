package account

// Account 是一个学习者账号。
// 本地密码与 Akasha 联邦两种登录方式归到同一行 ——
// 学习记录挂在 account.id 上，与登录方式无关。
type Account struct {
	ID           int64   `db:"id"`
	AkashaSub    *string `db:"akasha_sub"`
	Username     *string `db:"username"`
	PasswordHash *string `db:"password_hash"`
	Display      *string `db:"display"`
}

// DisplayName 给界面一个总是可用的称呼。
func (a *Account) DisplayName() string {
	if a.Display != nil && *a.Display != "" {
		return *a.Display
	}
	if a.Username != nil && *a.Username != "" {
		return *a.Username
	}
	return "学习者"
}
