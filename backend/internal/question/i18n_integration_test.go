package question

import (
	"context"
	"testing"
)

// 题目多语言的集成测试 —— 需要真数据库，设 MELETE_TEST_DSN 才跑。
//
// ⚠️ 这里测的核心不是「能读到译文」，而是【占位符顺序】：
// locale 的 `?` 出现在 LEFT JOIN 的 ON 里，也就是【所有 WHERE 参数之前】。
// 顺序错了 SQL 照样执行，只是把 locale 当成 bank_id ⇒ 静默返回 0 条。
// 与 due_integration_test.go 那条注释同源，是本文件最容易出错的地方。
//
// ⭐ 非破坏性：造自己的题库、只删自己造的行。
func TestQuestionLocaleFallbackIntegration(t *testing.T) {
	db := openDueTestDB(t)
	ctx := context.Background()
	r := NewMySQLRepository(db)

	tag := "t-i18n-" + randSuffix()
	res, err := db.ExecContext(ctx,
		`INSERT INTO bank (slug,name,locale,kind) VALUES (?,?,'zh','cert')`, tag, tag)
	if err != nil {
		t.Fatal(err)
	}
	bankID, _ := res.LastInsertId()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE i FROM choice_i18n i JOIN choice c ON c.id=i.choice_id
		                JOIN question q ON q.id=c.question_id WHERE q.bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE i FROM question_i18n i JOIN question q ON q.id=i.question_id WHERE q.bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE c FROM choice c JOIN question q ON q.id=c.question_id WHERE q.bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE FROM question WHERE bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE FROM bank WHERE id=?`, bankID)
	})

	res, err = db.ExecContext(ctx,
		`INSERT INTO question (bank_id,external_no,stem,kind,pick_count) VALUES (?,1,'中文题干','single',1)`, bankID)
	if err != nil {
		t.Fatal(err)
	}
	qid, _ := res.LastInsertId()
	choiceID := map[string]int64{}
	for _, label := range []string{"A", "B"} {
		res, err = db.ExecContext(ctx,
			`INSERT INTO choice (question_id,label,body) VALUES (?,?,?)`, qid, label, "中文选项"+label)
		if err != nil {
			t.Fatal(err)
		}
		choiceID[label], _ = res.LastInsertId()
	}
	// ⭐ 刻意【只译题干和选项 A】—— 部分翻译才是常态，
	// 而「选项各带各的 localized」正是为这种状态存在的。
	db.MustExec(`INSERT INTO question_i18n (question_id,locale,stem) VALUES (?,'ja','日本語の設問')`, qid)
	db.MustExec(`INSERT INTO choice_i18n (choice_id,locale,body) VALUES (?,'ja','日本語の選択肢A')`, choiceID["A"])

	ja, err := r.FindQuestionByID(ctx, qid, "ja")
	if err != nil {
		t.Fatal(err)
	}
	if ja.Stem != "日本語の設問" || !ja.Localized {
		t.Errorf("ja 题干 = %q localized=%v，期望取到译文", ja.Stem, ja.Localized)
	}
	if ja.SourceLocale != "zh" {
		t.Errorf("sourceLocale = %q，期望 zh —— 界面靠它区分「无译文」与「请求的就是源语言」", ja.SourceLocale)
	}
	if len(ja.Choices) != 2 {
		t.Fatalf("选项数 = %d", len(ja.Choices))
	}
	if ja.Choices[0].Body != "日本語の選択肢A" || !ja.Choices[0].Localized {
		t.Errorf("选项 A = %+v，期望译文", ja.Choices[0])
	}
	// ⛔ 无译文必须【回退且标注】，静默回退是本设计明确禁止的那一半
	if ja.Choices[1].Body != "中文选项B" || ja.Choices[1].Localized {
		t.Errorf("选项 B = %+v，期望回退到源语言且 localized=false", ja.Choices[1])
	}

	// 白名单外/未协商 ⇒ 空串 ⇒ 完全不碰 i18n 表
	zh, err := r.FindQuestionByID(ctx, qid, "")
	if err != nil {
		t.Fatal(err)
	}
	if zh.Stem != "中文题干" || zh.Localized {
		t.Errorf("源语言路径被污染: stem=%q localized=%v", zh.Stem, zh.Localized)
	}

	// 列表走的是另一条 SQL，参数拼装也另写了一遍 —— 必须各测各的
	page, err := r.ListQuestions(ctx, bankID, ListFilter{Locale: "ja", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("列表返回 %d 条（total=%d），期望 1 —— 多半是 locale 参数串位了", len(page.Items), page.Total)
	}
	if page.Items[0].Stem != "日本語の設問" || !page.Items[0].Localized {
		t.Errorf("列表项 = %+v", page.Items[0])
	}
}
