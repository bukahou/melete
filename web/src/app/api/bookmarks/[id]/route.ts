import { NextResponse } from "next/server";
import { setBookmark } from "@/lib/api";
import { readAccessToken } from "@/lib/session";

/**
 * 收藏 / 取消收藏（P9 #20）—— 浏览器 → 这里 → 后端。
 * 浏览器不直接碰 API（api 不对公网暴露），与 /api/attempts 同一个形状。
 */
async function toggle(req: Request, id: string, on: boolean) {
  const questionId = Number(id);
  if (!Number.isInteger(questionId) || questionId <= 0) return NextResponse.json({ error: "bad id" }, { status: 400 });
  // 会话过期要明说（401），客户端据此提示重新登录 —— ⛔ 不静默失败
  if (!(await readAccessToken())) return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
  const res = await setBookmark(questionId, on);
  return res.ok ? new NextResponse(null, { status: 204 }) : NextResponse.json({ error: "failed" }, { status: res.status });
}

export async function PUT(req: Request, { params }: { params: Promise<{ id: string }> }) {
  return toggle(req, (await params).id, true);
}

export async function DELETE(req: Request, { params }: { params: Promise<{ id: string }> }) {
  return toggle(req, (await params).id, false);
}
