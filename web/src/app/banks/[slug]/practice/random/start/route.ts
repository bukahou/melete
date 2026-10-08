import { randomInt } from "node:crypto";
import { NextResponse } from "next/server";
import { oidc } from "@/lib/auth";
import { drillHref } from "@/lib/drillSpec";

/**
 * 4.4 随机：生成一个新种子，重定向到刷题页（P9 #19）。
 *
 * ⭐ 种子在这里生成而不是写死在入口页的链接上：入口页会被缓存 / 被后退回来，
 *   同一个链接点两次就是同一批题 ——「再来一轮」必须真的换一批。
 * 种子进 URL：每题一次页面跳转，刷新 / 后退都不能换题。
 */
export async function GET(req: Request, { params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const n = Number(new URL(req.url).searchParams.get("n"));
  const count = Number.isInteger(n) && n >= 1 && n <= 200 ? n : 10;
  const seed = randomInt(0, 2 ** 31 - 1);
  return NextResponse.redirect(new URL(drillHref(slug, { mode: "random", seed, count }), oidc.origin), 303);
}
