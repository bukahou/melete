import Link from "next/link";
import { notFound } from "next/navigation";
import { AlertTriangle } from "lucide-react";
import { ApiError, getQuestion } from "@/lib/api";
import { ClaimsPanel } from "@/components/ClaimsPanel";
import { QuestionBody } from "@/components/QuestionBody";
import { Markdown } from "@/components/Markdown";
import { TagChip } from "@/components/TagChip";

export const revalidate = 300;

export default async function QuestionPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  let q;
  try {
    q = await getQuestion(Number(id));
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  return (
    <article className="space-y-12">
      <header className="flex flex-wrap items-baseline gap-x-4 gap-y-2 text-sm">
        {/* ⚠️ 这里曾经是「← 题库名」。那个箭头在暗示【浏览器后退】，
            而它其实是一个写死跳到学习台的链接 —— 从刷题页点详情再点它，
            会莫名其妙落在学习台且丢掉刷题位置。
            改成面包屑：它只描述【你在哪】，不承诺「回到你来的地方」。 */}
        <nav className="flex items-center gap-1.5 font-mono text-xs text-muted">
          <Link href="/" className="transition-colors hover:text-ink">首页</Link>
          <span aria-hidden>›</span>
          <Link href={`/banks/${q.bankSlug}`} className="transition-colors hover:text-ink">
            {q.bankSlug}
          </Link>
        </nav>
        <span className="display text-lg" style={{ fontFamily: "var(--font-mono-x)" }}>
          #{q.externalNo}
        </span>
        <span className="text-xs text-muted">{q.kind === "multi" ? `多选 · 选 ${q.pickCount} 项` : "单选"}</span>
      </header>

      {(q.dataIssue || (q.warnings?.length ?? 0) > 0) && (
        <div
          className="flex gap-3 rounded-md border border-line bg-raise p-4 text-xs leading-[1.8]"
          style={{ boxShadow: "inset 3px 0 0 var(--color-warn)" }}
        >
          <AlertTriangle size={15} className="mt-0.5 shrink-0" style={{ color: "var(--color-warn)" }} />
          <div>
            <b>这道题存在数据缺陷</b>
            {q.dataIssue && <p className="mt-1">{q.dataIssue}</p>}
            {(q.warnings?.length ?? 0) > 0 && (
              <p className="mt-1 font-mono text-muted">{q.warnings!.join(" · ")}</p>
            )}
          </div>
        </div>
      )}

      <QuestionBody stem={q.stem} choices={q.choices} pickCount={q.pickCount} selected={[]} />

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

      {q.tags.length > 0 && (
        <section>
          <h3 className="section-rule">
            <span className="eyebrow">标签</span>
          </h3>
          <div className="mt-4 flex flex-wrap gap-2">
            {q.tags.map((t) => (
              <TagChip key={t.id} tag={t} slug={q.bankSlug} />
            ))}
          </div>
        </section>
      )}
    </article>
  );
}
