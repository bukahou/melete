// Package health 是给 K8s 的两个探针端点（tracker #46，2026-10-22 用户裁定拆开）。
//
// 两个问题，两个端点 —— ⛔ 别再合成一个：
//
//	/livez   「进程还活着吗」→ K8s 据此决定【要不要重启】。⛔ 不碰数据库。
//	/healthz 「能不能接流量」→ K8s 据此决定【往不往这个 Pod 发请求】。ping 数据库。
//
// ⭐ 为什么要分：原先两个探针打的是同一个会 ping 数据库的端点，数据库一慢，
// K8s 就以为 API 坏了而重启它 —— 可重启解决不了数据库慢，只会让重启叠加在故障上。
// 数据库慢的正确反应是「先别给我发请求」（readiness），不是「杀掉我」（liveness）。
package health

import (
	"context"
	"net/http"
	"time"
)

// DBPinger 是 /healthz 唯一需要的能力。*sql.DB / *sqlx.DB 都满足。
type DBPinger interface {
	PingContext(ctx context.Context) error
}

// LivenessHandler 只要进程能回 HTTP 就回 200。
// ⚠️ 往这里加任何依赖检查（数据库、上游服务）都会把「依赖慢」变成「重启我」—— 正是拆开要避免的事。
func LivenessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
}

// ReadinessHandler ping 数据库，超过 timeout 就回 503。
// ⭐ 超时由应用自己掐：探针的 timeoutSeconds 要比它长（部署配置里写着），
// 这样慢的时候回的是一个明确的 503，而不是被探针截断、看不出原因。
func ReadinessHandler(db DBPinger, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
}
