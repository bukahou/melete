import { NextResponse, type NextRequest } from "next/server";
import {
  ACCESS_COOKIE,
  REFRESH_COOKIE,
  clearAccess,
  commitSession,
  decideProxyAction,
  logAuth,
  refreshWithApi,
} from "@/lib/session";

/**
 * 全站登录墙 + **access token 自动续期**（预判式的那一半）。
 *
 * 登录墙的理由是版权边界（用户拍板，见 learning-flows.md §3）：
 * 题库内容公网可访问 = 向任何人分发题库，与「数据放私有仓」是同一个问题 ——
 * 代码仓公开而数据私有，站点却裸奔的话，前面那道边界等于没有。
 *
 * 自动续期是双 token 架构在 web 端的落点，与 iOS 的 interceptor 同构：
 * access 过期就用 refresh 换新的，用户无感，而不是被踢回登录页。
 * access 的 TTL 由 API 决定（token 自己的 exp），⛔ 本文件不假设具体数值。
 *
 * ## 2026-09-08 重写：从「看 cookie 在不在」改为「看 token 有没有效」
 *
 * 旧版第一行是 `if (有 access cookie) 放行`。它判断不了 token 是否已过期 ——
 * 而 cookie 当时被故意设成比 token 多活 5 分钟，于是那 5 分钟里「信封在、信作废」，
 * 每次访问都放行 → SSR 调 API 401 → 500 错误页，刷新也走同一条路。
 * 现在 proxy 直接读 token payload 里的 exp（⛔ 不验签，见 session-core.decodeExp），
 * 快过期就提前刷 —— 决策表在 session-core.decideProxyAction，本文件只执行。
 *
 * ## ⭐⭐ 刷新【只发生在文档导航上】—— 「预取放大导致全设备登出」的修法
 *
 * 模块（gokit/localauth）检测到 refresh 重放时【吊销该用户全部会话】，且刻意没有开关
 * （代价不对称：误伤=重登一次；漏放=攻击者偷到的 refresh 链完好续到 TTL 结束）。
 * 而并发使用同一个 refresh，在模块看来【与失窃无法区分】。
 * Next 默认对视口内 <Link> 预取，一次渲染约 10 个 RSC 请求并发到达 ——
 * ⚠️ 实测过：10 个并发预取 → 9 条 replay_detected → 用户在所有设备上被登出。
 *
 * ⛔ 不能按 `Next-Router-Prefetch` 头判断：它在 proxy 看到请求【之前】就被剥掉了
 * （2026-09-07 实测，曾按那个头写过一版并当作已修，完全没生效）。
 * ⭐ 用 Accept 区分，因为它【是】proxy 收得到的：
 *   文档导航  Accept: text/html        ← 浏览器一次只加载一个文档，天然串行
 *   RSC/预取  Accept: text/x-component ← 会并发，⛔ 不许刷新，重定向回同 URL 退化成文档导航
 *
 * ⚠️ 剩余风险（本文件解不了）：多标签页 / 多副本下两次文档导航仍可能并发刷新。
 *   提前 60s 刷把概率压得很低但不为零；根治是 gokit 给 refresh 加重用宽限 —— 登录模块的事。
 */
export async function proxy(req: NextRequest) {
  const access = req.cookies.get(ACCESS_COOKIE)?.value ?? null;
  const refresh = req.cookies.get(REFRESH_COOKIE)?.value ?? null;
  const isDocument = (req.headers.get("accept") ?? "").includes("text/html");
  const path = req.nextUrl.pathname;

  const decision = decideProxyAction({ access, refresh, isDocument, nowS: Math.floor(Date.now() / 1000) });

  if (decision === "pass") {
    // 把当前路径塞给下游：Server Component 拿不到自己的 URL，
    // 而 api.ts 撞到 401 时要知道「回哪」（/auth/renew?return=…）。
    // 用 set 而不是 append —— 客户端伪造的同名头在这里被覆盖。
    const h = new Headers(req.headers);
    h.set("x-pathname", path + req.nextUrl.search);
    return NextResponse.next({ request: { headers: h } });
  }

  if (decision === "bounce") {
    // RSC / 预取：不刷新、也不去登录页 —— 送去登录页会把「该续期了」变成「你被登出了」。
    // 重定向回同一地址让路由退化成硬导航，那一次是文档请求，会走 refresh。
    return NextResponse.redirect(req.url);
  }

  if (decision === "refresh" && refresh) {
    const pair = await refreshWithApi(refresh);
    if (pair) {
      logAuth("refresh.ok", { path, reason: access ? "expiring" : "missing" });
      // 重定向回同一地址：让下游的服务端组件能读到新 cookie。
      // 直接 next() 的话新 cookie 只在响应头上，本次渲染仍拿不到 token。
      const retry = NextResponse.redirect(req.url);
      commitSession(retry, pair);
      return retry;
    }
    // refresh 也失效（过期 / 被吊销 / 已轮换）→ 两个 cookie 都清，走登录
    logAuth("refresh.failed", { path });
    const login = toLogin(req);
    login.cookies.delete(REFRESH_COOKIE);
    clearAccess(login);
    return login;
  }

  // login：没有可用的 refresh
  const login = toLogin(req);
  if (access) {
    // 一个已死的 access 别留着 —— 否则下次还要再判一遍才到这里
    clearAccess(login);
    logAuth("login.dead-access", { path });
  }
  return login;
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
  //
  // ⚠️ /auth/ 的豁免同时也让 /auth/renew 不会被本墙拦成循环（它要在无 access 时可达）。
  //
  // ⭐ 语言切换的两个端点也放行，且【只有这两条完整路径】——
  // 登录页本身要能换语言（看不懂中文的人正是在那一页被挡住的），
  // 而它们只写一个 cookie，不读也不写任何账号数据。
  // ⚠️ 刻意写成完整路径而不是 `settings/language` 前缀：前缀豁免意味着
  // 「以后每一个加在这个前缀下的端点都默认公开」，而加端点的人不会来读这里 ——
  // 与后端 RequireUserExcept 从前缀改成完整路径是同一条教训。
  matcher: [
    "/((?!auth/|_next/|favicon\\.ico|icon\\.|apple-icon\\.|manifest\\.|settings/language$|settings/language-form$).*)",
  ],
};
