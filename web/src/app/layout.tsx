import type { Metadata } from "next";
import Link from "next/link";
import { NextIntlClientProvider } from "next-intl";
import { getLocale, getTranslations } from "next-intl/server";
import Image from "next/image";
import logo from "./icon.png";
import { Noto_Serif_SC } from "next/font/google";
import { LogOut } from "lucide-react";
import { readAccessToken } from "@/lib/session";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import type { Locale } from "@/i18n/locales";
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
  // 登录态只用于决定是否显示登出入口；用户名从 token 的 cookie 拿不到，
  // 显示名改由页面按需取（避免每个页面都为了 header 多打一次 API）
  const signedIn = Boolean(await readAccessToken());
  // ⭐ lang 属性不是装饰：读屏软件靠它选发音，浏览器靠它选断行与字体回退。
  // 中日共用大量汉字，标错了日文会被用中文字形渲染 —— 这是肉眼可见的错。
  const locale = (await getLocale()) as Locale;
  const t = await getTranslations();
  return (
    <html lang={locale} className={notoSerif.variable}>
      <body className="flex min-h-screen flex-col overflow-x-clip">
        <NextIntlClientProvider>
        <header className="border-b border-line">
          <div className="mx-auto flex w-full max-w-[1400px] items-baseline gap-6 px-6 py-5">
            <Link href="/" className="group flex items-center gap-2.5">
              {/* 与 atlantis / geass 同形状的作品系列标识，配色取 melete 的题库赭 */}
              <Image src={logo} alt="" width={26} height={26} priority className="translate-y-px" />
              <span className="display text-lg tracking-wide">Melete</span>
            </Link>
            <span className="hidden text-xs tracking-wide text-muted sm:inline">{t("brand.tagline")}</span>
            <nav className="ml-auto flex items-baseline gap-5 text-sm">
              <Link href="/" className="text-muted transition-colors hover:text-ink">
                {t("common.home")}
              </Link>
              {signedIn && (
                <Link href="/me" className="text-muted transition-colors hover:text-ink">
                  {t("common.mine")}
                </Link>
              )}
              {signedIn && (
                <Link href="/settings" className="text-muted transition-colors hover:text-ink">
                  {t("common.settings")}
                </Link>
              )}
              <LanguageSwitcher current={locale} />
              {signedIn && (
                <span className="flex items-baseline gap-3">
                  <a
                    href="/auth/logout"
                    title={t("common.logout")}
                    className="translate-y-0.5 text-muted transition-colors hover:text-ink"
                  >
                    <LogOut size={13} />
                  </a>
                </span>
              )}
            </nav>
          </div>
        </header>

        <main className="mx-auto w-full max-w-4xl flex-1 px-6 py-12">{children}</main>

        <footer className="border-t border-line">
          <div className="mx-auto flex w-full max-w-[1400px] flex-wrap items-baseline gap-x-6 gap-y-1 px-6 py-6 text-xs text-muted">
            <span className="display text-sm">Μελέτη</span>
            <span>{t("brand.muse")}</span>
            {/* ⭐ 版本号 = 镜像 tag，由 CI 经 build-arg 落成运行时 ENV。
                本 layout 是 async server component 且已因读 cookie 而动态渲染，
                所以这里读到的是【运行时】的值，⛔ 不是 build 期被内联的常量。
                ⚠️ 显示它的意义不只是好看：打开页面就能回答「线上跑的到底是哪个 commit」——
                在 dev / prod 两套环境并存时，这是最省事的一条自证。 */}
            <span className="font-mono opacity-70" title={t("brand.versionTitle")}>
              {process.env.APP_VERSION ?? "dev"}
            </span>
            <span className="ml-auto">{t("brand.creed")}</span>
          </div>
        </footer>
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
