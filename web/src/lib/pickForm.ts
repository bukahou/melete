import type { DrillContext } from "./claims";

/**
 * 4.3 自选条件的表单字段 → 出处（P9 #12）。
 *   st  状态单选
 *   t   复选，可重复；一个分组的值是逗号串 ⇒ 全部拍平成并集
 *   r   范围："" 全部 / "s:<session>" 一套卷子 / "n:<from>-<to>" 一段题号
 *
 * ⭐ 「开始」跳转（start 路由）与「实时题数」（count 路由）共用这一处 ——
 *   两边各解析一份，按钮上写着 37 题、点进去却是另一个集合，而且不报错。
 */
const STATUSES = new Set(["all", "wrong", "unseen", "bookmarked", "contested"]);

export function pickContextFromForm(q: URLSearchParams): DrillContext {
  const st = q.get("st") ?? "all";
  const tagIds = [...new Set(q.getAll("t").flatMap((v) => v.split(",")).map(Number).filter((n) => Number.isInteger(n) && n > 0))];
  const ctx: DrillContext = { mode: "pick", status: (STATUSES.has(st) ? st : "all") as DrillContext["status"] };
  if (tagIds.length) ctx.tagIds = tagIds;
  const r = q.get("r") ?? "";
  if (r.startsWith("s:")) ctx.session = r.slice(2);
  const m = /^n:(\d+)-(\d+)$/.exec(r);
  if (m) Object.assign(ctx, { session: "", noFrom: Number(m[1]), noTo: Number(m[2]) });
  return ctx;
}
