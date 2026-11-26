package bank

import (
	"encoding/json"
	"fmt"

	"github.com/bukahou/melete/backend/internal/passgrade"
)

// Exam 是 bank.meta 里与正式考试有关的几项，合格判断要用。
// meta 的其余部分只有前端要用，后端不解析。
type Exam struct {
	PassScore *int `json:"passScore"`
	MaxScore  *int `json:"maxScore"`
	// SafetyRate 是题库显式指定的安全线；没有就按 passgrade 的公式算。
	SafetyRate *int `json:"safetyRate"`
	// ExamQuestions 是一场正式考试的题数：做过的题不满这个数之前不给合格等级。
	ExamQuestions *int `json:"examQuestions"`
}

// ParseExam 从 meta 列取出考试参数。meta 为空等于没有考试参数；
// JSON 写坏了是数据错误，返回错误，⛔ 不当作「没有及格线」悄悄跳过合格判断。
func ParseExam(meta *string) (Exam, error) {
	var e Exam
	if meta == nil || *meta == "" {
		return e, nil
	}
	if err := json.Unmarshal([]byte(*meta), &e); err != nil {
		return Exam{}, fmt.Errorf("解析题库元数据里的考试参数: %w", err)
	}
	return e, nil
}

// Scale 返回分档界线；ok 为 false 表示题库没有官方及格线。
func (e Exam) Scale() (passgrade.Scale, bool, error) {
	return passgrade.NewScale(intOrZero(e.PassScore), intOrZero(e.MaxScore), e.SafetyRate)
}

// Questions 是一场正式考试的题数；题库没写时为 0。
func (e Exam) Questions() int { return intOrZero(e.ExamQuestions) }

func intOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
