import { cookies } from "next/headers";

/**
 * web 端的认证 —— **token 由 melete-api 签发，本层只负责持有与转发**。
 *
 * 这是 2026-09-02 的架构变更（见 docs/design/active/deployment.md §0）：
 * iOS 端的加入让 API 必须成为 token 的唯一签发方，web 与 iOS 从此是平等客户端。
 * 原先 web 自己验 id_token、自己签会话 JWT 的做法已废止 ——
 * 那套逻辑无法被原生 App 复用，且导致 API 只能「相信 web 说的账号是谁」。
 *
 * 存储位置按平台取最佳实践（token 本身两端相同）：
 *   web  → httpOnly cookie：SSR 要在服务端读到它做登录墙，且 XSS 偷不走
 *   iOS  → Keychain（见 geass-mobile/CLAUDE.md）
 */

function required(name: string): string {
  const v = process.env[name];
  if (!v) throw new Error(`缺少环境变量 ${name}（凭证类无默认值是有意为之）`);
  return v;
}

/**
 * OIDC 由 melete-api 代理（2026-09-03）：web 不再持有 client_secret、不再自己走 PKCE。
 * 登录 = 302 到 api 的 /auth/oidc/start；回来时 api 把一张一次性票据（refresh token）
 * 交给 web 服务端，web 拿它去 /auth/refresh 换正式的一对并写 cookie。
 * 这样 Akasha 只见过一个 client（api），web 与 iOS 天然同一账号。
 */
export const oidc = {
  // issuer / clientId 仍由 web 读：登出时要把浏览器送去 Akasha 的 end_session（非敏感）
  get issuer() { return required("MELETE_OIDC_ISSUER"); },
  get clientId() { return required("MELETE_OIDC_CLIENT_ID"); },
  get origin() { return required("MELETE_WEB_ORIGIN"); },
  /** api 的**公网**地址 —— 浏览器要被 302 到这里，不能用集群内的 ClusterIP */
  get apiPublicBase() { return required("MELETE_API_PUBLIC_BASE"); },
};

export const ACCESS_COOKIE = "melete_at";
export const REFRESH_COOKIE = "melete_rt";

/** API 返回的 token 组合。 */
export interface TokenPair {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
  /**
   * 账号 id —— canonical UUIDv7 文本。
   *
   * ⚠️ 2026-09-07 breaking change：曾经是 number（BIGINT 自增）。
   * 换 UUID 的理由见 cross-exam 005 §22.2：TiDB 跳号严重，
   * 且 v7 时间有序，避免 v4 的索引页分裂。
   */
  accountId: string;
  display: string;
}

/** 当前请求可用的 access token；没有则未登录。 */
export async function accessToken(): Promise<string | null> {
  return (await cookies()).get(ACCESS_COOKIE)?.value ?? null;
}

export async function refreshToken(): Promise<string | null> {
  return (await cookies()).get(REFRESH_COOKIE)?.value ?? null;
}

/**
 * 把 token 写进响应的 cookie。
 *
 * access 的 cookie 有效期刻意设得比 token 本身长一点（+5 分钟）：
 * 让「cookie 还在但 token 已过期」成为可感知的状态，由 proxy 触发刷新，
 * 而不是 cookie 先消失导致用户莫名被登出。
 */
export function setAuthCookies(
  res: { cookies: { set: (name: string, value: string, opts: Record<string, unknown>) => void } },
  pair: TokenPair,
) {
  const secure = oidc.origin.startsWith("https");
  const base = { httpOnly: true, sameSite: "lax" as const, secure, path: "/" };
  res.cookies.set(ACCESS_COOKIE, pair.accessToken, { ...base, maxAge: pair.expiresIn + 300 });
  res.cookies.set(REFRESH_COOKIE, pair.refreshToken, { ...base, maxAge: 30 * 24 * 3600 });
}

export function clearAuthCookies(res: {
  cookies: { delete: (name: string) => void };
}) {
  res.cookies.delete(ACCESS_COOKIE);
  res.cookies.delete(REFRESH_COOKIE);
}
