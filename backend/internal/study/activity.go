package study

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bukahou/melete/backend/internal/userid"
)

// ActivityRange 是作答推移的期间。
type ActivityRange string

const (
	ActivityRange7d  ActivityRange = "7d"
	ActivityRange30d ActivityRange = "30d"
	ActivityRange90d ActivityRange = "90d"
	// ActivityRangeAll 从这个题库的第一次作答到今天。
	ActivityRangeAll ActivityRange = "all"

	// DefaultActivityRange 是不指定期间时用的。
	DefaultActivityRange = ActivityRange7d
)

// fixedRangeDays 是固定期间含今天在内的自然日数。all 不在表里：它从第一次作答起算。
var fixedRangeDays = map[ActivityRange]int{ActivityRange7d: 7, ActivityRange30d: 30, ActivityRange90d: 90}

// ErrUnknownRange 表示期间不是约定的取值。
var ErrUnknownRange = errors.New("unknown activity range")

// BucketUnit 是一个桶的长度。
type BucketUnit string

const (
	BucketDay   BucketUnit = "day"
	BucketWeek  BucketUnit = "week"  // 周一到周日
	BucketMonth BucketUnit = "month" // 1 日到月末
)

const (
	// dayBucketMaxDays：期间不超过这么多个自然日就按天 —— 恰好覆盖最长的固定期间（90d）。
	dayBucketMaxDays = 90
	// weekBucketMaxDays：不超过两年（含一个闰日）按周，最多约 105 个点；更长按月，点数不至于挤成一团。
	weekBucketMaxDays = 731
)

// ActivityStats 是一段时间里的作答计数，每次作答都算（含重做）。
type ActivityStats struct {
	AttemptCount int
	CorrectCount int
}

func (a ActivityStats) WrongCount() int { return a.AttemptCount - a.CorrectCount }

// Rate 是正确率（0–100，四舍五入）；没有作答时为 nil。不设最少次数的门槛。
func (a ActivityStats) Rate() *int {
	if a.AttemptCount == 0 {
		return nil
	}
	r := percentRate(a.CorrectCount, a.AttemptCount)
	return &r
}

func (a *ActivityStats) add(b ActivityStats) {
	a.AttemptCount += b.AttemptCount
	a.CorrectCount += b.CorrectCount
}

// ActivityBucket 是一个桶：Start–End 两个日历日（含两端，表示法同 localDate）里的作答。
type ActivityBucket struct {
	Start time.Time
	End   time.Time
	ActivityStats
}

// Activity 是一个题库在一段期间里按桶的作答推移。
type Activity struct {
	Range    ActivityRange
	Unit     BucketUnit
	TimeZone string
	Total    ActivityStats
	// Buckets 从旧到新、连续不断（没作答的桶计数为 0）；最后一个桶含今天。
	Buckets []ActivityBucket
}

// LoadActivity 统计 slug 这个题库在期间 r 里的作答推移。
// 调用方负责确认题库对这个账号可见；r 不是约定的取值 ⇒ ErrUnknownRange。
func (s *service) LoadActivity(ctx context.Context, accountID userid.UserID, slug string, r ActivityRange) (*Activity, error) {
	n, fixed := fixedRangeDays[r]
	if !fixed && r != ActivityRangeAll {
		return nil, ErrUnknownRange
	}
	now := time.Now()
	today := localDate(now)
	var since *time.Time
	if fixed {
		first := today.AddDate(0, 0, -(n - 1))
		since = &first
	}
	daily, err := s.loadDailyAttempts(ctx, accountID, slug, now, since)
	if err != nil {
		return nil, err
	}
	a := buildActivity(r, today, since, daily)
	return &a, nil
}

// loadDailyAttempts 按 studyTZ 的自然日统计作答次数与答对次数；since 为 nil 时不设下界。
// 只按天分组：周、月由 buildActivity 在内存里合并，所有期间共用这一条查询（走 idx_attempt_time）。
func (s *service) loadDailyAttempts(ctx context.Context, accountID userid.UserID, slug string,
	now time.Time, since *time.Time) (map[time.Time]ActivityStats, error) {
	query := `
		SELECT DATE_FORMAT(CONVERT_TZ(a.created_at, '+00:00', ?), '%Y-%m-%d') AS local_day,
		       COUNT(*) AS attempts, SUM(a.correct = 1) AS correct
		FROM attempt a
		JOIN question q ON q.id = a.question_id
		JOIN bank b     ON b.id = q.bank_id AND b.slug = ?
		WHERE a.user_id = ?`
	args := []any{sqlOffset(now), slug, accountID}
	if since != nil {
		query += ` AND a.created_at >= ?`
		args = append(args, dayStartUTC(*since))
	}
	query += ` GROUP BY local_day`

	var rows []struct {
		Day      string `db:"local_day"`
		Attempts int    `db:"attempts"`
		Correct  int    `db:"correct"`
	}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("按天统计作答: %w", err)
	}
	daily := make(map[time.Time]ActivityStats, len(rows))
	for _, r := range rows {
		d, err := time.Parse(time.DateOnly, r.Day)
		if err != nil {
			return nil, fmt.Errorf("解析作答日期 %q: %w", r.Day, err)
		}
		daily[d] = ActivityStats{AttemptCount: r.Attempts, CorrectCount: r.Correct}
	}
	return daily, nil
}

// buildActivity 把按日的计数排成桶：补上没作答的日子，按期间长短合并成周或月，再算合计。
// today 与 daily 的键都是 localDate 的表示法；since 为 nil 表示 all —— 从 daily 里最早的一天起，daily 为空时没有桶。
func buildActivity(r ActivityRange, today time.Time, since *time.Time, daily map[time.Time]ActivityStats) Activity {
	a := Activity{Range: r, Unit: BucketDay, TimeZone: studyTZ.String(), Buckets: []ActivityBucket{}}
	var first time.Time
	switch {
	case since != nil:
		first = *since
	case len(daily) == 0:
		return a
	default:
		first = today
		for d := range daily {
			if d.Before(first) {
				first = d
			}
		}
	}

	a.Unit = bucketUnitFor(daysBetween(first, today) + 1)
	for start := bucketStart(a.Unit, first); !start.After(today); {
		next := nextBucketStart(a.Unit, start)
		a.Buckets = append(a.Buckets, ActivityBucket{Start: start, End: next.AddDate(0, 0, -1)})
		start = next
	}
	for d, st := range daily {
		// 期间外的日子不计：作答时间由数据库的钟写，今天由应用的钟算，两个钟在午夜前后可能差一点
		if d.Before(first) || d.After(today) {
			continue
		}
		i := sort.Search(len(a.Buckets), func(i int) bool { return !a.Buckets[i].End.Before(d) })
		a.Buckets[i].add(st)
		a.Total.add(st)
	}
	return a
}

// bucketUnitFor 按期间跨的自然日数决定桶的长度：图上的点既不太少也不太挤。
func bucketUnitFor(spanDays int) BucketUnit {
	switch {
	case spanDays <= dayBucketMaxDays:
		return BucketDay
	case spanDays <= weekBucketMaxDays:
		return BucketWeek
	default:
		return BucketMonth
	}
}

// bucketStart 是含 d 的那个桶的第一天。
func bucketStart(u BucketUnit, d time.Time) time.Time {
	switch u {
	case BucketWeek:
		return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7)) // 退到周一
	case BucketMonth:
		return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return d
	}
}

// nextBucketStart 是 start 之后那个桶的第一天。start 必须是 bucketStart 的结果（按月时是 1 日，AddDate 不会溢出到下下个月）。
func nextBucketStart(u BucketUnit, start time.Time) time.Time {
	switch u {
	case BucketWeek:
		return start.AddDate(0, 0, 7)
	case BucketMonth:
		return start.AddDate(0, 1, 0)
	default:
		return start.AddDate(0, 0, 1)
	}
}

// daysBetween 两个日历日（localDate 的表示法）之间差几天。
func daysBetween(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}
