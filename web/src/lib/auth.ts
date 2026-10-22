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
 * ⭐ 2026-09-08 起本文件只剩认证配置（OIDC + 调试账号的密码登录开关）。cookie 的读、写、清、刷新、过期判定
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

/**
 * 调试账号的密码登录（P9 #35–#38，2026-10-21 用户裁定）开不开。
 *
 * 用户侧只有一种登录方式（#25）—— 密码登录只给调试账号用，入口是一个不在任何地方链接的页面
 * （/auth/login/password）。⭐ 「不链接」只为不打扰真实用户，⛔ 不是安全措施：仓库公开，路径谁都看得到。
 * ⭐ 本开关只管这一页。「这个环境能不能用密码登录」由 API 自己的开关决定（MELETE_API_PASSWORD_LOGIN，#39）——
 *   API 有自己的公网入口（iOS 直连），只关网页挡不住直接调 API 的人。两个都开，网页上才登得进去。
 *
 * ⛔ 缺省关闭（fail-closed）：只有部署配置显式写 "on" 才开，其它任何值都当关。哪些环境打开记在部署配置里。
 * ⚠️ 运行时读，⛔ 不得在构建期定死 —— 镜像在各环境是同一个，开关只能跟着运行环境走。
 */
export const passwordLogin = {
  get enabled() { return process.env.MELETE_WEB_PASSWORD_LOGIN === "on"; },
};
