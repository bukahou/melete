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

  // 🔴🔴 未解决的严重问题：预取放大导致的全设备强制登出
  //
  // ## 机制（三段都已实测证实）
  //
  //  1. gokit/localauth 的 RotateReplayed 处置是【吊销该用户全部会话】，
  //     且刻意没有开关（代价不对称：误伤=重登一次；漏放=攻击者偷到的
  //     refresh 链完好续到 TTL 结束）。已端到端实测：重放旧 refresh 后，
  //     连刚换出来的新 refresh 也随之失效。
  //  2. Next 默认对视口内 <Link> 预取；本站布局 3 个 + 刷题页 3 个 + 卡片 4 个。
  //  3. 本 proxy 的 matcher 覆盖 RSC 请求。
  //
  //  ⇒ access cookie 过期后的第一次渲染，约 10 个预取【并发】拿同一个
  //    refresh 来换 ⇒ 1 个成功、其余判为 Replayed ⇒ 用户在所有设备上被登出。
  //  ⚠️ 实测：10 个并发预取 → 9 条 replay_detected，会话被吊销。
  //
  // ## ⛔ 「预取请求跳过刷新」这个修法【不可实现】—— 2026-09-07 实测
  //
  // proxy 收到的请求头只有：accept / host / user-agent / x-forwarded-*。
  // `Next-Router-Prefetch`、`RSC`、`Next-Router-State-Tree` 全部在 proxy
  // 看到请求【之前】就被剥掉了；`?_rsc=` 查询参数同样不在 nextUrl 里。
  // ⇒ 这一层【结构上无法区分】预取与真实导航。
  //
  // ⚠️ 我曾按「读 next-router-prefetch 头」写过一版并当作已修复 ——
  // 它编译进去了、看起来合理、而且【完全没有生效】。
  // ⭐ 是 work 钉的那条门槛（「预取不得触发 Replayed」）把它抓出来的：
  // 若只做代码审查，这个修法会一路过到生产。
  //
  // ## 处置：等用户裁决（已按背景/影响/推荐呈报）
  //
  // 候选：给所有 <Link> 加 prefetch={false}（确定有效，牺牲导航速度）
  //      / 刷新移到客户端 single-flight（geass-v3 的形状，work 已验证它免疫）
  //      / 请 gokit 给轮换加一个极短的宽限窗口（⛔ 动的是三家共享的安全语义）
  // ⛔ 在裁决之前【不假装已修】—— 留着这段注释，让下一个读到的人
  //    知道这里有一个已知的、可复现的、会导致全设备登出的问题。

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
