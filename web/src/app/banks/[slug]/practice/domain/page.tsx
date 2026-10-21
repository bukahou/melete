import { notFound } from "next/navigation";
import { getLocale, getTranslations } from "next-intl/server";
import {
  ApiError, getBank, getMyTagStats, listAxisTags, summarizeQuestions,
  type BankDetail, type DrillContext, type Tag,
} from "@/lib/api";
import { tagName, tagTypeLabel } from "@/lib/claims";
import { drillHref, listParams } from "@/lib/drillSpec";
import { PracticeShell } from "@/components/PracticeShell";
import { EntryColumns, EntryHead, EntryRow, type EntryStats } from "@/components/EntryList";

export const revalidate = 0;

/**
 * 4.2 按考纲域（P9 #11 #17 #18）。
 *
 * 有考纲树（bank.meta.topicTree，IPA）就按 分野 › 大分類 › 中分類 三层列；
 * 没有（AWS）就平铺考纲域。选大分類 = 它下面几个中分類的并集（#17：大分類不是标签）。
 * 进去后按题号全部出（#18）。
 */
type Row = { ctx: DrillContext; title: string; level: 0 | 1 | 2; count?: number; tagId?: number };

function rows(bank: BankDetail, tags: Tag[], locale: string): Row[] {
  const domains = tags.filter((t) => t.type === "domain").sort((a, b) => a.value.localeCompare(b.value));
  const topicByValue = new Map(tags.filter((t) => t.type === "topic").map((t) => [t.value, t]));
  const tree = new Map((bank.meta.topicTree ?? []).map((d) => [d.domain, d.groups]));
  const out: Row[] = [];
  for (const d of domains) {
    out.push({ ctx: { mode: "domain", tagIds: [d.id] }, title: tagName(d, locale), level: 0, count: d.questionCount, tagId: d.id });
    for (const g of tree.get(d.value) ?? []) {
      // ⚠️ 考纲树列的是考纲，可能含题库里没有题的中分類 —— 那样的 topic 库里没有 tag，跳过
      const topics = g.topics.map((v) => topicByValue.get(v)).filter((x): x is Tag => x != null);
      if (topics.length === 0) continue;
      out.push({ ctx: { mode: "domain", tagIds: topics.map((x) => x.id) }, title: g.name, level: 1 });
      for (const tp of topics) {
        out.push({ ctx: { mode: "domain", tagIds: [tp.id] }, title: tagName(tp, locale), level: 2, count: tp.questionCount, tagId: tp.id });
      }
    }
  }
  return out;
}

export default async function DomainPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const [t, dash, locale] = await Promise.all([getTranslations("practice"), getTranslations("dash"), getLocale()]);
  let bank: BankDetail, tags: Tag[];
  try {
    [bank, tags] = await Promise.all([getBank(slug), listAxisTags(slug)]);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const domainLabel = tagTypeLabel(bank.meta, "domain", locale);
  const list = rows(bank, tags, locale);
  const [domainStats, topicStats] = await Promise.all([
    getMyTagStats("domain", 1, slug),
    getMyTagStats("topic", 1, slug),
  ]);
  const statById = new Map([...domainStats, ...topicStats].map((s) => [s.tagId, s]));
  // 分组（大分類）不是标签，没有现成统计 —— 逐个问小结（IPA 9 个，量小）
  const groupSums = await Promise.all(
    list.map((r) => (r.tagId == null ? summarizeQuestions(slug, listParams(r.ctx)) : null)),
  );
  // 标签行用我的标签统计（做过的题数 / 其中最近一次答对的），分组行用小结 —— 同一个口径（#9 掌握率）
  const statsOf = (r: Row, i: number): EntryStats => {
    const g = groupSums[i];
    if (g) return g;
    const st = r.tagId != null ? statById.get(r.tagId) : undefined;
    return { total: r.count ?? 0, answered: st?.total ?? 0, correct: st?.correct ?? 0 };
  };
  // 有考纲树（IPA）：分野 = 白条（整条能点），列名另起一行；没有（AWS）：一条白条兼作列头，考纲域平铺成行
  const tree = list.some((r) => r.level > 0);

  return (
    <PracticeShell home={t("home")} title={dash("drillDomain", { domain: domainLabel })} sub={t("domainSub", { domain: domainLabel })}>
      <section className="card overflow-hidden pb-2">
        {tree ? <EntryColumns /> : <EntryHead title={domainLabel} columns />}
        {list.map((r, i) => tree && r.level === 0
          ? <EntryHead key={i} title={r.title} href={drillHref(slug, r.ctx)} stats={statsOf(r, i)} />
          : <EntryRow key={i} href={drillHref(slug, r.ctx)} title={r.title} stats={statsOf(r, i)} level={r.level === 2 ? 2 : 1} />)}
      </section>
    </PracticeShell>
  );
}
