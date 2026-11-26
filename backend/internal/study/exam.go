package study

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bukahou/melete/backend/internal/bank"
)

// loadExam 取题库的考试参数（及格线、一场考试的题数），合格判断要用。
func (s *service) loadExam(ctx context.Context, slug string) (bank.Exam, error) {
	var meta sql.NullString
	err := s.db.GetContext(ctx, &meta, `SELECT meta FROM bank WHERE slug = ?`, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return bank.Exam{}, ErrNotFound
	}
	if err != nil {
		return bank.Exam{}, fmt.Errorf("查询题库 %q 的元数据: %w", slug, err)
	}
	if !meta.Valid {
		return bank.Exam{}, nil
	}
	return bank.ParseExam(&meta.String)
}
