package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// stubDB 按需模拟数据库：正常、直接报错、或很慢（block：2 秒后才回成功，除非 ctx 先结束）。
// ⭐ 慢的那种有上限：处理器若没掐超时，测试会在 2 秒后以「200 + 耗时过长」失败，而不是卡死。
type stubDB struct {
	err   error
	block bool
	calls int
}

func (s *stubDB) PingContext(ctx context.Context) error {
	s.calls++
	if s.block {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return nil
		}
	}
	return s.err
}

func serve(h http.Handler) (int, time.Duration) {
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Code, time.Since(start)
}

func TestLivenessNeverTouchesDatabase(t *testing.T) {
	// 存活探针没有数据库参数 —— 类型上就碰不到；这里只确认它总是 200
	if code, _ := serve(LivenessHandler()); code != http.StatusOK {
		t.Fatalf("/livez = %d, want 200", code)
	}
}

func TestReadiness(t *testing.T) {
	t.Run("数据库正常 → 200", func(t *testing.T) {
		db := &stubDB{}
		if code, _ := serve(ReadinessHandler(db, time.Second)); code != http.StatusOK || db.calls != 1 {
			t.Fatalf("code=%d calls=%d, want 200 / 1", code, db.calls)
		}
	})
	t.Run("数据库报错 → 503", func(t *testing.T) {
		if code, _ := serve(ReadinessHandler(&stubDB{err: errors.New("down")}, time.Second)); code != http.StatusServiceUnavailable {
			t.Fatalf("code=%d, want 503", code)
		}
	})
	t.Run("数据库慢 → 到点自己回 503，不一直挂着", func(t *testing.T) {
		code, took := serve(ReadinessHandler(&stubDB{block: true}, 50*time.Millisecond))
		if code != http.StatusServiceUnavailable {
			t.Fatalf("code=%d, want 503", code)
		}
		if took > time.Second {
			t.Fatalf("took %v: the handler must give up at its own timeout", took)
		}
	})
}
