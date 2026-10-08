package httpapi

import (
	"testing"

	"github.com/bukahou/melete/backend/internal/api"
)

// drillQueryFromContext 是「继续」能否回到原入口的唯一翻译 —— 翻错了不报错，只是继续到别的题上。
func TestDrillQueryFromContext(t *testing.T) {
	p := func(v int64) *int64 { return &v }
	i := func(v int) *int { return &v }
	s := func(v string) *string { return &v }
	st := func(v api.DrillContextStatus) *api.DrillContextStatus { return &v }
	ids := &[]int64{3, 4}

	cases := []struct {
		name string
		in   api.DrillContext
		ok   bool
		chk  func(d drillQuery) bool
	}{
		{"year 一套卷子", api.DrillContext{Mode: "year", Session: s("2026r08")}, true,
			func(d drillQuery) bool { return d.Session != nil && *d.Session == "2026r08" }},
		{"year 一组题号", api.DrillContext{Mode: "year", NoFrom: i(101), NoTo: i(200)}, true,
			func(d drillQuery) bool { return d.NoFrom == 101 && d.NoTo == 200 }},
		{"year 什么都没给 ⇒ 不是 4.1 会产生的出处", api.DrillContext{Mode: "year"}, false, nil},
		{"domain 标签并集", api.DrillContext{Mode: "domain", TagIds: ids}, true,
			func(d drillQuery) bool { return len(d.AnyTag) == 2 && d.Tag == nil }},
		{"domain 没有标签", api.DrillContext{Mode: "domain"}, false, nil},
		{"pick 错题 ∧ 标签", api.DrillContext{Mode: "pick", Status: st("wrong"), TagIds: ids}, true,
			func(d drillQuery) bool { return d.Mode == "wrong" && len(d.AnyTag) == 2 }},
		{"pick 打乱", api.DrillContext{Mode: "pick", Seed: p(9)}, true,
			func(d drillQuery) bool { return d.Seed != nil && *d.Seed == 9 && d.Take == 0 }},
		{"pick 收藏", api.DrillContext{Mode: "pick", Status: st("bookmarked")}, true,
			func(d drillQuery) bool { return d.Bookmarked && d.Mode == "" }},
		{"pick 有分歧", api.DrillContext{Mode: "pick", Status: st("contested")}, true,
			func(d drillQuery) bool { return d.Contested }},
		{"random", api.DrillContext{Mode: "random", Seed: p(7), Count: i(10)}, true,
			func(d drillQuery) bool { return *d.Seed == 7 && d.Take == 10 }},
		{"random 缺题数", api.DrillContext{Mode: "random", Seed: p(7)}, false, nil},
		{"旧入口 tag", api.DrillContext{Mode: "tag", TagId: p(9)}, true,
			func(d drillQuery) bool { return len(d.Tag) == 1 && d.Tag[0] == 9 }},
		{"旧入口 unseen", api.DrillContext{Mode: "unseen"}, true,
			func(d drillQuery) bool { return d.Mode == "unseen" }},
		{"未知 mode", api.DrillContext{Mode: "nope"}, false, nil},
	}
	for _, c := range cases {
		d, ok := drillQueryFromContext(c.in)
		if ok != c.ok {
			t.Errorf("%s: ok=%v，期望 %v", c.name, ok, c.ok)
			continue
		}
		if ok && !c.chk(d) {
			t.Errorf("%s: 翻译结果不对 %+v", c.name, d)
		}
	}
}
