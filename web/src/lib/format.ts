/**
 * 与语言相关的格式化。
 *
 * ⚠️ 这里【不再】导出 MODE_LABEL —— 学习模式的显示名已进 messages（`mode.*`），
 * 由 useTranslations/getTranslations 取。⛔ 别再在代码里放中文常量表：
 * 那正是「加一种语言就要满仓库找字符串」的来源。
 */

/**
 * 相对时间。用 Intl.RelativeTimeFormat，⛔ 不自己拼「分钟前」——
 * 日语的「3分前」没有空格、复数规则也与中文不同，手拼必然出错。
 */
export function timeAgo(iso: string | undefined, locale: string): string {
  if (!iso) return "";
  const sec = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  if (sec < 3600) return rtf.format(-Math.max(1, Math.round(sec / 60)), "minute");
  if (sec < 86400) return rtf.format(-Math.round(sec / 3600), "hour");
  if (sec < 86400 * 7) return rtf.format(-Math.round(sec / 86400), "day");
  return new Date(iso).toLocaleDateString(locale, { month: "numeric", day: "numeric" });
}

/** 绝对日期时间，跟随界面语言。 */
export function dateTime(iso: string | undefined, locale: string): string {
  if (!iso) return "";
  return new Date(iso).toLocaleString(locale, { dateStyle: "medium", timeStyle: "short" });
}
