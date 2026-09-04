// Package study 是学习记录域：作答、自评，以及将来的 FSRS 卡片。
package study

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/study/scheduler"
)

// referenceSource 是本域对 question 域的全部依赖 —— 接口定义在使用方（依赖倒置），
// 只要「拿参考答案」一件事，不引入对方的完整仓储。
type referenceSource interface {
	LoadReference(ctx context.Context, questionID int64) (*question.Reference, error)
	// LoadTextLength 题面字数，供评分纠正规则②判断「这个用时读不完题面」。
	LoadTextLength(ctx context.Context, questionID int64) (int, error)
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
	// Correction 说明自评是否被纠正、以及为什么。
	// ⚠️ 要显示给用户看 —— 不偷偷改调度，与三方主张并列展示是同一条哲学。
	Correction scheduler.Correction
	// NextDue 这道题下次该出现的时间。
	NextDue time.Time
	// NextState 调度后的卡片状态，供界面说「进入复习」还是「当天再来」。
	NextState scheduler.State
}

// ErrNotFound 表示题库不存在。
var ErrNotFound = errors.New("bank not found")

// Service 是学习记录域的门面。
type Service interface {
	RecordAttempt(ctx context.Context, a Attempt) (*Result, error)
	LoadProgress(ctx context.Context, accountID int64, slug string) (*Progress, error)
	LoadTagStats(ctx context.Context, accountID int64, slug, tagType string, minAttempts int) ([]TagStat, error)
	LoadResume(ctx context.Context, accountID int64, slug string) (*Resume, error)
	LoadOverview(ctx context.Context, accountID int64) (*Overview, error)
	LoadRecentSessions(ctx context.Context, accountID int64, limit int) ([]Session, error)
	LoadDueSummary(ctx context.Context, accountID int64) ([]DueSummary, error)
}

type service struct {
	db    *sqlx.DB
	refs  referenceSource
	sched *scheduler.Scheduler
}

func NewService(db *sqlx.DB, refs referenceSource) Service {
	return &service{db: db, refs: refs, sched: scheduler.New()}
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

	textLen, err := s.refs.LoadTextLength(ctx, a.QuestionID)
	if err != nil {
		return nil, err
	}

	// ⭐ 自评的可信度纠正 —— 详见 scheduler/rating.go。
	// attempt.rating 落库的永远是用户按下的那个键；纠正只影响调度输入。
	correction := scheduler.EffectiveRating(a.Rating, scheduler.Signals{
		Correct:      correct,
		HasReference: ref != nil,
		DurationMs:   a.DurationMs,
		StemChars:    textLen,
	})

	// 作答与卡片必须同一个事务：只写了 attempt 而卡片没更新，这题就再也不会到期；
	// 只更新了卡片而作答没落，统计与错题本就对不上。两者不一致都无法自愈。
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("开启事务: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // 已提交后的 Rollback 是空操作

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO attempt (account_id, question_id, chosen, correct, duration_ms, rating, context)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.AccountID, a.QuestionID, chosen, correct, a.DurationMs, a.Rating, a.Context); err != nil {
		return nil, fmt.Errorf("写入作答记录: %w", err)
	}

	card, err := loadCard(ctx, tx, a.AccountID, a.QuestionID)
	if err != nil {
		return nil, err
	}
	next := s.sched.Next(card, correction.Effective, time.Now().UTC())
	if err = saveCard(ctx, tx, a.AccountID, a.QuestionID, next); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交作答: %w", err)
	}

	return &Result{
		Correct:    correct,
		Reference:  ref,
		Correction: correction,
		NextDue:    next.Due,
		NextState:  next.State,
	}, nil
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
