package question

import (
	"context"
	"testing"
)

// 选项点评的集成测试 —— 需要真数据库，设 MELETE_TEST_DSN 才跑。
// 非破坏性：造自己的题库、只删自己造的行。
func TestChoiceExplanationIntegration(t *testing.T) {
	db := openDueTestDB(t)
	ctx := context.Background()
	r := NewMySQLRepository(db)

	tag := "t-cexp-" + randSuffix()
	res, err := db.ExecContext(ctx,
		`INSERT INTO bank (slug,name,locale,kind) VALUES (?,?,'zh','cert')`, tag, tag)
	if err != nil {
		t.Fatal(err)
	}
	bankID, _ := res.LastInsertId()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE e FROM choice_explanation e JOIN choice c ON c.id=e.choice_id
		                JOIN question q ON q.id=c.question_id WHERE q.bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE c FROM choice c JOIN question q ON q.id=c.question_id WHERE q.bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE FROM question WHERE bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE FROM bank WHERE id=?`, bankID)
	})

	res, err = db.ExecContext(ctx,
		`INSERT INTO question (bank_id,external_no,stem,kind,pick_count) VALUES (?,1,'题干','single',1)`, bankID)
	if err != nil {
		t.Fatal(err)
	}
	qid, _ := res.LastInsertId()
	choiceID := map[string]int64{}
	for _, label := range []string{"A", "B", "C"} {
		res, err = db.ExecContext(ctx,
			`INSERT INTO choice (question_id,label,body) VALUES (?,?,?)`, qid, label, "选项"+label)
		if err != nil {
			t.Fatal(err)
		}
		choiceID[label], _ = res.LastInsertId()
	}
	// A 有中日两种，B 只有中文，C 没有点评
	db.MustExec(`INSERT INTO choice_explanation (choice_id,source,locale,body) VALUES (?,'ai','zh','A 的中文点评')`, choiceID["A"])
	db.MustExec(`INSERT INTO choice_explanation (choice_id,source,locale,body) VALUES (?,'ai','ja','A の日本語の解説')`, choiceID["A"])
	db.MustExec(`INSERT INTO choice_explanation (choice_id,source,locale,body) VALUES (?,'ai','zh','B 的中文点评')`, choiceID["B"])

	ja, err := r.FindQuestionByID(ctx, qid, "ja")
	if err != nil {
		t.Fatal(err)
	}
	if len(ja.Choices) != 3 {
		t.Fatalf("选项数 = %d", len(ja.Choices))
	}
	if e := ja.Choices[0].Explanation; e == nil || e.Body != "A の日本語の解説" || !e.Localized {
		t.Errorf("A = %+v，期望日文点评", e)
	}
	if e := ja.Choices[1].Explanation; e == nil || e.Body != "B 的中文点评" || e.Localized || e.Locale != "zh" {
		t.Errorf("B = %+v，期望回退到中文且 localized=false", e)
	}
	if e := ja.Choices[2].Explanation; e != nil {
		t.Errorf("C = %+v，没有点评时应为 nil", e)
	}

	plain, err := r.FindQuestionByID(ctx, qid, "")
	if err != nil {
		t.Fatal(err)
	}
	if e := plain.Choices[0].Explanation; e == nil || e.Locale != "zh" || !e.Localized {
		t.Errorf("未指定语言时 A = %+v，期望题库语言（zh）", e)
	}
}
