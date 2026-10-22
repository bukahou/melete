import Link from "next/link";
import { Check, X } from "lucide-react";
import { useTranslations } from "next-intl";
import { BookmarkToggle } from "./BookmarkToggle";

/**
 * 一行题目（P9 #44，照 it-pass「令和8年度」那一页）：题号 · 这题考什么 · 上次对错 · 收藏。
 * 整行（收藏之外）可点 —— 从这题开始。题目列表页与「自选条件」共用；外层用 <ol>。
 * label 由 lib/questionSet 的 questionLabel 给（考点，没有考点退回题干开头）。
 */
export function QuestionRow({ href, id, no, label, last, bookmarked }: {
  href: string; id: number; no: number; label: string; last: boolean | null; bookmarked: boolean;
}) {
  const t = useTranslations("practice");
  return (
    <li className="mx-2 flex items-center border-b border-line-2 pr-1 transition-colors hover:bg-accent-soft">
      <Link href={href} className="flex min-h-14 min-w-0 flex-1 items-center gap-4 py-2 pl-3">
        <span className="w-14 shrink-0 tabular-nums">#{no}</span>
        <span className="min-w-0 flex-1 truncate text-[0.92rem]">{label}</span>
        {/* 上次对错：固定宽度占位，两列图标才对得齐 */}
        <span className="flex w-4 shrink-0 justify-center">
          {last === true && <Check size={16} style={{ color: "var(--color-ok)" }} aria-label={t("pickLastOk")} />}
          {last === false && <X size={16} style={{ color: "var(--color-warn)" }} aria-label={t("pickLastNg")} />}
        </span>
      </Link>
      {/* 同 it-pass：列表里直接收藏 / 取消（不进题也能整理） */}
      <BookmarkToggle key={`${id}-${bookmarked}`} questionId={id} initial={bookmarked} compact />
    </li>
  );
}
