// Package auth 是 melete 的登录编排层。
//
// ⭐ 它【不实现】任何认证逻辑 —— 退避、哈希比对、轮换、重放判定、
// 账号状态复查全部在 github.com/bukahou/gokit/localauth 里。
// 本包只做三件事：把请求翻译成模块的入参、把模块的结果翻译成 HTTP 层要的东西、
// 以及签发 access token（⭐ access 的形状是宿主的事，模块从不签它）。
//
// ⚠️ cross-exam 005 §35 的验收方式：melete 完全删除自己的登录逻辑，
// 只靠模块重建。所以⛔ 这里出现任何「自己判断能不能登录」的代码都是验收失败。
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bukahou/gokit/localauth"
	"github.com/golang-jwt/jwt/v5"

	"github.com/bukahou/melete/backend/internal/account"
	"github.com/bukahou/melete/backend/internal/userid"
)

const (
	// AccessTTL ⭐ 案卷裁决 ② 定为 15min（原 1h）。
	//
	// ⚠️ melete 没有 Redis ⇒ 没有吊销存储 ⇒ access TTL【就是】改密/封禁的
	// 残留窗口。与另一服务同量级（具体值见部署配置）。
	// 降到 15min 让两边同量级，成本是客户端多刷几次（web/iOS 都已有拦截器）。
	AccessTTL = 15 * time.Minute
	// RefreshTTL 30 天
	RefreshTTL = 30 * 24 * time.Hour
	issuer     = "melete-api"
)

var (
	// ErrBadCredentials 统一「用户不存在」「密码错误」「账号停用」——
	// ⛔ 对外不可区分。模块内部会发不同的审计事件，但返回值只有这一个。
	ErrBadCredentials = errors.New("用户名或密码错误")
	// ⚠️ ⛔ 刻意【没有】「尝试过于频繁」这个错误 —— 见 translateLogin 的注释：
	// 锁定与密码错误对外必须不可区分，否则差异本身就是预言机。
	// ErrUntrustedSource 对应 CodeClientIPUnavailable：请求没走预期链路，
	// 是请求问题不是服务器故障 ⇒ 403 而不是 5xx。
	ErrUntrustedSource = errors.New("请求来源不可信")
	ErrRevoked         = errors.New("会话已失效")
	ErrInvalidToken    = errors.New("token 无效")
)

// Pair 是一次签发的结果。
type Pair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	AccountID    string
	Display      string
}

// Service 组装模块的两个守卫。
type Service struct {
	guard        *localauth.Guard
	sessions     *localauth.SessionGuard
	passwords    *localauth.PasswordGuard
	registration *localauth.RegistrationGuard
	recovery     *localauth.RecoveryGuard
	emailChange  *localauth.EmailChangeGuard
	accounts     account.Repository
	lookup       localauth.LookupFunc
	secret       []byte
	log          *slog.Logger
}

func NewService(
	guard *localauth.Guard,
	sessions *localauth.SessionGuard,
	accounts account.Repository,
	lookup localauth.LookupFunc,
	jwtSecret string,
	log *slog.Logger,
) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{guard: guard, sessions: sessions, accounts: accounts,
		lookup: lookup, secret: []byte(jwtSecret), log: log}
}

// WithPasswordGuard 接上改密守卫。
//
// ⚠️ 做成事后注入而不是构造参数，是因为 PasswordGuard 需要
// AccessTokenIssuer，而那正是 Service 自己的方法 —— 构造期还拿不到。
// ⛔ 不接的话 ChangePassword 会明确报错，⛔ 不静默失败。
func (s *Service) WithPasswordGuard(g *localauth.PasswordGuard) *Service {
	s.passwords = g
	return s
}

// Login 密码登录。
//
// ⭐ 三步全在模块里：查退避 → 比对（恒定耗时）→ 计数/清零。
// ⛔ 本函数没有一行在判断「能不能登录」。
func (s *Service) Login(ctx context.Context, clientIP, username, password, deviceInfo string) (*Pair, error) {
	out, err := s.guard.Login(ctx, clientIP, username, password, s.lookup)
	if err != nil {
		return nil, translateLogin(err)
	}
	if !out.Allowed {
		// ⚠️ 理论上走不到：Login 要么返回错误要么 Allowed=true。
		// 留着是因为 LoginOutcome 是个 struct，将来加字段时这里不该默认放行。
		return nil, ErrBadCredentials
	}

	// ⚠️ 模块的 LookupFunc 只回哈希，不回 userID —— 它刻意不知道宿主的账号模型。
	// 所以这里再查一次拿 id。⛔ 这不是"重复校验"：能走到这里说明哈希已经匹配。
	a, err := s.accounts.FindByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("登录后取账号: %w", err)
	}
	return s.issue(ctx, a, deviceInfo, clientIP)
}

// EstablishFederated 走 Akasha 回来之后发会话。
//
// ⚠️ 「按 (provider, subject) 找或建账号」这一段属 akasha 范围（案卷 §35），
// 本次不改其编排；这里只负责它之后的【发会话】。
func (s *Service) EstablishFederated(ctx context.Context, sub, display, deviceInfo, clientIP string) (*Pair, error) {
	a, err := s.accounts.EstablishFederated(ctx, "akasha", sub, display)
	if err != nil {
		return nil, err
	}
	return s.issue(ctx, a, deviceInfo, clientIP)
}

func (s *Service) issue(ctx context.Context, a *account.Account, deviceInfo, clientIP string) (*Pair, error) {
	refresh, rec, err := s.sessions.Issue(ctx, localauth.SessionRecord{
		UserID:     a.ID,
		DeviceInfo: deviceInfo,
		// ⚠️ 必须是【解析后的可信 IP】，⛔ 不得是 XFF 整条链 ——
		// 会话列表要展示它，展示一条攻击者能随便写的字符串是有害的。
		ClientIP: clientIP,
	})
	if err != nil {
		return nil, fmt.Errorf("建立会话: %w", err)
	}
	access, err := s.signAccess(a.ID, rec.ID, time.Now())
	if err != nil {
		return nil, err
	}
	return &Pair{AccessToken: access, RefreshToken: refresh,
		ExpiresIn: int(AccessTTL.Seconds()), AccountID: a.ID, Display: a.DisplayName()}, nil
}

// Refresh 轮换。
//
// ⭐ 模块的编排顺序（⛔ 不可调换）：轮换 → 重放处置 → 复查账号状态 → 比对改密时间。
// 「复查账号状态」在签新票【之前】，所以封禁在一次刷新内即生效。
func (s *Service) Refresh(ctx context.Context, refreshToken, deviceInfo string) (*Pair, error) {
	out, err := s.sessions.Refresh(ctx, refreshToken)
	if err != nil {
		return nil, translateSession(err)
	}
	a, err := s.accounts.FindByID(ctx, out.Session.UserID)
	if err != nil {
		return nil, fmt.Errorf("刷新后取账号: %w", err)
	}
	access, err := s.signAccess(a.ID, out.Session.ID, time.Now())
	if err != nil {
		return nil, err
	}
	return &Pair{AccessToken: access, RefreshToken: out.NewRefreshToken,
		ExpiresIn: int(AccessTTL.Seconds()), AccountID: a.ID, Display: a.DisplayName()}, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if err := s.sessions.Logout(ctx, refreshToken); err != nil {
		return translateSession(err)
	}
	return nil
}

// ListSessions 用户自助的「我的登录设备」。
// ⛔ 返回值里没有任何 token / 哈希字段 —— 那是撤销凭据，不该出现在界面上。
func (s *Service) ListSessions(ctx context.Context, userID string) ([]localauth.SessionRecord, error) {
	return s.sessions.List(ctx, userID)
}

// RevokeOtherSessions 登出其它设备。
// ⚠️ keepSessionID 来自当前 access token 的 sid claim —— ⛔ 不能由客户端指定，
// 否则任何人都能构造一个「保留别人的会话、踢掉我的」的请求。
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, keepSessionID string) (int, error) {
	return s.sessions.RevokeOthers(ctx, userID, keepSessionID)
}

func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	return s.sessions.RevokeOne(ctx, userID, sessionID)
}

// ⭐ 关于错误码粒度 —— 接入计划 §6.3 ① 预判「码可能不够细」，实测结论是：
//
//	它【故意】粗，而且那是对的。
//
// `Guard.Login` 的退避命中、密码错误、用户不存在，全部返回 CodeInvalidCredentials；
// `SessionGuard.Refresh` 的重放、已吊销、账号停用、改密后失效，也全部是它。
// ⚠️ 案卷 §18.4 明写「retryAfter 只进日志」—— 锁定的用户可见行为【不该】与
// 密码错误不同，否则那个差异本身就是预言机（一次探测即读出账号近期有无失败）。
// ⇒ ⛔ melete 不得渲染「尝试过于频繁」这类文案，那会把模块刻意抹掉的差异加回来。
//
// 🔴 唯一必须分出来的是 CodeMisconfigured：它是【接线错误】，
// 把它映射成「密码错误」等于把一个启动期就该炸的 bug 伪装成用户输错了。

// translateLogin 登录路径的错误翻译。
func translateLogin(err error) error {
	if c, ok := codeOf(err); ok {
		switch c {
		case localauth.CodeMisconfigured:
			return err // ⛔ 原样上抛 → 500 + 日志，⚠️ 不得伪装成凭据错误
		case localauth.CodeClientIPUnavailable:
			return ErrUntrustedSource
		}
	}
	// ⚠️ 其余一律 ErrBadCredentials —— ⛔ 不区分，见上方注释。
	return ErrBadCredentials
}

// translateSession 刷新 / 登出路径的错误翻译。
func translateSession(err error) error {
	if c, ok := codeOf(err); ok && c == localauth.CodeMisconfigured {
		return err
	}
	return ErrRevoked
}

func codeOf(err error) (localauth.Code, bool) {
	var le *localauth.Error
	if errors.As(err, &le) {
		return le.Code, true
	}
	return "", false
}

// signAccess 签 access token。
//
// ⭐ 带 sid（会话 id）：「登出其它设备」要知道保留哪一条，
// 而那个信息只能来自当前这张票 —— ⛔ 不能让客户端说了算。
//
// ⛔⛔ issuedAt 由【调用方】给，⛔ 这里不得用 time.Now() 覆盖它。
//
// ⚠️ 这不是风格问题。gokit v0.2.0 改密流程里，模块会算出一个
// `reissueAt = nextSecond(changedAt)` 并要求重签出来的 token 的 `iat`
// 【正好是它】：吊销纪元设在 changedAt，判定是 `iat <= changedAt 即失效`，
// 于是其它设备同一秒签发的 token 会被作废，而重签的这张因为
// iat > changedAt 得以幸存。
// ⇒ 若这里把 iat 强制覆盖成 now（很可能与 changedAt 同一秒），
//
//	刚重签出来的 token 会【立刻失效】，用户改完密码当场掉线。
//
// ⚠️ geass-v3 正是撞在这上面（它的 jwt helper 把 iat 当保留 claim 强制覆盖），
// 因而接不了 v0.2.0。melete 这里只有这一处 MapClaims，所以透传即可。
// ⛔ 将来若有人抽一个"统一签 JWT"的 helper，必须保留这个参数。
func (s *Service) signAccess(userID, sessionID string, issuedAt time.Time) (string, error) {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": issuer,
		"sub": userID,
		"sid": sessionID,
		"iat": issuedAt.Unix(),
		"exp": issuedAt.Add(AccessTTL).Unix(),
	})
	out, err := t.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("签发 access token: %w", err)
	}
	return out, nil
}

// IssueAccessToken 是 localauth.AccessTokenIssuer 的实现。
//
// ⭐ 模块在改密重签时调它，并把算好的 issuedAt 传进来 —— 见 signAccess 的注释。
func (s *Service) IssueAccessToken(_ context.Context, userID, sessionID string, issuedAt time.Time) (string, time.Time, error) {
	tok, err := s.signAccess(userID, sessionID, issuedAt)
	if err != nil {
		return "", time.Time{}, err
	}
	return tok, issuedAt.Add(AccessTTL), nil
}

// 编译期契约检查 —— ⚠️ v0.2.0 给 AccessTokenIssuer 加了 issuedAt 参数，
// 少了这一行，签名变化要到运行时才发现。
var _ localauth.AccessTokenIssuer = (*Service)(nil).IssueAccessToken

// Claims 是一张 access token 解出来的东西。
type Claims struct {
	UserID    string
	SessionID string
}

// ParseAccessToken 验签并取出 claims。
// 显式限定 HS256：否则可用 alg=none 或换成非对称算法绕过验签。
func (s *Service) ParseAccessToken(raw string) (Claims, error) {
	t, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired())
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	m, ok := t.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, ErrInvalidToken
	}
	sub, _ := m["sub"].(string)
	sid, _ := m["sid"].(string)
	// ⭐ 形态在这一层就校严：sub 必须是 canonical UUID。
	// ⚠️ 少了这步，畸形 sub 会一路传到 SQL 才炸，那时错误信息里
	// 已经没有「这是个 token 问题」的线索了。
	if _, err := userid.Encode(sub); err != nil {
		return Claims{}, ErrInvalidToken
	}
	// ⚠️ sid 允许为空：阶段 3 之前签发的票没有它。
	// ⛔ 但「登出其它设备」在 sid 为空时必须拒绝，⛔ 不能退化成「登出全部」。
	return Claims{UserID: sub, SessionID: sid}, nil
}

// ── 改密 / 首次设密（阶段 4）──────────────────────────────────────

// ChangeResult 是改密对外的结果。
type ChangeResult struct {
	// Breached / BreachCount / Checked ⚠️ 必须一路带到前端。
	//
	// ⛔ 前端不得把 Checked=false && Breached=false 显示成「口令安全」——
	// Checked=false 有两种成因（未启用 / 查询失败），对用户没有区别：
	// 两种情况下「没有警告」都不等于「这个口令是安全的」。
	Breached    bool
	BreachCount int
	Checked     bool

	// RevokedCount 被吊销的会话数（含当前那条）。
	RevokedCount int
	// Pair ⚠️ 为 nil 表示【没有重签】—— 用户需要重新登录。
	// ⛔ 不等于改密失败：口令已经改好了。
	Pair *Pair
}

// ChangePassword 改密 / 首次设密。
//
// ⭐ 两件事共用一个入口是【模块的设计】，⛔ 不是我图省事：
// 「要不要验旧口令」由 Credential.Hash 是否为空【单独判定】，
// ⛔ 不由调用方传不传 oldPassword 决定 ——
// 否则调用方漏传就等于跳过了旧口令校验。
//
// ⚠️ 编排顺序在模块里（验旧 → 评估新 → 写 hash+changedAt → 吊销全部 → 重签当前设备）。
// 顺序错了会产生没有症状的缺口：先写 hash 后验旧口令 = 任何人能改任何人的口令；
// 先重签后吊销 = 刚签出来的那对立刻被自己吊销、用户当场掉线。
// ⛔ melete 不重新实现这个顺序。
func (s *Service) ChangePassword(ctx context.Context, userID, oldPassword, newPassword,
	currentSessionID, deviceInfo, clientIP string) (*ChangeResult, error) {
	if s.passwords == nil {
		return nil, errors.New("改密未启用：PasswordGuard 未接线")
	}
	out, err := s.passwords.Change(ctx, localauth.ChangeRequest{
		UserID:           userID,
		OldPassword:      oldPassword,
		NewPassword:      newPassword,
		CurrentSessionID: currentSessionID,
		DeviceInfo:       deviceInfo,
		ClientIP:         clientIP,
	})
	if err != nil {
		return nil, translateChange(err)
	}
	res := &ChangeResult{
		Breached: out.Advice.Breached, BreachCount: out.Advice.BreachCount,
		Checked: out.Advice.Checked, RevokedCount: out.RevokedCount,
	}
	if out.NewRefreshToken != "" {
		a, ferr := s.accounts.FindByID(ctx, userID)
		if ferr != nil {
			return nil, fmt.Errorf("改密后取账号: %w", ferr)
		}
		res.Pair = &Pair{
			AccessToken:  out.NewAccessToken,
			RefreshToken: out.NewRefreshToken,
			ExpiresIn:    int(AccessTTL.Seconds()),
			AccountID:    a.ID,
			Display:      a.DisplayName(),
		}
	}
	return res, nil
}

// ErrWeakPassword / ErrOldPasswordWrong 是改密路径【必须区分】的两个错误。
//
// ⚠️ 与登录路径相反：登录时「密码错」与「用户不存在」必须不可区分（防枚举），
// 而改密时调用方【已经通过认证】，「旧口令错」与「新口令太短」对他是
// 两种完全不同的操作提示。⛔ 混成一个会让用户不知道该改什么。
var (
	ErrOldPasswordWrong = errors.New("当前密码不正确")
	ErrWeakPassword     = errors.New("新密码不符合要求")
)

func translateChange(err error) error {
	c, ok := codeOf(err)
	if !ok {
		return err
	}
	switch c {
	case localauth.CodeMisconfigured:
		return err // ⛔ 接线错误原样上抛 → 500，⛔ 不伪装成用户输错
	case localauth.CodePasswordTooShort, localauth.CodePasswordTooLong:
		return ErrWeakPassword
	case localauth.CodeInvalidCredentials:
		return ErrOldPasswordWrong
	case localauth.CodeInvalidCode:
		// ⚠️ 「根本没有码 / 码错 / 已过期 / 试太多次」对外都是它 ——
		// ⛔ 区分它们等于告诉攻击者「你猜的方向对不对」。
		return ErrInvalidCode
	case localauth.CodeEmailTaken:
		return ErrEmailTaken
	default:
		return err
	}
}

// ── 注册 / 找回 / 改邮箱（阶段 5）──────────────────────────────────

// WithEmailFlows 接上邮件家族的三个守卫。
func (s *Service) WithEmailFlows(reg *localauth.RegistrationGuard,
	rec *localauth.RecoveryGuard, ec *localauth.EmailChangeGuard) *Service {
	s.registration, s.recovery, s.emailChange = reg, rec, ec
	return s
}

// SendRegisterCode 发注册验证码。
//
// ⭐ 模块的编排：限流（地址+IP）→ 查重 → 发码。
// ⚠️ 邮箱已被占用时，对请求方【仍然返回成功】—— 否则这个端点就是一个
// 「这个邮箱注册过没有」的查询接口。真正的处置是给那个地址的主人发一封
// 「有人用你的邮箱注册」的通知，⭐ 让知情权落在地址主人身上而不是请求方。
func (s *Service) SendRegisterCode(ctx context.Context, username, email, clientIP string) error {
	if s.registration == nil {
		return errors.New("注册未启用")
	}
	if err := s.registration.SendCode(ctx, localauth.SendCodeRequest{
		Username: username, Email: email, ClientIP: clientIP,
	}); err != nil {
		return translateSend(err)
	}
	return nil
}

// Register 完成注册。
func (s *Service) Register(ctx context.Context, username, password, displayName, email, code,
	deviceInfo, clientIP string) (*Pair, *ChangeResult, error) {
	if s.registration == nil {
		return nil, nil, errors.New("注册未启用")
	}
	out, err := s.registration.Register(ctx, localauth.RegistrationRequest{
		Username: username, Password: password, DisplayName: displayName,
		Email: email, Code: code,
	})
	if err != nil {
		return nil, nil, translateRegister(err)
	}
	a, err := s.accounts.FindByID(ctx, out.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("注册后取账号: %w", err)
	}
	pair, err := s.issue(ctx, a, deviceInfo, clientIP)
	if err != nil {
		return nil, nil, err
	}
	// ⭐ 泄露评估带给前端（裁决 D5：命中也放行）。
	return pair, &ChangeResult{
		Breached: out.Advice.Breached, BreachCount: out.Advice.BreachCount, Checked: out.Advice.Checked,
	}, nil
}

// RequestRecovery 发找回码。
//
// ⛔⛔ 无论地址存不存在、有没有已验证邮箱，对外【一律成功】。
// ⚠️ 任何差别（返回码、耗时、文案）都会让这个端点变成一个
// 「这个邮箱有账号吗」的查询接口。
// ⭐ 模块内部：查不到 / 未验证 / 联邦账号走同一条路（案卷 §18.3.2
// 「不实现 = 安全」）。
func (s *Service) RequestRecovery(ctx context.Context, address, clientIP string) error {
	if s.recovery == nil {
		return errors.New("找回未启用")
	}
	if err := s.recovery.Request(ctx, address, clientIP); err != nil {
		return translateSend(err)
	}
	return nil
}

// CompleteRecovery 验码并重置口令。
func (s *Service) CompleteRecovery(ctx context.Context, address, code, newPassword string) (*ChangeResult, error) {
	if s.recovery == nil {
		return nil, errors.New("找回未启用")
	}
	adv, err := s.recovery.Complete(ctx, localauth.CompleteRequest{
		Address: address, Code: code, NewPassword: newPassword,
	})
	if err != nil {
		return nil, translateChange(err)
	}
	return &ChangeResult{Breached: adv.Breached, BreachCount: adv.BreachCount, Checked: adv.Checked}, nil
}

// RequestEmailChange 给【新地址】发码。⛔ 不改库。
func (s *Service) RequestEmailChange(ctx context.Context, userID, newEmail, clientIP string) error {
	if s.emailChange == nil {
		return errors.New("改邮箱未启用")
	}
	if err := s.emailChange.Request(ctx, localauth.EmailChangeRequest{
		UserID: userID, NewEmail: newEmail, ClientIP: clientIP,
	}); err != nil {
		return translateSend(err)
	}
	return nil
}

// ConfirmEmailChange 验码 → 写入新地址 → 通知旧地址 → 吊销 → 重签。
//
// ⭐ 「通知旧地址」不是礼貌，是安全要件：它是账号接管链上
// 【唯一会让受害者察觉】的信号（案卷 §18.5）。
func (s *Service) ConfirmEmailChange(ctx context.Context, userID, code,
	currentSessionID, deviceInfo, clientIP string) (*Pair, error) {
	if s.emailChange == nil {
		return nil, errors.New("改邮箱未启用")
	}
	out, err := s.emailChange.Confirm(ctx, localauth.EmailChangeConfirm{
		UserID: userID, Code: code, CurrentSessionID: currentSessionID,
		DeviceInfo: deviceInfo, ClientIP: clientIP,
	})
	if err != nil {
		return nil, translateChange(err)
	}
	if out.Reissued.RefreshToken == "" {
		return nil, nil // ⚠️ 没重签：邮箱【已经改好了】，只是用户需重新登录。⛔ 不是失败。
	}
	a, ferr := s.accounts.FindByID(ctx, userID)
	if ferr != nil {
		return nil, fmt.Errorf("改邮箱后取账号: %w", ferr)
	}
	return &Pair{
		AccessToken: out.Reissued.AccessToken, RefreshToken: out.Reissued.RefreshToken,
		ExpiresIn: int(AccessTTL.Seconds()), AccountID: a.ID, Display: a.DisplayName(),
	}, nil
}

// ErrTooManyCodes 发码限流。
//
// ⭐ 与登录的退避【不同】：这个可以对外明说。
// ⚠️ 因为它按【地址/IP】计数，与账号是否存在无关 ⇒ ⛔ 不泄漏任何信息。
// 而登录退避一旦可见就是预言机（一次探测即读出该账号近期有无失败）。
var (
	ErrTooManyCodes  = errors.New("验证码发送过于频繁，请稍后再试")
	ErrInvalidCode   = errors.New("验证码无效或已过期")
	ErrUsernameTaken = errors.New("用户名已被占用")
	ErrEmailTaken    = errors.New("邮箱已被占用")
)

func translateSend(err error) error {
	c, ok := codeOf(err)
	if !ok {
		return err
	}
	switch c {
	case localauth.CodeMisconfigured:
		return err
	case localauth.CodeTooManyRequests:
		return ErrTooManyCodes
	default:
		return err
	}
}

func translateRegister(err error) error {
	c, ok := codeOf(err)
	if !ok {
		return err
	}
	switch c {
	case localauth.CodeMisconfigured:
		return err
	case localauth.CodeUsernameTaken:
		// ⭐ 用户名本来就公开（登录要用它），明说无妨 —— 案卷 §22 同源。
		return ErrUsernameTaken
	case localauth.CodeEmailTaken:
		return ErrEmailTaken
	case localauth.CodeInvalidCode:
		return ErrInvalidCode
	case localauth.CodePasswordTooShort, localauth.CodePasswordTooLong:
		return ErrWeakPassword
	case localauth.CodeTooManyRequests:
		return ErrTooManyCodes
	default:
		return err
	}
}
