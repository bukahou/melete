import { NextResponse } from "next/server";
import { setUserTier } from "@/lib/api";
import { oidc } from "@/lib/auth";

/**
 * 升级 / 降级（用户管理页的表单入口，P9 #27）。成功或失败都回到同一页、同一个分页，带一个提示码。
 * ⛔ 只转发 basic / advanced —— 「设为 admin」在应用里不存在。权限与「不能操作 admin」都由后端判定。
 */
export async function POST(req: Request) {
  const form = await req.formData();
  const userId = String(form.get("userId") ?? "");
  const tier = String(form.get("tier") ?? "");
  const page = Math.max(1, Number(form.get("page")) || 1);
  // ⚠️ 基址取 oidc.origin，⛔ 不用 req.url —— standalone 模式下它是 0.0.0.0。
  const back = (n: string) => {
    const url = new URL("/admin", oidc.origin);
    if (page > 1) url.searchParams.set("page", String(page));
    url.searchParams.set("n", n);
    return NextResponse.redirect(url, 303); // 303：POST → GET
  };
  if (!userId || (tier !== "basic" && tier !== "advanced")) return back("fail");

  const res = await setUserTier(userId, tier);
  if (res.ok) return back("ok");
  if (res.status === 409) return back("admin");
  console.error("[admin] 设定档位失败", res.status);
  return back("fail");
}
