import { NextResponse } from "next/server";
import { oidc, passwordLogin } from "@/lib/auth";
import { commitSession, logAuth, safeReturnPath, type TokenPair } from "@/lib/session";

const API = process.env.MELETE_API_BASE ?? "http://localhost:8899/api/v1";

/**
 * 调试账号的密码登录（P9 #35–#38）：转发给 API 换 token，再把 token 落进 httpOnly cookie。
 * 表单在 /auth/login/password —— 一个不在任何地方链接的页面；开关关着时本路由与那一页都是 404。
 *
 * 2026-10-21 从账号收敛（#25）撤下前的版本恢复，改了一处：
 *   登录后回到哪（`return`）统一过 safeReturnPath —— 旧版直接 `new URL(return, origin)`，
 *   传进一个完整的外站地址就会把人带出站。
 */
export async function POST(req: Request) {
  if (!passwordLogin.enabled) return new NextResponse(null, { status: 404 });

  const form = await req.formData();
  const username = String(form.get("username") ?? "");
  const password = String(form.get("password") ?? "");
  const returnTo = safeReturnPath(String(form.get("return") ?? ""));

  // 错误只回一个代码，文案由页面按白名单查表 —— ⛔ 不把任何来自 API 的文字带进 URL
  const back = (error: "credentials" | "rate") => {
    const url = new URL("/auth/login/password", oidc.origin);
    url.searchParams.set("return", returnTo);
    url.searchParams.set("error", error);
    // 303：把 POST 变 GET，避免刷新时重复提交
    return NextResponse.redirect(url, 303);
  };

  if (!username || !password) return back("credentials");

  const res = await fetch(`${API}/auth/password`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      username,
      password,
      deviceInfo: req.headers.get("user-agent")?.slice(0, 200) ?? "web",
    }),
  });
  // ⚠️ API 把「退避中」与「密码错误」刻意做成同一个 401（不给预言机），这里也 ⛔ 不细分
  if (res.status === 401) return back("credentials");
  // 429 是另一层的请求限流，与密码对错无关 —— 分开说，否则会误导人反复重试
  if (res.status === 429) return back("rate");
  if (!res.ok) {
    console.error("[auth] API 密码登录失败", res.status, await res.text());
    return new NextResponse("认证服务不可用", { status: 502 });
  }

  const pair = (await res.json()) as TokenPair;
  const out = NextResponse.redirect(new URL(returnTo, oidc.origin), 303);
  commitSession(out, pair);
  logAuth("login.password", {});
  return out;
}
