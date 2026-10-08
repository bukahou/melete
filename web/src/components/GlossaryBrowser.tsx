"use client";

import { useEffect, useMemo, useState } from "react";
import { Search, X } from "lucide-react";
import { useTranslations } from "next-intl";
import { usePathname, useRouter } from "next/navigation";
import { localized, type TermSummary } from "@/lib/claims";
import { termSub } from "@/lib/glossary";
import { TermRow } from "@/components/TermRow";

/**
 * 用语集检索框（P9 第 6 步）。没输入时显示 children（服务端渲染的分类目录），输入后换成命中列表。
 *
 * ⭐ 全部术语一次给到浏览器，检索在本地即时完成 —— ⛔ 不为每次按键打一次后端。
 * ⭐ 检索词同步进 URL（?q=，replace 不进历史）：从命中列表点进术语再返回，结果还在。
 *   2026-10-08 用户反馈「返回丢失当前位置」—— 位置只存在内存里，离开页面就没了。
 */
export function GlossarySearch({ slug, terms, initialQuery, children }: {
  slug: string; terms: TermSummary[]; initialQuery: string; children: React.ReactNode;
}) {
  const t = useTranslations("glossary");
  const locale = t("locale");
  const [q, setQ] = useState(initialQuery);

  // ⚠️ 用 router.replace 而不是 history.replaceState：后者只改了地址栏，Next 路由仍记着旧 URL，
  //    点进术语再返回时按旧 URL 还原 —— 检索词照样丢（实测）。停手 300ms 再同步，不为每次按键重渲染。
  const router = useRouter();
  const pathname = usePathname();
  useEffect(() => {
    const term = q.trim();
    if (term === initialQuery.trim()) return;
    const timer = setTimeout(() => {
      router.replace(term ? `${pathname}?q=${encodeURIComponent(term)}` : pathname, { scroll: false });
    }, 300);
    return () => clearTimeout(timer);
  }, [q, initialQuery, pathname, router]);

  const nameOf = (x: TermSummary) => localized(x.names, locale, x.slug);
  // 正式名称 + 各语言名 + 读音，不分大小写、包含即命中；名字以检索词开头的排前面
  const needle = q.trim().toLowerCase();
  const hits = useMemo(() => {
    if (!needle) return [];
    const scored = terms
      .map((x) => {
        const hay = [x.slug, ...Object.values(x.names ?? {}), x.reading ?? ""].map((s) => s.toLowerCase());
        if (!hay.some((h) => h.includes(needle))) return null;
        return { x, rank: hay.some((h) => h.startsWith(needle)) ? 0 : 1 };
      })
      .filter((v): v is { x: TermSummary; rank: number } => v != null);
    return scored.sort((a, b) => a.rank - b.rank || nameOf(a.x).localeCompare(nameOf(b.x), locale)).map((v) => v.x);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [needle, terms, locale]);

  return (
    <div className="grid gap-4">
      <label className="flex items-center gap-3 rounded-full border border-[color-mix(in_oklab,var(--color-accent-ink)_55%,transparent)] bg-tile px-5 py-3 focus-within:border-accent-ink">
        <Search size={17} className="text-muted" />
        <input value={q} onChange={(e) => setQ(e.target.value)}
               placeholder={t("searchPlaceholder", { n: terms.length })}
               className="min-w-0 flex-1 bg-transparent text-[0.95rem] outline-none placeholder:text-muted" />
        {q && <button type="button" onClick={() => setQ("")} aria-label={t("clear")} className="text-muted hover:text-ink"><X size={16} /></button>}
      </label>

      {needle ? (
        <section className="card overflow-hidden py-1.5">
          <div className="px-5 py-2 text-[0.78rem] text-muted">{t("hits", { n: hits.length })}</div>
          <div className="divide-y divide-line-2">
            {hits.slice(0, 200).map((x) => {
              const name = nameOf(x);
              return <TermRow key={x.id} href={`/banks/${slug}/glossary/${x.id}`} name={name} sub={termSub(x, name)}
                              count={x.questionCount > 0 ? t("inQuestions", { n: x.questionCount }) : undefined} />;
            })}
          </div>
          {hits.length === 0 && <p className="px-5 pb-4 text-sm text-muted">{t("noHit")}</p>}
        </section>
      ) : children}
    </div>
  );
}
