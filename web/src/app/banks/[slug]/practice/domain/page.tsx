import { notFound } from "next/navigation";
import { getLocale, getTranslations } from "next-intl/server";
import {
  ApiError, getBank, getMyTagStats, listAxisTags, summarizeQuestions,
  type BankDetail, type DrillContext, type Tag,
} from "@/lib/api";
import { tagName, tagTypeLabel } from "@/lib/claims";
import { drillHref, listParams } from "@/lib/drillSpec";
import { EntryRow, PracticeShell } from "@/components/PracticeShell";

export const revalidate = 0;

/**
 * 4.2 按考纲域（P9 #11 #17 #18）。
 *
 * 有考纲树（bank.meta.topicTree，IPA）就按 分野 › 大分類 › 中分類 三层列；
 * 没有（AWS）就平铺考纲域。选大分類 = 它下面几个中分類的并集（#17：大分類不是标签）。
 * 进去后按题号全部出（#18）。
 */
type Row = { ctx: DrillContext; title: string; indent: 0 | 1 | 2; count?: number; tagId?: number };

function rows(bank: BankDetail, tags: Tag[], locale: string): Row[] {
  const domains = tags.filter((t) => t.type === "domain").sort((a, b) => a.value.localeCompare(b.value));
  const topicByValue = new Map(tags.filter((t) => t.type === "topic").map((t) => [t.value, t]));
  const tree = new Map((bank.meta.topicTree ?? []).map((d) => [d.domain, d.groups]));
  const out: Row[] = [];
  for (const d of domains) {
    out.push({ ctx: { mode: "domain", tagIds: [d.id] }, title: tagName(d, locale), indent: 0, count: d.questionCount, tagId: d.id });
    for (const g of tree.get(d.value) ?? []) {
      // ⚠️ 考纲树列的是考纲，可能含题库里没有题的中分類 —— 那样的 topic 库里没有 tag，跳过
      const topics = g.topics.map((v) => topicByValue.get(v)).filter((x): x is Tag => x != null);
      if (topics.length === 0) continue;
      out.push({ ctx: { mode: "domain", tagIds: topics.map((x) => x.id) }, title: g.name, indent: 1 });
      for (const tp of topics) {
        out.push({ ctx: { mode: "domain", tagIds: [tp.id] }, title: tagName(tp, locale), indent: 2, count: tp.questionCount, tagId: tp.id });
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

  return (
    <PracticeShell home={t("home")} title={dash("drillDomain", { domain: domainLabel })} sub={t("domainSub", { domain: domainLabel })}>
      <section className="rounded-lg border border-line bg-raise">
        {list.map((r, i) => {
          const st = r.tagId != null ? statById.get(r.tagId) : undefined;
          const g = groupSums[i];
          const total = g ? g.total : r.count ?? 0;
          const answered = g ? g.answered : st?.total ?? 0;
          const rate = g ? (g.answered ? Math.round((g.correct / g.answered) * 100) : null) : st && st.total > 0 ? Math.round(st.rate) : null;
          return (
            <EntryRow
              key={i}
              href={drillHref(slug, r.ctx)}
              title={r.title}
              indent={r.indent}
              meta={t("questions", { n: total })}
              stat={answered > 0 && rate != null ? t("rowRate", { answered, rate }) : undefined}
              rate={rate}
            />
          );
        })}
      </section>
    </PracticeShell>
  );
}
