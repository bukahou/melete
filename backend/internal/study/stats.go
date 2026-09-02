package study

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Progress 是一个账号在某题库上的总体进度。
type Progress struct {
	BankSlug      string       `db:"bank_slug"`
	QuestionCount int          `db:"question_count"`
	SeenCount     int          `db:"seen_count"`
	CorrectCount  int          `db:"correct_count"`
	WrongCount    int          `db:"wrong_count"`
	UnsureCount   int          `db:"unsure_count"`
	AttemptCount  int          `db:"attempt_count"`
	LastActiveAt  sql.NullTime `db:"last_active_at"`
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

// Resume 是「上次刷到哪」。
type Resume struct {
	BankSlug   string       `db:"bank_slug"`
	QuestionID int64        `db:"question_id"`
	ExternalNo int          `db:"external_no"`
	Stem       string       `db:"stem"`
	AnsweredAt sql.NullTime `db:"answered_at"`
}

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
	err := s.db.GetContext(ctx, &p, `
		SELECT
		  b.slug AS bank_slug,
		  (SELECT COUNT(*) FROM question q WHERE q.bank_id = b.id) AS question_count,
		  COALESCE(COUNT(la.id), 0)                                AS seen_count,
		  COALESCE(SUM(la.correct = 1), 0)                         AS correct_count,
		  COALESCE(SUM(la.correct = 0), 0)                         AS wrong_count,
		  COALESCE(SUM(la.rating <= 2), 0)                         AS unsure_count,
		  (SELECT COUNT(*) FROM attempt a2
		     JOIN question q2 ON q2.id = a2.question_id AND q2.bank_id = b.id
		   WHERE a2.account_id = ?)                                AS attempt_count,
		  MAX(la.created_at)                                       AS last_active_at
		FROM bank b
		LEFT JOIN (`+latestAttempt+`) la ON 1 = 1
		LEFT JOIN question lq ON lq.id = la.question_id AND lq.bank_id = b.id
		WHERE b.slug = ? AND (la.id IS NULL OR lq.id IS NOT NULL)
		GROUP BY b.id, b.slug`, accountID, accountID, slug)
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

// LoadResume 找「上次刷到哪」。
func (s *service) LoadResume(ctx context.Context, accountID int64) (*Resume, error) {
	var r Resume
	err := s.db.GetContext(ctx, &r, `
		SELECT b.slug AS bank_slug, q.id AS question_id, q.external_no, q.stem,
		       a.created_at AS answered_at
		FROM attempt a
		JOIN question q ON q.id = a.question_id
		JOIN bank b     ON b.id = q.bank_id
		WHERE a.account_id = ?
		ORDER BY a.id DESC LIMIT 1`, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return &Resume{}, nil // 从未作答：返回空对象而非错误，前端引导「开始第一题」
	}
	if err != nil {
		return nil, fmt.Errorf("查询学习断点: %w", err)
	}
	return &r, nil
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
