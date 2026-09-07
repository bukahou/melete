import { NextResponse } from "next/server";
import { completeRecovery, sendRecoveryCode } from "@/lib/api";
import { oidc } from "@/lib/auth";

/**
 * 找回密码两步。
 *
 * ⛔⛔ 发码那一步【无论如何都跳到同一个页面状态】——
 * ⚠️ 后端对不存在/未验证的地址也返回 204（防枚举），前端若在这里
 * 按结果分叉，就把后端刻意抹掉的差异又加了回来。
 */
export async function POST(req: Request) {
  const form = await req.formData();
  const step = String(form.get("step") ?? "");
  const email = String(form.get("email") ?? "");

  const to = (params: Record<string, string>) => {
    const url = new URL("/auth/forgot", oidc.origin);
    for (const [k, v] of Object.entries(params)) url.searchParams.set(k, v);
    return NextResponse.redirect(url, 303);
  };

  if (step === "send") {
    if (!email) return to({});
    const status = await sendRecoveryCode(email);
    // ⚠️ 429 可以明说 —— 它按地址/IP 计数，与账号是否存在无关，⛔ 不泄漏。
    if (status === 429) return to({ n: "rate" });
    // ⛔ 其余一律进入「已发送」状态，包括 204 以外的意外 ——
    // 让失败也长得一样，⛔ 不给探测者任何信号。
    return to({ sent: "1" });
  }

  if (step === "reset") {
    const code = String(form.get("code") ?? "");
    const password = String(form.get("password") ?? "");
    if (!email || !code || !password) return to({ sent: "1", n: "bad" });
    const status = await completeRecovery({ email, code, newPassword: password });
    if (status !== 200) return to({ sent: "1", n: "bad" });
    // ⭐ 重置成功 ⇒ 旧会话全部吊销 ⇒ 用新密码重新登录。
    const url = new URL("/auth/login", oidc.origin);
    url.searchParams.set("reset", "1");
    return NextResponse.redirect(url, 303);
  }
  return to({});
}
