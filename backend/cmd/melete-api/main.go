// melete-api 是 Melete 的后端服务。
//
// 依赖组装全部发生在这里（构造函数注入），各层之间只通过接口耦合。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "github.com/go-sql-driver/mysql"

	"github.com/bukahou/gokit/localauth"

	"github.com/bukahou/melete/backend/internal/account"
	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/auth"
	"github.com/bukahou/melete/backend/internal/bank"
	"github.com/bukahou/melete/backend/internal/config"
	"github.com/bukahou/melete/backend/internal/httpapi"
	"github.com/bukahou/melete/backend/internal/httpauth"
	"github.com/bukahou/melete/backend/internal/localauthx"
	"github.com/bukahou/melete/backend/internal/platform/database"
	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/study"
	"github.com/bukahou/melete/backend/internal/token"
)

func main() {
	if err := run(); err != nil {
		slog.Error("启动失败", "err", err)
		os.Exit(1)
	}
}

func parseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}

// apiBase 与 OpenAPI 契约的 servers.url 一致，两处若不同步则路由与文档对不上。
const apiBase = "/api/v1"

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}))
	slog.SetDefault(log)

	db, err := database.OpenMySQL(cfg.DatabaseDSN, cfg.MaxOpenConns)
	if err != nil {
		return err
	}
	defer db.Close()

	// 依赖组装：repository → service → handler，方向单向
	questionRepo := question.NewMySQLRepository(db)
	bankSvc := bank.NewService(bank.NewMySQLRepository(db))
	questionSvc := question.NewService(questionRepo)
	accountRepo := account.NewMySQLRepository(db)
	studySvc := study.NewService(db, questionRepo)

	// ── localauth 接线（阶段 3）──────────────────────────────────
	//
	// ⭐ 从这里起，melete 不再有自己的登录逻辑：退避、哈希比对、轮换、
	// 重放判定、账号状态复查全部在模块里。⛔ 本文件只负责把实现接上去。
	creds := localauthx.NewCredentialStore(db)
	ipFailures, err := localauthx.NewFailureStore(db, "ip")
	if err != nil {
		return err
	}
	acctFailures, err := localauthx.NewFailureStore(db, "account")
	if err != nil {
		return err
	}
	// ⚠️ 四个位置参数【必填且无零值语义】—— 模块刻意用位置参数而不是可留空的
	// Config：编译期强于运行期，必填让这个决定必须在【写代码时】做出，
	// 而那是唯一有人在思考部署拓扑的时刻。
	guard, err := localauth.New(
		// ⭐ melete 走 CF Tunnel，origin 是 LAN 地址无公网入口
		// ⇒ 案卷 §2.11 检查③「不可绕过」结构上天然满足。
		localauth.TrustCloudflare(),
		// ⚠️ melete 目前【开放注册】，准入返回 nil。
		// ⛔ 这是有意的现状记录，不是遗漏 —— 案卷 §19.4 记着 melete
		// 「违反 5.3 JIT 约束」这笔债，它属 OIDC 侧，§35 已划归 akasha 范围。
		localauth.AdmissionFunc(func(context.Context, localauth.AdmitRequest) error { return nil }),
		ipFailures, acctFailures,
		localauth.WithAuditHook(auditTo(log)),
	)
	if err != nil {
		return err
	}
	sessionGuard, err := localauth.NewSessionGuard(
		localauthx.NewSessionStore(db),
		creds.AccountStatus,
		auth.RefreshTTL,
		localauth.WithSessionAudit(auditTo(log)),
	)
	if err != nil {
		return err
	}
	authSvc := auth.NewService(guard, sessionGuard, accountRepo,
		creds.LookupHashByUsername, cfg.JWTSecret, log)

	// ── 改密 / 首次设密 / HIBP（阶段 4）────────────────────────────
	//
	// ⭐ HIBP 口径沿用 §29「检测 + 警告放行」：查到泄露【不拒绝】，
	// 把次数带到前端由用户决定。⚠️ 理由是拒绝会把一次泄露查询变成
	// 一个可被枚举的口令预言机，而警告已经达到了目的。
	//
	// ⚠️ fail-open 在模块的 PasswordPolicy 里：HIBP 查不通时
	// Advice.Checked=false 而【不阻断改密】——
	// ⛔ 第三方服务抖动不该让用户改不了密码。
	policy := localauth.NewPasswordPolicy(
		localauth.WithBreachChecker(localauth.NewPwnedRangeChecker()),
		localauth.WithPolicyAudit(auditTo(log)),
	)
	passwordGuard, err := localauth.NewPasswordGuard(
		creds, sessionGuard, policy,
		// ⭐ 把 access 签发能力交给模块：改密后它会为当前设备重签，
		// 用户不必重新登录。⚠️ issuedAt 由模块算（见 auth.signAccess 的注释）。
		localauth.WithAccessTokenIssuer(authSvc.IssueAccessToken),
		localauth.WithPasswordAudit(auditTo(log)),
		// ⛔ 刻意【不传】 WithPasswordRevoker：melete 没有吊销存储（无 Redis）。
		// ⚠️ 后果如实记录：改密会吊销全部【refresh】会话，但已签发的
		// access token 在 TTL（15min）内仍然有效。那是裁决 ② 已接受的风险
		// （无 Redis ⇒ TTL 就是残留窗口），⛔ 不是这里漏配。
	)
	if err != nil {
		return err
	}
	authSvc = authSvc.WithPasswordGuard(passwordGuard)

	// ── 注册 / 找回 / 改邮箱（阶段 5）────────────────────────────
	//
	// ⛔ 发信通道未配置即【启动失败】（用户裁决 ③）：
	// 「忘了配」与「故意用 log」必须区分得开 —— 静默降级会让生产
	// 在无人察觉下把验证码明文打进日志。
	mailer, err := localauthx.NewMailer(cfg.Mailer, log)
	if err != nil {
		return err
	}
	// ⭐ 发码限流复用 login_failures 表，两个新 scope。
	// ⚠️ 发码是【外部成本】：被刷 = 邮件配额烧光 + 对任意邮箱的骚扰。
	// 参数用模块的默认值（用户裁决 D4：地址 3/10min + 60s 冷却、IP 20/h）。
	verifyAddrFailures, err := localauthx.NewFailureStore(db, "vaddr")
	if err != nil {
		return err
	}
	verifyIPFailures, err := localauthx.NewFailureStore(db, "vip")
	if err != nil {
		return err
	}
	verifyGuard, err := localauth.NewVerificationGuard(
		localauthx.NewVerificationStore(db), mailer,
		verifyAddrFailures, verifyIPFailures,
		[]byte(cfg.VerificationPepper),
		localauth.WithSendThrottle(localauth.DefaultSendThrottle()),
		localauth.WithVerificationAudit(auditTo(log)),
	)
	if err != nil {
		return err
	}
	registrationGuard, err := localauth.NewRegistrationGuard(
		verifyGuard, creds,
		// ⚠️ melete 目前开放注册，准入返回 nil —— 与登录守卫同一现状记录。
		localauth.AdmissionFunc(func(context.Context, localauth.AdmitRequest) error { return nil }),
		policy,
		localauth.WithRegistrationAudit(auditTo(log)),
	)
	if err != nil {
		return err
	}
	recoveryGuard, err := localauth.NewRecoveryGuard(
		verifyGuard, localauthx.NewRecoveryResolver(db), passwordGuard,
		localauth.WithRecoveryAudit(auditTo(log)),
	)
	if err != nil {
		return err
	}
	emailChangeGuard, err := localauth.NewEmailChangeGuard(
		verifyGuard, localauthx.NewEmailChanger(db), sessionGuard,
		// ⭐ 改邮箱后为当前设备重签，用户不必重新登录。
		localauth.WithEmailChangeReissuer(
			localauth.NewDeviceReissuer(sessionGuard, authSvc.IssueAccessToken)),
		localauth.WithEmailChangeAudit(auditTo(log)),
	)
	if err != nil {
		return err
	}
	authSvc = authSvc.WithEmailFlows(registrationGuard, recoveryGuard, emailChangeGuard)

	oidcVerifier := token.NewOIDCVerifier(cfg.OIDCIssuer, cfg.OIDCClientID)
	server := httpapi.NewServer(bankSvc, questionSvc, studySvc, authSvc, oidcVerifier, log)

	r := chi.NewRouter()
	// ⚠️ 刻意【不用】middleware.RealIP：它无条件信任 X-Forwarded-For 的第一个值，
	// 而 CF 是把真实 IP 追加在客户端自带的 XFF 之后 —— 于是限流键变成攻击者可控。
	// 2026-09-03 实证可绕过，详见 httpauth/ratelimit.go 的 clientIP 注释。
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// 没有 CORS：浏览器不用 XHR 直连本服务 —— web 经服务端 BFF 调用，iOS 是原生请求，
	// OIDC 两个端点是整页导航（302），都不受同源策略约束。

	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		if err := db.PingContext(req.Context()); err != nil {
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})

	// 契约生成的路由挂在 /api/v1 下，与 OpenAPI 的 servers 一致。
	// 认证见 internal/httpauth：所有端点要服务间密钥，业务端点额外要会话 JWT。
	// ⛔⛔ 免认证端点必须【逐条列出完整路径】，⛔ 不能用前缀。
	//
	// ⚠️ 2026-09-07 从 `apiBase + "/auth/"` 改过来：加 /auth/sessions
	// （会话列表，必须认证）时它直接落进了那个豁免前缀，端点等于没挂认证。
	// ⭐ 前缀豁免的问题不是「这次漏了一个」，是【默认值通向不安全的一侧】：
	// 以后每个加在 /auth/ 下的端点都默认公开，而加端点的人不会去读中间件。
	// 改成完整路径之后，忘记登记的新端点会【要求认证】而不是【放弃认证】。
	//
	// ⚠️ 加新的免认证端点时，必须在这里显式加一行 —— 那正是希望发生的摩擦。
	publicPaths := []string{
		apiBase + "/auth/password",      // 登录：这时还没有 token
		apiBase + "/auth/refresh",       // 刷新：拿 refresh 换，不看 access
		apiBase + "/auth/logout",        // 登出：同上
		apiBase + "/auth/sso",           // id_token 换本站 token
		apiBase + "/auth/oidc/start",    // 浏览器导航，手写挂载
		apiBase + "/auth/oidc/callback", // 同上
		// ── 阶段 5 的公开端点（注册，此时用户当然还没有 token）──
		apiBase + "/auth/register/code",
		apiBase + "/auth/register",
		// ⛔ 找回密码的两个端点【暂时移出白名单】(2026-09-07)：
		// 生产发信通道尚是 log 型（验证码不经安全信道投递），在有真实邮件通道之前
		// 不对匿名开放找回 —— 否则任何人可为任意邮箱触发验证码。前端入口也已同步撤下。
		// ⭐ 恢复时：配好真实 MessageSender 后，把下面两行放回本白名单。
		//   apiBase + "/auth/recovery/code",
		//   apiBase + "/auth/recovery/complete",
		// ⚠️ ⛔ 改邮箱的两个端点【不在这里】：/auth/email/code 与
		// /auth/email/confirm 必须认证 —— 它们改的是【当前登录用户】的邮箱。
		// ⭐ 而 /auth/register 与它们共享 /auth/ 前缀：若豁免还是前缀匹配，
		// 改邮箱就会变成公开的，任何人能把任何账号的邮箱改成自己的。
	}

	ctx := context.Background()
	// 后端替 web 与 iOS 当 Akasha 的 OIDC client（两个浏览器导航端点，绕开 JSON 生成层）
	oidcHandler := httpapi.NewOIDCHandler(ctx, httpapi.OIDCConfig{
		Issuer:       cfg.OIDCIssuer,
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  cfg.OIDCRedirectURL,
		CookieSecret: cfg.JWTSecret, // oidcrp 会 HKDF 派生，不直接使用
		WebOrigin:    cfg.WebOrigin,
		MountPath:    apiBase + "/auth/oidc",
	}, authSvc, log)

	r.Route(apiBase, func(v1 chi.Router) {
		// ⛔ 这里曾经有一个按 IP 的登录限流器，2026-09-06 用户裁定整个删除：
		// 「限流暂不考虑（现在基本没人），整体纳入集群层待办」。
		//
		// ⚠️ 这是一次【有意的风险接受】，不是问题被解决了：
		// 当前 /auth/* 零速率防护。模块的退避只按 IP + 账号计数，
		// 而轮换 IP + 轮换用户名的洪水两个键都不重复 ⇒ bcrypt 每次都跑，
		// 且【不需要任何凭据】。真正的防线在集群入口层（Cilium CEC +
		// Envoy local_ratelimit，键取 CF-Connecting-IP），已登记待办。
		// ⛔ 删掉这段代码不等于那条债还清了。
		// ⭐ 先解析可信客户端 IP —— 模块的 Guard 只收结果不做解析
		// （它的调用方常常隔着一次 RPC）。⚠️ 解析失败不阻断请求：
		// 那意味着请求没走预期链路，模块会降级成只按账号维度退避。
		v1.Use(httpauth.ResolveClientIP(localauth.TrustCloudflare()))
		// 业务端点要求 access token；认证端点豁免（那时还没有 token）
		v1.Use(httpauth.RequireUserExcept(authSvc, publicPaths...))
		oidcHandler.Register(v1) // 在 /auth/ 前缀下：限流与豁免天然覆盖
		api.HandlerFromMux(api.NewStrictHandler(server, nil), v1)
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 优雅关闭：收到信号后停止接受新连接，给在途请求 15 秒收尾
	idle := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Info("收到关闭信号，正在优雅退出")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("优雅关闭超时", "err", err)
		}
		close(idle)
	}()

	log.Info("melete-api 启动", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-idle
	return nil
}

// auditTo 把模块的审计事件送进本服务的日志。
//
// ⚠️ 模块把「降级」「IP 被拦」「会话吊销失败」这类事情做成【可计数的事件】
// 而不是静默行为 —— 接不上这个钩子，那些事情就只在模块内部发生过。
// ⭐ 案卷记过一个同类系统实测栽过的缺陷：会话审计事件一条不记，
// 于是吊销失败在日志里毫无症状。
func auditTo(log *slog.Logger) localauth.AuditHook {
	return func(ctx context.Context, e localauth.AuditEvent) {
		log.LogAttrs(ctx, slog.LevelInfo, "localauth",
			slog.String("kind", string(e.Kind)),
			slog.String("username", e.Username),
			slog.String("user_id", e.UserID),
			slog.String("client_ip", e.ClientIP),
			slog.String("detail", e.Detail),
		)
	}
}
