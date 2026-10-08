// Package httplocale 做入站的语言协商。
//
// ⭐ 用【标准 HTTP 内容协商】(Accept-Language) 而不是自造一个 ?locale= 查询参数：
//   - 语言是【整个响应】的属性，不是某一个端点的筛选条件。做成查询参数意味着
//     以后每加一个返回正文的端点都要记得补一个参数，忘了就默认回英/中 —— 又是
//     「默认值通向不安全的那一侧」那个形状。
//   - Vary: Accept-Language 让任何中间缓存（CDN / 浏览器）天然按语言分桶。
//     查询参数虽然也能分桶，但它把语言混进了「资源标识」里，同一道题会有两个 URL。
//
// ⚠️ 本包【不】决定回退策略。协商不出结果时给空串，由领域层理解成
// 「只要源语言」—— 缺译文时回退源语言并标注，⛔ 不静默（见 question.Summary.Localized）。
package httplocale

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

type ctxKey int

const localeKey ctxKey = iota

// RequestLocale 取出协商好的语言标签（如 "ja"）。空串 = 没协商出来，按源语言处理。
func RequestLocale(ctx context.Context) string {
	v, _ := ctx.Value(localeKey).(string)
	return v
}

// WithLocale 把语言写进上下文。测试与非 HTTP 入口（将来的 job）用得上。
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeKey, locale)
}

// NegotiateLocale 按 supported 里【列出的顺序之外】、完全依客户端 q 值排序来挑语言。
//
// supported 是白名单：⛔ 不接受客户端给的任意标签，否则 locale 会直接进 SQL 参数
// （虽然是占位符不至于注入，但会让 i18n 表长出无穷多个垃圾 locale 的查询）。
func NegotiateLocale(supported ...string) func(http.Handler) http.Handler {
	allow := make(map[string]bool, len(supported))
	for _, s := range supported {
		allow[strings.ToLower(s)] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			locale := pickLocale(r.Header.Get("Accept-Language"), allow)
			// ⚠️ 无论协商结果如何都要发 Vary：否则缓存会把某一种语言的响应
			// 喂给所有人。空结果同样是「取决于这个头」。
			w.Header().Add("Vary", "Accept-Language")
			if locale != "" {
				w.Header().Set("Content-Language", locale)
				r = r.WithContext(WithLocale(r.Context(), locale))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// pickLocale 解析 Accept-Language 并返回白名单里 q 值最高的那个。
//
// 只做到【主语言子标签】这一级（zh-TW → zh）：题库译文按语言存，不按地区。
// 真要区分繁简时再扩展 —— 那时白名单里会同时有 "zh-hans" / "zh-hant"，
// 而下面的精确匹配已经能吃下它们。
func pickLocale(header string, allow map[string]bool) string {
	best, bestQ := "", 0.0
	for _, part := range strings.Split(header, ",") {
		tag, q := strings.TrimSpace(part), 1.0
		if i := strings.Index(tag, ";"); i >= 0 {
			spec := strings.TrimSpace(tag[i+1:])
			tag = strings.TrimSpace(tag[:i])
			if v, err := strconv.ParseFloat(strings.TrimPrefix(spec, "q="), 64); err == nil {
				q = v
			}
		}
		tag = strings.ToLower(tag)
		if tag == "" || q <= bestQ {
			continue
		}
		// 精确匹配优先（zh-hant），否则退到主子标签（zh-TW → zh）。
		if !allow[tag] {
			if i := strings.Index(tag, "-"); i > 0 && allow[tag[:i]] {
				tag = tag[:i]
			} else {
				continue
			}
		}
		best, bestQ = tag, q
	}
	return best
}
