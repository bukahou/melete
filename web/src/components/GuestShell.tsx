import { CalendarRange, ChevronRight, Layers, Play, Search, Shuffle, SlidersHorizontal } from "lucide-react";
import { getLocale, getTranslations } from "next-intl/server";
import { SideNav } from "@/components/SideNav";
import type { Locale } from "@/i18n/locales";

/**
 * 未登录时垫在登录弹窗背后的「应用外壳」（2026-10-08 用户裁定：参照 it-pass，⛔ 不再一上来就是一整页的墙）。
 *
 * 它只是一张画：真实的侧栏 + 首页骨架（卡片标题是真的，数字一律「—」），整块 inert + aria-hidden ——
 * 点不动、键盘进不去、读屏跳过。⛔ 不取任何数据：登录前不碰 API，也就不存在「没登录看到了什么」的问题。
 * ⚠️ 登录仍然是必须的（用户裁定）—— 这里改的只是「先看到这是什么，再登录」的观感。
 */
export async function GuestShell() {
  const [t, nav, locale] = await Promise.all([getTranslations("dash"), getTranslations(), getLocale()]);
  const tiles = [
    { icon: CalendarRange, ico: "ico-teal", title: t("drillYear") },
    { icon: Layers, ico: "ico-amber", title: t("drillDomain", { domain: t("guestDomain") }) },
    { icon: SlidersHorizontal, ico: "ico-violet", title: t("drillPick") },
    { icon: Shuffle, ico: "ico-rose", title: t("drillRandom") },
    { icon: Play, ico: "ico-indigo", title: t("drillContinue") },
  ];
  const card = (title: string, body: React.ReactNode) => (
    <section className="card px-5 py-4">
      <div className="mb-3 flex items-center gap-3">
        <h2 className="shrink-0 text-[0.95rem] font-semibold">{title}</h2>
        <span className="h-px flex-1 bg-line" />
        <ChevronRight size={16} className="text-muted" />
      </div>
      {body}
    </section>
  );
  const dash = <span className="text-[1.6rem] font-medium leading-none text-muted">—</span>;

  return (
    <div className="pointer-events-none flex min-h-screen select-none" aria-hidden inert>
      <SideNav
        locale={locale as Locale}
        version=""
        labels={{
          home: nav("nav.home"), history: nav("nav.history"), glossary: nav("nav.glossary"),
          bookmarks: nav("nav.bookmarks"), settings: nav("common.settings"),
          logout: nav("common.logout"), soon: nav("nav.soon"), admin: nav("nav.admin"), mine: nav("nav.mine"),
        }}
      />
      <main className="min-w-0 flex-1 px-5 pb-24 pt-6 md:px-8">
        <div className="mx-auto grid w-full max-w-[960px] gap-4">
          <div className="flex items-center gap-3 rounded-full border border-line bg-tile px-5 py-3 text-muted">
            <Search size={17} /><span className="text-[0.95rem]">{t("searchPlaceholder")}</span>
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            {card(t("historyTitle"), <div className="grid grid-cols-2 gap-4 text-center">
              <div><div className="text-[0.75rem] text-muted">{t("historyRate")}</div><div className="mt-1">{dash}</div></div>
              <div><div className="text-[0.75rem] text-muted">{t("historyAnswered")}</div><div className="mt-1">{dash}</div></div>
            </div>)}
            {card(t("passTitle"), <p className="text-[0.92rem] text-muted">{t("passYours")} —</p>)}
          </div>
          <section className="card px-5 py-4">
            <div className="mb-3 flex items-center gap-3">
              <h2 className="shrink-0 text-[0.95rem] font-semibold">{t("drillTitle")}</h2>
              <span className="h-px flex-1 bg-line" />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              {tiles.map((e) => (
                <div key={e.title} className="tile flex items-center gap-3 px-3 py-2.5">
                  <span className={`ico ${e.ico} h-9 w-9 shrink-0`}><e.icon size={18} /></span>
                  <span className="flex-1 text-[0.92rem] font-medium">{e.title}</span>
                  <ChevronRight size={16} />
                </div>
              ))}
            </div>
          </section>
        </div>
      </main>
    </div>
  );
}
