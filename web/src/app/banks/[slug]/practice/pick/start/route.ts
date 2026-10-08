import { NextResponse } from "next/server";
import { oidc } from "@/lib/auth";
import { drillHref } from "@/lib/drillSpec";
import { pickContextFromForm } from "@/lib/pickForm";

/** 4.3 表单 → 刷题 URL（P9 #12）。只做字段归一，⛔ 不查库。字段形状见 pickForm。 */
export async function GET(req: Request, { params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const ctx = pickContextFromForm(new URL(req.url).searchParams);
  // ⚠️ 基址取 oidc.origin，⛔ 不用 req.url —— standalone 模式下它是 0.0.0.0
  return NextResponse.redirect(new URL(drillHref(slug, ctx), oidc.origin), 303);
}
