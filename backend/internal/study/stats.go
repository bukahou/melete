package study

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Progress 是一个账号在某题库上的总体进度。
type Progress struct {
	BankSlug      string `db:"bank_slug"`
	QuestionCount int    `db:"question_count"`
	SeenCount     int    `db:"seen_count"`
	CorrectCount  int    `db:"correct_count"`
	WrongCount    int    `db:"wrong_count"`
	UnsureCount   int    `db:"unsure_count"`
	AttemptCount  int    `db:"attempt_count"`
	// DueCount 已到期、等着复习的题数（FSRS）。⛔ 不含从没做过的题。
	DueCount     int          `db:"due_count"`
	LastActiveAt sql.NullTime `db:"last_active_at"`
}

// TagStat 是某个标签下的正确率。
type TagStat struct {
	TagID   int64          `db:"tag_id"`
	Type    string         `db:"type"`
	Value   string         `db:"value"`
	I18n    sql.NullString `db:"i18n"`
	Total   int            `db:"total"`
	Correct int            `db:"correct"`
}

// Resume 是「继续学习」的两条轨道。两条都从 attempt 推导，不落任何断点状态。
type Resume struct {
	BankSlug   string
	Sequential SequentialCursor
	Focus      *FocusCursor // nil = 从未做过专项
}

// SequentialCursor 是顺序进度：题号最小的没做过的题。QuestionID 无效 = 已全部做过。
type SequentialCursor struct {
	QuestionID sql.NullInt64  `db:"question_id"`
	ExternalNo sql.NullInt64  `db:"external_no"`
	Stem       sql.NullString `db:"stem"`
	DoneCount  int
	TotalCount int
	LastAt     sql.NullTime
}

// FocusCursor 是上次专项：最近一条 context.mode ∉ {unseen, all} 的作答的出处。
type FocusCursor struct {
	Mode   string
	TagID  *int64
	Tag    *TagRef
	Done   *int // 集合内做过的题数；只有 tag / contested 有意义
	Total  int  // 集合当前大小
	LastAt time.Time
}

// TagRef 是 focus 引用的标签（够前端显示即可，不引入 bank 域的完整类型）。
type TagRef struct {
	ID    int64          `db:"id"`
	Type  string         `db:"type"`
	Value string         `db:"value"`
	I18n  sql.NullString `db:"i18n"`
}

// drillContext 与 API 契约 DrillContext 同形；这里解码 attempt.context 列。
type drillContext struct {
	Mode  string `json:"mode"`
	TagID *int64 `json:"tagId,omitempty"`
}

// sequentialModes 不构成「上次专项」的出处。
//
// ⚠️ due 也在里面，虽然它不是「顺序刷」——
// 复习队列在首页与学习台都有【独立且更醒目】的入口（「复习 N 题」），
// 若它也算专项，每复习一轮「上次专项」就变成「该复习的」，
// 把你真正在攻的那个专项挤掉，而且与旁边的复习入口重复。
// ⛔「上次专项」要回答的是「我上次在攻哪一块」，复习队列不回答这个。
var sequentialModes = map[string]bool{"unseen": true, "all": true, "due": true, "": true}

// latestAttempt 取每题的最近一次作答。
//
// **每题只算最近一次**是这几个统计的共同前提：若把历次作答全算进去，
// 反复重做同一道错题会让正确率越刷越低 —— 与「我在进步」的直觉正好相反。
// 用 id 最大值定位「最近」，比 created_at 可靠（同秒内多次作答不会并列）。
const latestAttempt = `
	SELECT a.* FROM attempt a
	JOIN (
		SELECT question_id, MAX(id) AS max_id
		FROM attempt WHERE account_id = ?
		GROUP BY question_id
	) m ON m.max_id = a.id`

// LoadProgress 汇总某题库的学习进度。
func (s *service) LoadProgress(ctx context.Context, accountID int64, slug string) (*Progress, error) {
	var p Progress
	// 聚合先按 bank_id 分组算好再 LEFT JOIN 回题库行 —— 这样题库行【永远存在】，
	// 没有作答只是各项为 0。
	//
	// ⚠️ 曾经写成 `LEFT JOIN (最近作答) ON 1 = 1` 再用 WHERE 过滤：账号一旦在
	// **任何**题库有作答，查另一个题库时所有行都会被那个 WHERE 滤光 → 零行 →
	// ErrNotFound。0 作答时反而正常，所以单题库时期一直没暴露，第二个题库
	// 加上、用户做了第一道题的那一刻首页就整页 500 了。
	err := s.db.GetContext(ctx, &p, `
		SELECT
		  b.slug AS bank_slug,
		  (SELECT COUNT(*) FROM question q WHERE q.bank_id = b.id) AS question_count,
		  COALESCE(agg.seen_count, 0)    AS seen_count,
		  COALESCE(agg.correct_count, 0) AS correct_count,
		  COALESCE(agg.wrong_count, 0)   AS wrong_count,
		  COALESCE(agg.unsure_count, 0)  AS unsure_count,
		  (SELECT COUNT(*) FROM attempt a2
		     JOIN question q2 ON q2.id = a2.question_id AND q2.bank_id = b.id
		   WHERE a2.account_id = ?)      AS attempt_count,
		  -- FSRS 到期数。⚠️ 从没做过的题没有 card 行，不算到期 —— 它属于
		  -- 「没做过」那个入口。两者混进一个数字，「今天要复习 300 题」就没有意义。
		  (SELECT COUNT(*) FROM card c
		     JOIN question q3 ON q3.id = c.question_id AND q3.bank_id = b.id
		   WHERE c.account_id = ? AND c.due <= UTC_TIMESTAMP()) AS due_count,
		  agg.last_active_at
		FROM bank b
		LEFT JOIN (
		  SELECT q.bank_id,
		         COUNT(*)                 AS seen_count,
		         SUM(la.correct = 1)      AS correct_count,
		         SUM(la.correct = 0)      AS wrong_count,
		         SUM(la.rating <= 2)      AS unsure_count,
		         MAX(la.created_at)       AS last_active_at
		  FROM (`+latestAttempt+`) la
		  JOIN question q ON q.id = la.question_id
		  GROUP BY q.bank_id
		) agg ON agg.bank_id = b.id
		WHERE b.slug = ?`, accountID, accountID, accountID, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("统计进度: %w", err)
	}
	return &p, nil
}

// LoadTagStats 按标签聚合正确率 —— 「我哪里不会」的数据来源。
func (s *service) LoadTagStats(ctx context.Context, accountID int64, slug, tagType string, minAttempts int) ([]TagStat, error) {
	out := []TagStat{}
	err := s.db.SelectContext(ctx, &out, `
		SELECT t.id AS tag_id, t.type, t.value, t.i18n,
		       COUNT(*) AS total, SUM(la.correct) AS correct
		FROM (`+latestAttempt+`) la
		JOIN question q      ON q.id = la.question_id
		JOIN bank b          ON b.id = q.bank_id AND b.slug = ?
		JOIN question_tag qt ON qt.question_id = q.id
		JOIN tag t           ON t.id = qt.tag_id AND t.type = ?
		GROUP BY t.id
		HAVING total >= ?
		ORDER BY (SUM(la.correct) / COUNT(*)) ASC, total DESC`,
		accountID, slug, tagType, minAttempts)
	if err != nil {
		return nil, fmt.Errorf("统计标签正确率: %w", err)
	}
	return out, nil
}

// LoadResume 组装「继续学习」的两条轨道。
func (s *service) LoadResume(ctx context.Context, accountID int64, slug string) (*Resume, error) {
	var bankID int64
	err := s.db.GetContext(ctx, &bankID, `SELECT id FROM bank WHERE slug = ?`, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询题库 %q: %w", slug, err)
	}
	r := &Resume{BankSlug: slug}

	// ---- 顺序进度 ----
	err = s.db.GetContext(ctx, &r.Sequential, `
		SELECT q.id AS question_id, q.external_no, q.stem
		FROM question q
		WHERE q.bank_id = ?
		  AND NOT EXISTS (SELECT 1 FROM attempt a WHERE a.account_id = ? AND a.question_id = q.id)
		ORDER BY q.external_no LIMIT 1`, bankID, accountID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("查询顺序断点: %w", err)
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM question WHERE bank_id = ?),
		       (SELECT COUNT(DISTINCT a.question_id) FROM attempt a
		          JOIN question q ON q.id = a.question_id AND q.bank_id = ?
		         WHERE a.account_id = ?),
		       (SELECT MAX(a.created_at) FROM attempt a
		          JOIN question q ON q.id = a.question_id AND q.bank_id = ?
		         WHERE a.account_id = ?
		           AND (a.context IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(a.context, '$.mode')) IN ('unseen', 'all')))`,
		bankID, bankID, accountID, bankID, accountID,
	).Scan(&r.Sequential.TotalCount, &r.Sequential.DoneCount, &r.Sequential.LastAt)
	if err != nil {
		return nil, fmt.Errorf("统计顺序进度: %w", err)
	}

	// ---- 上次专项 ----
	var raw string
	var at time.Time
	err = s.db.QueryRowContext(ctx, `
		SELECT a.context, a.created_at
		FROM attempt a JOIN question q ON q.id = a.question_id
		WHERE a.account_id = ? AND q.bank_id = ? AND a.context IS NOT NULL
		  AND JSON_UNQUOTE(JSON_EXTRACT(a.context, '$.mode')) NOT IN ('unseen', 'all')
		ORDER BY a.id DESC LIMIT 1`, accountID, bankID).Scan(&raw, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询上次专项: %w", err)
	}
	var dc drillContext
	if json.Unmarshal([]byte(raw), &dc) != nil || sequentialModes[dc.Mode] {
		return r, nil // 坏数据当作没有专项，不让首页因一行脏 JSON 报错
	}
	f := &FocusCursor{Mode: dc.Mode, TagID: dc.TagID, LastAt: at}
	if err := s.fillFocusSize(ctx, accountID, bankID, f); err != nil {
		return nil, err
	}
	r.Focus = f
	return r, nil
}

// fillFocusSize 算出专项集合的当前大小（与做过的数量）。
// 集合定义须与 question 域的 mode 过滤、bank 域的 contested 统计**同一口径**，
// 否则首页写「EC2 27/490」、点进去却是 489 题。
func (s *service) fillFocusSize(ctx context.Context, accountID, bankID int64, f *FocusCursor) error {
	switch f.Mode {
	case "tag":
		if f.TagID == nil {
			return nil
		}
		var t TagRef
		if err := s.db.GetContext(ctx, &t, `SELECT id, type, value, i18n FROM tag WHERE id = ?`, *f.TagID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // 标签已被重跑富化清掉：专项仍显示，但没有名字与规模
			}
			return fmt.Errorf("查询专项标签: %w", err)
		}
		f.Tag = &t
		var done int
		err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*),
			       SUM(EXISTS (SELECT 1 FROM attempt a WHERE a.account_id = ? AND a.question_id = q.id))
			FROM question_tag qt JOIN question q ON q.id = qt.question_id
			WHERE qt.tag_id = ? AND q.bank_id = ?`, accountID, *f.TagID, bankID).Scan(&f.Total, &done)
		if err != nil {
			return fmt.Errorf("统计专项规模: %w", err)
		}
		f.Done = &done
	case "contested":
		// 与 bank.LoadStats 的 contested_count 同一定义：题库标注 ≠ 社区投票
		var done int
		err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*),
			       SUM(EXISTS (SELECT 1 FROM attempt a WHERE a.account_id = ? AND a.question_id = q.id))
			FROM question q
			WHERE q.bank_id = ? AND EXISTS (
			  SELECT 1 FROM answer_claim bl
			  JOIN answer_claim cv ON cv.question_id = bl.question_id AND cv.source = 'community_vote'
			  WHERE bl.question_id = q.id AND bl.source = 'bank_label' AND bl.answer <> cv.answer)`,
			accountID, bankID).Scan(&f.Total, &done)
		if err != nil {
			return fmt.Errorf("统计分歧题规模: %w", err)
		}
		f.Done = &done
	case "wrong", "unsure":
		// 集合本身就是「做过的」的子集，done 无意义；规模复用 Progress 的口径
		var bankSlug string
		if err := s.db.GetContext(ctx, &bankSlug, `SELECT slug FROM bank WHERE id = ?`, bankID); err != nil {
			return fmt.Errorf("查询题库: %w", err)
		}
		p, err := s.LoadProgress(ctx, accountID, bankSlug)
		if err != nil {
			return err
		}
		if f.Mode == "wrong" {
			f.Total = p.WrongCount
		} else {
			f.Total = p.UnsureCount
		}
	}
	return nil
}

// Rate 是正确率百分比，避免前端各算各的。
func (t TagStat) Rate() float32 {
	if t.Total == 0 {
		return 0
	}
	return float32(t.Correct) / float32(t.Total) * 100
}

// DisplayName 优先用英文全称（考纲域的 i18n 里有），否则用 value。
func (t TagStat) DisplayName() string {
	if !t.I18n.Valid || t.I18n.String == "" {
		return t.Value
	}
	if i := strings.Index(t.I18n.String, `"en":"`); i >= 0 {
		rest := t.I18n.String[i+6:]
		if j := strings.Index(rest, `"`); j > 0 {
			return rest[:j]
		}
	}
	return t.Value
}
