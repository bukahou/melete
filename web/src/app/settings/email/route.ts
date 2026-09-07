import { NextResponse } from "next/server";
import { confirmEmailChange, sendEmailChangeCode } from "@/lib/api";
import { oidc } from "@/lib/auth";

/** 改邮箱两步：发码 → 验码写入。⛔ 发码不改库。 */
export async function POST(req: Request) {
  const form = await req.formData();
  const step = String(form.get("step") ?? "");

  const back = (n: string) => {
    const url = new URL("/settings", oidc.origin);
    url.searchParams.set("n", n);
    return NextResponse.redirect(url, 303);
  };

  if (step === "send") {
    const email = String(form.get("email") ?? "");
    if (!email) return back("fail");
    const status = await sendEmailChangeCode(email);
    if (status === 429) return back("rate");
    // ⚠️ 新地址已被占用时后端【也返回 204】（防枚举），改由那个地址的主人收到通知。
    // ⇒ 这里看到 204 只代表「已受理」，⛔ 不代表「那个地址可用」。
    if (status !== 204) return back("fail");
    return back("mail-sent");
  }

  if (step === "confirm") {
    const code = String(form.get("code") ?? "");
    if (!code) return back("mail-bad");
    const status = await confirmEmailChange(code, req.headers.get("user-agent")?.slice(0, 200) ?? "web");
    if (status === 400) return back("mail-bad");
    if (status !== 200) return back("fail");
    // ⚠️ 改邮箱同样吊销全部会话 —— 与改密同一处置：让用户重新登录，
    // ⛔ 不留一个已失效的 cookie。
    const url = new URL("/auth/login", oidc.origin);
    url.searchParams.set("return", "/settings");
    const out = NextResponse.redirect(url, 303);
    out.cookies.delete("melete_at");
    out.cookies.delete("melete_rt");
    return out;
  }
  return back("fail");
}
