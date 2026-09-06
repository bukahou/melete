package study

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"

	"github.com/bukahou/melete/backend/internal/study/scheduler"
)

// cardRow 是 card 表的一行。last_review 可空（新卡从未复习过）。
type cardRow struct {
	State      int8         `db:"state"`
	Due        time.Time    `db:"due"`
	Stability  float64      `db:"stability"`
	Difficulty float64      `db:"difficulty"`
	Reps       int          `db:"reps"`
	Lapses     int          `db:"lapses"`
	LastReview sql.NullTime `db:"last_review"`
}

func (r cardRow) toCard() scheduler.Card {
	c := scheduler.Card{
		State:      scheduler.State(r.State),
		Due:        r.Due,
		Stability:  r.Stability,
		Difficulty: r.Difficulty,
		Reps:       r.Reps,
		Lapses:     r.Lapses,
	}
	if r.LastReview.Valid {
		c.LastReview = r.LastReview.Time
	}
	return c
}

// loadCard 取一张卡的当前状态；没有行就返回新卡。
//
// ⚠️ 必须在事务内调用：读出来的状态马上要被算新值写回去，
// 读与写之间隔着一次调度运算，中间被并发插一脚会丢更新。
func loadCard(ctx context.Context, tx *sqlx.Tx, accountID userid.UserID, questionID int64) (scheduler.Card, error) {
	var row cardRow
	err := tx.GetContext(ctx, &row, `
		SELECT state, due, stability, difficulty, reps, lapses, last_review
		FROM card WHERE user_id = ? AND question_id = ? FOR UPDATE`,
		accountID, questionID)
	if errors.Is(err, sql.ErrNoRows) {
		return scheduler.NewCard(), nil
	}
	if err != nil {
		return scheduler.Card{}, fmt.Errorf("查询卡片 (%s,%d): %w", accountID, questionID, err)
	}
	return row.toCard(), nil
}

// saveCard 写回卡片状态。
//
// 用 ON DUPLICATE KEY UPDATE 而不是「先查后插」：主键是 (user_id, question_id)，
// 交给数据库幂等处理，省掉一次往返也省掉一个竞态。
// ⚠️ 用 VALUES() 而不是 MySQL 8.0.20+ 的 `AS new` 别名写法 —— 后者在 TiDB 上的
// 支持情况没核实过，而本项目生产库是 TiDB。VALUES() 两边都吃得下。
func saveCard(ctx context.Context, tx *sqlx.Tx, accountID userid.UserID, questionID int64, c scheduler.Card) error {
	var lastReview any
	if !c.LastReview.IsZero() {
		lastReview = c.LastReview.UTC()
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO card (user_id, question_id, state, due, stability, difficulty, reps, lapses, last_review)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE
			state=VALUES(state), due=VALUES(due), stability=VALUES(stability),
			difficulty=VALUES(difficulty), reps=VALUES(reps), lapses=VALUES(lapses),
			last_review=VALUES(last_review)`,
		accountID, questionID, int8(c.State), c.Due.UTC(),
		c.Stability, c.Difficulty, c.Reps, c.Lapses, lastReview)
	if err != nil {
		return fmt.Errorf("写入卡片 (%s,%d): %w", accountID, questionID, err)
	}
	return nil
}

// DueSummary 是「今天该复习多少」的统计，按题库分组。
type DueSummary struct {
	BankSlug string `db:"slug" json:"bankSlug"`
	// Due 已到期、等着复习的题数
	Due int `db:"due_count" json:"due"`
	// Unseen 这个题库里还没建卡（从没做过）的题数
	Unseen int `db:"unseen_count" json:"unseen"`
}

// LoadDueSummary 统计每个题库有多少题到期。
//
// 「到期」= card.due <= now。新卡（没有 card 行）不算到期 —— 它们是「没做过」，
// 是另一个入口；把两者混在一个数字里，会让「今天要复习 300 题」这种数字
// 变得毫无意义，而那正是间隔重复最劝退的失败模式。
func (s *service) LoadDueSummary(ctx context.Context, accountID userid.UserID) ([]DueSummary, error) {
	out := []DueSummary{}
	err := s.db.SelectContext(ctx, &out, `
		SELECT b.slug,
		       COALESCE(SUM(c.question_id IS NOT NULL AND c.due <= UTC_TIMESTAMP()), 0) AS due_count,
		       COALESCE(SUM(c.question_id IS NULL), 0)                                  AS unseen_count
		FROM bank b
		JOIN question q ON q.bank_id = b.id
		LEFT JOIN card c ON c.question_id = q.id AND c.user_id = ?
		GROUP BY b.id, b.slug
		ORDER BY b.slug`, accountID)
	if err != nil {
		return nil, fmt.Errorf("统计到期卡片: %w", err)
	}
	return out, nil
}
