import Link from "next/link";
import { notFound } from "next/navigation";
import { ChevronRight } from "lucide-react";
import { getLocale, getTranslations } from "next-intl/server";
import { ApiError, getBank, listTerms, type BankDetail, type TermSummary } from "@/lib/api";
import { localized, tagTypeLabel } from "@/lib/claims";
import { categoryHref, categoryLabel, glossarySections, termSub } from "@/lib/glossary";
import { GlossarySearch } from "@/components/GlossaryBrowser";
import { TermRow } from "@/components/TermRow";

export const revalidate = 0;

const ALL = "*";

/**
 * 用语集：目录（大类 › 分类）与分类页（?c=）同一个路由 —— 结构见 lib/glossary.ts。
 * 版式参照 it-pass「用語集」：检索框 + 大类标题 + 分类行（N 語）。
 */
export default async function GlossaryPage({
  params, searchParams,
}: { params: Promise<{ slug: string }>; searchParams: Promise<{ q?: string; c?: string }> }) {
  const { slug } = await params;
  const { q, c } = await searchParams;
  const [t, locale] = await Promise.all([getTranslations("glossary"), getLocale()]);
  let bank: BankDetail, terms: TermSummary[];
  try {
    [bank, terms] = await Promise.all([getBank(slug), listTerms(slug)]);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const code = slug.toUpperCase().replace(/^AWS-/, "");
  const root = `/banks/${slug}/glossary`;
  const crumbs = (here?: string) => (
    <nav className="flex w-full items-center gap-2 text-[0.82rem] text-muted">
      <Link href="/" className="hover:text-ink">{t("home")}</Link>
      <span className="opacity-50">›</span>
      {here ? <Link href={root} className="hover:text-ink">{t("title")}</Link> : <span className="text-ink">{t("title")}</span>}
      {here && <><span className="opacity-50">›</span><span className="truncate text-ink">{here}</span></>}
    </nav>
  );

  // ---- 分类页 ----
  if (c) {
    const label = c === ALL ? t("allTerms") : categoryLabel(c, t("general"));
    const nameOf = (x: TermSummary) => localized(x.names, locale, x.slug);
    const list = terms.filter((x) => c === ALL || x.category === c)
      .map((x) => ({ x, name: nameOf(x) }))
      .sort((a, b) => (a.x.reading ?? a.name).localeCompare(b.x.reading ?? b.name, locale));
    if (list.length === 0) notFound();
    return (
      <div className="mx-auto grid max-w-3xl gap-4 pt-2">
        <header className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          {crumbs(label)}
          <h1 className="display text-[1.4rem]">{label}</h1>
          <span className="text-[0.82rem] text-muted">{code} · {t("count", { n: list.length })}</span>
        </header>
        <section className="card overflow-hidden">
          <div className="divide-y divide-line-2">
            {list.map(({ x, name }) => (
              <TermRow key={x.id} href={`${root}/${x.id}`} name={name} sub={termSub(x, name)}
                       count={x.questionCount > 0 ? t("inQuestions", { n: x.questionCount }) : undefined} />
            ))}
          </div>
        </section>
      </div>
    );
  }

  // ---- 目录 ----
  const sections = [
    { heading: t("all"), rows: [{ category: ALL, label: t("allTerms"), count: terms.length }] },
    ...glossarySections(bank.meta, terms, locale, { general: t("general"), other: t("other") }),
  ];
  return (
    <div className="mx-auto grid max-w-3xl gap-4 pt-2">
      <header className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        {crumbs()}
        <h1 className="display text-[1.4rem]">{t("title")}</h1>
        <span className="text-[0.82rem] text-muted">{code} · {t("summary", { n: terms.length, axis: tagTypeLabel(bank.meta, "topic", locale) })}</span>
      </header>
      {terms.length === 0 ? (
        <p className="card px-6 py-10 text-center text-sm text-muted">{t("empty")}</p>
      ) : (
        <GlossarySearch slug={slug} terms={terms} initialQuery={q ?? ""}>
          <div className="grid gap-4">
            {sections.map((s, i) => (
              <section key={s.heading ?? i} className="grid gap-2">
                {s.heading && <h2 className="px-1 text-[0.95rem] font-semibold">{s.heading}</h2>}
                <div className="card divide-y divide-line-2 overflow-hidden">
                  {s.rows.map((r) => (
                    <Link key={r.category} href={categoryHref(slug, r.category)}
                          className="group flex items-center gap-3 px-5 py-3 transition-colors hover:bg-accent-soft">
                      <span className="min-w-0 flex-1 truncate text-[0.92rem]">{r.label}</span>
                      <span className="shrink-0 text-[0.78rem] text-muted tabular-nums">{t("count", { n: r.count })}</span>
                      <ChevronRight size={15} className="shrink-0 text-muted group-hover:text-accent-ink" />
                    </Link>
                  ))}
                </div>
              </section>
            ))}
          </div>
        </GlossarySearch>
      )}
    </div>
  );
}
