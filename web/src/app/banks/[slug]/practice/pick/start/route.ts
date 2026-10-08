import { NextResponse } from "next/server";
import { oidc } from "@/lib/auth";
import { drillHref } from "@/lib/drillSpec";
import type { DrillContext } from "@/lib/claims";

/**
 * 4.3 表单 → 刷题 URL（P9 #12）。只做字段归一，⛔ 不查库。
 *   st  状态单选
 *   t   复选，可重复；一个分组的值是逗号串 ⇒ 全部拍平成并集
 *   r   范围："" 全部 / "s:<session>" 一套卷子 / "n:<from>-<to>" 一段题号
 */
const STATUSES = new Set(["all", "wrong", "unseen", "bookmarked", "contested"]);

export async function GET(req: Request, { params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const q = new URL(req.url).searchParams;
  const st = q.get("st") ?? "all";
  const tagIds = [...new Set(q.getAll("t").flatMap((v) => v.split(",")).map(Number).filter((n) => Number.isInteger(n) && n > 0))];
  const ctx: DrillContext = { mode: "pick", status: (STATUSES.has(st) ? st : "all") as DrillContext["status"] };
  if (tagIds.length) ctx.tagIds = tagIds;
  const r = q.get("r") ?? "";
  if (r.startsWith("s:")) ctx.session = r.slice(2);
  const m = /^n:(\d+)-(\d+)$/.exec(r);
  if (m) Object.assign(ctx, { session: "", noFrom: Number(m[1]), noTo: Number(m[2]) });
  // ⚠️ 基址取 oidc.origin，⛔ 不用 req.url —— standalone 模式下它是 0.0.0.0
  return NextResponse.redirect(new URL(drillHref(slug, ctx), oidc.origin), 303);
}
