import Link from "next/link";
import { notFound } from "next/navigation";
import { Settings2 } from "lucide-react";
import {
  ApiError,
  getBank,
  getMyProgress,
  getMyTagStats,
  listBankTags,
  summarizeQuestions,
  type BankDetail,
  type Tag,
  type TagStat,
} from "@/lib/api";
import { tagName, tagTypeLabel, tagWeight } from "@/lib/claims";
import { drillHref, listParams } from "@/lib/drillSpec";
import type { DrillContext } from "@/lib/claims";
import { getLocale, getTranslations } from "next-intl/server";
import { Page, Section } from "@/components/Page";

export const revalidate = 0;

/**
 * 学习履历 · 概况（P9 #6，2026-10-08 改版第一步）。
 *
 * ⭐ 用户反馈旧版「很乱，排版很难看」。旧版是 P9 之前的「学习台」，三件事挤在一页：
 *   继续学习（两条轨道，与首页的单一游标重复）· 按我的状态（该复习 / 不确定已失效，其余在自选条件里更好用）·
 *   考纲域 + 服务两张表。只有最后一块属于「学习履历」—— 留下它，其余删掉。
 *
 * 回答一个问题：**我哪里不会**。三层，从粗到细：
 *   全体（合格判断的同一个口径）→ 每个考纲域（IPA 再展开大分類）→ 知识对象（弱的在前）
 * 每一行都能点 —— 点进去就练那一块（4.2 的同一个入口）。
 *
 * 第二步（it-pass 的「解答数 / 正答率」按天图表）上线时，这里加左侧子页导航。
 */

type T = Awaited<ReturnType<typeof getTranslations<"history">>>;
const pct = (n: number, d: number) => (d > 0 ? Math.round((n / d) * 100) : null);

/** 一根尺：掌握率。line 给了就画一道竖线（只有「全体」画 —— P9 #7 不做分领域的合格判断）。 */
function Meter({ rate, line }: { rate: number | null; line?: number }) {
  return (
    <div className="relative h-1.5 rounded-full bg-line-2">
      {rate != null && <span className="absolute inset-y-0 left-0 rounded-full bg-accent-ink" style={{ width: `${rate}%` }} />}
      {line != null && <span className="absolute -bottom-1 -top-1 w-[2px] bg-ink" style={{ left: `${line}%` }} />}
    </div>
  );
}

/** 一行：名字 · 已答 / 题数 · 掌握率 + 尺。整行可点，点了去练这一块。
 *  ⭐ 纸面上的细线行（#41 #42）：名字不加粗、⛔ 不放箭头；每行自己画底线 ——
 *  ⛔ 不用外层 divide-y + last:border-b-0 那一套：考纲域行包在各自的组里，「组里最后一个」会单独没线。
 *  尺（掌握率）留着：这一页是「我哪里不会」的分析页，尺就是答案；入口列表（#42）去掉的是进度条。 */
function Row({ href, title, sub, done, total, rate, level = 0, t }: {
  href: string; title: string; sub?: string; done: number; total: number; rate: number | null; level?: 0 | 1; t: T;
}) {
  // ⚠️ 手机上「已答」挪到名字下面：四列挤在 390px 里，名字被截成「设计…」，看不出是哪一块
  const doneOf = t("doneOf", { done, total });
  return (
    <Link href={href}
          className={`mx-2 flex min-h-14 items-center gap-4 border-b border-line-2 py-2 pr-2 transition-colors hover:bg-accent-soft ${level ? "pl-9" : "pl-5"}`}>
      <span className="min-w-0 flex-1">
        <span className={`block truncate ${level ? "text-[0.89rem]" : "text-[0.97rem]"}`}>{title}</span>
        {/* 第二行：桌面只放副标题（考纲权重）；手机再加上「已答」。没有副标题时桌面上不占这一行 */}
        <span className={`block text-[0.72rem] text-muted tabular-nums ${sub ? "" : "sm:hidden"}`}>
          {sub}
          <span className="sm:hidden">{sub ? " · " : ""}{doneOf}</span>
        </span>
      </span>
      <span className="hidden w-[5.5rem] shrink-0 text-right text-[0.78rem] text-muted tabular-nums sm:block">{doneOf}</span>
      <span className="grid w-[5.5rem] shrink-0 gap-1 sm:w-[6.5rem]">
        <span className="text-right text-[0.88rem] font-medium tabular-nums">{rate != null ? `${rate}%` : <span className="text-muted">—</span>}</span>
        <Meter rate={rate} />
      </span>
    </Link>
  );
}

const statOf = (stats: TagStat[]) => new Map(stats.map((s) => [s.tagId, s]));
const rateOf = (s?: TagStat) => (s && s.total > 0 ? Math.round(s.rate) : null);

export default async function HistoryOverviewPage({
  params, searchParams,
}: { params: Promise<{ slug: string }>; searchParams: Promise<{ all?: string }> }) {
  const { slug } = await params;
  const sp = await searchParams;
  const [t, locale] = await Promise.all([getTranslations("history"), getLocale()]);
  let bank: BankDetail;
  try {
    bank = await getBank(slug);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const meta = bank.meta;
  const [progress, domainTags, topicTags, domainStats, topicStats] = await Promise.all([
    getMyProgress(slug),
    listBankTags(slug, "domain"),
    listBankTags(slug, "topic"),
    getMyTagStats("domain", 1, slug),
    getMyTagStats("topic", 1, slug),
  ]);
  const byDomain = statOf(domainStats);
  const byTopic = statOf(topicStats);
  const domainLabel = tagTypeLabel(meta, "domain", locale);
  const topicLabel = tagTypeLabel(meta, "topic", locale);
  const total = bank.stats.questionCount;
  const mastery = pct(progress.correctCount, progress.seenCount);
  const practice = pct(progress.attemptCorrectCount, progress.attemptCount);
  const href = (tagIds: number[]) => drillHref(slug, { mode: "domain", tagIds } satisfies DrillContext);

  // IPA：考纲域下再展开大分類（分组不是标签，没有现成统计 —— 逐组问一次小结，量小）
  const topicByValue = new Map(topicTags.map((x) => [x.value, x]));
  const groupsOf = (d: Tag) =>
    ((meta.topicTree ?? []).find((x) => x.domain === d.value)?.groups ?? [])
      .map((g) => ({ name: g.name, ids: g.topics.map((v) => topicByValue.get(v)?.id).filter((x): x is number => x != null) }))
      .filter((g) => g.ids.length > 0);
  const domains = [...domainTags].sort((a, b) => a.value.localeCompare(b.value));
  const groupSums = await Promise.all(
    domains.map((d) => Promise.all(groupsOf(d).map(async (g) => ({ ...g, sum: await summarizeQuestions(slug, listParams({ mode: "domain", tagIds: g.ids })) })))),
  );

  // 知识对象：做过的按掌握率升序（弱的在前），没碰过的按题量降序排在后面
  const topics = [...topicTags].sort((a, b) => {
    const ra = rateOf(byTopic.get(a.id)), rb = rateOf(byTopic.get(b.id));
    if (ra != null && rb != null) return ra - rb;
    if (ra != null) return -1;
    if (rb != null) return 1;
    return (b.questionCount ?? 0) - (a.questionCount ?? 0);
  });
  const showAll = sp.all === "1";
  const topicRows = showAll ? topics : topics.slice(0, 10);
  const code = slug.toUpperCase().replace(/^AWS-/, "");

  return (
    // 学习履历是侧栏直达的顶级页：顶栏只有标题（没有返回），右侧放「换题库」
    <Page title={t("title")} width="wide" sub={code}
          actions={(
            <Link href="/settings#bank" className="inline-flex shrink-0 items-center gap-1 text-[0.78rem] text-muted hover:text-accent-ink">
              <Settings2 size={13} />{t("switchBank")}
            </Link>
          )}>
      {/* 全体：与首页「合格判断」同一个口径 —— 掌握率（每题最近一次）对着安全线 */}
      <Section title={t("overall")} side={meta.safetyRate != null ? t("safety", { line: meta.safetyRate }) : undefined}>
        <div className="px-4 pt-4">
          <div className="grid grid-cols-3 gap-4 pb-3">
            {[
              { label: t("answered"), value: progress.seenCount, unit: `/ ${total}` },
              { label: t("mastery"), value: mastery ?? "—", unit: mastery != null ? "%" : undefined },
              { label: t("practice"), value: practice ?? "—", unit: practice != null ? "%" : undefined },
            ].map((f) => (
              <div key={f.label} className="text-center">
                <div className="text-[0.75rem] text-muted">{f.label}</div>
                <div className="mt-1 text-[1.6rem] font-medium leading-none text-accent-ink tabular-nums">
                  {f.value}{f.unit && <small className="ml-0.5 text-[0.78rem] text-muted">{f.unit}</small>}
                </div>
              </div>
            ))}
          </div>
          <Meter rate={mastery} line={meta.safetyRate ?? undefined} />
          <p className="mt-2 text-[0.72rem] text-muted">{t("masteryNote")}</p>
        </div>
      </Section>

      {/* 考纲域：⛔ 不画安全线 —— P9 #7 只看整体，不做分领域的合格判断 */}
      <Section title={domainLabel} side={t("weakHint")}>
        {domains.map((d, i) => {
          const s = byDomain.get(d.id);
          const w = tagWeight(meta, d);
          return (
            <div key={d.id}>
              <Row t={t} href={href([d.id])} title={tagName(d, locale)} sub={w != null ? t("weight", { w }) : undefined}
                   done={s?.total ?? 0} total={d.questionCount ?? 0} rate={rateOf(s)} />
              {groupSums[i].map((g) => (
                <Row key={g.name} t={t} level={1} href={href(g.ids)} title={g.name}
                     done={g.sum.answered} total={g.sum.total} rate={pct(g.sum.correct, g.sum.answered)} />
              ))}
            </div>
          );
        })}
      </Section>

      {/* 知识对象：AWS 是服务，IPA 是中分類 —— 弱的在前 */}
      <Section title={topicLabel} side={t("topicSide", { n: topics.length })}>
        {topicRows.map((tp) => {
          const s = byTopic.get(tp.id);
          return <Row key={tp.id} t={t} href={href([tp.id])} title={tagName(tp, locale)}
                      done={s?.total ?? 0} total={tp.questionCount ?? 0} rate={rateOf(s)} />;
        })}
        {topics.length > 10 && (
          <Link href={showAll ? `/banks/${slug}` : `/banks/${slug}?all=1`}
                className="mx-2 block py-3 text-center text-[0.85rem] text-accent-ink hover:bg-accent-soft">
            {showAll ? t("collapse") : t("showMore", { n: topics.length - 10 })}
          </Link>
        )}
      </Section>
    </Page>
  );
}
