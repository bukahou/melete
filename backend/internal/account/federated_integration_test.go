package account

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

// 🔴 联邦账号确立（Akasha OIDC 回调之后那一步）的集成测试。
//
// ⚠️⚠️ 这个文件补的是 2026-09-07 评审会查出的最大缺口：
//
//	EstablishFederated 在阶段 2（54b8beb，09-06 18:38）被【整段重写】——
//	从 `INSERT ... ON DUPLICATE KEY UPDATE`（对 account.akasha_sub 天然幂等）
//	换成「同一事务里建 users + identities」+ 手写并发首登兜底。
//	而库里两个真实用户建于 09-02 / 09-04 —— ⛔ 都是被现在已不存在的代码写进去的。
//	⇒ 新路径的输入输出两端，在生产数据上都是零样本。
//
// ⭐ 而重写恰好【去掉了】原来那个天然幂等：两表同事务之后，
// 「两个请求同时为同一个人建号」不再自动收敛，必须显式补重查兜底。
// 那段兜底写完至今零执行 —— 本测试就是来执行它的。
//
// ⚠️ 本测试【非破坏性】：只造自己的账号，t.Cleanup 只删自己造的行。

func testDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("MELETE_TEST_DSN")
	if dsn == "" {
		t.Skip("未设 MELETE_TEST_DSN，跳过")
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Fatalf("连接: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// cleanupSubject 只删本测试用这个 subject 造出来的账号。
func cleanupSubject(t *testing.T, db *sqlx.DB, subject string) {
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE u FROM users u JOIN identities i ON i.user_id=u.id
		                WHERE i.provider='akasha' AND i.subject=?`, subject)
		_, _ = db.Exec(`DELETE FROM identities WHERE provider='akasha' AND subject=?`, subject)
	})
}

func TestEstablishFederatedIntegration(t *testing.T) {
	db := testDB(t)
	repo := NewMySQLRepository(db)
	ctx := context.Background()
	sub := fmt.Sprintf("test-sub-%d", time.Now().UnixNano())
	cleanupSubject(t, db, sub)

	t.Run("首次：建 users + identities，两者同生", func(t *testing.T) {
		a, err := repo.EstablishFederated(ctx, "akasha", sub, "测试用户A")
		if err != nil {
			t.Fatalf("首次确立失败: %v", err)
		}
		if _, err := userid.Encode(a.ID); err != nil {
			t.Fatalf("返回的 id 不是 canonical UUID: %q", a.ID)
		}
		// ⛔ 两表必须同生 —— 只有 users 没有 identities = 孤儿账号，
		// 下次登录会再建一个，同一个人变成两个账号。
		var n int
		if err := db.Get(&n, `SELECT COUNT(*) FROM identities WHERE provider='akasha' AND subject=?`, sub); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("identities 行数 = %d，期望 1", n)
		}
		if err := db.Get(&n, `SELECT COUNT(*) FROM users WHERE id=?`, userid.UserID(a.ID)); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("users 行数 = %d，期望 1", n)
		}
		// ⚠️ 中文 display 清洗后为空 ⇒ 必须回落到 provider 前缀，
		// ⛔ 而不是写入空 username（users.username 是 NOT NULL）。
		if a.Username == "" {
			t.Fatal("username 为空 —— 中文 display 清洗后的回落没有生效")
		}
		t.Logf("  建号成功 id=%s username=%s display=%s", a.ID, a.Username, a.DisplayName())
	})

	t.Run("再次：找到同一个账号，⛔ 不新建", func(t *testing.T) {
		a, err := repo.EstablishFederated(ctx, "akasha", sub, "测试用户A")
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.Get(&n, `SELECT COUNT(*) FROM identities WHERE provider='akasha' AND subject=?`, sub); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("🔴 第二次登录又建了一个账号：identities 行数 = %d", n)
		}
		var uid []byte
		if err := db.Get(&uid, `SELECT user_id FROM identities WHERE provider='akasha' AND subject=?`, sub); err != nil {
			t.Fatal(err)
		}
		got, _ := userid.Decode(uid)
		if got != a.ID {
			t.Fatalf("🔴 返回的 id 与库里的不一致：%s vs %s", a.ID, got)
		}
	})
}

// 🔴🔴 并发首登 —— 这是重写【去掉】的那个天然幂等，靠手写兜底补回来的。
//
// ⚠️ 旧实现 `INSERT ... ON DUPLICATE KEY UPDATE` 天然收敛：
// 两个请求同时来，第二个自动变成 UPDATE。
// 新实现是两表同事务，撞唯一索引会整体回滚 ⇒ 必须重查复用赢家建好的账号，
// ⛔ 否则用户会拿到一个 500，或者更糟 —— 两个账号。
//
// ⭐ 判据是【只建出一个账号】且所有并发调用拿到【同一个 id】。
func TestEstablishFederatedConcurrentFirstLoginIntegration(t *testing.T) {
	db := testDB(t)
	repo := NewMySQLRepository(db)
	sub := fmt.Sprintf("test-race-%d", time.Now().UnixNano())
	cleanupSubject(t, db, sub)

	const n = 8
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // 尽量让它们真的同时进
			a, err := repo.EstablishFederated(context.Background(), "akasha", sub, "测试用户B")
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = a.ID
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("🔴 第 %d 个并发请求失败：%v —— 并发首登不该把 500 抛给用户", i, err)
		}
	}
	uniq := map[string]int{}
	for _, id := range ids {
		if id != "" {
			uniq[id]++
		}
	}
	if len(uniq) != 1 {
		t.Fatalf("🔴 %d 个并发首登产生了 %d 个不同的账号 id：%v\n"+
			"⚠️ 同一个人被建成了多个账号 —— 重写去掉了 ON DUPLICATE KEY UPDATE 的"+
			"天然幂等，而手写的重查兜底没有生效", n, len(uniq), uniq)
	}
	var rows int
	if err := db.Get(&rows, `SELECT COUNT(*) FROM identities WHERE provider='akasha' AND subject=?`, sub); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("🔴 identities 行数 = %d，期望 1", rows)
	}
	for id, c := range uniq {
		t.Logf("  %d 个并发首登 → 恰好 1 个账号 %s（%d 次命中）", n, id, c)
	}
}
