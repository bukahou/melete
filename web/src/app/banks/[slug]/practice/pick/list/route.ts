import { NextResponse } from "next/server";
import { unstable_rethrow } from "next/navigation";
import { listQuestions } from "@/lib/api";
import { listParams } from "@/lib/drillSpec";
import { pickContextFromForm } from "@/lib/pickForm";

/**
 * 4.3 题目列表（P9 #12，2026-10-08 用户裁定照 it-pass「絞り込んで出題」：标签栏 + 题目列表）。
 *
 * 每点一个筛选标签，页面就来这里取一次 —— 结果直接是题目，「生效了没有」一眼可见。
 * 字段解析共用 pickForm，集合与刷题页同表（drillSpec.listParams）：列表第 i 行 = 刷题页 offset i。
 */
const PAGE = 50;

export async function GET(req: Request, { params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const q = new URL(req.url).searchParams;
  const ctx = pickContextFromForm(q);
  const offset = Math.max(0, Number(q.get("offset")) || 0);
  try {
    const page = await listQuestions(slug, { ...listParams(ctx), limit: PAGE, offset });
    return NextResponse.json(
      {
        total: page.total,
        offset,
        items: page.items.map((it) => ({
          id: it.id,
          no: it.externalNo,
          // 只给一行摘要：列表是用来「认出这道题」的，⛔ 不是用来读题的
          stem: it.stem.replace(/\s+/g, " ").slice(0, 90),
          last: it.lastCorrect ?? null,
          bookmarked: it.bookmarked ?? false,
        })),
      },
      { headers: { "cache-control": "no-store" } },
    );
  } catch (e) {
    // ⚠️ 会话过期时 api.ts 会发起「去续期」的重定向 —— 放行它
    unstable_rethrow(e);
    return NextResponse.json({ error: "list failed" }, { status: 502 });
  }
}
