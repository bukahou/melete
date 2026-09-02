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

export const oidc = {
  get issuer() { return required("MELETE_OIDC_ISSUER"); },
  get clientId() { return required("MELETE_OIDC_CLIENT_ID"); },
  get clientSecret() { return required("MELETE_OIDC_CLIENT_SECRET"); },
  get origin() { return required("MELETE_WEB_ORIGIN"); },
  get redirectUri() { return `${this.origin}/auth/callback`; },
};

export const ACCESS_COOKIE = "melete_at";
export const REFRESH_COOKIE = "melete_rt";
export const TXN_COOKIE = "melete_oidc_txn"; // 登录事务中间态（state/nonce/verifier）

/** API 返回的 token 组合。 */
export interface TokenPair {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
  accountId: number;
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

// ---- PKCE / 随机数（web 侧的 OIDC 流程仍在 web 完成，只是结果交给 API）----

export function randomToken(bytes = 32): string {
  return Buffer.from(crypto.getRandomValues(new Uint8Array(bytes))).toString("base64url");
}

export async function s256(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return Buffer.from(digest).toString("base64url");
}
