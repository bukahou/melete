import { notFound } from "next/navigation";
import { getLocale, getTranslations } from "next-intl/server";
import { ApiError, getBank, summarizeQuestions, type BankDetail, type DrillContext } from "@/lib/api";
import { listParams, setHref } from "@/lib/drillSpec";
import { sessionLabel } from "@/lib/drillLabel";
import { PracticeShell } from "@/components/PracticeShell";
import { EntryHead, EntryRow } from "@/components/EntryList";

export const revalidate = 0;

/**
 * 4.1 历年 / 分组（P9 #10 #18）。
 *
 * 有卷子（session）的题库按卷子列；只有一套的（AWS 汇编）按题号每 groupSize 题一组，
 * 标题写「第 1–100 题」—— ⛔ 不冒充考卷。点进去先到这一组的题目列表（#44），点哪题从哪题开始。
 */
function entries(bank: BankDetail): Array<{ ctx: DrillContext; count?: number }> {
  const sessions = bank.stats.sessions ?? [];
  const named = sessions.filter((s) => s.session !== "");
  if (named.length > 0) {
    return named.map((s) => ({ ctx: { mode: "year", session: s.session }, count: s.questionCount }));
  }
  const all = sessions[0];
  if (!all) return [];
  const size = bank.meta.groupSize ?? 100;
  const out: Array<{ ctx: DrillContext }> = [];
  // 按题号区间切，⚠️ 不按「第几道」切：题号有缺（素材里少几题），区间才与原卷的编号对得上
  for (let from = Math.floor((all.noFrom - 1) / size) * size + 1; from <= all.noTo; from += size) {
    out.push({ ctx: { mode: "year", session: "", noFrom: from, noTo: Math.min(from + size - 1, all.noTo) } });
  }
  return out;
}

export default async function YearPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const [t, dash, locale] = await Promise.all([getTranslations("practice"), getTranslations("dash"), getLocale()]);
  let bank: BankDetail;
  try {
    bank = await getBank(slug);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const rows = entries(bank);
  const sums = await Promise.all(rows.map((r) => summarizeQuestions(slug, listParams(r.ctx))));
  const size = bank.meta.groupSize ?? 100;
  const bySession = rows.some((r) => r.ctx.session);

  return (
    <PracticeShell home={t("home")} title={dash("drillYear")} sub={t("yearSub", { size })}>
      <section>
        <EntryHead title={bySession ? t("groupBySession") : t("groupBySize", { size })} columns />
        {rows.map((r, i) => (
          <EntryRow
            key={i}
            href={setHref(slug, r.ctx)}
            title={r.ctx.session
              ? sessionLabel(bank.meta, r.ctx.session, locale)
              : dash("rangeLabel", { from: r.ctx.noFrom ?? 1, to: r.ctx.noTo ?? 1 })}
            stats={sums[i]}
          />
        ))}
      </section>
    </PracticeShell>
  );
}
