import Link from "next/link";
import { notFound } from "next/navigation";
import { ChevronRight } from "lucide-react";
import { getLocale, getTranslations } from "next-intl/server";
import { ApiError, getBank, getTerm, type TermDetail } from "@/lib/api";
import { localized } from "@/lib/claims";
import { sessionLabel } from "@/lib/drillLabel";

export const revalidate = 0;

/**
 * 术语详情（参照 it-pass 的用語詳細）：读音 → 名称 → 释义 → 分组 → 出题历史。
 * ⭐ 出题历史是本页的价值所在 —— 「这个词在哪些题考过」，点进去看原题。
 */
export default async function TermPage({ params }: { params: Promise<{ slug: string; id: string }> }) {
  const { slug, id } = await params;
  const [t, locale] = await Promise.all([getTranslations("glossary"), getLocale()]);
  let term: TermDetail;
  try {
    term = await getTerm(Number(id));
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  if (term.bankSlug !== slug) notFound();
  const bank = await getBank(slug);
  const name = localized(term.names, locale, term.slug);
  const sub = term.reading ?? (name !== term.slug ? term.slug : undefined);
  const otherNames = Object.entries(term.names).filter(([l, v]) => l !== locale && v !== name && v !== sub);

  return (
    <div className="mx-auto grid max-w-3xl gap-4 pt-2">
      <nav className="flex items-center gap-2 text-[0.82rem] text-muted">
        <Link href="/" className="hover:text-ink">{t("home")}</Link>
        <span className="opacity-50">›</span>
        <Link href={`/banks/${slug}/glossary`} className="hover:text-ink">{t("title")}</Link>
        <span className="opacity-50">›</span>
        <span className="truncate text-ink">{name}</span>
      </nav>

      <section className="card px-6 py-5">
        {sub && <p className="text-[0.8rem] text-muted">{sub}</p>}
        <h1 className="display mt-0.5 text-[1.5rem]">{name}</h1>
        {otherNames.length > 0 && (
          <p className="mt-1 text-[0.8rem] text-muted">{otherNames.map(([, v]) => v).join(" · ")}</p>
        )}
        <div className="my-4 h-px bg-line" />
        <p className="text-[0.95rem] leading-[1.9]">{localized(term.definition, locale)}</p>
        <dl className="mt-5 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-[0.82rem]">
          <dt className="text-muted">{t("category")}</dt>
          <dd>{term.category === "General" ? t("general") : term.category}</dd>
        </dl>
      </section>

      <section className="card overflow-hidden pt-4">
        <div className="mb-2 flex items-center gap-3 px-5">
          <h2 className="shrink-0 text-[0.95rem] font-semibold">{t("history")}</h2>
          <span className="h-px flex-1 bg-line" />
          <span className="text-[0.78rem] text-muted">{t("inQuestions", { n: term.questions.length })}</span>
        </div>
        <div className="divide-y divide-line-2 border-t border-line-2">
          {term.questions.map((q) => (
            <Link key={q.id} href={`/questions/${q.id}`}
                  className="group flex items-center gap-4 px-5 py-3 transition-colors hover:bg-accent-soft">
              <span className="w-24 shrink-0 text-[0.82rem] sm:w-auto sm:min-w-24">
                {q.session && <span className="block text-[0.7rem] text-muted sm:whitespace-nowrap">{sessionLabel(bank.meta, q.session, locale)}</span>}
                <span className="font-semibold tabular-nums">#{q.externalNo}</span>
              </span>
              <span className="min-w-0 flex-1 truncate text-[0.86rem] text-muted">{q.stem.replace(/\s+/g, " ")}</span>
              <ChevronRight size={15} className="shrink-0 text-muted group-hover:text-accent-ink" />
            </Link>
          ))}
        </div>
      </section>
    </div>
  );
}
