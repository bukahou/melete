import { NextResponse } from "next/server";
import { oidc } from "@/lib/auth";
import { commitSession, logAuth, safeReturnPath, type TokenPair } from "@/lib/session";

const API = process.env.MELETE_API_BASE ?? "http://localhost:8899/api/v1";

/**
 * api 完成 Akasha 往返后把浏览器送回这里，带一张一次性票据（refresh token）。
 *
 * 为什么不直接把 token 对写进 URL：web 的 token 存 httpOnly cookie，浏览器脚本
 * 读不到 fragment；而 query 会进日志。所以只走一个 refresh token，并**立刻拿它去
 * /auth/refresh 换新的一对** —— refresh 轮换让 URL 里那个用一次即废，
 * 日志里留下的是张作废的票。整个过程在服务端 route handler 里完成。
 */
export async function GET(req: Request) {
  const url = new URL(req.url);
  const ticket = url.searchParams.get("ticket");
  // 同源路径校验统一走 session-core（这里此前是手写的一版，少了 "/\" 和控制字符两条）
  const safeNext = safeReturnPath(url.searchParams.get("next"));

  if (!ticket) {
    return NextResponse.redirect(new URL("/auth/login?oidc_error=state", oidc.origin));
  }

  const res = await fetch(`${API}/auth/refresh`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ refreshToken: ticket }),
  }).catch(() => null);

  if (!res?.ok) {
    console.error("[auth] 票据兑换失败", res?.status);
    return NextResponse.redirect(new URL("/auth/login?oidc_error=upstream", oidc.origin));
  }

  const pair = (await res.json()) as TokenPair;
  const out = NextResponse.redirect(new URL(safeNext, oidc.origin), 303);
  commitSession(out, pair);
  logAuth("login.oidc", {});
  return out;
}
