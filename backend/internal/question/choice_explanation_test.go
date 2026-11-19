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
		want         map[string]ChoiceExplanation
	}{
		{"请求日文：有就用，没有回退到题库语言并标出", "ja", map[string]ChoiceExplanation{
			"A": {Source: "ai", Locale: "ja", Body: "A 日本語", Localized: true},
			"B": {Source: "editor", Locale: "zh", Body: "B 人工订正", Localized: false},
			"C": {Source: "ai", Locale: "zh", Body: "C 中文", Localized: false},
		}},
		{"请求题库语言", "zh", map[string]ChoiceExplanation{
			"A": {Source: "ai", Locale: "zh", Body: "A 中文", Localized: true},
			"B": {Source: "editor", Locale: "zh", Body: "B 人工订正", Localized: true},
			"C": {Source: "ai", Locale: "zh", Body: "C 中文", Localized: true},
		}},
		{"没有指定语言：用题库语言，不算回退", "", map[string]ChoiceExplanation{
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
