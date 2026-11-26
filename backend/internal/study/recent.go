package study

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	"github.com/bukahou/melete/backend/internal/userid"
)

const (
	// RecentWindowDays 是近期正确率的窗口：从请求时刻往前这么多个 24 小时（滚动，不按日历日切）。
	RecentWindowDays = 7
	// RecentMinAttempts 是给出近期正确率所需的最少作答次数：次数太少时一两道题就能让它大起大落。
	RecentMinAttempts = 10
)

// RecentAccuracy 是近期正确率：窗口内的每次作答都算（含重做），回答「最近练得怎么样」。
//
// ⚠️ 与合格等级用的当前掌握率（每题只看最近一次）是两个口径。
type RecentAccuracy struct {
	WindowDays   int
	AttemptCount int
	Rate         *int // 0–100，四舍五入；作答少于 RecentMinAttempts 时为 nil
}

// loadRecentAccuracy 统计某题库在窗口内的作答。
// 窗口用数据库的时钟：attempt.created_at 也是数据库写的，两边用同一个钟才不会差出时区或时钟漂移。
func (s *service) loadRecentAccuracy(ctx context.Context, accountID userid.UserID, slug string) (RecentAccuracy, error) {
	var row struct {
		Attempts int           `db:"attempts"`
		Correct  sql.NullInt64 `db:"correct"`
	}
	err := s.db.GetContext(ctx, &row, `
		SELECT COUNT(*) AS attempts, SUM(a.correct = 1) AS correct
		FROM attempt a
		JOIN question q ON q.id = a.question_id
		JOIN bank b     ON b.id = q.bank_id AND b.slug = ?
		WHERE a.user_id = ? AND a.created_at >= UTC_TIMESTAMP() - INTERVAL ? HOUR`,
		slug, accountID, RecentWindowDays*24)
	if err != nil {
		return RecentAccuracy{}, fmt.Errorf("统计近期正确率: %w", err)
	}
	r := RecentAccuracy{WindowDays: RecentWindowDays, AttemptCount: row.Attempts}
	if row.Attempts >= RecentMinAttempts {
		rate := int(math.Round(float64(row.Correct.Int64) * 100 / float64(row.Attempts)))
		r.Rate = &rate
	}
	return r, nil
}
