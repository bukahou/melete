/**
 * 应用支持的语言 —— **界面文案**的清单。
 *
 * ⚠️ 与后端 httplocale 的白名单是【两件事】，⛔ 别把它们混成一个：
 * 这里说的是「界面有没有翻译」，那边说的是「题库正文有没有译文」。
 * 一道题可以在界面已是日文的情况下【没有】日文正文 —— 那时正文回退源语言
 * 并显式标注（QuestionSummary.localized / QuestionDetail.sourceLocale）。
 */
export const LOCALES = ["zh", "ja"] as const;

export type Locale = (typeof LOCALES)[number];

/** 默认语言。⚠️ 也是 messages 的兜底来源：缺 key 时回落到它，⛔ 不显示 key 本身。 */
export const DEFAULT_LOCALE: Locale = "zh";

/** 语言选择器里的自称（endonym）—— ⛔ 不用「日文 / 中文」这种他称：
 *  正在找母语的人认得的是自己语言里的写法。 */
export const LOCALE_LABEL: Record<Locale, string> = {
  zh: "简体中文",
  ja: "日本語",
};

/** 存放显式语言选择的 cookie。⭐ 不是 httpOnly —— 客户端切换器要能读它做高亮。 */
export const LOCALE_COOKIE = "melete_locale";

export function isLocale(v: unknown): v is Locale {
  return typeof v === "string" && (LOCALES as readonly string[]).includes(v);
}

/**
 * 从 Accept-Language 挑一个支持的语言；挑不出返回 null。
 *
 * ⚠️ 与后端 httplocale.pickLocale 是【同一套规则的第二份实现】。
 * 会不会漂移？会 —— 但两边的职责不同（这边选界面，那边选正文），
 * 强行共用反而要跨语言共享一份配置。⭐ 代价可控的前提是：
 * 两边都只做到主子标签这一级，且都用 q 值排序。改一边时记得看另一边。
 */
export function pickLocale(header: string | null): Locale | null {
  let best: Locale | null = null;
  let bestQ = 0;
  for (const part of (header ?? "").split(",")) {
    const [rawTag, ...params] = part.split(";");
    let tag = rawTag.trim().toLowerCase();
    // ⚠️ q 解析失败按 1.0，⛔ 不按 0 —— 脏参数不该让整个语言消失（后端同样处理）
    const parsed = Number(params.join(";").trim().replace(/^q=/, ""));
    const q = Number.isFinite(parsed) && params.length ? parsed : 1;
    if (!tag || q <= bestQ) continue;
    if (!isLocale(tag)) {
      const base = tag.split("-")[0];
      if (!isLocale(base)) continue;
      tag = base;
    }
    best = tag as Locale;
    bestQ = q;
  }
  return best;
}

/**
 * needsSourceNotice 判断「要不要告诉用户这段正文不是译文」。
 *
 * ⚠️ 只看 localized 是不够的：请求源语言本身时它也是 false，
 * 那时提示「本题暂无该语言版本」是【错的】—— 用户看的就是原文，没有缺任何东西。
 * 所以必须同时比对题库源语言。
 *
 * sourceLocale 可能缺（旧版本 API / 未来的其它内容类型）—— 缺就不提示：
 * ⭐ 宁可漏一次提示，也不要对着正确的原文喊「没有译文」。
 */
export function needsSourceNotice(
  sourceLocale: string | undefined | null,
  requested: string,
  localized: boolean | undefined,
): boolean {
  if (!sourceLocale || sourceLocale === requested) return false;
  return !localized;
}
