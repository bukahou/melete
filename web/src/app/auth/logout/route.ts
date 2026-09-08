import { NextResponse } from "next/server";
import { oidc } from "@/lib/auth";
import { clearSession, logAuth, readRefreshToken } from "@/lib/session";

const API = process.env.MELETE_API_BASE ?? "http://localhost:8899/api/v1";

/** 登出：先让 API 吊销该会话（refresh 落库，可撤回），再去 Akasha 结束中枢会话。 */
export async function GET() {
  const rt = await readRefreshToken();
  if (rt) {
    // 吊销失败不该阻断登出 —— 本地 cookie 清掉，用户体感已登出
    await fetch(`${API}/auth/logout`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ refreshToken: rt }),
    }).catch((e) => console.error("[auth] 吊销会话失败", e));
  }

  const end = new URL(`${oidc.issuer}/end_session`);
  end.search = new URLSearchParams({
    client_id: oidc.clientId,
    post_logout_redirect_uri: `${oidc.origin}/`,
  }).toString();

  const res = NextResponse.redirect(end);
  clearSession(res);
  logAuth("logout", { hadRefresh: Boolean(rt) });
  return res;
}
