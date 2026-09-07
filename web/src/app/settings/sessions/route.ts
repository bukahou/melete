import { NextResponse } from "next/server";
import { revokeOtherSessions } from "@/lib/api";
import { oidc } from "@/lib/auth";

/**
 * 登出其它设备。
 *
 * ⛔ 前端【不传】要保留哪一条 —— 后端从当前 access token 的 sid 取。
 * ⚠️ 否则任何人都能构造一个「保留别人的会话、踢掉我的」的请求。
 */
export async function POST() {
  const back = (n: string) => {
    const url = new URL("/settings", oidc.origin);
    url.searchParams.set("n", n);
    return NextResponse.redirect(url, 303);
  };
  const res = await revokeOtherSessions();
  if (!res.ok) {
    // 409 = 当前 token 不含 sid（阶段 3 之前签发的旧票）。
    // ⛔ 后端在这种情况下拒绝而不是「登出全部」—— 那会把一次误操作放大成自己也掉线。
    console.error("[settings] 登出其它设备失败", res.status);
    return back("fail");
  }
  return back("sess-ok");
}
