import Link from "next/link";
import { ArrowRight, CalendarRange, ChevronRight, Layers, Play, Settings2, Shuffle, SlidersHorizontal } from "lucide-react";
import {
  getBank,
  getMyBank,
  getMyProgress,
  getMyResume,
  listAxisTags,
  type BankDetail,
  type Progress,
  type Resume,
} from "@/lib/api";
import { tagTypeLabel } from "@/lib/claims";
import { drillHref } from "@/lib/drillSpec";
import { describeContext, type LabelKit } from "@/lib/drillLabel";
import { timeAgo } from "@/lib/format";
import { getLocale, getTranslations } from "next-intl/server";

export const revalidate = 0;

/**
 * 首页 = 当前题库的仪表盘（P9）。
 *
 * ⭐ 2026-10-08 改版：按【层级】排，⛔ 不按「功能清单」排。
 *   用户原话：「一眼望去有点视觉疲劳…找不到重点」。旧版五个数字都是 2.6rem 900 衬线、
 *   五块面板同样的边框 —— 所有东西都是重点 = 没有重点。现在三级，每级只做一件事：
 *
 *   1 级  继续上次 / 开始 —— 全页唯一的强调色实底（来这里最常做的事）
 *   2 级  四个演练入口   —— 白卡片 + 强调色图标，⛔ 无边框
 *   3 级  学习履历 · 合格判断 —— 小字、普通字重，只交代现状
 *
 * ① 术语检索要等术语表（第 6 步），在那之前不放一个搜不出东西的框。
 * 当前题库由后端定（GET /me/bank），⛔ 前端不自己猜 —— iOS 与网页必须看到同一个。
 */

type T = Awaited<ReturnType<typeof getTranslations<"dash">>>;

const pct = (n: number, d: number) => (d > 0 ? Math.round((n / d) * 100) : null);

/** 1 级：继续上次。从没作答过 ⇒「从第一组开始」—— 首页永远有一个明确的下一步。 */
function Primary({ t, bank, resume, kit }: { t: T; bank: BankDetail; resume: Resume; kit: LabelKit }) {
  const cur = resume.cursor;
  const href = cur ? drillHref(bank.slug, cur.context, { i: cur.offset }) : `/banks/${bank.slug}/practice/year`;
  return (
    <Link
      href={href}
      className="group flex items-center gap-4 rounded-2xl px-6 py-5 transition-[filter] hover:brightness-110"
      style={{ background: "var(--color-accent)", color: "var(--color-accent-fg)" }}
    >
      <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full"
            style={{ background: "color-mix(in oklab, var(--color-accent-fg) 18%, transparent)" }}>
        <Play size={18} fill="currentColor" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-[1.05rem] font-semibold">{cur ? t("drillContinue") : t("startFirst")}</span>
        <span className="mt-0.5 block truncate text-[0.85rem] opacity-85">
          {cur
            ? <>
                {describeContext(cur.context, kit)}
                {" · "}
                {cur.finished ? t("continueFinished") : t("continueAt", { no: cur.offset + 1, total: cur.total })}
                {" · "}
                {timeAgo(cur.lastAt, kit.locale)}
              </>
            : t("startFirstHint")}
        </span>
      </span>
      <ArrowRight size={18} className="shrink-0 transition-transform group-hover:translate-x-0.5" />
    </Link>
  );
}

/** 2 级：四个演练入口。图标用强调色的淡底 —— 它告诉你「这里能点」，但不跟 1 级抢。 */
function Entries({ t, bank, locale }: { t: T; bank: BankDetail; locale: string }) {
  const domain = tagTypeLabel(bank.meta, "domain", locale);
  const base = `/banks/${bank.slug}/practice`;
  const entries = [
    { href: `${base}/year`, icon: CalendarRange, title: t("drillYear"), hint: t("drillYearHint", { size: bank.meta.groupSize ?? 100 }) },
    { href: `${base}/domain`, icon: Layers, title: t("drillDomain", { domain }), hint: t("drillDomainHint", { domain }) },
    { href: `${base}/pick`, icon: SlidersHorizontal, title: t("drillPick"), hint: t("drillPickHint", { domain }) },
    { href: `${base}/random`, icon: Shuffle, title: t("drillRandom"), hint: t("drillRandomHint") },
  ];
  return (
    <section>
      <h2 className="eyebrow mb-3 px-1">{t("drillTitle")}</h2>
      <div className="grid gap-3 sm:grid-cols-2">
        {entries.map((e) => (
          <Link key={e.href} href={e.href}
                className="card group flex items-center gap-4 px-5 py-4 transition-shadow hover:shadow-[0_0_0_1.5px_var(--color-accent-ink)]">
            <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl"
                  style={{ background: "var(--color-accent-soft)", color: "var(--color-accent-ink)" }}>
              <e.icon size={18} />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block text-[0.95rem] font-semibold">{e.title}</span>
              <span className="mt-0.5 block truncate text-[0.78rem] text-muted">{e.hint}</span>
            </span>
            <ChevronRight size={16} className="shrink-0 text-muted transition-colors group-hover:text-ink" />
          </Link>
        ))}
      </div>
    </section>
  );
}

/** 3 级：学习履历（P9 #6 #9）。数字小而安静 —— 它交代现状，⛔ 不抢「下一步做什么」。 */
function History({ t, bank, progress, locale }: { t: T; bank: BankDetail; progress: Progress; locale: string }) {
  const total = bank.stats.questionCount;
  const practice = pct(progress.attemptCorrectCount, progress.attemptCount);
  return (
    <section className="card grid content-start gap-3 px-5 py-4">
      <div className="flex items-baseline justify-between">
        <h2 className="eyebrow">{t("historyTitle")}</h2>
        <Link href={`/banks/${bank.slug}`} className="inline-flex items-center gap-0.5 text-[0.78rem] text-muted hover:text-ink">
          {t("historyDetail", { domain: tagTypeLabel(bank.meta, "domain", locale) })} <ChevronRight size={13} />
        </Link>
      </div>
      <div className="grid grid-cols-2 gap-4">
        <div>
          <div className="text-[0.75rem] text-muted">{t("historyAnswered")}</div>
          <div className="mt-0.5 text-[1.35rem] font-semibold tabular-nums">
            {progress.seenCount}<small className="ml-1 text-[0.8rem] font-normal text-muted">/ {total}</small>
          </div>
        </div>
        <div>
          <div className="text-[0.75rem] text-muted">{t("historyRate")}</div>
          <div className="mt-0.5 text-[1.35rem] font-semibold tabular-nums">
            {practice != null ? <>{practice}<small className="text-[0.8rem] font-normal text-muted">%</small></> : <span className="text-muted">—</span>}
          </div>
        </div>
      </div>
      <div className="h-1 overflow-hidden rounded-full bg-line-2">
        <span className="block h-full rounded-full bg-muted" style={{ width: `${(progress.seenCount / Math.max(total, 1)) * 100}%` }} />
      </div>
    </section>
  );
}

/**
 * 3 级：合格判断（P9 #7 #8 #9）：只看整体一个数，⛔ 不出预测分。
 * 判断用「当前掌握率」（每题只看最近一次）；答满一场考试的题数之前不判断。
 * ⭐ 状态色只在【已判断】时出现 —— 那是这张卡唯一需要你注意的信息。
 */
function PassCheck({ t, bank, progress }: { t: T; bank: BankDetail; progress: Progress }) {
  const { safetyRate, examQuestions, passScore, maxScore } = bank.meta;
  const passLine = pct(passScore ?? 0, maxScore ?? 0);
  const head = (
    <div className="flex items-baseline justify-between">
      <h2 className="eyebrow">{t("passTitle")}</h2>
      {safetyRate != null && <span className="text-[0.78rem] text-muted">{t("passSafety", { line: safetyRate })}</span>}
    </div>
  );
  if (safetyRate == null) {
    return <section className="card grid content-start gap-3 px-5 py-4">{head}<p className="text-[0.85rem] text-muted">{t("passNone")}</p></section>;
  }
  const need = Math.max((examQuestions ?? 0) - progress.seenCount, 0);
  const mastery = pct(progress.correctCount, progress.seenCount);
  const judged = need === 0 && mastery != null;
  const ok = judged && mastery >= safetyRate;
  const tone = ok ? "var(--color-ok)" : "var(--color-warn)";
  return (
    <section className="card grid content-start gap-3 px-5 py-4" title={passLine != null ? t("passSafetyNote", { pass: passLine, line: safetyRate }) : undefined}>
      {head}
      {judged ? (
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="text-[0.75rem] text-muted">{t("passMastery")}</div>
            <div className="mt-0.5 text-[1.35rem] font-semibold tabular-nums">{mastery}<small className="text-[0.8rem] font-normal text-muted">%</small></div>
          </div>
          <span className="rounded-full px-3 py-1 text-[0.8rem] font-semibold"
                style={{ color: tone, background: `color-mix(in oklab, ${tone} 12%, transparent)` }}>
            {ok ? t("passOk") : t("passGap", { gap: safetyRate - mastery })}
          </span>
        </div>
      ) : (
        // ⚠️ 还没判断时⛔ 不挂「当前掌握率」标签 —— 标签说的是一个数，下面却是一句话，对不上
        <div className="text-[0.95rem]">{t("passWaiting", { n: need })}</div>
      )}
      {/* 一根尺：掌握率（或「已答 / 一场考试」）对着安全线；安全线是一道竖线，⛔ 不靠颜色单独表达 */}
      <div className="relative h-1 rounded-full bg-line-2">
        <span className="absolute inset-y-0 left-0 rounded-full"
              style={{
                width: `${judged ? mastery : (progress.seenCount / Math.max(examQuestions ?? 1, 1)) * 100}%`,
                background: judged ? tone : "var(--color-muted)",
              }} />
        {judged && <span className="absolute -top-1 -bottom-1 w-[2px] bg-ink" style={{ left: `${safetyRate}%` }} />}
      </div>
    </section>
  );
}

/** 还没有当前题库（新用户）：引导去设置选（P9 #14）。 */
function NoBank({ t }: { t: T }) {
  return (
    <div className="mx-auto max-w-xl pt-16 text-center">
      <h1 className="display text-[1.6rem]">{t("noBankTitle")}</h1>
      <p className="mt-4 text-[0.92rem] leading-relaxed text-muted">{t("noBankBody")}</p>
      <Link href="/settings#bank"
            className="mt-8 inline-flex items-center gap-2 rounded-xl px-5 py-2.5 text-[0.9rem] font-semibold"
            style={{ background: "var(--color-accent)", color: "var(--color-accent-fg)" }}>
        <Settings2 size={14} />{t("noBankGo")}
      </Link>
    </div>
  );
}

export default async function HomePage() {
  const [t, mode, locale, current] = await Promise.all([
    getTranslations("dash"),
    getTranslations("mode"),
    getLocale(),
    getMyBank(),
  ]);
  if (current.source === "none" || !current.bankSlug) return <NoBank t={t} />;

  const slug = current.bankSlug;
  const [bank, progress, resume, tags] = await Promise.all([
    getBank(slug), getMyProgress(slug), getMyResume(slug), listAxisTags(slug),
  ]);
  const kit: LabelKit = {
    meta: bank.meta, tags, locale,
    t: (k, v) => t(k as never, v as never),
    mode: mode as never,
  };
  const shortName = bank.name.replace(/\s*\((?:[A-Z]{2,4})-[A-Z0-9]+\)\s*$/, "");
  const code = slug.toUpperCase().replace(/^AWS-/, "");

  return (
    <div className="mx-auto grid max-w-3xl gap-6">
      {/* 题库名：一行，安静。切换是设置里的事，这里只留一个小入口 */}
      <header className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-1">
        <h1 className="display text-[1.35rem]">{shortName}</h1>
        <span className="text-[0.8rem] text-muted">
          {code} · {t("questionCount", { n: bank.stats.questionCount })}
          {current.source === "recent" && <> · {t("recentNote")}</>}
        </span>
        <Link href="/settings#bank" className="ml-auto inline-flex items-center gap-1 text-[0.8rem] text-muted hover:text-ink">
          <Settings2 size={13} />{t("switchBank")}
        </Link>
      </header>

      <Primary t={t} bank={bank} resume={resume} kit={kit} />
      <Entries t={t} bank={bank} locale={locale} />
      <div className="grid gap-3 sm:grid-cols-2">
        <History t={t} bank={bank} progress={progress} locale={locale} />
        <PassCheck t={t} bank={bank} progress={progress} />
      </div>
    </div>
  );
}
