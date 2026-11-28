// Package localize 按请求语言从多语言的名字里挑出显示用的那一个。
//
// 挑选规则只在这里有一份：客户端拿到的就是挑好的名字，⛔ 不在网页、iOS 各写一遍回退顺序。
package localize

import "sort"

// FallbackLocale 是请求的语言没有名字时优先回退的语言（考纲域等官方名字原文多为英文）。
const FallbackLocale = "en"

// PickName 从 {语言: 名字} 里挑显示名：请求的语言 → 英文 → 其余语言里语言代码最小的 → fallback。
//
// localized 表示挑到的是否为请求的语言：
//   - 请求没有指定语言（locale 为空）时为 true —— 无所谓回退
//   - names 为空时为 true —— 这类名字与语言无关（服务名、命令名），fallback 本身就是显示名
func PickName(names map[string]string, fallback, locale string) (name string, localized bool) {
	if locale != "" && names[locale] != "" {
		return names[locale], true
	}
	if len(names) == 0 {
		return fallback, true
	}
	if names[FallbackLocale] != "" {
		return names[FallbackLocale], locale == "" || locale == FallbackLocale
	}
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if names[k] != "" {
			return names[k], locale == ""
		}
	}
	return fallback, locale == ""
}
