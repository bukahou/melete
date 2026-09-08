package question

import "github.com/bukahou/melete/backend/internal/userid"

import "fmt"

// Summary 是列表页用的题目摘要。
type Summary struct {
	ID         int64  `db:"id"`
	ExternalNo int    `db:"external_no"`
	Stem       string `db:"stem"`
	Kind       string `db:"kind"`
	PickCount  int    `db:"pick_count"`
	// Contested 表示题库标注答案与社区投票不一致。
	// 这不是边缘情况：884 道有对照数据的题里占 38%。
	Contested bool `db:"contested"`
	Enriched  bool `db:"enriched"`
	// Localized 说明 Stem 是不是请求语言的译文。
	//
	// ⭐ false = 没有该语言的译文，上面给的是【源语言】原文。
	// 界面必须把这件事显示出来（「本题暂无日文版」），⛔ 不静默回退 ——
	// 与「答案主张并列展示」同一条哲学：把状况摆出来，不替学习者下结论。
	// ⚠️ 请求源语言本身时也是 false（那本来就不是译文），界面据此不必标注：
	//    判断标注与否用「请求的 locale ≠ 题库源语言 且 !Localized」。
	Localized bool `db:"localized"`
}

// Choice 是一个选项。
type Choice struct {
	Label string `db:"label"`
	Body  string `db:"body"`
	// Localized 同 Summary.Localized —— 选项的译文可能与题干不同步
	// （比如译文只补了题干），所以各自带各自的标记。
	Localized bool `db:"localized"`
}

// Claim 是一条**带来源的答案主张**。
//
// 这是本项目最核心的设计判断：答案不是 question 上的一个字段，
// 而是一组各有出处的主张。题库说 A、98% 的社区说 B、AI 判 B 并给出理由 ——
// 并列呈现这个分歧，本身就是最好的学习材料。
// 任何把它压平成单一 correct_answer 的改动，都是在删掉这个项目的核心价值。
type Claim struct {
	Source     string  `db:"source"`
	Answer     string  `db:"answer"`
	Confidence *int    `db:"confidence"`
	Rationale  *string `db:"rationale"`
	Meta       RawJSON `db:"meta"`
}

// RawJSON 承载数据库里的 JSON 列。
//
// 不直接用 json.RawMessage：database/sql 的类型断言是**精确匹配 *[]byte**，
// *json.RawMessage 虽然底层同为 []byte 也不被接受，扫到 NULL 会报
// "unsupported Scan, storing driver.Value type <nil>"。实现 sql.Scanner 兜住。
type RawJSON []byte

func (r *RawJSON) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*r = nil
	case []byte:
		*r = append((*r)[:0], v...)
	case string:
		*r = []byte(v)
	default:
		return fmt.Errorf("RawJSON: 无法从 %T 扫描", src)
	}
	return nil
}

// Explanation 是面向学习者的完整解析，与 Claim.Rationale 职责不同：
// Rationale 论证「为什么选它」，绑定到某条主张；Explanation 绑定到题目本身。
type Explanation struct {
	Source string `db:"source"`
	Locale string `db:"locale"`
	Body   string `db:"body"`
}

// Tag 采用通用 (type, value) 结构，不硬编码任何题库特有的分类体系。
type Tag struct {
	ID    int64   `db:"id"`
	Type  string  `db:"type"`
	Value string  `db:"value"`
	I18n  *string `db:"i18n"`
}

// Reference 是判对错用的参考答案。
//
// 优先级 ai_verdict > community_vote > bank_label：AI 是唯一看过全部信息
// 并给出理由的来源；题库标注 38% 与社区投票不一致，是最不可信的那个。
// 它只决定「打勾还是打叉」—— 界面仍并列展示全部主张，不替学习者下结论。
type Reference struct {
	Answer string `db:"answer"`
	Source string `db:"source"`
}

// resolveOrder 是参考答案的信任顺序，SQL 与内存判定共用这一份定义。
var resolveOrder = []string{"ai_verdict", "community_vote", "bank_label"}

// ResolveReference 从一组主张里选出参考答案；一条主张都没有时返回 nil。
func ResolveReference(claims []Claim) *Reference {
	for _, want := range resolveOrder {
		for _, c := range claims {
			if c.Source == want {
				return &Reference{Answer: c.Answer, Source: c.Source}
			}
		}
	}
	return nil
}

// Detail 是单题详情。
type Detail struct {
	Summary
	BankSlug string
	// SourceLocale 是题库正文的语言。⭐ 界面靠它把「没有译文」与
	// 「请求的就是源语言」区分开 —— 只看 Localized 两者都是 false。
	SourceLocale string
	Reference    *Reference
	DataIssue    *string
	Warnings     []string
	Choices      []Choice
	Claims       []Claim
	Explanations []Explanation
	Tags         []Tag
}

// ListFilter 是列表查询的筛选条件。
// Mode 为 wrong/unsure/unseen 时是个人化过滤，必须同时给 AccountID。
type ListFilter struct {
	TagIDs        []int64
	OnlyContested bool
	OnlyEnriched  bool
	Mode          string
	AccountID     userid.UserID
	// Locale 是希望拿到的语言。空串 = 只要源语言，⛔ 不查 i18n 表。
	Locale string
	Limit  int
	Offset int
}

// Page 是一页题目。
type Page struct {
	Items  []Summary
	Total  int
	Limit  int
	Offset int
}
