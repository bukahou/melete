// oidc.go：后端替所有客户端当 Akasha 的 OIDC client —— 两个【浏览器导航】端点。
//
// 为什么是后端而不是 App / web 自己走 OIDC（2026-09-03，iOS 真机实测得出的结论）：
//   · Akasha 里 melete 是 confidential client，/token 强制校验 client_secret，
//     不因带 PKCE 而放行 —— 原生 App 拿不到 secret，兑换必失败
//   · Akasha 是 pairwise sub（HMAC(client_id, user)）—— 另注册一个 public client
//     会让同一个人在 iOS 上变成另一个 melete 账号
//   · 所以 Akasha 只该见过【一个】client：本服务。web 与 iOS 都经这里进出，
//     同一 client_id → 同一 sub → 同一账号。web 从此不再持有 client_secret。
//
// 流程：客户端打开 /auth/oidc/start?next=… → 302 Akasha → 回 /auth/oidc/callback
// → 验 state / 换 code / 验 id_token+nonce（akasha/pkg/oidcrp v0.1.0）→ EstablishSSO → 签本站双 token
// → 302 回客户端。id_token 到此为止，客户端手里只有本站签的 token。
//
// 照搬 geass-v3 的 internal/gateway/account/oidc_handler.go，差别只在「回客户端」那一段：
// geass web 用 localStorage 存 token，所以整对 token 走 fragment；melete web 用 httpOnly
// cookie，浏览器脚本碰不到 fragment，改为把 refresh token 当一次性票据走 query，
// web 服务端拿它去 /auth/refresh 换新的一对 —— refresh 轮换让 URL 里那个立即失效。
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bukahou/akasha/pkg/oidcrp"
	"github.com/go-chi/chi/v5"

	"github.com/bukahou/melete/backend/internal/account"
	"github.com/bukahou/melete/backend/internal/token"
)

// OIDCConfig 后端作为 OIDC client 所需的全部配置。
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL 本服务的回调地址，须与 Akasha 白名单逐字一致。
	RedirectURL string
	// CookieSecret 状态 cookie 的签名密钥（oidcrp 会 HKDF 派生，可与 JWT 密钥同源）。
	CookieSecret string
	// WebOrigin web 前端的源，登录完成后把浏览器送回这里。
	WebOrigin string
	// MountPath 两个端点挂在哪个前缀下（含 /auth/oidc），用作状态 cookie 的 Path。
	MountPath string
}

// nativeCallback 原生 App 的回调地址 —— 精确常量，不是前缀白名单。
//
// 放行任意 "melete://" 开头的地址等于把开放重定向搬到自定义 scheme 上。
// 自定义 scheme 在 iOS 上任何 App 都能注册（RFC 8252 §8.1），真正挡住抢注的是
// 客户端必须用 ASWebAuthenticationSession —— 系统把回调直接交回发起会话的 App。
const nativeCallback = "melete://auth/callback"

// initRetryInterval Akasha 不可达时的最小重试间隔，免得每个请求都去拉 discovery。
const initRetryInterval = 15 * time.Second

func isNative(next string) bool {
	return next == nativeCallback || strings.HasPrefix(next, nativeCallback+"?")
}

// safeNext 在 oidcrp 默认策略（只放行本站绝对路径）之上额外放行原生回调。
func safeNext(next string) bool {
	return isNative(next) || oidcrp.DefaultSafeNext(next)
}

// OIDCHandler 挂载 /auth/oidc/{start,callback}。
//
// flow 懒初始化：构造时试拉一次 Akasha 的 discovery，拉不到不算失败 ——
// Akasha 是登录页上的一个额外按钮，不是本服务的依赖，密码登录不受影响。
type OIDCHandler struct {
	mu          sync.Mutex
	flow        *oidcrp.Flow
	lastAttempt time.Time

	cfg      OIDCConfig
	accounts account.Service
	tokens   *token.Issuer
	log      *slog.Logger
}

func NewOIDCHandler(ctx context.Context, cfg OIDCConfig, accounts account.Service, tokens *token.Issuer, log *slog.Logger) *OIDCHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &OIDCHandler{cfg: cfg, accounts: accounts, tokens: tokens, log: log}
	if err := h.ensureFlow(ctx); err != nil {
		log.Warn("Akasha 第三方登录暂不可用，将在请求时重试", "issuer", cfg.Issuer, "err", err)
	} else {
		log.Info("Akasha 第三方登录已就绪", "issuer", cfg.Issuer, "redirect", cfg.RedirectURL)
	}
	return h
}

func (h *OIDCHandler) ensureFlow(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.flow != nil {
		return nil
	}
	if time.Since(h.lastAttempt) < initRetryInterval {
		return errors.New("Akasha 暂不可用，稍后重试")
	}
	h.lastAttempt = time.Now()

	flow, err := oidcrp.New(ctx, oidcrp.Config{
		IssuerURL:    h.cfg.Issuer,
		ClientID:     h.cfg.ClientID,
		ClientSecret: h.cfg.ClientSecret,
		RedirectURL:  h.cfg.RedirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		CookieSecret: h.cfg.CookieSecret,
		CookiePrefix: "melete_oidc_",
		CookiePath:   strings.TrimRight(h.cfg.MountPath, "/") + "/",
		CookieSecure: strings.HasPrefix(h.cfg.RedirectURL, "https://"),
		Logger:       h.log,

		OnAuthenticated: h.onAuthenticated,
		OnError:         h.onError,
		SafeNext:        safeNext,
	})
	if err != nil {
		return fmt.Errorf("初始化 Akasha 登录流程: %w", err)
	}
	h.flow = flow
	return nil
}

// Register 挂到 chi 路由。走裸路由而非 OpenAPI 生成层：响应是 302 + Set-Cookie，
// 生成层是为 JSON 设计的。规格里仍登记了这两个端点（codegen 排除其 operationId）。
func (h *OIDCHandler) Register(r chi.Router) {
	r.Get("/auth/oidc/start", h.withFlow(func(f *oidcrp.Flow) http.HandlerFunc { return f.Start }))
	r.Get("/auth/oidc/callback", h.withFlow(func(f *oidcrp.Flow) http.HandlerFunc { return f.Callback }))
}

func (h *OIDCHandler) withFlow(pick func(*oidcrp.Flow) http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h.ensureFlow(r.Context()); err != nil {
			h.log.WarnContext(r.Context(), "Akasha 第三方登录不可用", "err", err)
			// flow 还没建起来，next 只能从 query 现取 —— 攻击者可控的原始输入，先过白名单
			next := r.URL.Query().Get("next")
			if !safeNext(next) {
				next = ""
			}
			h.onError(w, r, oidcrp.ReasonUpstream, next)
			return
		}
		h.mu.Lock()
		f := h.flow
		h.mu.Unlock()
		pick(f)(w, r)
	}
}

// onAuthenticated Akasha 那边验完了：确立账号、签本站 token、送回客户端。
func (h *OIDCHandler) onAuthenticated(w http.ResponseWriter, r *http.Request, res oidcrp.Result) (string, error) {
	id := res.Identity
	display := id.Name
	if display == "" {
		display = id.PreferredUsername
	}
	if display == "" {
		display = "学习者"
	}
	acct, err := h.accounts.EstablishSSO(r.Context(), id.Subject, display)
	if err != nil {
		return "", fmt.Errorf("确立账号: %w", err)
	}
	device := "oidc/web"
	if isNative(res.Next) {
		device = "oidc/ios"
	}
	pair, err := h.tokens.Issue(r.Context(), acct.ID, device)
	if err != nil {
		return "", fmt.Errorf("签发 token: %w", err)
	}
	return h.clientCallbackURL(pair, res.Next), nil
}

// clientCallbackURL 把 token 交回客户端。
//
// 原生：整对 token 走 fragment —— fragment 不发给任何服务器，不进日志与 Referer；
//       系统认证会话看到这个 scheme 就结束并把 URL 交回 App。
// web： 只把 refresh token 当票据走 query（web 服务端在 route handler 里读，
//       浏览器脚本不参与），随后立刻拿它去 /auth/refresh 换一对新的 ——
//       refresh 轮换让 URL 里那个用一次即废，日志里留下的是张作废的票。
func (h *OIDCHandler) clientCallbackURL(pair *token.Pair, next string) string {
	if isNative(next) {
		frag := url.Values{}
		frag.Set("access_token", pair.AccessToken)
		frag.Set("refresh_token", pair.RefreshToken)
		frag.Set("expires_in", fmt.Sprintf("%d", pair.ExpiresIn))
		return next + "#" + frag.Encode()
	}
	q := url.Values{}
	q.Set("ticket", pair.RefreshToken)
	if next != "" {
		q.Set("next", next) // 已在 Start 时过了 SafeNext
	}
	return strings.TrimRight(h.cfg.WebOrigin, "/") + "/auth/callback?" + q.Encode()
}

// onError 登录失败：只带原因码回客户端，不带文案（文案回显 = 借可信域名行骗的口子）。
func (h *OIDCHandler) onError(w http.ResponseWriter, r *http.Request, reason, next string) {
	q := url.Values{}
	q.Set("oidc_error", reason)
	if isNative(next) {
		http.Redirect(w, r, nativeCallback+"?"+q.Encode(), http.StatusFound)
		return
	}
	http.Redirect(w, r, strings.TrimRight(h.cfg.WebOrigin, "/")+"/auth/login?"+q.Encode(), http.StatusFound)
}
