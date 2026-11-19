"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import type { Choice } from "@/lib/claims";

/**
 * 题干与选项。选项字母做成印章式方形序号，
 * 选中 / 对 / 错三种状态靠左边色条 + 序号反色表达。
 * 揭晓后每个选项下面显示它自己的点评；自己选的那项用正文色，其余用次要色。
 */
export function QuestionBody({
  stem,
  choices,
  pickCount,
  selected,
  onSelect,
  revealed,
  correctAnswer,
  explain,
}: {
  stem: string;
  choices: Choice[];
  pickCount: number;
  selected?: string[];
  onSelect?: (labels: string[]) => void;
  revealed?: boolean;
  correctAnswer?: string;
  /** 是否显示选项点评；缺省跟随 revealed */
  explain?: boolean;
}) {
  const t = useTranslations("choices");
  const [local, setLocal] = useState<string[]>([]);
  const picked = selected ?? local;
  const interactive = Boolean(onSelect) || selected === undefined;
  const showNotes = explain ?? Boolean(revealed);
  const notesFallback = showNotes && choices.some((c) => c.explanation && !c.explanation.localized);

  function toggle(label: string) {
    if (revealed) return;
    const next =
      pickCount > 1
        ? picked.includes(label)
          ? picked.filter((l) => l !== label)
          : picked.length < pickCount
            ? [...picked, label].sort()
            : picked
        : [label];
    setLocal(next);
    onSelect?.(next);
  }

  return (
    <div className="space-y-6">
      <p className="max-w-3xl text-[1rem] leading-[2]">{stem}</p>
      {notesFallback && <p className="text-xs text-muted">{t("notesFallback")}</p>}
      <ul className="space-y-2.5">
        {choices.map((c) => {
          const isPicked = picked.includes(c.label);
          const isCorrect = revealed && correctAnswer?.includes(c.label);
          const isWrongPick = revealed && isPicked && !isCorrect;
          const edge = isCorrect
            ? "var(--color-ok)"
            : isWrongPick
              ? "var(--color-warn)"
              : isPicked
                ? "var(--color-accent-ink)"   // 多选题揭晓前的「已选」：可点的东西用强调色（层级规则 1）
                : "transparent";
          return (
            <li key={c.label}>
              <button
                type="button"
                disabled={revealed || !interactive}
                onClick={() => toggle(c.label)}
                aria-pressed={isPicked}
                className="flex w-full gap-3.5 rounded-xl border border-transparent bg-raise p-4 text-left text-[0.92rem] leading-[1.85] transition-all disabled:cursor-default enabled:hover:border-accent-ink"
                style={{ boxShadow: `inset 3px 0 0 ${edge}` }}
              >
                <span
                  className="mt-0.5 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-sm border font-mono text-[0.8rem] font-semibold transition-colors"
                  style={
                    isPicked || isCorrect
                      ? { background: edge === "transparent" ? "var(--color-accent-ink)" : edge,
                          borderColor: "transparent",
                          color: "var(--color-raise)" }
                      : { borderColor: "var(--color-line)", color: "var(--color-muted)", background: "var(--color-surface)" }
                  }
                >
                  {c.label}
                </span>
                <span className="flex min-w-0 flex-col gap-1.5">
                  <span>{c.body}</span>
                  {showNotes && c.explanation && (
                    <span
                      className="text-[0.85rem] leading-[1.8]"
                      style={{ color: isPicked ? "var(--color-ink)" : "var(--color-muted)" }}
                    >
                      {c.explanation.body}
                    </span>
                  )}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
