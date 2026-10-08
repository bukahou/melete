import { notFound } from "next/navigation";
import { getLocale, getTranslations } from "next-intl/server";
import { ApiError, getBank, listAxisTags, type BankDetail, type Tag } from "@/lib/api";
import { tagName, tagTypeLabel } from "@/lib/claims";
import { sessionLabel } from "@/lib/drillLabel";
import { PracticeShell } from "@/components/PracticeShell";
import { PickForm, type PickDomain } from "@/components/PickForm";

export const revalidate = 0;

/**
 * 4.3 自选条件（P9 #12）：状态 ∧ 考纲域（多选并集）∧ 范围。
 *
 * 仍是 GET 表单交给 start 路由归一成刷题 URL；交互（醒目的选中态 + 实时题数）在 PickForm 里。
 * 为什么不直接 GET 到刷题页：表单的字段形状（复选框里一个分组 = 多个 id、范围下拉 = "s:…/n:a-b"）
 * 与刷题页的 URL 形状不同，⛔ 不该让刷题页认两套 —— drillSpec 只认一种。
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

export default async function PickPage({ params }: { params: Promise<{ slug: string }> }) {
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
  const topicByValue = new Map(tags.filter((x) => x.type === "topic").map((x) => [x.value, x]));
  const tree = new Map((bank.meta.topicTree ?? []).map((d) => [d.domain, d.groups]));
  const domains: PickDomain[] = tags
    .filter((x) => x.type === "domain")
    .sort((a, b) => a.value.localeCompare(b.value))
    .map((d) => ({
      value: String(d.id),
      label: tagName(d, locale),
      // ⭐ 一个分组 = 它下面几个中分類，值是逗号串；pickForm 把所有勾选拍平成并集
      groups: (tree.get(d.value) ?? [])
        .map((g) => ({ label: g.name, ids: g.topics.map((v) => topicByValue.get(v)?.id).filter((x): x is number => x != null) }))
        .filter((g) => g.ids.length > 0)
        .map((g) => ({ value: g.ids.join(","), label: g.label })),
    }));

  return (
    <PracticeShell home={t("home")} title={dash("drillPick")} sub={t("pickSub", { domain: domainLabel })}>
      <PickForm
        slug={slug}
        statuses={STATUSES.map((s) => ({ value: s, label: dash(`status_${s}`) }))}
        domains={domains}
        ranges={ranges(bank, (from, to) => dash("rangeLabel", { from, to }), locale)}
        labels={{
          status: t("pickStatus"), tags: t("pickTags", { domain: domainLabel }), range: t("pickRange"),
          rangeAll: t("pickRangeAll"), bookmarkNote: t("pickBookmarkNote"),
        }}
      />
    </PracticeShell>
  );
}
