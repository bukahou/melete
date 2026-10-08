import { getTranslations } from "next-intl/server";
import { ComingSoon } from "@/components/ComingSoon";

/** 侧栏「书签」—— 即将开放（P9 第 5 步）。 */
export default async function BookmarksPage() {
  const t = await getTranslations("nav");
  return <ComingSoon title={t("bookmarks")} body={t("bookmarksBody")} soon={t("soon")} back={t("backHome")} />;
}
