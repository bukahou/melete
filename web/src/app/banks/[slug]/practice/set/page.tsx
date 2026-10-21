import Link from "next/link";
import { notFound } from "next/navigation";
import { Play } from "lucide-react";
import { getLocale, getTranslations } from "next-intl/server";
import {
  ApiError, getBank, listAxisTags, listQuestions,
  type BankDetail, type DrillContext, type QuestionSummary, type Tag,
} from "@/lib/api";
import { tagName, tagTypeLabel } from "@/lib/claims";
import { contextFromSearch, drillHref, listParams } from "@/lib/drillSpec";
import { describeContext } from "@/lib/drillLabel";
import { bandStats, bandsByDomain, questionLabel } from "@/lib/questionSet";
import { Page } from "@/components/Page";
import { QuestionRow } from "@/components/QuestionRow";

export const revalidate = 0;

/** 后端一页最多 100 题（契约 listQuestions.limit 的上限）。 */
const PAGE = 100;

/**
 * 题目列表（P9 #44，2026-10-21 用户裁定，照 it-pass「令和8年度」那一页）：
 * 「历年 / 分组」「按考纲域」点进来先到这里 —— 看这一组每题考什么、做得怎样，点哪题从哪题开始。
 *
 * ⭐ 列表第 i 行 = 刷题页 offset i：同一个出处（DrillContext）、同一个 listParams、同一个排序（后端 orderBy），
 *   ⛔ 这里不自己排序、不自己过滤 —— 否则点第 5 行进去做的是另一道题，而且不报错。
 * 整组一次取完（分段要看整组；AWS 一个考纲域最多三百来题，分几次取）。
 */
export default async function SetPage({
  params, searchParams,
}: {
  params: Promise<{ slug: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { slug } = await params;
  const ctx: DrillContext = contextFromSearch(await searchParams);
  // 只有这两种入口有列表页；其它出处（随机、自选条件…）各有各的页
  if (ctx.mode !== "year" && ctx.mode !== "domain") notFound();

  const [t, dash, modeT, locale] = await Promise.all([
    getTranslations("practice"), getTranslations("dash"), getTranslations("mode"), getLocale(),
  ]);
  let bank: BankDetail, tags: Tag[];
  try {
    [bank, tags] = await Promise.all([getBank(slug), listAxisTags(slug)]);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  const listP = listParams(ctx);
  const items: QuestionSummary[] = [];
  for (let offset = 0; ; offset += PAGE) {
    const page = await listQuestions(slug, { ...listP, limit: PAGE, offset, fresh: true });
    items.push(...page.items);
    if (page.items.length === 0 || items.length >= page.total) break;
  }
  if (items.length === 0) notFound();

  const byId = new Map(tags.map((x) => [x.id, x]));
  const topicName = (id: number) => {
    const tag = byId.get(id);
    return tag?.type === "topic" ? tagName(tag, locale) : undefined;
  };
  const rows = items.map((it, i) => ({
    it, i,
    domain: it.tagIds.find((id) => byId.get(id)?.type === "domain"),
    label: questionLabel(it.tagIds, topicName, it.stem),
  }));
  const bands = bandsByDomain(rows, (r) => r.domain);
  const statText = (lasts: Array<boolean | null | undefined>) => {
    const s = bandStats(lasts);
    return t("setBandStat", { answered: s.answered, total: s.total, rate: s.rate == null ? "—" : `${s.rate}%` });
  };

  const label = describeContext(ctx, {
    meta: bank.meta, tags, locale,
    t: (k, v) => dash(k as never, v as never),
    mode: modeT as never,
  });
  const entry = ctx.mode === "year"
    ? { href: `/banks/${slug}/practice/year`, label: dash("drillYear") }
    : { href: `/banks/${slug}/practice/domain`, label: dash("drillDomain", { domain: tagTypeLabel(bank.meta, "domain", locale) }) };

  return (
    <Page title={label} crumbs={[{ href: "/", label: t("home") }, entry, { label }]}>
      {bands.map((b, bi) => (
        <section key={bi} className="[&+&]:mt-6">
          {/* 白条：分段名（考纲域；不分段时写整组多少题）+ 这一段的已答 / 掌握率（同 it-pass「ストラテジ　正答率：88%」） */}
          <div className="band flex items-center gap-3">
            <span className="min-w-0 flex-1 truncate">
              {b.domain != null && byId.get(b.domain) ? tagName(byId.get(b.domain)!, locale) : t("setAll", { n: b.rows.length })}
            </span>
            <span className="shrink-0 text-[0.78rem] font-normal text-muted tabular-nums">
              {statText(b.rows.map((r) => r.it.lastCorrect))}
            </span>
          </div>
          <ol>
            {b.rows.map((r) => (
              <QuestionRow key={r.it.id} href={drillHref(slug, ctx, { i: r.i })} id={r.it.id} no={r.it.externalNo}
                           label={r.label} last={r.it.lastCorrect ?? null} bookmarked={r.it.bookmarked ?? false} />
            ))}
          </ol>
        </section>
      ))}

      {/* 从第 1 题开始（贴底，手机上让开底部标签栏）—— 点某一行 = 从那题开始 */}
      <div className="sticky bottom-20 z-10 mt-6 flex items-center gap-4 rounded-[0.875rem] bg-tile px-5 py-3 shadow-[0_-2px_14px_rgba(0,0,0,0.08)] md:bottom-4">
        {/* 手机上只放按钮：统计在段头的白条上已经有了，挤在一行里会折成两截 */}
        <span className="hidden min-w-0 flex-1 text-[0.85rem] tabular-nums sm:block">{statText(items.map((it) => it.lastCorrect))}</span>
        <Link href={drillHref(slug, ctx)}
              className="inline-flex w-full shrink-0 items-center justify-center gap-2 rounded-xl px-6 py-2.5 text-sm font-semibold sm:w-auto"
              style={{ background: "var(--color-accent)", color: "var(--color-accent-fg)" }}>
          <Play size={13} fill="currentColor" />{t("pickStartFirst")}
        </Link>
      </div>
    </Page>
  );
}
