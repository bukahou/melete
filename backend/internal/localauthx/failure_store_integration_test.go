package localauthx

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/bukahou/gokit/localauth/storetest"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/gokit/localauth"
)

// 契约测试：拿模块作者写的一致性套件验我的 MySQL 实现。
//
// ⭐ 为什么这比自己写单测硬：套件里的断言【就是守卫编排所依赖的语义】
// （原子自增、Peek 零值不报错、Reset 幂等）。⛔ 过不了这里，守卫在这个
// 实现上的行为就是未定义的，不管我自己的单测多绿。
//
// 需要一个可写的 MySQL，设 MELETE_TEST_DSN 才运行：
//
//	MELETE_TEST_DSN="$MELETE_DB_DSN" go test ./internal/localauthx/ -run Integration -v
//
// ⚠️ 本套件与 study / question 那两个集成测试【不同】，指向开发库是安全的：
// 它只碰 login_failures 一张表，且每个子测试用一个独立的 scope
// （见 uniqueScope），⛔ 不 DELETE 全表、不碰 account/attempt/card。
// 那两个测试头上的「不要指向开发库」是因为它们会清空业务表 —— 理由不适用于这里。
func testDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("MELETE_TEST_DSN")
	if dsn == "" {
		t.Skip("未设 MELETE_TEST_DSN，跳过契约测试")
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Fatalf("连接测试库: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

var scopeSeq atomic.Int64

// uniqueScope 给每个子测试一个干净的桶。
//
// ⚠️ 套件要求 factory 每次返回【干净】的存储。用独立 scope 而不是清表：
// 清表会让并行运行的子测试互相踩，而独立 scope 天然隔离
// （scope 是复合主键的第一列）。⛔ 长度受 VARCHAR(8) 限制，故用短前缀。
func uniqueScope(t *testing.T, db *sqlx.DB) string {
	s := fmt.Sprintf("t%d", scopeSeq.Add(1))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM login_failures WHERE scope = ?`, s)
	})
	return s
}

func TestFailureStoreContractIntegration(t *testing.T) {
	db := testDB(t)
	storetest.RunFailureStoreTests(t, func(t *testing.T) localauth.FailureStore {
		s, err := NewFailureStore(db, uniqueScope(t, db))
		if err != nil {
			t.Fatalf("NewFailureStore: %v", err)
		}
		return s
	})
}

// ⛔⛔ 套件【刻意】不断言大小写，因为它不知道宿主用什么库 ——
// 模块注释原文：「两个都不调 = 这条边界没有被测」。
//
// melete 的答案：login_failures 与 users 同为 utf8mb4_general_ci，
// 所以账号维度必须【同桶】。⚠️ 这条一旦变红，含义不是"测试挂了"，
// 而是【变换大小写可以绕开账号退避】—— 见 tracker 的 COLLATE 技术债。
//
// ⚠️ 而它在 TiDB 上（默认 utf8mb4_bin，逐字节比）会变红，
// 这正是那条债还没还清的证据。⛔ TiDB 禁令期内验不了，但这条断言
// 会在迁库那天第一时间告诉我们。
func TestAccountKeysFoldCaseIntegration(t *testing.T) {
	db := testDB(t)
	s, err := NewFailureStore(db, uniqueScope(t, db))
	if err != nil {
		t.Fatalf("NewFailureStore: %v", err)
	}
	storetest.AssertKeysShareBucket(t, s, "Alice", "alice")

	// 前提自检：确认这个库的排序规则真的会折叠大小写。
	// ⚠️ 少了它，上面那条断言在一个逐字节比的库上会以"实现有 bug"的形式变红，
	// 而真正的原因是【排序规则不对】—— 两种原因的修法完全不同。
	var collation string
	if err := db.Get(&collation, `
		SELECT TABLE_COLLATION FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'login_failures'`); err != nil {
		t.Fatalf("查排序规则: %v", err)
	}
	t.Logf("login_failures 的排序规则 = %s", collation)
	if collation == "utf8mb4_bin" {
		t.Fatalf("⛔ login_failures 是 utf8mb4_bin（逐字节比）—— 变换大小写即可绕开账号退避")
	}
	_ = context.Background()
}
