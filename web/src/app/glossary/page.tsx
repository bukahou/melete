import { getTranslations } from "next-intl/server";
import { ComingSoon } from "@/components/ComingSoon";

/** 侧栏「用语集」—— 即将开放（P9 第 6 步）。 */
export default async function GlossaryPage() {
  const t = await getTranslations("nav");
  return <ComingSoon title={t("glossary")} body={t("glossaryBody")} soon={t("soon")} back={t("backHome")} />;
}
