package httpauth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimit 是登录端点的暴力破解防护。
//
// 本 API 对公网暴露后，/auth/password 是唯一可被无限次试探的入口。
// bcrypt 本身就慢（这是它的设计），但慢不等于挡得住 —— 攻击者可以并发。
//
// 用最简单的固定窗口计数，按客户端 IP 划分。不引 Redis：
// ⚠️ 计数是**每副本各一份**，所以有效阈值 = 配置值 × 副本数。
// 线上实测（2 副本、配置 10/分钟）：30 次请求 19 通过 11 拒，实际阈值 ≈ 20。
// 这是有意接受的近似 —— 目的是把爆破从「每秒数千」压到「每分钟几十」，
// 不是精确配额。但**配置值不等于实际阈值**，改副本数会改变安全属性，
// 抄这段代码到别的服务前必须重新算（cross-exam 004 由 geass-v3 指出）。
type RateLimit struct {
	max    int
	window time.Duration

	mu   sync.Mutex
	hits map[string]*counter
}

type counter struct {
	n     int
	reset time.Time
}

func NewRateLimit(max int, window time.Duration) *RateLimit {
	return &RateLimit{max: max, window: window, hits: map[string]*counter{}}
}

// Middleware 只对匹配 prefixes 的路径限流，其余放行。
func (r *RateLimit) Middleware(prefixes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !hasAnyPrefix(req.URL.Path, prefixes) {
				next.ServeHTTP(w, req)
				return
			}
			if !r.allow(clientIP(req)) {
				w.Header().Set("Retry-After", "60")
				writeJSONError(w, http.StatusTooManyRequests, "登录尝试过于频繁，请稍后再试")
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

func (r *RateLimit) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()

	// 顺带清理过期条目，避免 map 随攻击者变换 IP 无限增长
	for k, c := range r.hits {
		if now.After(c.reset) {
			delete(r.hits, k)
		}
	}
	c, ok := r.hits[key]
	if !ok || now.After(c.reset) {
		r.hits[key] = &counter{n: 1, reset: now.Add(r.window)}
		return true
	}
	c.n++
	return c.n <= r.max
}

func hasAnyPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if len(path) >= len(p) && path[:len(p)] == p {
			return true
		}
	}
	return false
}

// clientIP 取限流用的客户端标识。
//
// ⚠️ 2026-09-03 实测漏洞与修复 —— 这里曾经直接用 RemoteAddr，而 main 挂了
// chi 的 middleware.RealIP。RealIP 会**无条件信任** X-Forwarded-For / X-Real-IP
// 并取其第一个值，而 Cloudflare 是把真实 IP **追加**在客户端自带的 XFF 之后：
//
//	客户端发   X-Forwarded-For: 203.0.113.1
//	CF 转发成  X-Forwarded-For: 203.0.113.1, <真实 IP>
//	RealIP 取  203.0.113.1   ← 攻击者控制
//
// 线上实证（melete-api v1.0.0-03ce218）：真实 IP 被限到 429 的同一时刻，
// 带任意伪造 XFF 的请求全部 401 通过 —— **每次换一个值即可无限试探，防护为零**。
//
// 修法：只认由可信代理写入、且客户端伪造无效的头。
//   1. CF-Connecting-IP —— Cloudflare 总是**覆盖**它（不是追加），客户端伪造不了
//   2. 退回 RemoteAddr（TCP 对端，无法伪造）
// 刻意**不读** X-Forwarded-For / X-Real-IP：在本服务的拓扑里它们没有可信来源。
//
// 退回 RemoteAddr 时外部流量会共用网关 pod 的 IP → 退化成全局限流。
// 那是**失败关闭**（误伤真实用户）而非失败打开（放行攻击者），是这里该选的方向。
func clientIP(r *http.Request) string {
	if cf := r.Header.Get("CF-Connecting-IP"); cf != "" {
		return cf
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
