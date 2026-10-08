import Link from "next/link";
import { ArrowRight, Play, Settings2 } from "lucide-react";
import {
  getBank,
  getMyBank,
  getMyProgress,
  getMyResume,
  type BankDetail,
  type Progress,
  type Resume,
} from "@/lib/api";
import { tagName, tagTypeLabel } from "@/lib/claims";
import { rateColor } from "@/components/RateBar";
import { Band, Wide } from "@/components/Band";
import { timeAgo } from "@/lib/format";
import { getLocale, getTranslations } from "next-intl/server";
import { modeLabel } from "@/i18n/modeLabel";

export const revalidate = 0;

/**
 * 首页 = 当前题库的仪表盘（P9，2026-10-08）。
 *
 * ⭐ 原来的首页是多题库选择台。用户裁定（P9 #1）：「把题库选择转移到设置中…
 * 如同更换账号一样」—— 一次只面对一个题库，换题库去设置。
 *
 * 三块，顺序即优先级（P9 #4）：② 学习履历 · ③ 合格判断 · ④ 过去问演练。
 * ① 术语检索要等术语表（第 6 步），在那之前不放一个搜不出东西的框。
 *
 * 当前题库由后端定（GET /me/bank：选过的 → 最近作答的 → 没有），
 * ⛔ 前端不自己猜 —— iOS 与网页必须看到同一个。
 */

type T = Awaited<ReturnType<typeof getTranslations<"dash">>>;

function Panel({ title, side, className = "", children }: {
  title: string; side?: React.ReactNode; className?: string; children: React.ReactNode;
}) {
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

const pct = (n: number, d: number) => (d > 0 ? Math.round((n / d) * 100) : null);

/** ② 学习履历：总题数固定，所以不按时间段，只看「答了多少 / 练得怎么样」（P9 #6）。 */
function History({ t, bank, progress, locale }: { t: T; bank: BankDetail; progress: Progress; locale: string }) {
  const total = bank.stats.questionCount;
  const practice = pct(progress.attemptCorrectCount, progress.attemptCount);
  return (
    <Panel title={t("historyTitle")} className="col-span-12 lg:col-span-6">
      <div className="grid gap-5 p-6">
        <div className="flex items-end justify-between gap-6">
          <div>
            <div className="text-[0.78rem] text-muted">{t("historyAnswered")}</div>
            <div className="display mt-1 text-[2.6rem] leading-none tabular-nums">
              {progress.seenCount}
              <small className="ml-1 text-[1.05rem] text-muted">/ {total}</small>
            </div>
          </div>
          <div className="text-right">
            <div className="text-[0.78rem] text-muted">{t("historyRate")}</div>
            <div
              className="display mt-1 text-[2.6rem] leading-none tabular-nums"
              style={practice != null ? { color: rateColor(practice) } : undefined}
            >
              {practice != null ? <>{practice}<small className="text-[1.05rem] text-muted">%</small></> : <span className="text-muted">—</span>}
            </div>
          </div>
        </div>
        <div className="h-[5px] overflow-hidden rounded-sm bg-line">
          <span className="block h-full rounded-sm bg-ink" style={{ width: `${(progress.seenCount / Math.max(total, 1)) * 100}%` }} />
        </div>
        <p className="text-[0.76rem] leading-relaxed text-muted">
          {progress.attemptCount > 0 ? t("historyRateNote", { n: progress.attemptCount }) : t("historyEmpty")}
        </p>
        <Link href={`/banks/${bank.slug}`} className="inline-flex items-center gap-1 text-[0.88rem] text-src-community">
          {t("historyDetail", { domain: tagTypeLabel(bank.meta, "domain", locale) })} <ArrowRight size={14} />
        </Link>
      </div>
    </Panel>
  );
}

/**
 * ③ 合格判断（P9 #7 #8 #9）：只看整体一个数，⛔ 不出预测分。
 * 判断用「当前掌握率」（每题只看最近一次）—— 它回答的是「现在去考能不能过」。
 * 答满一场考试的题数之前不判断：样本太小的正确率会骗人。
 */
function PassCheck({ t, bank, progress }: { t: T; bank: BankDetail; progress: Progress }) {
  const { safetyRate, examQuestions, passScore, maxScore } = bank.meta;
  const passLine = pct(passScore ?? 0, maxScore ?? 0);
  if (safetyRate == null) {
    return (
      <Panel title={t("passTitle")} className="col-span-12 lg:col-span-6">
        <p className="p-6 text-sm text-muted">{t("passNone")}</p>
      </Panel>
    );
  }
  const need = Math.max((examQuestions ?? 0) - progress.seenCount, 0);
  const mastery = pct(progress.correctCount, progress.seenCount);
  const judged = need === 0 && mastery != null;
  const ok = judged && mastery >= safetyRate;
  const tone = ok ? "var(--color-ok)" : "var(--color-warn)";

  return (
    <Panel
      title={t("passTitle")}
      side={t("passSafety", { line: safetyRate })}
      className="col-span-12 lg:col-span-6"
    >
      <div className="grid gap-5 p-6">
        {judged ? (
          <div className="flex items-end justify-between gap-6">
            <div>
              <div className="text-[0.78rem] text-muted">{t("passMastery")}</div>
              <div className="display mt-1 text-[2.6rem] leading-none tabular-nums" style={{ color: tone }}>
                {mastery}<small className="text-[1.05rem] text-muted">%</small>
              </div>
            </div>
            <span
              className="rounded-md border px-3 py-1.5 text-[0.88rem] font-semibold"
              style={{ borderColor: tone, color: tone, background: `color-mix(in oklab, ${tone} 8%, transparent)` }}
            >
              {ok ? t("passOk") : t("passGap", { gap: safetyRate - mastery })}
            </span>
          </div>
        ) : (
          <div>
            <div className="display text-[1.6rem] leading-tight">{t("passWaiting", { n: need })}</div>
            <p className="mt-2 text-[0.82rem] leading-relaxed text-muted">{t("passWaitingNote", { exam: examQuestions ?? 0 })}</p>
          </div>
        )}
        {/* 一根尺：掌握率（或「已答 / 一场考试」）对着安全线。安全线是一道竖线，⛔ 不靠颜色单独表达 */}
        <div className="relative h-[7px] rounded-sm bg-line">
          <span
            className="absolute inset-y-0 left-0 rounded-sm"
            style={{
              width: `${judged ? mastery : (progress.seenCount / Math.max(examQuestions ?? 1, 1)) * 100}%`,
              background: judged ? tone : "var(--color-ink)",
            }}
          />
          {judged && (
            <span className="absolute -top-1 -bottom-1 w-[2px] bg-ink" style={{ left: `${safetyRate}%` }} />
          )}
        </div>
        <p className="text-[0.76rem] leading-relaxed text-muted">
          {judged && t("passMasteryNote")}
          {judged && passLine != null && " · "}
          {passLine != null && t("passSafetyNote", { pass: passLine, line: safetyRate })}
        </p>
      </div>
    </Panel>
  );
}

function continueHref(slug: string, c: { mode: string; tagId?: number | null }): string {
  if (c.mode === "tag" && c.tagId != null) return `/banks/${slug}/drill?tag=${c.tagId}`;
  if (c.mode === "contested") return `/banks/${slug}/drill?contested=true`;
  return `/banks/${slug}/drill?mode=${c.mode}`;
}

/**
 * ④ 过去问演练（P9 #10–#13）。
 * 4.1–4.4 的出题条件在第 4 步实装；这里先把入口的位置与说明立起来。
 * 4.5「继续」现在先用已有的两条轨道里【更近的那一条】—— 第 4 步换成单一游标（#13）。
 */
function Drills({ t, bank, resume, locale, mode }: {
  t: T; bank: BankDetail; resume: Resume; locale: string;
  mode: Awaited<ReturnType<typeof getTranslations<"mode">>>;
}) {
  const domain = tagTypeLabel(bank.meta, "domain", locale);
  const entries = [
    { key: "year", title: t("drillYear"), hint: t("drillYearHint", { size: bank.meta.groupSize ?? 100 }) },
    { key: "domain", title: t("drillDomain", { domain }), hint: t("drillDomainHint", { domain }) },
    { key: "pick", title: t("drillPick"), hint: t("drillPickHint", { domain }) },
    { key: "random", title: t("drillRandom"), hint: t("drillRandomHint") },
  ];

  const seq = resume.sequential;
  const focus = resume.focus;
  const seqAt = seq.lastAt && seq.questionId != null ? Date.parse(seq.lastAt) : -1;
  const focusAt = focus ? Date.parse(focus.lastAt) : -1;
  const last =
    focusAt > seqAt && focus
      ? {
          href: continueHref(bank.slug, focus.context),
          label: focus.tag ? tagName(focus.tag, locale) : modeLabel(mode, focus.context.mode, focus.label),
          at: focus.lastAt,
        }
      : seqAt >= 0
        ? { href: `/banks/${bank.slug}/drill?mode=unseen`, label: t("continueSeq", { no: seq.externalNo ?? 1 }), at: seq.lastAt! }
        : null;

  return (
    <Panel title={t("drillTitle")} className="col-span-12">
      <div className="grid sm:grid-cols-2 lg:grid-cols-5">
        {entries.map((e) => (
          <div
            key={e.key}
            className="grid content-start gap-1.5 border-b border-line px-6 py-5 sm:border-r lg:border-b-0"
            aria-disabled="true"
          >
            <span className="flex items-center gap-2">
              <span className="text-[0.98rem] font-semibold text-muted">{e.title}</span>
              <span className="rounded-sm border border-dashed border-line px-1.5 text-[0.62rem] tracking-[0.06em] text-muted">
                {t("drillSoon")}
              </span>
            </span>
            <span className="text-[0.76rem] leading-relaxed text-muted">{e.hint}</span>
          </div>
        ))}
        {last ? (
          <Link
            href={last.href}
            className="group grid content-start gap-1.5 px-6 py-5 transition-colors hover:bg-surface"
          >
            <span className="flex items-center gap-2 text-[0.98rem] font-semibold">
              <Play size={12} />{t("drillContinue")}
              <ArrowRight size={14} className="ml-auto text-muted opacity-0 transition-opacity group-hover:opacity-100" />
            </span>
            <span className="truncate text-[0.82rem]">{last.label}</span>
            <time className="font-mono text-[0.72rem] text-muted">{timeAgo(last.at, locale)}</time>
          </Link>
        ) : (
          <div className="grid content-start gap-1.5 px-6 py-5">
            <span className="text-[0.98rem] font-semibold text-muted">{t("drillContinue")}</span>
            <span className="text-[0.76rem] text-muted">{t("drillContinueNone")}</span>
          </div>
        )}
      </div>
    </Panel>
  );
}

/** 还没有当前题库（新用户）：引导去设置选（P9 #14）。 */
function NoBank({ t }: { t: T }) {
  return (
    <div className="mx-auto max-w-xl pt-16 text-center">
      <h1 className="display text-[2rem] leading-tight">{t("noBankTitle")}</h1>
      <p className="mt-4 text-[0.92rem] leading-relaxed text-muted">{t("noBankBody")}</p>
      <Link
        href="/settings#bank"
        className="mt-8 inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-[0.9rem] font-semibold"
        style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
      >
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
  const [bank, progress, resume] = await Promise.all([getBank(slug), getMyProgress(slug), getMyResume(slug)]);
  const shortName = bank.name.replace(/\s*\((?:[A-Z]{2,4})-[A-Z0-9]+\)\s*$/, "");
  const code = slug.toUpperCase().replace(/^AWS-/, "");

  return (
    <Wide>
      <Band
        title={shortName}
        sub={
          <>
            <b className="font-semibold text-ink">{code}</b> · {t("questionCount", { n: bank.stats.questionCount })}
            {current.source === "recent" && <> · {t("recentNote")}</>}
          </>
        }
        below={
          <div className="mt-3.5">
            <Link
              href="/settings#bank"
              className="inline-flex items-center gap-1.5 rounded-md border border-line bg-raise px-3 py-1 text-[0.8rem] transition-colors hover:border-muted"
            >
              <Settings2 size={13} />{t("switchBank")}
            </Link>
          </div>
        }
      />
      <div className="grid grid-cols-12 items-start gap-6">
        <History t={t} bank={bank} progress={progress} locale={locale} />
        <PassCheck t={t} bank={bank} progress={progress} />
        <Drills t={t} bank={bank} resume={resume} locale={locale} mode={mode} />
      </div>
    </Wide>
  );
}
