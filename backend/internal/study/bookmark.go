package study

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bukahou/melete/backend/internal/userid"
)

// ErrQuestionNotFound 表示要收藏的题目不存在。
var ErrQuestionNotFound = errors.New("question not found")

// SetBookmark 收藏 / 取消收藏一题（P9 #20）。两个方向都幂等。
// ⚠️ 不建外键（TiDB 纪律）⇒ 收藏前由这里确认题目存在，⛔ 不让一个 id 随手写进表。
func (s *service) SetBookmark(ctx context.Context, accountID userid.UserID, questionID int64, on bool) error {
	if !on {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM bookmark WHERE user_id = ? AND question_id = ?`, accountID, questionID); err != nil {
			return fmt.Errorf("取消收藏: %w", err)
		}
		return nil
	}
	var one int
	if err := s.db.GetContext(ctx, &one, `SELECT 1 FROM question WHERE id = ?`, questionID); errors.Is(err, sql.ErrNoRows) {
		return ErrQuestionNotFound
	} else if err != nil {
		return fmt.Errorf("查题目: %w", err)
	}
	// 已收藏时保留最初的收藏时间（「收藏的先后」以第一次为准）
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO bookmark (user_id, question_id, created_at) VALUES (?, ?, UTC_TIMESTAMP())
		ON DUPLICATE KEY UPDATE created_at = created_at`, accountID, questionID); err != nil {
		return fmt.Errorf("收藏: %w", err)
	}
	return nil
}

// IsBookmarked 该账号是否收藏了这题。
func (s *service) IsBookmarked(ctx context.Context, accountID userid.UserID, questionID int64) (bool, error) {
	var n int
	if err := s.db.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM bookmark WHERE user_id = ? AND question_id = ?`, accountID, questionID); err != nil {
		return false, fmt.Errorf("查收藏: %w", err)
	}
	return n > 0, nil
}
