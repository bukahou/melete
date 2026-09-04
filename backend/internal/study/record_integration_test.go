package study

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	_ "github.com/go-sql-driver/mysql"

	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/study/scheduler"
)

// 集成测试：作答 → 纠正 → 调度 → 落库，走真实事务。
//
// 需要一个可写的 MySQL —— 设 MELETE_TEST_DSN 才运行，⛔ 不要指向开发库或生产库：
// 它会建账号、写作答、写卡片。用一次性容器：
//
//	docker run -d --name melete-cardtest -e MYSQL_ROOT_PASSWORD=t \
//	  -e MYSQL_DATABASE=melete -p 13307:3306 mysql:8.0
//	mysql -h 127.0.0.1 -P 13307 -uroot -pt melete < db/schema.sql
//	MELETE_TEST_DSN='root:t@tcp(127.0.0.1:13307)/melete?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27' \
//	  go test ./internal/study/ -run Integration -v
func openTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("MELETE_TEST_DSN")
	if dsn == "" {
		t.Skip("未设 MELETE_TEST_DSN，跳过集成测试")
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Fatalf("连接测试库: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// fixture 建一个题库、一道四选一的题、一条 ai_verdict 主张（正确答案 A）和一个账号。
func fixture(t *testing.T, db *sqlx.DB, stem string) (accountID, questionID int64) {
	t.Helper()
	ctx := context.Background()
	for _, s := range []string{
		`DELETE FROM card`, `DELETE FROM attempt`, `DELETE FROM answer_claim`,
		`DELETE FROM choice`, `DELETE FROM question`, `DELETE FROM bank`, `DELETE FROM account`,
	} {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("清库 %q: %v", s, err)
		}
	}
	r, err := db.ExecContext(ctx,
		`INSERT INTO bank (slug,name,locale,kind) VALUES ('t-bank','测试题库','zh','cert')`)
	if err != nil {
		t.Fatal(err)
	}
	bankID, _ := r.LastInsertId()

	r, err = db.ExecContext(ctx,
		`INSERT INTO question (bank_id,external_no,stem,kind,pick_count) VALUES (?,1,?,'single',1)`,
		bankID, stem)
	if err != nil {
		t.Fatal(err)
	}
	questionID, _ = r.LastInsertId()

	for _, l := range []string{"A", "B", "C", "D"} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO choice (question_id,label,body) VALUES (?,?,?)`, questionID, l, "选项"+l); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO answer_claim (question_id,source,answer) VALUES (?,'ai_verdict','A')`, questionID); err != nil {
		t.Fatal(err)
	}
	r, err = db.ExecContext(ctx, `INSERT INTO account (akasha_sub,display) VALUES ('t-sub','测试')`)
	if err != nil {
		t.Fatal(err)
	}
	accountID, _ = r.LastInsertId()
	return accountID, questionID
}

func readCard(t *testing.T, db *sqlx.DB, acct, q int64) cardRow {
	t.Helper()
	var row cardRow
	err := db.Get(&row, `SELECT state,due,stability,difficulty,reps,lapses,last_review
	                     FROM card WHERE account_id=? AND question_id=?`, acct, q)
	if err != nil {
		t.Fatalf("读卡片: %v", err)
	}
	return row
}

func TestRecordAttemptIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	// 1000 字的题面 → 按 200 字/分，读完至少 5 分钟
	stem := ""
	for len(([]rune(stem))) < 1000 {
		stem += "题面文字"
	}
	acct, qid := fixture(t, db, stem)
	svc := NewService(db, question.NewMySQLRepository(db))

	t.Run("诚实答错 → 卡片进入 relearning/learning，reps=1", func(t *testing.T) {
		res, err := svc.RecordAttempt(ctx, Attempt{
			AccountID: acct, QuestionID: qid, Chosen: "B", Rating: scheduler.RatingAgain,
			DurationMs: ptr(600_000),
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Correct {
			t.Errorf("选了 B 而答案是 A，不该判对")
		}
		if res.Correction.Adjusted() {
			t.Errorf("诚实作答不该被纠正，却给了 %v", res.Correction.Reasons)
		}
		c := readCard(t, db, acct, qid)
		if c.Reps != 1 {
			t.Errorf("reps = %d，期望 1", c.Reps)
		}
		t.Logf("卡片: state=%v due=%s reps=%d", scheduler.State(c.State), c.Due.Format(time.RFC3339), c.Reps)
	})

	t.Run("⭐ 嘴硬：答错却按「掌握」→ 有效评分被压到 1 且理由入库可见", func(t *testing.T) {
		res, err := svc.RecordAttempt(ctx, Attempt{
			AccountID: acct, QuestionID: qid, Chosen: "C", Rating: scheduler.RatingGood,
			DurationMs: ptr(600_000),
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Correction.Effective != scheduler.RatingAgain {
			t.Errorf("有效评分 = %d，期望 1", res.Correction.Effective)
		}
		if res.Correction.Raw != scheduler.RatingGood {
			t.Errorf("⛔ Raw 被改了：%d", res.Correction.Raw)
		}
		// attempt 表里存的必须是用户按的那个键，不是纠正后的
		var stored int
		if err := db.Get(&stored,
			`SELECT rating FROM attempt ORDER BY id DESC LIMIT 1`); err != nil {
			t.Fatal(err)
		}
		if stored != scheduler.RatingGood {
			t.Errorf("⛔ attempt.rating = %d —— 落库的必须是原始自评（3），纠正只影响调度", stored)
		}
		t.Logf("纠正理由: %v", res.Correction.Reasons)
	})

	t.Run("用时读不完题面 → 答对也压到 2", func(t *testing.T) {
		res, err := svc.RecordAttempt(ctx, Attempt{
			AccountID: acct, QuestionID: qid, Chosen: "A", Rating: scheduler.RatingEasy,
			DurationMs: ptr(3_000), // 3 秒读 1000 字？
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Correct {
			t.Fatalf("选了 A 应判对")
		}
		if res.Correction.Effective != scheduler.RatingHard {
			t.Errorf("有效评分 = %d，期望 2（理由 %v）", res.Correction.Effective, res.Correction.Reasons)
		}
		t.Logf("纠正理由: %v", res.Correction.Reasons)
	})

	t.Run("同一张卡反复作答只有一行，reps 递增", func(t *testing.T) {
		var n int
		if err := db.Get(&n, `SELECT COUNT(*) FROM card WHERE account_id=? AND question_id=?`, acct, qid); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("卡片行数 = %d，期望 1（主键应当去重）", n)
		}
		c := readCard(t, db, acct, qid)
		if c.Reps != 3 {
			t.Errorf("reps = %d，期望 3（前面做了三次）", c.Reps)
		}
		if !c.LastReview.Valid {
			t.Errorf("last_review 仍为 NULL —— 复习过就该有值")
		}
	})

	t.Run("到期统计：这题刚做完不该算到期", func(t *testing.T) {
		sum, err := svc.LoadDueSummary(ctx, acct)
		if err != nil {
			t.Fatal(err)
		}
		if len(sum) != 1 {
			t.Fatalf("题库数 = %d", len(sum))
		}
		t.Logf("题库 %s: 到期 %d，没做过 %d", sum[0].BankSlug, sum[0].Due, sum[0].Unseen)
		if sum[0].Unseen != 0 {
			t.Errorf("唯一那道题已做过，unseen 应为 0，实际 %d", sum[0].Unseen)
		}
	})

	t.Run("把 due 拨到过去 → 该题变成到期", func(t *testing.T) {
		if _, err := db.Exec(`UPDATE card SET due = UTC_TIMESTAMP() - INTERVAL 1 DAY
		                      WHERE account_id=? AND question_id=?`, acct, qid); err != nil {
			t.Fatal(err)
		}
		sum, err := svc.LoadDueSummary(ctx, acct)
		if err != nil {
			t.Fatal(err)
		}
		if sum[0].Due != 1 {
			t.Errorf("到期数 = %d，期望 1", sum[0].Due)
		}
	})
}

// 无参考答案的题（题库里有 6 道）：correct 恒为 false，⛔ 不得据此判嘴硬。
func TestNoReferenceNotJudgedAsLyingIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	acct, qid := fixture(t, db, "短题面")
	if _, err := db.Exec(`DELETE FROM answer_claim WHERE question_id=?`, qid); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, question.NewMySQLRepository(db))

	res, err := svc.RecordAttempt(ctx, Attempt{
		AccountID: acct, QuestionID: qid, Chosen: "A", Rating: scheduler.RatingEasy,
		DurationMs: ptr(600_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reference != nil {
		t.Fatalf("这题不该有参考答案")
	}
	if res.Correction.Adjusted() {
		t.Errorf("⛔ 判不了对错的题被当成嘴硬纠正了：%v", res.Correction.Reasons)
	}
	if res.Correction.Effective != scheduler.RatingEasy {
		t.Errorf("有效评分 = %d，期望原样 4", res.Correction.Effective)
	}
}

// ⭐⭐ 纠正必须真的作用到【调度】上，不能只体现在返回值里。
//
// ⚠️ 这条是补上来的：第一版测试只断言 res.Correction.Effective 的数值，
// 于是把 `s.sched.Next(card, correction.Effective, ...)` 改成
// `s.sched.Next(card, a.Rating, ...)`（纠正完全失效）测试照样全绿 ——
// 而那一行是整个功能的全部意义。2026-09-04 变异测试实测过。
func TestCorrectionActuallyDrivesSchedulingIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	acct, qid := fixture(t, db, "短题面")
	svc := NewService(db, question.NewMySQLRepository(db))

	// 答错（答案是 A，选 C）却按「掌握」—— 嘴硬
	if _, err := svc.RecordAttempt(ctx, Attempt{
		AccountID: acct, QuestionID: qid, Chosen: "C", Rating: scheduler.RatingGood,
		DurationMs: ptr(600_000),
	}); err != nil {
		t.Fatal(err)
	}
	got := readCard(t, db, acct, qid)

	// 拿同一个调度器算出「按 Again 排」和「按 Good 排」两种结果，看库里到底是哪一种
	sch := scheduler.New()
	fresh := scheduler.NewCard()
	asAgain := sch.Next(fresh, scheduler.RatingAgain, time.Now().UTC())
	asGood := sch.Next(fresh, scheduler.RatingGood, time.Now().UTC())

	t.Logf("库里     S=%.4f", got.Stability)
	t.Logf("按 Again S=%.4f   按 Good S=%.4f", asAgain.Stability, asGood.Stability)

	// ⚠️ 先确认这个维度分得开。第一版拿 state 比，而新卡按 Again 和按 Good
	// 都进 learning —— 断言恒成立，等于没断言。守卫必须在断言之前。
	if asAgain.Stability == asGood.Stability {
		t.Fatal("这个测试失去了鉴别力：两种评分给出了同一个稳定性，换个断言维度")
	}
	if got.Stability != asAgain.Stability {
		t.Errorf("⛔ 卡片是按【原始评分】排的（S=%.4f，Again 应为 %.4f、Good 是 %.4f）"+
			" —— 纠正没有作用到调度上", got.Stability, asAgain.Stability, asGood.Stability)
	}
}

func ptr(v int) *int { return &v }

var _ = sql.ErrNoRows
