import Link from "next/link";
import { ArrowRight } from "lucide-react";
import { Band } from "./Band";

/**
 * 四个出题入口页（P9 4.1–4.4）共用的外壳：面包屑回首页 + 标题 + 一句说明。
 * 入口页是阅读宽度的列表，⛔ 不用 Wide —— 一行一个选项，扫视比铺满屏更快。
 */
export function PracticeShell({ home, title, sub, children }: {
  home: string; title: string; sub: string; children: React.ReactNode;
}) {
  return (
    <div className="pt-2">
      <Band crumbs={[{ href: "/", label: home }, { label: title }]} title={title} sub={sub} />
      {children}
    </div>
  );
}

/**
 * 入口页的一行：名字 + 规模 + 我的情况 + 进入。整行可点。
 * indent 表示层级（考纲树：分野 › 大分類 › 中分類）。
 */
export function EntryRow({ href, title, meta, stat, rate, indent = 0 }: {
  href: string; title: string; meta?: string; stat?: string; rate?: number | null; indent?: 0 | 1 | 2;
}) {
  const pad = ["pl-5", "pl-10", "pl-16"][indent];
  return (
    <Link
      href={href}
      className={`group grid grid-cols-[1fr_auto] items-center gap-4 border-b border-line-2 py-3.5 pr-5 transition-colors last:border-b-0 hover:bg-surface ${pad}`}
    >
      <span className="min-w-0">
        <span className={`block truncate ${indent === 0 ? "font-semibold" : "text-[0.92rem]"}`}>{title}</span>
        {(meta || stat) && (
          <span className="mt-0.5 block text-[0.76rem] text-muted tabular-nums">
            {[meta, stat].filter(Boolean).join(" · ")}
          </span>
        )}
        {rate != null && (
          <span className="mt-1.5 block h-1 w-40 max-w-full overflow-hidden rounded-sm bg-line">
            <span className="block h-full rounded-sm bg-ink" style={{ width: `${rate}%` }} />
          </span>
        )}
      </span>
      <ArrowRight size={15} className="text-muted transition-colors group-hover:text-ink" />
    </Link>
  );
}
