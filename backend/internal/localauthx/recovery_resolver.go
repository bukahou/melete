package localauthx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bukahou/gokit/localauth"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

// recoveryResolver 回答「往哪里发找回码」。
//
// ⛔⛔ 两个方法都【只认本应用验证过的地址】：只查 email 列且要求
// email_verified = 1，⛔ 永不查 upstream_email、⛔ 永不 COALESCE。
//
// ⚠️ 为什么这条这么硬：upstream_email 是上游 IdP 说的，本应用【没有验证过】。
// 拿它当恢复地址，等于把「谁能重置这个账号的密码」这件事委托给上游 ——
// 而上游换了邮箱、或上游本身被接管，本应用一无所知。
// ⭐ 案卷 §18.3.3：「即使字面相同也必须走完验证 —— 相同的字面值
// 不等于相同的证明来源」。
//
// ⭐ 联邦账号在这里自然落空（没有已验证 email）⇒ 找回流程对它不可用。
// 案卷 §18.3.2：这是「不实现 = 安全」，⛔ 不是「忘记实现 = 洞」。
type recoveryResolver struct{ db *sqlx.DB }

var _ localauth.RecoveryAddressResolver = (*recoveryResolver)(nil)

func NewRecoveryResolver(db *sqlx.DB) localauth.RecoveryAddressResolver {
	return &recoveryResolver{db: db}
}

func (r *recoveryResolver) UserByVerifiedAddress(ctx context.Context, address string) (string, bool, error) {
	var bin []byte
	err := r.db.GetContext(ctx, &bin, `
		SELECT id FROM users
		 WHERE email = ? AND email_verified = 1 AND deleted_at IS NULL`, address)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("按已验证地址查账号: %w", err)
	}
	id, derr := userid.Decode(bin)
	if derr != nil {
		return "", false, derr
	}
	return id, true, nil
}

// VerifiedRecoveryAddress 该用户【此刻】的已验证地址。
//
// ⭐ 为什么模块要这第二个方法：请求与完成之间地址可能被改了。
// 2026-09-05 实测到的账号接管链正是这个形状 —— 先换恢复地址，再申请找回。
// ⇒ 完成时必须复查「码发往的地址」仍是「此刻的已验证地址」。
func (r *recoveryResolver) VerifiedRecoveryAddress(ctx context.Context, uid string) (string, bool, error) {
	var addr sql.NullString
	err := r.db.GetContext(ctx, &addr, `
		SELECT email FROM users
		 WHERE id = ? AND email_verified = 1 AND deleted_at IS NULL`, userid.UserID(uid))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !addr.Valid) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("查已验证恢复地址: %w", err)
	}
	return addr.String, true, nil
}
