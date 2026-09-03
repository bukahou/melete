import { NextResponse } from "next/server";
import { oidc } from "@/lib/auth";

/**
 * 发起 Akasha 登录：直接把浏览器送去 api 的 /auth/oidc/start。
 * state / nonce / PKCE 全在 api 侧 —— web 这里没有任何事务状态。
 * next 只传本站路径；api 侧的白名单会再校一次。
 */
export async function GET(req: Request) {
  const returnTo = new URL(req.url).searchParams.get("return") ?? "/";
  const next = returnTo.startsWith("/") && !returnTo.startsWith("//") ? returnTo : "/";
  const start = new URL(`${oidc.apiPublicBase}/auth/oidc/start`);
  start.searchParams.set("next", next);
  return NextResponse.redirect(start);
}
