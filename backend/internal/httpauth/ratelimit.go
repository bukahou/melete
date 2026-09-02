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
// 单实例够用，多实例时各自限流仍能把总量压到可接受范围。
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

// clientIP 取真实客户端 IP。
// 生产在 Cloudflare Tunnel + Gateway 之后，链路会带 X-Forwarded-For；
// chi 的 RealIP 中间件已把它解析进 RemoteAddr，这里直接取即可。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
