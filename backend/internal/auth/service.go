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
	guard    *localauth.Guard
	sessions *localauth.SessionGuard
	accounts account.Repository
	lookup   localauth.LookupFunc
	secret   []byte
	log      *slog.Logger
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
	access, err := s.signAccess(a.ID, rec.ID)
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
	access, err := s.signAccess(a.ID, out.Session.ID)
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
func (s *Service) signAccess(userID, sessionID string) (string, error) {
	now := time.Now()
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": issuer,
		"sub": userID,
		"sid": sessionID,
		"iat": now.Unix(),
		"exp": now.Add(AccessTTL).Unix(),
	})
	out, err := t.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("签发 access token: %w", err)
	}
	return out, nil
}

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
