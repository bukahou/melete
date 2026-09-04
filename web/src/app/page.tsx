import Link from "next/link";
import { ArrowRight, Play, RotateCcw } from "lucide-react";
import {
  getBank,
  getMyOverview,
  getMyProgress,
  getMyRecent,
  getMyResume,
  listBanks,
  type BankDetail,
  type Progress,
  type Resume,
  type StudySession,
} from "@/lib/api";
import { tagName, tagTypeLabel } from "@/lib/claims";
import { rateColor } from "@/components/RateBar";
import { Band, Stat, Wide } from "@/components/Band";
import { MODE_LABEL, timeAgo } from "@/lib/format";

export const revalidate = 0;

/**
 * 首页 = 题库选择台。只做一件事：选一个题库进去。
 * 每张卡就是一扇门，门上写着我在里面走到哪了：进度、两条轨道、三个状态计数。
 * 「继续」按钮直接在卡上 —— 老用户一步进题，不必先进学习台。
 * 层级只用两样东西：数字与留白。卡的右上角是最重要的数字（做过 / 总数）。
 */

type BankCard = { bank: BankDetail; progress: Progress; resume: Resume };

function levelOf(meta: BankDetail["meta"], name: string): string | undefined {
  // 等级章：meta 有就用；否则从名字里认（Associate / Professional / Level 1…）
  const m = meta as { level?: string };
  if (m.level) return m.level;
  const hit = /(Associate|Professional|Specialty|Foundational|Practitioner|Level \d)/i.exec(name);
  return hit?.[1];
}

function Card({ bank, progress, resume }: BankCard) {
  const slug = bank.slug;
  const meta = bank.meta;
  const total = bank.stats.questionCount;
  const fresh = progress.seenCount === 0;
  const rate = progress.seenCount ? Math.round((progress.correctCount / progress.seenCount) * 100) : null;
  const seq = resume.sequential;
  const focus = resume.focus;
  const level = levelOf(meta, bank.name);
  const unseen = total - progress.seenCount;
  const due = progress.dueCount;

  return (
    <article className={`overflow-hidden rounded-lg border border-line bg-raise transition-colors hover:border-muted ${fresh ? "fresh" : ""}`}>
      {/* 身份 + 大数字 */}
      <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-6 px-7 pt-6 pb-5">
        <div>
          <div className="mb-3.5 flex gap-2">
            {level && <span className="chip chip-level">{level}</span>}
            <span className="chip">{bank.locale}</span>
          </div>
          <h2 className="display text-[1.45rem] leading-[1.25]">{bank.name.replace(/\s*\((?:SAA|SAP|[A-Z]{2,4})-[A-Z0-9]+\)\s*$/, "")}</h2>
          <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[0.82rem] text-muted">
            <span><b className="font-semibold text-ink">{slug.toUpperCase().replace(/^AWS-/, "")}</b></span>
            <span><b className="font-semibold text-ink tabular-nums">{total}</b> 题</span>
            {meta.passScore != null && meta.maxScore != null && (
              <span>及格 <b className="font-semibold text-ink tabular-nums">{meta.passScore}</b>/{meta.maxScore}</span>
            )}
            <span>答案有分歧 <b className="font-semibold text-ink tabular-nums">{bank.stats.contestedCount}</b></span>
          </div>
        </div>
        <div className="text-right">
          <div className={`display text-[2.6rem] leading-none tabular-nums ${fresh ? "text-muted" : ""}`}>
            {progress.seenCount}
            <small className="text-[1.05rem] text-muted"> / {total}</small>
          </div>
          <div className="mt-1.5 text-[0.74rem] text-muted">
            {fresh ? "还没开始" : <>做过 · 正确率 <b className="font-semibold" style={{ color: rateColor(rate ?? 0) }}>{rate}%</b></>}
          </div>
        </div>
      </div>
      <div className="px-7 pb-5">
        <div className="h-[5px] overflow-hidden rounded-sm bg-line">
          <span className="block h-full rounded-sm bg-ink" style={{ width: `${(progress.seenCount / Math.max(total, 1)) * 100}%` }} />
        </div>
      </div>

      {/* 两条轨道：两张票根 */}
      <div className="grid border-y border-line bg-surface/60 md:grid-cols-2">
        <div className="grid min-w-0 gap-1.5 px-7 py-4">
          <div className="flex items-center gap-2 text-[0.72rem] uppercase tracking-[0.1em] text-muted">
            <span className="dot bg-ink" />顺序进度
            {seq.lastAt && <time className="ml-auto font-mono normal-case tracking-normal">{timeAgo(seq.lastAt)}</time>}
          </div>
          {seq.questionId != null ? (
            <>
              <div className={`display text-[1.25rem] leading-tight tabular-nums ${fresh ? "text-muted" : ""}`}>
                {fresh ? "从第 1 题开始" : <>#{seq.externalNo} <small className="font-sans text-[0.85rem] font-normal text-muted">/ {total}</small></>}
              </div>
              <div className="truncate text-[0.8rem] text-muted">{fresh ? `${total} 题按题号顺序` : seq.stem}</div>
            </>
          ) : (
            <>
              <div className="display text-[1.05rem] text-muted">已全部做过一遍</div>
              <div className="truncate text-[0.8rem] text-muted">从错题与不确定的题里巩固</div>
            </>
          )}
        </div>
        <div className="grid min-w-0 gap-1.5 border-t border-line px-7 py-4 md:border-l md:border-t-0">
          <div className="flex items-center gap-2 text-[0.72rem] uppercase tracking-[0.1em] text-muted">
            <span className="dot" style={{ background: "var(--color-src-bank)" }} />上次专项
            {focus && <time className="ml-auto font-mono normal-case tracking-normal">{timeAgo(focus.lastAt)}</time>}
          </div>
          {focus ? (
            <>
              <div className="display text-[1.25rem] leading-tight">
                {focus.tag ? tagName(focus.tag) : MODE_LABEL[focus.context.mode] ?? focus.label}
                {focus.tag && <small className="font-sans text-[0.85rem] font-normal text-muted"> · {tagTypeLabel(meta, focus.tag.type)}</small>}
              </div>
              <div className="truncate text-[0.8rem] text-muted">
                {focus.done != null ? `${focus.total} 题中做了 ${focus.done}` : `当前 ${focus.total} 题`}
              </div>
            </>
          ) : (
            <>
              <div className="display text-[1.05rem] text-muted">还没有</div>
              <div className="truncate text-[0.8rem] text-muted">进学习台，从{tagTypeLabel(meta, "domain")}或{tagTypeLabel(meta, "topic")}里选一个</div>
            </>
          )}
        </div>
      </div>

      {/* 动作行 */}
      <div className="grid items-center gap-4 px-7 pb-6 pt-5 sm:grid-cols-[auto_1fr_auto]">
        <div className="flex flex-wrap gap-2.5">
          {/* 有题到期时，复习是主行动 —— 间隔重复的全部意义就是「先做快忘的」。
              新题永远做得完，而错过的复习窗口补不回来。 */}
          {due > 0 && (
            <Link href={`/banks/${slug}/drill?mode=due`} className="inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-[0.9rem] font-semibold" style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}>
              <RotateCcw size={13} />
              复习 {due} 题
            </Link>
          )}
          {seq.questionId != null && (
            <Link
              href={`/banks/${slug}/drill?mode=unseen`}
              className={`inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-[0.9rem] font-semibold ${due > 0 ? "border border-ink" : ""}`}
              style={due > 0 ? undefined : { background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
            >
              <Play size={12} />
              {fresh ? `开始 #${seq.externalNo}` : `继续 #${seq.externalNo}`}
            </Link>
          )}
          {focus && due === 0 && (
            <Link href={focusHref(slug, focus.context)} className="inline-flex items-center gap-2 rounded-md border border-ink px-5 py-2.5 text-[0.9rem] font-semibold">
              继续 {focus.tag ? tagName(focus.tag) : MODE_LABEL[focus.context.mode]}
            </Link>
          )}
        </div>
        <div className="flex justify-center gap-5 text-[0.8rem] text-muted">
          <span>做错<b className="display ml-1 text-base text-ink">{progress.wrongCount}</b></span>
          <span>不确定<b className="display ml-1 text-base text-ink">{progress.unsureCount}</b></span>
          <span>没做过<b className="display ml-1 text-base text-ink">{unseen}</b></span>
          {/* ⚠️ 到期与「没做过」是两个不相交的集合：没做过的题没有卡片，不算到期。
              合成一个数字会让「今天要复习 300 题」失去意义 —— 那是间隔重复最劝退的失败模式。 */}
          <span>该复习<b className="display ml-1 text-base" style={{ color: due > 0 ? "var(--color-warn)" : undefined }}>{due}</b></span>
        </div>
        <Link href={`/banks/${slug}`} className="inline-flex items-center gap-1 text-[0.88rem] text-src-community">
          进入学习台 <ArrowRight size={14} />
        </Link>
      </div>
    </article>
  );
}

function focusHref(slug: string, c: { mode: string; tagId?: number | null }): string {
  if (c.mode === "tag" && c.tagId != null) return `/banks/${slug}/drill?tag=${c.tagId}`;
  if (c.mode === "contested") return `/banks/${slug}/drill?contested=true`;
  return `/banks/${slug}/drill?mode=${c.mode}`;
}

function sessionHref(s: StudySession): string {
  return focusHref(s.bankSlug, s.context);
}

function sessionLabel(s: StudySession): string {
  if (s.tag) return `${s.tag.type === "domain" ? "考纲" : "专项"} · ${tagName(s.tag)}`;
  if ((s.context.mode === "unseen" || s.context.mode === "all") && s.firstNo != null) {
    return s.lastNo != null && s.lastNo !== s.firstNo ? `顺序 · #${s.firstNo}–#${s.lastNo}` : `顺序 · #${s.firstNo}`;
  }
  return MODE_LABEL[s.context.mode] ?? s.label;
}

export default async function HomePage() {
  const banks = await listBanks();
  const [overview, recent, ...cards] = await Promise.all([
    getMyOverview(),
    getMyRecent(5),
    ...banks.map(async (b) => {
      const [bank, progress, resume] = await Promise.all([getBank(b.slug), getMyProgress(b.slug), getMyResume(b.slug)]);
      return { bank, progress, resume } as BankCard;
    }),
  ]);

  return (
    <Wide>
      <Band title="题库" sub="选一个进去。卡上写着你在里面走到哪了。">
        <Stat value={overview.todayCount} unit="题" label="今天" />
        <Stat value={overview.streakDays} unit="天" label="连续" />
        <Stat value={overview.seenTotal} unit="题" label="累计" />
      </Band>

      <div className="grid gap-6 lg:grid-cols-2">
        {cards.map((c) => (
          <Card key={c.bank.slug} {...c} />
        ))}
      </div>

      <div className="mt-6 grid gap-6 lg:grid-cols-[8fr_4fr]">
        <section className="rounded-lg border border-line bg-raise">
          <div className="flex items-baseline gap-4 border-b border-line px-6 py-4">
            <span className="eyebrow">最近</span>
            <span className="ml-auto text-[0.78rem] text-muted">同一入口的连续作答算一次 · 点击回到那个入口</span>
          </div>
          {recent.length === 0 ? (
            <p className="px-6 py-8 text-sm text-muted">还没有作答记录。从上面任意一张卡开始。</p>
          ) : (
            recent.map((s, i) => (
              <Link
                key={`${s.bankSlug}-${s.startedAt}-${i}`}
                href={sessionHref(s)}
                className="grid grid-cols-[6rem_1fr_auto_4.5rem] items-baseline gap-5 border-b border-line-2 px-6 py-3.5 text-[0.9rem] transition-colors last:border-b-0 hover:bg-surface"
              >
                <span className="font-mono text-[0.76rem] text-muted">{timeAgo(s.endedAt)}</span>
                <span>
                  <b className="mr-2 font-semibold">{s.bankSlug.toUpperCase().replace(/^AWS-/, "")}</b>
                  <span className="text-muted">{sessionLabel(s)}</span>
                </span>
                <span className="display text-[1.05rem] tabular-nums">
                  <span style={{ color: "var(--color-ok)" }}>{s.correct}</span>
                  <small className="font-sans text-[0.8rem] font-normal text-muted"> / {s.count}</small>
                </span>
                <span className="text-right text-[0.82rem] text-src-community">继续 →</span>
              </Link>
            ))
          )}
        </section>
        <section className="rounded-lg border border-line bg-raise">
          <div className="flex items-baseline gap-4 border-b border-line px-6 py-4">
            <span className="eyebrow">今日到期</span>
            <span className="ml-auto rounded-sm border border-dashed border-line px-1.5 text-[0.62rem] uppercase tracking-[0.1em] text-muted">P3 · FSRS</span>
          </div>
          <div className="grid gap-2.5 px-6 py-6">
            <div className="display text-[3rem] leading-none text-line">—</div>
            <p className="text-[0.82rem] leading-relaxed text-muted">
              记忆调度上线后，这里是跨题库「今天该复习多少题」—— 首页唯一「必须做」的入口。现在先留位。
            </p>
          </div>
        </section>
      </div>
    </Wide>
  );
}
