// Package passgrade 是「现在去考能不能过」的分档规则：A–D 四档的界线，以及什么时候才给档。
//
// 这套规则只在这里有一份。学习进度的合格等级、考纲域的等级、题库元数据里的安全线
// 都从这里取，客户端只负责显示 —— 两端各写一份分档逻辑，迟早会给同一个人两种结论。
package passgrade

import (
	"errors"
	"fmt"
	"math"
)

// Grade 是合格等级。
type Grade string

const (
	GradeA Grade = "A" // ≥ 安全线
	GradeB Grade = "B" // ≥ 合格线，< 安全线
	GradeC Grade = "C" // ≥ 合格线 − FloorMargin，< 合格线
	GradeD Grade = "D" // 其余
)

const (
	// SafetyMargin 是安全线高出合格线的点数。
	// 官方分数经过加权和标定，素朴的正确率只是它的近似，卡在合格线上的正确率不等于能过；
	// 多出来的这几个点是给这层近似留的余量。题库可以在元数据里显式指定安全线来覆盖。
	SafetyMargin = 10
	// FloorMargin 是 C 档下限低于合格线的点数：差得不多的（C）与差得远的（D）分开。
	FloorMargin = 15
	// DomainMinSeen 是考纲域给等级前至少要做过的题数（去重）。
	// 几道题的正确率起伏太大，按它分档会误导。
	DomainMinSeen = 10
)

// ErrInvalidExam 表示题库的合格线配置自相矛盾，例如安全线低于合格线。
var ErrInvalidExam = errors.New("题库的合格线配置不合理")

// Scale 是一个题库的分档界线，单位是正确率的百分点。
type Scale struct {
	Pass   int // 合格线：官方及格分换算成正确率
	Safety int // 安全线
	Floor  int // C 档的下限
}

// NewScale 由官方及格分、满分，以及题库显式指定的安全线（可为 nil）算出分档界线。
// ok 为 false 表示题库没有官方及格线，不做合格判断。
func NewScale(passScore, maxScore int, safetyOverride *int) (scale Scale, ok bool, err error) {
	if passScore <= 0 || maxScore <= 0 {
		return Scale{}, false, nil
	}
	pass := int(math.Round(float64(passScore) * 100 / float64(maxScore)))
	if pass > 100 {
		return Scale{}, false, fmt.Errorf("%w：及格分 %d 高于满分 %d", ErrInvalidExam, passScore, maxScore)
	}
	safety := min(pass+SafetyMargin, 100)
	if safetyOverride != nil {
		safety = *safetyOverride
		if safety < pass || safety > 100 {
			return Scale{}, false, fmt.Errorf("%w：安全线 %d%% 不在合格线 %d%% 与 100%% 之间", ErrInvalidExam, safety, pass)
		}
	}
	return Scale{Pass: pass, Safety: safety, Floor: max(pass-FloorMargin, 0)}, true, nil
}

// GradeOf 按「做对几道 / 做过几道」分档。total 须大于 0。
//
// 用整数比较而不先算出百分比：81.6% 若先四舍五入成 82，就会被错升到 A 档。
func (s Scale) GradeOf(correct, total int) Grade {
	switch {
	case correct*100 >= s.Safety*total:
		return GradeA
	case correct*100 >= s.Pass*total:
		return GradeB
	case correct*100 >= s.Floor*total:
		return GradeC
	default:
		return GradeD
	}
}

// Band 是一档的正确率范围：含 Min，不含 Max；Max 为 nil 表示没有上限。
type Band struct {
	Grade Grade
	Min   int
	Max   *int
}

// Bands 按 A → D 返回四档的范围，供界面直接渲染「等级说明」。
func (s Scale) Bands() []Band {
	safety, pass, floor := s.Safety, s.Pass, s.Floor
	return []Band{
		{Grade: GradeA, Min: safety},
		{Grade: GradeB, Min: pass, Max: &safety},
		{Grade: GradeC, Min: floor, Max: &pass},
		{Grade: GradeD, Min: 0, Max: &floor},
	}
}

// Judgement 是一个题库整体的合格判断。
type Judgement struct {
	Grade     *Grade // nil = 做过的题还不够，暂不给等级
	Remaining int    // 还要再做几道（去重）才给等级；0 表示已给出
	Bands     []Band
}

// Judge 用当前掌握率（每题只看最近一次：correct / seen）给整个题库分档。
//
// 做过的题不满一场考试的题数之前不给等级：只做了几道就判 A，这个结论没有意义。
// examQuestions 为 0（题库没写）时，做过一道就给。
func (s Scale) Judge(seen, correct, examQuestions int) Judgement {
	need := max(examQuestions, 1)
	j := Judgement{Remaining: max(need-seen, 0), Bands: s.Bands()}
	if j.Remaining == 0 {
		g := s.GradeOf(correct, seen)
		j.Grade = &g
	}
	return j
}

// DomainGrade 给一个考纲域分档；做过的题少于 DomainMinSeen 时返回 nil（不给等级）。
func (s Scale) DomainGrade(correct, seen int) *Grade {
	if seen < DomainMinSeen {
		return nil
	}
	g := s.GradeOf(correct, seen)
	return &g
}
