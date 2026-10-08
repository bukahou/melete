import { NextResponse } from "next/server";
import { oidc } from "@/lib/auth";
import { THEME_COOKIE, isTheme } from "@/lib/theme";

/** 设置页的外观表单：写 cookie → 303 回设置页（形态同 language-form）。 */
export async function POST(req: Request) {
  const form = await req.formData();
  const theme = String(form.get("theme") ?? "");
  // ⚠️ 基址取 oidc.origin，⛔ 不用 req.url —— standalone 模式下它是 0.0.0.0
  const back = new URL("/settings", oidc.origin);
  back.searchParams.set("n", isTheme(theme) ? "theme-ok" : "fail");
  const res = NextResponse.redirect(back, 303);
  if (isTheme(theme)) {
    res.cookies.set(THEME_COOKIE, theme, {
      path: "/",
      maxAge: 60 * 60 * 24 * 365,
      sameSite: "lax",
      secure: oidc.origin.startsWith("https"),
    });
  }
  return res;
}
