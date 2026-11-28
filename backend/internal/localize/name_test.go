package localize

import "testing"

func TestPickName(t *testing.T) {
	domain := map[string]string{"en": "Design Secure Architectures", "zh": "设计安全架构"}
	jaOnly := map[string]string{"ja": "ストラテジ系"}
	cases := []struct {
		name          string
		names         map[string]string
		locale        string
		wantName      string
		wantLocalized bool
	}{
		{"请求的语言有名字", domain, "zh", "设计安全架构", true},
		{"请求的语言没有：回退英文并标出", domain, "ja", "Design Secure Architectures", false},
		{"请求英文", domain, "en", "Design Secure Architectures", true},
		{"没有指定语言：用英文，不算回退", domain, "", "Design Secure Architectures", true},
		{"没有英文：用其余语言里代码最小的", map[string]string{"zh": "设计", "ja": "設計"}, "ko", "設計", false},
		{"只有原文语言的名字", jaOnly, "zh", "ストラテジ系", false},
		{"只有原文语言的名字，请求的正是它", jaOnly, "ja", "ストラテジ系", true},
		{"没有多语言名字：value 就是显示名", nil, "ja", "API Gateway", true},
		{"名字全是空串：回退 value 并标出", map[string]string{"en": ""}, "ja", "API Gateway", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, localized := PickName(c.names, "API Gateway", c.locale)
			if got != c.wantName || localized != c.wantLocalized {
				t.Errorf("PickName(%v, %q) = (%q, %v)，想要 (%q, %v)", c.names, c.locale, got, localized, c.wantName, c.wantLocalized)
			}
		})
	}
}
