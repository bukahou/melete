import { NextResponse } from "next/server";
import { changePassword } from "@/lib/api";
import { oidc } from "@/lib/auth";

/**
 * 改密 / 首次设密。
 *
 * ⚠️ 401（当前密码不对）与 400（新密码不合规）必须分开回给用户 ——
 * 他已经登录了，混成一个只会让他不知道该改什么。
 * ⛔ 这与登录路径「所有失败不可区分」不冲突：那里的调用方是【未认证】的。
 */
export async function POST(req: Request) {
  const form = await req.formData();
  const oldPassword = String(form.get("old") ?? "");
  const newPassword = String(form.get("new") ?? "");

  const back = (n: string, c?: number) => {
    const url = new URL("/settings", oidc.origin);
    url.searchParams.set("n", n);
    if (c != null) url.searchParams.set("c", String(c));
    return NextResponse.redirect(url, 303); // 303：POST → GET，刷新不重复提交
  };

  if (!newPassword) return back("pw-weak");

  const res = await changePassword({
    // ⭐ 空串表示「首次设密」。⛔ 真正判定「要不要验旧密码」的是后端
    // （看账号有没有密码），⛔ 不是这里传不传。
    oldPassword: oldPassword || undefined,
    newPassword,
    deviceInfo: req.headers.get("user-agent")?.slice(0, 200) ?? "web",
  });

  if (!res.ok) {
    if (res.status === 401) return back("pw-old");
    if (res.status === 400) return back("pw-weak");
    console.error("[settings] 改密失败", res.status, res.message);
    return back("fail");
  }
  // ⚠️ 改密成功后【全部会话被吊销】，包括当前这条 —— 后端会为当前设备重签，
  // 但那对新 token 在这次响应里拿不到（BFF 转发的是 JSON，不是 cookie）。
  // ⇒ 让用户重新登录，⛔ 比留一个已失效的 cookie 让他之后莫名 401 好。
  const url = new URL("/auth/login", oidc.origin);
  url.searchParams.set("return", "/settings");
  const out = NextResponse.redirect(url, 303);
  out.cookies.delete("melete_at");
  out.cookies.delete("melete_rt");
  return out;
}
