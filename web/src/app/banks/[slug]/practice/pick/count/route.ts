import { NextResponse } from "next/server";
import { unstable_rethrow } from "next/navigation";
import { summarizeQuestions } from "@/lib/api";
import { listParams } from "@/lib/drillSpec";
import { pickContextFromForm } from "@/lib/pickForm";

/**
 * 4.3 实时题数（P9 #12，2026-10-08 用户裁定方案 b）：
 * 每点一个条件，表单就问一次「这样组合有几题」—— 选中马上有反馈，0 题当场可见。
 *
 * 字段与 start 路由同形（pickForm），集合与刷题页同表（drillSpec.listParams）——
 * ⛔ 三处任何一处各算各的，按钮上的数字就会与点进去的题对不上。
 */
export async function GET(req: Request, { params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const ctx = pickContextFromForm(new URL(req.url).searchParams);
  try {
    const sum = await summarizeQuestions(slug, listParams(ctx));
    return NextResponse.json({ total: sum.total }, { headers: { "cache-control": "no-store" } });
  } catch (e) {
    // ⚠️ 会话过期时 api.ts 会发起「去续期」的重定向 —— 放行它；客户端拿到非 JSON 就当「算不出来」
    unstable_rethrow(e);
    return NextResponse.json({ error: "count failed" }, { status: 502 });
  }
}
