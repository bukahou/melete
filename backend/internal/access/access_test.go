package access

import "testing"

// 档位推导与 Scope 是纯函数 —— 规则的核心，单测钉死。
func TestTierAndScope(t *testing.T) {
	cases := []struct {
		perms []string
		tier  Tier
	}{
		{nil, TierBasic},
		{[]string{}, TierBasic},
		{[]string{"content:adult"}, TierBasic}, // 不认识的权限不产生效力
		{[]string{PermPrivate}, TierAdvanced},
		{[]string{PermManage}, TierAdmin},
		{[]string{PermPrivate, PermManage}, TierAdmin}, // 最高那一档说了算
		{[]string{PermAll}, TierAdmin},
	}
	for _, c := range cases {
		if got := TierOf(c.perms); got != c.tier {
			t.Fatalf("TierOf(%v) = %s，期望 %s", c.perms, got, c.tier)
		}
	}

	var zero Scope // 零值 = 只看公开（fail-closed）
	for _, c := range []struct {
		scope      Scope
		pub, priv  bool
		unknownVis bool
	}{
		{zero, true, false, false},
		{ScopeFor(TierBasic), true, false, false},
		{ScopeFor(TierAdvanced), true, true, true},
		{ScopeFor(TierAdmin), true, true, true},
	} {
		if c.scope.Allows(VisibilityPublic) != c.pub || c.scope.Allows(VisibilityPrivate) != c.priv ||
			c.scope.Allows("something-else") != c.unknownVis {
			t.Fatalf("Scope %+v 的判定不对", c.scope)
		}
	}
	if got := ScopeFor(TierBasic).BankFilter("b"); got != "b.visibility = 'public'" {
		t.Fatalf("普通用户的过滤条件不对：%s", got)
	}
	if got := ScopeFor(TierAdvanced).BankFilter("b"); got != "1 = 1" {
		t.Fatalf("高级用户的过滤条件不对：%s", got)
	}
}
