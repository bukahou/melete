package httpapi

import (
	"database/sql"
	"testing"

	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/study"
)

// 标签在各端点用同一个构造处挑显示名：题目详情、专项进度、会话记录给出的名字必须一致。
func TestTagNameConsistentAcrossEndpoints(t *testing.T) {
	names := `{"en":"Design Secure Architectures","zh":"设计安全架构"}`
	qt := toAPIQuestionTag(question.Tag{ID: 7, Type: "domain", Value: "domain-1", I18n: &names}, "ja")
	ss := toAPISession(study.Session{Mode: "tag", Tag: &study.TagRef{
		ID: 7, Type: "domain", Value: "domain-1", I18n: sql.NullString{String: names, Valid: true},
	}}, "ja")
	if qt.Name != "Design Secure Architectures" || qt.Localized {
		t.Errorf("题目详情的标签 = (%q, %v)，期望回退英文并标出", qt.Name, qt.Localized)
	}
	if ss.Tag == nil || ss.Tag.Name != qt.Name || ss.Tag.Localized != qt.Localized {
		t.Errorf("会话记录的标签 = %+v，应与题目详情一致", ss.Tag)
	}
	plain := toAPIQuestionTag(question.Tag{ID: 8, Type: "topic", Value: "API Gateway"}, "ja")
	if plain.Name != "API Gateway" || !plain.Localized || plain.I18n != nil {
		t.Errorf("没有多语言名字的标签 = %+v，name 应为 value 且 localized=true", plain)
	}
}

func TestClaimRationaleLanguage(t *testing.T) {
	text := "中文分歧说明"
	with := toAPIClaim(question.Claim{Source: "ai_verdict", Answer: "B", Rationale: &text,
		RationaleLocale: "zh", RationaleLocalized: false})
	if with.RationaleLocale == nil || *with.RationaleLocale != "zh" || with.RationaleLocalized == nil || *with.RationaleLocalized {
		t.Errorf("有理由时应带语言与 localized=false，得到 %+v", with)
	}
	without := toAPIClaim(question.Claim{Source: "bank_label", Answer: "A"})
	if without.RationaleLocale != nil || without.RationaleLocalized != nil {
		t.Errorf("没有理由时不应带语言标记，得到 %+v", without)
	}
}
