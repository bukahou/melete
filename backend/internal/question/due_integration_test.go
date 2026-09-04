package question

import (
	"context"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// mode=due 的集成测试 —— 需要真数据库，设 MELETE_TEST_DSN 才跑。
//
// ⚠️ 这条查询里的占位符顺序是【最容易静默出错】的地方：排序表达式带一个
// account_id 参数，它必须排在 LIMIT/OFFSET 之前。顺序错了 SQL 照样执行，
// 只是把 account_id 当成 LIMIT —— 不报错，只是返回条数莫名其妙。
// ⛔ 与 study 包共用同一个测试库且都会 DELETE 全表 ——
// 多包一起跑必须 `go test -p 1`，否则并行时必然互相踩。
func openDueTestDB(t *testing.T) *sqlx.DB {
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

// dueFixture 建 5 道题，给账号建卡片如下：
//
//	#1 到期（3 天前）   #2 到期（1 天前）   #3 到期（10 天前，最该复习）
//	#4 未到期（3 天后） #5 没有卡片（没做过）
//
// 期望 due 模式返回 {3, 1, 2}，按到期时间升序 —— 最该复习的排最前。
func dueFixture(t *testing.T, db *sqlx.DB) (bankID, accountID int64) {
	t.Helper()
	ctx := context.Background()
	for _, s := range []string{
		`DELETE FROM card`, `DELETE FROM attempt`, `DELETE FROM answer_claim`,
		`DELETE FROM choice`, `DELETE FROM question`, `DELETE FROM bank`, `DELETE FROM account`,
	} {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("清库: %v", err)
		}
	}
	r, err := db.ExecContext(ctx,
		`INSERT INTO bank (slug,name,locale,kind) VALUES ('t-due','测试','zh','cert')`)
	if err != nil {
		t.Fatal(err)
	}
	bankID, _ = r.LastInsertId()
	r, err = db.ExecContext(ctx, `INSERT INTO account (akasha_sub,display) VALUES ('due-sub','测试')`)
	if err != nil {
		t.Fatal(err)
	}
	accountID, _ = r.LastInsertId()

	// ⚠️ 另建一个账号并给它反向的到期时间：若查询漏了 account_id 条件，
	// 或把它绑错了位置，这些行就会混进结果里 —— 没有它，跨账号泄漏测不出来。
	r2, err := db.ExecContext(ctx, `INSERT INTO account (akasha_sub,display) VALUES ('other-sub','别人')`)
	if err != nil {
		t.Fatal(err)
	}
	otherID, _ := r2.LastInsertId()

	qids := map[int]int64{}
	for no := 1; no <= 5; no++ {
		r, err := db.ExecContext(ctx,
			`INSERT INTO question (bank_id,external_no,stem,kind,pick_count) VALUES (?,?,?,'single',1)`,
			bankID, no, "题面")
		if err != nil {
			t.Fatal(err)
		}
		qids[no], _ = r.LastInsertId()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO choice (question_id,label,body) VALUES (?,'A','x')`, qids[no]); err != nil {
			t.Fatal(err)
		}
	}
	// 天数为负 = 已到期
	for no, days := range map[int]int{1: -3, 2: -1, 3: -10, 4: 3} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO card (account_id,question_id,state,due,stability,difficulty,reps,lapses)
			VALUES (?,?,2, UTC_TIMESTAMP() + INTERVAL ? DAY, 1,5,1,0)`,
			accountID, qids[no], days); err != nil {
			t.Fatal(err)
		}
	}
	// 别人的卡片：把【本人未到期】的 #4 和【本人没做过】的 #5 都设成已到期
	for _, no := range []int{4, 5} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO card (account_id,question_id,state,due,stability,difficulty,reps,lapses)
			VALUES (?,?,2, UTC_TIMESTAMP() - INTERVAL 30 DAY, 1,5,1,0)`,
			otherID, qids[no]); err != nil {
			t.Fatal(err)
		}
	}
	return bankID, accountID
}

func TestListQuestionsDueModeIntegration(t *testing.T) {
	db := openDueTestDB(t)
	ctx := context.Background()
	bankID, acct := dueFixture(t, db)
	repo := NewMySQLRepository(db)

	t.Run("只返回本人已到期的题，按到期时间升序", func(t *testing.T) {
		page, err := repo.ListQuestions(ctx, bankID, ListFilter{
			Mode: "due", AccountID: acct, Limit: 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		var got []int
		for _, it := range page.Items {
			got = append(got, it.ExternalNo)
		}
		t.Logf("返回题号 = %v，总数 = %d", got, page.Total)

		want := []int{3, 1, 2} // -10 天、-3 天、-1 天
		if len(got) != len(want) {
			t.Fatalf("返回 %v，期望 %v —— 数量就不对", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("第 %d 个是 #%d，期望 #%d（完整结果 %v，期望 %v）", i+1, got[i], want[i], got, want)
			}
		}
		if page.Total != 3 {
			t.Errorf("Total = %d，期望 3", page.Total)
		}
	})

	t.Run("⛔ 不得混入别人的卡片", func(t *testing.T) {
		page, err := repo.ListQuestions(ctx, bankID, ListFilter{
			Mode: "due", AccountID: acct, Limit: 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range page.Items {
			if it.ExternalNo == 4 || it.ExternalNo == 5 {
				t.Errorf("⛔ #%d 只有【别人】的卡片到期，不该出现在我的队列里", it.ExternalNo)
			}
		}
	})

	t.Run("没做过的题不算到期（它属于 unseen 入口）", func(t *testing.T) {
		page, err := repo.ListQuestions(ctx, bankID, ListFilter{
			Mode: "due", AccountID: acct, Limit: 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range page.Items {
			if it.ExternalNo == 5 {
				t.Errorf("⛔ #5 从没做过、没有卡片，不该算「到期」")
			}
		}
	})

	t.Run("分页时占位符顺序不能错位", func(t *testing.T) {
		// limit=2 → 应拿到最该复习的两道 {3, 1}
		page, err := repo.ListQuestions(ctx, bankID, ListFilter{
			Mode: "due", AccountID: acct, Limit: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("limit=2 却返回 %d 条 —— 占位符很可能错位了（account_id 被当成 LIMIT）",
				len(page.Items))
		}
		if page.Items[0].ExternalNo != 3 || page.Items[1].ExternalNo != 1 {
			t.Errorf("第一页 = [#%d,#%d]，期望 [#3,#1]",
				page.Items[0].ExternalNo, page.Items[1].ExternalNo)
		}
		// 第二页
		page2, err := repo.ListQuestions(ctx, bankID, ListFilter{
			Mode: "due", AccountID: acct, Limit: 2, Offset: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page2.Items) != 1 || page2.Items[0].ExternalNo != 2 {
			t.Errorf("第二页 = %v，期望只有 #2", page2.Items)
		}
	})

	t.Run("其余模式仍按题号排，未被 due 的排序改动影响", func(t *testing.T) {
		page, err := repo.ListQuestions(ctx, bankID, ListFilter{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		for i, it := range page.Items {
			if it.ExternalNo != i+1 {
				t.Errorf("默认模式第 %d 条是 #%d，应按题号升序", i+1, it.ExternalNo)
			}
		}
	})

	_ = time.Now
}
