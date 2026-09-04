package scheduler

import (
	"strings"
	"testing"
)

func ms(v int) *int { return &v }

func TestEffectiveRating(t *testing.T) {
	// ⚠️ 下界写成【绝对常量】，不由 readingCharsPerMinute 算出。
	//
	// 第一版写的是 `floor = stem * 60_000 / readingCharsPerMinute` ——
	// 基准取自被检查的对象，于是把速度从 200 改成 2000 字/分（十倍宽松，
	// 等于规则②基本失效）测试照样全绿。2026-09-04 变异测试实测过。
	// 绝对常量不由被检查项导出，改动它必然有测试变红。
	const stem = 1000     // 题面 1000 字
	const floor = 300_000 // 按 200 字/分读完要 5 分钟

	cases := []struct {
		name    string
		raw     int
		sig     Signals
		want    int
		wantWhy string // 期望理由里含这个子串；"" = 不该有理由
	}{
		// ── 规则① 嘴硬 ────────────────────────────────────────────
		{"答错却自评掌握 → 压到不会", RatingGood,
			Signals{Correct: false, HasReference: true, StemChars: stem}, RatingAgain, "答错却自评"},
		{"答错却自评轻松 → 压到不会", RatingEasy,
			Signals{Correct: false, HasReference: true, StemChars: stem}, RatingAgain, "答错却自评"},

		// ── 诚实的作答一律不动 ────────────────────────────────────
		{"答错且自评不会 → 不纠正", RatingAgain,
			Signals{Correct: false, HasReference: true, StemChars: stem}, RatingAgain, ""},
		{"答错且自评模糊 → 不纠正", RatingHard,
			Signals{Correct: false, HasReference: true, StemChars: stem}, RatingHard, ""},
		{"答对且自评不会（诚实承认蒙对）→ 不纠正，FSRS 自己会缩短间隔", RatingAgain,
			Signals{Correct: true, HasReference: true, DurationMs: ms(floor + 1), StemChars: stem}, RatingAgain, ""},

		// ── 规则② 用时不足 ────────────────────────────────────────
		{"答对但 4 秒读不完题面 → 压到模糊", RatingEasy,
			Signals{Correct: true, HasReference: true, DurationMs: ms(4_000), StemChars: stem}, RatingHard, "读完"},
		{"答对且用时充足 → 不纠正", RatingEasy,
			Signals{Correct: true, HasReference: true, DurationMs: ms(floor), StemChars: stem}, RatingEasy, ""},
		// ⚠️ 这条必须用 raw=4 且 HasReference=false 才有鉴别力：
		// raw 取 2 会与规则②的上限相等（观察不到差别），HasReference=true
		// 又会让规则①先把它压到 1 —— 两种写法都会让「规则②漏判答错」的
		// 缺陷从测试里溜过去。2026-09-04 变异测试实测过这一点。
		{"答【错】而用时短 → 规则②不适用（它只管答对的情况）", RatingEasy,
			Signals{Correct: false, HasReference: false, DurationMs: ms(1_000), StemChars: stem}, RatingEasy, ""},

		// ── 规则③ 无参考答案的 6 道题 ─────────────────────────────
		{"无参考答案时 correct 恒为 false，⛔ 不得据此判嘴硬", RatingEasy,
			Signals{Correct: false, HasReference: false, StemChars: stem}, RatingEasy, ""},

		// ── 缺数据时的退化 ────────────────────────────────────────
		{"没有用时数据 → 规则②跳过", RatingEasy,
			Signals{Correct: true, HasReference: true, DurationMs: nil, StemChars: stem}, RatingEasy, ""},
		{"题面字数为 0（数据脏）→ 规则②跳过，不拿 0 当下界", RatingEasy,
			Signals{Correct: true, HasReference: true, DurationMs: ms(1), StemChars: 0}, RatingEasy, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EffectiveRating(c.raw, c.sig)
			if got.Effective != c.want {
				t.Errorf("有效评分 = %d，期望 %d（理由 %v）", got.Effective, c.want, got.Reasons)
			}
			if got.Raw != c.raw {
				t.Errorf("⛔ Raw 被改成了 %d —— 原始自评必须原样保留", got.Raw)
			}
			joined := strings.Join(got.Reasons, " | ")
			if c.wantWhy == "" && len(got.Reasons) != 0 {
				t.Errorf("不该有纠正理由，却给了 %q", joined)
			}
			if c.wantWhy != "" && !strings.Contains(joined, c.wantWhy) {
				t.Errorf("理由里没有 %q，实际是 %q", c.wantWhy, joined)
			}
		})
	}
}

// 只降不升是贯穿全篇的规则 —— 穷举所有输入组合验证它，
// 而不是挑几个例子。规则叠加时最容易出的错就是某条把评分抬回去。
func TestEffectiveRatingNeverRaises(t *testing.T) {
	for raw := RatingAgain; raw <= RatingEasy; raw++ {
		for _, correct := range []bool{true, false} {
			for _, hasRef := range []bool{true, false} {
				for _, d := range []*int{nil, ms(0), ms(1_000), ms(600_000)} {
					for _, chars := range []int{0, 1, 1000, 100_000} {
						got := EffectiveRating(raw, Signals{
							Correct: correct, HasReference: hasRef, DurationMs: d, StemChars: chars,
						})
						if got.Effective > raw {
							t.Fatalf("⛔ 评分被抬高了：raw=%d → %d（correct=%v hasRef=%v d=%v chars=%d）",
								raw, got.Effective, correct, hasRef, d, chars)
						}
						if got.Effective < RatingAgain {
							t.Fatalf("⛔ 评分掉出合法区间：%d", got.Effective)
						}
					}
				}
			}
		}
	}
}

// minReadMs 的具体数值单独钉死 —— 这是规则②唯一的调参点，
// 改它等于改变整条规则的松紧，必须有测试挡着。
func TestMinReadMs(t *testing.T) {
	for _, c := range []struct{ chars, wantMs int }{
		{200, 60_000},   // 200 字 = 1 分钟
		{1000, 300_000}, // 1000 字 = 5 分钟
		{1474, 442_200}, // SAP 平均题面（题干 597 + 选项 877）≈ 7.4 分钟
		{394, 118_200},  // SAA 平均题面（题干 147 + 选项 247）≈ 2 分钟
	} {
		if got := minReadMs(c.chars); got != c.wantMs {
			t.Errorf("minReadMs(%d) = %d，期望 %d —— 读题速度常量被改了？", c.chars, got, c.wantMs)
		}
	}
}
