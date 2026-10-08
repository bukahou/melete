import Link from "next/link";
import { notFound } from "next/navigation";
import { AlertTriangle, ArrowLeft } from "lucide-react";
import { ApiError, getBank, getQuestion, listAxisTags, listQuestions, summarizeQuestions, type DrillContext } from "@/lib/api";
import { contextFromSearch, drillHref, isShrinking, listParams } from "@/lib/drillSpec";
import { describeContext } from "@/lib/drillLabel";
import { ClaimsPanel } from "@/components/ClaimsPanel";
import { DrillCard } from "@/components/DrillCard";
import { Markdown } from "@/components/Markdown";
import { TagChip } from "@/components/TagChip";
import { getLocale, getTranslations } from "next-intl/server";
import { needsSourceNotice } from "@/i18n/locales";

type Search = Record<string, string | string[] | undefined>;

/**
 * pickExplanations 挑该语言的解析。
 *
 * explanation 表天生带 locale，同一道题可以有多语言解析。
 * ⚠️ 全部渲染会让日语用户同时看到中文和日文两份 —— 不是「更全」，是噪音。
 * 该语言有就只给该语言；一份都没有就把现有的都给出来（⛔ 宁可语言不对，
 * 也不该把仅有的解析藏起来），上方的「暂无译文」提示已经说明了状况。
 */
function pickExplanations<T extends { locale: string }>(all: T[], locale: string): T[] {
  const hit = all.filter((e) => e.locale === locale);
  return hit.length > 0 ? hit : all;
}

/**
 * 刷题页。每题一次页面导航（URL 携带位置）：可分享、可后退、刷新不丢进度，且天然 SSR。
 */
export default async function DrillPage({
  params,
  searchParams,
}: {
  params: Promise<{ slug: string }>;
  searchParams: Promise<Search>;
}) {
  const { slug } = await params;
  const sp = await searchParams;
  const [t, dash, modeT, locale] = await Promise.all([
    getTranslations("drill"),
    getTranslations("dash"),
    getTranslations("mode"),
    getLocale(),
  ]);

  const index = Math.max(0, Number(sp.i ?? 0) || 0);
  const done = Math.max(0, Number(sp.done ?? 0) || 0);
  // ⭐ 出处 = 这个题目集合本身。URL 怎么解析、取题参数怎么拼，全在 drillSpec 一处（P9 第 4 步）。
  const context: DrillContext = contextFromSearch(sp);
  const listP = listParams(context);
  const mode = listP.mode;

  let page, bank, tags;
  try {
    [page, bank, tags] = await Promise.all([
      listQuestions(slug, { ...listP, limit: 1, offset: index }),
      getBank(slug),
      listAxisTags(slug),
    ]);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const label = describeContext(context, {
    meta: bank.meta, tags, locale,
    t: (k, v) => dash(k as never, v as never),
    mode: modeT as never,
  });

  // done = 本轮已完成几题。⚠️ 它有两个用途，缺一不可：
  //  · 会缩短的题目集合里 i 恒为 0，进度只能靠它算（i 不再有意义）
  //  · 让「下一题」的 URL 与当前 URL【不同】—— 否则 Next.js 认为没有导航，
  //    页面不会重新渲染，你会以为点了没反应
  const linkTo = (i: number, done?: number) => drillHref(slug, context, { i, done });
  const shrinking = isShrinking(context);

  // ⭐ 按顺序的集合（年度 / 领域 / 随机一轮 …）走过最后一题 ⇒ 本轮小结（P9 #19）。
  //   小结按【每题最近一次】算，⛔ 不存「一轮」这个状态 —— 学习侧只存事实。
  if (page.items.length === 0 && !shrinking && page.total > 0) {
    const sum = await summarizeQuestions(slug, listP);
    const again =
      context.mode === "random"
        ? `/banks/${slug}/practice/random/start?n=${context.count ?? 10}` // 再来一轮 = 换一个种子
        : linkTo(0);
    return (
      <div className="mx-auto max-w-md space-y-6 pt-12 text-center">
        <p className="eyebrow">{label}</p>
        <p className="display text-2xl">{t("roundDone")}</p>
        <p className="display text-[2.6rem] leading-none tabular-nums">
          {sum.correct}
          <small className="text-[1.05rem] text-muted"> / {sum.total}</small>
        </p>
        <p className="text-sm text-muted">{t("roundSummary", { total: sum.total, answered: sum.answered, correct: sum.correct })}</p>
        <p className="text-xs text-muted">{t("roundNote")}</p>
        <div className="flex justify-center gap-3 pt-2">
          <Link href={again} className="rounded-md px-5 py-2.5 text-sm font-medium"
                style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}>
            {context.mode === "random" ? t("roundNewRandom") : t("roundAgain")}
          </Link>
          <Link href="/" className="rounded-md border border-line px-5 py-2.5 text-sm">{t("backHome")}</Link>
        </div>
      </div>
    );
  }

  if (page.items.length === 0) {
    // ⚠️ 复习队列空了是【好事】，不是「筛选条件没匹配到」——
    // 同一个空结果，在不同入口下含义完全相反，文案不能共用。
    const empty =
      mode === "due"
        ? page.total === 0
          ? t("doneToday")
          : t("doneBatch")
        : page.total === 0
          ? t("noMatch")
          : t("lastOne");
    return (
      <div className="space-y-5 pt-10 text-center">
        <p className="display text-xl text-muted">{empty}</p>
        {mode === "due" && page.total === 0 && (
          <p className="text-sm text-muted">
            {t("noDueHint")}
            <Link href={`/banks/${slug}/drill?mode=unseen`} className="underline underline-offset-4" style={{ color: "var(--color-src-community)" }}>
              {t("noDueLink")}
            </Link>
          </p>
        )}
        <Link
          href="/"
          className="inline-flex items-center gap-1.5 text-sm transition-colors hover:text-ink"
          style={{ color: "var(--color-src-community)" }}
        >
          <ArrowLeft size={15} />
          {t("backHome")}
        </Link>
      </div>
    );
  }

  // ⚠️ 这四个模式（due/wrong/unsure/unseen）的题目集合会随作答【缩短】
  // ——做完就离开该集合。所以「答完之后的下一题」是 offset 0 而不是 offset+1，
  // 后者会漏题且不报错。详见 DrillCard 的 nav 属性注释。
  const nav = {
    prevHref: index > 0 ? linkTo(index - 1) : undefined,
    // ⭐ 按顺序的集合在最后一题也给出口 —— 指向 offset=total，那里是本轮小结
    skipHref: index + 1 < page.total || !shrinking ? linkTo(index + 1, done) : undefined,
    // ⚠️ 会缩短的集合里【总是】给 nextHref，哪怕这是最后一题 ——
    // 答完最后一题跳到 offset 0，落到「今天的复习做完了」那个空状态，
    // 那正是这条队列应有的结尾。若这里给 undefined，答完最后一题就没有出口了。
    nextHref: shrinking ? linkTo(0, done + 1) : undefined,
    shrinking,
  };

  const q = await getQuestion(page.items[0].id);
  const reference = q.reference ?? null;
  // ⭐ 请求的语言没有译文时【明说】，⛔ 不静默把原文当译文端上来。
  // 判断本身在 needsSourceNotice 里（有测试锁住「请求源语言时不该提示」这条）。
  const untranslated = needsSourceNotice(q.sourceLocale, locale, q.localized);

  return (
    <div className="space-y-8">
      <header className="flex flex-wrap items-baseline gap-x-4 gap-y-2 text-sm">
        <Link
          href="/"
          className="inline-flex items-center gap-1.5 font-mono text-xs text-muted transition-colors hover:text-ink"
        >
          <ArrowLeft size={13} />
          {slug}
        </Link>
        <span className="tabular-nums text-xs text-muted">
          {index + 1} / {page.total}
        </span>
        {context.mode !== "all" && (
          <span className="rounded-sm border border-line px-1.5 py-0.5 text-[0.65rem] text-muted">
            {label}
          </span>
        )}
        <span className="display text-lg" style={{ fontFamily: "var(--font-mono-x)" }}>
          #{q.externalNo}
        </span>
        {q.contested && (
          <span className="inline-flex items-center gap-1 text-xs font-medium" style={{ color: "var(--color-warn)" }}>
            <AlertTriangle size={12} />
            {t("contested")}
          </span>
        )}
        {/* ⛔ 这里曾经有一个「详情页」链接，已删。
            理由不是它不好，是它在刷题流里【什么都不提供】：详情页渲染的是
            QuestionBody + ClaimsPanel + 解析 + 标签 —— 与揭晓后完全相同，
            只是少了自评。点进去看不到新东西，而它头上的 ← 会把你扔到学习台，
            刷题位置全丢。⚠️ 一个内容为零、代价是丢失位置的出口 = 陷阱。
            详情页本身保留（错题本 / 标签列表 / 分享单题的固定链接要用），
            只是不该出现在刷题过程中。2026-09-05 用户实测报告。 */}
      </header>

      {/* 进度条：hairline 轨道 + 墨色进度（dataviz：数据是唯一允许大声的东西） */}
      {/* ⚠️ 会缩短的集合（due/wrong/unsure/unseen）里 index 恒为 0 而 total 一直在减，
          用 (index+1)/total 画出来的进度会【往回走】。这类模式改用
          已完成 / (已完成 + 剩余) —— 分母随作答自然增长，进度单调向前。 */}
      <div className="h-[3px] overflow-hidden rounded-full bg-line" role="progressbar"
           aria-valuenow={shrinking ? done : index + 1} aria-valuemin={0}
           aria-valuemax={shrinking ? done + page.total : page.total}>
        <div
          className="h-full rounded-full transition-all"
          style={{
            width: `${shrinking
              ? (done / Math.max(done + page.total, 1)) * 100
              : ((index + 1) / page.total) * 100}%`,
            background: "var(--color-ink)",
          }}
        />
      </div>

      {untranslated && (
        <p className="rounded-md border border-line bg-raise px-4 py-2.5 text-xs text-muted"
           style={{ boxShadow: "inset 3px 0 0 var(--color-src-bank)" }}>
          {t("sourceFallback")}
        </p>
      )}

      {/* ⛔ key 不能省：不加它 React 会复用同一个 DrillCard 实例，
          上一题的 picked / revealed / saveState 全都留到下一题 ——
          表现就是「下一题默认选中了上一题的选项」。2026-09-05 实测。 */}
      <DrillCard key={q.id} questionId={q.id} stem={q.stem} choices={q.choices} pickCount={q.pickCount} reference={reference} context={context} nav={nav}>
        <ClaimsPanel claims={q.claims} />
        {pickExplanations(q.explanations, locale).map((e) => (
          <section key={`${e.source}-${e.locale}`}>
            <h3 className="section-rule">
              <span className="eyebrow">{t("explanation")}</span>
            </h3>
            <div className="mt-5 rounded-md border border-line bg-raise p-6 sm:p-8">
              <Markdown>{e.body}</Markdown>
            </div>
          </section>
        ))}
        {q.explanations.length === 0 && (
          <p className="text-xs leading-relaxed text-muted">{t("noExplanation")}</p>
        )}
        {q.tags.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {q.tags.map((t) => (
              <TagChip key={t.id} tag={t} slug={slug} />
            ))}
          </div>
        )}
      </DrillCard>

    </div>
  );
}
