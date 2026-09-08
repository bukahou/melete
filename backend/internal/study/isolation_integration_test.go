package study

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/userid"
)

// 🔴 跨账号隔离 —— localauth 阶段 2 的硬门槛。
//
// ⚠️ 为什么必须单独有这条：阶段 2 把账号 id 从 int64 换成 UUID，
// 编译器能抓住类型不匹配，⛔ **抓不住「查错了人的数据」**。
// `WHERE user_id = ?` 传错一个变量 —— 编译通过、返回空集、
// 界面显示「你还没有作答」，看起来完全正常。
//
// ⭐ 判据不是「返回 200」，是【返回的内容属于自己】。
// 所以每条断言都同时检查「看得到自己的」与「看不到别人的」——
// 只查前者的话，一个恒返回空集的实现也会通过。
//
// ⚠️ 本测试与同包的 record_integration_test 不同：它【不清全表】，
// 只造自己的账号、只删自己的行 ⇒ 指向开发库是安全的。
func TestCrossAccountIsolationIntegration(t *testing.T) {
	dsn := os.Getenv("MELETE_TEST_DSN")
	if dsn == "" {
		t.Skip("未设 MELETE_TEST_DSN，跳过")
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Fatalf("连接: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	// 借用库里已有的题目，⛔ 不新建 —— 新建题目会污染题库统计。
	var qids []int64
	if err := db.Select(&qids, `SELECT id FROM question ORDER BY id LIMIT 2`); err != nil || len(qids) < 2 {
		t.Skipf("测试库里题目不足（%d 道），跳过", len(qids))
	}
	var slug string
	if err := db.Get(&slug, `SELECT b.slug FROM bank b JOIN question q ON q.bank_id=b.id WHERE q.id=?`, qids[0]); err != nil {
		t.Fatal(err)
	}

	alice := mkIsolationUser(t, db, "iso_a")
	bob := mkIsolationUser(t, db, "iso_b")
	svc := NewService(db, question.NewMySQLRepository(db))

	// Alice 答第一题，Bob 答第二题 —— ⭐ 故意答【不同】的题，
	// 这样「串号」会表现为看到一道自己没做过的题，而不是数字对不上。
	mustRecord(t, ctx, svc, alice, qids[0], "A")
	mustRecord(t, ctx, svc, bob, qids[1], "B")

	t.Run("progress 只算自己的", func(t *testing.T) {
		pa, err := svc.LoadProgress(ctx, alice, slug)
		if err != nil {
			t.Fatal(err)
		}
		if pa.SeenCount != 1 {
			t.Fatalf("Alice 应恰好做过 1 题，得到 %d —— 要么串到了 Bob 的，要么自己的没查到", pa.SeenCount)
		}
	})

	t.Run("最近作答不含别人的题", func(t *testing.T) {
		for name, pair := range map[string]struct {
			who  userid.UserID
			mine int64
			nots int64
		}{
			"Alice": {alice, qids[0], qids[1]},
			"Bob":   {bob, qids[1], qids[0]},
		} {
			t.Run(name, func(t *testing.T) {
				var got []int64
				if err := db.Select(&got,
					`SELECT question_id FROM attempt WHERE user_id = ?`, pair.who); err != nil {
					t.Fatal(err)
				}
				var sawMine, sawOther bool
				for _, q := range got {
					if q == pair.mine {
						sawMine = true
					}
					if q == pair.nots {
						sawOther = true
					}
				}
				// ⭐ 两条都要 —— 只查后者的话，恒空的实现会通过。
				if !sawMine {
					t.Fatalf("%s 看不到自己刚做的题 —— 查询根本没匹配上（BINARY(16) 列拿错类型比对的典型症状）", name)
				}
				if sawOther {
					t.Fatalf("🔴 %s 看到了别人的作答 —— 跨账号串号", name)
				}
			})
		}
	})

	t.Run("卡片按人隔离", func(t *testing.T) {
		var n int
		if err := db.Get(&n, `SELECT COUNT(*) FROM card WHERE user_id=? AND question_id=?`, alice, qids[1]); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("🔴 Alice 名下出现了 Bob 那道题的卡片")
		}
		if err := db.Get(&n, `SELECT COUNT(*) FROM card WHERE user_id=? AND question_id=?`, alice, qids[0]); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("Alice 自己那道题的卡片应有 1 张，得到 %d", n)
		}
	})
}

func mustRecord(t *testing.T, ctx context.Context, svc Service, who userid.UserID, q int64, chosen string) {
	t.Helper()
	if _, err := svc.RecordAttempt(ctx, Attempt{
		AccountID: who, QuestionID: q, Chosen: chosen, Rating: ratingPtr(3),
	}); err != nil {
		t.Fatalf("记录作答: %v", err)
	}
}

// mkIsolationUser 造一个测试账号，并在测试结束时【只删自己的行】。
func mkIsolationUser(t *testing.T, db *sqlx.DB, prefix string) userid.UserID {
	t.Helper()
	id, err := userid.New()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := userid.Encode(id)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()%1e9)
	if _, err := db.Exec(`INSERT INTO users (id,username,status,created_at,updated_at,display_name)
	                      VALUES (?,?,1,UTC_TIMESTAMP(),UTC_TIMESTAMP(),?)`, bin, name, name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// ⛔ 只删本测试造出来的东西，⚠️ 不碰任何别的行。
		_, _ = db.Exec(`DELETE FROM attempt WHERE user_id=?`, bin)
		_, _ = db.Exec(`DELETE FROM card WHERE user_id=?`, bin)
		_, _ = db.Exec(`DELETE FROM users WHERE id=?`, bin)
	})
	return userid.UserID(id)
}
