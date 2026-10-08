package question

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

var ErrNotFound = errors.New("question not found")

// Repository 是题目数据的读取契约。
type Repository interface {
	ListQuestions(ctx context.Context, bankID int64, f ListFilter) (*Page, error)
	// FindQuestionByID 的 locale 空串 = 只要源语言，⛔ 不查 i18n 表。
	FindQuestionByID(ctx context.Context, id int64, locale string) (*Detail, error)
	// LoadReference 只取参考答案（study 域判对错用的窄路径，避免拉全量详情）。
	LoadReference(ctx context.Context, questionID int64) (*Reference, error)
	// LoadTextLength 只取题面字数（题干 + 全部选项）。
	// study 域用它判断「这个用时根本读不完题面」，同样是窄路径 —— 不拉正文。
	LoadTextLength(ctx context.Context, questionID int64) (int, error)
	// ListQuestionIDs 返回整个集合的 id，顺序与 ListQuestions 完全一致。
	ListQuestionIDs(ctx context.Context, bankID int64, f ListFilter) ([]int64, error)
	// SummarizeAnswers 统计这些题里该账号做过几道、最近一次答对几道。
	SummarizeAnswers(ctx context.Context, account userid.UserID, ids []int64) (answered, correct int, err error)
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
	if f.Session != nil {
		conds = append(conds, "q.session = ?")
		args = append(args, *f.Session)
	}
	if f.NoFrom > 0 {
		conds = append(conds, "q.external_no >= ?")
		args = append(args, f.NoFrom)
	}
	if f.NoTo > 0 {
		conds = append(conds, "q.external_no <= ?")
		args = append(args, f.NoTo)
	}
	if len(f.AnyTagIDs) > 0 {
		// 并集用 EXISTS 而不是 JOIN：JOIN 会让命中两个标签的题出现两次，分页与计数都会错。
		conds = append(conds, "EXISTS (SELECT 1 FROM question_tag qa WHERE qa.question_id = q.id AND qa.tag_id IN (?"+
			strings.Repeat(",?", len(f.AnyTagIDs)-1)+"))")
		for _, id := range f.AnyTagIDs {
			args = append(args, id)
		}
	}
	if f.OnlyBookmarked {
		conds = append(conds, "EXISTS (SELECT 1 FROM bookmark bm WHERE bm.user_id = ? AND bm.question_id = q.id)")
		args = append(args, f.AccountID)
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

	// ⭐ 译文：LEFT JOIN + COALESCE，没有该语言就回退源语言，并用 localized 标记出来。
	//
	// ⚠️⚠️ 这个 JOIN 的 `?` 出现在 where 的占位符【之前】—— 占位符按出现顺序绑定，
	//   所以 locale 必须排在 args 最前面。这正是本函数下面那条注释警告过的同一个陷阱：
	//   顺序错了不报错，只会把参数喂给错误的位置（静默返回错数据）。
	i18nJoin, localeArgs := "", []any{}
	stemExpr, localizedExpr := "q.stem", "FALSE"
	if f.Locale != "" {
		i18nJoin = " LEFT JOIN question_i18n qi ON qi.question_id = q.id AND qi.locale = ?"
		localeArgs = []any{f.Locale}
		stemExpr, localizedExpr = "COALESCE(qi.stem, q.stem)", "(qi.stem IS NOT NULL)"
	}

	listQuery := `SELECT q.id, q.external_no, ` + stemExpr + ` AS stem, q.kind, q.pick_count,
		` + contestedExpr + ` AS contested, ` + enrichedExpr + ` AS enriched,
		` + localizedExpr + ` AS localized
		FROM question q` + i18nJoin + join + where
	if having != "" {
		listQuery += " GROUP BY q.id" + having
	}
	// 排序：复习队列按到期时间，其余按原题号（让「第 N 题」在界面上可预期）。
	// ⚠️ 排序参数必须插在 LIMIT/OFFSET 【之前】—— 占位符是按出现顺序绑定的，
	// 顺序错了不会报错，只会把 user_id 当成 LIMIT。
	listArgs := append(append([]any{}, localeArgs...), args...)
	order, orderArgs := orderBy(f)
	listQuery += order
	listArgs = append(listArgs, orderArgs...)
	listQuery += " LIMIT ? OFFSET ?"

	// Take：集合 = 排序后的前 Take 题。total 与可取的 limit 都要跟着收窄，
	// 否则「第 11 题」会从集合外面拿 —— 排序是确定的，所以「前 Take 题」也是确定的。
	limit := f.Limit
	if f.Take > 0 {
		if total > f.Take {
			total = f.Take
		}
		if f.Offset >= total {
			return &Page{Items: []Summary{}, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
		}
		if f.Offset+limit > total {
			limit = total - f.Offset
		}
	}

	items := []Summary{}
	if err := r.db.SelectContext(ctx, &items, listQuery, append(listArgs, limit, f.Offset)...); err != nil {
		return nil, fmt.Errorf("查询题目列表: %w", err)
	}
	return &Page{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
}

// orderBy 是题目集合的唯一排序规则 —— 列表、定位（PositionAfter）、小结都用它，
// ⛔ 不各写一份：三处排序不一致，「继续」就会落到别的题上而且不报错。
//
//   - due：按到期时间（复习队列，最该复习的在前）
//   - seed：按种子固定打乱。CRC32 两边（MySQL / TiDB）都有且结果一致；
//     同哈希再按 id 排，保证顺序完全确定
//   - 其余：按 (卷子, 原题号) —— 多套卷子的题库里题号会重复，只按题号会把各套交错在一起
func orderBy(f ListFilter) (string, []any) {
	switch {
	case f.Mode == "due":
		return " ORDER BY " + dueOrderExpr + " ASC, q.id", []any{f.AccountID}
	case f.Seed != nil:
		return " ORDER BY CRC32(CONCAT(?, ':', q.id)), q.id", []any{*f.Seed}
	default:
		return " ORDER BY q.session, q.external_no", nil
	}
}

// ListQuestionIDs 按 orderBy 返回整个集合的题目 id（已按 Take 截断）。
// 只取 id，一个题库一千来题也只是几 KB —— 定位与小结在应用层做，比把同一套过滤再写成 SQL 窗口函数简单可靠。
func (r *mysqlRepository) ListQuestionIDs(ctx context.Context, bankID int64, f ListFilter) ([]int64, error) {
	where, having, args := buildFilter(bankID, f)
	join := ""
	if len(f.TagIDs) > 0 {
		join = " JOIN question_tag qt ON qt.question_id = q.id"
	}
	query := `SELECT q.id FROM question q` + join + where
	if having != "" {
		query += " GROUP BY q.id" + having
	}
	order, orderArgs := orderBy(f)
	query += order
	args = append(args, orderArgs...)
	if f.Take > 0 {
		query += " LIMIT ?"
		args = append(args, f.Take)
	}
	ids := []int64{}
	if err := r.db.SelectContext(ctx, &ids, query, args...); err != nil {
		return nil, fmt.Errorf("查询题目集合: %w", err)
	}
	return ids, nil
}

// SummarizeAnswers 统计这些题里该账号做过几道、最近一次答对几道。
func (r *mysqlRepository) SummarizeAnswers(ctx context.Context, account userid.UserID, ids []int64) (answered, correct int, err error) {
	for i := 0; i < len(ids); i += 500 { // 分批：IN 列表别无限长
		chunk := ids[i:min(i+500, len(ids))]
		q, args, err := sqlx.In(`
			SELECT COUNT(*) AS answered, COALESCE(SUM(a.correct = 1), 0) AS correct
			FROM attempt a
			JOIN (SELECT question_id, MAX(id) AS max_id FROM attempt
			      WHERE user_id = ? AND question_id IN (?) GROUP BY question_id) m
			  ON m.max_id = a.id`, account, chunk)
		if err != nil {
			return 0, 0, err
		}
		var row struct {
			Answered int `db:"answered"`
			Correct  int `db:"correct"`
		}
		if err := r.db.GetContext(ctx, &row, r.db.Rebind(q), args...); err != nil {
			return 0, 0, fmt.Errorf("统计作答: %w", err)
		}
		answered += row.Answered
		correct += row.Correct
	}
	return answered, correct, nil
}

// FindQuestionByID 拉取单题的全部关联数据。
// 分多条查询而非一条大 join —— 一对多的笛卡尔积在应用层拼装更清晰，
// 也避免了 stem / explanation 这类大字段被重复传输。
func (r *mysqlRepository) FindQuestionByID(ctx context.Context, id int64, locale string) (*Detail, error) {
	var row struct {
		Summary
		BankSlug     string         `db:"bank_slug"`
		SourceLocale string         `db:"source_locale"`
		DataIssue    *string        `db:"data_issue"`
		Raw          sql.NullString `db:"raw"`
	}
	// 同 ListQuestions：LEFT JOIN 译文、COALESCE 回退、localized 标记。
	// ⚠️ locale 的占位符在 WHERE 之前，args 顺序必须跟着。
	i18nJoin, stemExpr, localizedExpr := "", "q.stem", "FALSE"
	args := []any{id}
	if locale != "" {
		i18nJoin = " LEFT JOIN question_i18n qi ON qi.question_id = q.id AND qi.locale = ?"
		stemExpr, localizedExpr = "COALESCE(qi.stem, q.stem)", "(qi.stem IS NOT NULL)"
		args = []any{locale, id}
	}
	err := r.db.GetContext(ctx, &row, `
		SELECT q.id, q.external_no, `+stemExpr+` AS stem, q.kind, q.pick_count, q.data_issue, q.raw,
		       b.slug AS bank_slug, b.locale AS source_locale,
		       `+contestedExpr+` AS contested, `+enrichedExpr+` AS enriched,
		       `+localizedExpr+` AS localized
		FROM question q JOIN bank b ON b.id = q.bank_id`+i18nJoin+`
		WHERE q.id = ?`, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询题目 %d: %w", id, err)
	}

	d := &Detail{
		Summary:      row.Summary,
		BankSlug:     row.BankSlug,
		SourceLocale: row.SourceLocale,
		DataIssue:    row.DataIssue,
		Warnings:     parseWarnings(row.Raw),
	}
	if d.Choices, err = r.loadChoices(ctx, id, locale); err != nil {
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

func (r *mysqlRepository) loadChoices(ctx context.Context, id int64, locale string) ([]Choice, error) {
	out := []Choice{}
	// 选项的译文与题干各自独立（可能只补了题干），所以各带各的 localized。
	query := `SELECT c.label, c.body, FALSE AS localized
	          FROM choice c WHERE c.question_id = ? ORDER BY c.label`
	args := []any{id}
	if locale != "" {
		query = `SELECT c.label, COALESCE(ci.body, c.body) AS body, (ci.body IS NOT NULL) AS localized
		         FROM choice c
		         LEFT JOIN choice_i18n ci ON ci.choice_id = c.id AND ci.locale = ?
		         WHERE c.question_id = ? ORDER BY c.label`
		args = []any{locale, id}
	}
	if err := r.db.SelectContext(ctx, &out, query, args...); err != nil {
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
