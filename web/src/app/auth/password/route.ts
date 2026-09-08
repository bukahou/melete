import { NextResponse } from "next/server";
import { oidc } from "@/lib/auth";
import { commitSession, logAuth, type TokenPair } from "@/lib/session";

const API = process.env.MELETE_API_BASE ?? "http://localhost:8899/api/v1";

/** 密码登录：转发给 API 换 token，再把 token 落进 httpOnly cookie。 */
export async function POST(req: Request) {
  const form = await req.formData();
  const username = String(form.get("username") ?? "");
  const password = String(form.get("password") ?? "");
  const returnTo = String(form.get("return") ?? "/");

  const back = (error?: string) => {
    const url = new URL("/auth/login", oidc.origin);
    url.searchParams.set("return", returnTo);
    if (error) url.searchParams.set("error", error);
    // 303：把 POST 变 GET，避免刷新时重复提交
    return NextResponse.redirect(url, 303);
  };

  if (!username || !password) return back("1");

  const res = await fetch(`${API}/auth/password`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      username,
      password,
      deviceInfo: req.headers.get("user-agent")?.slice(0, 200) ?? "web",
    }),
  });
  if (res.status === 401) return back("1");
  // 限流是用户可理解的状态，与「密码错误」区分开 —— 否则会误导用户反复重试
  if (res.status === 429) return back("rate");
  if (!res.ok) {
    console.error("[auth] API 登录失败", res.status, await res.text());
    return new NextResponse("认证服务不可用", { status: 502 });
  }

  const pair = (await res.json()) as TokenPair;
  const out = NextResponse.redirect(new URL(returnTo, oidc.origin), 303);
  commitSession(out, pair);
  logAuth("login.password", {});
  return out;
}
