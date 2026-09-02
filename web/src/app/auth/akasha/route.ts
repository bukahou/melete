import { NextResponse } from "next/server";
import { TXN_COOKIE, oidc, randomToken, s256 } from "@/lib/auth";

/** 发起登录：生成 PKCE + state + nonce，暂存事务 cookie，跳 Akasha。 */
export async function GET(req: Request) {
  const state = randomToken(16);
  const nonce = randomToken(16);
  const verifier = randomToken(32);

  // 登录成功后回到用户原本想去的页面
  const returnTo = new URL(req.url).searchParams.get("return") ?? "/";

  const authorize = new URL(`${oidc.issuer}/authorize`);
  authorize.search = new URLSearchParams({
    response_type: "code",
    client_id: oidc.clientId,
    redirect_uri: oidc.redirectUri,
    scope: "openid email profile",
    state,
    nonce,
    code_challenge: await s256(verifier),
    code_challenge_method: "S256",
  }).toString();

  const res = NextResponse.redirect(authorize);
  res.cookies.set(TXN_COOKIE, JSON.stringify({ state, nonce, verifier, returnTo }), {
    httpOnly: true,
    sameSite: "lax",
    secure: oidc.origin.startsWith("https"),
    maxAge: 600,
    path: "/auth",
  });
  return res;
}
