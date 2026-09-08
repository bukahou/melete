import { NextResponse } from "next/server";
import { readAccessToken } from "@/lib/session";
import { ApiError, recordAttempt, type DrillContext } from "@/lib/api";

/**
 * 作答记录的 BFF 转发口。
 * 浏览器只带会话 cookie 打同源接口；这里校验会话存在后转发给内网 API，
 * 由 API 自己验签会话 JWT 并取出账号 —— web 层不参与身份声明。
 */
export async function POST(req: Request) {
  if (!(await readAccessToken())) {
    return NextResponse.json({ message: "未登录" }, { status: 401 });
  }
  const body = (await req.json()) as {
    questionId: number;
    chosen: string;
    rating: number;
    durationMs?: number;
    context?: DrillContext;
  };
  if (!body?.questionId || !body?.chosen || !body?.rating) {
    return NextResponse.json({ message: "参数不完整" }, { status: 400 });
  }
  // 不再传 accountId —— api 从转发的会话 JWT 里自己取，web 无从冒充他人
  try {
    const result = await recordAttempt(body);
    return NextResponse.json(result);
  } catch (e) {
    // 这是 JSON 路由，⛔ 不能像 Server Component 那样 redirect 去 renew ——
    // 把 401 原样交给浏览器，客户端刷新页面时 proxy 会续期。
    if (e instanceof ApiError && e.status === 401) {
      return NextResponse.json({ message: "会话已失效，请刷新页面" }, { status: 401 });
    }
    throw e;
  }
}
