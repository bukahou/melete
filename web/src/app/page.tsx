import Link from "next/link";
import { CalendarRange, ChevronRight, Layers, Play, Search, Settings2, Shuffle, SlidersHorizontal } from "lucide-react";
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
import { getLocale, getTranslations } from "next-intl/server";

export const revalidate = 0;

/**
 * 首页 = 当前题库的仪表盘（P9）。
 *
 * ⭐ 版式参照 it-pass（2026-10-08 用户裁定「几乎完全参考 itpass 的排版设计，但是功能并不一样」）：
 *
 *   ┌ 检索框（术语，即将开放）───────────────────────┐
 *   ├ 学习履历 ─────────────┬ 合格判断 ─────────────┤
 *   ├ 过去问演练 ────────────────────────────────────┤
 *   │  年度/分组      按考纲域                       │
 *   │  自选条件      随机                            │
 *   │  继续上次                                      │  ← 第 5 个按钮（用户裁定，同 it-pass「前回の続き」）
 *   └────────────────────────────────────────────────┘
 *
 * 层级规则仍然成立（globals.css 头注）：卡片无边框；只有「能点」的按钮带强调色描边。
 * 当前题库由后端定（GET /me/bank），⛔ 前端不自己猜 —— iOS 与网页必须看到同一个。
 */

type T = Awaited<ReturnType<typeof getTranslations<"dash">>>;

const pct = (n: number, d: number) => (d > 0 ? Math.round((n / d) * 100) : null);

/** 卡片标题：粗体 + 一条拖到右边的细线（it-pass 的节标题形态）。 */
function CardTitle({ children, side }: { children: React.ReactNode; side?: React.ReactNode }) {
  return (
    <div className="mb-3 flex items-center gap-3">
      <h2 className="shrink-0 text-[0.95rem] font-semibold">{children}</h2>
      <span className="h-px flex-1 bg-line" />
      {side && <span className="shrink-0 text-[0.78rem] text-muted">{side}</span>}
    </div>
  );
}

/** 一个统计数：小标签在上，强调色数字 + 小单位。 */
function Figure({ label, value, unit }: { label: string; value: React.ReactNode; unit?: string }) {
  return (
    <div className="text-center">
      <div className="text-[0.78rem] text-muted">{label}</div>
      <div className="mt-1 text-[1.9rem] font-medium leading-none text-accent-ink tabular-nums">
        {value}
        {unit && <small className="ml-0.5 text-[0.8rem] text-muted">{unit}</small>}
      </div>
    </div>
  );
}

/** 学习履历（P9 #6 #9）：练习正确率（全部作答）+ 已答题数。按领域展开在学习台。 */
function History({ t, bank, progress }: { t: T; bank: BankDetail; progress: Progress }) {
  const practice = pct(progress.attemptCorrectCount, progress.attemptCount);
  return (
    <Link href="/history" className="card group block px-5 py-4">
      <CardTitle side={<ChevronRight size={15} className="transition-colors group-hover:text-accent-ink" />}>
        {t("historyTitle")}
      </CardTitle>
      <div className="grid grid-cols-2 gap-4 pb-1">
        <Figure label={t("historyRate")} value={practice ?? "—"} unit={practice != null ? "%" : undefined} />
        <Figure label={t("historyAnswered")} value={progress.seenCount} unit={`/ ${bank.stats.questionCount}`} />
      </div>
    </Link>
  );
}

/**
 * 合格判断（P9 #7 #8 #9）：只看整体一个数，⛔ 不出预测分。
 * 判断用「当前掌握率」（每题只看最近一次）；答满一场考试的题数之前不判断。
 */
function PassCheck({ t, bank, progress }: { t: T; bank: BankDetail; progress: Progress }) {
  const { safetyRate, examQuestions, passScore, maxScore } = bank.meta;
  const passLine = pct(passScore ?? 0, maxScore ?? 0);
  let body: React.ReactNode;
  if (safetyRate == null) {
    body = <p className="text-[0.88rem] text-muted">{t("passNone")}</p>;
  } else {
    const need = Math.max((examQuestions ?? 0) - progress.seenCount, 0);
    const mastery = pct(progress.correctCount, progress.seenCount);
    if (need > 0 || mastery == null) {
      body = (
        <div className="flex items-center gap-2 text-[0.92rem]">
          {t("passYours")}
          <span className="rounded bg-line-2 px-2 py-0.5 text-[0.8rem] text-muted">—</span>
          <span className="text-[0.78rem] text-muted">{t("passWaiting", { n: need })}</span>
        </div>
      );
    } else {
      const ok = mastery >= safetyRate;
      const tone = ok ? "var(--color-ok)" : "var(--color-warn)";
      body = (
        <div className="flex flex-wrap items-center gap-2 text-[0.92rem]">
          {t("passYours")}
          <span className="rounded-full px-2.5 py-0.5 text-[0.8rem] font-semibold"
                style={{ color: tone, background: `color-mix(in oklab, ${tone} 12%, transparent)` }}>
            {ok ? t("passOk") : t("passGap", { gap: safetyRate - mastery })}
          </span>
          <span className="text-[0.78rem] text-muted">{t("passMasteryIs", { rate: mastery })}</span>
        </div>
      );
    }
  }
  return (
    <Link href="/history" className="card group block px-5 py-4"
          title={passLine != null && safetyRate != null ? t("passSafetyNote", { pass: passLine, line: safetyRate }) : undefined}>
      <CardTitle side={<ChevronRight size={15} className="transition-colors group-hover:text-accent-ink" />}>
        {t("passTitle")}
      </CardTitle>
      <div className="flex min-h-[3.1rem] items-center">{body}</div>
      {safetyRate != null && <p className="mt-1 text-[0.72rem] text-muted">{t("passSafety", { line: safetyRate })}</p>}
    </Link>
  );
}

/** 过去问演练：五个白底描边按钮，两列；「继续上次」是第 5 个（P9 #13，版式同 it-pass）。 */
function Drills({ t, bank, resume, kit, code }: { t: T; bank: BankDetail; resume: Resume; kit: LabelKit; code: string }) {
  const domain = tagTypeLabel(bank.meta, "domain", kit.locale);
  const base = `/banks/${bank.slug}/practice`;
  const cur = resume.cursor;
  const entries = [
    { href: `${base}/year`, icon: CalendarRange, ico: "ico-teal", title: t("drillYear") },
    { href: `${base}/domain`, icon: Layers, ico: "ico-amber", title: t("drillDomain", { domain }) },
    { href: `${base}/pick`, icon: SlidersHorizontal, ico: "ico-violet", title: t("drillPick") },
    { href: `${base}/random`, icon: Shuffle, ico: "ico-rose", title: t("drillRandom") },
    {
      // 从没作答过 ⇒ 第一组从头开始 —— 第 5 个按钮永远有去处
      href: cur ? drillHref(bank.slug, cur.context, { i: cur.offset }) : `${base}/year`,
      icon: Play, ico: "ico-indigo", title: t("drillContinue"),
      sub: cur
        ? `${describeContext(cur.context, kit)} · ${cur.finished ? t("continueFinished") : t("continueAt", { no: cur.offset + 1, total: cur.total })}`
        : t("startFirstHint"),
    },
  ];
  return (
    <section className="card px-5 py-4">
      <CardTitle side={
        <Link href="/settings#bank" className="inline-flex items-center gap-1 hover:text-accent-ink">
          {code} <Settings2 size={12} />
        </Link>
      }>
        {t("drillTitle")}
      </CardTitle>
      <div className="grid gap-3 sm:grid-cols-2">
        {entries.map((e) => (
          <Link key={e.title} href={e.href} className="tile group flex items-center gap-3 px-3 py-2.5">
            <span className={`ico ${e.ico} h-9 w-9 shrink-0`}><e.icon size={18} /></span>
            <span className="min-w-0 flex-1">
              <span className="block text-[0.92rem] font-medium">{e.title}</span>
              {"sub" in e && e.sub && <span className="block truncate text-[0.74rem] text-muted">{e.sub}</span>}
            </span>
            <ChevronRight size={16} className="shrink-0" />
          </Link>
        ))}
      </div>
    </section>
  );
}

/**
 * 顶部检索框（P9 #5：只搜术语）—— 回车即进用语集并带上检索词（那里是本地即时检索）。
 * 普通 GET 表单：⛔ 不在首页引入客户端状态，首页只负责把人送到对的地方。
 */
function SearchBox({ t, slug }: { t: T; slug: string }) {
  return (
    <form action={`/banks/${slug}/glossary`} method="GET"
          className="flex items-center gap-3 rounded-full border border-[color-mix(in_oklab,var(--color-accent-ink)_55%,transparent)] bg-tile px-5 py-3 focus-within:border-accent-ink">
      <Search size={17} className="text-muted" />
      <input name="q" placeholder={t("searchPlaceholder")} aria-label={t("searchPlaceholder")}
             className="min-w-0 flex-1 bg-transparent text-[0.92rem] outline-none placeholder:text-muted" />
    </form>
  );
}

/** 还没有当前题库（新用户）：引导去设置选（P9 #14）。 */
function NoBank({ t }: { t: T }) {
  return (
    <div className="mx-auto max-w-xl pt-16">
      <div className="card px-8 py-10 text-center">
        <h1 className="display text-[1.4rem]">{t("noBankTitle")}</h1>
        <p className="mt-4 text-[0.92rem] leading-relaxed text-muted">{t("noBankBody")}</p>
        <Link href="/settings#bank"
              className="mt-8 inline-flex items-center gap-2 rounded-xl px-5 py-2.5 text-[0.9rem] font-semibold"
              style={{ background: "var(--color-accent)", color: "var(--color-accent-fg)" }}>
          <Settings2 size={14} />{t("noBankGo")}
        </Link>
      </div>
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
  const code = slug.toUpperCase().replace(/^AWS-/, "");

  return (
    <div className="mx-auto grid max-w-[960px] gap-4">
      <SearchBox t={t} slug={slug} />
      <div className="grid gap-4 md:grid-cols-2">
        <History t={t} bank={bank} progress={progress} />
        <PassCheck t={t} bank={bank} progress={progress} />
      </div>
      <Drills t={t} bank={bank} resume={resume} kit={kit} code={code} />
    </div>
  );
}
