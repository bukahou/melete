import { redirect } from "next/navigation";
import { getMyBank } from "@/lib/api";

export const revalidate = 0;

/**
 * 侧栏「学习履历」→ 当前题库的学习台。
 * 侧栏在 layout 里，不知道当前题库；这里经 GET /me/bank 问一次再跳 ——
 * ⛔ 不让 layout 为了一个链接每页多打一次 API。没有当前题库就回首页（那里会引导去设置选）。
 */
export default async function HistoryPage() {
  const cur = await getMyBank();
  redirect(cur.bankSlug ? `/banks/${cur.bankSlug}` : "/");
}
