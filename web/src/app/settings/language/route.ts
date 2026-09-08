import { NextResponse } from "next/server";
import { LOCALE_COOKIE, isLocale } from "@/i18n/locales";

/**
 * 保存语言偏好。
 *
 * 只写 cookie，⛔ 不落库 —— 账号上还没有 locale 字段。
 * ⚠️ 这意味着换设备要重选一次。这是【已知的、有意的】取舍：
 * 落库要一条迁移 + 一个 PATCH 端点 + 一处 mapper，而收益只是跨设备记住偏好。
 * 真要跨设备时再补，届时优先级是 账号设置 > cookie > Accept-Language。
 *
 * 一年有效期：语言偏好不像会话，⛔ 不该跟着会话一起过期
 * （那会导致「登出再登录就变回中文」这种莫名其妙的行为）。
 */
export async function POST(req: Request) {
  const body = (await req.json().catch(() => null)) as { locale?: string } | null;
  if (!isLocale(body?.locale)) {
    return NextResponse.json({ error: "unsupported locale" }, { status: 400 });
  }
  const res = NextResponse.json({ locale: body.locale });
  res.cookies.set(LOCALE_COOKIE, body.locale, {
    path: "/",
    maxAge: 60 * 60 * 24 * 365,
    sameSite: "lax",
    // ⛔ 不设 httpOnly：这不是凭据，且客户端切换器要读它做当前值高亮。
    // ⚠️ secure 跟随部署：本地 http 开发时设了会写不进去。
    secure: process.env.NODE_ENV === "production",
  });
  return res;
}
