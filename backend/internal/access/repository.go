package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

type service struct{ db *sqlx.DB }

// NewService 用数据库实现 access 的门面。
func NewService(db *sqlx.DB) Service { return &service{db: db} }

// 有效权限：没过期的（expires_at 为空 = 永久）。
const livePermission = `(expires_at IS NULL OR expires_at > UTC_TIMESTAMP())`

func (s *service) permissionsOf(ctx context.Context, q sqlx.QueryerContext, id userid.UserID) ([]string, error) {
	var perms []string
	if err := sqlx.SelectContext(ctx, q, &perms,
		`SELECT permission FROM user_permissions WHERE user_id = ? AND `+livePermission, id); err != nil {
		return nil, fmt.Errorf("读取权限: %w", err)
	}
	return perms, nil
}

func (s *service) Resolve(ctx context.Context, id userid.UserID) (Tier, Scope, error) {
	perms, err := s.permissionsOf(ctx, s.db, id)
	if err != nil {
		return "", Scope{}, err
	}
	t := TierOf(perms)
	return t, ScopeFor(t), nil
}

func (s *service) visibility(ctx context.Context, query string, arg int64) (string, error) {
	var v string
	err := s.db.GetContext(ctx, &v, query, arg)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("查询题库可见性: %w", err)
	}
	return v, nil
}

func (s *service) QuestionVisibility(ctx context.Context, questionID int64) (string, error) {
	return s.visibility(ctx, `
		SELECT b.visibility FROM question q JOIN bank b ON b.id = q.bank_id WHERE q.id = ?`, questionID)
}

func (s *service) AttemptVisibility(ctx context.Context, attemptID int64) (string, error) {
	return s.visibility(ctx, `
		SELECT b.visibility FROM attempt a
		JOIN question q ON q.id = a.question_id
		JOIN bank b     ON b.id = q.bank_id
		WHERE a.id = ?`, attemptID)
}

func (s *service) requireAdmin(ctx context.Context, q sqlx.QueryerContext, actor userid.UserID) error {
	perms, err := s.permissionsOf(ctx, q, actor)
	if err != nil {
		return err
	}
	if TierOf(perms) != TierAdmin {
		return ErrNotAdmin
	}
	return nil
}

func (s *service) ListUsers(ctx context.Context, actor userid.UserID, page, pageSize int) ([]User, int, error) {
	if err := s.requireAdmin(ctx, s.db, actor); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	var total int
	if err := s.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`); err != nil {
		return nil, 0, fmt.Errorf("统计用户: %w", err)
	}
	type row struct {
		ID          []byte     `db:"id"`
		DisplayName string     `db:"display_name"`
		Email       string     `db:"email"`
		CreatedAt   time.Time  `db:"created_at"`
		LastLoginAt *time.Time `db:"last_login_at"`
	}
	var rows []row
	// ⚠️ 按 created_at 排而不是 id：id 是 UUIDv7 虽然时间有序，但「注册时间」才是界面上的语义。
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT id, COALESCE(display_name, username) AS display_name,
		       COALESCE(upstream_email, '') AS email, created_at, last_login_at
		FROM users WHERE deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize); err != nil {
		return nil, 0, fmt.Errorf("列出用户: %w", err)
	}
	out := make([]User, 0, len(rows))
	ids := make([]userid.UserID, 0, len(rows))
	for _, r := range rows {
		id, err := userid.Decode(r.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("解码用户 id: %w", err)
		}
		ids = append(ids, userid.UserID(id))
		out = append(out, User{ID: userid.UserID(id), DisplayName: r.DisplayName, Email: r.Email,
			Tier: TierBasic, CreatedAt: r.CreatedAt, LastLoginAt: r.LastLoginAt})
	}
	if len(ids) == 0 {
		return out, total, nil
	}
	query, args, err := sqlx.In(`SELECT user_id, permission FROM user_permissions WHERE user_id IN (?) AND `+livePermission, ids)
	if err != nil {
		return nil, 0, err
	}
	type perm struct {
		UserID     []byte `db:"user_id"`
		Permission string `db:"permission"`
	}
	var perms []perm
	if err := s.db.SelectContext(ctx, &perms, s.db.Rebind(query), args...); err != nil {
		return nil, 0, fmt.Errorf("读取用户权限: %w", err)
	}
	byUser := map[userid.UserID][]string{}
	for _, p := range perms {
		id, err := userid.Decode(p.UserID)
		if err != nil {
			return nil, 0, fmt.Errorf("解码权限 user_id: %w", err)
		}
		byUser[userid.UserID(id)] = append(byUser[userid.UserID(id)], p.Permission)
	}
	for i := range out {
		out[i].Tier = TierOf(byUser[out[i].ID])
	}
	return out, total, nil
}

// SetTier 升降级。在一个事务里：确认调用方是 admin → 确认目标存在且不是 admin → 写 / 删 content:private。
// 两个方向都幂等。
func (s *service) SetTier(ctx context.Context, actor, target userid.UserID, tier Tier) error {
	if tier != TierBasic && tier != TierAdvanced {
		return ErrInvalidTier
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.requireAdmin(ctx, tx, actor); err != nil {
		return err
	}
	var exists int
	if err := tx.GetContext(ctx, &exists,
		`SELECT COUNT(*) FROM users WHERE id = ? AND deleted_at IS NULL`, target); err != nil {
		return fmt.Errorf("查目标用户: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	targetPerms, err := s.permissionsOf(ctx, tx, target)
	if err != nil {
		return err
	}
	// ⭐ 自己也是 admin，所以「不能操作自己」不需要单独判断 —— 这一条就覆盖了。
	if TierOf(targetPerms) == TierAdmin {
		return ErrTargetIsAdmin
	}
	if tier == TierAdvanced {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO user_permissions (user_id, permission, granted_at, granted_by)
			VALUES (?, ?, UTC_TIMESTAMP(), ?)
			ON DUPLICATE KEY UPDATE expires_at = NULL`, target, PermPrivate, actor)
	} else {
		_, err = tx.ExecContext(ctx,
			`DELETE FROM user_permissions WHERE user_id = ? AND permission = ?`, target, PermPrivate)
	}
	if err != nil {
		return fmt.Errorf("写档位: %w", err)
	}
	return tx.Commit()
}
