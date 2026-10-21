import Link from "next/link";
import { notFound } from "next/navigation";
import { ChevronRight } from "lucide-react";
import { getLocale, getTranslations } from "next-intl/server";
import { ApiError, getBank, getTerm, listTerms, type BankDetail, type TermSummary } from "@/lib/api";
import { localized, tagTypeLabel } from "@/lib/claims";
import { arrangeCategory, categoryHref, categoryLabel, glossarySections, termSub } from "@/lib/glossary";
import { GlossarySearch } from "@/components/GlossaryBrowser";
import { TermRow } from "@/components/TermRow";
import { Page } from "@/components/Page";

export const revalidate = 0;

const ALL = "*";

/**
 * 用语集：目录（大类 › 分类）与分类页（?c=）同一个路由 —— 结构见 lib/glossary.ts。
 * 版式参照 it-pass「用語集」：检索框 + 大类标题 + 分类行（N 語）。
 */
export default async function GlossaryPage({
  params, searchParams,
}: { params: Promise<{ slug: string }>; searchParams: Promise<{ q?: string; c?: string; more?: string }> }) {
  const { slug } = await params;
  const { q, c, more: moreParam } = await searchParams;
  const more = moreParam === "1";
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
  // ---- 分类页 ----
  // 排法见 lib/glossary.ts 的 arrangeCategory：主条目置顶（带释义）→ 常考的按出题数 → 只出现 1 次的折叠
  if (c) {
    const label = c === ALL ? t("allTerms") : categoryLabel(c, t("general"));
    const nameOf = (x: TermSummary) => localized(x.names, locale, x.slug);
    // 「全部术语」不是一个分类，没有主条目
    const pool = terms.filter((x) => c === ALL || x.category === c).map((x) => (c === ALL ? { ...x, lead: false } : x));
    if (pool.length === 0) notFound();
    const { lead, main, rest } = arrangeCategory(pool, nameOf, locale);
    const leadDetail = lead ? await getTerm(lead.id) : undefined;
    const row = (x: TermSummary) => {
      const name = nameOf(x);
      return <TermRow key={x.id} href={`${root}/${x.id}`} name={name} sub={termSub(x, name)}
                      count={x.questionCount > 0 ? t("inQuestions", { n: x.questionCount }) : undefined} />;
    };
    return (
      <Page title={label} crumbs={[{ href: "/", label: t("home") }, { href: root, label: t("title") }, { label }]}
            sub={`${code} · ${t("count", { n: pool.length })}`}>
        {lead && leadDetail && (
          <section className="px-4 pb-6">
            <h2 className="text-[1.1rem] font-semibold">{nameOf(lead)}</h2>
            {termSub(lead, nameOf(lead)) && <p className="mt-0.5 text-[0.78rem] text-muted">{termSub(lead, nameOf(lead))}</p>}
            <p className="mt-3 text-[0.95rem] leading-[1.9]">{localized(leadDetail.definition, locale)}</p>
            <Link href={`${root}/${lead.id}`} className="mt-3 inline-flex items-center gap-1 text-[0.85rem] text-accent-ink hover:underline">
              {t("leadMore", { n: lead.questionCount })}<ChevronRight size={14} />
            </Link>
          </section>
        )}
        {/* 细线行直接放在纸上（#41）；第一行补一条顶线，与上面的释义分开 */}
        {main.length > 0 && <section className="[&>a:first-child]:border-t [&>a:first-child]:border-line-2">{main.map(row)}</section>}
        {/* ⚠️ 展开状态进 URL（&more=1），⛔ 不用 <details>：展开后点进术语再返回，内存里的展开态就丢了 */}
        {rest.length > 0 && (
          <section className="mt-6">
            <Link href={more ? categoryHref(slug, c) : `${categoryHref(slug, c)}&more=1`} scroll={false} replace
                  className="band flex items-center gap-3 font-normal transition-colors hover:bg-accent-soft">
              <ChevronRight size={15} className={`text-muted transition-transform ${more ? "rotate-90" : ""}`} />
              <span className="flex-1 text-[0.92rem]">{t("rest", { n: rest.length })}</span>
              <span className="text-[0.72rem] text-muted">{t("restNote")}</span>
            </Link>
            {more && rest.map(row)}
          </section>
        )}
      </Page>
    );
  }

  // ---- 目录 ----
  const sections = [
    { heading: t("all"), rows: [{ category: ALL, label: t("allTerms"), count: terms.length }] },
    ...glossarySections(bank.meta, terms, locale, { general: t("general"), other: t("other") }),
  ];
  return (
    // 用语集是侧栏直达的顶级页：顶栏只有标题、没有返回（同 it-pass「用語集」）
    <Page title={t("title")} sub={`${code} · ${t("summary", { n: terms.length, axis: tagTypeLabel(bank.meta, "topic", locale) })}`}>
      {terms.length === 0 ? (
        <p className="px-4 py-10 text-center text-sm text-muted">{t("empty")}</p>
      ) : (
        <GlossarySearch slug={slug} terms={terms} initialQuery={q ?? ""}>
          <div>
            {sections.map((s, i) => (
              <section key={s.heading ?? i} className="[&+&]:mt-6">
                {s.heading && <div className="band flex items-center">{s.heading}</div>}
                {s.rows.map((r) => (
                  <Link key={r.category} href={categoryHref(slug, r.category)}
                        className="mx-2 flex min-h-14 items-center gap-3 border-b border-line-2 pl-5 pr-2 transition-colors hover:bg-accent-soft">
                    <span className="min-w-0 flex-1 truncate text-[0.97rem]">{r.label}</span>
                    <span className="shrink-0 text-[0.84rem] text-muted tabular-nums">{t("count", { n: r.count })}</span>
                  </Link>
                ))}
              </section>
            ))}
          </div>
        </GlossarySearch>
      )}
    </Page>
  );
}
