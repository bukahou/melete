import { DRILL_MODES } from "@/lib/claims";

/** attempt.context.mode 的全部取值 —— DRILL_MODES 之外还有三个只出现在出处里的。 */
const CONTEXT_MODES = [...DRILL_MODES, "contested", "all", "tag"] as const;
type ContextMode = (typeof CONTEXT_MODES)[number];

/**
 * 把后端给的 mode 字符串译成文案。
 *
 * ⚠️ mode 来自【数据】（attempt.context.mode），⛔ 不保证 messages 里有对应的键。
 * 后端将来加一种模式而前端没跟上时，直接 t(mode) 会抛 missing message 把整页打红。
 * 所以先查白名单，命中不了就用后端给的 label —— 少一句译文远好过整页崩。
 */
export function modeLabel(
  t: (key: ContextMode) => string,
  mode: string,
  fallback: string,
): string {
  return (CONTEXT_MODES as readonly string[]).includes(mode) ? t(mode as ContextMode) : fallback;
}
