package scheduler

import (
	"testing"
	"time"
)

var base = time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)

// 一条完整的学习轨迹。
//
// ⚠️ 断言的是【我们依赖的性质】，不是把库的输出数值抄下来当期望值：
// 抄数值的测试在升级 go-fsrs 时必然变红，而红了也说明不了有没有真问题；
// 性质（learning 在当天内、review 跨天、间隔递增、失败使稳定性坍塌）
// 是 FSRS 的定义本身，换版本也该成立。
//
// 第一版写成「间隔应等于 0 天」，而 learning 阶段的间隔是【分钟级】——
// 失败信息打出「间隔 = 0.0 天，期望 0.0 天」，两个值印出来一模一样，
// 根本读不出为什么红。⚠️ 断言的输出精度必须足以区分被比较的两个值。
func TestTypicalTrajectory(t *testing.T) {
	s := New()
	c := NewCard()
	now := base

	steps := []struct {
		rating    int
		wantState State
		sameDay   bool // true = 当天内再来（learning/relearning），false = 跨天
	}{
		{RatingGood, StateLearning, true},
		{RatingGood, StateReview, false},
		{RatingGood, StateReview, false},
		{RatingAgain, StateRelearning, true}, // ⭐ 失败 → 当天拉回，稳定性坍塌
		{RatingGood, StateReview, false},
	}

	var prevStability, prevReviewInterval float64
	for i, st := range steps {
		prev := c
		c = s.Next(c, st.rating, now)
		gap := c.Due.Sub(now)

		if c.State != st.wantState {
			t.Errorf("第%d次: 状态 = %v，期望 %v", i+1, c.State, st.wantState)
		}
		// ⚠️ 用 Duration 直接比，不换算成天再用 %.1f 打印
		if st.sameDay && gap >= 24*time.Hour {
			t.Errorf("第%d次: %v 状态的间隔应在当天内，实际 %v", i+1, c.State, gap)
		}
		if !st.sameDay && gap < 24*time.Hour {
			t.Errorf("第%d次: review 的间隔应跨天，实际 %v", i+1, gap)
		}
		if c.Reps != prev.Reps+1 {
			t.Errorf("第%d次: reps 没有递增（%d → %d）", i+1, prev.Reps, c.Reps)
		}
		t.Logf("第%d次 评分=%d  %v→%-10v S=%6.2f D=%.2f  间隔=%v",
			i+1, st.rating, prev.State, c.State, c.Stability, c.Difficulty, gap)

		// ⭐ 第 4 次是本项目关心的那一刻：一次失败让稳定性大幅坍塌。
		// 「嘴硬」把它按成 Good 就不会发生 —— 那道题会两周后才回来，而你其实不会。
		if st.rating == RatingAgain {
			if c.Stability >= prevStability {
				t.Errorf("第%d次: 答「不会」后稳定性没有下降（%.2f → %.2f）",
					i+1, prevStability, c.Stability)
			}
			if c.Lapses != prev.Lapses+1 {
				t.Errorf("第%d次: lapses 没有递增", i+1)
			}
		}
		// 连续答对时，review 的间隔必须一次比一次长 —— 这是间隔重复的全部意义。
		if st.wantState == StateReview && st.rating == RatingGood && prev.State == StateReview {
			if d := gap.Hours() / 24; d <= prevReviewInterval {
				t.Errorf("第%d次: 连续答对但间隔没变长（%.1f → %.1f 天）", i+1, prevReviewInterval, d)
			}
		}
		if c.State == StateReview {
			prevReviewInterval = gap.Hours() / 24
		}
		prevStability = c.Stability
		now = c.Due
	}
}

// ⭐ 「嘴硬」到底让多少调度失真 —— 同一串作答，只有第 4 次的评分不同。
// 这个测试的价值不在断言，在于它把代价量化了：如果没有 EffectiveRating，
// 一次谎报会把复习推迟多久。
func TestLyingDelaysReview(t *testing.T) {
	run := func(fourth int) (time.Time, float64) {
		s := New()
		c := NewCard()
		now := base
		for _, r := range []int{RatingGood, RatingGood, RatingGood, fourth} {
			c = s.Next(c, r, now)
			now = c.Due
		}
		return c.Due, c.Stability
	}

	honestDue, honestS := run(RatingAgain) // 诚实：不会
	lyingDue, lyingS := run(RatingGood)    // 嘴硬：明明答错却按掌握

	delay := lyingDue.Sub(honestDue)
	t.Logf("诚实按「不会」→ 下次 %s（S=%.2f）", honestDue.Format("2006-01-02"), honestS)
	t.Logf("嘴硬按「掌握」→ 下次 %s（S=%.2f）", lyingDue.Format("2006-01-02"), lyingS)
	t.Logf("⭐ 一次嘴硬把复习推迟了 %.0f 天，稳定性虚高 %.1f 倍",
		delay.Hours()/24, lyingS/honestS)

	if delay <= 0 {
		t.Fatalf("嘴硬没有推迟复习？延迟 = %v —— 那 EffectiveRating 就没有意义了", delay)
	}
	if lyingS <= honestS {
		t.Errorf("嘴硬后稳定性没有虚高（%.2f vs %.2f）", lyingS, honestS)
	}
}

// 新卡的零值必须能安全地喂进调度 —— card 表里没有行时就是这个状态。
func TestNewCardIsSchedulable(t *testing.T) {
	s := New()
	c := NewCard()
	if c.State != StateNew {
		t.Fatalf("新卡状态 = %v，期望 new", c.State)
	}
	if !c.LastReview.IsZero() {
		t.Errorf("新卡的 LastReview 应为零值，实际 %v", c.LastReview)
	}
	next := s.Next(c, RatingGood, base)
	if next.State == StateNew {
		t.Errorf("做过一次之后不该还是 new")
	}
	if next.Due.Before(base) {
		t.Errorf("下次到期时间 %v 早于作答时间 %v", next.Due, base)
	}
}

// 隔了很久才回来复习，间隔应当按【实际经过的时间】算而不是按计划的时间。
// 这正是 FSRS 相对 SM-2 的改进之一，也是我们不持久化 ElapsedDays 的理由 ——
// 它每次都从 LastReview 现算。
func TestLateReviewUsesActualElapsed(t *testing.T) {
	s := New()
	c := NewCard()
	c = s.Next(c, RatingGood, base)
	c = s.Next(c, RatingGood, c.Due) // 进入 review

	onTime := s.Next(c, RatingGood, c.Due)
	late := s.Next(c, RatingGood, c.Due.AddDate(0, 0, 60)) // 迟到 60 天

	t.Logf("准时复习 → S=%.2f", onTime.Stability)
	t.Logf("迟到60天 → S=%.2f", late.Stability)
	if late.Stability <= onTime.Stability {
		t.Errorf("隔更久才想起来，稳定性应更高（间隔效应）：迟到 %.2f vs 准时 %.2f",
			late.Stability, onTime.Stability)
	}
}
