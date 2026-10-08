package study

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bukahou/melete/backend/internal/userid"
)

// CurrentBankSource 说明「当前题库」是怎么定出来的。
type CurrentBankSource string

const (
	CurrentBankChosen CurrentBankSource = "chosen" // 用户在设置里选的
	CurrentBankRecent CurrentBankSource = "recent" // 没选过，按最近作答推出
	CurrentBankNone   CurrentBankSource = "none"   // 都没有（新用户）
)

// CurrentBank 是首页显示的那个题库（P9 #2 #14）。Source 为 none 时 Slug 为空。
type CurrentBank struct {
	Slug   string
	Source CurrentBankSource
}

// LoadCurrentBank 按 chosen → recent → none 的顺序定出当前题库。
//
// ⭐ recent 只是推出来的，⛔ 不写回 users.current_bank_id —— 选题库是用户的动作，
// 读接口替他「选」一次，下次他在别的题库作答时首页就不会再跟着走了。
//
// ⚠️ chosen 指向的题库若已不存在（下架），视同没选过，继续往下推 ——
// ⛔ 不返回一个打不开的 slug。
func (s *service) LoadCurrentBank(ctx context.Context, accountID userid.UserID) (*CurrentBank, error) {
	var slug string
	err := s.db.GetContext(ctx, &slug, `
		SELECT b.slug FROM users u JOIN bank b ON b.id = u.current_bank_id
		WHERE u.id = ?`, accountID)
	switch {
	case err == nil:
		return &CurrentBank{Slug: slug, Source: CurrentBankChosen}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("读取当前题库: %w", err)
	}

	// ⚠️ 按 created_at 而不是 id 取「最近」：TiDB 的 auto_increment 各节点各持一段，
	// id 大 ≠ 写得晚（四条纪律之二）。id 只作同一秒内的次序。
	err = s.db.GetContext(ctx, &slug, `
		SELECT b.slug FROM attempt a
		JOIN question q ON q.id = a.question_id
		JOIN bank b     ON b.id = q.bank_id
		WHERE a.user_id = ?
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT 1`, accountID)
	switch {
	case err == nil:
		return &CurrentBank{Slug: slug, Source: CurrentBankRecent}, nil
	case errors.Is(err, sql.ErrNoRows):
		return &CurrentBank{Source: CurrentBankNone}, nil
	default:
		return nil, fmt.Errorf("按最近作答推当前题库: %w", err)
	}
}

// ChooseCurrentBank 把当前题库设为 slug。题库不存在返回 ErrNotFound。
func (s *service) ChooseCurrentBank(ctx context.Context, accountID userid.UserID, slug string) error {
	var bankID int64
	err := s.db.GetContext(ctx, &bankID, `SELECT id FROM bank WHERE slug = ?`, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("查题库: %w", err)
	}
	// ⚠️ 不动 updated_at：那一列属于账号生命周期（改密 / 改邮箱），
	// 切个题库就刷新它会让「资料最后修改时间」失去意义。
	if _, err := s.db.ExecContext(ctx,
		`UPDATE users SET current_bank_id = ? WHERE id = ?`, bankID, accountID); err != nil {
		return fmt.Errorf("写当前题库: %w", err)
	}
	return nil
}

// LastAttempt 是某题库里最近一次作答（P9 #13 单一继续槽位的来源）。
type LastAttempt struct {
	QuestionID int64     `db:"question_id"`
	Context    *string   `db:"context"`
	At         time.Time `db:"created_at"`
}

// LoadLastAttempt 取这个题库里最近一次作答；从没作答过返回 nil。
// ⚠️ 与 LoadCurrentBank 同理按 created_at 排（TiDB 的 id 不保证单调）。
func (s *service) LoadLastAttempt(ctx context.Context, accountID userid.UserID, slug string) (*LastAttempt, error) {
	var la LastAttempt
	err := s.db.GetContext(ctx, &la, `
		SELECT a.question_id, a.context, a.created_at
		FROM attempt a
		JOIN question q ON q.id = a.question_id
		JOIN bank b     ON b.id = q.bank_id
		WHERE a.user_id = ? AND b.slug = ?
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT 1`, accountID, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取最近一次作答: %w", err)
	}
	return &la, nil
}
