package question

import "testing"

func TestPickChoiceExplanations(t *testing.T) {
	rows := []choiceExplanationRow{
		{Label: "A", Source: "ai", Locale: "zh", Body: "A 中文"},
		{Label: "A", Source: "ai", Locale: "ja", Body: "A 日本語"},
		{Label: "B", Source: "ai", Locale: "zh", Body: "B 中文"},
		{Label: "B", Source: "editor", Locale: "zh", Body: "B 人工订正"},
		{Label: "C", Source: "ai", Locale: "en", Body: "C English"},
		{Label: "C", Source: "ai", Locale: "zh", Body: "C 中文"},
	}
	cases := []struct {
		name, locale string
		want         map[string]PickedText
	}{
		{"请求日文：有就用，没有回退到题库语言并标出", "ja", map[string]PickedText{
			"A": {Source: "ai", Locale: "ja", Body: "A 日本語", Localized: true},
			"B": {Source: "editor", Locale: "zh", Body: "B 人工订正", Localized: false},
			"C": {Source: "ai", Locale: "zh", Body: "C 中文", Localized: false},
		}},
		{"请求题库语言", "zh", map[string]PickedText{
			"A": {Source: "ai", Locale: "zh", Body: "A 中文", Localized: true},
			"B": {Source: "editor", Locale: "zh", Body: "B 人工订正", Localized: true},
			"C": {Source: "ai", Locale: "zh", Body: "C 中文", Localized: true},
		}},
		{"没有指定语言：用题库语言，不算回退", "", map[string]PickedText{
			"A": {Source: "ai", Locale: "zh", Body: "A 中文", Localized: true},
			"B": {Source: "editor", Locale: "zh", Body: "B 人工订正", Localized: true},
			"C": {Source: "ai", Locale: "zh", Body: "C 中文", Localized: true},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pickChoiceExplanations(rows, c.locale, "zh")
			if len(got) != len(c.want) {
				t.Fatalf("得到 %d 个选项，期望 %d", len(got), len(c.want))
			}
			for label, want := range c.want {
				if got[label] != want {
					t.Errorf("%s = %+v，期望 %+v", label, got[label], want)
				}
			}
		})
	}
}

func TestPickExplanation(t *testing.T) {
	all := []Explanation{
		{Source: "ai", Locale: "zh", Body: "中文解析"},
		{Source: "editor", Locale: "zh", Body: "人工订正的中文解析"},
		{Source: "ai", Locale: "en", Body: "English"},
	}
	cases := []struct {
		name, locale string
		want         *PickedText
	}{
		{"请求英文：有就用", "en", &PickedText{Source: "ai", Locale: "en", Body: "English", Localized: true}},
		{"请求日文：没有，回退题库语言、人工订正优先并标出", "ja",
			&PickedText{Source: "editor", Locale: "zh", Body: "人工订正的中文解析", Localized: false}},
		{"没有指定语言：题库语言，不算回退", "", &PickedText{Source: "editor", Locale: "zh", Body: "人工订正的中文解析", Localized: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pickExplanation(all, c.locale, "zh")
			if got == nil || *got != *c.want {
				t.Errorf("得到 %+v，期望 %+v", got, c.want)
			}
		})
	}
	if got := pickExplanation(nil, "ja", "zh"); got != nil {
		t.Errorf("没有解析时应为 nil，得到 %+v", got)
	}
	// 题库语言也没有：按语言代码取最小的那一份
	if got := pickExplanation([]Explanation{{Source: "ai", Locale: "ko", Body: "한국어"}, {Source: "ai", Locale: "en", Body: "English"}}, "ja", "zh"); got == nil || got.Locale != "en" || got.Localized {
		t.Errorf("得到 %+v，期望回退到 en 且 localized=false", got)
	}
}

func TestMarkRationaleLocale(t *testing.T) {
	text := "理由"
	cases := []struct {
		name, locale, queried string
		rationale             *string
		wantLang              string
		wantLocalized         bool
	}{
		{"用了译文", "ja", "ja", &text, "ja", true},
		{"没有译文：原文是题库语言，标出回退", "ja", "", &text, "zh", false},
		{"没有指定语言", "", "", &text, "zh", true},
		{"没有理由：零值", "ja", "", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl := Claim{Rationale: c.rationale, RationaleLocale: c.queried}
			markRationaleLocale(&cl, c.locale, "zh")
			if cl.RationaleLocale != c.wantLang || cl.RationaleLocalized != c.wantLocalized {
				t.Errorf("得到 (%q, %v)，期望 (%q, %v)", cl.RationaleLocale, cl.RationaleLocalized, c.wantLang, c.wantLocalized)
			}
		})
	}
}
