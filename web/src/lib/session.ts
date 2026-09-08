import { cookies } from "next/headers";
import { oidc } from "./auth";
import {
  ACCESS_COOKIE,
  REFRESH_COOKIE,
  RENEW_MARK_COOKIE,
  RENEW_MARK_TTL_S,
  type TokenPair,
} from "./session-core";

export * from "./session-core";

/**
 * 会话生命周期的【门面】—— cookie 的读、写、清、刷新，全部只经这里。
 *
 * 纯逻辑（过期判定、proxy 决策、return 校验）在 session-core.ts；
 * 这里是绑定 Next 运行时的副作用层。proxy.ts / api.ts / 各 route ⛔ 不再自己
 * `cookies.set` —— 2026-09-08 之前它们各写各的，于是 cookie 寿命（auth.ts）
 * 和刷新时机（proxy.ts）脱节了 5 分钟，谁也没发现。改一处 = 改全部，是这个门面的意义。
 */

const API = process.env.MELETE_API_BASE ?? "http://localhost:8899/api/v1";

/** NextResponse 与 Route Handler 的 response 都满足这个最小接口。 */
export interface CookieWriter {
  cookies: {
    set: (name: string, value: string, opts: Record<string, unknown>) => void;
    delete: (name: string) => void;
  };
}

function cookieBase() {
  return { httpOnly: true, sameSite: "lax" as const, secure: oidc.origin.startsWith("https"), path: "/" };
}

/**
 * 写入一对新 token。
 *
 * ⭐ access 的 maxAge **严格等于** expiresIn，⛔ 不再 +300。
 *   旧注释说「让 cookie 比 token 多活一点，由 proxy 触发刷新」—— 意图是对的，
 *   但 proxy 当时只看 cookie 在不在、从不在 cookie 存在时刷新，于是那 5 分钟
 *   变成「信封在、信作废、每次访问 500」。现在 proxy 直接读 token 的 exp（core），
 *   不再需要靠 cookie 寿命来「感知」过期，maxAge 只是第二道保险。
 */
export function commitSession(res: CookieWriter, pair: TokenPair): void {
  const base = cookieBase();
  res.cookies.set(ACCESS_COOKIE, pair.accessToken, { ...base, maxAge: pair.expiresIn });
  res.cookies.set(REFRESH_COOKIE, pair.refreshToken, { ...base, maxAge: 30 * 24 * 3600 });
}

/** 登出：两个都清。 */
export function clearSession(res: CookieWriter): void {
  res.cookies.delete(ACCESS_COOKIE);
  res.cookies.delete(REFRESH_COOKIE);
  res.cookies.delete(RENEW_MARK_COOKIE);
}

/** 只清 access（/auth/renew 用）：留着 refresh，让 proxy 在下一跳换新。 */
export function clearAccess(res: CookieWriter): void {
  res.cookies.delete(ACCESS_COOKIE);
}

/** 打上「刚 renew 过」标记，30s 内再撞 401 就升级为登出（防死循环，见 core）。 */
export function markRenewed(res: CookieWriter): void {
  res.cookies.set(RENEW_MARK_COOKIE, "1", { ...cookieBase(), maxAge: RENEW_MARK_TTL_S });
}

/** 当前请求的 access token；没有则未登录。 */
export async function readAccessToken(): Promise<string | null> {
  return (await cookies()).get(ACCESS_COOKIE)?.value ?? null;
}

export async function readRefreshToken(): Promise<string | null> {
  return (await cookies()).get(REFRESH_COOKIE)?.value ?? null;
}

export async function hasRenewMark(): Promise<boolean> {
  return Boolean((await cookies()).get(RENEW_MARK_COOKIE)?.value);
}

/**
 * 拿 refresh 去 API 换一对新的。失败（过期 / 被吊销 / 被判重放 / 网络）一律 null。
 *
 * ⚠️ 调用方必须保证**同一时刻只有一个**调用在飞（proxy 只在文档导航上调）——
 *    gokit 把并发使用同一个 refresh 判为重放并吊销全部会话，且刻意没有开关。
 */
export async function refreshWithApi(refreshToken: string): Promise<TokenPair | null> {
  const res = await fetch(`${API}/auth/refresh`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ refreshToken }),
  }).catch(() => null);
  if (!res?.ok) return null;
  return (await res.json()) as TokenPair;
}

/**
 * 认证路径的结构化日志（一行 JSON，进 Pod 日志）。
 *
 * ⛔ 永远不记 token 本身，只记事件与原因。
 * 存在的理由：2026-09-08 那次故障，用户看到的只是 digest 2012285127，
 * 是靠翻 Pod 日志才对上「GET /banks → 401」—— 这一层此前对自己的决策一言不发。
 * 以后 refresh 成/败、renew 触发、循环熔断，都自己会说话。
 */
export function logAuth(evt: string, fields: Record<string, string | number | boolean | null> = {}): void {
  console.log(JSON.stringify({ ts: new Date().toISOString(), src: "web.auth", evt, ...fields }));
}
