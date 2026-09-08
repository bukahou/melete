import { NextResponse } from "next/server";
import { readAccessToken } from "@/lib/session";
import { ApiError, rateAttempt } from "@/lib/api";

/**
 * 自评的 BFF 转发口 —— 给一条已记录的作答补上 rating。
 *
 * ⭐ 与 POST /api/attempts 分开是因为它们是两件事：
 *   POST  = 记录这次作答（揭晓即发生，⛔ 不依赖自评）
 *   PATCH = 补上自评，据此排 FSRS 卡片
 * 合并成一个端点就要靠「body 里有没有 attemptId」猜意图，⛔ 日后看代码全靠猜。
 *
 * 归属校验在 API 侧（按会话账号 WHERE user_id）—— web 层不参与身份声明。
 */
export async function PATCH(req: Request, { params }: { params: Promise<{ id: string }> }) {
  if (!(await readAccessToken())) {
    return NextResponse.json({ message: "未登录" }, { status: 401 });
  }
  const { id } = await params;
  const attemptId = Number(id);
  if (!Number.isInteger(attemptId) || attemptId <= 0) {
    return NextResponse.json({ message: "作答 id 无效" }, { status: 400 });
  }
  const body = (await req.json().catch(() => null)) as { rating?: number } | null;
  const rating = body?.rating;
  if (!Number.isInteger(rating) || rating! < 1 || rating! > 4) {
    return NextResponse.json({ message: "自评必须是 1-4" }, { status: 400 });
  }

  try {
    return NextResponse.json(await rateAttempt(attemptId, rating!));
  } catch (e) {
    // JSON 路由，⛔ 不能像 Server Component 那样 redirect 去 renew
    if (e instanceof ApiError && e.status === 401) {
      return NextResponse.json({ message: "会话已失效，请刷新页面" }, { status: 401 });
    }
    // 404 = 这条作答不存在或不是你的（API 侧刻意不区分）
    if (e instanceof ApiError && e.status === 404) {
      return NextResponse.json({ message: "作答记录不存在" }, { status: 404 });
    }
    throw e;
  }
}
