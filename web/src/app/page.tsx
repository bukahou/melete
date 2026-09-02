import Link from "next/link";
import { ArrowRight, Play } from "lucide-react";
import {
  getBank,
  getMyProgress,
  getMyResume,
  getMyTagStats,
  listBankTags,
  listBanks,
  type BankDetail,
  type FocusCursor,
  type Tag,
  type TagStat,
} from "@/lib/api";
import { tagName, tagTypeLabel, tagWeight } from "@/lib/claims";
import { rateColor } from "@/components/RateBar";

export const revalidate = 0;

/**
 * 首页 = 学习台。原则只有一条：**每个模块都是入口，不是事实**。
 * 页面上没有任何「只能看」的东西 —— 数字回答「有多少在等我」，点下去就是动作。
 *
 * 四个面板：继续学习（两条轨道）· 按我的状态 · 考纲轴 · 知识对象轴 · 题库。
 * 轴的名字、权重、及格线全部来自 bank.meta，这一页对 AWS 一无所知。
 * 新用户不需要另一套页面：0 作答时各格子自然是 0 / 全部 / —，布局自己就是空状态。
 */

const MODE_LABEL: Record<string, string> = {
  unseen: "没做过的",
  wrong: "做错的",
  unsure: "不确定的",
  contested: "分歧题",
  all: "全部",
  tag: "专项",
};

function timeAgo(iso?: string): string {
  if (!iso) return "";
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} 分钟前`;
  if (s < 86400) return `${Math.round(s / 3600)} 小时前`;
  if (s < 86400 * 7) return `${Math.round(s / 86400)} 天前`;
  return new Date(iso).toLocaleDateString("zh-CN", { month: "numeric", day: "numeric" });
}

/** 面板：统一的外框 + 标题行。层级只在「继续学习」里抬高一处（实心按钮），其余靠 hairline。 */
function Panel({
  title,
  side,
  className = "",
  children,
}: {
  title: string;
  side?: React.ReactNode;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <section className={`rounded-md border border-line bg-raise ${className}`}>
      <div className="flex items-baseline gap-4 border-b border-line px-5 py-3.5">
        <span className="eyebrow">{title}</span>
        {side && <span className="ml-auto text-xs text-muted">{side}</span>}
      </div>
      {children}
    </section>
  );
}

/** 标签 ⋈ 正确率：把「题库有多少」和「我做了多少、对多少」拼成一行。 */
type TagRow = { tag: Tag; done: number; rate: number | null; weight?: number };

function mergeTagRows(tags: Tag[], stats: TagStat[], meta: BankDetail["meta"]): TagRow[] {
  const byId = new Map(stats.map((s) => [s.tagId, s]));
  return tags.map((tag) => {
    const s = byId.get(tag.id);
    return {
      tag,
      done: s?.total ?? 0,
      rate: s && s.total > 0 ? s.rate : null,
      weight: tagWeight(meta, tag),
    };
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

export default async function HomePage() {
  const banks = await listBanks();
  const details = await Promise.all(banks.map((b) => getBank(b.slug)));
  if (details.length === 0) return <p className="text-sm text-muted">还没有题库。</p>;

  const progress = await getMyProgress();
  const bank = details.find((b) => b.slug === progress.bankSlug) ?? details[0];
  const slug = bank.slug;
  const meta = bank.meta;

  const [resume, domainTags, topicTags, domainStats, topicStats] = await Promise.all([
    getMyResume(slug),
    listBankTags(slug, "domain"),
    listBankTags(slug, "topic"),
    getMyTagStats("domain", 1),
    getMyTagStats("topic", 1),
  ]);

  const unseen = progress.questionCount - progress.seenCount;
  const rate = progress.seenCount ? Math.round((progress.correctCount / progress.seenCount) * 100) : null;
  const seq = resume.sequential;
  const focus = resume.focus;

  const domains = weakestFirst(mergeTagRows(domainTags, domainStats, meta), (a, b) =>
    a.tag.value.localeCompare(b.tag.value),
  );
  const topics = weakestFirst(mergeTagRows(topicTags, topicStats, meta), (a, b) =>
    (b.tag.questionCount ?? 0) - (a.tag.questionCount ?? 0),
  );
  const topicLabel = tagTypeLabel(meta, "topic");
  const domainLabel = tagTypeLabel(meta, "domain");
  const passLine = meta.passScore && meta.maxScore ? Math.round((meta.passScore / meta.maxScore) * 100) : null;

  const tiles = [
    { key: "wrong", label: MODE_LABEL.wrong, n: progress.wrongCount, hint: "最近一次做错", dot: "var(--color-warn)", href: `/banks/${slug}/drill?mode=wrong` },
    { key: "unsure", label: MODE_LABEL.unsure, n: progress.unsureCount, hint: "自评「模糊」或「不会」", dot: "var(--color-src-bank)", href: `/banks/${slug}/drill?mode=unsure` },
    { key: "unseen", label: MODE_LABEL.unseen, n: unseen, hint: "按题号顺序", dot: "var(--color-muted)", href: `/banks/${slug}/drill?mode=unseen` },
    { key: "contested", label: MODE_LABEL.contested, n: bank.stats.contestedCount, hint: "题库与社区答案不一致", dot: "var(--color-src-community)", href: `/banks/${slug}/drill?contested=true` },
  ];

  return (
    // 突破 layout 的阅读宽度：学习台是扫视和操作的，需要宽画布
    <div className="relative left-1/2 w-[min(100vw,1400px)] -translate-x-1/2 px-6">
      <div className="grid grid-cols-12 items-start gap-5">
        {/* ---- 继续学习：两条轨道 ---- */}
        <Panel
          title="继续学习"
          className="col-span-12 lg:col-span-8"
          side={
            <>
              <b className="text-ink">{progress.seenCount}</b> / {progress.questionCount} 做过
              {rate != null && (
                <>
                  {" · "}正确率 <b style={{ color: rateColor(rate) }}>{rate}%</b>
                </>
              )}
            </>
          }
        >
          <div className="grid md:grid-cols-2">
            {/* 顺序进度 —— 题号最小的没做过的题，无需存状态 */}
            <div className="grid min-h-[190px] grid-rows-[auto_1fr_auto] gap-2.5 p-5">
              <div className="flex items-center gap-2 text-xs text-muted">
                <span className="h-2 w-2 rounded-full bg-ink" aria-hidden />
                顺序进度
                <span className="ml-auto font-mono">{seq.lastAt ? `上次 ${timeAgo(seq.lastAt)}` : "尚未开始"}</span>
              </div>
              <div>
                {seq.questionId != null ? (
                  <>
                    <div className="display text-2xl leading-tight">
                      <span className="tabular-nums">#{seq.externalNo}</span>
                      <span className="ml-1.5 text-base text-muted">/ {seq.totalCount}</span>
                    </div>
                    <p className="mt-2 line-clamp-2 text-[0.86rem] leading-relaxed text-muted">{seq.stem}</p>
                  </>
                ) : (
                  <>
                    <div className="display text-2xl leading-tight text-muted">已全部做过一遍</div>
                    <p className="mt-2 text-[0.86rem] leading-relaxed text-muted">接下来从右边的错题与不确定的题里巩固。</p>
                  </>
                )}
              </div>
              <div className="flex items-center gap-4">
                <div className="h-1 flex-1 overflow-hidden rounded-sm bg-line">
                  <span className="block h-full rounded-sm bg-ink" style={{ width: `${(seq.doneCount / Math.max(seq.totalCount, 1)) * 100}%` }} />
                </div>
                <span className="w-10 text-right font-mono text-xs text-muted tabular-nums">
                  {Math.round((seq.doneCount / Math.max(seq.totalCount, 1)) * 100)}%
                </span>
                {seq.questionId != null && (
                  <Link
                    href={`/banks/${slug}/drill?mode=unseen`}
                    className="inline-flex items-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium"
                    style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
                  >
                    <Play size={12} />
                    {seq.doneCount === 0 ? `开始 #${seq.externalNo}` : `继续 #${seq.externalNo}`}
                  </Link>
                )}
              </div>
            </div>

            {/* 上次专项 —— 唯一需要记的东西：attempt.context */}
            <div className="grid min-h-[190px] grid-rows-[auto_1fr_auto] gap-2.5 border-t border-line p-5 md:border-l md:border-t-0">
              <div className="flex items-center gap-2 text-xs text-muted">
                <span className="h-2 w-2 rounded-full" style={{ background: "var(--color-src-bank)" }} aria-hidden />
                上次专项
                {focus && <span className="ml-auto font-mono">{timeAgo(focus.lastAt)}</span>}
              </div>
              {focus ? (
                <>
                  <div>
                    <div className="display text-2xl leading-tight">
                      {focus.tag ? tagName(focus.tag) : MODE_LABEL[focus.context.mode] ?? focus.label}
                      {focus.tag && (
                        <span className="ml-2 text-base text-muted">· {tagTypeLabel(meta, focus.tag.type)}</span>
                      )}
                    </div>
                    <p className="mt-2 text-[0.86rem] leading-relaxed text-muted">
                      {focus.done != null
                        ? `${focus.total} 题中做了 ${focus.done}。`
                        : `当前 ${focus.total} 题。`}
                    </p>
                  </div>
                  <div className="flex items-center gap-4">
                    <div className="h-1 flex-1 overflow-hidden rounded-sm bg-line">
                      {focus.done != null && (
                        <span
                          className="block h-full rounded-sm"
                          style={{ width: `${(focus.done / Math.max(focus.total, 1)) * 100}%`, background: "var(--color-src-bank)" }}
                        />
                      )}
                    </div>
                    <span className="w-14 text-right font-mono text-xs text-muted tabular-nums">
                      {focus.done != null ? `${focus.done}/${focus.total}` : focus.total}
                    </span>
                    <Link
                      href={focusHref(slug, focus)}
                      className="inline-flex items-center gap-2 rounded-md border border-ink px-4 py-2.5 text-sm font-medium"
                    >
                      <Play size={12} />
                      继续
                    </Link>
                  </div>
                </>
              ) : (
                <>
                  <div>
                    <div className="display text-2xl leading-tight text-muted">还没有专项</div>
                    <p className="mt-2 text-[0.86rem] leading-relaxed text-muted">
                      从下方任何一项点进去开始专项，这里会记住你上次刷的是什么。
                    </p>
                  </div>
                  <span className="text-xs text-muted">↓ 选一个专项</span>
                </>
              )}
            </div>
          </div>
        </Panel>

        {/* ---- 按我的状态：2×2 ---- */}
        <Panel title="按我的状态" side="数字 = 有多少在等我" className="col-span-12 lg:col-span-4">
          <div className="grid grid-cols-2">
            {tiles.map((t, i) => (
              <Link
                key={t.key}
                href={t.href}
                className={`group relative grid gap-0.5 px-5 py-4 transition-colors hover:bg-surface ${i % 2 === 0 ? "border-r border-line" : ""} ${i < 2 ? "border-b border-line" : ""}`}
              >
                <span className="flex items-center gap-2 text-xs text-muted">
                  <span className="h-2 w-2 rounded-full" style={{ background: t.dot }} aria-hidden />
                  {t.label}
                </span>
                <span className={`display mt-1 text-[1.9rem] leading-tight tabular-nums ${t.n === 0 ? "text-muted" : ""}`}>{t.n}</span>
                <span className="text-[0.72rem] text-muted">{t.hint}</span>
                <ArrowRight size={14} className="absolute right-4 top-4 text-muted opacity-0 transition-opacity group-hover:opacity-100" />
              </Link>
            ))}
          </div>
        </Panel>

        {/* ---- 考纲轴 ---- */}
        <Panel
          title={domainLabel}
          side={passLine != null ? `及格线 ${meta.passScore}/${meta.maxScore}` : undefined}
          className="col-span-12 lg:col-span-4"
        >
          <div>
            {domains.map((r) => (
              <Link
                key={r.tag.id}
                href={`/banks/${slug}/drill?tag=${r.tag.id}`}
                className="grid grid-cols-[1.6rem_1fr_auto] items-center gap-3 border-b border-line px-5 py-3.5 transition-colors last:border-b-0 hover:bg-surface"
              >
                <span className="font-mono text-[0.7rem] text-muted">{r.tag.value.replace(/^domain-/, "D")}</span>
                <span>
                  <span className="text-[0.9rem]">
                    {tagName(r.tag)}
                    {r.weight != null && <span className="ml-1.5 font-mono text-[0.68rem] text-muted">{r.weight}%</span>}
                  </span>
                  <span className="block text-[0.72rem] text-muted">
                    {r.tag.questionCount} 题 · 做过 <span className="tabular-nums">{r.done}</span>
                  </span>
                  <span className="mt-1.5 block h-1 overflow-hidden rounded-sm bg-line">
                    {r.rate != null && (
                      <span className="block h-full rounded-sm" style={{ width: `${r.rate}%`, background: rateColor(r.rate) }} />
                    )}
                  </span>
                </span>
                <span className={`display min-w-12 text-right text-lg tabular-nums ${r.rate == null ? "text-muted" : ""}`}>
                  {r.rate != null ? `${Math.round(r.rate)}%` : "—"}
                </span>
              </Link>
            ))}
          </div>
          <p className="border-t border-line px-5 py-3 text-[0.72rem] leading-relaxed text-muted">
            灰色百分比 = 官方权重。正确率低的排前面
            {passLine != null && <>；四条都过 {passLine}% 才算稳</>}。
          </p>
        </Panel>

        {/* ---- 知识对象轴：表格，宽度真正派上用场的地方 ---- */}
        <Panel
          title={topicLabel}
          side={
            <>
              {topicTags.length} 个 · 正确率低的排前面 ·{" "}
              <Link href={`/banks/${slug}`} className="text-src-community">
                全部 →
              </Link>
            </>
          }
          className="col-span-12 lg:col-span-8"
        >
          <div className="overflow-x-auto">
            <table className="w-full text-[0.86rem]">
              <thead>
                <tr className="border-b border-line text-left text-[0.68rem] uppercase tracking-[0.12em] text-muted">
                  <th className="px-5 py-2.5 font-medium">{topicLabel}</th>
                  <th className="px-5 py-2.5 text-right font-medium">题量</th>
                  <th className="px-5 py-2.5 text-right font-medium">做过</th>
                  <th className="w-[38%] px-5 py-2.5 font-medium">正确率</th>
                </tr>
              </thead>
              <tbody>
                {topics.slice(0, 10).map((r) => (
                  <tr key={r.tag.id} className="border-b border-line transition-colors last:border-b-0 hover:bg-surface">
                    <td className="px-5 py-2.5 font-medium">
                      <Link href={`/banks/${slug}/drill?tag=${r.tag.id}`} className="block">
                        {tagName(r.tag)}
                      </Link>
                    </td>
                    <td className="px-5 py-2.5 text-right font-mono text-xs text-muted tabular-nums">{r.tag.questionCount}</td>
                    <td className="px-5 py-2.5 text-right font-mono text-xs text-muted tabular-nums">{r.done}</td>
                    <td className="px-5 py-2.5">
                      <div className="grid grid-cols-[1fr_3rem] items-center gap-3">
                        <div className="h-1.5 overflow-hidden rounded-sm bg-line">
                          {r.rate != null && (
                            <span className="block h-full rounded-sm" style={{ width: `${r.rate}%`, background: rateColor(r.rate) }} />
                          )}
                        </div>
                        <span
                          className="text-right font-mono text-xs tabular-nums"
                          style={{ color: r.rate == null ? "var(--color-muted)" : rateColor(r.rate) }}
                        >
                          {r.rate != null ? `${Math.round(r.rate)}%` : "—"}
                        </span>
                      </div>
                    </td>
                  </tr>
                ))}
                {topics.length > 10 && (
                  <tr>
                    <td colSpan={4} className="px-5 py-3 text-center text-xs">
                      <Link href={`/banks/${slug}`} className="text-src-community">
                        再看 {topics.length - 10} 个 →
                      </Link>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </Panel>

        {/* ---- 题库：横卡，也是切换入口 ---- */}
        <div className="col-span-12 grid gap-5 md:grid-cols-2">
          {details.map((b) => {
            const current = b.slug === slug;
            const done = current ? progress.seenCount : 0;
            return (
              <Link
                key={b.slug}
                href={`/banks/${b.slug}`}
                className="grid items-center gap-5 rounded-md border border-line bg-raise px-5 py-4 transition-colors hover:border-muted sm:grid-cols-[1fr_200px]"
              >
                <span>
                  <h3 className="display text-[1.05rem] leading-snug">
                    {b.name}
                    {current && (
                      <span className="ml-2 inline-block translate-y-[-2px] rounded-sm border border-current px-1.5 text-[0.62rem] uppercase tracking-[0.1em]" style={{ color: "var(--color-src-bank)" }}>
                        当前
                      </span>
                    )}
                  </h3>
                  <span className="mt-1 block font-mono text-[0.7rem] text-muted">
                    {b.slug} · {b.locale} · {b.stats.questionCount} 题 · 已解析 {b.stats.enrichedCount} · 分歧 {b.stats.contestedCount}
                  </span>
                </span>
                <span>
                  <span className="flex justify-between text-[0.72rem] text-muted">
                    <span>做过</span>
                    <b className="text-ink tabular-nums">
                      {done} / {b.stats.questionCount}
                    </b>
                  </span>
                  <span className="mt-1.5 block h-1 overflow-hidden rounded-sm bg-line">
                    <span className="block h-full rounded-sm bg-ink" style={{ width: `${(done / Math.max(b.stats.questionCount, 1)) * 100}%` }} />
                  </span>
                </span>
              </Link>
            );
          })}
        </div>
      </div>
    </div>
  );
}
