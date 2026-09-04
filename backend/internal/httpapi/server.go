// Package httpapi 实现 OpenAPI 契约生成的服务端接口。
//
// 它只依赖各领域的 Service **接口**，不依赖 Repository 或具体 SQL ——
// 依赖方向单向：接口适配层 → 领域服务，不反向。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/bukahou/melete/backend/internal/account"
	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/bank"
	"github.com/bukahou/melete/backend/internal/httpauth"
	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/study"
	"github.com/bukahou/melete/backend/internal/token"
)

// Server 实现 api.StrictServerInterface。
type Server struct {
	banks     bank.Service
	questions question.Service
	accounts  account.Service
	studies   study.Service
	tokens    *token.Issuer
	oidc      *token.OIDCVerifier
	log       *slog.Logger
}

func NewServer(
	banks bank.Service, questions question.Service, accounts account.Service,
	studies study.Service, tokens *token.Issuer, oidc *token.OIDCVerifier, log *slog.Logger,
) *Server {
	return &Server{
		banks: banks, questions: questions, accounts: accounts,
		studies: studies, tokens: tokens, oidc: oidc, log: log,
	}
}

var _ api.StrictServerInterface = (*Server)(nil)

func notFound(msg string) api.NotFoundJSONResponse {
	return api.NotFoundJSONResponse{Message: msg}
}

// fail 记录内部错误并向上抛。
// 错误细节只进日志，不返回给客户端 —— 避免泄漏 SQL 结构。
func (s *Server) fail(op string, err error) error {
	s.log.Error("请求处理失败", "op", op, "err", err)
	return err
}

func (s *Server) ListBanks(ctx context.Context, _ api.ListBanksRequestObject) (api.ListBanksResponseObject, error) {
	banks, err := s.banks.ListBanks(ctx)
	if err != nil {
		return nil, s.fail("ListBanks", err)
	}
	out := make(api.ListBanks200JSONResponse, 0, len(banks))
	for _, b := range banks {
		out = append(out, toAPIBank(b))
	}
	return out, nil
}

func (s *Server) GetBank(ctx context.Context, req api.GetBankRequestObject) (api.GetBankResponseObject, error) {
	b, stats, err := s.banks.GetBankDetail(ctx, req.Slug)
	if errors.Is(err, bank.ErrNotFound) {
		return api.GetBank404JSONResponse{NotFoundJSONResponse: notFound("题库不存在: " + req.Slug)}, nil
	}
	if err != nil {
		return nil, s.fail("GetBank", err)
	}
	return api.GetBank200JSONResponse(toAPIBankDetail(b, stats)), nil
}

func (s *Server) ListBankTags(ctx context.Context, req api.ListBankTagsRequestObject) (api.ListBankTagsResponseObject, error) {
	tagType := ""
	if req.Params.Type != nil {
		tagType = string(*req.Params.Type)
	}
	tags, err := s.banks.ListTags(ctx, req.Slug, tagType)
	if errors.Is(err, bank.ErrNotFound) {
		return api.ListBankTags404JSONResponse{NotFoundJSONResponse: notFound("题库不存在: " + req.Slug)}, nil
	}
	if err != nil {
		return nil, s.fail("ListBankTags", err)
	}
	out := make(api.ListBankTags200JSONResponse, 0, len(tags))
	for _, t := range tags {
		out = append(out, toAPIBankTag(t))
	}
	return out, nil
}

func (s *Server) ListQuestions(ctx context.Context, req api.ListQuestionsRequestObject) (api.ListQuestionsResponseObject, error) {
	b, _, err := s.banks.GetBankDetail(ctx, req.Slug)
	if errors.Is(err, bank.ErrNotFound) {
		return api.ListQuestions404JSONResponse{NotFoundJSONResponse: notFound("题库不存在: " + req.Slug)}, nil
	}
	if err != nil {
		return nil, s.fail("ListQuestions.bank", err)
	}

	f := question.ListFilter{}
	if p := req.Params; true {
		if p.Tag != nil {
			f.TagIDs = *p.Tag
		}
		if p.Contested != nil {
			f.OnlyContested = *p.Contested
		}
		if p.Enriched != nil {
			f.OnlyEnriched = *p.Enriched
		}
		if p.Limit != nil {
			f.Limit = *p.Limit
		}
		if p.Offset != nil {
			f.Offset = *p.Offset
		}
		if p.Mode != nil {
			f.Mode = string(*p.Mode)
		}
	}
	// 账号来自已验签的会话 JWT，不是请求参数 —— 无从伪造
	if id, ok := httpauth.AccountID(ctx); ok {
		f.AccountID = id
	}

	page, err := s.questions.ListQuestions(ctx, b.ID, f)
	if errors.Is(err, question.ErrModeNeedsAccount) {
		return api.ListQuestions404JSONResponse{NotFoundJSONResponse: notFound(err.Error())}, nil
	}
	if err != nil {
		return nil, s.fail("ListQuestions", err)
	}
	items := make([]api.QuestionSummary, 0, len(page.Items))
	for _, q := range page.Items {
		items = append(items, toAPISummary(q))
	}
	return api.ListQuestions200JSONResponse{
		Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset,
	}, nil
}

func (s *Server) GetQuestion(ctx context.Context, req api.GetQuestionRequestObject) (api.GetQuestionResponseObject, error) {
	d, err := s.questions.GetQuestion(ctx, req.Id)
	if errors.Is(err, question.ErrNotFound) {
		return api.GetQuestion404JSONResponse{NotFoundJSONResponse: notFound("题目不存在")}, nil
	}
	if err != nil {
		return nil, s.fail("GetQuestion", err)
	}
	return api.GetQuestion200JSONResponse(toAPIDetail(d)), nil
}

func (s *Server) PasswordLogin(ctx context.Context, req api.PasswordLoginRequestObject) (api.PasswordLoginResponseObject, error) {
	a, err := s.accounts.VerifyPassword(ctx, req.Body.Username, req.Body.Password)
	if errors.Is(err, account.ErrBadCredentials) {
		return api.PasswordLogin401JSONResponse{Message: err.Error()}, nil
	}
	if err != nil {
		return nil, s.fail("PasswordLogin", err)
	}
	pair, err := s.tokens.Issue(ctx, a.ID, deref(req.Body.DeviceInfo))
	if err != nil {
		return nil, s.fail("PasswordLogin.issue", err)
	}
	return api.PasswordLogin200JSONResponse(toTokenPair(pair, a.ID, a.DisplayName())), nil
}

// SsoExchange 用 Akasha id_token 换本 API 的 token。
//
// id_token **由本 API 自己验签**，不接受客户端自称的 sub ——
// web 与 iOS 都只是把 OIDC 流程拿到的凭证交过来，身份由签名说了算。
func (s *Server) SsoExchange(ctx context.Context, req api.SsoExchangeRequestObject) (api.SsoExchangeResponseObject, error) {
	sub, display, err := s.oidc.Verify(ctx, req.Body.IdToken)
	if err != nil {
		s.log.Warn("id_token 验签失败", "err", err)
		return api.SsoExchange401JSONResponse{Message: "id_token 无效"}, nil
	}
	a, err := s.accounts.EstablishSSO(ctx, sub, display)
	if err != nil {
		return nil, s.fail("SsoExchange.establish", err)
	}
	pair, err := s.tokens.Issue(ctx, a.ID, deref(req.Body.DeviceInfo))
	if err != nil {
		return nil, s.fail("SsoExchange.issue", err)
	}
	return api.SsoExchange200JSONResponse(toTokenPair(pair, a.ID, a.DisplayName())), nil
}

func (s *Server) RefreshToken(ctx context.Context, req api.RefreshTokenRequestObject) (api.RefreshTokenResponseObject, error) {
	pair, accountID, err := s.tokens.Refresh(ctx, req.Body.RefreshToken)
	if errors.Is(err, token.ErrRevoked) {
		return api.RefreshToken401JSONResponse{Message: "会话已失效，请重新登录"}, nil
	}
	if err != nil {
		return nil, s.fail("RefreshToken", err)
	}
	a, err := s.accounts.FindByID(ctx, accountID)
	if err != nil {
		return nil, s.fail("RefreshToken.account", err)
	}
	return api.RefreshToken200JSONResponse(toTokenPair(pair, a.ID, a.DisplayName())), nil
}

func (s *Server) Logout(ctx context.Context, req api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	if err := s.tokens.Revoke(ctx, req.Body.RefreshToken); err != nil {
		return nil, s.fail("Logout", err)
	}
	return api.Logout204Response{}, nil
}

func toTokenPair(p *token.Pair, accountID int64, display string) api.TokenPair {
	return api.TokenPair{
		AccessToken:  p.AccessToken,
		RefreshToken: p.RefreshToken,
		ExpiresIn:    p.ExpiresIn,
		AccountId:    accountID,
		Display:      display,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Server) RecordAttempt(ctx context.Context, req api.RecordAttemptRequestObject) (api.RecordAttemptResponseObject, error) {
	accountID, ok := httpauth.AccountID(ctx)
	if !ok {
		// 中间件已挡住无 token 的请求，走到这里说明路由挂错了中间件
		return nil, s.fail("RecordAttempt", errors.New("上下文缺少已认证账号"))
	}
	var contextJSON *string
	if req.Body.Context != nil {
		b, err := json.Marshal(req.Body.Context)
		if err != nil {
			return nil, s.fail("RecordAttempt.context", err)
		}
		str := string(b)
		contextJSON = &str
	}
	res, err := s.studies.RecordAttempt(ctx, study.Attempt{
		AccountID:  accountID,
		QuestionID: req.Body.QuestionId,
		Chosen:     req.Body.Chosen,
		Rating:     req.Body.Rating,
		DurationMs: req.Body.DurationMs,
		Context:    contextJSON,
	})
	if err != nil {
		return nil, s.fail("RecordAttempt", err)
	}
	out := api.RecordAttempt200JSONResponse{Correct: res.Correct}
	if res.Reference != nil {
		out.Reference = toAPIReference(res.Reference)
	}
	out.Schedule = toAPISchedule(res)
	return out, nil
}

// ---- 个人统计（/me/*）----

// requireAccount 是三个个人统计端点的共同前置。
// 中间件已挡住无 token 的请求，这里拿不到即为路由配置错误。
func (s *Server) requireAccount(ctx context.Context, op string) (int64, error) {
	id, ok := httpauth.AccountID(ctx)
	if !ok {
		return 0, s.fail(op, errors.New("上下文缺少已认证账号"))
	}
	return id, nil
}

func (s *Server) GetMyProgress(ctx context.Context, req api.GetMyProgressRequestObject) (api.GetMyProgressResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "GetMyProgress")
	if err != nil {
		return nil, err
	}
	slug, err := s.currentBank(ctx, req.Params.Bank)
	if err != nil {
		return nil, s.fail("GetMyProgress.banks", err)
	}
	p, err := s.studies.LoadProgress(ctx, accountID, slug)
	if err != nil {
		return nil, s.fail("GetMyProgress", err)
	}
	out := api.GetMyProgress200JSONResponse{
		BankSlug: p.BankSlug, QuestionCount: p.QuestionCount, SeenCount: p.SeenCount,
		CorrectCount: p.CorrectCount, WrongCount: p.WrongCount,
		UnsureCount: p.UnsureCount, AttemptCount: p.AttemptCount,
	}
	if p.LastActiveAt.Valid {
		t := p.LastActiveAt.Time
		out.LastActiveAt = &t
	}
	return out, nil
}

func (s *Server) GetMyTagStats(ctx context.Context, req api.GetMyTagStatsRequestObject) (api.GetMyTagStatsResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "GetMyTagStats")
	if err != nil {
		return nil, err
	}
	slug, err := s.currentBank(ctx, req.Params.Bank)
	if err != nil {
		return nil, s.fail("GetMyTagStats.banks", err)
	}
	minAttempts := 3
	if req.Params.MinAttempts != nil {
		minAttempts = *req.Params.MinAttempts
	}
	stats, err := s.studies.LoadTagStats(ctx, accountID, slug, string(req.Params.Type), minAttempts)
	if err != nil {
		return nil, s.fail("GetMyTagStats", err)
	}
	out := make(api.GetMyTagStats200JSONResponse, 0, len(stats))
	for _, st := range stats {
		item := api.TagStat{
			TagId: st.TagID, Type: api.TagStatType(st.Type), Value: st.Value,
			Total: st.Total, Correct: st.Correct, Rate: st.Rate(),
		}
		if st.I18n.Valid && st.I18n.String != "" {
			m := map[string]string{}
			if json.Unmarshal([]byte(st.I18n.String), &m) == nil && len(m) > 0 {
				item.I18n = &m
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// currentBank 决定「当前题库」：显式传了 bank 就用它，否则退回第一个题库。
// 多题库后「当前」应改为最近活跃的那个 —— 到时只改这一处。
func (s *Server) currentBank(ctx context.Context, explicit *string) (string, error) {
	if explicit != nil && *explicit != "" {
		return *explicit, nil
	}
	banks, err := s.banks.ListBanks(ctx)
	if err != nil {
		return "", err
	}
	if len(banks) == 0 {
		return "", errors.New("没有任何题库")
	}
	return banks[0].Slug, nil
}

func (s *Server) GetMyResume(ctx context.Context, req api.GetMyResumeRequestObject) (api.GetMyResumeResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "GetMyResume")
	if err != nil {
		return nil, err
	}
	slug, err := s.currentBank(ctx, req.Params.Bank)
	if err != nil {
		return nil, s.fail("GetMyResume.banks", err)
	}
	r, err := s.studies.LoadResume(ctx, accountID, slug)
	if err != nil {
		return nil, s.fail("GetMyResume", err)
	}
	return api.GetMyResume200JSONResponse(toAPIResume(r)), nil
}

func (s *Server) GetMyOverview(ctx context.Context, _ api.GetMyOverviewRequestObject) (api.GetMyOverviewResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "GetMyOverview")
	if err != nil {
		return nil, err
	}
	o, err := s.studies.LoadOverview(ctx, accountID)
	if err != nil {
		return nil, s.fail("GetMyOverview", err)
	}
	return api.GetMyOverview200JSONResponse{TodayCount: o.TodayCount, StreakDays: o.StreakDays, SeenTotal: o.SeenTotal}, nil
}

func (s *Server) GetMyRecent(ctx context.Context, req api.GetMyRecentRequestObject) (api.GetMyRecentResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "GetMyRecent")
	if err != nil {
		return nil, err
	}
	limit := 5
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	sessions, err := s.studies.LoadRecentSessions(ctx, accountID, limit)
	if err != nil {
		return nil, s.fail("GetMyRecent", err)
	}
	out := api.GetMyRecent200JSONResponse{}
	for _, ss := range sessions {
		out = append(out, toAPISession(ss))
	}
	return out, nil
}
