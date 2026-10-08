import Link from "next/link";
import { notFound } from "next/navigation";
import { ChevronDown, ChevronRight, Play } from "lucide-react";
import { getLocale, getTranslations } from "next-intl/server";
import { ApiError, getBank, getTerm, listBankTags, type TermDetail } from "@/lib/api";
import { localized } from "@/lib/claims";
import { sessionLabel } from "@/lib/drillLabel";
import { categoryHref, categoryLabel } from "@/lib/glossary";
import { drillHref } from "@/lib/drillSpec";

/** 出题历史一次列多少题（「显示更多」每次再加这么多）。 */
const HISTORY_PAGE = 20;

export const revalidate = 0;

/**
 * 术语详情（参照 it-pass 的用語詳細）：读音 → 名称 → 释义 → 分组 → 出题历史。
 * ⭐ 出题历史是本页的价值所在 —— 「这个词在哪些题考过」，点进去看原题。
 * 面包屑带分类：上一层是这个术语所在的分类页，⛔ 不是目录首页（用户反馈「返回丢失当前位置」）。
 *
 * 出题历史分页（2026-10-08 用户裁定）：主条目按服务标签补完关联后动辄几百题（SAA 的 EC2 有 490 题），
 * 先列 HISTORY_PAGE 题，「显示更多」每次再加一页；已展开到多少记在 URL（?n=），返回不丢位置。
 * 主条目另给「练习这批题」—— 进入现有的专项练习（按这个服务的 topic 标签出题），与学习履历同一个入口。
 */
export default async function TermPage({
  params, searchParams,
}: { params: Promise<{ slug: string; id: string }>; searchParams: Promise<{ n?: string }> }) {
  const { slug, id } = await params;
  const shown = Math.max(HISTORY_PAGE, Number((await searchParams).n) || HISTORY_PAGE);
  const [t, locale] = await Promise.all([getTranslations("glossary"), getLocale()]);
  let term: TermDetail;
  try {
    term = await getTerm(Number(id));
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  if (term.bankSlug !== slug) notFound();
  const [bank, topics] = await Promise.all([getBank(slug), term.lead ? listBankTags(slug, "topic") : Promise.resolve([])]);
  // 主条目 = 分类本身，分类名就是 topic 标签的值
  const topic = topics.find((x) => x.value === term.category);
  const total = term.questions.length;
  const more = Math.min(HISTORY_PAGE, total - shown);
  const name = localized(term.names, locale, term.slug);
  const sub = term.reading ?? (name !== term.slug ? term.slug : undefined);
  const catLabel = categoryLabel(term.category, t("general"));
  const catHref = categoryHref(slug, term.category);
  // 其他语言名：与显示名只差全角 / 半角括号的不算另一个名字（Amazon EC2 的中日名就只差括号）
  const same = (a: string, b?: string) => b != null && a.replace(/（/g, "(").replace(/）/g, ")").replace(/\s+/g, "") === b.replace(/（/g, "(").replace(/）/g, ")").replace(/\s+/g, "");
  const otherNames = Object.entries(term.names).filter(([l, v]) => l !== locale && !same(v, name) && !same(v, sub));

  return (
    <div className="mx-auto grid max-w-3xl gap-4 pt-2">
      <nav className="flex items-center gap-2 text-[0.82rem] text-muted">
        <Link href="/" className="hover:text-ink">{t("home")}</Link>
        <span className="opacity-50">›</span>
        <Link href={`/banks/${slug}/glossary`} className="shrink-0 hover:text-ink">{t("title")}</Link>
        <span className="opacity-50">›</span>
        <Link href={catHref} className="shrink-0 hover:text-ink">{catLabel}</Link>
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
          <dd><Link href={catHref} className="text-accent-ink hover:underline">{catLabel}</Link></dd>
        </dl>
        {topic && (
          <Link href={drillHref(slug, { mode: "domain", tagIds: [topic.id] })}
                className="mt-5 inline-flex items-center gap-2 rounded-full bg-accent px-5 py-2 text-[0.88rem] font-semibold text-accent-fg hover:opacity-90">
            <Play size={14} />{t("practice", { n: topic.questionCount ?? total })}
          </Link>
        )}
      </section>

      <section className="card overflow-hidden pt-4">
        <div className="mb-2 flex items-center gap-3 px-5">
          <h2 className="shrink-0 text-[0.95rem] font-semibold">{t("history")}</h2>
          <span className="h-px flex-1 bg-line" />
          <span className="text-[0.78rem] text-muted">{total > shown ? t("shownOf", { shown, total }) : t("inQuestions", { n: total })}</span>
        </div>
        <div className="divide-y divide-line-2 border-t border-line-2">
          {term.questions.slice(0, shown).map((q) => (
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
        {more > 0 && (
          <Link href={`/banks/${slug}/glossary/${id}?n=${shown + HISTORY_PAGE}`} scroll={false} replace
                className="flex items-center justify-center gap-1 border-t border-line-2 py-3 text-[0.85rem] text-accent-ink hover:bg-accent-soft">
            <ChevronDown size={15} />{t("showMore", { n: more })}
          </Link>
        )}
      </section>
    </div>
  );
}
