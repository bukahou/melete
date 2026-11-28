package question

import (
	"context"
	"testing"
)

// 题目解析与 AI 理由按请求语言挑选的集成测试 —— 需要真数据库，设 MELETE_TEST_DSN 才跑。
// 非破坏性：造自己的题库、只删自己造的行。
func TestLocalizedExplanationAndRationaleIntegration(t *testing.T) {
	db := openDueTestDB(t)
	ctx := context.Background()
	r := NewMySQLRepository(db)

	tag := "t-lrat-" + randSuffix()
	res, err := db.ExecContext(ctx,
		`INSERT INTO bank (slug,name,locale,kind) VALUES (?,?,'zh','cert')`, tag, tag)
	if err != nil {
		t.Fatal(err)
	}
	bankID, _ := res.LastInsertId()
	res, err = db.ExecContext(ctx,
		`INSERT INTO question (bank_id,external_no,stem,kind,pick_count) VALUES (?,1,'题干','single',1)`, bankID)
	if err != nil {
		t.Fatal(err)
	}
	qid, _ := res.LastInsertId()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM answer_claim_i18n WHERE question_id=?`, qid)
		_, _ = db.Exec(`DELETE FROM answer_claim WHERE question_id=?`, qid)
		_, _ = db.Exec(`DELETE FROM explanation WHERE question_id=?`, qid)
		_, _ = db.Exec(`DELETE FROM choice WHERE question_id=?`, qid)
		_, _ = db.Exec(`DELETE FROM question WHERE id=?`, qid)
		_, _ = db.Exec(`DELETE FROM bank WHERE id=?`, bankID)
	})
	db.MustExec(`INSERT INTO choice (question_id,label,body) VALUES (?,'A','选项A'),(?,'B','选项B')`, qid, qid)
	db.MustExec(`INSERT INTO explanation (question_id,source,locale,body) VALUES (?,'ai','zh','中文解析'),(?,'ai','ja','日本語の解説')`, qid, qid)
	db.MustExec(`INSERT INTO answer_claim (question_id,source,answer) VALUES (?,'bank_label','A')`, qid)
	db.MustExec(`INSERT INTO answer_claim (question_id,source,answer,rationale) VALUES (?,'ai_verdict','B','中文分歧说明')`, qid)
	db.MustExec(`INSERT INTO answer_claim_i18n (question_id,source,locale,rationale) VALUES (?,'ai_verdict','ja','日本語の説明')`, qid)

	cases := []struct {
		name, locale       string
		explBody, explLang string
		explLocalized      bool
		ratBody, ratLang   string
		ratLocalized       bool
	}{
		{"请求日文：两者都有日文", "ja", "日本語の解説", "ja", true, "日本語の説明", "ja", true},
		{"请求没有的语言：回退题库语言并标出", "en", "中文解析", "zh", false, "中文分歧说明", "zh", false},
		{"请求题库语言", "zh", "中文解析", "zh", true, "中文分歧说明", "zh", true},
		{"没有指定语言：原文，不算回退", "", "中文解析", "zh", true, "中文分歧说明", "zh", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := r.FindQuestionByID(ctx, qid, c.locale)
			if err != nil {
				t.Fatal(err)
			}
			if e := d.Explanation; e == nil || e.Body != c.explBody || e.Locale != c.explLang || e.Localized != c.explLocalized {
				t.Errorf("解析 = %+v，期望 %s / %s / localized=%v", e, c.explBody, c.explLang, c.explLocalized)
			}
			if len(d.Explanations) != 2 {
				t.Errorf("explanations 仍应给出全部 2 份，得到 %d", len(d.Explanations))
			}
			if len(d.Claims) != 2 {
				t.Fatalf("主张数 = %d", len(d.Claims))
			}
			bl, ai := d.Claims[0], d.Claims[1]
			if bl.Source != "bank_label" || bl.Rationale != nil || bl.RationaleLocale != "" || bl.RationaleLocalized {
				t.Errorf("题库标注 = %+v，没有理由时语言标记应为零值", bl)
			}
			if ai.Rationale == nil || *ai.Rationale != c.ratBody || ai.RationaleLocale != c.ratLang || ai.RationaleLocalized != c.ratLocalized {
				t.Errorf("AI 裁决理由 = %v / %s / %v，期望 %s / %s / %v",
					ai.Rationale, ai.RationaleLocale, ai.RationaleLocalized, c.ratBody, c.ratLang, c.ratLocalized)
			}
		})
	}
}
