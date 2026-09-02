import { NextResponse, type NextRequest } from "next/server";
import { ACCESS_COOKIE, REFRESH_COOKIE, setAuthCookies, type TokenPair } from "@/lib/auth";

const API = process.env.MELETE_API_BASE ?? "http://localhost:8899/api/v1";

/**
 * 全站登录墙 + **access token 自动续期**。
 *
 * 登录墙的理由是版权边界（用户拍板，见 learning-flows.md §3）：
 * 题库内容公网可访问 = 分发考试转储，与仓库必须永久 private 是同一个问题。
 *
 * 自动续期是双 token 架构在 web 端的落点，与 iOS 的 interceptor 同构
 * （见 geass-mobile/CLAUDE.md：401 → 自动 refresh → 重试）：
 * access 只有 1 小时，若过期就用 refresh 换新的，用户无感 ——
 * 而不是让人每小时被踢回登录页。
 */
export async function proxy(req: NextRequest) {
  if (req.cookies.get(ACCESS_COOKIE)?.value) {
    return NextResponse.next();
  }

  // access 没了但 refresh 还在 → 静默续期后放行本次请求
  const rt = req.cookies.get(REFRESH_COOKIE)?.value;
  if (rt) {
    const res = await fetch(`${API}/auth/refresh`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ refreshToken: rt }),
    }).catch(() => null);

    if (res?.ok) {
      const pair = (await res.json()) as TokenPair;
      // 重定向回同一地址：让下游的服务端组件能读到新 cookie。
      // 直接 next() 的话新 cookie 只在响应头上，本次渲染仍拿不到 token。
      const retry = NextResponse.redirect(req.url);
      setAuthCookies(retry, pair);
      return retry;
    }
    // refresh 也失效（过期 / 被吊销 / 已轮换）→ 清掉残留 cookie，走登录
    const login = toLogin(req);
    login.cookies.delete(REFRESH_COOKIE);
    return login;
  }

  return toLogin(req);
}

function toLogin(req: NextRequest) {
  const login = new URL("/auth/login", req.url);
  login.searchParams.set("return", req.nextUrl.pathname + req.nextUrl.search);
  return NextResponse.redirect(login);
}

export const config = {
  // 放行：认证路由 / Next 静态资源 / favicon（页面与数据请求全部设墙）
  matcher: ["/((?!auth/|_next/|favicon.ico).*)"],
};
