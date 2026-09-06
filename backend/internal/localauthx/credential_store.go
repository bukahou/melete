package localauthx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bukahou/gokit/localauth"
	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

// 账号状态。
//
// ⛔⛔ 零值【不是】可登录的那一侧。
//
// ⚠️ 这与模块把 RotateUnknown 放在零值位是同一条纪律：默认值不得通向
// 有破坏力的一侧。这里"有破坏力"的是【放行】—— 一行 status 因为任何原因
// 是 0（列默认、迁移遗漏、写入 bug），必须表现为不能登录，而不是能登录。
const (
	StatusUnknown  = 0 // ⛔ 占据零值位，不可登录
	StatusActive   = 1
	StatusInactive = 2
	StatusBanned   = 3
)

// canLogin ⭐ 白名单，⛔ 不是黑名单。
//
// ⚠️ 写成 `status != StatusBanned` 是一个真实发生过的缺陷：
// cross-exam 005 记录一个同类系统的缺陷 —— 用黑名单判定导致
// 【停用的账号仍可登录】。黑名单要求你穷举所有坏状态，而新增一个状态
// 是最正常不过的事；白名单只要求你列出好状态，新增状态默认被拒。
func canLogin(status int) bool { return status == StatusActive }

// credentialStore 实现 localauth.CredentialStore，并顺带提供
// LookupFunc / AccountStatusFunc / AccountCreator —— 它们读写同一张 users 表。
type credentialStore struct{ db *sqlx.DB }

// 编译期契约检查 —— ⭐ 两个函数类型也钉住。
//
// ⚠️ LookupFunc / AccountStatusFunc 是 func 类型而不是 interface，
// 少了这两行，签名变化要到【接线那一刻】才会被发现，
// 而接线在阶段 3 —— 那时已经隔了几十个提交。
var (
	_ localauth.CredentialStore   = (*credentialStore)(nil)
	_ localauth.AccountCreator    = (*credentialStore)(nil)
	_ localauth.LookupFunc        = (*credentialStore)(nil).LookupHashByUsername
	_ localauth.AccountStatusFunc = (*credentialStore)(nil).AccountStatus
)

func NewCredentialStore(db *sqlx.DB) *credentialStore { return &credentialStore{db: db} }

func (s *credentialStore) LoadCredential(ctx context.Context, userID string) (localauth.Credential, bool, error) {
	uid, err := userid.Encode(userID)
	if err != nil {
		return localauth.Credential{}, false, err
	}
	var row struct {
		Hash   sql.NullString `db:"password_hash"`
		Status int            `db:"status"`
	}
	err = s.db.GetContext(ctx, &row,
		`SELECT password_hash, status FROM users WHERE id = ? AND deleted_at IS NULL`, uid)
	if errors.Is(err, sql.ErrNoRows) {
		return localauth.Credential{}, false, nil
	}
	if err != nil {
		return localauth.Credential{}, false, fmt.Errorf("加载口令: %w", err)
	}
	// ⭐ Hash 空串 = 该账号没有口令（纯 OIDC）。⛔ 不是错误，也不是"未加载"——
	// 「能不能设初始口令」正是靠它判断的。
	return localauth.Credential{Hash: row.Hash.String, Active: canLogin(row.Status)}, true, nil
}

// SaveCredential ⚠️ hash 与 changedAt 必须在【同一条语句】里写入。
//
// 分两条写的话，两者之间存在一个「新口令已生效、但 password_changed_at
// 还是旧值」的窗口 —— 而吊销判定用的正是 password_changed_at。
// ⇒ 那个窗口里，用旧口令签发的 access token 仍然有效。
func (s *credentialStore) SaveCredential(ctx context.Context, userID, hash string, changedAt time.Time) error {
	uid, err := userid.Encode(userID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, password_changed_at = ?, updated_at = ? WHERE id = ?`,
		hash, changedAt, time.Now().UTC(), uid)
	if err != nil {
		return fmt.Errorf("保存口令: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// ⚠️ 0 行说明账号不存在。⛔ 不能静默成功 —— 那会让"改密成功"
		// 这个返回值失去意义，而调用方据此告诉用户"已改好"。
		return fmt.Errorf("保存口令: 账号 %s 不存在", userID)
	}
	return nil
}

// LookupHashByUsername 是 localauth.LookupFunc 的实现。
//
// ⚠️ 返回 found=false 与返回空 hash 是【两件事】，但对调用方的效果相同 ——
// 模块把两者都交给 Verifier 走同一条路径（见 guard.go ③ 的注释），
// 于是"用户不存在"与"联邦账号无口令"耗时一致，⛔ 不构成枚举预言机。
func (s *credentialStore) LookupHashByUsername(ctx context.Context, username string) (string, bool, error) {
	var row struct {
		Hash   sql.NullString `db:"password_hash"`
		Status int            `db:"status"`
	}
	err := s.db.GetContext(ctx, &row,
		`SELECT password_hash, status FROM users WHERE username = ? AND deleted_at IS NULL`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("按用户名查口令: %w", err)
	}
	return row.Hash.String, true, nil
}

// AccountStatus 是 localauth.AccountStatusFunc 的实现。
func (s *credentialStore) AccountStatus(ctx context.Context, userID string) (localauth.AccountStatus, error) {
	uid, err := userid.Encode(userID)
	if err != nil {
		return localauth.AccountStatus{}, err
	}
	var row struct {
		Status    int          `db:"status"`
		ChangedAt sql.NullTime `db:"password_changed_at"`
		DeletedAt sql.NullTime `db:"deleted_at"`
	}
	err = s.db.GetContext(ctx, &row,
		`SELECT status, password_changed_at, deleted_at FROM users WHERE id = ?`, uid)
	if errors.Is(err, sql.ErrNoRows) {
		// ⛔ 账号不存在 → Active=false（零值），⛔ 不报错。
		// 报错会让"查不到"与"存储故障"可区分，而前者是攻击者可探测的。
		return localauth.AccountStatus{}, nil
	}
	if err != nil {
		return localauth.AccountStatus{}, fmt.Errorf("查询账号状态: %w", err)
	}
	return localauth.AccountStatus{
		Active:            canLogin(row.Status) && !row.DeletedAt.Valid,
		PasswordChangedAt: row.ChangedAt.Time,
	}, nil
}

func (s *credentialStore) UsernameTaken(ctx context.Context, username string) (bool, error) {
	return s.exists(ctx, `SELECT 1 FROM users WHERE username = ?`, username)
}

func (s *credentialStore) EmailTaken(ctx context.Context, email string) (bool, error) {
	return s.exists(ctx, `SELECT 1 FROM users WHERE email = ?`, email)
}

func (s *credentialStore) exists(ctx context.Context, q string, arg any) (bool, error) {
	var one int
	err := s.db.GetContext(ctx, &one, q, arg)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("查重: %w", err)
	}
	return true, nil
}

// CreateAccount 建号。
//
// ⚠️ UsernameTaken / EmailTaken 是【建议性】的：查完到写入之间有窗口，
// 并发注册两边都会查到"没被占用"。⛔ 所以真正的守门是数据库的唯一索引，
// 而这里必须把撞索引翻译成 CodeUsernameTaken / CodeEmailTaken ——
// 一次正常的并发撞车不该被报成 500。
func (s *credentialStore) CreateAccount(ctx context.Context, acct localauth.NewAccount) (string, error) {
	id, err := userid.New()
	if err != nil {
		return "", fmt.Errorf("生成账号 id: %w", err)
	}
	idBin, err := userid.Encode(id)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, status, created_at, updated_at,
		                   password_hash, password_changed_at,
		                   email, email_verified, display_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idBin, acct.Username, StatusActive, now, now,
		nullStr(acct.PasswordHash), nullTime(acct.PasswordHash != "", now),
		nullStr(acct.Email), acct.Email != "", nullStr(acct.DisplayName))
	if err != nil {
		if code, ok := duplicateKeyOf(err); ok {
			return "", localauth.NewError(code, "该标识已被占用")
		}
		return "", fmt.Errorf("建立账号: %w", err)
	}
	return id, nil
}

// duplicateKeyOf 把 MySQL 的 1062 翻译成模块的错误码。
//
// ⚠️ 靠索引【名字】区分是哪一个撞了 —— 所以索引名是契约的一部分，
// ⛔ 改名 uk_username / uk_email 会静默地把这里降级成"未知重复"。
func duplicateKeyOf(err error) (localauth.Code, bool) {
	var me *mysql.MySQLError
	if !errors.As(err, &me) || me.Number != 1062 {
		return "", false
	}
	switch {
	case strings.Contains(me.Message, "uk_username"):
		return localauth.CodeUsernameTaken, true
	case strings.Contains(me.Message, "uk_email"):
		return localauth.CodeEmailTaken, true
	default:
		return "", false
	}
}

func nullTime(ok bool, t time.Time) any {
	if !ok {
		return nil
	}
	return t
}
