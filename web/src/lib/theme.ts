/**
 * 外观（深浅色）—— 2026-10-08 改版：默认浅色，设置里可选。
 *
 * ⚠️ 旧版只跟随系统、没有开关：系统是深色的用户永远只能看到深色版。
 * 存 cookie（与语言同一个机制），服务端渲染时直接写进 <html data-theme>，
 * ⛔ 不在客户端读 localStorage —— 那样首屏会先闪一下浅色再变深。
 */
export const THEME_COOKIE = "melete_theme";
export const THEMES = ["light", "dark", "system"] as const;
export type Theme = (typeof THEMES)[number];
export const DEFAULT_THEME: Theme = "light";

export function isTheme(v: unknown): v is Theme {
  return typeof v === "string" && (THEMES as readonly string[]).includes(v);
}
