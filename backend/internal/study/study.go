// Package study 是学习记录域：作答、自评，以及将来的 FSRS 卡片。
package study

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"

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
	AccountID  userid.UserID
	QuestionID int64
	Chosen     string
	// Rating 是 FSRS 四键自评：1=Again 2=Hard 3=Good 4=Easy。
	//
	// ⭐ 2026-09-08 起可为 nil —— 作答在【揭晓那一刻】就记录，自评是可选增强。
	// ⚠️ 改因是一次真实的数据缺失：一个用户天天在用，attempt 表里一条都没有，
	//    因为她答完直接翻页、从不点自评 —— 而当时「保存」整个挂在自评那个动作上。
	//    ⇒ 作答本身就是事实（答对没、用了多久、从哪个入口来），
	//      它不该依赖一个可选的后续动作才能存活。
	// nil 时不排 FSRS 卡片（没有记忆信号可用），其余一切照记。
	Rating     *int
	DurationMs *int
	// Context 是这次作答的出处（JSON 原文，形如 {"mode":"tag","tagId":44}）。
	// 领域层只负责落库与原样读出，不解释它；解释权在 LoadResume。
	Context *string
}

// Result 是记录后的判定结果。
type Result struct {
	// AttemptID 这条作答的主键 —— 客户端随后用它 PATCH 补自评。
	AttemptID int64
	Correct   bool
	Reference *question.Reference
	// Scheduled 说明下面三个调度字段是否有意义。
	// ⛔ 未自评时为 false —— 那时没有卡片被排，NextDue / NextState 是零值，
	//    调用方必须据此决定「返回 schedule」还是「返回 null」，⛔ 不要看零值猜。
	Scheduled bool
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

// ErrAttemptNotFound 表示这条作答不存在，**或不属于当前账号**。
// ⭐ 两种情况刻意合并成同一个错误：区分开就等于告诉调用方「这个 id 存在，只是不是你的」，
// 那是一条可枚举他人记录的信息泄漏。与登录路径「用户不存在 / 密码错误」不可区分同源。
var ErrAttemptNotFound = errors.New("attempt not found")

// Service 是学习记录域的门面。
type Service interface {
	RecordAttempt(ctx context.Context, a Attempt) (*Result, error)
	// RateAttempt 给一条已有的作答补上自评，并据此排 FSRS 卡片。
	// 只能改自己的记录；不存在或不属于该账号一律 ErrAttemptNotFound。
	RateAttempt(ctx context.Context, accountID userid.UserID, attemptID int64, rating int) (*Result, error)
	LoadProgress(ctx context.Context, accountID userid.UserID, slug string) (*Progress, error)
	LoadTagStats(ctx context.Context, accountID userid.UserID, slug, tagType string, minAttempts int) ([]TagStat, error)
	LoadResume(ctx context.Context, accountID userid.UserID, slug string) (*Resume, error)
	LoadOverview(ctx context.Context, accountID userid.UserID) (*Overview, error)
	LoadRecentSessions(ctx context.Context, accountID userid.UserID, limit int) ([]Session, error)
	LoadDueSummary(ctx context.Context, accountID userid.UserID) ([]DueSummary, error)
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
//
// ⭐ a.Rating 为 nil 时只落 attempt、不排卡片（揭晓即记录的主路径）。
func (s *service) RecordAttempt(ctx context.Context, a Attempt) (*Result, error) {
	ref, err := s.refs.LoadReference(ctx, a.QuestionID)
	if err != nil {
		return nil, err
	}

	chosen := normalizeAnswer(a.Chosen)
	correct := ref != nil && chosen == ref.Answer

	// 作答与卡片必须同一个事务：只写了 attempt 而卡片没更新，这题就再也不会到期；
	// 只更新了卡片而作答没落，统计与错题本就对不上。两者不一致都无法自愈。
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("开启事务: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // 已提交后的 Rollback 是空操作

	res, err := tx.ExecContext(ctx, `
		INSERT INTO attempt (user_id, question_id, chosen, correct, duration_ms, rating, context)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.AccountID, a.QuestionID, chosen, correct, a.DurationMs, a.Rating, a.Context)
	if err != nil {
		return nil, fmt.Errorf("写入作答记录: %w", err)
	}
	attemptID, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("取作答 id: %w", err)
	}

	out := &Result{AttemptID: attemptID, Correct: correct, Reference: ref}

	// ⭐ 只有自评过才排卡片 —— 没有 rating 就没有记忆信号，排不出有意义的间隔。
	// ⚠️ 未排卡片 ≠ 这题没做过：attempt 已经落库，unseen / 正确率 / resume 都认它。
	//    只有「到期复习（due）」这一个集合看卡片，所以未评分的题不会进复习队列 —— 这是对的。
	if a.Rating != nil {
		correction, next, err := s.scheduleFor(ctx, tx, a.AccountID, a.QuestionID, *a.Rating, correct, ref, a.DurationMs)
		if err != nil {
			return nil, err
		}
		out.Scheduled, out.Correction, out.NextDue, out.NextState = true, correction, next.Due, next.State
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交作答: %w", err)
	}
	return out, nil
}

// RateAttempt 给一条已记录的作答补上自评，并据此排 FSRS 卡片。
//
// ⭐ 归属校验放在 UPDATE 的 WHERE 里（user_id = ?），⛔ 不是先查后判 ——
// 先查后判存在检查与写入之间的窗口，且容易在重构时把 user_id 条件弄丢；
// 写在 WHERE 里则「改不到别人的记录」由 SQL 本身保证，影响行数为 0 就是没权限或不存在。
func (s *service) RateAttempt(ctx context.Context, accountID userid.UserID, attemptID int64, rating int) (*Result, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("开启事务: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var row struct {
		QuestionID int64 `db:"question_id"`
		Correct    bool  `db:"correct"`
		DurationMs *int  `db:"duration_ms"`
	}
	if err := tx.GetContext(ctx, &row, `
		SELECT question_id, correct, duration_ms FROM attempt WHERE id = ? AND user_id = ?`,
		attemptID, accountID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAttemptNotFound
		}
		return nil, fmt.Errorf("读取作答记录: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE attempt SET rating = ? WHERE id = ? AND user_id = ?`,
		rating, attemptID, accountID); err != nil {
		return nil, fmt.Errorf("写入自评: %w", err)
	}

	ref, err := s.refs.LoadReference(ctx, row.QuestionID)
	if err != nil {
		return nil, err
	}
	correction, next, err := s.scheduleFor(ctx, tx, accountID, row.QuestionID, rating, row.Correct, ref, row.DurationMs)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交自评: %w", err)
	}

	return &Result{
		AttemptID:  attemptID,
		Correct:    row.Correct,
		Reference:  ref,
		Scheduled:  true,
		Correction: correction,
		NextDue:    next.Due,
		NextState:  next.State,
	}, nil
}

// scheduleFor 由自评算出纠正后的评分并推进卡片。RecordAttempt 与 RateAttempt 共用 ——
// ⛔ 两处各写一份的话，纠正规则改了只改一处就会出现「记录时排一种、补评时排另一种」。
func (s *service) scheduleFor(
	ctx context.Context, tx *sqlx.Tx, accountID userid.UserID, questionID int64,
	rating int, correct bool, ref *question.Reference, durationMs *int,
) (scheduler.Correction, scheduler.Card, error) {
	textLen, err := s.refs.LoadTextLength(ctx, questionID)
	if err != nil {
		return scheduler.Correction{}, scheduler.Card{}, err
	}
	// ⭐ 自评的可信度纠正 —— 详见 scheduler/rating.go。
	// attempt.rating 落库的永远是用户按下的那个键；纠正只影响调度输入。
	correction := scheduler.EffectiveRating(rating, scheduler.Signals{
		Correct:      correct,
		HasReference: ref != nil,
		DurationMs:   durationMs,
		StemChars:    textLen,
	})
	card, err := loadCard(ctx, tx, accountID, questionID)
	if err != nil {
		return correction, scheduler.Card{}, err
	}
	next := s.sched.Next(card, correction.Effective, time.Now().UTC())
	if err = saveCard(ctx, tx, accountID, questionID, next); err != nil {
		return correction, next, err
	}
	return correction, next, nil
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
