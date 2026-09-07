import type { Metadata } from "next";
import Link from "next/link";
import Image from "next/image";
import logo from "./icon.png";
import { Noto_Serif_SC } from "next/font/google";
import { LogOut } from "lucide-react";
import { accessToken } from "@/lib/auth";
import "./globals.css";

const notoSerif = Noto_Serif_SC({
  subsets: ["latin"],
  weight: ["600", "900"],
  variable: "--font-noto-serif",
  display: "swap",
});

export const metadata: Metadata = {
  title: { default: "Melete — 练习与修习", template: "%s · Melete" },
  description: "题库驱动的学习平台。题目是第一等公民，答案是一组带来源的主张。",
};

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  // 登录态只用于决定是否显示登出入口；用户名从 token 的 cookie 拿不到，
  // 显示名改由页面按需取（避免每个页面都为了 header 多打一次 API）
  const signedIn = Boolean(await accessToken());
  return (
    <html lang="zh-CN" className={notoSerif.variable}>
      <body className="flex min-h-screen flex-col overflow-x-clip">
        <header className="border-b border-line">
          <div className="mx-auto flex w-full max-w-[1400px] items-baseline gap-6 px-6 py-5">
            <Link href="/" className="group flex items-center gap-2.5">
              {/* 与 atlantis / geass 同形状的作品系列标识，配色取 melete 的题库赭 */}
              <Image src={logo} alt="" width={26} height={26} priority className="translate-y-px" />
              <span className="display text-lg tracking-wide">Melete</span>
            </Link>
            <span className="hidden text-xs tracking-wide text-muted sm:inline">练习与修习</span>
            <nav className="ml-auto flex items-baseline gap-5 text-sm">
              <Link href="/" className="text-muted transition-colors hover:text-ink">
                首页
              </Link>
              {signedIn && (
                <Link href="/me" className="text-muted transition-colors hover:text-ink">
                  我的
                </Link>
              )}
              {signedIn && (
                <Link href="/settings" className="text-muted transition-colors hover:text-ink">
                  设置
                </Link>
              )}
              {signedIn && (
                <span className="flex items-baseline gap-3">
                  <a
                    href="/auth/logout"
                    title="登出"
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
            <span>司「练习、修习」的缪斯</span>
            <span className="ml-auto">被动阅读不产生学习，主动回忆才产生</span>
          </div>
        </footer>
      </body>
    </html>
  );
}
