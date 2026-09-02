// Package study 是学习记录域：作答、自评，以及将来的 FSRS 卡片。
package study

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/question"
)

// referenceSource 是本域对 question 域的全部依赖 —— 接口定义在使用方（依赖倒置），
// 只要「拿参考答案」一件事，不引入对方的完整仓储。
type referenceSource interface {
	LoadReference(ctx context.Context, questionID int64) (*question.Reference, error)
}

// Attempt 是一次作答记录。
type Attempt struct {
	AccountID  int64
	QuestionID int64
	Chosen     string
	Rating     int // FSRS 四键：1=Again 2=Hard 3=Good 4=Easy
	DurationMs *int
	// Context 是这次作答的出处（JSON 原文，形如 {"mode":"tag","tagId":44}）。
	// 领域层只负责落库与原样读出，不解释它；解释权在 LoadResume。
	Context *string
}

// Result 是记录后的判定结果。
type Result struct {
	Correct   bool
	Reference *question.Reference
}

// ErrNotFound 表示题库不存在。
var ErrNotFound = errors.New("bank not found")

// Service 是学习记录域的门面。
type Service interface {
	RecordAttempt(ctx context.Context, a Attempt) (*Result, error)
	LoadProgress(ctx context.Context, accountID int64, slug string) (*Progress, error)
	LoadTagStats(ctx context.Context, accountID int64, slug, tagType string, minAttempts int) ([]TagStat, error)
	LoadResume(ctx context.Context, accountID int64, slug string) (*Resume, error)
}

type service struct {
	db   *sqlx.DB
	refs referenceSource
}

func NewService(db *sqlx.DB, refs referenceSource) Service {
	return &service{db: db, refs: refs}
}

// RecordAttempt 判定对错并落库。
// correct 由服务端算（chosen 归一后与参考答案比对）—— 判定权威只有这一处。
func (s *service) RecordAttempt(ctx context.Context, a Attempt) (*Result, error) {
	ref, err := s.refs.LoadReference(ctx, a.QuestionID)
	if err != nil {
		return nil, err
	}

	chosen := normalizeAnswer(a.Chosen)
	correct := ref != nil && chosen == ref.Answer

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO attempt (account_id, question_id, chosen, correct, duration_ms, rating, context)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.AccountID, a.QuestionID, chosen, correct, a.DurationMs, a.Rating, a.Context)
	if err != nil {
		return nil, fmt.Errorf("写入作答记录: %w", err)
	}
	return &Result{Correct: correct, Reference: ref}, nil
}

// normalizeAnswer 把选择归一成升序去重的字母串（与 answer_claim.answer 同一约定）。
func normalizeAnswer(raw string) string {
	letters := strings.Split(strings.ToUpper(strings.TrimSpace(raw)), "")
	sort.Strings(letters)
	uniq := letters[:0]
	for i, l := range letters {
		if l == "" || (i > 0 && l == letters[i-1]) {
			continue
		}
		uniq = append(uniq, l)
	}
	return strings.Join(uniq, "")
}
