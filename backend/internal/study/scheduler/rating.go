// Package scheduler 把「一次作答」翻译成「下次什么时候再来」。
//
// 它由两层组成，职责严格分开：
//
//	rating.go     自评的【可信度纠正】 —— 本项目特有，纯函数
//	scheduler.go  FSRS 记忆模型        —— 官方实现，原样调用
//
// 为什么要分开：FSRS 那 19 个权重是在标准 1-4 评分上拟合出来的。
// 改动模型本身会让全部权重失效，所以纠正必须发生在【评分进入 FSRS 之前】，
// 而 FSRS 那一层一个字不动。
package scheduler

import (
	"fmt"

	fsrs "github.com/open-spaced-repetition/go-fsrs/v3"
)

// 自评四键，与前端 DrillCard 的 RATINGS 一一对应。
//
// 语义按 learning-flows.md 2026-09-01 已定：rating 是**回忆质量的主观陈述**，
// 客观对错由 attempt.correct 独立记录 —— 两者并存，矛盾本身就是信号。
const (
	RatingAgain = int(fsrs.Again) // 1 不会
	RatingHard  = int(fsrs.Hard)  // 2 模糊
	RatingGood  = int(fsrs.Good)  // 3 掌握
	RatingEasy  = int(fsrs.Easy)  // 4 轻松
)

// scanCharsPerMinute 是「至少扫了一眼题面」的速度上界，用来判断
// 「这个用时根本【看都没看】题面」。
//
// ⚠️ 2026-09-04 订正了一个方向性错误。原值 200 字/分，注释写的是
// 「保守取值，宁可漏判也不误判」—— 方向反了：
//
//	字/分取得越【低】→ 要求的时间越【长】→ 误判越【多】
//
// 开发库里唯一一条真实作答当场证伪了它：359 字的题面用时 30.3 秒
// （= 711 字/分，完全正常的中文阅读速度），却被规则②判成「读不完」。
// ⚠️ 而全部单测都没抓到 —— 测试里的数值和这个常量出自同一个错误假设，
// 一起错就一起对。这是本项目反复出现的那个形状的又一例。
//
// 现在的口径不是「读完」而是「有没有看」：这条规则要抓的是 2-4 秒的作答，
// 不是「读得比我想象的快」。1200 字/分 = 20 字/秒，快到不可能是在阅读，
// 于是：SAA 平均题面 394 字 → 20 秒，SAP 平均 1474 字 → 74 秒。
//
// ⚠️ 它仍然不是从数据测出来的。等真实作答积累起来，应该用
// 【答对且自评 ≥3】那批作答的用时分布回头校准 —— 那批人最可能真读了题。
// 校准之前不要把它当成实测结论。
const scanCharsPerMinute = 1200

// Correction 是一次评分纠正的完整说明。
//
// ⚠️ Raw 永远保留用户按下的那个键 —— attempt.rating 存的也是它。
// 纠正是【调度时现算】的推导结论，不覆盖原始事实：规则改了可以整批重算，
// 原始的覆盖掉就回不来了。这与 answer_claim 不合并三方主张是同一条纪律。
type Correction struct {
	Raw       int      // 用户按的键
	Effective int      // 实际喂给 FSRS 的
	Reasons   []string // 为什么降 —— 要显示给用户看，不偷偷改
}

// Adjusted 是否发生了纠正。
func (c Correction) Adjusted() bool { return c.Effective != c.Raw }

// Signals 是纠正所需的全部输入。
//
// HasReference 为 false 时（题库里 6 道题一条答案主张都没有），Correct 恒为 false
// 而那不代表答错 —— 判不了对错就不该拿对错说事，规则 ① 必须跳过。
type Signals struct {
	Correct      bool
	HasReference bool
	DurationMs   *int // 可能没有（旧记录 / 客户端没报）
	// StemChars 题面字数（题干 + 全部选项）。用来算「最低读完时间」。
	StemChars int
}

// EffectiveRating 把用户的自评纠正成实际用于调度的评分。
//
// ⭐ 贯穿全篇的一条规则：**只降不升**。
//
//	effective = min(raw, 各条规则的上限...)
//
// 只降不升让行为可预测（叠加多条规则不会互相抬高）、可解释（每次降都有理由）、
// 也让「纠正」这件事在语义上只有一个方向：我们只怀疑「你高估了自己」，
// 从不替用户宣称「你其实比你想的更熟」。
func EffectiveRating(raw int, s Signals) Correction {
	c := Correction{Raw: raw, Effective: raw}

	cap := func(limit int, reason string) {
		if c.Effective > limit {
			c.Effective = limit
			c.Reasons = append(c.Reasons, reason)
		}
	}

	// ① 嘴硬：答错却自评「掌握 / 轻松」。
	//
	// 这一条之所以硬，是因为本项目的交互顺序是【作答 → 揭晓 → 自评】——
	// 用户是在看到正确答案和解析【之后】才按的键。所以不存在
	// 「我只是没算准」这种解释空间：答案就摆在眼前。
	//
	// 压到 1 而不是 2：四选一里「错」的信息量很足，而 FSRS 的 Again
	// 正是「这次没回忆出来」的意思 —— 答错就是没回忆出来。
	if s.HasReference && !s.Correct && raw >= RatingGood {
		cap(RatingAgain, "答错却自评「掌握」—— 按「不会」安排复习")
	}

	// ② 证据不足：答对，但用时短到读不完题面。
	//
	// 压到 2 不压到 1：毕竟答对了，只是这次答对提供不了「你会」的证据。
	// 判成彻底失败会让正常的快速复习（真的熟了）被误伤。
	if s.Correct && s.DurationMs != nil && s.StemChars > 0 {
		if floor := minReadMs(s.StemChars); *s.DurationMs < floor {
			cap(RatingHard, fmt.Sprintf("用时 %.1f 秒，%d 字的题面扫一眼也要 %.0f 秒",
				float64(*s.DurationMs)/1000, s.StemChars, float64(floor)/1000))
		}
	}

	return c
}

// minReadMs 扫过 n 个字所需时间的下界（毫秒）。
func minReadMs(chars int) int {
	return chars * 60_000 / scanCharsPerMinute
}
