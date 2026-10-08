import Link from "next/link";
import { notFound } from "next/navigation";
import { ChevronRight, Settings2 } from "lucide-react";
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

function CardTitle({ children, side }: { children: React.ReactNode; side?: React.ReactNode }) {
  return (
    <div className="mb-3 flex items-center gap-3">
      <h2 className="shrink-0 text-[0.95rem] font-semibold">{children}</h2>
      <span className="h-px flex-1 bg-line" />
      {side && <span className="shrink-0 text-[0.78rem] text-muted">{side}</span>}
    </div>
  );
}

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
 *  ⚠️ 分隔线由外层 divide-y 画，⛔ 不在行上用 last:border-b-0 —— 考纲域行包在各自的组里，
 *  「组里最后一个」恰好是它，于是只有它没线（第一版截图里的不齐就是这么来的）。 */
function Row({ href, title, sub, done, total, rate, level = 0, t }: {
  href: string; title: string; sub?: string; done: number; total: number; rate: number | null; level?: 0 | 1; t: T;
}) {
  // ⚠️ 手机上「已答」挪到名字下面：四列挤在 390px 里，名字被截成「设计…」，看不出是哪一块
  const doneOf = t("doneOf", { done, total });
  return (
    <Link href={href}
          className={`group flex items-center gap-4 py-3 pr-4 transition-colors hover:bg-accent-soft ${level ? "pl-9" : "pl-5"}`}>
      <span className="min-w-0 flex-1">
        <span className={`block truncate ${level ? "text-[0.88rem]" : "text-[0.95rem] font-semibold"}`}>{title}</span>
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
      <ChevronRight size={15} className="text-muted transition-colors group-hover:text-accent-ink" />
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
    <div className="mx-auto grid max-w-[960px] gap-4">
      <header className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <nav className="flex w-full items-center gap-2 text-[0.82rem] text-muted">
          <Link href="/" className="hover:text-ink">{t("home")}</Link>
          <span className="opacity-50">›</span>
          <span className="text-ink">{t("title")}</span>
        </nav>
        <h1 className="display text-[1.4rem]">{t("title")}</h1>
        <span className="text-[0.82rem] text-muted">{code}</span>
        <Link href="/settings#bank" className="ml-auto inline-flex items-center gap-1 text-[0.78rem] text-muted hover:text-accent-ink">
          <Settings2 size={13} />{t("switchBank")}
        </Link>
      </header>

      {/* 全体：与首页「合格判断」同一个口径 —— 掌握率（每题最近一次）对着安全线 */}
      <section className="card px-5 py-4">
        <CardTitle side={meta.safetyRate != null ? t("safety", { line: meta.safetyRate }) : undefined}>{t("overall")}</CardTitle>
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
      </section>

      {/* 考纲域：⛔ 不画安全线 —— P9 #7 只看整体，不做分领域的合格判断 */}
      <section className="card overflow-hidden pt-4">
        <div className="px-5"><CardTitle side={t("weakHint")}>{domainLabel}</CardTitle></div>
        <div className="divide-y divide-line-2 border-t border-line-2">
        {domains.map((d, i) => {
          const s = byDomain.get(d.id);
          const w = tagWeight(meta, d);
          return (
            <div key={d.id} className="divide-y divide-line-2">
              <Row t={t} href={href([d.id])} title={tagName(d, locale)} sub={w != null ? t("weight", { w }) : undefined}
                   done={s?.total ?? 0} total={d.questionCount ?? 0} rate={rateOf(s)} />
              {groupSums[i].map((g) => (
                <Row key={g.name} t={t} level={1} href={href(g.ids)} title={g.name}
                     done={g.sum.answered} total={g.sum.total} rate={pct(g.sum.correct, g.sum.answered)} />
              ))}
            </div>
          );
        })}
        </div>
      </section>

      {/* 知识对象：AWS 是服务，IPA 是中分類 —— 弱的在前 */}
      <section className="card overflow-hidden pt-4">
        <div className="px-5"><CardTitle side={t("topicSide", { n: topics.length })}>{topicLabel}</CardTitle></div>
        <div className="divide-y divide-line-2 border-t border-line-2">
          {topicRows.map((tp) => {
            const s = byTopic.get(tp.id);
            return <Row key={tp.id} t={t} href={href([tp.id])} title={tagName(tp, locale)}
                        done={s?.total ?? 0} total={tp.questionCount ?? 0} rate={rateOf(s)} />;
          })}
        </div>
        {topics.length > 10 && (
          <Link href={showAll ? `/banks/${slug}` : `/banks/${slug}?all=1`}
                className="block border-t border-line-2 py-3 text-center text-[0.85rem] text-accent-ink hover:bg-accent-soft">
            {showAll ? t("collapse") : t("showMore", { n: topics.length - 10 })}
          </Link>
        )}
      </section>
    </div>
  );
}
