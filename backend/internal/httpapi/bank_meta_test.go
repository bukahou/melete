package httpapi

import "testing"

// BankMeta.safetyRate 与合格等级的 A 档下限必须是同一个数：都由 passgrade 算，⛔ 不原样透传列里的值。
func TestParseBankMetaSafetyRate(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name string
		meta string
		want *int
	}{
		{name: "没写安全线：按公式", meta: `{"passScore":720,"maxScore":1000}`, want: intPtr(82)},
		{name: "显式指定：用指定的", meta: `{"passScore":720,"maxScore":1000,"safetyRate":80}`, want: intPtr(80)},
		{name: "没有及格线：不给安全线", meta: `{"safetyRate":80}`, want: nil},
		{name: "配置矛盾：不给安全线", meta: `{"passScore":720,"maxScore":1000,"safetyRate":60}`, want: nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseBankMeta(str(c.meta)).SafetyRate
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Fatalf("得到 %v，期望 %v", showRate(got), showRate(c.want))
			}
		})
	}
}

func intPtr(v int) *int { return &v }

func showRate(p *int) any {
	if p == nil {
		return "缺省"
	}
	return *p
}
