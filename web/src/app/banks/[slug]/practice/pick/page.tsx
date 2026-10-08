import Link from "next/link";
import { notFound } from "next/navigation";
import { getLocale, getTranslations } from "next-intl/server";
import { ApiError, getBank, listAxisTags, type BankDetail, type Tag } from "@/lib/api";
import { tagName, tagTypeLabel } from "@/lib/claims";
import { sessionLabel } from "@/lib/drillLabel";
import { PickBrowser, type PickNode } from "@/components/PickBrowser";

export const revalidate = 0;

/**
 * 4.3 自选条件（P9 #12）：状态 ∧ 考纲域（并集）∧ 范围，结果直接列成题目。
 * 交互全在 PickBrowser；这里只准备「有哪些可选」—— 考纲树（带题数）与范围。
 */
const STATUSES = ["all", "wrong", "unseen", "bookmarked", "contested"] as const;

function ranges(bank: BankDetail, rangeLabel: (from: number, to: number) => string, locale: string) {
  const sessions = bank.stats.sessions ?? [];
  const named = sessions.filter((s) => s.session !== "");
  if (named.length > 0) return named.map((s) => ({ value: `s:${s.session}`, label: sessionLabel(bank.meta, s.session, locale) }));
  const all = sessions[0];
  if (!all) return [];
  const size = bank.meta.groupSize ?? 100;
  const out = [];
  for (let from = Math.floor((all.noFrom - 1) / size) * size + 1; from <= all.noTo; from += size) {
    const to = Math.min(from + size - 1, all.noTo);
    out.push({ value: `n:${from}-${to}`, label: rangeLabel(from, to) });
  }
  return out;
}

export default async function PickPage({
  params, searchParams,
}: { params: Promise<{ slug: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { slug } = await params;
  const sp = await searchParams;
  const [t, dash, locale] = await Promise.all([getTranslations("practice"), getTranslations("dash"), getLocale()]);
  let bank: BankDetail, tags: Tag[];
  try {
    [bank, tags] = await Promise.all([getBank(slug), listAxisTags(slug)]);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const topicByValue = new Map(tags.filter((x) => x.type === "topic").map((x) => [x.value, x]));
  const treeByDomain = new Map((bank.meta.topicTree ?? []).map((d) => [d.domain, d.groups]));
  const tree: PickNode[] = tags
    .filter((x) => x.type === "domain")
    .sort((a, b) => a.value.localeCompare(b.value))
    .map((d) => ({
      value: String(d.id),
      label: tagName(d, locale),
      count: d.questionCount ?? 0,
      // ⭐ 一个分组 = 它下面几个中分類，值是逗号串；pickForm 把所有勾选拍平成并集
      children: (treeByDomain.get(d.value) ?? [])
        .map((g) => ({ label: g.name, topics: g.topics.map((v) => topicByValue.get(v)).filter((x): x is Tag => x != null) }))
        .filter((g) => g.topics.length > 0)
        .map((g) => ({
          value: g.topics.map((x) => x.id).join(","),
          label: g.label,
          // 分组题数 = 各中分類之和（IPA 每题只挂一个中分類，和就是并集的大小）
          count: g.topics.reduce((n, x) => n + (x.questionCount ?? 0), 0),
        })),
    }));

  // 把进页时的条件原样交给客户端（刷新 / 后退回来条件还在）
  const initial = new URLSearchParams();
  for (const [k, v] of Object.entries(sp)) for (const x of Array.isArray(v) ? v : v != null ? [v] : []) initial.append(k, x);

  return (
    <div className="mx-auto max-w-3xl pt-2">
      <nav className="mb-3 flex items-center gap-2 text-[0.82rem] text-muted">
        <Link href="/" className="hover:text-ink">{t("home")}</Link>
        <span className="opacity-50">›</span>
        <span className="text-ink">{dash("drillPick")}</span>
      </nav>
      <PickBrowser
        slug={slug}
        initial={initial.toString()}
        statuses={STATUSES.map((s) => ({ value: s, label: dash(`status_${s}`) }))}
        tree={tree}
        ranges={ranges(bank, (from, to) => dash("rangeLabel", { from, to }), locale)}
        labels={{ domain: tagTypeLabel(bank.meta, "domain", locale), range: t("pickRange") }}
      />
    </div>
  );
}
