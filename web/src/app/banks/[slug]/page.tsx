import Link from "next/link";
import { notFound } from "next/navigation";
import { ArrowRight, Play } from "lucide-react";
import {
  ApiError,
  getBank,
  getMyOverview,
  getMyProgress,
  getMyResume,
  getMyTagStats,
  listBankTags,
  type BankDetail,
  type FocusCursor,
  type Tag,
  type TagStat,
} from "@/lib/api";
import { tagName, tagTypeLabel, tagWeight } from "@/lib/claims";
import { rateColor } from "@/components/RateBar";
import { Band, Stat, Wide } from "@/components/Band";
import { timeAgo } from "@/lib/format";
import { getLocale, getTranslations } from "next-intl/server";
import { modeLabel } from "@/i18n/modeLabel";

export const revalidate = 0;

/**
 * 题库页 = 这个题库的个人学习台。
 * 原则：**每个模块都是入口，不是事实**。四个面板：继续学习（两条轨道）· 按我的状态 · 考纲轴 · 知识对象轴。
 * 轴的名字、权重、及格线全部来自 bank.meta，这一页对 AWS 一无所知。
 * 0 作答时各格子自然是 0 / 全部 / —，布局自己就是空状态。
 */

function Panel({ title, side, className = "", children }: { title: string; side?: React.ReactNode; className?: string; children: React.ReactNode }) {
  return (
    <section className={`rounded-lg border border-line bg-raise ${className}`}>
      <div className="flex items-baseline gap-4 border-b border-line px-6 py-4">
        <span className="eyebrow">{title}</span>
        {side && <span className="ml-auto text-[0.78rem] text-muted">{side}</span>}
      </div>
      {children}
    </section>
  );
}

type TagRow = { tag: Tag; done: number; rate: number | null; weight?: number };

function mergeTagRows(tags: Tag[], stats: TagStat[], meta: BankDetail["meta"]): TagRow[] {
  const byId = new Map(stats.map((s) => [s.tagId, s]));
  return tags.map((tag) => {
    const s = byId.get(tag.id);
    return { tag, done: s?.total ?? 0, rate: s && s.total > 0 ? s.rate : null, weight: tagWeight(meta, tag) };
  });
}

/** 弱的排前面：做过的按正确率升序，没碰过的排后面。 */
function weakestFirst(rows: TagRow[], untouched: (a: TagRow, b: TagRow) => number): TagRow[] {
  const seen = rows.filter((r) => r.rate != null).sort((a, b) => a.rate! - b.rate!);
  const rest = rows.filter((r) => r.rate == null).sort(untouched);
  return [...seen, ...rest];
}

function focusHref(slug: string, f: FocusCursor): string {
  const c = f.context;
  if (c.mode === "tag" && c.tagId != null) return `/banks/${slug}/drill?tag=${c.tagId}`;
  if (c.mode === "contested") return `/banks/${slug}/drill?contested=true`;
  return `/banks/${slug}/drill?mode=${c.mode}`;
}

function levelOf(meta: BankDetail["meta"], name: string): string | undefined {
  const m = meta as { level?: string };
  if (m.level) return m.level;
  return /(Associate|Professional|Specialty|Foundational|Practitioner|Level \d)/i.exec(name)?.[1];
}

export default async function BankPage({
  params,
  searchParams,
}: {
  params: Promise<{ slug: string }>;
  searchParams: Promise<{ all?: string }>;
}) {
  const { slug } = await params;
  const sp = await searchParams;
  const [t, mode, common, home, locale] = await Promise.all([
    getTranslations("bank"),
    getTranslations("mode"),
    getTranslations("common"),
    getTranslations("home"),
    getLocale(),
  ]);
  let bank: BankDetail;
  try {
    bank = await getBank(slug);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const meta = bank.meta;
  const [progress, resume, overview, domainTags, topicTags, domainStats, topicStats] = await Promise.all([
    getMyProgress(slug),
    getMyResume(slug),
    getMyOverview(),
    listBankTags(slug, "domain"),
    listBankTags(slug, "topic"),
    getMyTagStats("domain", 1, slug),
    getMyTagStats("topic", 1, slug),
  ]);

  const total = bank.stats.questionCount;
  const unseen = total - progress.seenCount;
  const rate = progress.seenCount ? Math.round((progress.correctCount / progress.seenCount) * 100) : null;
  const seq = resume.sequential;
  const focus = resume.focus;
  const level = levelOf(meta, bank.name);
  const shortName = bank.name.replace(/\s*\((?:[A-Z]{2,4})-[A-Z0-9]+\)\s*$/, "");
  const code = slug.toUpperCase().replace(/^AWS-/, "");

  const domains = weakestFirst(mergeTagRows(domainTags, domainStats, meta), (a, b) => a.tag.value.localeCompare(b.tag.value));
  const topics = weakestFirst(mergeTagRows(topicTags, topicStats, meta), (a, b) => (b.tag.questionCount ?? 0) - (a.tag.questionCount ?? 0));
  const showAll = sp.all === "1";
  const topicRows = showAll ? topics : topics.slice(0, 10);
  const topicLabel = tagTypeLabel(meta, "topic", locale);
  const domainLabel = tagTypeLabel(meta, "domain", locale);
  const passLine = meta.passScore && meta.maxScore ? Math.round((meta.passScore / meta.maxScore) * 100) : null;
  const weights = domainTags
    .map((t) => ({ t, w: tagWeight(meta, t) }))
    .filter((x) => x.w != null)
    .sort((a, b) => a.t.value.localeCompare(b.t.value));

  const tiles = [
    // ⚠️「该复习的」排在最前：它是有时效的 —— 错过复习窗口补不回来，
    // 而「做错的 / 没做过的」永远在那里等着。顺序本身就是一句建议。
    { key: "due", label: mode("due"), n: progress.dueCount, hint: t("hintDue"), dot: "var(--color-cta)", href: `/banks/${slug}/drill?mode=due` },
    { key: "wrong", label: mode("wrong"), n: progress.wrongCount, hint: t("hintWrong"), dot: "var(--color-warn)", href: `/banks/${slug}/drill?mode=wrong` },
    { key: "unsure", label: mode("unsure"), n: progress.unsureCount, hint: t("hintUnsure"), dot: "var(--color-src-bank)", href: `/banks/${slug}/drill?mode=unsure` },
    { key: "unseen", label: mode("unseen"), n: unseen, hint: t("hintUnseen"), dot: "var(--color-muted)", href: `/banks/${slug}/drill?mode=unseen` },
    { key: "contested", label: mode("contested"), n: bank.stats.contestedCount, hint: t("hintContested"), dot: "var(--color-src-community)", href: `/banks/${slug}/drill?contested=true` },
  ];

  return (
    <Wide>
      <Band
        crumbs={[{ href: "/", label: common("home") }, { label: code }]}
        title={shortName}
        sub={
          <>
            <b className="font-semibold text-ink">{code}</b> · {common("questions", { n: total })}
            {meta.passScore != null && meta.maxScore != null && (
              <> · {home("pass", { score: meta.passScore, max: meta.maxScore })}</>
            )}
            {" · "}
            {home("contested", { n: bank.stats.contestedCount })}
            {weights.length > 0 && (
              <span className="ml-2 inline-flex gap-1.5 align-middle">
                {weights.map(({ t, w }) => (
                  <span key={t.id} className="rounded-sm border border-line px-1.5 font-mono text-[0.7rem] text-muted">
                    {t.value.replace(/^domain-/, "D")} {w}%
                  </span>
                ))}
              </span>
            )}
          </>
        }
        below={
          <div className="mt-3.5 flex gap-2">
            {level && <span className="chip chip-level">{level}</span>}
            <span className="chip">{bank.locale}</span>
            <Link href="/" className="inline-flex items-center gap-1.5 rounded-md border border-line bg-raise px-3 py-1 text-[0.8rem] transition-colors hover:border-muted">
              {t("switchBank")} ▾
            </Link>
          </div>
        }
      >
        <Stat value={progress.seenCount} unit={`/ ${total}`} label={t("statSeen")} />
        <Stat value={rate ?? common("unknown")} unit={rate != null ? "%" : undefined} label={t("statRate")} color={rate != null ? rateColor(rate) : undefined} />
        <Stat value={overview.streakDays} unit={home("unitDay")} label={t("statStreak")} />
      </Band>

      <div className="grid grid-cols-12 items-start gap-6">
        {/* ---- 继续学习：两条轨道 ---- */}
        <Panel title={t("continueTitle")} side={t("continueHint")} className="col-span-12 lg:col-span-8">
          <div className="grid md:grid-cols-2">
            <div className="grid min-h-[212px] grid-rows-[auto_1fr_auto] gap-3 p-6">
              <div className="flex items-center gap-2 text-[0.72rem] uppercase tracking-[0.1em] text-muted">
                <span className="dot bg-ink" />{t("sequential")}
                <time className="ml-auto font-mono normal-case tracking-normal">
                  {seq.lastAt ? t("lastAt", { when: timeAgo(seq.lastAt, locale) }) : t("notStarted")}
                </time>
              </div>
              <div>
                {seq.questionId != null ? (
                  <>
                    <div className="display text-[2.1rem] leading-[1.1] tabular-nums">#{seq.externalNo}<small className="ml-1.5 text-base text-muted">/ {total}</small></div>
                    <p className="mt-2 line-clamp-2 text-[0.88rem] leading-[1.7] text-muted">{seq.stem}</p>
                  </>
                ) : (
                  <>
                    <div className="display text-[1.6rem] leading-tight text-muted">{t("allSeen")}</div>
                    <p className="mt-2 text-[0.88rem] text-muted">{t("allSeenHint")}</p>
                  </>
                )}
              </div>
              <div className="flex items-center gap-4">
                <div className="h-[5px] flex-1 overflow-hidden rounded-sm bg-line">
                  <span className="block h-full rounded-sm bg-ink" style={{ width: `${(seq.doneCount / Math.max(seq.totalCount, 1)) * 100}%` }} />
                </div>
                <span className="w-12 text-right font-mono text-[0.76rem] text-muted tabular-nums">{Math.round((seq.doneCount / Math.max(seq.totalCount, 1)) * 100)}%</span>
                {seq.questionId != null && (
                  <Link href={`/banks/${slug}/drill?mode=unseen`} className="inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-[0.9rem] font-semibold" style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}>
                    <Play size={12} />
                    {seq.doneCount === 0
                      ? t("start", { no: seq.externalNo ?? 1 })
                      : t("continue", { no: seq.externalNo ?? 1 })}
                  </Link>
                )}
              </div>
            </div>

            <div className="grid min-h-[212px] grid-rows-[auto_1fr_auto] gap-3 border-t border-line p-6 md:border-l md:border-t-0">
              <div className="flex items-center gap-2 text-[0.72rem] uppercase tracking-[0.1em] text-muted">
                <span className="dot" style={{ background: "var(--color-src-bank)" }} />{t("lastFocus")}
                {focus && <time className="ml-auto font-mono normal-case tracking-normal">{timeAgo(focus.lastAt, locale)}</time>}
              </div>
              {focus ? (
                <>
                  <div>
                    <div className="display text-[2.1rem] leading-[1.1]">
                      {focus.tag ? tagName(focus.tag, locale) : modeLabel(mode, focus.context.mode, focus.label)}
                      {focus.tag && <small className="ml-2 text-base text-muted">· {tagTypeLabel(meta, focus.tag.type, locale)}</small>}
                    </div>
                    <p className="mt-2 text-[0.88rem] leading-[1.7] text-muted">
                      {focus.done != null
                        ? t("focusProgress", { total: focus.total, done: focus.done })
                        : t("focusCurrent", { total: focus.total })}
                    </p>
                  </div>
                  <div className="flex items-center gap-4">
                    <div className="h-[5px] flex-1 overflow-hidden rounded-sm bg-line">
                      {focus.done != null && <span className="block h-full rounded-sm" style={{ width: `${(focus.done / Math.max(focus.total, 1)) * 100}%`, background: "var(--color-src-bank)" }} />}
                    </div>
                    <span className="w-14 text-right font-mono text-[0.76rem] text-muted tabular-nums">{focus.done != null ? `${focus.done}/${focus.total}` : focus.total}</span>
                    <Link href={focusHref(slug, focus)} className="inline-flex items-center gap-2 rounded-md border border-ink px-5 py-2.5 text-[0.9rem] font-semibold">
                      <Play size={12} />{t("goOn")}
                    </Link>
                  </div>
                </>
              ) : (
                <>
                  <div>
                    <div className="display text-[1.6rem] leading-tight text-muted">{t("noFocus")}</div>
                    <p className="mt-2 text-[0.88rem] leading-[1.7] text-muted">{t("noFocusHint")}</p>
                  </div>
                  <span className="text-[0.8rem] text-muted">{t("pickFocus")}</span>
                </>
              )}
            </div>
          </div>
        </Panel>

        {/* ---- 按我的状态 ---- */}
        <Panel title={t("statusTitle")} side={t("statusHint")} className="col-span-12 lg:col-span-4">
          <div className="grid grid-cols-2">
            {tiles.map((t, i) => (
              <Link
                key={t.key}
                href={t.href}
                className={`group relative grid gap-0.5 px-6 py-5 transition-colors hover:bg-surface ${i % 2 === 0 ? "border-r border-line" : ""} ${i < 2 ? "border-b border-line" : ""}`}
              >
                <span className="flex items-center gap-2 text-[0.8rem] text-muted"><span className="dot" style={{ background: t.dot }} />{t.label}</span>
                <span className={`display mt-1.5 text-[2.3rem] leading-[1.1] tabular-nums ${t.n === 0 ? "text-muted" : ""}`}>{t.n}</span>
                <span className="text-[0.74rem] text-muted">{t.hint}</span>
                <ArrowRight size={14} className="absolute right-5 top-5 text-muted opacity-0 transition-opacity group-hover:opacity-100" />
              </Link>
            ))}
          </div>
        </Panel>

        {/* ---- 考纲轴 ---- */}
        <Panel title={domainLabel} side={passLine != null ? t("passAll", { line: passLine }) : undefined} className="col-span-12 lg:col-span-4">
          <div>
            {domains.map((r) => (
              <Link
                key={r.tag.id}
                href={`/banks/${slug}/drill?tag=${r.tag.id}`}
                className="grid grid-cols-[1.8rem_1fr_auto] items-center gap-3.5 border-b border-line-2 px-6 py-4 transition-colors last:border-b-0 hover:bg-surface"
              >
                <span className="font-mono text-[0.72rem] text-muted">{r.tag.value.replace(/^domain-/, "D")}</span>
                <span>
                  <span className="text-[0.95rem] font-semibold">
                    {tagName(r.tag, locale)}
                    {r.weight != null && <span className="ml-1.5 font-mono text-[0.7rem] font-normal text-muted">{r.weight}%</span>}
                  </span>
                  <span className="block text-[0.76rem] text-muted">
                    {common("questions", { n: r.tag.questionCount ?? 0 })} ·{" "}
                    {r.done > 0 ? <>{t("tagDone")} <span className="tabular-nums">{r.done}</span></> : t("tagged")}
                  </span>
                  <span className="mt-2 block h-1 overflow-hidden rounded-sm bg-line">
                    {r.rate != null && <span className="block h-full rounded-sm" style={{ width: `${r.rate}%`, background: rateColor(r.rate) }} />}
                  </span>
                </span>
                <span className={`display min-w-[3.4rem] text-right text-[1.45rem] tabular-nums ${r.rate == null ? "text-muted" : ""}`}>{r.rate != null ? `${Math.round(r.rate)}%` : common("unknown")}</span>
              </Link>
            ))}
          </div>
          <p className="border-t border-line px-6 py-3 text-[0.74rem] leading-relaxed text-muted">{t("domainNote")}</p>
        </Panel>

        {/* ---- 知识对象轴：表格 ---- */}
        <Panel
          title={topicLabel}
          side={
            <>
              {t("topicSide", { n: topicTags.length })}
              {showAll ? (
                <Link href={`/banks/${slug}`} className="text-src-community">{t("collapse")}</Link>
              ) : (
                <Link href={`/banks/${slug}?all=1`} className="text-src-community">{t("showAll")}</Link>
              )}
            </>
          }
          className="col-span-12 lg:col-span-8"
        >
          <div className="overflow-x-auto">
            <table className="w-full text-[0.9rem]">
              <thead>
                <tr className="border-b border-line text-left text-[0.68rem] uppercase tracking-[0.12em] text-muted">
                  <th className="px-6 py-2.5 font-medium">{topicLabel}</th>
                  <th className="px-6 py-2.5 text-right font-medium">{t("colCount")}</th>
                  <th className="px-6 py-2.5 text-right font-medium">{t("colDone")}</th>
                  <th className="w-[40%] px-6 py-2.5 font-medium">{t("colRate")}</th>
                </tr>
              </thead>
              <tbody>
                {topicRows.map((r) => (
                  <tr key={r.tag.id} className="border-b border-line-2 transition-colors last:border-b-0 hover:bg-surface">
                    <td className="px-6 py-3 font-semibold"><Link href={`/banks/${slug}/drill?tag=${r.tag.id}`} className="block">{tagName(r.tag, locale)}</Link></td>
                    <td className="px-6 py-3 text-right font-mono text-[0.8rem] text-muted tabular-nums">{r.tag.questionCount}</td>
                    <td className="px-6 py-3 text-right font-mono text-[0.8rem] text-muted tabular-nums">{r.done}</td>
                    <td className="px-6 py-3">
                      <div className="grid grid-cols-[1fr_3.2rem] items-center gap-3.5">
                        <div className="h-[7px] overflow-hidden rounded-sm bg-line">
                          {r.rate != null && <span className="block h-full rounded-sm" style={{ width: `${r.rate}%`, background: rateColor(r.rate) }} />}
                        </div>
                        <span className="text-right font-mono text-[0.8rem] tabular-nums" style={{ color: r.rate == null ? "var(--color-muted)" : rateColor(r.rate), fontWeight: r.rate != null && r.rate < 55 ? 600 : 400 }}>
                          {r.rate != null ? `${Math.round(r.rate)}%` : common("unknown")}
                        </span>
                      </div>
                    </td>
                  </tr>
                ))}
                {!showAll && topics.length > 10 && (
                  <tr>
                    <td colSpan={4} className="px-6 py-3 text-center text-[0.82rem]">
                      <Link href={`/banks/${slug}?all=1`} className="text-src-community">{t("showMore", { n: topics.length - 10 })}</Link>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </Panel>
      </div>
    </Wide>
  );
}
