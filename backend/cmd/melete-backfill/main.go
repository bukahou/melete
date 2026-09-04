// melete-backfill 把历史作答重放成 FSRS 卡片状态。
//
//	go run ./cmd/melete-backfill              # 只报告，不写库（默认）
//	go run ./cmd/melete-backfill --apply      # 真的写
//
// # 为什么可以整表重建
//
// card 是【派生状态】—— 它的每一个字段都能由 attempt 序列重放出来。
// attempt 才是事实（learning-flows.md：学习侧只存事实，不存状态；
// card 是 P3 唯一合法的例外，而它仍然是派生的）。
// 所以重建是无损的，也因此这个命令是幂等的：跑一次和跑十次结果相同。
//
// # 与线上调度的一处差异，是【更准】而不是不一致
//
// 线上是在作答那一刻用 time.Now() 调度；重放用的是 attempt.created_at。
// 对已经发生的历史，后者才是正确的复习时刻 —— FSRS 按实际经过的时间算间隔，
// 拿今天的时间去重放三个月前的作答会算出完全不同的稳定性。
//
// ⚠️ 因此重放【会改变】已有卡片的 due，这是有意的。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/study/scheduler"
)

type attemptRow struct {
	AccountID  int64         `db:"account_id"`
	QuestionID int64         `db:"question_id"`
	Correct    bool          `db:"correct"`
	Rating     sql.NullInt64 `db:"rating"`
	DurationMs sql.NullInt64 `db:"duration_ms"`
	CreatedAt  time.Time     `db:"created_at"`
}

type key struct{ account, question int64 }

func main() {
	dsn := flag.String("dsn", os.Getenv("MELETE_DB_DSN"), "数据库连接串，默认取 MELETE_DB_DSN")
	apply := flag.Bool("apply", false, "真的写库；不带这个参数只报告")
	flag.Parse()

	if *dsn == "" {
		exit("未提供 DSN。设置 MELETE_DB_DSN 或用 --dsn\n" +
			"  凭证类无默认值是有意为之 —— 忘配就失败，优于默默连错库")
	}
	// parseTime 与 UTC：card.due / attempt.created_at 全库按 UTC 存
	// （2026-09-03 时区迁移）。少了这两个参数会把 UTC 当本地时间读，
	// 而 JST 与 UTC 差 9 小时 —— 重放出来的间隔会整体偏移一天，且不报错。
	full := *dsn
	if !hasParams(full) {
		full += "?"
	} else {
		full += "&"
	}
	full += "parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27"

	db, err := sqlx.Connect("mysql", full)
	if err != nil {
		exit("连接数据库: " + err.Error())
	}
	defer db.Close()
	ctx := context.Background()

	// 题面字数与「有没有参考答案」—— 评分纠正要用，一次全量拉进内存。
	// 题库总共 1545 道，两张小表，比逐题查一次往返划算得多。
	textLen := map[int64]int{}
	if err := scanPairs(ctx, db, `
		SELECT q.id, CHAR_LENGTH(q.stem) + COALESCE(
		         (SELECT SUM(CHAR_LENGTH(c.body)) FROM choice c WHERE c.question_id = q.id), 0)
		FROM question q`, textLen); err != nil {
		exit(err.Error())
	}
	hasRef := map[int64]int{}
	if err := scanPairs(ctx, db, `
		SELECT DISTINCT question_id, 1 FROM answer_claim
		WHERE source IN ('ai_verdict','community_vote','bank_label')`, hasRef); err != nil {
		exit(err.Error())
	}

	// ⚠️ 必须按 created_at 排序重放 —— FSRS 的每一步都依赖上一步的状态，
	// 顺序错了结果就是错的，而且不会有任何报错。
	// 同一毫秒内的多条用 id 兜底，让重放可复现。
	var rows []attemptRow
	if err := db.SelectContext(ctx, &rows, `
		SELECT account_id, question_id, correct, rating, duration_ms, created_at
		FROM attempt ORDER BY account_id, question_id, created_at, id`); err != nil {
		exit("读取作答记录: " + err.Error())
	}

	sch := scheduler.New()
	cards := map[key]scheduler.Card{}
	var replayed, corrected, skipped int

	for _, a := range rows {
		if !a.Rating.Valid {
			// 没有自评就没有 FSRS 的输入信号。跳过而不是猜一个值 ——
			// 猜出来的评分会污染稳定性，而且事后分不清哪些是猜的。
			skipped++
			continue
		}
		k := key{a.AccountID, a.QuestionID}
		c, ok := cards[k]
		if !ok {
			c = scheduler.NewCard()
		}
		var dur *int
		if a.DurationMs.Valid {
			v := int(a.DurationMs.Int64)
			dur = &v
		}
		corr := scheduler.EffectiveRating(int(a.Rating.Int64), scheduler.Signals{
			Correct:      a.Correct,
			HasReference: hasRef[a.QuestionID] == 1,
			DurationMs:   dur,
			StemChars:    textLen[a.QuestionID],
		})
		if corr.Adjusted() {
			corrected++
		}
		cards[k] = sch.Next(c, corr.Effective, a.CreatedAt.UTC())
		replayed++
	}

	// 已有卡片：分成「会被重建的」与「孤儿」（有卡片但没有任何作答）。
	// 孤儿不该存在（卡片只在作答时创建），但真出现了要报出来，不静默删。
	existing := map[key]bool{}
	var ex []struct {
		AccountID  int64 `db:"account_id"`
		QuestionID int64 `db:"question_id"`
	}
	if err := db.SelectContext(ctx, &ex, `SELECT account_id, question_id FROM card`); err != nil {
		exit("读取现有卡片: " + err.Error())
	}
	var orphans []key
	for _, e := range ex {
		k := key{e.AccountID, e.QuestionID}
		existing[k] = true
		if _, ok := cards[k]; !ok {
			orphans = append(orphans, k)
		}
	}

	var created, updated int
	for k := range cards {
		if existing[k] {
			updated++
		} else {
			created++
		}
	}

	fmt.Printf("作答记录    %d 条（重放 %d，跳过 %d —— 无自评）\n", len(rows), replayed, skipped)
	fmt.Printf("评分纠正    %d 次\n", corrected)
	fmt.Printf("卡片        新建 %d，覆盖 %d，孤儿 %d\n", created, updated, len(orphans))
	for _, k := range orphans {
		fmt.Printf("  ⚠️ 孤儿卡片 account=%d question=%d（没有任何作答记录）\n", k.account, k.question)
	}
	if len(cards) > 0 {
		printSample(cards, sch)
	}

	if !*apply {
		fmt.Println("\n（未加 --apply，以上只是预演，一个字也没写）")
		return
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		exit("开启事务: " + err.Error())
	}
	defer tx.Rollback() //nolint:errcheck

	for _, k := range orphans {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM card WHERE account_id=? AND question_id=?`, k.account, k.question); err != nil {
			exit("删除孤儿卡片: " + err.Error())
		}
	}
	for k, c := range cards {
		var lastReview any
		if !c.LastReview.IsZero() {
			lastReview = c.LastReview.UTC()
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO card (account_id, question_id, state, due, stability, difficulty, reps, lapses, last_review)
			VALUES (?,?,?,?,?,?,?,?,?)
			ON DUPLICATE KEY UPDATE
				state=VALUES(state), due=VALUES(due), stability=VALUES(stability),
				difficulty=VALUES(difficulty), reps=VALUES(reps), lapses=VALUES(lapses),
				last_review=VALUES(last_review)`,
			k.account, k.question, int8(c.State), c.Due.UTC(),
			c.Stability, c.Difficulty, c.Reps, c.Lapses, lastReview); err != nil {
			exit("写入卡片: " + err.Error())
		}
	}
	if err := tx.Commit(); err != nil {
		exit("提交: " + err.Error())
	}
	fmt.Printf("\n✓ 已写入 %d 张卡片，删除孤儿 %d 张\n", len(cards), len(orphans))
}

// printSample 挑几张卡片打出来，让人能一眼看出重放结果是否合理。
func printSample(cards map[key]scheduler.Card, sch *scheduler.Scheduler) {
	ks := make([]key, 0, len(cards))
	for k := range cards {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool {
		if ks[i].account != ks[j].account {
			return ks[i].account < ks[j].account
		}
		return ks[i].question < ks[j].question
	})
	fmt.Println("\n抽样（最多 5 张）：")
	now := time.Now().UTC()
	for i, k := range ks {
		if i >= 5 {
			break
		}
		c := cards[k]
		fmt.Printf("  account=%d question=%d  %-10v reps=%d lapses=%d S=%.2f D=%.2f  due=%s  此刻 R=%.3f\n",
			k.account, k.question, c.State, c.Reps, c.Lapses, c.Stability, c.Difficulty,
			c.Due.Format("2006-01-02 15:04"), sch.Retrievability(c, now))
	}
}

func scanPairs(ctx context.Context, db *sqlx.DB, query string, out map[int64]int) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("查询 %.40s…: %w", query, err)
	}
	defer rows.Close()
	for rows.Next() {
		var k int64
		var v int
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		out[k] = v
	}
	return rows.Err()
}

func hasParams(dsn string) bool {
	for i := len(dsn) - 1; i >= 0; i-- {
		if dsn[i] == '?' {
			return true
		}
		if dsn[i] == '/' {
			return false
		}
	}
	return false
}

func exit(msg string) {
	fmt.Fprintln(os.Stderr, "✗ "+msg)
	os.Exit(1)
}
