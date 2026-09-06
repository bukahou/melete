// Package config 集中管理运行时配置。
//
// 纪律（见 CLAUDE.md「凭证与配置纪律」）：
// **凭证类一律无默认值 + required** —— 生产忘配就启动失败，
// 优于默默连上一个错误的库。非凭证项（端口、超时、连接数）才给默认值。
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	// 凭证类：无默认值，required
	DatabaseDSN string `env:"MELETE_DB_DSN,required,notEmpty"`
	// access token 的签发/验签密钥（HS256）。仅本服务持有 ——
	// token 由本服务独家签发，web 与 iOS 都只是持有者，不参与签名。
	JWTSecret string `env:"MELETE_JWT_SECRET,required,notEmpty"`

	// 非凭证类：允许默认值
	// OIDC：验证 Akasha 签发的 id_token（issuer 与 audience 都要对上）
	OIDCIssuer   string `env:"MELETE_OIDC_ISSUER,required,notEmpty"`
	OIDCClientID string `env:"MELETE_OIDC_CLIENT_ID,required,notEmpty"`
	// 后端替所有客户端当 Akasha 的 client（见 httpapi/oidc.go）：secret 只在这里，web 不再持有
	OIDCClientSecret string `env:"MELETE_OIDC_CLIENT_SECRET,required,notEmpty"`
	// 本服务的公网回调地址，须与 Akasha 白名单逐字一致
	OIDCRedirectURL string `env:"MELETE_OIDC_REDIRECT_URL,required,notEmpty"`
	// web 前端的源：OIDC 登录完成后把浏览器送回这里
	WebOrigin string `env:"MELETE_WEB_ORIGIN,required,notEmpty"`

	// Mailer 发信通道。⛔ 无默认值：未配置即启动失败（用户裁决 ③）。
	//
	// ⚠️ 「忘了配」与「故意用 log」必须区分得开 —— 静默降级会让生产
	// 在无人察觉下把验证码明文打进日志。
	Mailer string `env:"MELETE_MAILER"`

	// VerificationPepper 验证码摘要的 pepper。⛔ 凭证类，无默认值 + required。
	//
	// ⚠️ 它不在数据库里 —— 库被读走也无法离线爆破 6 位码。
	// ⛔ 换掉它会让所有在途验证码立刻失效（可接受：TTL 以分钟计）。
	// ⚠️ 模块要求【至少 16 字节】，不足会在启动时报 misconfigured 并退出。
	VerificationPepper string `env:"MELETE_VERIFICATION_PEPPER,required,notEmpty"`

	Addr string `env:"MELETE_ADDR" envDefault:":8080"`
	// 登录端点限流：每 IP 每窗口的最大尝试次数
	LoginRateMax    int           `env:"MELETE_LOGIN_RATE_MAX" envDefault:"10"`
	LoginRateWindow time.Duration `env:"MELETE_LOGIN_RATE_WINDOW" envDefault:"1m"`
	MaxOpenConns    int           `env:"MELETE_DB_MAX_CONNS" envDefault:"20"`
	LogLevel        string        `env:"MELETE_LOG_LEVEL" envDefault:"info"`
}

func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("读取配置失败（凭证类无默认值是有意为之）: %w", err)
	}
	return &cfg, nil
}
