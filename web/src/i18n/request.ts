import { getRequestConfig } from "next-intl/server";
import { DEFAULT_LOCALE } from "./locales";
import { resolveLocale } from "./resolve";

/**
 * next-intl 的服务端入口。
 *
 * ⭐ 刻意【不用】next-intl 的路由方案（/zh/... /ja/...）：
 * 语言是**用户偏好**，不是资源标识 —— 同一道题在两种语言下是同一道题，
 * 不该有两个 URL。带前缀的路由会让分享出去的链接把分享者的语言强加给接收者，
 * 也会让 return-to 这类回跳路径凭空多一个必须维护的维度。
 *
 * 代价：没有语言前缀 ⇒ 页面必须动态渲染。本应用的页面本就因读 cookie 而动态。
 *
 * ⚠️ messages 缺 key 时回落到默认语言的文案，⛔ 不显示 key 本身 ——
 * 半成品翻译对用户是「少了一句中文」，不是「界面裂开」。
 */
export default getRequestConfig(async () => {
  const locale = await resolveLocale();
  const fallback = (await import(`../messages/${DEFAULT_LOCALE}.json`)).default;
  const messages =
    locale === DEFAULT_LOCALE ? fallback : (await import(`../messages/${locale}.json`)).default;
  return {
    locale,
    messages: locale === DEFAULT_LOCALE ? fallback : deepMerge(fallback, messages),
    // 时间一律按用户所在时区呈现由客户端决定，这里只固定格式的语言
    now: new Date(),
  };
});

/** 浅层递归合并：译文覆盖兜底，缺的沿用兜底。 */
function deepMerge(base: Record<string, unknown>, over: Record<string, unknown>) {
  const out: Record<string, unknown> = { ...base };
  for (const [k, v] of Object.entries(over)) {
    const b = out[k];
    out[k] =
      b && v && typeof b === "object" && typeof v === "object" && !Array.isArray(b)
        ? deepMerge(b as Record<string, unknown>, v as Record<string, unknown>)
        : v;
  }
  return out;
}
