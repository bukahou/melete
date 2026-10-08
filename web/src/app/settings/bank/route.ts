import { NextResponse } from "next/server";
import { chooseMyBank } from "@/lib/api";
import { oidc } from "@/lib/auth";

/**
 * 切换当前题库（P9 #1 #2）—— 设置页的表单入口。
 *
 * ⭐ 成功后回【首页】而不是设置页：用户说的是「如同更换账号一样」——
 * 换完账号你看到的是那个账号的首页，⛔ 不是停在「切换成功」的提示上。
 * 失败才回设置页带提示。
 */
export async function POST(req: Request) {
  const form = await req.formData();
  const slug = String(form.get("bank") ?? "");
  // ⚠️ 基址取 oidc.origin，⛔ 不用 req.url —— standalone 模式下它是 0.0.0.0。
  const fail = () => {
    const url = new URL("/settings", oidc.origin);
    url.searchParams.set("n", "fail");
    return NextResponse.redirect(url, 303);
  };
  if (!slug) return fail();

  const res = await chooseMyBank(slug);
  if (!res.ok) {
    console.error("[settings] 切换题库失败", res.status);
    return fail();
  }
  return NextResponse.redirect(new URL("/", oidc.origin), 303); // 303：POST → GET
}
