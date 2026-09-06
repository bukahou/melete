package localauthx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bukahou/gokit/localauth"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

type emailChanger struct{ db *sqlx.DB }

var _ localauth.EmailChanger = (*emailChanger)(nil)

func NewEmailChanger(db *sqlx.DB) localauth.EmailChanger { return &emailChanger{db: db} }

func (e *emailChanger) EmailTaken(ctx context.Context, email string) (bool, error) {
	var one int
	err := e.db.GetContext(ctx, &one, `SELECT 1 FROM users WHERE email = ?`, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("查邮箱是否占用: %w", err)
	}
	return true, nil
}

// ChangeVerifiedEmail ⚠️ 在【同一条语句】里写 email 与 email_verified。
//
// 分两条写的话，两者之间存在一个「新邮箱已生效、但 email_verified 还是 0」
// 的窗口 —— 而恢复地址解析要求 email_verified = 1，
// ⇒ 那个窗口里找回流程会认为该账号没有恢复地址。
func (e *emailChanger) ChangeVerifiedEmail(ctx context.Context, uid, newEmail string) (string, error) {
	var old string
	err := runInTx(ctx, e.db, func(tx *sqlx.Tx) error {
		// 先取旧地址 —— ⭐ 模块要用它给旧邮箱发「你的邮箱被改了」的通知。
		// ⚠️ 那条通知是账号接管链上唯一会让受害者察觉的信号（案卷 §18.5）。
		var prev *string
		if err := tx.GetContext(ctx, &prev,
			`SELECT email FROM users WHERE id = ? FOR UPDATE`, userid.UserID(uid)); err != nil {
			return fmt.Errorf("取旧邮箱: %w", err)
		}
		if prev != nil {
			old = *prev
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET email = ?, email_verified = 1, updated_at = ? WHERE id = ?`,
			newEmail, time.Now().UTC(), userid.UserID(uid)); err != nil {
			if code, ok := duplicateKeyOf(err); ok && code == localauth.CodeEmailTaken {
				return localauth.NewError(localauth.CodeEmailTaken, "该邮箱已被占用")
			}
			return fmt.Errorf("写入新邮箱: %w", err)
		}
		return nil
	})
	return old, err
}
