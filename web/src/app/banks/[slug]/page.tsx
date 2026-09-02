import Link from "next/link";
import { notFound } from "next/navigation";
import { AlertTriangle, CircleDashed, HelpCircle, Play, Sparkles, XCircle } from "lucide-react";
import { ApiError, getBank, listBankTags, type Tag } from "@/lib/api";
import { TagChip, TAG_TYPE_LABEL } from "@/components/TagChip";

export const revalidate = 60;

function TagGroup({ type, tags, slug }: { type: Tag["type"]; tags: Tag[]; slug: string }) {
  if (tags.length === 0) return null;
  return (
    <section>
      <h3 className="section-rule">
        <span className="eyebrow">{TAG_TYPE_LABEL[type].label}</span>
        <span className="font-mono text-xs text-muted">{tags.length}</span>
      </h3>
      <div className="mt-4 flex flex-wrap gap-2">
        {tags.map((t) => (
          <TagChip key={t.id} tag={t} slug={slug} />
        ))}
      </div>
    </section>
  );
}

export default async function BankPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  let bank, tags;
  try {
    [bank, tags] = await Promise.all([getBank(slug), listBankTags(slug)]);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  const byType = (t: Tag["type"]) => tags.filter((x) => x.type === t);
  const unenriched = bank.stats.questionCount - bank.stats.enrichedCount;
  const stats: Array<[string, number, string?]> = [
    ["题目", bank.stats.questionCount],
    ["已解析", bank.stats.enrichedCount],
    ["答案有分歧", bank.stats.contestedCount, "var(--color-warn)"],
  ];

  return (
    <div className="space-y-14">
      <header>
        <p className="font-mono text-xs text-muted">{bank.slug}</p>
        <h1 className="display mt-3 max-w-2xl text-3xl leading-[1.35]">{bank.name}</h1>

        <dl className="mt-8 flex gap-10 border-y border-line py-6">
          {stats.map(([label, n, color]) => (
            <div key={label}>
              <dd className="display text-[2rem] leading-none tabular-nums" style={color ? { color } : undefined}>
                {n}
              </dd>
              <dt className="mt-2 text-xs text-muted">{label}</dt>
            </div>
          ))}
        </dl>
      </header>

      <div className="flex flex-wrap gap-3">
        <Link
          href={`/banks/${slug}/drill`}
          className="inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-sm font-medium"
          style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
        >
          <Play size={15} />
          开始刷题
        </Link>
        <Link
          href={`/banks/${slug}/drill?contested=true`}
          className="inline-flex items-center gap-2 rounded-md border border-line bg-raise px-5 py-2.5 text-sm transition-colors hover:border-muted"
        >
          <AlertTriangle size={15} style={{ color: "var(--color-warn)" }} />
          只刷有分歧的 <b className="tabular-nums">{bank.stats.contestedCount}</b> 道
        </Link>
        <Link
          href={`/banks/${slug}/drill?enriched=true`}
          className="inline-flex items-center gap-2 rounded-md border border-line bg-raise px-5 py-2.5 text-sm transition-colors hover:border-muted"
        >
          <Sparkles size={15} style={{ color: "var(--color-src-ai)" }} />
          只刷有解析的 <b className="tabular-nums">{bank.stats.enrichedCount}</b> 道
        </Link>
      </div>

      <div>
        <h3 className="section-rule">
          <span className="eyebrow">我的</span>
        </h3>
        <div className="mt-4 flex flex-wrap gap-3">
          <Link
            href={`/banks/${slug}/drill?mode=wrong`}
            className="inline-flex items-center gap-2 rounded-md border border-line bg-raise px-4 py-2 text-sm transition-colors hover:border-muted"
          >
            <XCircle size={14} style={{ color: "var(--color-warn)" }} />
            错题本
          </Link>
          <Link
            href={`/banks/${slug}/drill?mode=unsure`}
            className="inline-flex items-center gap-2 rounded-md border border-line bg-raise px-4 py-2 text-sm transition-colors hover:border-muted"
          >
            <HelpCircle size={14} style={{ color: "var(--color-src-bank)" }} />
            不清楚的
          </Link>
          <Link
            href={`/banks/${slug}/drill?mode=unseen`}
            className="inline-flex items-center gap-2 rounded-md border border-line bg-raise px-4 py-2 text-sm transition-colors hover:border-muted"
          >
            <CircleDashed size={14} className="text-muted" />
            没做过的
          </Link>
        </div>
        <p className="mt-3 text-xs leading-relaxed text-muted">
          错题本收「最近一次答错」的题 —— 后来做对了会自动移出；
          「不清楚的」收自评为 不会/模糊 的题。
        </p>
      </div>

      {unenriched > 0 && (
        <p className="max-w-2xl text-xs leading-[1.9] text-muted">
          还有 <b className="text-ink">{unenriched}</b> 道题尚未完成 AI 富化（裁决 / 解析 / 标签）。
          题干与各方答案主张已可用，解析会随富化推进陆续补齐。
        </p>
      )}

      <div className="space-y-10">
        <TagGroup type="domain" tags={byType("domain")} slug={slug} />
        <TagGroup type="service" tags={byType("service")} slug={slug} />
        <TagGroup type="concept" tags={byType("concept")} slug={slug} />
      </div>
    </div>
  );
}
