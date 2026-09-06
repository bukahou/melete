package question

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("question not found")

// Repository 是题目数据的读取契约。
type Repository interface {
	ListQuestions(ctx context.Context, bankID int64, f ListFilter) (*Page, error)
	FindQuestionByID(ctx context.Context, id int64) (*Detail, error)
	// LoadReference 只取参考答案（study 域判对错用的窄路径，避免拉全量详情）。
	LoadReference(ctx context.Context, questionID int64) (*Reference, error)
	// LoadTextLength 只取题面字数（题干 + 全部选项）。
	// study 域用它判断「这个用时根本读不完题面」，同样是窄路径 —— 不拉正文。
	LoadTextLength(ctx context.Context, questionID int64) (int, error)
}

type mysqlRepository struct{ db *sqlx.DB }

func NewMySQLRepository(db *sqlx.DB) Repository { return &mysqlRepository{db: db} }

// contestedExpr 判定「题库标注与社区投票不一致」。
// 放在 SQL 里而非应用层，是因为它要参与 WHERE 过滤和分页计数。
const contestedExpr = `EXISTS(
	SELECT 1 FROM answer_claim bl
	JOIN answer_claim cv
	  ON cv.question_id = bl.question_id AND cv.source = 'community_vote'
	WHERE bl.question_id = q.id AND bl.source = 'bank_label' AND bl.answer <> cv.answer
)`

const enrichedExpr = `EXISTS(SELECT 1 FROM explanation e WHERE e.question_id = q.id)`

// lastAttemptExpr 取该账号对这道题**最近一次**作答的某个字段。
// 错题本的语义是「最近一次是错的」—— 后来做对了就该出本子。
const lastAttemptExpr = `(SELECT a.%s FROM attempt a
	WHERE a.user_id = ? AND a.question_id = q.id
	ORDER BY a.id DESC LIMIT 1)`

// dueExpr 判定「这道题已到期」。UTC_TIMESTAMP() 而非 NOW()：
// card.due 按 UTC 存（2026-09-03 时区迁移后全库统一），NOW() 取的是会话时区。
const dueExpr = `EXISTS (SELECT 1 FROM card c
	WHERE c.user_id = ? AND c.question_id = q.id AND c.due <= UTC_TIMESTAMP())`

// dueOrderExpr 让复习队列按【到期时间】排，最该复习的排在最前。
// 其余模式按原题号排 —— 那是浏览列表，「第 N 题」要可预期；
// 复习队列不是浏览列表，按题号排等于把最危险的题排到最后。
const dueOrderExpr = `(SELECT c.due FROM card c
	WHERE c.user_id = ? AND c.question_id = q.id)`

// buildFilter 拼出 WHERE 子句与参数。
// 多个标签取**交集**（同时命中全部标签），用 HAVING 计数实现。
func buildFilter(bankID int64, f ListFilter) (where string, having string, args []any) {
	conds := []string{"q.bank_id = ?"}
	args = append(args, bankID)

	if len(f.TagIDs) > 0 {
		conds = append(conds, "qt.tag_id IN (?"+strings.Repeat(",?", len(f.TagIDs)-1)+")")
		for _, id := range f.TagIDs {
			args = append(args, id)
		}
		having = fmt.Sprintf(" HAVING COUNT(DISTINCT qt.tag_id) = %d", len(f.TagIDs))
	}
	if f.OnlyContested {
		conds = append(conds, contestedExpr)
	}
	if f.OnlyEnriched {
		conds = append(conds, enrichedExpr)
	}
	switch f.Mode {
	case "wrong":
		conds = append(conds, fmt.Sprintf(lastAttemptExpr, "correct")+" = 0")
		args = append(args, f.AccountID)
	case "unsure":
		conds = append(conds, fmt.Sprintf(lastAttemptExpr, "rating")+" <= 2")
		args = append(args, f.AccountID)
	case "unseen":
		conds = append(conds, `NOT EXISTS (SELECT 1 FROM attempt a
			WHERE a.user_id = ? AND a.question_id = q.id)`)
		args = append(args, f.AccountID)
	case "due":
		// FSRS 复习队列：已到期的卡片。
		// ⚠️ 从没做过的题【不算到期】—— 它们没有 card 行，属于「没做过」那个入口。
		// 把两者混进同一个数字，「今天要复习 300 题」就没有意义了，
		// 而那正是间隔重复最劝退的失败模式。
		conds = append(conds, dueExpr)
		args = append(args, f.AccountID)
	}
	return " WHERE " + strings.Join(conds, " AND "), having, args
}

func (r *mysqlRepository) ListQuestions(ctx context.Context, bankID int64, f ListFilter) (*Page, error) {
	where, having, args := buildFilter(bankID, f)
	join := ""
	if len(f.TagIDs) > 0 {
		join = " JOIN question_tag qt ON qt.question_id = q.id"
	}

	countQuery := `SELECT COUNT(*) FROM (SELECT q.id FROM question q` + join + where +
		func() string {
			if having != "" {
				return " GROUP BY q.id" + having
			}
			return ""
		}() + `) x`
	var total int
	if err := r.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, fmt.Errorf("统计题目数: %w", err)
	}

	listQuery := `SELECT q.id, q.external_no, q.stem, q.kind, q.pick_count,
		` + contestedExpr + ` AS contested, ` + enrichedExpr + ` AS enriched
		FROM question q` + join + where
	if having != "" {
		listQuery += " GROUP BY q.id" + having
	}
	// 排序：复习队列按到期时间，其余按原题号（让「第 N 题」在界面上可预期）。
	// ⚠️ 排序参数必须插在 LIMIT/OFFSET 【之前】—— 占位符是按出现顺序绑定的，
	// 顺序错了不会报错，只会把 user_id 当成 LIMIT。
	listArgs := args
	if f.Mode == "due" {
		listQuery += " ORDER BY " + dueOrderExpr + " ASC"
		listArgs = append(append([]any{}, args...), f.AccountID)
	} else {
		listQuery += " ORDER BY q.external_no"
	}
	listQuery += " LIMIT ? OFFSET ?"

	items := []Summary{}
	if err := r.db.SelectContext(ctx, &items, listQuery, append(listArgs, f.Limit, f.Offset)...); err != nil {
		return nil, fmt.Errorf("查询题目列表: %w", err)
	}
	return &Page{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
}

// FindQuestionByID 拉取单题的全部关联数据。
// 分多条查询而非一条大 join —— 一对多的笛卡尔积在应用层拼装更清晰，
// 也避免了 stem / explanation 这类大字段被重复传输。
func (r *mysqlRepository) FindQuestionByID(ctx context.Context, id int64) (*Detail, error) {
	var row struct {
		Summary
		BankSlug  string         `db:"bank_slug"`
		DataIssue *string        `db:"data_issue"`
		Raw       sql.NullString `db:"raw"`
	}
	err := r.db.GetContext(ctx, &row, `
		SELECT q.id, q.external_no, q.stem, q.kind, q.pick_count, q.data_issue, q.raw,
		       b.slug AS bank_slug,
		       `+contestedExpr+` AS contested, `+enrichedExpr+` AS enriched
		FROM question q JOIN bank b ON b.id = q.bank_id
		WHERE q.id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询题目 %d: %w", id, err)
	}

	d := &Detail{
		Summary:   row.Summary,
		BankSlug:  row.BankSlug,
		DataIssue: row.DataIssue,
		Warnings:  parseWarnings(row.Raw),
	}
	if d.Choices, err = r.loadChoices(ctx, id); err != nil {
		return nil, err
	}
	if d.Claims, err = r.loadClaims(ctx, id); err != nil {
		return nil, err
	}
	if d.Explanations, err = r.loadExplanations(ctx, id); err != nil {
		return nil, err
	}
	if d.Tags, err = r.loadTags(ctx, id); err != nil {
		return nil, err
	}
	d.Reference = ResolveReference(d.Claims)
	return d, nil
}

// LoadReference 用一条 SQL 按信任顺序取参考答案（study 域的窄路径）。
func (r *mysqlRepository) LoadReference(ctx context.Context, questionID int64) (*Reference, error) {
	var ref Reference
	err := r.db.GetContext(ctx, &ref, `
		SELECT answer, source FROM answer_claim
		WHERE question_id = ? AND source IN ('ai_verdict', 'community_vote', 'bank_label')
		ORDER BY FIELD(source, 'ai_verdict', 'community_vote', 'bank_label')
		LIMIT 1`, questionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // 没有任何主张的题：能作答但无从判对错
	}
	if err != nil {
		return nil, fmt.Errorf("查询题目 %d 的参考答案: %w", questionID, err)
	}
	return &ref, nil
}

// LoadTextLength 返回题干与全部选项加起来的字符数。
//
// 用 CHAR_LENGTH 而不是 LENGTH：后者返回字节数，中文题面会被算成三倍，
// 于是「最低阅读时间」凭空变成三倍，规则②几乎不会触发 —— 而且不报任何错。
func (r *mysqlRepository) LoadTextLength(ctx context.Context, questionID int64) (int, error) {
	var n int
	err := r.db.GetContext(ctx, &n, `
		SELECT CHAR_LENGTH(q.stem) + COALESCE(
		         (SELECT SUM(CHAR_LENGTH(c.body)) FROM choice c WHERE c.question_id = q.id), 0)
		FROM question q WHERE q.id = ?`, questionID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // 题目不存在：交给上层的其它校验去报，这里不制造第二处错误来源
	}
	if err != nil {
		return 0, fmt.Errorf("查询题目 %d 的题面字数: %w", questionID, err)
	}
	return n, nil
}

func (r *mysqlRepository) loadChoices(ctx context.Context, id int64) ([]Choice, error) {
	out := []Choice{}
	err := r.db.SelectContext(ctx, &out,
		`SELECT label, body FROM choice WHERE question_id = ? ORDER BY label`, id)
	if err != nil {
		return nil, fmt.Errorf("查询题目 %d 的选项: %w", id, err)
	}
	return out, nil
}

// loadClaims 的排序刻意固定：题库标注 → 社区投票 → AI 裁决 → 用户笔记。
// 界面按这个顺序并列展示，学习者一眼看到分歧在哪。
func (r *mysqlRepository) loadClaims(ctx context.Context, id int64) ([]Claim, error) {
	out := []Claim{}
	err := r.db.SelectContext(ctx, &out, `
		SELECT source, answer, confidence, rationale, meta
		FROM answer_claim WHERE question_id = ?
		ORDER BY FIELD(source, 'bank_label', 'community_vote', 'ai_verdict', 'user_note')`, id)
	if err != nil {
		return nil, fmt.Errorf("查询题目 %d 的答案主张: %w", id, err)
	}
	return out, nil
}

func (r *mysqlRepository) loadExplanations(ctx context.Context, id int64) ([]Explanation, error) {
	out := []Explanation{}
	err := r.db.SelectContext(ctx, &out,
		`SELECT source, locale, body FROM explanation WHERE question_id = ? ORDER BY source, locale`, id)
	if err != nil {
		return nil, fmt.Errorf("查询题目 %d 的解析: %w", id, err)
	}
	return out, nil
}

func (r *mysqlRepository) loadTags(ctx context.Context, id int64) ([]Tag, error) {
	out := []Tag{}
	err := r.db.SelectContext(ctx, &out, `
		SELECT t.id, t.type, t.value, t.i18n
		FROM tag t JOIN question_tag qt ON qt.tag_id = t.id
		WHERE qt.question_id = ?
		ORDER BY FIELD(t.type, 'domain', 'topic', 'concept'), t.value`, id)
	if err != nil {
		return nil, fmt.Errorf("查询题目 %d 的标签: %w", id, err)
	}
	return out, nil
}

// parseWarnings 从 question.raw 取出 P0 解析阶段的告警。
// 告警不静默丢弃 —— 21 道题带着题库自身的脏点，界面上要能看到。
func parseWarnings(raw sql.NullString) []string {
	if !raw.Valid {
		return nil
	}
	var payload struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(raw.String), &payload); err != nil {
		return nil
	}
	return payload.Warnings
}
