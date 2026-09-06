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

  // ⭐⭐ 刷新【只发生在文档导航上】—— 这是「预取放大导致全设备登出」的修法。
  //
  // ## 要挡的是什么
  //
  // 模块（gokit/localauth）检测到 refresh 重放时【吊销该用户全部会话】，
  // 且刻意没有开关（代价不对称：误伤=重登一次；漏放=攻击者偷到的 refresh
  // 链完好续到 TTL 结束）。而并发使用同一个 refresh，在模块看来
  // 【与失窃无法区分】。
  //
  // Next 默认对视口内 <Link> 预取（本站布局 3 + 刷题页 3 + 卡片 4），
  // 而本 proxy 的 matcher 覆盖 RSC 请求 ⇒ access 过期后一次渲染约 10 个
  // 预取【并发】拿同一个 refresh 来换 ⇒ 1 个成功、其余判为 Replayed
  // ⇒ 用户在所有设备上被登出。⚠️ 实测过：10 个并发预取 → 9 条 replay_detected。
  //
  // ## ⛔ 为什么不能按「预取头」判断（2026-09-07 实测）
  //
  // proxy 收到的请求头只有 accept / host / user-agent / x-forwarded-*。
  // `Next-Router-Prefetch`、`RSC`、`Next-Router-State-Tree` 全部在 proxy
  // 看到请求【之前】就被剥掉了，`?_rsc=` 查询参数也不在 nextUrl 里。
  // ⚠️ 我曾按读那个头写过一版并当作已修 —— 它编译进去了、看起来合理、
  // 完全没生效。是门槛实测把它抓出来的。
  //
  // ## ⭐ 改用 Accept 区分，因为它【是】proxy 收得到的
  //
  //   文档导航  Accept: text/html,...        ← 浏览器一次只加载一个文档，天然串行
  //   RSC/预取  Accept: text/x-component     ← 会并发，⛔ 不许刷新
  //
  // ⇒ 刷新只在文档导航上发生 ⇒ 同一时刻只有一个 ⇒ 不会被判成重放。
  //
  // ⚠️ 代价写明：RSC 请求在 access 过期时拿不到新票，于是走下面的
  // 重定向 —— 那会让客户端路由退化成一次硬导航（整页加载），
  // 而硬导航是文档请求，会正常续期。⇒ 用户看到的是「点一下慢了一拍」，
  // ⛔ 不是被登出。
  const isDocument = (req.headers.get("accept") ?? "").includes("text/html");

  const rt = req.cookies.get(REFRESH_COOKIE)?.value;
  if (rt && !isDocument) {
    // ⛔ RSC / 预取：不刷新、也不去登录页。
    //
    // ⚠️ 送去登录页会把「token 该续期了」变成「你被登出了」——
    // 而用户手上明明有一个有效的 refresh。
    // ⭐ 重定向回同一地址会让路由退化成硬导航，那一次是文档请求，
    // 会走上面的续期分支。代价是一次整页加载，⛔ 不是一次登出。
    return NextResponse.redirect(req.url);
  }

  // access 没了但 refresh 还在 → 静默续期后放行本次请求（只到这里的都是文档导航）
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
