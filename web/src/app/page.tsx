import Link from "next/link";
import { ArrowRight, ArrowUpRight, Play, TrendingDown } from "lucide-react";
import { getMyProgress, getMyResume, getMyTagStats } from "@/lib/api";
import { RateBar } from "@/components/RateBar";
import {
  SOURCE_LABEL,
  getBank,
  getQuestion,
  listBanks,
  listQuestions,
  type AnswerClaim,
  type QuestionDetail,
} from "@/lib/api";

export const revalidate = 60;

/** 首页 teaser 用的迷你主张章：来源色块 + 标签 + 答案字母。身份 = 色 + 文字，从不只靠色。 */
function ClaimSeal({ claim }: { claim: AnswerClaim }) {
  const color = `var(--color-src-${{ bank_label: "bank", community_vote: "community", ai_verdict: "ai", user_note: "ai" }[claim.source]})`;
  return (
    <span className="inline-flex items-baseline gap-2 rounded-sm border border-line bg-raise py-1.5 pl-2.5 pr-3 text-xs">
      <span className="h-2 w-2 translate-y-px rounded-full" style={{ background: color }} aria-hidden />
      <span className="text-muted">{SOURCE_LABEL[claim.source]}</span>
      <span className="display text-base leading-none" style={{ fontFamily: "var(--font-mono-x)" }}>
        {claim.answer}
      </span>
      {claim.confidence != null && <span className="tabular-nums text-muted">{claim.confidence}%</span>}
    </span>
  );
}

async function findTeaser(slug: string): Promise<QuestionDetail | null> {
  try {
    const page = await listQuestions(slug, { contested: true, enriched: true, limit: 1 });
    if (page.items.length === 0) return null;
    return await getQuestion(page.items[0].id);
  } catch {
    return null;
  }
}

/**
 * 登录后的学习概览。
 *
 * 与下方的宣言段并存而非替换：宣言解释「这个产品为什么这样设计」，
 * 概览回答「我现在该做什么」—— 后者放在最上面，因为老用户每天都要看它。
 */
async function StudyOverview() {
  const [progress, resume, weakest] = await Promise.all([
    getMyProgress(),
    getMyResume(),
    getMyTagStats("service", 2),
  ]);
  if (progress.seenCount === 0) return null;

  const rate = Math.round((progress.correctCount / progress.seenCount) * 100);
  const slug = progress.bankSlug;

  return (
    <section className="space-y-6">
      <h2 className="section-rule">
        <span className="eyebrow">继续学习</span>
        <Link href="/me" className="text-xs text-muted transition-colors hover:text-ink">
          全部统计 →
        </Link>
      </h2>

      <div className="grid gap-4 sm:grid-cols-[1fr_auto]">
        {/* 断点：从 attempt 推导，不额外维护状态 */}
        <Link
          href={`/banks/${slug}/drill`}
          className="group rounded-md border border-line bg-raise p-5 transition-colors hover:border-muted"
        >
          {resume.questionId ? (
            <>
              <p className="font-mono text-xs text-muted">
                上次做到 {resume.bankSlug} · #{resume.externalNo}
              </p>
              <p className="mt-2.5 line-clamp-2 text-[0.92rem] leading-relaxed">{resume.stem}</p>
            </>
          ) : (
            <p className="text-sm text-muted">开始第一题</p>
          )}
          <p className="mt-4 inline-flex items-center gap-1.5 text-sm" style={{ color: "var(--color-src-community)" }}>
            <Play size={14} />
            继续刷题
          </p>
        </Link>

        <dl className="flex gap-7 rounded-md border border-line bg-raise px-5 py-4 sm:flex-col sm:gap-3">
          {[
            ["做过", progress.seenCount, undefined],
            ["正确率", `${rate}%`, rate < 60 ? "var(--color-warn)" : "var(--color-ok)"],
            ["错题", progress.wrongCount, "var(--color-warn)"],
          ].map(([label, v, color]) => (
            <div key={label as string}>
              <dd className="display text-xl leading-none tabular-nums" style={color ? { color: color as string } : undefined}>
                {v as string | number}
              </dd>
              <dt className="mt-1 text-[0.7rem] text-muted">{label as string}</dt>
            </div>
          ))}
        </dl>
      </div>

      {/* 弱项 Top 3：把统计直接变成下一步行动 */}
      {weakest.length > 0 && (
        <div className="rounded-md border border-line bg-raise p-5">
          <p className="inline-flex items-center gap-1.5 text-xs font-medium" style={{ color: "var(--color-warn)" }}>
            <TrendingDown size={13} />
            最薄弱的服务
          </p>
          <div className="mt-3">
            {weakest.slice(0, 3).map((s) => (
              <Link key={s.tagId} href={`/banks/${slug}/drill?tag=${s.tagId}`} className="block hover:opacity-75">
                <RateBar stat={s} />
              </Link>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}

export default async function HomePage() {
  const banks = await listBanks();
  const details = await Promise.all(banks.map((b) => getBank(b.slug)));
  const teaser = details.length > 0 ? await findTeaser(details[0].slug) : null;

  // 分歧数据带取第一个题库（当前也只有一个）；多题库后改为聚合
  const lead = details[0];
  const contestedPct = lead ? Math.round((lead.stats.contestedCount / lead.stats.questionCount) * 100) : 0;

  return (
    <div className="space-y-20">
      {/* 有作答记录时才显示；新用户直接看宣言 */}
      <StudyOverview />

      {/* ---- 宣言 ---- */}
      <section className="pt-4">
        <p className="eyebrow">Melete · <span className="normal-case">Μελέτη</span> — 练习与修习</p>
        <h1 className="display mt-5 text-[2.5rem] leading-[1.22] sm:text-[3.25rem]">
          被动阅读不产生学习，
          <br />
          <em className="mark-em">主动回忆</em>才产生。
        </h1>
        <p className="mt-6 max-w-xl text-[0.95rem] leading-[1.9] text-muted">
          题目是第一等公民。每道题并列保存题库标注、社区投票与 AI
          裁决三方主张 —— 当它们互相打架时，分歧本身就是最好的学习材料。
        </p>
      </section>

      {/* ---- 分歧数据带：这个项目存在的理由 ---- */}
      {lead && (
        <section className="grid gap-10 border-y border-line py-10 sm:grid-cols-[auto_1fr] sm:gap-14">
          <div>
            <div className="flex items-baseline gap-1">
              <span className="display text-[3.5rem] leading-none tabular-nums">{contestedPct}</span>
              <span className="display text-2xl text-muted">%</span>
            </div>
            <p className="mt-2 max-w-[16rem] text-sm leading-relaxed">
              的题目，题库标注答案与社区投票<b>不一致</b>
            </p>
          </div>
          <div className="flex flex-col justify-center gap-3">
            <div className="meter" role="img" aria-label={`${lead.stats.contestedCount} 道分歧，${lead.stats.questionCount - lead.stats.contestedCount} 道无分歧`}>
              <span style={{ flex: lead.stats.contestedCount, background: "var(--color-warn)" }} />
              <span style={{ flex: lead.stats.questionCount - lead.stats.contestedCount, background: "var(--color-line)" }} />
            </div>
            <div className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted">
              <span className="inline-flex items-center gap-1.5">
                <span className="h-2 w-2 rounded-full" style={{ background: "var(--color-warn)" }} aria-hidden />
                答案有分歧 <b className="tabular-nums text-ink">{lead.stats.contestedCount}</b>
              </span>
              <span className="inline-flex items-center gap-1.5">
                <span className="h-2 w-2 rounded-full bg-line" aria-hidden />
                无分歧 <b className="tabular-nums text-ink">{lead.stats.questionCount - lead.stats.contestedCount}</b>
              </span>
            </div>
            <p className="text-xs leading-relaxed text-muted">
              个别题目有高达 98% 的社区投票一致反对题库标注 ——
              照搬标注答案，会背错相当一部分。
            </p>
          </div>
        </section>
      )}

      {/* ---- 一道正在打架的题 ---- */}
      {teaser && (
        <section>
          <h2 className="section-rule">
            <span className="eyebrow">一道正在打架的题</span>
          </h2>
          <Link
            href={`/questions/${teaser.id}`}
            className="group mt-6 block rounded-md border border-line bg-raise p-6 transition-colors hover:border-muted"
          >
            <p className="font-mono text-xs text-muted">
              {teaser.bankSlug} · #{teaser.externalNo}
            </p>
            <p className="mt-3 line-clamp-2 max-w-2xl text-[0.95rem] leading-[1.85]">{teaser.stem}</p>
            <div className="mt-5 flex flex-wrap items-center gap-2.5">
              {teaser.claims.map((c) => (
                <ClaimSeal key={c.source} claim={c} />
              ))}
            </div>
            <p className="mt-5 inline-flex items-center gap-1 text-sm text-src-community">
              看 AI 怎么裁
              <ArrowUpRight size={15} className="transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5" />
            </p>
          </Link>
        </section>
      )}

      {/* ---- 题库目录 ---- */}
      <section>
        <h2 className="section-rule">
          <span className="eyebrow">题库</span>
        </h2>
        <ol className="mt-2">
          {details.map((b, i) => (
            <li key={b.slug} className={i > 0 ? "border-t border-line" : ""}>
              <Link href={`/banks/${b.slug}`} className="group flex flex-col gap-4 py-7 sm:flex-row sm:items-center sm:gap-8">
                <span className="font-mono text-xs text-muted">{String(i + 1).padStart(2, "0")}</span>
                <div className="min-w-0 flex-1">
                  <h3 className="display text-xl leading-snug transition-colors group-hover:text-src-community">
                    {b.name}
                  </h3>
                  <p className="mt-1.5 font-mono text-xs text-muted">
                    {b.slug} · {b.locale}
                  </p>
                </div>
                <dl className="flex shrink-0 gap-7 text-right sm:text-left">
                  {[
                    ["题目", b.stats.questionCount, "var(--color-ink)"],
                    ["已解析", b.stats.enrichedCount, "var(--color-ink)"],
                    ["有分歧", b.stats.contestedCount, "var(--color-warn)"],
                  ].map(([label, n, color]) => (
                    <div key={label as string}>
                      <dd className="display text-2xl leading-none tabular-nums" style={{ color: color as string }}>
                        {n as number}
                      </dd>
                      <dt className="mt-1.5 text-xs text-muted">{label as string}</dt>
                    </div>
                  ))}
                </dl>
                <ArrowRight
                  size={18}
                  className="hidden shrink-0 text-muted transition-transform group-hover:translate-x-1 sm:block"
                />
              </Link>
            </li>
          ))}
        </ol>
      </section>
    </div>
  );
}
