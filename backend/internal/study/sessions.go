package study

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/bukahou/melete/backend/internal/userid"
	"time"
)

// Overview 是首页统计带：跨题库、只关于「我」。
type Overview struct {
	TodayCount int
	StreakDays int
	SeenTotal  int
}

// Session 是一次学习会话：同一题库、同一出处、相邻间隔 ≤ sessionGap 的连续作答。
// 会话不是事实，是对 attempt 的解释 —— 所以不落库，每次现算。
type Session struct {
	BankSlug  string
	BankName  string
	Mode      string
	TagID     *int64
	Tag       *TagRef
	StartedAt time.Time
	EndedAt   time.Time
	Count     int
	Correct   int
	FirstNo   *int // 顺序刷（unseen / all）时记录起止题号
	LastNo    *int
}

const sessionGap = 30 * time.Minute

// 连续天数按学习者所在时区的自然日算；开发者在东京，先写死，将来进 account 偏好。
var studyTZ = time.FixedZone("JST", 9*3600)

func (s *service) LoadOverview(ctx context.Context, accountID userid.UserID) (*Overview, error) {
	o := &Overview{}
	now := time.Now().In(studyTZ)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, studyTZ).UTC()
	err := s.db.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM attempt WHERE user_id = ? AND created_at >= ?),
		  (SELECT COUNT(DISTINCT question_id) FROM attempt WHERE user_id = ?)`,
		accountID, dayStart, accountID).Scan(&o.TodayCount, &o.SeenTotal)
	if err != nil {
		return nil, fmt.Errorf("统计总览: %w", err)
	}

	// 连续天数：取最近 400 天有作答的自然日集合，从今天（或昨天）往回数
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT DATE(CONVERT_TZ(created_at, '+00:00', '+09:00'))
		FROM attempt WHERE user_id = ? AND created_at >= ?
		ORDER BY 1 DESC`, accountID, now.AddDate(0, 0, -400).UTC())
	if err != nil {
		return nil, fmt.Errorf("统计连续天数: %w", err)
	}
	defer rows.Close()
	days := map[string]bool{}
	for rows.Next() {
		var d sql.NullString
		if err := rows.Scan(&d); err == nil && d.Valid {
			days[d.String[:10]] = true
		}
	}
	cursor := now
	if !days[cursor.Format("2006-01-02")] {
		cursor = cursor.AddDate(0, 0, -1) // 今天还没做不算断
	}
	for days[cursor.Format("2006-01-02")] {
		o.StreakDays++
		cursor = cursor.AddDate(0, 0, -1)
	}
	return o, nil
}

// LoadRecentSessions 把最近的作答聚合成会话。
// 只拉最近 300 条 attempt 在内存里切分 —— 首页只要前几个会话，不必在 SQL 里做窗口函数。
func (s *service) LoadRecentSessions(ctx context.Context, accountID userid.UserID, limit int) ([]Session, error) {
	type row struct {
		Context   sql.NullString `db:"context"`
		Correct   bool           `db:"correct"`
		CreatedAt time.Time      `db:"created_at"`
		No        int            `db:"external_no"`
		BankSlug  string         `db:"bank_slug"`
		BankName  string         `db:"bank_name"`
	}
	var rs []row
	err := s.db.SelectContext(ctx, &rs, `
		SELECT a.context, a.correct, a.created_at, q.external_no, b.slug AS bank_slug, b.name AS bank_name
		FROM attempt a
		JOIN question q ON q.id = a.question_id
		JOIN bank b     ON b.id = q.bank_id
		WHERE a.user_id = ?
		ORDER BY a.id DESC LIMIT 300`, accountID)
	if err != nil {
		return nil, fmt.Errorf("查询最近作答: %w", err)
	}

	var out []Session
	var cur *Session
	key := func(r row) (string, string, *int64) {
		var dc drillContext
		if r.Context.Valid {
			_ = json.Unmarshal([]byte(r.Context.String), &dc)
		}
		if dc.Mode == "" {
			dc.Mode = "all"
		}
		tagKey := ""
		if dc.TagID != nil {
			tagKey = fmt.Sprint(*dc.TagID)
		}
		return r.BankSlug + "|" + dc.Mode + "|" + tagKey, dc.Mode, dc.TagID
	}
	var curKey string
	for _, r := range rs { // 倒序：r 比 cur 更早
		k, mode, tagID := key(r)
		if cur != nil && k == curKey && cur.StartedAt.Sub(r.CreatedAt) <= sessionGap {
			cur.StartedAt = r.CreatedAt
			cur.Count++
			if r.Correct {
				cur.Correct++
			}
			if sequentialModes[mode] {
				no := r.No
				cur.FirstNo = &no
			}
			continue
		}
		if len(out) >= limit {
			break
		}
		no := r.No
		cur = &Session{BankSlug: r.BankSlug, BankName: r.BankName, Mode: mode, TagID: tagID,
			StartedAt: r.CreatedAt, EndedAt: r.CreatedAt, Count: 1}
		if r.Correct {
			cur.Correct = 1
		}
		if sequentialModes[mode] {
			cur.FirstNo, cur.LastNo = &no, &no
		}
		curKey = k
		out = append(out, *cur)
		cur = &out[len(out)-1]
	}
	// 补标签名
	for i := range out {
		if out[i].TagID == nil {
			continue
		}
		var t TagRef
		if err := s.db.GetContext(ctx, &t, `SELECT id, type, value, i18n FROM tag WHERE id = ?`, *out[i].TagID); err == nil {
			out[i].Tag = &t
		}
	}
	return out, nil
}
