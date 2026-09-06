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

  // ⛔⛔ 预取请求【一律不走刷新路径】。
  //
  // ⚠️ 这不是优化，是一个会造成全设备强制登出的真实缺陷的堵法：
  //
  //   1. 模块（gokit/localauth）的 RotateReplayed 处置是【吊销该用户全部会话】，
  //      且刻意没有开关 —— 理由是代价不对称（误伤=重登一次；漏放=攻击者
  //      偷到的 refresh 链完好无损续到 TTL 结束）。
  //   2. Next 默认对视口内 <Link> 预取，本站布局 3 个 + 刷题页 3 个 + 卡片 4 个。
  //   3. 本 proxy 的 matcher 覆盖 RSC 数据请求（预取不走 /_next/）。
  //
  //   ⇒ access cookie 过期后的第一次渲染，约 10 个预取【并发】拿同一个
  //     refresh 去换 ⇒ 1 个 Rotated、其余命中 prev ⇒ 判为 Replayed
  //     ⇒ 全部会话被吊销 ⇒ 用户在所有设备上被登出，且不知道为什么。
  //
  // ⭐ 问题不在模块 —— 是本站制造出了「同一个 refresh 被并发使用」这个
  // 在模块看来【与失窃无法区分】的形态。
  //
  // ⚠️ 进程内 single-flight 去重不够：melete 是 2 副本，跨副本挡不住。
  // ⭐ 正解是这一层：**预取本来就不该有副作用**，让它触发 token 轮换
  // 等于把一个只读操作变成了写操作。真实导航会自己走刷新，用户无感。
  if (req.headers.get("next-router-prefetch") === "1") {
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
  // 放行：认证路由 / Next 静态资源 / 站点图标（页面与数据请求全部设墙）。
  //
  // 图标必须放行，且不能只写 favicon.ico —— 图标走的是 App Router 的
  // 文件约定（app/icon.png → /icon.png、apple-icon.png → /apple-icon.png），
  // 根本不存在 favicon.ico。曾经漏掉这条，结果登录页自己的标签页图标
  // 被 307 到登录页，永远加载不出来。
  matcher: ["/((?!auth/|_next/|favicon\\.ico|icon\\.|apple-icon\\.|manifest\\.).*)"],
};
