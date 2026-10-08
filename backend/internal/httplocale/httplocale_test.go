package httplocale

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPickLocale(t *testing.T) {
	allow := map[string]bool{"zh": true, "ja": true, "en": true}
	cases := []struct {
		header, want, why string
	}{
		{"", "", "没有头 = 不协商，交给源语言"},
		{"ja", "ja", "最简单的情形"},
		{"ja-JP", "ja", "地区子标签退到主语言"},
		{"ko", "", "白名单外的语言不接受，⛔ 不得原样进 SQL 参数"},
		{"ko,ja;q=0.8", "ja", "跳过不支持的，取次优"},
		{"zh;q=0.5,ja;q=0.9", "ja", "q 值决定顺序，⛔ 不是书写顺序"},
		{"*", "", "通配不算表达偏好"},
		{"JA", "ja", "大小写不敏感"},
		{"ja;q=oops", "ja", "q 解析失败按默认 1.0，不因脏输入丢掉整个头"},
	}
	for _, c := range cases {
		if got := pickLocale(c.header, allow); got != c.want {
			t.Errorf("pickLocale(%q) = %q, 期望 %q（%s）", c.header, got, c.want, c.why)
		}
	}
}

// TestNegotiateLocaleAlwaysVaries 锁住那条最容易被「优化」掉的行为：
// 协商不出结果时【依然】要发 Vary。否则缓存会把某一语言的响应喂给所有人。
func TestNegotiateLocaleAlwaysVaries(t *testing.T) {
	for _, header := range []string{"ja", "ko", ""} {
		var seen string
		h := NegotiateLocale("zh", "ja")(http.HandlerFunc(
			func(_ http.ResponseWriter, r *http.Request) { seen = RequestLocale(r.Context()) }))
		req := httptest.NewRequest(http.MethodGet, "/v1/questions/1", nil)
		if header != "" {
			req.Header.Set("Accept-Language", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Vary") != "Accept-Language" {
			t.Errorf("Accept-Language=%q 时缺 Vary 头", header)
		}
		if header == "ja" && seen != "ja" {
			t.Errorf("上下文里的语言 = %q，期望 ja", seen)
		}
		if header != "ja" && seen != "" {
			t.Errorf("Accept-Language=%q 不该协商出 %q", header, seen)
		}
	}
}
