import { redirect } from "next/navigation";

/**
 * 旧「我的」页（学习统计）—— 2026-10-08 用户裁定删除：登录只用 Akasha，没有账号信息可展示；
 * 统计内容（掌握率、薄弱点）学习履历已有，练习入口首页已有。
 * 留这条重定向，让书签与旧链接不 404。
 */
export default function MePage() {
  redirect("/history");
}
