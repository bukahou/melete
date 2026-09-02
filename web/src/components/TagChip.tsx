import Link from "next/link";
import type { Tag } from "@/lib/claims";

const TYPE_STYLE: Record<Tag["type"], { label: string; color: string }> = {
  domain: { label: "考纲域", color: "var(--color-src-bank)" },
  service: { label: "服务", color: "var(--color-src-community)" },
  concept: { label: "概念", color: "var(--color-src-ai)" },
};

/** concept 标签跨题库共享 —— 它回答「缺的是 AWS 知识还是底层原理」。 */
export function TagChip({ tag, slug }: { tag: Tag; slug?: string }) {
  const { color } = TYPE_STYLE[tag.type];
  const name = tag.i18n?.en ?? tag.value;
  const chip = (
    <span className="inline-flex items-center gap-1.5 rounded-sm border border-line bg-raise px-2.5 py-1 text-xs text-ink transition-colors">
      <span className="h-1.5 w-1.5 rounded-full" style={{ background: color }} aria-hidden />
      {name}
      {tag.questionCount != null && (
        <span className="tabular-nums text-muted">{tag.questionCount}</span>
      )}
    </span>
  );
  return slug ? (
    <Link href={`/banks/${slug}/drill?tag=${tag.id}`} className="transition-opacity hover:opacity-70">
      {chip}
    </Link>
  ) : (
    chip
  );
}

export const TAG_TYPE_LABEL = TYPE_STYLE;
