import Link from "next/link";
import { getTranslations } from "next-intl/server";

/**
 * 404。⚠️ 看不到的题库（P9 #27）与真的不存在走的是同一页、同一句话 ——
 * ⛔ 不提示「你没有权限」：那等于告诉普通用户「这里有东西」。
 */
export default async function NotFound() {
  const t = await getTranslations();
  return (
    <div className="mx-auto max-w-md pt-16 text-center">
      <p className="font-mono text-[0.8rem] text-muted">404</p>
      <h1 className="display mt-2 text-[1.4rem]">{t("common.notFoundTitle")}</h1>
      <p className="mt-3 text-[0.9rem] text-muted">{t("common.notFoundBody")}</p>
      <Link href="/" className="mt-7 inline-block rounded-lg px-5 py-2.5 text-[0.9rem] font-semibold"
            style={{ background: "var(--color-accent-soft)", color: "var(--color-accent-ink)" }}>
        {t("nav.backHome")}
      </Link>
    </div>
  );
}
