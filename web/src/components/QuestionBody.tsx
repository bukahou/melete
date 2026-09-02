"use client";

import { useState } from "react";
import type { Choice } from "@/lib/claims";

/**
 * 题干与选项。选项字母做成印章式方形序号，
 * 选中 / 对 / 错三种状态靠左边色条 + 序号反色表达。
 */
export function QuestionBody({
  stem,
  choices,
  pickCount,
  selected,
  onSelect,
  revealed,
  correctAnswer,
}: {
  stem: string;
  choices: Choice[];
  pickCount: number;
  selected?: string[];
  onSelect?: (labels: string[]) => void;
  revealed?: boolean;
  correctAnswer?: string;
}) {
  const [local, setLocal] = useState<string[]>([]);
  const picked = selected ?? local;
  const interactive = Boolean(onSelect) || selected === undefined;

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
                ? "var(--color-ink)"
                : "transparent";
          return (
            <li key={c.label}>
              <button
                type="button"
                disabled={revealed || !interactive}
                onClick={() => toggle(c.label)}
                aria-pressed={isPicked}
                className="flex w-full gap-3.5 rounded-md border border-line bg-raise p-4 text-left text-[0.92rem] leading-[1.85] transition-all disabled:cursor-default enabled:hover:border-muted"
                style={{ boxShadow: `inset 3px 0 0 ${edge}` }}
              >
                <span
                  className="mt-0.5 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-sm border font-mono text-[0.8rem] font-semibold transition-colors"
                  style={
                    isPicked || isCorrect
                      ? { background: edge === "transparent" ? "var(--color-ink)" : edge,
                          borderColor: "transparent",
                          color: "var(--color-raise)" }
                      : { borderColor: "var(--color-line)", color: "var(--color-muted)" }
                  }
                >
                  {c.label}
                </span>
                <span>{c.body}</span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
