package httpapi

import (
	"context"
	"errors"

	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/bank"
	"github.com/bukahou/melete/backend/internal/glossary"
	"github.com/bukahou/melete/backend/internal/httplocale"
)

// ListTerms 用语集目录（P9 第 6 步）。与题库同一个可见性：看不到题库 ⇒ 404（P9 #27）。
func (s *Server) ListTerms(ctx context.Context, req api.ListTermsRequestObject) (api.ListTermsResponseObject, error) {
	_, scope, err := s.viewer(ctx, "ListTerms")
	if err != nil {
		return nil, err
	}
	b, err := s.visibleBank(ctx, scope, req.Slug)
	if errors.Is(err, bank.ErrNotFound) {
		return api.ListTerms404JSONResponse{NotFoundJSONResponse: notFound("题库不存在: " + req.Slug)}, nil
	}
	if err != nil {
		return nil, s.fail("ListTerms.bank", err)
	}
	terms, err := s.glossary.ListTerms(ctx, b.ID)
	if err != nil {
		return nil, s.fail("ListTerms", err)
	}
	out := make(api.ListTerms200JSONResponse, 0, len(terms))
	for _, t := range terms {
		out = append(out, toAPITermSummary(t))
	}
	return out, nil
}

// GetTerm 术语详情：释义 + 出题历史（题干跟界面语言走）。
func (s *Server) GetTerm(ctx context.Context, req api.GetTermRequestObject) (api.GetTermResponseObject, error) {
	_, scope, err := s.viewer(ctx, "GetTerm")
	if err != nil {
		return nil, err
	}
	d, err := s.glossary.GetTerm(ctx, req.Id, httplocale.RequestLocale(ctx))
	// ⚠️ 路径里没有题库名：取到术语后按它所属题库判断，看不到 ⇒ 与「不存在」同一个 404（P9 #27）
	if err == nil {
		if _, verr := s.visibleBank(ctx, scope, d.BankSlug); errors.Is(verr, bank.ErrNotFound) {
			err = glossary.ErrNotFound
		} else if verr != nil {
			return nil, s.fail("GetTerm.access", verr)
		}
	}
	if errors.Is(err, glossary.ErrNotFound) {
		return api.GetTerm404JSONResponse{NotFoundJSONResponse: notFound("术语不存在")}, nil
	}
	if err != nil {
		return nil, s.fail("GetTerm", err)
	}
	sum := toAPITermSummary(d.Summary)
	out := api.GetTerm200JSONResponse{
		Id: sum.Id, Slug: sum.Slug, Names: sum.Names, Reading: sum.Reading,
		Category: sum.Category, Lead: sum.Lead, QuestionCount: sum.QuestionCount,
		BankSlug: d.BankSlug, Definition: d.Definition,
	}
	for _, q := range d.Questions {
		out.Questions = append(out.Questions, struct {
			ExternalNo int    `json:"externalNo"`
			Id         int64  `json:"id"`
			Session    string `json:"session"`
			Stem       string `json:"stem"`
		}{ExternalNo: q.ExternalNo, Id: q.ID, Session: q.Session, Stem: q.Stem})
	}
	return out, nil
}

func toAPITermSummary(t glossary.Summary) api.TermSummary {
	return api.TermSummary{
		Id: t.ID, Slug: t.Slug, Names: t.Names, Reading: t.Reading,
		Category: t.Category, Lead: t.Lead, QuestionCount: t.QuestionCount,
	}
}
