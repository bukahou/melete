package httpapi

import (
	"encoding/json"

	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/bank"
	"github.com/bukahou/melete/backend/internal/question"
)

// 本文件只做「领域模型 → API 表示」的翻译。
// 放在接口适配层，让领域模型不必迁就 OpenAPI 生成的类型。

func toAPIBank(b bank.Bank) api.Bank {
	return api.Bank{
		Id: b.ID, Slug: b.Slug, Name: b.Name, Description: b.Description,
		Locale: b.Locale, Kind: api.BankKind(b.Kind),
	}
}

func toAPIBankDetail(b *bank.Bank, s *bank.Stats) api.BankDetail {
	return api.BankDetail{
		Id: b.ID, Slug: b.Slug, Name: b.Name, Description: b.Description,
		Locale: b.Locale, Kind: api.BankDetailKind(b.Kind),
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
		Reference: toAPIReference(d.Reference),
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
