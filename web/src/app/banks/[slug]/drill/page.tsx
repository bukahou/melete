import Link from "next/link";
import { notFound } from "next/navigation";
import { AlertTriangle, ArrowLeft } from "lucide-react";
import { ApiError, getQuestion, listQuestions, parseDrillMode, type DrillContext, type DrillMode } from "@/lib/api";
import { ClaimsPanel } from "@/components/ClaimsPanel";
import { DrillCard } from "@/components/DrillCard";
import { Markdown } from "@/components/Markdown";
import { TagChip } from "@/components/TagChip";

type Search = {
  i?: string;
  contested?: string;
  enriched?: string;
  mode?: string;
  done?: string;
  tag?: string | string[];
};

const MODE_TITLE: Record<DrillMode, string> = {
  due: "今日复习",
  wrong: "错题本",
  unsure: "不清楚的",
  unseen: "没做过的",
};

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

  const index = Math.max(0, Number(sp.i ?? 0) || 0);
  const done = Math.max(0, Number(sp.done ?? 0) || 0);
  const tags = (Array.isArray(sp.tag) ? sp.tag : sp.tag ? [sp.tag] : []).map(Number).filter(Boolean);
  const mode = parseDrillMode(sp.mode);
  const filters = {
    contested: sp.contested === "true",
    enriched: sp.enriched === "true",
    tags,
    mode,
  };

  let page;
  try {
    page = await listQuestions(slug, { ...filters, limit: 1, offset: index });
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  // done = 本轮已完成几题。⚠️ 它有两个用途，缺一不可：
  //  · 会缩短的题目集合里 i 恒为 0，进度只能靠它算（i 不再有意义）
  //  · 让「下一题」的 URL 与当前 URL【不同】—— 否则 Next.js 认为没有导航，
  //    页面不会重新渲染，你会以为点了没反应
  const linkTo = (i: number, done?: number) => {
    const q = new URLSearchParams();
    q.set("i", String(i));
    if (done != null) q.set("done", String(done));
    if (filters.contested) q.set("contested", "true");
    if (filters.enriched) q.set("enriched", "true");
    if (mode) q.set("mode", mode);
    tags.forEach((t) => q.append("tag", String(t)));
    return `/banks/${slug}/drill?${q}`;
  };

  if (page.items.length === 0) {
    // ⚠️ 复习队列空了是【好事】，不是「筛选条件没匹配到」——
    // 同一个空结果，在不同入口下含义完全相反，文案不能共用。
    const empty =
      mode === "due"
        ? page.total === 0
          ? "今天的复习做完了"
          : "这一批复习做完了"
        : page.total === 0
          ? "当前筛选条件下没有题目"
          : "已经是最后一题了";
    return (
      <div className="space-y-5 pt-10 text-center">
        <p className="display text-xl text-muted">{empty}</p>
        {mode === "due" && page.total === 0 && (
          <p className="text-sm text-muted">
            没有到期的题。要往前推进，去做<Link href={`/banks/${slug}/drill?mode=unseen`} className="underline underline-offset-4" style={{ color: "var(--color-src-community)" }}>没做过的</Link>。
          </p>
        )}
        <Link
          href={`/banks/${slug}`}
          className="inline-flex items-center gap-1.5 text-sm transition-colors hover:text-ink"
          style={{ color: "var(--color-src-community)" }}
        >
          <ArrowLeft size={15} />
          返回题库
        </Link>
      </div>
    );
  }

  // ⚠️ 这四个模式（due/wrong/unsure/unseen）的题目集合会随作答【缩短】
  // ——做完就离开该集合。所以「答完之后的下一题」是 offset 0 而不是 offset+1，
  // 后者会漏题且不报错。详见 DrillCard 的 nav 属性注释。
  const shrinking = mode != null;
  const nav = {
    prevHref: index > 0 ? linkTo(index - 1) : undefined,
    skipHref: index + 1 < page.total ? linkTo(index + 1, done) : undefined,
    // ⚠️ 会缩短的集合里【总是】给 nextHref，哪怕这是最后一题 ——
    // 答完最后一题跳到 offset 0，落到「今天的复习做完了」那个空状态，
    // 那正是这条队列应有的结尾。若这里给 undefined，答完最后一题就没有出口了。
    nextHref: shrinking ? linkTo(0, done + 1) : undefined,
    shrinking,
  };

  const q = await getQuestion(page.items[0].id);
  const reference = q.reference ?? null;

  // 出处：用户是从哪个入口进来做这题的。mode 优先；多标签时记第一个（入口只会传一个）。
  // unseen 与不带条件的 all 视同「顺序刷」，其余都是专项。
  const context: DrillContext = mode
    ? { mode }
    : tags.length > 0
      ? { mode: "tag", tagId: tags[0] }
      : filters.contested
        ? { mode: "contested" }
        : { mode: "all" };

  return (
    <div className="space-y-8">
      <header className="flex flex-wrap items-baseline gap-x-4 gap-y-2 text-sm">
        <Link
          href={`/banks/${slug}`}
          className="inline-flex items-center gap-1.5 font-mono text-xs text-muted transition-colors hover:text-ink"
        >
          <ArrowLeft size={13} />
          {slug}
        </Link>
        <span className="tabular-nums text-xs text-muted">
          {index + 1} / {page.total}
        </span>
        {mode && (
          <span className="rounded-sm border border-line px-1.5 py-0.5 text-[0.65rem] text-muted">
            {MODE_TITLE[mode]}
          </span>
        )}
        <span className="display text-lg" style={{ fontFamily: "var(--font-mono-x)" }}>
          #{q.externalNo}
        </span>
        {q.contested && (
          <span className="inline-flex items-center gap-1 text-xs font-medium" style={{ color: "var(--color-warn)" }}>
            <AlertTriangle size={12} />
            答案有分歧
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

      {/* ⛔ key 不能省：不加它 React 会复用同一个 DrillCard 实例，
          上一题的 picked / revealed / saveState 全都留到下一题 ——
          表现就是「下一题默认选中了上一题的选项」。2026-09-05 实测。 */}
      <DrillCard key={q.id} questionId={q.id} stem={q.stem} choices={q.choices} pickCount={q.pickCount} reference={reference} context={context} nav={nav}>
        <ClaimsPanel claims={q.claims} />
        {q.explanations.map((e) => (
          <section key={`${e.source}-${e.locale}`}>
            <h3 className="section-rule">
              <span className="eyebrow">解析</span>
            </h3>
            <div className="mt-5 rounded-md border border-line bg-raise p-6 sm:p-8">
              <Markdown>{e.body}</Markdown>
            </div>
          </section>
        ))}
        {q.explanations.length === 0 && (
          <p className="text-xs leading-relaxed text-muted">
            这道题还没有 AI 解析。上方各方主张仍可对照参考。
          </p>
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
