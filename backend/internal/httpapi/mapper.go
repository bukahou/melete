package httpapi

import (
	"encoding/json"

	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/bank"
	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/study"
)

// 本文件只做「领域模型 → API 表示」的翻译。
// 放在接口适配层，让领域模型不必迁就 OpenAPI 生成的类型。

func toAPIBank(b bank.Bank) api.Bank {
	return api.Bank{
		Id: b.ID, Slug: b.Slug, Name: b.Name, Description: b.Description,
		Locale: b.Locale, Kind: api.BankKind(b.Kind), Meta: parseBankMeta(b.Meta),
	}
}

// parseBankMeta 把 bank.meta JSON 列解成契约类型。
// 列里可能还有导入侧留下的其它键（如 domains 英文名表），未在契约中的一律忽略；
// 解析失败退回空 meta —— 展示元数据缺失只该让前端退回默认文案，不该让题库 404。
func parseBankMeta(raw *string) api.BankMeta {
	var m api.BankMeta
	if raw == nil || *raw == "" {
		return m
	}
	if err := json.Unmarshal([]byte(*raw), &m); err != nil {
		return api.BankMeta{}
	}
	return m
}

func toAPIBankDetail(b *bank.Bank, s *bank.Stats) api.BankDetail {
	return api.BankDetail{
		Id: b.ID, Slug: b.Slug, Name: b.Name, Description: b.Description,
		Locale: b.Locale, Kind: api.BankDetailKind(b.Kind), Meta: parseBankMeta(b.Meta),
		Stats: api.BankStats{
			QuestionCount:  s.QuestionCount,
			EnrichedCount:  s.EnrichedCount,
			ContestedCount: s.ContestedCount,
		},
	}
}

// parseI18n 把数据库里的 i18n JSON 列翻成 map。
// 解析失败时返回 nil 而不是报错 —— 标签的多语言名缺失不该让整个请求失败。
func parseI18n(raw *string) *map[string]string {
	if raw == nil || *raw == "" {
		return nil
	}
	m := map[string]string{}
	if err := json.Unmarshal([]byte(*raw), &m); err != nil {
		return nil
	}
	return &m
}

func toAPIBankTag(t bank.Tag) api.Tag {
	count := t.QuestionCount
	return api.Tag{
		Id: t.ID, Type: api.TagType(t.Type), Value: t.Value,
		I18n: parseI18n(t.I18n), QuestionCount: &count,
	}
}

func toAPIQuestionTag(t question.Tag) api.Tag {
	return api.Tag{
		Id: t.ID, Type: api.TagType(t.Type), Value: t.Value, I18n: parseI18n(t.I18n),
	}
}

func toAPISummary(q question.Summary) api.QuestionSummary {
	return api.QuestionSummary{
		Id: q.ID, ExternalNo: q.ExternalNo, Stem: q.Stem,
		Kind: api.QuestionSummaryKind(q.Kind), PickCount: q.PickCount,
		Contested: q.Contested, Enriched: q.Enriched,
	}
}

// toAPIClaim 保留每一条主张的完整出处。
// 绝不在这里做「挑一个最可信的」之类的合并 —— 分歧本身就是要呈现的内容。
func toAPIClaim(c question.Claim) api.AnswerClaim {
	out := api.AnswerClaim{
		Source: api.AnswerClaimSource(c.Source), Answer: c.Answer,
		Confidence: c.Confidence, Rationale: c.Rationale,
	}
	if len(c.Meta) > 0 {
		m := map[string]any{}
		if err := json.Unmarshal(c.Meta, &m); err == nil && len(m) > 0 {
			out.Meta = &m
		}
	}
	return out
}

func toAPIReference(r *question.Reference) *api.Reference {
	if r == nil {
		return nil
	}
	return &api.Reference{Answer: r.Answer, Source: api.ReferenceSource(r.Source)}
}

func toAPIDetail(d *question.Detail) api.QuestionDetail {
	out := api.QuestionDetail{
		Id: d.ID, ExternalNo: d.ExternalNo, Stem: d.Stem,
		Kind: api.QuestionDetailKind(d.Kind), PickCount: d.PickCount,
		Contested: d.Contested, Enriched: d.Enriched,
		BankSlug: d.BankSlug, DataIssue: d.DataIssue,
		Reference:    toAPIReference(d.Reference),
		Choices:      make([]api.Choice, 0, len(d.Choices)),
		Claims:       make([]api.AnswerClaim, 0, len(d.Claims)),
		Explanations: make([]api.Explanation, 0, len(d.Explanations)),
		Tags:         make([]api.Tag, 0, len(d.Tags)),
	}
	for _, c := range d.Choices {
		out.Choices = append(out.Choices, api.Choice{Label: c.Label, Body: c.Body})
	}
	for _, c := range d.Claims {
		out.Claims = append(out.Claims, toAPIClaim(c))
	}
	for _, e := range d.Explanations {
		out.Explanations = append(out.Explanations, api.Explanation{
			Source: api.ExplanationSource(e.Source), Locale: e.Locale, Body: e.Body,
		})
	}
	for _, t := range d.Tags {
		out.Tags = append(out.Tags, toAPIQuestionTag(t))
	}
	if len(d.Warnings) > 0 {
		w := d.Warnings
		out.Warnings = &w
	}
	return out
}

func toAPIResume(r *study.Resume) api.Resume {
	seq := api.SequentialCursor{DoneCount: r.Sequential.DoneCount, TotalCount: r.Sequential.TotalCount}
	if r.Sequential.QuestionID.Valid {
		id := r.Sequential.QuestionID.Int64
		no := int(r.Sequential.ExternalNo.Int64)
		stem := r.Sequential.Stem.String
		seq.QuestionId, seq.ExternalNo, seq.Stem = &id, &no, &stem
	}
	if r.Sequential.LastAt.Valid {
		t := r.Sequential.LastAt.Time
		seq.LastAt = &t
	}
	out := api.Resume{BankSlug: r.BankSlug, Sequential: seq}
	if f := r.Focus; f != nil {
		fc := api.FocusCursor{
			Context: api.DrillContext{Mode: api.DrillContextMode(f.Mode), TagId: f.TagID},
			Label:   f.Mode, Total: f.Total, LastAt: f.LastAt, Done: f.Done,
		}
		if f.Tag != nil {
			fc.Label = f.Tag.Value
			i18n := f.Tag.I18n.String
			fc.Tag = &api.Tag{Id: f.Tag.ID, Type: api.TagType(f.Tag.Type), Value: f.Tag.Value, I18n: parseI18n(&i18n)}
		}
		out.Focus = &fc
	}
	return out
}

func toAPISession(ss study.Session) api.StudySession {
	out := api.StudySession{
		BankSlug: ss.BankSlug, BankName: ss.BankName,
		Context: api.DrillContext{Mode: api.DrillContextMode(ss.Mode), TagId: ss.TagID},
		Label:   ss.Mode, StartedAt: ss.StartedAt, EndedAt: ss.EndedAt, Count: ss.Count, Correct: ss.Correct,
		FirstNo: ss.FirstNo, LastNo: ss.LastNo,
	}
	if ss.Tag != nil {
		out.Label = ss.Tag.Value
		i18n := ss.Tag.I18n.String
		out.Tag = &api.Tag{Id: ss.Tag.ID, Type: api.TagType(ss.Tag.Type), Value: ss.Tag.Value, I18n: parseI18n(&i18n)}
	}
	return out
}

// toAPISchedule 把调度结果搬到契约类型。
//
// ⚠️ reasons 即便为空也返回空数组而不是 null —— 前端要能无条件 .map()，
// 而 Go 的 nil slice 会被 encoding/json 编成 null。这类「空 vs 不存在」
// 的差别在跨语言边界上最容易漏，而且不报错，只是界面上少一块。
// toAPISchedule 把调度结果映射给客户端。
//
// ⭐ 未自评时返回 nil —— 那时根本没有卡片被排。
// ⛔ 不能看零值猜：Correction 的零值是「rating 0、无理由」，Due 是零时间，
//    照直映射出去就成了一条「下次复习时间 0001-01-01」的假调度。
func toAPISchedule(r *study.Result) *api.ScheduleResult {
	if !r.Scheduled {
		return nil
	}
	reasons := r.Correction.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	return &api.ScheduleResult{
		Rating:          r.Correction.Raw,
		EffectiveRating: r.Correction.Effective,
		Reasons:         reasons,
		Due:             r.NextDue,
		State:           api.ScheduleResultState(r.NextState.String()),
	}
}
