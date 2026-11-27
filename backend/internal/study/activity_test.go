package study

import (
	"testing"
	"time"
)

// 作答推移的分桶（纯函数，不碰数据库）：补空、按天 / 周 / 月合并、合计、期间边界。

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

func stats(attempts, correct int) ActivityStats {
	return ActivityStats{AttemptCount: attempts, CorrectCount: correct}
}

func checkBucket(t *testing.T, step string, b ActivityBucket, start, end string, attempts, correct int, rate *int) {
	t.Helper()
	if !b.Start.Equal(day(start)) || !b.End.Equal(day(end)) {
		t.Fatalf("%s: 桶 %s–%s，期望 %s–%s", step, b.Start.Format(time.DateOnly), b.End.Format(time.DateOnly), start, end)
	}
	if b.AttemptCount != attempts || b.CorrectCount != correct || b.WrongCount() != attempts-correct || !sameIntPtr(b.Rate(), rate) {
		t.Fatalf("%s: 桶 %s 得到 {次数 %d, 对 %d, 正确率 %s}，期望 {%d, %d, %s}", step, start,
			b.AttemptCount, b.CorrectCount, fmtIntPtr(b.Rate()), attempts, correct, fmtIntPtr(rate))
	}
}

func TestBuildActivityFixedRangeIsDailyAndZeroFilled(t *testing.T) {
	today := day("2026-11-25") // 周三
	since := today.AddDate(0, 0, -6)
	daily := map[time.Time]ActivityStats{
		day("2026-11-19"): stats(3, 2),
		day("2026-11-22"): stats(1, 0),
		day("2026-11-25"): stats(4, 4),
	}
	a := buildActivity(ActivityRange7d, today, &since, daily)
	if a.Unit != BucketDay || a.Range != ActivityRange7d || a.TimeZone != "Asia/Tokyo" {
		t.Fatalf("得到 {%s, %s, %s}，期望 {day, 7d, Asia/Tokyo}", a.Unit, a.Range, a.TimeZone)
	}
	if len(a.Buckets) != 7 {
		t.Fatalf("7d 应有 7 个桶（含今天），得到 %d", len(a.Buckets))
	}
	checkBucket(t, "第一天", a.Buckets[0], "2026-11-19", "2026-11-19", 3, 2, intp(67))
	checkBucket(t, "空的一天", a.Buckets[1], "2026-11-20", "2026-11-20", 0, 0, nil)
	checkBucket(t, "全错的一天", a.Buckets[3], "2026-11-22", "2026-11-22", 1, 0, intp(0))
	checkBucket(t, "今天", a.Buckets[6], "2026-11-25", "2026-11-25", 4, 4, intp(100))
	if a.Total != stats(8, 6) || *a.Total.Rate() != 75 {
		t.Fatalf("合计得到 %+v，期望 8 次对 6 次（75%%）", a.Total)
	}
}

func TestBuildActivityIgnoresDaysOutsideRange(t *testing.T) {
	today := day("2026-11-25")
	since := today.AddDate(0, 0, -6)
	daily := map[time.Time]ActivityStats{
		day("2026-11-18"): stats(5, 5), // 期间前一天
		day("2026-11-26"): stats(2, 2), // 应用的钟还没到、数据库的钟已过午夜
		day("2026-11-24"): stats(1, 1),
	}
	a := buildActivity(ActivityRange7d, today, &since, daily)
	if a.Total != stats(1, 1) {
		t.Fatalf("期间外的日子不应计入，合计得到 %+v", a.Total)
	}
}

func TestBuildActivityAllWithoutAttemptsIsEmpty(t *testing.T) {
	a := buildActivity(ActivityRangeAll, day("2026-11-25"), nil, map[time.Time]ActivityStats{})
	if a.Buckets == nil || len(a.Buckets) != 0 {
		t.Fatalf("从没作答过时 buckets 应为空数组（不是 nil），得到 %#v", a.Buckets)
	}
	if a.Total != (ActivityStats{}) || a.Total.Rate() != nil {
		t.Fatalf("合计应全为 0，得到 %+v", a.Total)
	}
}

func TestBuildActivityAllStartsAtFirstAttempt(t *testing.T) {
	today := day("2026-11-25")
	daily := map[time.Time]ActivityStats{
		day("2026-11-23"): stats(2, 1),
		day("2026-11-25"): stats(1, 1),
	}
	a := buildActivity(ActivityRangeAll, today, nil, daily)
	if a.Unit != BucketDay || len(a.Buckets) != 3 {
		t.Fatalf("跨 3 天应按天 3 个桶，得到 %s × %d", a.Unit, len(a.Buckets))
	}
	checkBucket(t, "第一次作答那天", a.Buckets[0], "2026-11-23", "2026-11-23", 2, 1, intp(50))
}

func TestBuildActivityAllByWeekStartsOnMonday(t *testing.T) {
	today := day("2026-11-25") // 周三
	first := day("2026-08-16") // 周日，距今天 101 天 ⇒ 按周
	daily := map[time.Time]ActivityStats{
		first:             stats(2, 2),
		day("2026-08-17"): stats(3, 0), // 周一：下一周
		day("2026-11-23"): stats(1, 1), // 本周一
		today:             stats(4, 3),
	}
	a := buildActivity(ActivityRangeAll, today, nil, daily)
	if a.Unit != BucketWeek {
		t.Fatalf("跨 102 天应按周，得到 %s", a.Unit)
	}
	checkBucket(t, "第一周", a.Buckets[0], "2026-08-10", "2026-08-16", 2, 2, intp(100))
	checkBucket(t, "第二周", a.Buckets[1], "2026-08-17", "2026-08-23", 3, 0, intp(0))
	last := a.Buckets[len(a.Buckets)-1]
	checkBucket(t, "本周（endDate 晚于今天）", last, "2026-11-23", "2026-11-29", 5, 4, intp(80))
	for i := 1; i < len(a.Buckets); i++ {
		if !a.Buckets[i].Start.Equal(a.Buckets[i-1].End.AddDate(0, 0, 1)) {
			t.Fatalf("桶 %d 与前一个不连续", i)
		}
	}
	if a.Total != stats(10, 6) {
		t.Fatalf("合计得到 %+v，期望 10 次对 6 次", a.Total)
	}
}

func TestBuildActivityAllByMonth(t *testing.T) {
	today := day("2026-11-25")
	daily := map[time.Time]ActivityStats{
		day("2023-02-15"): stats(1, 1),
		day("2024-02-29"): stats(2, 1),
	}
	a := buildActivity(ActivityRangeAll, today, nil, daily)
	if a.Unit != BucketMonth {
		t.Fatalf("跨三年多应按月，得到 %s", a.Unit)
	}
	checkBucket(t, "第一个月", a.Buckets[0], "2023-02-01", "2023-02-28", 1, 1, intp(100))
	checkBucket(t, "闰年二月", a.Buckets[12], "2024-02-01", "2024-02-29", 2, 1, intp(50))
	checkBucket(t, "本月", a.Buckets[len(a.Buckets)-1], "2026-11-01", "2026-11-30", 0, 0, nil)
	if len(a.Buckets) != 46 { // 2023-02 … 2026-11
		t.Fatalf("应有 46 个月，得到 %d", len(a.Buckets))
	}
}

func TestBucketUnitThresholds(t *testing.T) {
	for _, c := range []struct {
		span int
		want BucketUnit
	}{{1, BucketDay}, {90, BucketDay}, {91, BucketWeek}, {731, BucketWeek}, {732, BucketMonth}} {
		if got := bucketUnitFor(c.span); got != c.want {
			t.Fatalf("跨 %d 天应为 %s，得到 %s", c.span, c.want, got)
		}
	}
	// 三个固定期间都按天
	for r, n := range fixedRangeDays {
		if bucketUnitFor(n) != BucketDay {
			t.Fatalf("%s 应按天", r)
		}
	}
}

func TestPercentRateRoundsHalfUp(t *testing.T) {
	for _, c := range []struct{ correct, attempts, want int }{{2, 3, 67}, {1, 8, 13}, {1, 3, 33}, {0, 5, 0}, {5, 5, 100}} {
		if got := percentRate(c.correct, c.attempts); got != c.want {
			t.Fatalf("%d/%d 应为 %d，得到 %d", c.correct, c.attempts, c.want, got)
		}
	}
}

func TestStudyTimeZoneHelpers(t *testing.T) {
	if got := sqlOffset(time.Now()); got != "+09:00" {
		t.Fatalf("Asia/Tokyo 的偏移应为 +09:00，得到 %s", got)
	}
	// 东京 11-27 00:30 = UTC 11-26 15:30：自然日是 27 号
	if got := localDate(time.Date(2026, 11, 26, 15, 30, 0, 0, time.UTC)); !got.Equal(day("2026-11-27")) {
		t.Fatalf("自然日应为 2026-11-27，得到 %s", got.Format(time.DateOnly))
	}
	if got := dayStartUTC(day("2026-11-27")); !got.Equal(time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("东京 11-27 00:00 应为 UTC 11-26 15:00，得到 %s", got)
	}
}
