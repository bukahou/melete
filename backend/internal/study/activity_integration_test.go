package study

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/question"
)

// 作答推移（契约 Activity）：按 studyTZ 的自然日切、只算指定题库、期间边界、all 按跨度换成按周。
//
// ⭐ 自造题库：⛔ 不借库里已有的题 —— CI 的测试库是空表。
func TestActivityIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	svc := NewService(db, question.NewMySQLRepository(db))

	b := gradeFixture(t, db, 4, `{}`)
	other := gradeFixture(t, db, 1, `{}`) // 它的作答 ⛔ 不能算进 b
	me := mkUser(t, db, fmt.Sprintf("activity-%d", time.Now().UnixNano()%1e9))

	today := localDate(time.Now())
	midnight := dayStartUTC(today) // 东京今天 00:00（UTC）
	at := func(q int64, chosen string, when time.Time) {
		t.Helper()
		res, err := svc.RecordAttempt(ctx, Attempt{AccountID: me, QuestionID: q, Chosen: chosen})
		if err != nil {
			t.Fatalf("记录作答: %v", err)
		}
		setAttemptTime(t, db, res.AttemptID, when)
	}
	noon := func(daysAgo int) time.Time { return midnight.AddDate(0, 0, -daysAgo).Add(12 * time.Hour) }

	// 答 A 为对
	at(b.qs[0], "A", midnight.Add(30*time.Minute))  // 东京今天 00:30（UTC 是昨天）⇒ 今天
	at(b.qs[1], "B", midnight.Add(-30*time.Minute)) // 东京昨天 23:30 ⇒ 昨天
	at(b.qs[2], "A", noon(6))                       // 7d 的第一天
	at(b.qs[3], "B", noon(7))                       // 7d 之外、30d 之内
	at(b.qs[0], "A", noon(120))                     // 只在 all 里；让 all 跨 121 天 ⇒ 按周
	at(other.qs[0], "A", midnight.Add(time.Hour))   // 另一个题库

	load := func(r ActivityRange) *Activity {
		t.Helper()
		a, err := svc.LoadActivity(ctx, me, b.slug, r)
		if err != nil {
			t.Fatalf("%s: %v", r, err)
		}
		return a
	}

	// ① 7d：7 个按天的桶，含今天；昨天 23:30 与今天 00:30 分在两天
	a := load(ActivityRange7d)
	if a.Unit != BucketDay || len(a.Buckets) != 7 || a.TimeZone != "Asia/Tokyo" {
		t.Fatalf("7d 得到 %s × %d（%s），期望 day × 7（Asia/Tokyo）", a.Unit, len(a.Buckets), a.TimeZone)
	}
	d := func(daysAgo int) string { return today.AddDate(0, 0, -daysAgo).Format(time.DateOnly) }
	checkBucket(t, "7d 今天", a.Buckets[6], d(0), d(0), 1, 1, intp(100))
	checkBucket(t, "7d 昨天", a.Buckets[5], d(1), d(1), 1, 0, intp(0))
	checkBucket(t, "7d 第一天", a.Buckets[0], d(6), d(6), 1, 1, intp(100))
	checkBucket(t, "7d 空的一天", a.Buckets[3], d(3), d(3), 0, 0, nil)
	if a.Total != stats(3, 2) {
		t.Fatalf("7d 合计得到 %+v，期望 3 次对 2 次（另一个题库与 7 天前的不算）", a.Total)
	}

	// ② 30d：多出 7 天前那一次
	if a := load(ActivityRange30d); len(a.Buckets) != 30 || a.Total != stats(4, 2) {
		t.Fatalf("30d 得到 %d 个桶、合计 %+v，期望 30 个、4 次对 2 次", len(a.Buckets), a.Total)
	}

	// ③ all：从 120 天前起，跨 121 天 ⇒ 按周；第一周含 120 天前，最后一周含今天
	a = load(ActivityRangeAll)
	if a.Unit != BucketWeek || a.Total != stats(5, 3) {
		t.Fatalf("all 得到 %s、合计 %+v，期望 week、5 次对 3 次", a.Unit, a.Total)
	}
	firstDay, last := today.AddDate(0, 0, -120), a.Buckets[len(a.Buckets)-1]
	if f := a.Buckets[0]; firstDay.Before(f.Start) || firstDay.After(f.End) || f.Start.Weekday() != time.Monday || f.AttemptCount != 1 {
		t.Fatalf("all 的第一周应从周一起、含 %s、1 次作答，得到 %+v", firstDay.Format(time.DateOnly), f)
	}
	if today.Before(last.Start) || today.After(last.End) {
		t.Fatalf("all 的最后一周应含今天，得到 %s–%s", last.Start.Format(time.DateOnly), last.End.Format(time.DateOnly))
	}

	// ④ 不认识的期间 ⇒ ErrUnknownRange（接口层回 400）
	if _, err := svc.LoadActivity(ctx, me, b.slug, "1y"); !errors.Is(err, ErrUnknownRange) {
		t.Fatalf("不认识的期间应返回 ErrUnknownRange，得到 %v", err)
	}

	// ⑤ 这个题库从没作答过的人：all 为空数组，7d 照样 7 个全 0 的桶
	stranger := mkUser(t, db, fmt.Sprintf("activity-none-%d", time.Now().UnixNano()%1e9))
	if a, err := svc.LoadActivity(ctx, stranger, b.slug, ActivityRangeAll); err != nil || len(a.Buckets) != 0 || a.Buckets == nil {
		t.Fatalf("没作答过的 all 应为空数组，得到 %+v（%v）", a, err)
	}
	if a, err := svc.LoadActivity(ctx, stranger, b.slug, ActivityRange7d); err != nil || len(a.Buckets) != 7 || a.Total != (ActivityStats{}) {
		t.Fatalf("没作答过的 7d 应为 7 个全 0 的桶，得到 %+v（%v）", a, err)
	}
}

// setAttemptTime 把一条作答的时间改成 when（created_at 按 UTC 存）。
func setAttemptTime(t *testing.T, db *sqlx.DB, attemptID int64, when time.Time) {
	t.Helper()
	if _, err := db.Exec(`UPDATE attempt SET created_at = ? WHERE id = ?`, when.UTC(), attemptID); err != nil {
		t.Fatal(err)
	}
}
