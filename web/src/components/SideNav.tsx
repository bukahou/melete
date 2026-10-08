"use client";

import Link from "next/link";
import Image from "next/image";
import { usePathname } from "next/navigation";
import { Bookmark, BookOpen, House, LineChart, LogOut, Settings, UserRound } from "lucide-react";
import logo from "@/app/icon.png";
import { LanguageSwitcher } from "./LanguageSwitcher";
import type { Locale } from "@/i18n/locales";

/**
 * 左侧栏（桌面）+ 底部标签栏（手机）—— 版式参照 it-pass（2026-10-08 用户裁定）。
 *
 * ⭐ 图标在上、文字在下，当前项在图标后面垫一块白色胶囊。
 * 书签（P9 第 5 步）与用语集（第 6 步）先摆出来、标「即将开放」（用户裁定）——
 *   点进去是说明页，⛔ 不是 404。做好之后去掉 soon 即可。
 *
 * 为什么是客户端组件：当前项要看路径，而 layout 是服务端组件、拿不到路径。
 * 文案由 layout 传进来 —— 这里不碰 i18n，保持它只管「画」。
 */
export type NavLabels = {
  home: string; history: string; glossary: string; bookmarks: string;
  mine: string; settings: string; logout: string; soon: string;
};

const MAIN = [
  { href: "/", icon: House, key: "home", match: (p: string) => p === "/" },
  { href: "/history", icon: LineChart, key: "history", match: (p: string) => p.startsWith("/history") || /^\/banks\/[^/]+$/.test(p) },
  { href: "/glossary", icon: BookOpen, key: "glossary", soon: true, match: (p: string) => p.startsWith("/glossary") },
  { href: "/bookmarks", icon: Bookmark, key: "bookmarks", soon: true, match: (p: string) => p.startsWith("/bookmarks") },
] as const;

function Item({ href, icon: Icon, label, active, soon, soonLabel, compact }: {
  href: string; icon: typeof House; label: string; active: boolean; soon?: boolean; soonLabel: string; compact?: boolean;
}) {
  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      title={soon ? `${label}（${soonLabel}）` : label}
      className={`group flex flex-col items-center gap-1 ${compact ? "flex-1 py-1.5" : "w-full py-1"} ${soon ? "opacity-60" : ""}`}
    >
      <span className={`flex h-8 w-12 items-center justify-center rounded-full transition-colors ${
        active ? "bg-tile text-accent-ink shadow-sm" : "text-ink group-hover:bg-tile/60"}`}>
        <Icon size={18} strokeWidth={active ? 2.4 : 1.9} />
      </span>
      <span className={`text-[0.68rem] leading-tight ${active ? "font-semibold text-accent-ink" : "text-ink"}`}>{label}</span>
    </Link>
  );
}

export function SideNav({ labels, locale, version }: { labels: NavLabels; locale: Locale; version: string }) {
  const path = usePathname() ?? "/";
  return (
    <>
      {/* 桌面：左侧栏 */}
      <aside className="sticky top-0 hidden h-screen w-[84px] shrink-0 flex-col items-center gap-1 bg-rail py-4 md:flex">
        <Link href="/" className="mb-4 flex flex-col items-center gap-1" title="Melete">
          <Image src={logo} alt="" width={28} height={28} priority />
          <span className="text-[0.7rem] font-semibold tracking-wide">Melete</span>
        </Link>
        <nav className="flex w-full flex-col items-center gap-3">
          {MAIN.map((m) => (
            <Item key={m.href} href={m.href} icon={m.icon} label={labels[m.key]} active={m.match(path)}
                  soon={"soon" in m && m.soon} soonLabel={labels.soon} />
          ))}
        </nav>
        <div className="mt-auto flex w-full flex-col items-center gap-3">
          <Item href="/me" icon={UserRound} label={labels.mine} active={path.startsWith("/me")} soonLabel={labels.soon} />
          <Item href="/settings" icon={Settings} label={labels.settings} active={path.startsWith("/settings")} soonLabel={labels.soon} />
          <div className="max-w-full px-1 text-[0.7rem]"><LanguageSwitcher current={locale} short /></div>
          <a href="/auth/logout" title={labels.logout} className="text-muted transition-colors hover:text-ink">
            <LogOut size={14} />
          </a>
          {/* 版本号 = 镜像 tag：打开页面就能回答「跑的是哪个 commit」 */}
          <span className="font-mono text-[0.58rem] text-muted opacity-70">{version}</span>
        </div>
      </aside>

      {/* 手机：底部标签栏。我的 / 语言 / 登出 收进设置页 */}
      <nav className="fixed inset-x-0 bottom-0 z-20 flex border-t border-line bg-rail px-1 pb-[env(safe-area-inset-bottom)] md:hidden">
        {MAIN.map((m) => (
          <Item key={m.href} href={m.href} icon={m.icon} label={labels[m.key]} active={m.match(path)}
                soon={"soon" in m && m.soon} soonLabel={labels.soon} compact />
        ))}
        <Item href="/settings" icon={Settings} label={labels.settings} active={path.startsWith("/settings")} soonLabel={labels.soon} compact />
      </nav>
    </>
  );
}
