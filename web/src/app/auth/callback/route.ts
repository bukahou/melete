import { NextResponse } from "next/server";
import { TXN_COOKIE, oidc, setAuthCookies, type TokenPair } from "@/lib/auth";

const API = process.env.MELETE_API_BASE ?? "http://localhost:8899/api/v1";

/**
 * Akasha 回调。
 *
 * web 只负责走完浏览器侧的 OIDC 流程拿到 id_token，
 * **验签与换 token 都交给 API** —— 这样 iOS 端用同一个 /auth/sso 端点，
 * 且 API 不必信任任何客户端自称的身份。
 */
export async function GET(req: Request) {
  const url = new URL(req.url);
  const code = url.searchParams.get("code");
  const state = url.searchParams.get("state");

  const txnRaw = req.headers.get("cookie")?.match(new RegExp(`${TXN_COOKIE}=([^;]+)`))?.[1];
  if (!code || !state || !txnRaw) {
    return NextResponse.redirect(new URL("/auth/login", oidc.origin));
  }
  let txn: { state: string; nonce: string; verifier: string; returnTo: string };
  try {
    txn = JSON.parse(decodeURIComponent(txnRaw));
  } catch {
    return NextResponse.redirect(new URL("/auth/login", oidc.origin));
  }
  if (state !== txn.state) {
    return new NextResponse("state 不匹配（可能是 CSRF），请重新登录", { status: 400 });
  }

  // 用授权码换 id_token（confidential client：带 client_secret，全程服务端）
  const tokenRes = await fetch(`${oidc.issuer}/token`, {
    method: "POST",
    headers: { "content-type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      code,
      redirect_uri: oidc.redirectUri,
      client_id: oidc.clientId,
      client_secret: oidc.clientSecret,
      code_verifier: txn.verifier,
    }),
  });
  if (!tokenRes.ok) {
    return new NextResponse(`token 兑换失败: ${await tokenRes.text()}`, { status: 502 });
  }
  const { id_token } = (await tokenRes.json()) as { id_token: string };

  // 交给 API 验签并换取本平台的 token
  const exchange = await fetch(`${API}/auth/sso`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      idToken: id_token,
      deviceInfo: req.headers.get("user-agent")?.slice(0, 200) ?? "web",
    }),
  });
  if (!exchange.ok) {
    console.error("[auth] SSO 换 token 失败", exchange.status, await exchange.text());
    return new NextResponse("登录失败", { status: 502 });
  }

  const pair = (await exchange.json()) as TokenPair;
  const out = NextResponse.redirect(new URL(txn.returnTo || "/", oidc.origin), 303);
  setAuthCookies(out, pair);
  out.cookies.delete(TXN_COOKIE);
  return out;
}
