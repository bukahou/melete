import Link from "next/link";
import { ChevronRight } from "lucide-react";

/** 一行术语：名称 · 读音或正式名 · 出题数。目录检索结果与分类页共用（无 hook，服务端也能渲染）。 */
export function TermRow({ href, name, sub, count }: { href: string; name: string; sub?: string; count?: string }) {
  return (
    <Link href={href} className="group flex items-center gap-3 px-5 py-2.5 transition-colors hover:bg-accent-soft">
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[0.92rem]">{name}</span>
        {sub && <span className="block truncate text-[0.72rem] text-muted">{sub}</span>}
      </span>
      {count && <span className="shrink-0 text-[0.72rem] text-muted tabular-nums">{count}</span>}
      <ChevronRight size={15} className="shrink-0 text-muted group-hover:text-accent-ink" />
    </Link>
  );
}
