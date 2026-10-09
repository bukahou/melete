import type { Metadata } from "next";
import { NextIntlClientProvider } from "next-intl";
import { getLocale, getTranslations } from "next-intl/server";
import { Noto_Serif_SC } from "next/font/google";
import { unstable_rethrow } from "next/navigation";
import { readAccessToken } from "@/lib/session";
import { getMyAccess } from "@/lib/api";
import { SideNav } from "@/components/SideNav";
import { GuestShell } from "@/components/GuestShell";
import type { Locale } from "@/i18n/locales";
import { cookies } from "next/headers";
import { DEFAULT_THEME, THEME_COOKIE, isTheme } from "@/lib/theme";
import "./globals.css";

const notoSerif = Noto_Serif_SC({
  subsets: ["latin"],
  weight: ["600", "900"],
  variable: "--font-noto-serif",
  display: "swap",
});

// ⚠️ metadata 也要跟着语言走 —— 标题会进浏览器标签页与分享卡片。
// 用 generateMetadata（函数）而不是常量：常量在构建期求值，拿不到请求的语言。
export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations("brand");
  return {
    title: { default: t("metaTitle"), template: "%s · Melete" },
    description: t("metaDescription"),
  };
}

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  // 登录态决定是否显示导航（侧栏）；用户名从 token 的 cookie 拿不到，
  // 显示名改由页面按需取（避免每个页面都为了导航多打一次 API）
  const signedIn = Boolean(await readAccessToken());
  // 档位只用来决定显不显示「用户管理」入口（P9 #27）。取不到就当不是 admin —— 少显示一个入口，⛔ 不影响页面
  const isAdmin = signedIn ? await getMyAccess().then((a) => a.tier === "admin").catch((e) => { unstable_rethrow(e); return false; }) : false;
  // ⭐ lang 属性不是装饰：读屏软件靠它选发音，浏览器靠它选断行与字体回退。
  // 中日共用大量汉字，标错了日文会被用中文字形渲染 —— 这是肉眼可见的错。
  const locale = (await getLocale()) as Locale;
  const t = await getTranslations();
  const themeCookie = (await cookies()).get(THEME_COOKIE)?.value;
  const theme = isTheme(themeCookie) ? themeCookie : DEFAULT_THEME;
  return (
    <html lang={locale} data-theme={theme} className={notoSerif.variable}>
      <body className="min-h-screen overflow-x-clip">
        <NextIntlClientProvider>
        {signedIn ? (
          // ⭐ 版式参照 it-pass（2026-10-08 用户裁定）：左侧栏 + 浅彩色整页铺底，⛔ 没有顶栏与页脚
          <div className="flex min-h-screen">
            <SideNav
              locale={locale}
              version={process.env.APP_VERSION ?? "dev"}
              isAdmin={isAdmin}
              labels={{
                home: t("nav.home"), history: t("nav.history"), glossary: t("nav.glossary"),
                bookmarks: t("nav.bookmarks"), settings: t("common.settings"),
                logout: t("common.logout"), soon: t("nav.soon"), admin: t("nav.admin"),
              }}
            />
            {/* pb-24：手机上给底部标签栏让位 */}
            <main className="min-w-0 flex-1 px-5 pb-24 pt-6 md:px-8 md:pb-10">
              <div className="mx-auto w-full max-w-[1000px]">{children}</div>
            </main>
          </div>
        ) : (
          // 未登录：背后垫一张应用外壳（只是画，inert），登录页作为弹窗叠在上面 —— 参照 it-pass（2026-10-08 用户裁定）
          <div className="relative min-h-screen">
            <div className="fixed inset-0 overflow-hidden"><GuestShell /></div>
            <div className="fixed inset-0 bg-black/35 backdrop-blur-[1.5px]" />
            <main className="relative z-10 flex min-h-screen items-start justify-center px-4 py-10 md:items-center md:py-12">{children}</main>
          </div>
        )}
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
