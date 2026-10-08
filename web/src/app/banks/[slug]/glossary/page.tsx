import Link from "next/link";
import { notFound } from "next/navigation";
import { getLocale, getTranslations } from "next-intl/server";
import { ApiError, getBank, listTerms, type BankDetail, type TermSummary } from "@/lib/api";
import { tagTypeLabel } from "@/lib/claims";
import { GlossaryBrowser, type GlossaryGroup } from "@/components/GlossaryBrowser";

export const revalidate = 0;

/**
 * 用语集目录（P9 第 6 步）。分组顺序在这里定：
 *   IPA —— 考纲树（分野 › 大分類 › 中分類），标题用大分類，与 it-pass 的用語集目录一致
 *   AWS —— 服务名字母序，「General」（跨服务的通用概念）放最前
 */
function groupsOf(bank: BankDetail, terms: TermSummary[], general: string): GlossaryGroup[] {
  const present = new Set(terms.map((t) => t.category));
  const tree = bank.meta.topicTree ?? [];
  if (tree.length > 0) {
    const out: GlossaryGroup[] = [];
    for (const d of tree) for (const g of d.groups) for (const c of g.topics) out.push({ heading: g.name, category: c, label: c });
    // 考纲树之外的分组（不该有，但别让它们消失）
    for (const c of present) if (!out.some((g) => g.category === c)) out.push({ category: c, label: c });
    return out;
  }
  return [...present]
    .sort((a, b) => (a === "General" ? -1 : b === "General" ? 1 : a.localeCompare(b)))
    .map((c) => ({ category: c, label: c === "General" ? general : c }));
}

export default async function GlossaryPage({
  params, searchParams,
}: { params: Promise<{ slug: string }>; searchParams: Promise<{ q?: string }> }) {
  const { slug } = await params;
  const { q } = await searchParams;
  const [t, locale] = await Promise.all([getTranslations("glossary"), getLocale()]);
  let bank: BankDetail, terms: TermSummary[];
  try {
    [bank, terms] = await Promise.all([getBank(slug), listTerms(slug)]);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const code = slug.toUpperCase().replace(/^AWS-/, "");
  return (
    <div className="mx-auto grid max-w-3xl gap-4 pt-2">
      <header className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <nav className="flex w-full items-center gap-2 text-[0.82rem] text-muted">
          <Link href="/" className="hover:text-ink">{t("home")}</Link>
          <span className="opacity-50">›</span>
          <span className="text-ink">{t("title")}</span>
        </nav>
        <h1 className="display text-[1.4rem]">{t("title")}</h1>
        <span className="text-[0.82rem] text-muted">{code} · {t("summary", { n: terms.length, axis: tagTypeLabel(bank.meta, "topic", locale) })}</span>
      </header>
      {terms.length === 0 ? (
        <p className="card px-6 py-10 text-center text-sm text-muted">{t("empty")}</p>
      ) : (
        <GlossaryBrowser slug={slug} terms={terms} groups={groupsOf(bank, terms, t("general"))} initialQuery={q ?? ""} />
      )}
    </div>
  );
}
