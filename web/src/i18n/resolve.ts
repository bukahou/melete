import "server-only";
import { cookies, headers } from "next/headers";
import { DEFAULT_LOCALE, LOCALE_COOKIE, isLocale, pickLocale, type Locale } from "./locales";

/**
 * 决定这次渲染用哪种语言。优先级：
 *
 *   ① melete_locale cookie —— 用户【显式】选过。⭐ 它压过浏览器偏好：
 *      一个日语系统的用户点了「简体中文」，就是要中文，⛔ 不该被自动纠回去。
 *   ② Accept-Language —— 没选过时的合理猜测。
 *   ③ DEFAULT_LOCALE。
 *
 * ⚠️ 读 cookie 会让页面变成动态渲染。本应用的页面本来就因为读 access token
 * 而是动态的，所以这里不新增代价；将来若有静态页面要国际化，得另想办法。
 */
export async function resolveLocale(): Promise<Locale> {
  const chosen = (await cookies()).get(LOCALE_COOKIE)?.value;
  if (isLocale(chosen)) return chosen;
  return pickLocale((await headers()).get("accept-language")) ?? DEFAULT_LOCALE;
}
