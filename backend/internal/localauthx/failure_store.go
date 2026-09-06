package localauthx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bukahou/gokit/localauth"
	"github.com/jmoiron/sqlx"
)

// failureStore 是 localauth.FailureStore 的 MySQL 实现。
//
// ⭐ 一张表两个 scope：模块要两个独立实例（IP 维度 / 账号维度），
// 但它不关心它们是不是同一张表 —— 隔离由 scope 列 + 复合主键保证。
//
// ⚠️⚠️ 账号维度的实例，其表的排序规则【必须与 users.username 一致】。
// 否则 Alice / alice 落进不同的桶 ⇒ 变换大小写即可绕开账号退避。
// melete 两张表同为 utf8mb4_general_ci（建表时钉死，见 db/schema.sql），
// 由 AssertKeysShareBucket 断言守住 —— ⛔ 那条断言不是可选的装饰，
// 模块把它做成可选是因为【它不知道宿主用什么库】，不是因为可以不测。
type failureStore struct {
	db    *sqlx.DB
	scope string
}

// 编译期契约检查 —— ⭐ 这一行是本包存在的理由之一。
// 接口一旦变化，构建立刻失败，而不是等到运行时某条路径被走到。
var _ localauth.FailureStore = (*failureStore)(nil)

// NewFailureStore 建一个绑定到某个 scope 的失败计数存储。
//
// scope 取 "ip" 或 "account"（测试可用别的），⛔ 不得留空 ——
// 空 scope 会让两个维度的计数落进同一个桶，而那正是这张表要分开的东西。
func NewFailureStore(db *sqlx.DB, scope string) (localauth.FailureStore, error) {
	if scope == "" {
		return nil, errors.New("failure store 的 scope 不得为空")
	}
	return &failureStore{db: db, scope: scope}, nil
}

func (s *failureStore) Peek(ctx context.Context, key string) (localauth.FailureState, error) {
	var row struct {
		Fails   int       `db:"fails"`
		FirstAt time.Time `db:"first_at"`
		LastAt  time.Time `db:"last_at"`
	}
	err := s.db.GetContext(ctx, &row,
		`SELECT fails, first_at, last_at FROM login_failures WHERE scope = ? AND fail_key = ?`,
		s.scope, key)
	if errors.Is(err, sql.ErrNoRows) {
		// ⛔ 不是错误：没有失败记录是绝大多数请求的正常状态。
		return localauth.FailureState{}, nil
	}
	if err != nil {
		return localauth.FailureState{}, fmt.Errorf("查询失败计数 %s/%q: %w", s.scope, key, err)
	}
	return localauth.FailureState{Count: row.Fails, FirstFailAt: row.FirstAt, LastFailAt: row.LastAt}, nil
}

// Bump 原子自增，返回【自增之后】的状态。
//
// ⚠️ 为什么用事务而不是「一条 upsert + 一次读」：
// upsert 本身是原子的，但紧随其后的那次 SELECT 不在同一个原子区间里 ——
// 并发时它可能读到别人又自增过的值，于是返回的 Count 不是"我这一次"的结果。
// 而模块的判定依赖的正是自增后的值（见 FailureStore 的注释）。
//
// 放进事务后，upsert 拿到该行的排他锁，并发的 Bump 会阻塞到本事务提交，
// 所以这里的 SELECT 读到的就是我这一次的状态。⭐ 代价是一次事务往返，
// 而它只发生在【登录失败】路径上，不在正常登录路径上。
//
// ⛔ 不用「读出来 +1 再写回去」：那是模块注释点名的读-改-写竞态，
// N 个并发请求可能都读到同一个旧值、最终只 +1 —— 计数丢失 = 退避形同虚设。
func (s *failureStore) Bump(ctx context.Context, key string, now time.Time) (localauth.FailureState, error) {
	var out localauth.FailureState
	err := runInTx(ctx, s.db, func(tx *sqlx.Tx) error {
		// first_at 只在插入时写入，⛔ ON DUPLICATE 分支不得碰它 ——
		// 它记的是"这一轮失败从什么时候开始"，被刷新就失去意义了。
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO login_failures (scope, fail_key, fails, first_at, last_at)
			VALUES (?, ?, 1, ?, ?)
			ON DUPLICATE KEY UPDATE fails = fails + 1, last_at = VALUES(last_at)`,
			s.scope, key, now, now); err != nil {
			return fmt.Errorf("自增失败计数: %w", err)
		}
		var row struct {
			Fails   int       `db:"fails"`
			FirstAt time.Time `db:"first_at"`
			LastAt  time.Time `db:"last_at"`
		}
		if err := tx.GetContext(ctx, &row,
			`SELECT fails, first_at, last_at FROM login_failures WHERE scope = ? AND fail_key = ?`,
			s.scope, key); err != nil {
			return fmt.Errorf("回读失败计数: %w", err)
		}
		out = localauth.FailureState{Count: row.Fails, FirstFailAt: row.FirstAt, LastFailAt: row.LastAt}
		return nil
	})
	return out, err
}

// Reset 清掉某个键的失败记录。幂等：不存在也返回 nil。
//
// ⭐ 登录成功时调用它。⚠️ 模块把 Reset 放在账号退避判定【之前】是有意的
// （见 guard.go ④）：否则退避期内合法用户输对密码也走不到 Reset，
// 计数只能等窗口自然过期，而攻击者一过窗口补一次失败就能重新压住。
func (s *failureStore) Reset(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM login_failures WHERE scope = ? AND fail_key = ?`, s.scope, key); err != nil {
		return fmt.Errorf("重置失败计数 %s/%q: %w", s.scope, key, err)
	}
	return nil
}

// runInTx 跑一个事务，出错回滚。
//
// ⚠️ rollback 的错误【刻意吞掉】：此刻已经有一个更值得报的业务错误，
// 而回滚失败几乎总是"连接已断"的次生现象。两个错误一起往上抛只会
// 让真正的原因更难找。
func runInTx(ctx context.Context, db *sqlx.DB, fn func(*sqlx.Tx) error) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务: %w", err)
	}
	return nil
}
