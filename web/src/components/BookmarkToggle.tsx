"use client";

import { useState } from "react";
import { Bookmark, BookmarkCheck } from "lucide-react";
import { useTranslations } from "next-intl";

/**
 * 收藏按钮（P9 #20）。
 *
 * ⭐ 用户的定位：「答题中『不懂、靠猜』的替代版本」—— 想用的人用，单纯刷题的人不受任何影响。
 *   所以它是一个安静的图标按钮（⛔ 不是实底、不抢「下一题」），放在刷题页顶部不碍事的地方。
 *
 * 点了立刻变（乐观更新），失败再退回并提示 —— 收藏要是点了没反应，用户会再点一次，
 * 结果正好把它取消掉（自选条件那次「点了看不出生效」的教训）。
 */
export function BookmarkToggle({ questionId, initial, compact = false }: {
  questionId: number; initial: boolean; compact?: boolean;
}) {
  const t = useTranslations("bookmark");
  const [on, setOn] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);

  async function toggle(e: React.MouseEvent) {
    e.preventDefault(); // 列表里它在一行链接旁边，⛔ 别触发跳转
    e.stopPropagation();
    if (busy) return;
    const next = !on;
    setOn(next);
    setBusy(true);
    setFailed(false);
    try {
      const res = await fetch(`/api/bookmarks/${questionId}`, { method: next ? "PUT" : "DELETE" });
      if (!res.ok) throw new Error(String(res.status));
    } catch {
      setOn(!next);
      setFailed(true);
    } finally {
      setBusy(false);
    }
  }

  const Icon = on ? BookmarkCheck : Bookmark;
  return (
    <button
      type="button"
      onClick={toggle}
      aria-pressed={on}
      title={failed ? t("failed") : on ? t("remove") : t("add")}
      className={`inline-flex shrink-0 items-center gap-1 rounded-full transition-colors ${
        compact ? "p-1.5" : "px-2.5 py-1 text-[0.78rem]"
      } ${on ? "text-accent-ink" : "text-muted hover:text-accent-ink"} ${failed ? "ring-1 ring-[var(--color-warn)]" : ""}`}
    >
      <Icon size={compact ? 17 : 15} fill={on ? "currentColor" : "none"} />
      {!compact && <span className={on ? "font-semibold" : ""}>{failed ? t("failed") : on ? t("on") : t("off")}</span>}
    </button>
  );
}
