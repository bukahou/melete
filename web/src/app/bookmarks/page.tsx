import { redirect } from "next/navigation";
import { getMyBank } from "@/lib/api";

export const revalidate = 0;

/**
 * 侧栏「书签」→ 当前题库的「自选条件 · 收藏」（P9 #20）。
 * 收藏的题本来就是 4.3 列表的一个筛选 —— 同一份列表（可打乱、可再叠加考纲域、点哪题从哪题开始），
 * ⛔ 不另做一个只会列题的书签页。没有当前题库就回首页（那里会引导去设置选）。
 */
export default async function BookmarksPage() {
  const cur = await getMyBank();
  redirect(cur.bankSlug ? `/banks/${cur.bankSlug}/practice/pick?st=bookmarked` : "/");
}
