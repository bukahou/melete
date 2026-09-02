import Link from "next/link";
import { notFound } from "next/navigation";
import { AlertTriangle, ArrowLeft, ArrowRight, ExternalLink } from "lucide-react";
import { ApiError, getQuestion, listQuestions, type DrillMode } from "@/lib/api";
import { ClaimsPanel } from "@/components/ClaimsPanel";
import { DrillCard } from "@/components/DrillCard";
import { Markdown } from "@/components/Markdown";
import { TagChip } from "@/components/TagChip";

type Search = {
  i?: string;
  contested?: string;
  enriched?: string;
  mode?: string;
  tag?: string | string[];
};

const MODE_TITLE: Record<DrillMode, string> = {
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
  const tags = (Array.isArray(sp.tag) ? sp.tag : sp.tag ? [sp.tag] : []).map(Number).filter(Boolean);
  const mode = (["wrong", "unsure", "unseen"].includes(sp.mode ?? "") ? sp.mode : undefined) as
    | DrillMode
    | undefined;
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

  const linkTo = (i: number) => {
    const q = new URLSearchParams();
    q.set("i", String(i));
    if (filters.contested) q.set("contested", "true");
    if (filters.enriched) q.set("enriched", "true");
    if (mode) q.set("mode", mode);
    tags.forEach((t) => q.append("tag", String(t)));
    return `/banks/${slug}/drill?${q}`;
  };

  if (page.items.length === 0) {
    return (
      <div className="space-y-5 pt-10 text-center">
        <p className="display text-xl text-muted">
          {page.total === 0 ? "当前筛选条件下没有题目" : "已经是最后一题了"}
        </p>
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

  const q = await getQuestion(page.items[0].id);
  const reference = q.reference ?? null;

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
        <Link
          href={`/questions/${q.id}`}
          className="ml-auto inline-flex items-center gap-1 text-xs text-muted transition-colors hover:text-ink"
        >
          详情页
          <ExternalLink size={12} />
        </Link>
      </header>

      {/* 进度条：hairline 轨道 + 墨色进度（dataviz：数据是唯一允许大声的东西） */}
      <div className="h-[3px] overflow-hidden rounded-full bg-line" role="progressbar"
           aria-valuenow={index + 1} aria-valuemin={1} aria-valuemax={page.total}>
        <div
          className="h-full rounded-full transition-all"
          style={{ width: `${((index + 1) / page.total) * 100}%`, background: "var(--color-ink)" }}
        />
      </div>

      <DrillCard questionId={q.id} stem={q.stem} choices={q.choices} pickCount={q.pickCount} reference={reference}>
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

      <nav className="flex items-center justify-between border-t border-line pt-6">
        {index > 0 ? (
          <Link href={linkTo(index - 1)} className="inline-flex items-center gap-1.5 text-sm text-muted transition-colors hover:text-ink">
            <ArrowLeft size={15} />
            上一题
          </Link>
        ) : (
          <span />
        )}
        {index + 1 < page.total && (
          <Link
            href={linkTo(index + 1)}
            className="inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-sm font-medium"
            style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
          >
            下一题
            <ArrowRight size={15} />
          </Link>
        )}
      </nav>
    </div>
  );
}
