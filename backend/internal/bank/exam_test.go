package bank

import "testing"

func strp(s string) *string { return &s }

func TestParseExam(t *testing.T) {
	e, err := ParseExam(strp(`{"tagTypes":{},"passScore":720,"maxScore":1000,"examQuestions":65,"groupSize":10}`))
	if err != nil {
		t.Fatal(err)
	}
	s, ok, err := e.Scale()
	if err != nil || !ok || s.Pass != 72 || s.Safety != 82 || e.Questions() != 65 {
		t.Fatalf("得到 scale=%+v ok=%v err=%v questions=%d", s, ok, err, e.Questions())
	}

	for name, meta := range map[string]*string{"nil": nil, "空串": strp(""), "没有及格分": strp(`{"tagTypes":{}}`)} {
		e, err := ParseExam(meta)
		if err != nil {
			t.Fatalf("%s：%v", name, err)
		}
		if _, ok, err := e.Scale(); ok || err != nil {
			t.Fatalf("%s：应视为没有及格线，得到 ok=%v err=%v", name, ok, err)
		}
	}

	// JSON 写坏了是数据错误，⛔ 不能当作「没有及格线」
	if _, err := ParseExam(strp(`{"passScore":`)); err == nil {
		t.Fatal("坏 JSON 应返回错误")
	}
	if _, err := ParseExam(strp(`{"passScore":"720"}`)); err == nil {
		t.Fatal("类型不对应返回错误")
	}
}
