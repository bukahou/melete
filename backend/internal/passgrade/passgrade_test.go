package passgrade

import (
	"errors"
	"testing"
)

func intp(v int) *int { return &v }

func TestNewScale(t *testing.T) {
	cases := []struct {
		name                string
		pass, max           int
		override            *int
		want                Scale
		wantOK, wantInvalid bool
	}{
		{name: "SAA：720 / 1000", pass: 720, max: 1000, want: Scale{Pass: 72, Safety: 82, Floor: 57}, wantOK: true},
		{name: "IPA：600 / 1000", pass: 600, max: 1000, want: Scale{Pass: 60, Safety: 70, Floor: 45}, wantOK: true},
		{name: "显式指定安全线", pass: 720, max: 1000, override: intp(80), want: Scale{Pass: 72, Safety: 80, Floor: 57}, wantOK: true},
		{name: "换算后四舍五入", pass: 725, max: 1000, want: Scale{Pass: 73, Safety: 83, Floor: 58}, wantOK: true},
		{name: "合格线很高：安全线封顶 100", pass: 950, max: 1000, want: Scale{Pass: 95, Safety: 100, Floor: 80}, wantOK: true},
		{name: "合格线很低：C 档下限不为负", pass: 100, max: 1000, want: Scale{Pass: 10, Safety: 20, Floor: 0}, wantOK: true},
		{name: "没有及格分", pass: 0, max: 1000},
		{name: "没有满分", pass: 720, max: 0},
		{name: "安全线低于合格线", pass: 720, max: 1000, override: intp(70), wantInvalid: true},
		{name: "安全线超过 100", pass: 720, max: 1000, override: intp(101), wantInvalid: true},
		{name: "及格分高于满分", pass: 1200, max: 1000, wantInvalid: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok, err := NewScale(c.pass, c.max, c.override)
			if c.wantInvalid {
				if !errors.Is(err, ErrInvalidExam) {
					t.Fatalf("应返回 ErrInvalidExam，得到 %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ok != c.wantOK || got != c.want {
				t.Fatalf("得到 (%+v, %v)，期望 (%+v, %v)", got, ok, c.want, c.wantOK)
			}
		})
	}
}

func TestGradeOfBoundaries(t *testing.T) {
	s := Scale{Pass: 72, Safety: 82, Floor: 57}
	cases := []struct {
		correct, total int
		want           Grade
	}{
		{41, 50, GradeA},   // 82% 正好在安全线上
		{408, 500, GradeB}, // 81.6%：⛔ 不能四舍五入成 82 升到 A
		{36, 50, GradeB},   // 72% 正好在合格线上
		{359, 500, GradeC}, // 71.8%
		{57, 100, GradeC},  // 57% 正好在 C 档下限上
		{56, 100, GradeD},
		{0, 3, GradeD},
		{3, 3, GradeA},
	}
	for _, c := range cases {
		if got := s.GradeOf(c.correct, c.total); got != c.want {
			t.Errorf("%d/%d：得到 %s，期望 %s", c.correct, c.total, got, c.want)
		}
	}
}

func TestBands(t *testing.T) {
	bands := Scale{Pass: 72, Safety: 82, Floor: 57}.Bands()
	type flat struct {
		g        Grade
		min, max int // max = -1 表示没有上限
	}
	want := []flat{{GradeA, 82, -1}, {GradeB, 72, 82}, {GradeC, 57, 72}, {GradeD, 0, 57}}
	if len(bands) != len(want) {
		t.Fatalf("应有 %d 档，得到 %d", len(want), len(bands))
	}
	for i, b := range bands {
		got := flat{b.Grade, b.Min, -1}
		if b.Max != nil {
			got.max = *b.Max
		}
		if got != want[i] {
			t.Errorf("第 %d 档：得到 %+v，期望 %+v", i, got, want[i])
		}
	}
}

func TestJudgeWaitsForOneExam(t *testing.T) {
	s := Scale{Pass: 72, Safety: 82, Floor: 57}

	j := s.Judge(0, 0, 65)
	if j.Grade != nil || j.Remaining != 65 {
		t.Fatalf("一题没做：应不给等级、还差 65，得到 %+v", j)
	}
	j = s.Judge(64, 64, 65)
	if j.Grade != nil || j.Remaining != 1 {
		t.Fatalf("差一题：即使全对也不给等级，得到 %+v", j)
	}
	j = s.Judge(65, 53, 65) // 81.5%
	if j.Grade == nil || *j.Grade != GradeB || j.Remaining != 0 {
		t.Fatalf("做满一场：应给 B，得到 %+v", j)
	}
	if len(j.Bands) != 4 {
		t.Fatalf("不论给不给等级都要带四档范围，得到 %d 档", len(j.Bands))
	}

	// 题库没写一场考试的题数：做过一道就给
	if j := s.Judge(0, 0, 0); j.Grade != nil || j.Remaining != 1 {
		t.Fatalf("没写考试题数、一题没做：应还差 1，得到 %+v", j)
	}
	if j := s.Judge(1, 1, 0); j.Grade == nil || *j.Grade != GradeA {
		t.Fatalf("没写考试题数、做过一道：应给等级，得到 %+v", j)
	}
}

func TestDomainGradeNeedsEnoughQuestions(t *testing.T) {
	s := Scale{Pass: 72, Safety: 82, Floor: 57}
	if g := s.DomainGrade(DomainMinSeen-1, DomainMinSeen-1); g != nil {
		t.Fatalf("做过 %d 道：不应给等级，得到 %s", DomainMinSeen-1, *g)
	}
	if g := s.DomainGrade(5, DomainMinSeen); g == nil || *g != GradeD {
		t.Fatalf("做过 %d 道、对 5 道：应为 D，得到 %v", DomainMinSeen, g)
	}
}
