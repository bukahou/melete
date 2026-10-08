package glossary

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// 用语集（P9 第 6 步）：目录带出题数、详情带释义与出题历史（题干跟语言走）。
// 自造题库与术语、只删自己造的行 —— CI 的空表与 dev 库都能跑。
func TestGlossaryIntegration(t *testing.T) {
	dsn := os.Getenv("MELETE_TEST_DSN")
	if dsn == "" {
		t.Skip("未设 MELETE_TEST_DSN，跳过集成测试")
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := db.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := r.LastInsertId()
		return id
	}

	slug := fmt.Sprintf("t-gl-%d", time.Now().UnixNano()%1e9)
	bankID := exec(`INSERT INTO bank (slug,name,locale,kind) VALUES (?,?,'zh','cert')`, slug, slug)
	q1 := exec(`INSERT INTO question (bank_id,external_no,stem,kind,pick_count) VALUES (?,2,'题干二','single',1)`, bankID)
	q2 := exec(`INSERT INTO question (bank_id,external_no,stem,kind,pick_count) VALUES (?,1,'题干一','single',1)`, bankID)
	exec(`INSERT INTO question_i18n (question_id,locale,stem) VALUES (?,'ja','問題文一')`, q2)
	used := exec(`INSERT INTO term (bank_id,slug,names,definition,category,is_lead) VALUES (?,'Amazon S3','{"zh":"S3","ja":"S3"}','{"zh":"对象存储","ja":"オブジェクトストレージ"}','S3',TRUE)`, bankID)
	unused := exec(`INSERT INTO term (bank_id,slug,names,definition,category) VALUES (?,'Amazon EFS','{"zh":"EFS"}','{"zh":"文件存储"}','EFS')`, bankID)
	exec(`INSERT INTO term_question (term_id,question_id) VALUES (?,?),(?,?)`, used, q1, used, q2)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM term_question WHERE term_id IN (?,?)`, used, unused)
		_, _ = db.Exec(`DELETE FROM term WHERE bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE FROM question_i18n WHERE question_id=?`, q2)
		_, _ = db.Exec(`DELETE FROM question WHERE bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE FROM bank WHERE id=?`, bankID)
	})

	svc := NewService(db)
	list, err := svc.ListTerms(ctx, bankID)
	if err != nil || len(list) != 2 {
		t.Fatalf("目录应有 2 个术语，得到 %d（%v）", len(list), err)
	}
	counts := map[string]int{}
	leads := map[string]bool{}
	for _, x := range list {
		counts[x.Slug] = x.QuestionCount
		leads[x.Slug] = x.Lead
	}
	if !leads["Amazon S3"] || leads["Amazon EFS"] {
		t.Fatalf("主条目标记不对：%v", leads)
	}
	if counts["Amazon S3"] != 2 || counts["Amazon EFS"] != 0 {
		t.Fatalf("出题数不对：%v", counts)
	}

	d, err := svc.GetTerm(ctx, used, "ja")
	if err != nil {
		t.Fatal(err)
	}
	if d.Definition["ja"] != "オブジェクトストレージ" || d.Names["zh"] != "S3" {
		t.Fatalf("释义 / 名称解码不对：%+v %+v", d.Definition, d.Names)
	}
	// 出题历史按题号排；有日文译文的题显示译文，没有的回退原文
	if len(d.Questions) != 2 || d.Questions[0].ExternalNo != 1 || d.Questions[0].Stem != "問題文一" || d.Questions[1].Stem != "题干二" {
		t.Fatalf("出题历史不对：%+v", d.Questions)
	}
	if _, err := svc.GetTerm(ctx, -1, ""); err != ErrNotFound {
		t.Fatalf("不存在的术语应 ErrNotFound，得到 %v", err)
	}
}
