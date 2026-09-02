import Link from "next/link";
import { CircleDashed, HelpCircle, XCircle } from "lucide-react";
import { getBank, getMyProgress, getMyTagStats } from "@/lib/api";
import { tagTypeLabel, type TagType } from "@/lib/claims";
import { RateBar, RateLegend } from "@/components/RateBar";

export const metadata = { title: "我的学习" };

/** 三条轴的角色与提示语是通用的；轴的**名字**从题库 meta 读，这里不写任何题库词。 */
const GROUPS: Array<{ type: TagType; hint: string }> = [
  { type: "domain", hint: "离及格还差多少" },
  { type: "topic", hint: "哪一块不熟" },
  { type: "concept", hint: "缺的是题库知识还是底层原理" },
];

export default async function MePage() {
  const progress = await getMyProgress();
  const [bank, ...groups] = await Promise.all([
    getBank(progress.bankSlug),
    ...GROUPS.map((g) => getMyTagStats(g.type, 2)),
  ]);

  const unseen = progress.questionCount - progress.seenCount;
  const rate = progress.seenCount ? (progress.correctCount / progress.seenCount) * 100 : 0;
  const slug = progress.bankSlug;

  return (
    <div className="space-y-14">
      <header>
        <p className="eyebrow">我的学习</p>
        <h1 className="display mt-4 text-3xl leading-tight">
          {progress.seenCount === 0 ? (
            "还没有作答记录"
          ) : (
            <>
              做过 <span className="tabular-nums">{progress.seenCount}</span> 道，
              正确率 <span style={{ color: rate < 60 ? "var(--color-warn)" : "var(--color-ok)" }}>
                {Math.round(rate)}%
              </span>
            </>
          )}
        </h1>
      </header>

      {progress.seenCount === 0 ? (
        <p className="text-sm leading-relaxed text-muted">
          先去
          <Link href={`/banks/${slug}/drill`} className="mx-1 underline underline-offset-4">
            刷几道题
          </Link>
          ，这里会显示你的进度与弱项分布。
        </p>
      ) : (
        <>
          {/* 进度条：做过 / 未做，单一分段条比三个数字更快传达"还剩多少" */}
          <section className="space-y-3">
            <div className="meter" role="img"
                 aria-label={`已做 ${progress.seenCount} 题，未做 ${unseen} 题`}>
              <span style={{ flex: progress.correctCount, background: "var(--color-ok)" }} />
              <span style={{ flex: progress.wrongCount, background: "var(--color-warn)" }} />
              <span style={{ flex: Math.max(unseen, 1), background: "var(--color-line)" }} />
            </div>
            <div className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted">
              {[
                ["答对", progress.correctCount, "var(--color-ok)"],
                ["答错", progress.wrongCount, "var(--color-warn)"],
                ["未做", unseen, "var(--color-line)"],
              ].map(([label, n, c]) => (
                <span key={label as string} className="inline-flex items-center gap-1.5">
                  <span className="h-2 w-2 rounded-full" style={{ background: c as string }} aria-hidden />
                  {label as string} <b className="tabular-nums text-ink">{n as number}</b>
                </span>
              ))}
              <span className="ml-auto">共作答 {progress.attemptCount} 次（含重做）</span>
            </div>
          </section>

          {/* 三个入口：把统计直接变成行动 */}
          <section className="flex flex-wrap gap-3">
            {[
              { mode: "wrong", label: "错题本", n: progress.wrongCount, Icon: XCircle, color: "var(--color-warn)" },
              { mode: "unsure", label: "不清楚的", n: progress.unsureCount, Icon: HelpCircle, color: "var(--color-src-bank)" },
              { mode: "unseen", label: "没做过的", n: unseen, Icon: CircleDashed, color: "var(--color-muted)" },
            ].map(({ mode, label, n, Icon, color }) => (
              <Link
                key={mode}
                href={`/banks/${slug}/drill?mode=${mode}`}
                className="inline-flex items-center gap-2 rounded-md border border-line bg-raise px-4 py-2.5 text-sm transition-colors hover:border-muted"
              >
                <Icon size={15} style={{ color }} />
                {label}
                <b className="tabular-nums">{n}</b>
              </Link>
            ))}
          </section>

          {/* 弱项分布 */}
          {GROUPS.map((g, i) => {
            const stats = groups[i];
            if (stats.length === 0) return null;
            return (
              <section key={g.type}>
                <h2 className="section-rule">
                  <span className="eyebrow">{tagTypeLabel(bank.meta, g.type)}</span>
                  <span className="text-xs text-muted">{g.hint}</span>
                </h2>
                <div className="mt-4 space-y-1">
                  {stats.map((s) => (
                    <RateBar key={s.tagId} stat={s} />
                  ))}
                </div>
                {i === 0 && (
                  <div className="mt-4">
                    <RateLegend />
                  </div>
                )}
              </section>
            );
          })}

          <p className="text-xs leading-relaxed text-muted">
            只统计每道题<b className="text-ink">最近一次</b>的作答 ——
            重做订正过的题算作已掌握，不会被首次的错拖累。
            样本少于 2 题的标签不显示（正确率没有意义）。
          </p>
        </>
      )}
    </div>
  );
}
