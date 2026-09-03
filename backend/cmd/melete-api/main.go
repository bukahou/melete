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

	"github.com/bukahou/melete/backend/internal/account"
	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/bank"
	"github.com/bukahou/melete/backend/internal/config"
	"github.com/bukahou/melete/backend/internal/httpapi"
	"github.com/bukahou/melete/backend/internal/httpauth"
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
	accountSvc := account.NewService(account.NewMySQLRepository(db))
	studySvc := study.NewService(db, questionRepo)
	tokenIssuer := token.NewIssuer(db, cfg.JWTSecret)
	oidcVerifier := token.NewOIDCVerifier(cfg.OIDCIssuer, cfg.OIDCClientID)
	server := httpapi.NewServer(bankSvc, questionSvc, accountSvc, studySvc, tokenIssuer, oidcVerifier, log)

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
	authPrefix := apiBase + "/auth/"
	loginLimiter := httpauth.NewRateLimit(cfg.LoginRateMax, cfg.LoginRateWindow)

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
	}, accountSvc, tokenIssuer, log)

	r.Route(apiBase, func(v1 chi.Router) {
		// 认证端点对公网开放，是唯一可被无限试探的入口 —— 按 IP 限流
		v1.Use(loginLimiter.Middleware(authPrefix))
		// 业务端点要求 access token；认证端点豁免（那时还没有 token）
		v1.Use(httpauth.RequireUserExcept(tokenIssuer, authPrefix))
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
