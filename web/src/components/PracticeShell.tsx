import Link from "next/link";
import { ChevronRight } from "lucide-react";
import { Band } from "./Band";

/**
 * 四个出题入口页（P9 4.1–4.4）共用的外壳：面包屑回首页 + 标题 + 一句说明。
 * 入口页是阅读宽度的列表，⛔ 不用 Wide —— 一行一个选项，扫视比铺满屏更快。
 */
export function PracticeShell({ home, title, sub, children }: {
  home: string; title: string; sub: string; children: React.ReactNode;
}) {
  return (
    <div className="mx-auto max-w-3xl pt-2">
      <Band crumbs={[{ href: "/", label: home }, { label: title }]} title={title} sub={sub} />
      <div className="grid gap-4">{children}</div>
    </div>
  );
}

/**
 * 入口页的一行：名字在左，规模与我的情况在右，整行可点。
 *
 * ⭐ 2026-10-08 改版：一行只占一行。旧版名字下面再挂一行「N 题 · 已答…」，
 *   IPA 的三层树（35 行）因此拉到两屏长、层级全靠缩进辨认。
 * level 表示层级（考纲树：0 分野 › 1 大分類 › 2 中分類）—— 用字重与字号区分，⛔ 不只靠缩进。
 */
export function EntryRow({ href, title, meta, stat, rate, level = 0 }: {
  href: string; title: string; meta?: string; stat?: string; rate?: number | null; level?: 0 | 1 | 2;
}) {
  const style = [
    "py-3.5 pl-5 text-[0.98rem] font-semibold",
    "py-3 pl-5 text-[0.92rem] font-semibold",
    "py-2 pl-9 text-[0.86rem]",
  ][level];
  return (
    <Link
      href={href}
      className={`group flex items-center gap-4 pr-4 transition-colors hover:bg-accent-soft ${style}`}
    >
      <span className="min-w-0 flex-1 truncate">{title}</span>
      {rate != null && (
        <span className="hidden h-1 w-20 overflow-hidden rounded-full bg-line-2 sm:block">
          <span className="block h-full rounded-full bg-muted" style={{ width: `${rate}%` }} />
        </span>
      )}
      <span className="shrink-0 text-right text-[0.76rem] font-normal text-muted tabular-nums">
        {[stat, meta].filter(Boolean).join(" · ")}
      </span>
      <ChevronRight size={15} className="shrink-0 text-muted transition-colors group-hover:text-accent-ink" />
    </Link>
  );
}
