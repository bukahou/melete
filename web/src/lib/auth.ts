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
 *
 * ⭐ 2026-09-08 起本文件只剩 OIDC 配置。cookie 的读、写、清、刷新、过期判定
 *   全部搬到 lib/session.ts（门面）+ lib/session-core.ts（纯逻辑）——
 *   此前它们散在 auth.ts / proxy.ts / 各 route 里，cookie 寿命和刷新时机脱节了 5 分钟
 *   而没人发现。⛔ 不要再往这里加 cookie 相关的东西。
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
