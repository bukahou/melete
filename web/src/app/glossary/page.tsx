import { redirect } from "next/navigation";
import { resolveStudyBank } from "@/lib/api";

export const revalidate = 0;

/** 侧栏「用语集」→ 当前题库的用语集（同 /history、/bookmarks 的做法）。 */
export default async function GlossaryRedirectPage() {
  const slug = await resolveStudyBank();
  redirect(slug ? `/banks/${slug}/glossary` : "/");
}
