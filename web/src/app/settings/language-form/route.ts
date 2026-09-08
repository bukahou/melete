import { NextResponse } from "next/server";
import { oidc } from "@/lib/auth";
import { LOCALE_COOKIE, isLocale } from "@/i18n/locales";

/**
 * 设置页的语言表单（`<form method="POST">`）入口。
 *
 * ⚠️ 与 /settings/language 是【两个】端点，不是重复：
 *   · /settings/language      —— JSON，给顶栏切换器（fetch + router.refresh）
 *   · /settings/language-form —— 表单，303 回设置页
 * 设置页刻意不引入客户端状态（见 page.tsx 头注），所以它需要一个会 303 的入口；
 * 而顶栏的切换器不能 303（那会把用户从当前页扔到设置页）。
 * ⭐ 两者写的是【同一个 cookie】，⛔ 不是两套状态。
 */
export async function POST(req: Request) {
  const form = await req.formData();
  const locale = String(form.get("locale") ?? "");
  // ⚠️ 基址取 oidc.origin，⛔ 不用 req.url —— standalone 模式下它是 0.0.0.0，
  // 重定向会把用户送到一个不存在的地址（2026-09-08 已在 /auth/renew 踩过一次）。
  const back = new URL("/settings", oidc.origin);
  back.searchParams.set("n", isLocale(locale) ? "lang-ok" : "fail");
  const res = NextResponse.redirect(back, 303); // 303：POST → GET，刷新不重复提交
  if (isLocale(locale)) {
    res.cookies.set(LOCALE_COOKIE, locale, {
      path: "/",
      maxAge: 60 * 60 * 24 * 365,
      sameSite: "lax",
      secure: process.env.NODE_ENV === "production",
    });
  }
  return res;
}
