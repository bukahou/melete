import { NextResponse, type NextRequest } from "next/server";
import { oidc } from "@/lib/auth";
import { clearAccess, logAuth, markRenewed, safeReturnPath } from "@/lib/session";

/**
 * 反应式续期入口 —— SSR 撞到 API 401 时来这里。
 *
 * 这是 geass 客户端拦截器「401 → refresh → 重试」的**服务端等价物**。
 * 为什么必须是独立的 Route Handler：Server Component 渲染期间 cookies() 是只读的，
 * 写不了 cookie；只有 Route Handler / proxy 能写。
 *
 * 流程：
 *   清 access cookie（留 refresh）→ 307 回 return
 *   → 那一跳 proxy 看到「无 access、有 refresh、文档导航」→ 换新 → 正常渲染
 *   → refresh 也失效 → proxy 已有逻辑：清 refresh、去登录页
 *
 * 死循环闸：同时打 30s 的 renew 标记；api.ts 在标记存在时再撞 401 → 直接登出。
 *
 * ⚠️ /auth/ 前缀在 proxy 的 matcher 豁免里，所以本路由不会被登录墙拦成循环。
 * ⚠️ `return` 必须经 safeReturnPath —— 否则这是一个开放重定向。
 */
export async function GET(req: NextRequest) {
  const back = safeReturnPath(req.nextUrl.searchParams.get("return"));
  // ⚠️ 以 oidc.origin（MELETE_WEB_ORIGIN）拼绝对地址，⛔ 不用 req.url：
  //    standalone 模式下 Route Handler 的 req.url 取的是监听地址（HOSTNAME=0.0.0.0），
  //    本地实测重定向落到了 http://0.0.0.0:3399/ —— 集群里同样是 0.0.0.0，会坏。
  //    callback / password 两个路由早就是这么写的，本路由沿用同一模式。
  const res = NextResponse.redirect(new URL(back, oidc.origin), 307);
  clearAccess(res);
  markRenewed(res);
  logAuth("renew", { back });
  return res;
}
