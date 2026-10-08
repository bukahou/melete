import Link from "next/link";
import { useLocale } from "next-intl";
import { tagName, type Tag } from "@/lib/claims";

/**
 * 角色色是通用的（三条轴各一色，且与来源色复用同一组 token），
 * 文案不是 —— 文案走 tagTypeLabel(bank.meta, type)，这里不出现任何题库词。
 */
export const TAG_TYPE_COLOR: Record<Tag["type"], string> = {
  domain: "var(--color-src-bank)",
  topic: "var(--color-src-community)",
  concept: "var(--color-src-ai)",
};

/** concept 标签跨题库共享 —— 它回答「缺的是 AWS 知识还是底层原理」。 */
export function TagChip({ tag, slug }: { tag: Tag; slug?: string }) {
  const color = TAG_TYPE_COLOR[tag.type];
  // 同 RateBar：⛔ 不再写死 en。标签名跟界面语言走。
  const name = tagName(tag, useLocale());
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

