import Link from "next/link";

/**
 * 一行术语：名称 · 读音或正式名 · 出题数。目录检索结果与分类页共用（无 hook，服务端也能渲染）。
 * 纸面上的细线行（#41 #42）：名字不加粗、⛔ 不放箭头（整行可点），行自己画底线 —— 外层 ⛔ 再加 divide-y。
 */
export function TermRow({ href, name, sub, count }: { href: string; name: string; sub?: string; count?: string }) {
  return (
    <Link href={href} className="mx-2 flex min-h-14 items-center gap-3 border-b border-line-2 py-2 pl-5 pr-2 transition-colors hover:bg-accent-soft">
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[0.97rem]">{name}</span>
        {sub && <span className="block truncate text-[0.74rem] text-muted">{sub}</span>}
      </span>
      {count && <span className="shrink-0 text-[0.84rem] text-muted tabular-nums">{count}</span>}
    </Link>
  );
}
