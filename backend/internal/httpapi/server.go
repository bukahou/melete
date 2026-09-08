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

	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/auth"
	"github.com/bukahou/melete/backend/internal/bank"
	"github.com/bukahou/melete/backend/internal/httpauth"
	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/study"
	"github.com/bukahou/melete/backend/internal/token"
	"github.com/bukahou/melete/backend/internal/userid"
)

// Server 实现 api.StrictServerInterface。
type Server struct {
	banks     bank.Service
	questions question.Service
	studies   study.Service
	// ⭐ auth 是登录的唯一入口。⛔ 注意这里【没有】account.Service ——
	// 阶段 3 起 melete 不再有自己的「校验密码」这件事，那全在模块里。
	auth *auth.Service
	oidc *token.OIDCVerifier
	log  *slog.Logger
}

func NewServer(
	banks bank.Service, questions question.Service,
	studies study.Service, authSvc *auth.Service, oidc *token.OIDCVerifier, log *slog.Logger,
) *Server {
	return &Server{
		banks: banks, questions: questions,
		studies: studies, auth: authSvc, oidc: oidc, log: log,
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
		f.AccountID = userid.UserID(id)
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
	pair, err := s.auth.Login(ctx, clientIP(ctx), req.Body.Username, req.Body.Password, deref(req.Body.DeviceInfo))
	// ⚠️ 退避命中与密码错误【同一个返回】—— 模块刻意不区分（案卷 §18.4：
	// retryAfter 只进日志）。⛔ 这里不得按错误细分文案，那会把差异加回来，
	// 而那个差异本身就是预言机。
	if errors.Is(err, auth.ErrBadCredentials) {
		return api.PasswordLogin401JSONResponse{Message: err.Error()}, nil
	}
	if err != nil {
		return nil, s.fail("PasswordLogin", err)
	}
	return api.PasswordLogin200JSONResponse(toTokenPair(pair)), nil
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
	pair, err := s.auth.EstablishFederated(ctx, sub, display, deref(req.Body.DeviceInfo), clientIP(ctx))
	if err != nil {
		return nil, s.fail("SsoExchange.establish", err)
	}
	return api.SsoExchange200JSONResponse(toTokenPair(pair)), nil
}

func (s *Server) RefreshToken(ctx context.Context, req api.RefreshTokenRequestObject) (api.RefreshTokenResponseObject, error) {
	pair, err := s.auth.Refresh(ctx, req.Body.RefreshToken, "")
	// ⚠️ 重放 / 已吊销 / 账号停用 / 改密后失效 —— 模块全部返回同一个码，
	// 于是这里也只有一种回应。⛔ 区分它们等于把内部状态透给攻击者。
	if errors.Is(err, auth.ErrRevoked) {
		return api.RefreshToken401JSONResponse{Message: "会话已失效，请重新登录"}, nil
	}
	if err != nil {
		return nil, s.fail("RefreshToken", err)
	}
	return api.RefreshToken200JSONResponse(toTokenPair(pair)), nil
}

func (s *Server) Logout(ctx context.Context, req api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	// ⭐ 登出是幂等的：已失效的 token 再登出一次也返回 204。
	// ⚠️ 报错会让「这个 token 存在过」变成可探测的信息。
	if err := s.auth.Logout(ctx, req.Body.RefreshToken); err != nil && !errors.Is(err, auth.ErrRevoked) {
		return nil, s.fail("Logout", err)
	}
	return api.Logout204Response{}, nil
}

func toTokenPair(p *auth.Pair) api.TokenPair {
	return api.TokenPair{
		AccessToken:  p.AccessToken,
		RefreshToken: p.RefreshToken,
		ExpiresIn:    p.ExpiresIn,
		AccountId:    p.AccountID,
		Display:      p.Display,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Server) RecordAttempt(ctx context.Context, req api.RecordAttemptRequestObject) (api.RecordAttemptResponseObject, error) {
	// ⭐ 与其它端点一样走 requireAccount —— 它是 HTTP 层到业务层
	// 唯一的账号 id 转换点。⛔ 这里曾经自己取一次 AccountID，
	// 于是它是唯一一处绕过那个转换点的地方。
	accountID, err := s.requireAccount(ctx, "RecordAttempt")
	if err != nil {
		return nil, err
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
	out := api.RecordAttempt200JSONResponse{Correct: res.Correct, AttemptId: &res.AttemptID}
	if res.Reference != nil {
		out.Reference = toAPIReference(res.Reference)
	}
	out.Schedule = toAPISchedule(res)
	return out, nil
}

// RateAttempt 给一条已记录的作答补上自评。
//
// ⭐ 这是「揭晓即记录」拆出来的第二步：作答已经在 RecordAttempt 落库了，
// 这里只补 rating 并据此排 FSRS 卡片。⛔ 不再有「不评分就什么都不存」。
func (s *Server) RateAttempt(ctx context.Context, req api.RateAttemptRequestObject) (api.RateAttemptResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "RateAttempt")
	if err != nil {
		return nil, err
	}
	res, err := s.studies.RateAttempt(ctx, accountID, req.Id, req.Body.Rating)
	if err != nil {
		// ⚠️ 不存在与不属于本账号合并成同一个 404（见 study.ErrAttemptNotFound）——
		// 区分开就等于确认「这个 id 存在，只是不是你的」。
		if errors.Is(err, study.ErrAttemptNotFound) {
			return api.RateAttempt404JSONResponse{NotFoundJSONResponse: notFound("作答记录不存在")}, nil
		}
		return nil, s.fail("RateAttempt", err)
	}
	out := api.RateAttempt200JSONResponse{Correct: res.Correct, AttemptId: &res.AttemptID}
	if res.Reference != nil {
		out.Reference = toAPIReference(res.Reference)
	}
	out.Schedule = toAPISchedule(res)
	return out, nil
}

// ---- 个人统计（/me/*）----

// requireAccount 是三个个人统计端点的共同前置。
// 中间件已挡住无 token 的请求，这里拿不到即为路由配置错误。
// ⭐ 返回 userid.UserID 而不是 string —— 这是 HTTP 层与业务层之间
// 【唯一】的账号 id 转换点。
//
// ⚠️ 类型不同不是形式主义：study/question 的函数只收 userid.UserID，
// 所以「把一个普通 string 当账号 id 传进去」是编译错误，
// ⛔ 而不是一次静默的空结果（BINARY(16) 列拿字符串比对，匹配不到任何行，
// 界面上看起来就是「你还没有作答」——完全正常的样子）。
func (s *Server) requireAccount(ctx context.Context, op string) (userid.UserID, error) {
	id, ok := httpauth.AccountID(ctx)
	if !ok {
		return "", s.fail(op, errors.New("上下文缺少已认证账号"))
	}
	return userid.UserID(id), nil
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
		DueCount: p.DueCount,
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

// clientIP 从上下文取解析后的可信 IP。
//
// ⚠️ 空串 = 来源不可用 → 模块【降级】：关掉 IP 维度的退避，账号维度照常。
// ⛔ 那不是失败，理由与代价写在模块 guard.go 的 ipUnavailable 一段。
//
// ⚠️ 解析本身不在这里：它需要 HTTP 头，而 strict-server 的 handler 只拿到
// ctx。⇒ 由中间件解析一次后放进 ctx（见 httpauth）。
func clientIP(ctx context.Context) string {
	ip, _ := httpauth.ClientIP(ctx)
	return ip
}

// ListSessions 我的登录设备。
//
// ⛔ 返回值里没有任何 token / 哈希字段 —— 模块的 SessionRecord 本身就不带，
// 这是它的类型层面保证（案卷记过一个同类系统实测验证过这一条）。
func (s *Server) ListSessions(ctx context.Context, req api.ListSessionsRequestObject) (api.ListSessionsResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "ListSessions")
	if err != nil {
		return nil, err
	}
	// ⭐ 当前会话取自 token 的 sid，用来给界面标出「这一台就是你现在用的」。
	current, _ := httpauth.SessionID(ctx)
	rows, err := s.auth.ListSessions(ctx, string(accountID))
	if err != nil {
		return nil, s.fail("ListSessions", err)
	}
	out := make(api.ListSessions200JSONResponse, 0, len(rows))
	for _, r := range rows {
		item := api.SessionInfo{
			Id: r.ID, CreatedAt: r.CreatedAt, LastActiveAt: r.LastActiveAt,
			ExpiresAt: r.ExpiresAt, Current: r.ID == current,
		}
		if r.DeviceInfo != "" {
			item.DeviceInfo = &r.DeviceInfo
		}
		if r.ClientIP != "" {
			item.ClientIp = &r.ClientIP
		}
		out = append(out, item)
	}
	return out, nil
}

// RevokeOtherSessions 登出其它设备。
func (s *Server) RevokeOtherSessions(ctx context.Context, req api.RevokeOtherSessionsRequestObject) (api.RevokeOtherSessionsResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "RevokeOtherSessions")
	if err != nil {
		return nil, err
	}
	// ⛔⛔ 保留哪一条【只能】来自当前这张票，不接受客户端指定。
	//
	// ⚠️ sid 为空时返回 409 而不是「登出全部」：
	// 阶段 3 之前签发的旧票没有 sid，若在这里退化成全部吊销，
	// 用户点一次「登出其它设备」会把自己也踢掉 ——
	// 一次误操作被放大成全员掉线，而一个同类系统实测栽过的缺陷正是这个形状。
	current, ok := httpauth.SessionID(ctx)
	if !ok {
		return api.RevokeOtherSessions409JSONResponse{
			Message: "当前令牌不含会话标识，请重新登录后再试",
		}, nil
	}
	n, err := s.auth.RevokeOtherSessions(ctx, string(accountID), current)
	if err != nil {
		return nil, s.fail("RevokeOtherSessions", err)
	}
	return api.RevokeOtherSessions200JSONResponse{Revoked: n}, nil
}

// ChangePassword 修改密码 / 首次设置密码。
//
// ⚠️ 与登录路径相反，这里的错误【必须区分】：调用方已经通过认证，
// 「旧密码错」与「新密码太短」对他是两种完全不同的操作提示。
// ⛔ 混成一个会让用户不知道该改什么。
// （登录路径必须不可区分是为了防枚举 —— 两条纪律不冲突，因为
//
//	那里的调用方是【未认证】的。）
func (s *Server) ChangePassword(ctx context.Context, req api.ChangePasswordRequestObject) (api.ChangePasswordResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "ChangePassword")
	if err != nil {
		return nil, err
	}
	// ⭐ 当前会话取自 token 的 sid：模块用它决定「改密后为哪台设备重签」。
	// ⛔ 不接受客户端指定 —— 否则可以让别人的设备被重签。
	current, _ := httpauth.SessionID(ctx)

	res, err := s.auth.ChangePassword(ctx, string(accountID),
		deref(req.Body.OldPassword), req.Body.NewPassword,
		current, deref(req.Body.DeviceInfo), clientIP(ctx))
	switch {
	case errors.Is(err, auth.ErrOldPasswordWrong):
		return api.ChangePassword401JSONResponse{Message: err.Error()}, nil
	case errors.Is(err, auth.ErrWeakPassword):
		return api.ChangePassword400JSONResponse{Message: err.Error()}, nil
	case err != nil:
		return nil, s.fail("ChangePassword", err)
	}
	out := api.ChangePassword200JSONResponse{
		Breached: res.Breached, BreachCount: res.BreachCount,
		Checked: res.Checked, RevokedCount: res.RevokedCount,
	}
	if res.Pair != nil {
		p := toTokenPair(res.Pair)
		out.Tokens = &p
	}
	return out, nil
}

// ── 注册 / 找回 / 改邮箱（阶段 5）──────────────────────────────────

// SendRegisterCode 发注册验证码。
//
// ⛔ 邮箱已被占用时【仍然 204】—— 见契约里的理由。
func (s *Server) SendRegisterCode(ctx context.Context, req api.SendRegisterCodeRequestObject) (api.SendRegisterCodeResponseObject, error) {
	err := s.auth.SendRegisterCode(ctx, req.Body.Username, string(req.Body.Email), clientIP(ctx))
	if errors.Is(err, auth.ErrTooManyCodes) {
		return api.SendRegisterCode429JSONResponse{Message: err.Error()}, nil
	}
	if err != nil {
		return nil, s.fail("SendRegisterCode", err)
	}
	return api.SendRegisterCode204Response{}, nil
}

func (s *Server) Register(ctx context.Context, req api.RegisterRequestObject) (api.RegisterResponseObject, error) {
	pair, advice, err := s.auth.Register(ctx,
		req.Body.Username, req.Body.Password, deref(req.Body.DisplayName),
		string(req.Body.Email), req.Body.Code, deref(req.Body.DeviceInfo), clientIP(ctx))
	switch {
	case errors.Is(err, auth.ErrInvalidCode), errors.Is(err, auth.ErrWeakPassword),
		errors.Is(err, auth.ErrUsernameTaken), errors.Is(err, auth.ErrEmailTaken),
		errors.Is(err, auth.ErrTooManyCodes):
		return api.Register400JSONResponse{Message: err.Error()}, nil
	case err != nil:
		return nil, s.fail("Register", err)
	}
	return api.Register200JSONResponse{
		Tokens:   toTokenPair(pair),
		Breached: advice.Breached, BreachCount: advice.BreachCount, Checked: advice.Checked,
	}, nil
}

// SendRecoveryCode 发找回码。
//
// ⛔⛔ 无论地址存不存在、有没有已验证邮箱，一律 204。
// ⚠️ 任何差别都会让它变成「这个邮箱有账号吗」的查询接口。
func (s *Server) SendRecoveryCode(ctx context.Context, req api.SendRecoveryCodeRequestObject) (api.SendRecoveryCodeResponseObject, error) {
	err := s.auth.RequestRecovery(ctx, string(req.Body.Email), clientIP(ctx))
	if errors.Is(err, auth.ErrTooManyCodes) {
		return api.SendRecoveryCode429JSONResponse{Message: err.Error()}, nil
	}
	if err != nil {
		return nil, s.fail("SendRecoveryCode", err)
	}
	return api.SendRecoveryCode204Response{}, nil
}

func (s *Server) CompleteRecovery(ctx context.Context, req api.CompleteRecoveryRequestObject) (api.CompleteRecoveryResponseObject, error) {
	res, err := s.auth.CompleteRecovery(ctx, string(req.Body.Email), req.Body.Code, req.Body.NewPassword)
	switch {
	case errors.Is(err, auth.ErrInvalidCode), errors.Is(err, auth.ErrWeakPassword):
		return api.CompleteRecovery400JSONResponse{Message: err.Error()}, nil
	case err != nil:
		return nil, s.fail("CompleteRecovery", err)
	}
	return api.CompleteRecovery200JSONResponse{
		Breached: res.Breached, BreachCount: res.BreachCount,
		Checked: res.Checked, RevokedCount: res.RevokedCount,
	}, nil
}

func (s *Server) SendEmailChangeCode(ctx context.Context, req api.SendEmailChangeCodeRequestObject) (api.SendEmailChangeCodeResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "SendEmailChangeCode")
	if err != nil {
		return nil, err
	}
	err = s.auth.RequestEmailChange(ctx, string(accountID), string(req.Body.NewEmail), clientIP(ctx))
	if errors.Is(err, auth.ErrTooManyCodes) {
		return api.SendEmailChangeCode429JSONResponse{Message: err.Error()}, nil
	}
	if err != nil {
		return nil, s.fail("SendEmailChangeCode", err)
	}
	return api.SendEmailChangeCode204Response{}, nil
}

func (s *Server) ConfirmEmailChange(ctx context.Context, req api.ConfirmEmailChangeRequestObject) (api.ConfirmEmailChangeResponseObject, error) {
	accountID, err := s.requireAccount(ctx, "ConfirmEmailChange")
	if err != nil {
		return nil, err
	}
	current, _ := httpauth.SessionID(ctx)
	pair, err := s.auth.ConfirmEmailChange(ctx, string(accountID), req.Body.Code,
		current, deref(req.Body.DeviceInfo), clientIP(ctx))
	switch {
	case errors.Is(err, auth.ErrInvalidCode), errors.Is(err, auth.ErrEmailTaken):
		return api.ConfirmEmailChange400JSONResponse{Message: err.Error()}, nil
	case err != nil:
		return nil, s.fail("ConfirmEmailChange", err)
	}
	out := api.ConfirmEmailChange200JSONResponse{}
	// ⚠️ pair 为 nil = 没重签：邮箱【已经改好了】，只是用户需重新登录。
	// ⛔ 不是失败，⛔ 不能因此返回错误。
	if pair != nil {
		p := toTokenPair(pair)
		out.Tokens = &p
	}
	return out, nil
}
