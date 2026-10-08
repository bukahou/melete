"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { ChevronRight, Search, X } from "lucide-react";
import { useTranslations } from "next-intl";
import { localized, type TermSummary } from "@/lib/claims";

/**
 * 用语集目录（P9 第 6 步）—— 版式参照 it-pass「用語集」：检索框 + 按分组的术语列表。
 *
 * ⭐ 全部术语一次给到浏览器（目录不带释义，几千条也只有几百 KB），检索在本地即时完成 ——
 *   每敲一个字就出结果，⛔ 不为每次按键打一次后端。
 * 分组顺序由服务端给（IPA 按考纲树的 大分類 › 中分類；AWS 按服务名）。
 */
export type GlossaryGroup = { heading?: string; category: string; label: string };

export function GlossaryBrowser({ slug, terms, groups, initialQuery }: {
  slug: string; terms: TermSummary[]; groups: GlossaryGroup[]; initialQuery: string;
}) {
  const t = useTranslations("glossary");
  const locale = t("locale");
  const [q, setQ] = useState(initialQuery);

  const nameOf = (x: TermSummary) => localized(x.names, locale, x.slug);
  const byCategory = useMemo(() => {
    const m = new Map<string, TermSummary[]>();
    for (const x of terms) m.set(x.category, [...(m.get(x.category) ?? []), x]);
    for (const list of m.values()) list.sort((a, b) => nameOf(a).localeCompare(nameOf(b), locale));
    return m;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [terms, locale]);

  // 检索：正式名称 + 各语言名 + 读音，不分大小写、包含即命中；名字以检索词开头的排前面
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

  const Row = ({ x }: { x: TermSummary }) => (
    <Link href={`/banks/${slug}/glossary/${x.id}`}
          className="group flex items-center gap-3 px-5 py-2.5 transition-colors hover:bg-accent-soft">
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[0.92rem]">{nameOf(x)}</span>
        {(x.reading || nameOf(x) !== x.slug) && (
          <span className="block truncate text-[0.72rem] text-muted">{x.reading ?? x.slug}</span>
        )}
      </span>
      {x.questionCount > 0 && <span className="shrink-0 text-[0.72rem] text-muted tabular-nums">{t("inQuestions", { n: x.questionCount })}</span>}
      <ChevronRight size={15} className="shrink-0 text-muted group-hover:text-accent-ink" />
    </Link>
  );

  return (
    <div className="grid gap-4">
      <label className="flex items-center gap-3 rounded-full border border-[color-mix(in_oklab,var(--color-accent-ink)_55%,transparent)] bg-tile px-5 py-3 focus-within:border-accent-ink">
        <Search size={17} className="text-muted" />
        <input value={q} onChange={(e) => setQ(e.target.value)} autoFocus
               placeholder={t("searchPlaceholder", { n: terms.length })}
               className="min-w-0 flex-1 bg-transparent text-[0.95rem] outline-none placeholder:text-muted" />
        {q && <button type="button" onClick={() => setQ("")} aria-label={t("clear")} className="text-muted hover:text-ink"><X size={16} /></button>}
      </label>

      {needle ? (
        <section className="card overflow-hidden py-1.5">
          <div className="px-5 py-2 text-[0.78rem] text-muted">{t("hits", { n: hits.length })}</div>
          <div className="divide-y divide-line-2">{hits.slice(0, 200).map((x) => <Row key={x.id} x={x} />)}</div>
          {hits.length === 0 && <p className="px-5 pb-4 text-sm text-muted">{t("noHit")}</p>}
        </section>
      ) : (
        groups.map((g, i) => {
          const list = byCategory.get(g.category) ?? [];
          if (list.length === 0) return null;
          return (
            <section key={g.category} className="grid gap-2">
              {g.heading && (i === 0 || groups[i - 1].heading !== g.heading) && (
                <h2 className="px-1 pt-2 text-[0.95rem] font-semibold">{g.heading}</h2>
              )}
              <details className="card overflow-hidden [&[open]_.chev]:rotate-90">
                <summary className="flex cursor-pointer list-none items-center gap-3 px-5 py-3 hover:bg-accent-soft">
                  <ChevronRight size={15} className="chev text-muted transition-transform" />
                  <span className="flex-1 font-semibold">{g.label}</span>
                  <span className="text-[0.78rem] text-muted tabular-nums">{t("count", { n: list.length })}</span>
                </summary>
                <div className="divide-y divide-line-2 border-t border-line-2">{list.map((x) => <Row key={x.id} x={x} />)}</div>
              </details>
            </section>
          );
        })
      )}
    </div>
  );
}
